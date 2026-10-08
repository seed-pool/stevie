package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stevie-media/stevie/apps/server/internal/artwork"
	"github.com/stevie-media/stevie/apps/server/internal/auth"
	"github.com/stevie-media/stevie/apps/server/internal/config"
	"github.com/stevie-media/stevie/apps/server/internal/ffprobe"
	"github.com/stevie-media/stevie/apps/server/internal/livetv"
	"github.com/stevie-media/stevie/apps/server/internal/playback"
	"github.com/stevie-media/stevie/apps/server/internal/scanner"
	"github.com/stevie-media/stevie/apps/server/internal/sportsdb"
	"github.com/stevie-media/stevie/apps/server/internal/store"
	"github.com/stevie-media/stevie/apps/server/internal/streamed"
	"github.com/stevie-media/stevie/apps/server/internal/teamlogo"
	"github.com/stevie-media/stevie/apps/server/internal/tmdb"
)

const sessionCookie = "stevie_session"

type Server struct {
	cfg      config.Config
	store    *store.Store
	rdb      *redis.Client
	scan     *scanner.Service
	tmdb     *tmdb.Client
	artwork  *artwork.Cache
	tokens   *playback.TokenStore
	live     *livetv.Service
	xtream   *livetv.SyncService
	vod      *livetv.VodSyncService
	recorder *playback.Recorder
	vodDL    *playback.VodDownloader
	recMeta  *recordingMetaCache
	logoIdx  *logoIndexCache
	sports    *sportsdb.SyncService
	streamed  *streamed.Client
	teamLogos *teamlogo.Catalog
}

