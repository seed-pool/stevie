package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stevie-media/stevie/apps/server/internal/livetv"
	"github.com/stevie-media/stevie/apps/server/internal/playback"
	"github.com/stevie-media/stevie/apps/server/internal/store"
)

func (s *Server) handleVodCategories(w http.ResponseWriter, r *http.Request) {
	if s.vod == nil || !s.vod.Configured() {
		writeErr(w, http.StatusBadRequest, "xtream not configured — set XTREAM_URL, XTREAM_USER, XTREAM_PASSWORD")
		return
	}
	kind := strings.TrimSpace(r.URL.Query().Get("kind"))
	if kind != livetv.VodKindSeries {
		kind = livetv.VodKindMovie
	}
	cats, err := s.vod.ListCategories(r.Context(), kind)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	imported, _ := s.store.ImportedVodCategoryIDs(r.Context(), kind)
	if cats == nil {
		cats = []livetv.XtreamCategory{}
	}
	livetv.SortXtreamCategories(cats)
	writeJSON(w, http.StatusOK, map[string]any{
		"kind":       kind,
		"categories": cats,
		"imported":   imported,
	})
}

type vodSyncRequest struct {
	MovieCategoryIDs  []string `json:"movie_category_ids"`
	SeriesCategoryIDs []string `json:"series_category_ids"`
	SelectAllMovies   bool     `json:"select_all_movies"`
	SelectAllSeries   bool     `json:"select_all_series"`
}