// New builds the HTTP API. If bg is non-nil, the recording scheduler runs until bg is cancelled.
// The returned shutdown func gracefully stops active recordings (finalize MKVs) before KillAll.
func New(cfg config.Config, st *store.Store, rdb *redis.Client, scan *scanner.Service, tm *tmdb.Client, art *artwork.Cache, bg context.Context) (http.Handler, func()) {
	live := livetv.NewService(st, cfg.LiveLocalRoots(), cfg.ResolveLiveSource)
	xtreamClient := livetv.NewXtreamClient(livetv.XtreamCredentials{
		BaseURL:  cfg.XtreamURL,
		Username: cfg.XtreamUser,
		Password: cfg.XtreamPassword,
	})
	xtreamSync := livetv.NewSyncService(st, xtreamClient, rdb, cfg.LiveLocalRoots(), cfg.ResolveLiveSource)
	vodSync := livetv.NewVodSyncService(st, xtreamClient, rdb)
	live.SetSync(xtreamSync)
	recorder, recErr := playback.NewRecorder(cfg.RecordingsMount)
	if recErr != nil {
		slog.Warn("live recording disabled", "dir", cfg.RecordingsMount, "err", recErr)
		recorder = nil
	} else {
		slog.Info("live recordings", "mount", cfg.RecordingsMount, "host", cfg.RecordingsHost)
		recorder.RepairIncompleteAsync()
	}
	vodDL, vodDLErr := playback.NewVodDownloader(cfg.RecordingsMount)
	if vodDLErr != nil {
		slog.Warn("vod downloads disabled", "dir", cfg.RecordingsMount, "err", vodDLErr)
		vodDL = nil
	}
	sportsClient := sportsdb.NewClient(cfg.TheSportsDBAPIKey)
	sportsSync := sportsdb.NewSyncService(st, sportsClient, cfg.TheSportsDBCountries)
	streamedClient := streamed.NewClient(cfg.StreamedBaseURL)
	sportsSync.SetStreamed(streamedClient)
	var logos *teamlogo.Catalog
	if cat, err := teamlogo.Open(cfg.TeamLogosDir); err != nil {
		slog.Warn("bundled team logos unavailable", "dir", cfg.TeamLogosDir, "err", err)
	} else {
		logos = cat
		sportsSync.SetTeamLogoCatalog(cat)
		slog.Info("bundled team logos", "dir", cfg.TeamLogosDir, "teams", len(cat.Teams()))
	}
	s := &Server{
		cfg:       cfg,
		store:     st,
		rdb:       rdb,
		scan:      scan,
		tmdb:      tm,
		artwork:   art,
		tokens:    playback.NewTokenStore(rdb),
		live:      live,
		xtream:    xtreamSync,
		vod:       vodSync,
		recorder:  recorder,
		vodDL:     vodDL,
		recMeta:   newRecordingMetaCache(),
		logoIdx:   newLogoIndexCache(2 * time.Minute),
		sports:    sportsSync,
		streamed:  streamedClient,
		teamLogos: logos,
	}
	if bg != nil && recorder != nil {
		bridge := playback.NewScheduleBridge(recorder, st, loopbackHTTPBase(cfg.HTTPAddr))
		go playback.NewScheduler(st, bridge).Run(bg)
		slog.Info("scheduled recording poller started")
	}
	if bg != nil {
		go sportsSync.Run(bg)
		slog.Info("sports sync started", "countries", cfg.TheSportsDBCountries)
	}
	shutdownRecordings := func() {
		if vodDL != nil {
			slog.Info("stopping active vod downloads")
			vodDL.StopAll()
		}
		if recorder != nil {
			slog.Info("stopping active live recordings")
			recorder.StopAll()
		}
	}
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	r.Get("/readyz", s.handleReady)

	r.Route("/api", func(r chi.Router) {
		r.Post("/auth/login", s.handleLogin)
		r.Post("/auth/logout", s.handleLogout)
		r.Get("/auth/me", s.handleMe)
		r.Post("/auth/handoff", s.handleConsumeHandoff)

		// Long-lived media streaming (cookie or stream token). No request timeout.
		r.Get("/media/{id}/stream", s.handleStreamMedia)
		r.Get("/media/{id}/remux", s.handleRemuxMedia)
		r.Get("/media/{id}/subtitles/{index}.vtt", s.handleSubtitleVTT)
		r.Get("/media/{id}/external.m3u", s.handleExternalPlaylist)
		r.Get("/live/channels/{id}/stream", s.handleLiveStream)
		r.Get("/live/channels/{id}/relay", s.handleLiveRelay)
		r.Get("/live/channels/{id}/external.m3u", s.handleLiveExternalPlaylist)
		r.Get("/live/channels/{id}/play-info", s.handleLivePlayInfo)
		r.Get("/live/recordings/file/{name}/stream", s.handleStreamRecordingFile)
		r.Get("/live/recordings/file/{name}/remux", s.handleRemuxRecordingFile)
		r.Get("/vod/movies/{id}/remux", s.handleVodMovieRemux)
		r.Get("/vod/series/{id}/episodes/{epId}/remux", s.handleVodEpisodeRemux)
		// Streamed HLS for browser + loopback ffmpeg (cookie or loopback via authorizeLiveMedia).
		r.Get("/sports/streamed/playlist", s.handleStreamedPlaylist)
		r.Get("/sports/streamed/hls", s.handleStreamedHLS)
		// Bundled team crests (same auth model as live logo — cookie or we keep it authed below).
		r.Get("/sports/logo", s.handleSportsLogo)

		// Playlist / EPG refresh can download large files.
		r.Group(func(r chi.Router) {
			r.Use(middleware.Timeout(15 * time.Minute))
			r.Use(s.requireAuth)
			r.Put("/settings/live", s.handlePutLiveSettings)
			r.Post("/live/refresh", s.handleRefreshLive)
			r.Post("/live/epg/refresh", s.handleRefreshEPG)
			r.Post("/live/xtream/sync", s.handleXtreamSyncStart)
			r.Post("/vod/xtream/sync", s.handleVodSyncStart)
			r.Post("/sports/sync", s.handleSportsSync)
		})

		r.Group(func(r chi.Router) {
			r.Use(middleware.Timeout(60 * time.Second))
			r.Use(s.requireAuth)
			r.Post("/auth/handoff/create", s.handleCreateHandoff)
			r.Post("/auth/prepare-live", s.handlePrepareLiveSession)
			r.Get("/status", s.handleStatus)
			r.Get("/libraries", s.handleListLibraries)
			r.Post("/libraries/{id}/scan", s.handleScanLibrary)
			r.Get("/libraries/{id}/scan", s.handleScanProgress)
			r.Get("/movies", s.handleListMovies)
			r.Get("/movies/{id}", s.handleGetMovie)
			r.Get("/shows", s.handleListShows)
			r.Get("/shows/{id}", s.handleGetShow)
			r.Get("/episodes/{id}/media", s.handleGetEpisodeMedia)
			r.Get("/media/{id}", s.handleGetMedia)
			r.Get("/media/{id}/playback", s.handleGetPlayback)
			r.Post("/media/{id}/playback-token", s.handleCreatePlaybackToken)
			r.Get("/search", s.handleSearch)
			r.Get("/tmdb/search", s.handleTMDBSearch)
			r.Post("/media/{id}/match", s.handleMatch)
			r.Get("/artwork/{kind}", s.handleArtwork)
			r.Get("/settings/live", s.handleGetLiveSettings)
			r.Get("/live/channels", s.handleListLiveChannels)
			r.Get("/live/channels/{id}", s.handleGetLiveChannel)
			r.Put("/live/channels/{id}/favorite", s.handleLiveFavorite)
			r.Post("/live/channels/{id}/play-token", s.handleLivePlayToken)
			r.Post("/live/channels/{id}/recording", s.handleStartLiveRecording)
			r.Delete("/live/channels/{id}/recording", s.handleStopLiveRecording)
			r.Get("/live/channels/{id}/recording", s.handleLiveRecordingStatus)
			r.Get("/live/recordings", s.handleListLiveRecordings)
			r.Get("/live/recordings/file/{name}", s.handleGetRecordingFile)
			r.Delete("/live/recordings/file/{name}", s.handleDeleteRecordingFile)
			r.Post("/live/recordings/schedule", s.handleCreateScheduledRecording)
			r.Get("/live/recordings/schedule", s.handleListScheduledRecordings)
			r.Delete("/live/recordings/schedule/{id}", s.handleCancelScheduledRecording)
			r.Get("/live/groups", s.handleLiveGroups)
			r.Get("/live/categories", s.handleLiveCategories)
			r.Get("/live/guide", s.handleLiveGuide)
			r.Get("/live/logo", s.handleLiveLogo)
			r.Get("/live/xtream/categories", s.handleXtreamCategories)
			r.Get("/live/xtream/sync", s.handleXtreamSyncProgress)
			r.Get("/live/epg/refresh", s.handleEPGRefreshProgress)
			r.Get("/vod/xtream/categories", s.handleVodCategories)
			r.Get("/vod/xtream/sync", s.handleVodSyncProgress)
			r.Get("/vod/categories", s.handleListImportedVodCategories)
			r.Get("/vod/movies", s.handleListVodMovies)
			r.Get("/vod/movies/{id}", s.handleGetVodMovie)
			r.Post("/vod/movies/{id}/download", s.handleVodMovieDownload)
			r.Post("/vod/movies/{id}/analyze", s.handleVodMovieAnalyze)
			r.Get("/vod/series", s.handleListVodSeries)
			r.Get("/vod/series/{id}", s.handleGetVodSeries)
			r.Post("/vod/series/{id}/episodes/{epId}/download", s.handleVodEpisodeDownload)
			r.Get("/vod/downloads", s.handleVodDownloads)
			r.Delete("/vod/downloads/{id}", s.handleVodDownloadCancel)
			r.Get("/sports/meta", s.handleSportsMeta)
			r.Get("/sports/events", s.handleSportsEvents)
			r.Get("/sports/streamed/play", s.handleStreamedPlay)
			r.Post("/sports/streamed/recording", s.handleStartStreamedRecording)
			r.Delete("/sports/streamed/recording", s.handleStopStreamedRecording)
			r.Get("/sports/streamed/recording", s.handleStreamedRecordingStatus)
		})
	})
	return r, shutdownRecordings
}

func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := s.store.Pool().Ping(ctx); err != nil {
		writeErr(w, http.StatusServiceUnavailable, "postgres unavailable")
		return
	}
	if err := s.rdb.Ping(ctx).Err(); err != nil {
		writeErr(w, http.StatusServiceUnavailable, "redis unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"domain":                s.cfg.StevieDomain,
		"https_port":            s.cfg.StevieHTTPSPort,
		"listen_port":           s.cfg.StevieListenPort,
		"local_mode":            s.cfg.IsLocal(),
		"tmdb_configured":       s.tmdb != nil && s.tmdb.Enabled(),
		"media_host_path":       s.cfg.MediaLibraryHost,
		"media_mount_path":      s.cfg.MediaLibraryMount,
		"recordings_host_path":  s.cfg.RecordingsHost,
		"recordings_mount_path": s.cfg.RecordingsMount,
		"recording_enabled":     s.recorder != nil,
	})
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func clearSessionCookies(w http.ResponseWriter) {
	// Firefox rejects a non-Secure stevie_session while a Secure one exists — clear both.
	for _, secure := range []bool{true, false} {
		http.SetCookie(w, &http.Cookie{
			Name:     sessionCookie,
			Value:    "",
			Path:     "/",
			HttpOnly: true,
			Secure:   secure,
			SameSite: http.SameSiteLaxMode,
			MaxAge:   -1,
		})
	}
}