func (s *Server) handleVodSyncStart(w http.ResponseWriter, r *http.Request) {
	if s.vod == nil || !s.vod.Configured() {
		writeErr(w, http.StatusBadRequest, "xtream not configured")
		return
	}
	var req vodSyncRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && r.ContentLength != 0 {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if !req.SelectAllMovies && !req.SelectAllSeries && len(req.MovieCategoryIDs) == 0 && len(req.SeriesCategoryIDs) == 0 {
		writeErr(w, http.StatusBadRequest, "select at least one category")
		return
	}
	if err := s.vod.StartSync(r.Context(), req.MovieCategoryIDs, req.SeriesCategoryIDs, req.SelectAllMovies, req.SelectAllSeries); err != nil {
		writeErr(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"status": "started"})
}

func (s *Server) handleVodSyncProgress(w http.ResponseWriter, r *http.Request) {
	if s.vod == nil {
		writeJSON(w, http.StatusOK, livetv.VodSyncProgress{Phase: "idle", Done: true})
		return
	}
	p, err := s.vod.GetProgress(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) handleListImportedVodCategories(w http.ResponseWriter, r *http.Request) {
	kind := strings.TrimSpace(r.URL.Query().Get("kind"))
	if kind != livetv.VodKindSeries {
		kind = livetv.VodKindMovie
	}
	cats, err := s.store.ListVodCategories(r.Context(), kind, true)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if cats == nil {
		cats = []store.VodCategory{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"kind": kind, "categories": cats})
}

func (s *Server) handleListVodMovies(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	movies, total, err := s.store.ListVodMovies(r.Context(), store.VodListOpts{
		Category: r.URL.Query().Get("category"),
		Q:        r.URL.Query().Get("q"),
		Sort:     r.URL.Query().Get("sort"),
		Limit:    limit,
		Offset:   offset,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if movies == nil {
		movies = []store.VodMovieRow{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"movies": movies, "total": total})
}

func (s *Server) handleListVodSeries(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	series, total, err := s.store.ListVodSeries(r.Context(), store.VodListOpts{
		Category: r.URL.Query().Get("category"),
		Q:        r.URL.Query().Get("q"),
		Sort:     r.URL.Query().Get("sort"),
		Limit:    limit,
		Offset:   offset,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if series == nil {
		series = []store.VodSeriesRow{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"series": series, "total": total})
}

func mapInfoString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			s := strings.TrimSpace(livetvAnyString(v))
			if s != "" {
				return s
			}
		}
	}
	return ""
}

func livetvAnyString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case float64:
		if t == float64(int64(t)) {
			return strconv.FormatInt(int64(t), 10)
		}
		return strconv.FormatFloat(t, 'f', -1, 64)
	default:
		return strings.TrimSpace(stringify(v))
	}
}

func stringify(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	s := string(b)
	if len(s) >= 2 && s[0] == '"' {
		var out string
		if json.Unmarshal(b, &out) == nil {
			return out
		}
	}
	return s
}

func (s *Server) handleGetVodMovie(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	movie, err := s.store.VodMovieByID(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	extra := map[string]string{}
	// Enrich from panel when possible.
	if s.vod != nil && s.vod.Configured() {
		if info, err := s.xtreamClient().VodInfo(r.Context(), movie.ExternalID); err == nil && info.Info != nil {
			plot := mapInfoString(info.Info, "plot", "description")
			backdrop := livetv.FirstBackdropPublic(info.Info["backdrop_path"])
			if backdrop == "" {
				backdrop = mapInfoString(info.Info, "movie_image", "cover_big", "cover")
			}
			genre := mapInfoString(info.Info, "genre")
			director := mapInfoString(info.Info, "director")
			cast := mapInfoString(info.Info, "cast")
			duration := mapInfoString(info.Info, "duration", "duration_secs")
			trailer := mapInfoString(info.Info, "youtube_trailer", "trailer")
			year := mapInfoString(info.Info, "year")
			release := mapInfoString(info.Info, "releasedate", "releaseDate", "release_date")
			tmdb := mapInfoString(info.Info, "tmdb_id", "tmdb")
			rating := 0.0
			if rs := mapInfoString(info.Info, "rating"); rs != "" {
				rating, _ = strconv.ParseFloat(rs, 64)
			}
			_ = s.store.UpdateVodMovieDetails(r.Context(), movie.ID, plot, backdrop, genre, director, cast, duration, trailer, year, release, rating, tmdb)

			container := movie.Container
			if info.MovieData != nil {
				if ext := mapInfoString(info.MovieData, "container_extension"); ext != "" {
					container = ext
				}
			}
			tech := livetv.ApplyVodTech(movie.Name, info.Info)
			_ = s.store.UpdateVodMovieTech(r.Context(), movie.ID, tech, container)

			if country := mapInfoString(info.Info, "country"); country != "" {
				extra["country"] = country
			}
			if age := mapInfoString(info.Info, "age"); age != "" {
				extra["age"] = age
			}

			if refreshed, err := s.store.VodMovieByID(r.Context(), id); err == nil {
				movie = refreshed
			}
			if movie.Container == "" {
				movie.Container = container
			}
		}
	}

	if u := livetv.FirstBackdropPublic(movie.BackdropURL); u != "" || strings.HasPrefix(strings.TrimSpace(movie.BackdropURL), "[") {
		movie.BackdropURL = u
	}
	store.EnrichVodMovieTech(&movie)

	versions, _ := s.store.ListVodMovieVersions(r.Context(), movie.TMDBID, movie.ID, 20)
	if versions == nil {
		versions = []store.VodMovieRow{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"movie":        movie,
		"duration_sec": parseDurationToSec(movie.Duration),
		"versions":     versions,
		"extra":        extra,
		"urls": map[string]string{
			"remux": "/api/vod/movies/" + movie.ID.String() + "/remux",
		},
	})
}

func (s *Server) handleGetVodSeries(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	series, err := s.store.VodSeriesByID(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	var episodes map[string][]map[string]any
	var seasons []map[string]any
	if s.vod != nil && s.vod.Configured() {
		if info, err := s.xtreamClient().SeriesInfo(r.Context(), series.ExternalID); err == nil {
			episodes = info.Episodes
			seasons = info.Seasons
			if info.Info != nil {
				plot := mapInfoString(info.Info, "plot", "description")
				backdrop := livetv.FirstBackdropPublic(info.Info["backdrop_path"])
				if backdrop == "" {
					backdrop = mapInfoString(info.Info, "cover", "cover_big", "movie_image")
				}
				genre := mapInfoString(info.Info, "genre")
				director := mapInfoString(info.Info, "director")
				cast := mapInfoString(info.Info, "cast")
				trailer := mapInfoString(info.Info, "youtube_trailer", "trailer")
				year := ""
				release := mapInfoString(info.Info, "releasedate", "releaseDate", "release_date")
				if len(release) >= 4 {
					year = release[:4]
				}
				tmdb := mapInfoString(info.Info, "tmdb_id", "tmdb")
				rating := 0.0
				if rs := mapInfoString(info.Info, "rating"); rs != "" {
					rating, _ = strconv.ParseFloat(rs, 64)
				}
				_ = s.store.UpdateVodSeriesDetails(r.Context(), series.ID, plot, backdrop, genre, director, cast, trailer, year, release, rating, tmdb)
				if refreshed, err := s.store.VodSeriesByID(r.Context(), id); err == nil {
					series = refreshed
				}
			}
		}
	}
	if episodes == nil {
		episodes = map[string][]map[string]any{}
	}
	if seasons == nil {
		seasons = []map[string]any{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"series":   series,
		"seasons":  seasons,
		"episodes": episodes,
	})
}

func (s *Server) xtreamClient() *livetv.XtreamClient {
	if s.vod != nil {
		return s.vod.Client()
	}
	return nil
}

func (s *Server) handleVodMovieRemux(w http.ResponseWriter, r *http.Request) {
	if _, err := s.userFromRequest(r); err != nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	movie, err := s.store.VodMovieByID(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	xc := s.xtreamClient()
	if xc == nil || !xc.Configured() {
		writeErr(w, http.StatusBadRequest, "xtream not configured")
		return
	}
	ext := movie.Container
	if ext == "" {
		ext = "mkv"
	}
	streamURL := xc.MovieStreamURL(movie.ExternalID, ext)
	start, _ := strconv.ParseFloat(r.URL.Query().Get("start"), 64)
	if start < 0 {
		start = 0
	}
	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Accept-Ranges", "none")
	if f, ok := w.(http.Flusher); ok {
		w.WriteHeader(http.StatusOK)
		f.Flush()
	} else {
		w.WriteHeader(http.StatusOK)
	}
	_ = playback.RunVodFMP4(r.Context(), streamURL, start, w)
}

func (s *Server) handleVodEpisodeRemux(w http.ResponseWriter, r *http.Request) {
	if _, err := s.userFromRequest(r); err != nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	epID := strings.TrimSpace(chi.URLParam(r, "epId"))
	if epID == "" {
		writeErr(w, http.StatusBadRequest, "missing episode id")
		return
	}
	series, err := s.store.VodSeriesByID(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	_ = series
	xc := s.xtreamClient()
	if xc == nil || !xc.Configured() {
		writeErr(w, http.StatusBadRequest, "xtream not configured")
		return
	}
	ext := strings.TrimSpace(r.URL.Query().Get("ext"))
	if ext == "" {
		ext = "mkv"
	}
	// Prefer extension from live series info when available.
	if info, err := xc.SeriesInfo(r.Context(), series.ExternalID); err == nil {
		for _, eps := range info.Episodes {
			for _, ep := range eps {
				if livetvAnyString(ep["id"]) == epID {
					if e := mapInfoString(ep, "container_extension"); e != "" {
						ext = e
					}
					break
				}
			}
		}
	}
	streamURL := xc.SeriesEpisodeURL(epID, ext)
	start, _ := strconv.ParseFloat(r.URL.Query().Get("start"), 64)
	if start < 0 {
		start = 0
	}
	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Accept-Ranges", "none")
	if f, ok := w.(http.Flusher); ok {
		w.WriteHeader(http.StatusOK)
		f.Flush()
	} else {
		w.WriteHeader(http.StatusOK)
	}
	_ = playback.RunVodFMP4(r.Context(), streamURL, start, w)
}

func (s *Server) handleVodMovieAnalyze(w http.ResponseWriter, r *http.Request) {
	if _, err := s.userFromRequest(r); err != nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	movie, err := s.store.VodMovieByID(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	xc := s.xtreamClient()
	if xc == nil || !xc.Configured() {
		writeErr(w, http.StatusBadRequest, "xtream not configured")
		return
	}
	ext := movie.Container
	if ext == "" {
		ext = "mkv"
	}
	streamURL := xc.MovieStreamURL(movie.ExternalID, ext)

	// Analyze can take a while (sample + MediaInfo).
	ctx := r.Context()
	result, err := playback.AnalyzeVodStream(ctx, streamURL, ext)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}

	// Prefer probe tech; keep name-derived source/HDR when probe didn't find them.
	fromName := livetv.ParseVodTech(movie.Name)
	tech := result.Tech
	if tech.Source == "" {
		tech.Source = fromName.Source
	}
	if tech.HDR == "" {
		tech.HDR = fromName.HDR
	}
	if tech.Resolution == "" {
		tech.Resolution = fromName.Resolution
	}
	if tech.VideoCodec == "" {
		tech.VideoCodec = fromName.VideoCodec
	}
	if tech.AudioCodec == "" {
		tech.AudioCodec = fromName.AudioCodec
	}
	container := result.Container
	if container == "" {
		container = ext
	}
	_ = s.store.UpdateVodMovieTech(ctx, movie.ID, tech, container)

	if refreshed, err := s.store.VodMovieByID(ctx, id); err == nil {
		movie = refreshed
	}
	store.EnrichVodMovieTech(&movie)

	streams := make([]map[string]any, 0, len(result.Streams))
	for _, st := range result.Streams {
		streams = append(streams, map[string]any{
			"index":          st.Index,
			"codec_type":     st.CodecType,
			"codec_name":     st.CodecName,
			"profile":        st.Profile,
			"width":          st.Width,
			"height":         st.Height,
			"pix_fmt":        st.PixFmt,
			"fps":            st.FPS,
			"bit_rate":       st.BitRate,
			"channels":       st.Channels,
			"channel_layout": st.ChannelLayout,
			"language":       st.Language,
			"title":          st.Title,
			"color_transfer": st.ColorTransfer,
			"color_space":    st.ColorSpace,
		})
	}

	resp := map[string]any{
		"movie":        movie,
		"tech":         tech,
		"sample_bytes": result.SampleBytes,
		"sample_sec":   result.SampleSec,
		"duration_ms":  result.DurationMS,
		"bitrate":      result.BitRate,
		"streams":      streams,
		"report_text":  result.ReportText,
	}
	if len(result.ReportJSON) > 0 {
		resp["report_json"] = json.RawMessage(result.ReportJSON)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleVodMovieDownload(w http.ResponseWriter, r *http.Request) {
	if _, err := s.userFromRequest(r); err != nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if s.vodDL == nil {
		writeErr(w, http.StatusBadRequest, "downloads not configured — set STEVIE_RECORDINGS_PATH")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	movie, err := s.store.VodMovieByID(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	xc := s.xtreamClient()
	if xc == nil || !xc.Configured() {
		writeErr(w, http.StatusBadRequest, "xtream not configured")
		return
	}
	ext := movie.Container
	if ext == "" {
		ext = "mkv"
	}
	title := movie.Name
	if y := strings.TrimSpace(movie.Year); y != "" && !strings.Contains(title, y) {
		title = title + " " + y
	}
	meta, err := s.vodDL.Start(playback.VodDownloadOpts{
		Title:     title,
		Kind:      "movie",
		StreamURL: xc.MovieStreamURL(movie.ExternalID, ext),
		Container: ext,
	})
	if err != nil {
		writeErr(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"download": meta})
}

func (s *Server) handleVodEpisodeDownload(w http.ResponseWriter, r *http.Request) {
	if _, err := s.userFromRequest(r); err != nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if s.vodDL == nil {
		writeErr(w, http.StatusBadRequest, "downloads not configured — set STEVIE_RECORDINGS_PATH")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	epID := strings.TrimSpace(chi.URLParam(r, "epId"))
	if epID == "" {
		writeErr(w, http.StatusBadRequest, "missing episode id")
		return
	}
	series, err := s.store.VodSeriesByID(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	xc := s.xtreamClient()
	if xc == nil || !xc.Configured() {
		writeErr(w, http.StatusBadRequest, "xtream not configured")
		return
	}
	ext := strings.TrimSpace(r.URL.Query().Get("ext"))
	if ext == "" {
		ext = "mkv"
	}
	epTitle := ""
	season, epNum := 0, 0
	if info, err := xc.SeriesInfo(r.Context(), series.ExternalID); err == nil {
		for seasonKey, eps := range info.Episodes {
			for _, ep := range eps {
				if livetvAnyString(ep["id"]) != epID {
					continue
				}
				if e := mapInfoString(ep, "container_extension"); e != "" {
					ext = e
				}
				epTitle = mapInfoString(ep, "title", "name")
				epNum = anyIntFromMap(ep, "episode_num")
				season, _ = strconv.Atoi(seasonKey)
				if season == 0 {
					season = anyIntFromMap(ep, "season")
				}
			}
		}
	}
	code := ""
	if season > 0 || epNum > 0 {
		code = fmt.Sprintf("S%02dE%02d", season, epNum)
	}
	title := series.Name
	if code != "" {
		title = title + " " + code
	}
	if epTitle != "" {
		title = title + " " + epTitle
	}
	meta, err := s.vodDL.Start(playback.VodDownloadOpts{
		Title:     title,
		Kind:      "episode",
		StreamURL: xc.SeriesEpisodeURL(epID, ext),
		Container: ext,
	})
	if err != nil {
		writeErr(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"download": meta})
}

func (s *Server) handleVodDownloads(w http.ResponseWriter, r *http.Request) {
	if s.vodDL == nil {
		writeJSON(w, http.StatusOK, map[string]any{"downloads": []any{}})
		return
	}
	list := s.vodDL.List()
	if list == nil {
		list = []playback.VodDownloadMeta{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"downloads": list})
}

func (s *Server) handleVodDownloadCancel(w http.ResponseWriter, r *http.Request) {
	if s.vodDL == nil {
		writeErr(w, http.StatusBadRequest, "downloads not configured")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := s.vodDL.Cancel(id); err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "cancelled"})
}

func anyIntFromMap(m map[string]any, key string) int {
	s := mapInfoString(m, key)
	if s == "" {
		return 0
	}
	n, _ := strconv.Atoi(s)
	return n
}

// parseDurationToSec accepts Xtream-style values: seconds, HH:MM:SS, MM:SS, or "2h 11m".
func parseDurationToSec(v string) float64 {
	raw := strings.TrimSpace(v)
	if raw == "" {
		return 0
	}
	if n, err := strconv.ParseFloat(raw, 64); err == nil && n > 0 {
		return n
	}
	parts := strings.Split(raw, ":")
	if len(parts) == 3 {
		h, e1 := strconv.Atoi(parts[0])
		m, e2 := strconv.Atoi(parts[1])
		s, e3 := strconv.ParseFloat(parts[2], 64)
		if e1 == nil && e2 == nil && e3 == nil {
			return float64(h*3600+m*60) + s
		}
	}
	if len(parts) == 2 {
		m, e1 := strconv.Atoi(parts[0])
		s, e2 := strconv.ParseFloat(parts[1], 64)
		if e1 == nil && e2 == nil {
			return float64(m*60) + s
		}
	}
	var sec float64
	lower := strings.ToLower(raw)
	if i := strings.IndexAny(lower, "hms"); i >= 0 {
		// crude token scan: Nh Nm Ns
		var num strings.Builder
		for _, r := range lower + " " {
			if r >= '0' && r <= '9' {
				num.WriteRune(r)
				continue
			}
			if num.Len() == 0 {
				continue
			}
			n, _ := strconv.ParseFloat(num.String(), 64)
			num.Reset()
			switch r {
			case 'h':
				sec += n * 3600
			case 'm':
				sec += n * 60
			case 's':
				sec += n
			}
		}
	}
	if sec > 0 {
		return sec
	}
	return 0
}