func setSessionCookie(w http.ResponseWriter, token string, exp time.Time, secure bool) {
	clearSessionCookies(w)
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		Expires:  exp,
	})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	u, err := s.store.UserByUsername(r.Context(), req.Username)
	if err != nil || !auth.CheckPassword(u.PasswordHash, req.Password) {
		writeErr(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	token, err := auth.NewSessionToken()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "session error")
		return
	}
	exp := time.Now().Add(s.cfg.SessionTTL)
	if err := s.store.CreateSession(r.Context(), u.ID, token, exp); err != nil {
		writeErr(w, http.StatusInternalServerError, "session error")
		return
	}
	// Prefer a non-Secure cookie so :9443 and :9080 share one session (needed for Live TV).
	setSessionCookie(w, token, exp, false)
	writeJSON(w, http.StatusOK, map[string]any{
		"user": map[string]any{"id": u.ID, "username": u.Username, "is_admin": u.IsAdmin},
	})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		_ = s.store.DeleteSession(r.Context(), c.Value)
	}
	clearSessionCookies(w)
	writeJSON(w, http.StatusOK, map[string]string{"status": "logged_out"})
}

// cookieSecure is false on the cleartext Live companion (:9080) so the session
// works there; HTTPS keeps Secure cookies when STEVIE_SECURE_COOKIES is on.
func (s *Server) cookieSecure(r *http.Request) bool {
	if !s.cfg.SecureCookies {
		return false
	}
	proto := r.Header.Get("X-Forwarded-Proto")
	if proto == "" {
		if r.TLS != nil {
			proto = "https"
		} else {
			proto = "http"
		}
	}
	return proto == "https"
}

// handlePrepareLiveSession replaces a Secure HTTPS cookie with a shared non-Secure
// cookie (must run on :9443). Firefox blocks setting non-Secure cookies over HTTP
// when a Secure cookie with the same name already exists.
func (s *Server) handlePrepareLiveSession(w http.ResponseWriter, r *http.Request) {
	u, err := s.userFromRequest(r)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	token, err := auth.NewSessionToken()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "session error")
		return
	}
	exp := time.Now().Add(s.cfg.SessionTTL)
	if err := s.store.CreateSession(r.Context(), u.ID, token, exp); err != nil {
		writeErr(w, http.StatusInternalServerError, "session error")
		return
	}
	setSessionCookie(w, token, exp, false)
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"user":   map[string]any{"id": u.ID, "username": u.Username, "is_admin": u.IsAdmin},
	})
}

type handoffRequest struct {
	Handoff string `json:"handoff"`
}

func (s *Server) handleCreateHandoff(w http.ResponseWriter, r *http.Request) {
	u, err := s.userFromRequest(r)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	handoff, exp, err := s.tokens.IssueHandoff(r.Context(), u.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "token error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"handoff":    handoff,
		"expires_at": exp.UTC().Format(time.RFC3339),
	})
}

func (s *Server) handleConsumeHandoff(w http.ResponseWriter, r *http.Request) {
	var req handoffRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Handoff == "" {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	st, err := s.tokens.Consume(r.Context(), req.Handoff)
	if err != nil || st.Kind != "handoff" || st.UserID == uuid.Nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	u, err := s.store.UserByID(r.Context(), st.UserID)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	token, err := auth.NewSessionToken()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "session error")
		return
	}
	exp := time.Now().Add(s.cfg.SessionTTL)
	if err := s.store.CreateSession(r.Context(), u.ID, token, exp); err != nil {
		writeErr(w, http.StatusInternalServerError, "session error")
		return
	}
	// Handoff responses on HTTP cannot clear a leftover Secure cookie — prefer
	// prepare-live on HTTPS. Still set non-Secure for clean HTTP-only logins.
	setSessionCookie(w, token, exp, false)
	writeJSON(w, http.StatusOK, map[string]any{
		"user": map[string]any{"id": u.ID, "username": u.Username, "is_admin": u.IsAdmin},
	})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	u, err := s.userFromRequest(r)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"user": map[string]any{"id": u.ID, "username": u.Username, "is_admin": u.IsAdmin},
	})
}

func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := s.userFromRequest(r); err != nil {
			writeErr(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) userFromRequest(r *http.Request) (store.User, error) {
	c, err := r.Cookie(sessionCookie)
	if err != nil || c.Value == "" {
		return store.User{}, store.ErrNotFound
	}
	return s.store.SessionUser(r.Context(), c.Value)
}

func (s *Server) handleListLibraries(w http.ResponseWriter, r *http.Request) {
	libs, err := s.store.ListLibraries(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"libraries": libs})
}

func (s *Server) handleScanLibrary(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	if s.scan.IsRunning(id) {
		writeErr(w, http.StatusConflict, "scan already running")
		return
	}
	// Detach from request cancellation so the scan outlives the HTTP call.
	go func() {
		_ = s.scan.ScanLibrary(context.Background(), id)
	}()
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "started"})
}

func (s *Server) handleScanProgress(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	p, err := s.scan.GetProgress(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) handleListMovies(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	movies, err := s.store.ListMovies(r.Context(), store.ListOpts{
		Sort:  r.URL.Query().Get("sort"),
		Limit: limit,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"movies": movies})
}

func (s *Server) handleGetMovie(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	movie, err := s.store.MovieByID(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	files, err := s.store.MediaFilesForMovie(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	for i := range files {
		files[i] = s.displayMedia(files[i])
	}
	selected := store.MediaFile{}
	if want := r.URL.Query().Get("file"); want != "" {
		if fid, err := uuid.Parse(want); err == nil {
			for _, f := range files {
				if f.ID == fid {
					selected = f
					break
				}
			}
		}
	}
	if selected.ID == uuid.Nil && len(files) > 0 {
		selected = files[0]
	}
	if selected.ID != uuid.Nil {
		movie.MediaFileID = &selected.ID
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"movie":       movie,
		"media":       selected,
		"media_files": files,
	})
}

func (s *Server) handleListShows(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	shows, err := s.store.ListShows(r.Context(), store.ListOpts{
		Sort:  r.URL.Query().Get("sort"),
		Limit: limit,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"shows": shows})
}

func (s *Server) handleGetShow(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	show, episodes, err := s.store.ShowByID(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"show": show, "episodes": episodes})
}

func (s *Server) handleGetEpisodeMedia(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	files, err := s.store.MediaFilesForEpisode(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	for i := range files {
		files[i] = s.displayMedia(files[i])
	}
	writeJSON(w, http.StatusOK, map[string]any{"media_files": files})
}

func (s *Server) handleGetMedia(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	file, err := s.store.MediaFileByID(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.displayMedia(file))
}

func (s *Server) displayMedia(f store.MediaFile) store.MediaFile {
	// Detect HDR/DV from stored probe JSON before rewriting the path for display.
	dr := ffprobe.DetectDynamicRange(f.ProbeJSON, f.Path)
	f.Path = s.cfg.DisplayPath(f.Path)
	f.DolbyVision = dr.DolbyVision
	f.HDR10 = dr.HDR10
	f.HDR10Plus = dr.HDR10Plus
	f.HLG = dr.HLG
	f.HDRLabels = dr.Labels()
	// Strip bulky probe payload from API responses once derived fields exist.
	f.ProbeJSON = nil
	return f
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	limit := 100
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			limit = n
		}
	}
	result, err := s.store.MediaSearch(r.Context(), q, limit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleTMDBSearch(w http.ResponseWriter, r *http.Request) {
	if s.tmdb == nil || !s.tmdb.Enabled() {
		writeErr(w, http.StatusBadRequest, "tmdb not configured")
		return
	}
	q := r.URL.Query().Get("q")
	kind := r.URL.Query().Get("type")
	if q == "" || (kind != "movie" && kind != "tv") {
		writeErr(w, http.StatusBadRequest, "q and type=movie|tv required")
		return
	}
	var results []tmdb.SearchResult
	var err error
	if kind == "movie" {
		results, err = s.tmdb.SearchMovie(r.Context(), q, 0)
	} else {
		results, err = s.tmdb.SearchTV(r.Context(), q, 0)
	}
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": results})
}

type matchRequest struct {
	Type          string `json:"type"`
	TMDBID        int    `json:"tmdb_id"`
	SeasonNumber  int    `json:"season_number"`
	EpisodeNumber int    `json:"episode_number"`
}

func (s *Server) handleMatch(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req matchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	switch req.Type {
	case "movie":
		if err := s.scan.MatchMovie(r.Context(), id, req.TMDBID); err != nil {
			writeErr(w, http.StatusBadGateway, err.Error())
			return
		}
	case "episode":
		if err := s.scan.MatchEpisode(r.Context(), id, req.TMDBID, req.SeasonNumber, req.EpisodeNumber); err != nil {
			writeErr(w, http.StatusBadGateway, err.Error())
			return
		}
	default:
		writeErr(w, http.StatusBadRequest, "type must be movie or episode")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "matched"})
}

func (s *Server) handleArtwork(w http.ResponseWriter, r *http.Request) {
	kind := chi.URLParam(r, "kind")
	path := r.URL.Query().Get("path")
	size := r.URL.Query().Get("size")
	if size == "" {
		size = "w780"
	}
	local, err := s.artwork.Ensure(r.Context(), kind, path, size)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	http.ServeFile(w, r, local)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
