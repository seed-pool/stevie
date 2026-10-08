package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stevie-media/stevie/apps/server/internal/livetv"
	"github.com/stevie-media/stevie/apps/server/internal/store"
)

type liveSettingsRequest struct {
	M3USource   string `json:"m3u_source"`
	XMLTVSource string `json:"xmltv_source"`
	Refresh     bool   `json:"refresh"`
}

// CDN hosts learned while rewriting HLS playlists (channel stream host often redirects).
var liveRelayHosts sync.Map // channelID string -> map[string]struct{}

func hostOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Host)
}

func rememberLiveRelayHost(channelID uuid.UUID, host string) {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return
	}
	key := channelID.String()
	actual, _ := liveRelayHosts.LoadOrStore(key, &sync.Map{})
	actual.(*sync.Map).Store(host, struct{}{})
}

func liveRelayHostAllowed(channelID uuid.UUID, host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return false
	}
	v, ok := liveRelayHosts.Load(channelID.String())
	if !ok {
		return false
	}
	_, allowed := v.(*sync.Map).Load(host)
	return allowed
}

func (s *Server) handleGetLiveSettings(w http.ResponseWriter, r *http.Request) {
	m3u, xmltv, err := s.store.LiveSettings(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	chCount, _ := s.store.CountLiveChannels(r.Context())
	progCount, _ := s.store.CountLivePrograms(r.Context())
	imported, _ := s.store.ImportedXtreamCategoryIDs(r.Context())
	xtreamCh, _ := s.store.CountXtreamChannels(r.Context())
	var syncProg any
	var epgProg any
	if s.xtream != nil {
		if p, err := s.xtream.GetProgress(r.Context()); err == nil {
			syncProg = p
		}
		if p, err := s.xtream.GetEPGProgress(r.Context()); err == nil {
			epgProg = p
		}
	}
	movieCats, _ := s.store.ImportedVodCategoryIDs(r.Context(), livetv.VodKindMovie)
	seriesCats, _ := s.store.ImportedVodCategoryIDs(r.Context(), livetv.VodKindSeries)
	vodMovies, _ := s.store.VodMovieCount(r.Context())
	vodSeries, _ := s.store.VodSeriesCount(r.Context())
	var vodSync any
	if s.vod != nil {
		if p, err := s.vod.GetProgress(r.Context()); err == nil {
			vodSync = p
		}
	}
	// Never expose host filesystem paths to the browser (Firefox treats /home/... in
	// JSON/UI as file:// and logs Security Error on HTTPS pages).
	m3uOut := m3u
	if resolved := s.cfg.ResolveLiveSource(m3u); resolved != "" {
		m3uOut = resolved
	}
	xmlOut := xmltv
	if resolved := s.cfg.ResolveLiveSource(xmltv); resolved != "" {
		xmlOut = resolved
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"m3u_source":                 m3uOut,
		"xmltv_source":               xmlOut,
		"channel_count":              chCount,
		"program_count":              progCount,
		"live_mount_path":            s.cfg.LiveSourcesMount,
		// Intentionally omit live_host_path — clients only need the container mount.
		"xtream_configured":          s.xtream != nil && s.xtream.Configured(),
		"xtream_imported_categories": imported,
		"xtream_channel_count":       xtreamCh,
		"xtream_sync":                syncProg,
		"epg_sync":                   epgProg,
		"vod_movie_categories":       movieCats,
		"vod_series_categories":      seriesCats,
		"vod_movie_count":            vodMovies,
		"vod_series_count":           vodSeries,
		"vod_sync":                   vodSync,
	})
}

func (s *Server) handlePutLiveSettings(w http.ResponseWriter, r *http.Request) {
	var req liveSettingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if err := s.store.SetSetting(r.Context(), store.SettingLiveM3U, s.cfg.ResolveLiveSource(strings.TrimSpace(req.M3USource))); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.store.SetSetting(r.Context(), store.SettingLiveXMLTV, s.cfg.ResolveLiveSource(strings.TrimSpace(req.XMLTVSource))); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	result := map[string]any{
		"m3u_source":   s.cfg.ResolveLiveSource(strings.TrimSpace(req.M3USource)),
		"xmltv_source": s.cfg.ResolveLiveSource(strings.TrimSpace(req.XMLTVSource)),
	}
	if req.Refresh {
		ref, err := s.live.RefreshAll(r.Context())
		if err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		result["channels"] = ref.Channels
		result["programs"] = ref.Programs
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleRefreshLive(w http.ResponseWriter, r *http.Request) {
	// Detach so long downloads aren't killed by request timeout middleware... 
	// This handler is outside the 60s group for the same reason? Actually it's inside.
	// Use background context with a longer deadline.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	ref, err := s.live.RefreshAll(ctx)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, ref)
}

// handleRefreshEPG starts an async provider XMLTV refresh (no M3U / VOD).
func (s *Server) handleRefreshEPG(w http.ResponseWriter, r *http.Request) {
	if s.xtream == nil {
		writeErr(w, http.StatusServiceUnavailable, "epg sync unavailable")
		return
	}
	if err := s.xtream.StartEPGRefresh(r.Context()); err != nil {
		writeErr(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"status": "started"})
}

func (s *Server) handleEPGRefreshProgress(w http.ResponseWriter, r *http.Request) {
	if s.xtream == nil {
		writeJSON(w, http.StatusOK, livetv.EPGProgress{Phase: "idle", Done: true})
		return
	}
	p, err := s.xtream.GetEPGProgress(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) handleListLiveChannels(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	group := r.URL.Query().Get("group")
	q := r.URL.Query().Get("q")
	// Require group or search so we never dump the full catalog to the browser.
	if group == "" && q == "" {
		if limit <= 0 || limit > 300 {
			limit = 300
		}
	}
	channels, err := s.store.ListLiveChannels(r.Context(), store.LiveListOpts{
		Group:  group,
		Query:  q,
		Limit:  limit,
		Source: r.URL.Query().Get("source"),
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if channels == nil {
		channels = []store.LiveChannel{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"channels": channels})
}

func (s *Server) handleLiveGroups(w http.ResponseWriter, r *http.Request) {
	groups, err := s.store.LiveGroups(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if groups == nil {
		groups = []string{}
	}
	cats, _ := s.store.ListLiveCategories(r.Context(), true)
	if cats == nil {
		cats = []store.LiveCategory{}
	}
	favCount, _ := s.store.CountLiveFavorites(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{
		"groups":           groups,
		"categories":       cats,
		"favorites_count":  favCount,
		"favorites_group":  store.FavoritesGroup,
	})
}

func (s *Server) handleLiveCategories(w http.ResponseWriter, r *http.Request) {
	cats, err := s.store.ListLiveCategories(r.Context(), true)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if cats == nil {
		cats = []store.LiveCategory{}
	}
	favCount, _ := s.store.CountLiveFavorites(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{
		"categories":      cats,
		"favorites_count": favCount,
		"favorites_group": store.FavoritesGroup,
	})
}

func (s *Server) handleLiveGuide(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	from := now.Add(-30 * time.Minute)
	to := now.Add(6 * time.Hour)
	if v := r.URL.Query().Get("from"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			from = t
		}
	}
	if v := r.URL.Query().Get("to"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			to = t
		}
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if offset < 0 {
		offset = 0
	}
	group := r.URL.Query().Get("group")
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	pq := strings.TrimSpace(r.URL.Query().Get("pq"))
	cats, _ := s.store.ListLiveCategories(r.Context(), true)
	if cats == nil {
		cats = []store.LiveCategory{}
	}
	groups, _ := s.store.LiveGroups(r.Context())
	if groups == nil {
		groups = []string{}
	}
	favCount, _ := s.store.CountLiveFavorites(r.Context())

	listOpts := store.LiveListOpts{
		Group:         group,
		Query:         q,
		ProgramQuery:  pq,
		Limit:         limit,
		Offset:        offset,
		From:          from,
		To:            to,
		FavoritesOnly: group == store.FavoritesGroup,
	}
	total, _ := s.store.CountLiveChannelsOpts(r.Context(), listOpts)
	guide, err := s.store.LiveGuideOpts(r.Context(), from, to, listOpts)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if guide == nil {
		guide = []store.LiveChannelGuide{}
	}
	for i := range guide {
		guide[i].LogoURL = livetv.SanitizeLogoURL(guide[i].LogoURL)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"from":            from.UTC(),
		"to":              to.UTC(),
		"groups":          groups,
		"categories":      cats,
		"group":           group,
		"favorites_count": favCount,
		"favorites_group": store.FavoritesGroup,
		"channels":        guide,
		"total":           total,
		"limit":           limit,
		"offset":          offset,
	})
}

func (s *Server) handleLiveFavorite(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req struct {
		Favorite *bool `json:"favorite"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Favorite == nil {
		writeErr(w, http.StatusBadRequest, "expected {\"favorite\": true|false}")
		return
	}
	ch, err := s.store.SetLiveFavorite(r.Context(), id, *req.Favorite)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "channel not found")
			return
		}
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	favCount, _ := s.store.CountLiveFavorites(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{
		"channel":         ch,
		"favorites_count": favCount,
	})
}

func (s *Server) handleXtreamCategories(w http.ResponseWriter, r *http.Request) {
	if s.xtream == nil || !s.xtream.Configured() {
		writeErr(w, http.StatusBadRequest, "xtream not configured — set XTREAM_URL, XTREAM_USER, XTREAM_PASSWORD")
		return
	}
	cats, err := s.xtream.ListCategories(r.Context())
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	imported, _ := s.store.ImportedXtreamCategoryIDs(r.Context())
	if cats == nil {
		cats = []livetv.XtreamCategory{}
	}
	livetv.SortXtreamCategories(cats)
	writeJSON(w, http.StatusOK, map[string]any{
		"categories": cats,
		"imported":   imported,
	})
}

type xtreamSyncRequest struct {
	CategoryIDs []string `json:"category_ids"`
	SelectAll   bool     `json:"select_all"`
}

func (s *Server) handleXtreamSyncStart(w http.ResponseWriter, r *http.Request) {
	if s.xtream == nil || !s.xtream.Configured() {
		writeErr(w, http.StatusBadRequest, "xtream not configured")
		return
	}
	var req xtreamSyncRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && r.ContentLength != 0 {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if !req.SelectAll && len(req.CategoryIDs) == 0 {
		writeErr(w, http.StatusBadRequest, "select at least one category or select_all")
		return
	}
	if err := s.xtream.StartSync(r.Context(), req.CategoryIDs, req.SelectAll); err != nil {
		writeErr(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"status": "started"})
}

func (s *Server) handleXtreamSyncProgress(w http.ResponseWriter, r *http.Request) {
	if s.xtream == nil {
		writeJSON(w, http.StatusOK, livetv.SyncProgress{Phase: "idle", Done: true})
		return
	}
	p, err := s.xtream.GetProgress(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) handleLivePlayToken(w http.ResponseWriter, r *http.Request) {
	u, err := s.userFromRequest(r)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	ch, err := s.store.LiveChannelByID(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	token, exp, err := s.tokens.IssueKind(r.Context(), u.ID, ch.ID, "live")
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "token error")
		return
	}
	handoff, _, err := s.tokens.IssueHandoff(r.Context(), u.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "token error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"token":       token,
		"handoff":     handoff,
		"expires_at":  exp.UTC().Format(time.RFC3339),
		"cleartext_path": fmt.Sprintf("/live?autoplay=%s&token=%s&handoff=%s", ch.ID, token, handoff),
		"external":    fmt.Sprintf("/api/live/channels/%s/external.m3u", ch.ID),
	})
}

func (s *Server) handleLivePlayInfo(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	token := r.URL.Query().Get("token")
	if token == "" {
		if _, err := s.userFromRequest(r); err != nil {
			writeErr(w, http.StatusUnauthorized, "unauthorized")
			return
		}
	} else {
		st, err := s.tokens.Get(r.Context(), token)
		if err != nil || st.MediaID != id || (st.Kind != "" && st.Kind != "live") {
			writeErr(w, http.StatusUnauthorized, "unauthorized")
			return
		}
	}
	ch, err := s.store.LiveChannelByID(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":       ch.ID,
		"name":     ch.Name,
		"logo_url": livetv.SanitizeLogoURL(ch.LogoURL),
	})
}

func (s *Server) handleGetLiveChannel(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	ch, err := s.store.LiveChannelByID(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"channel": map[string]any{
			"id":          ch.ID,
			"tvg_id":      ch.TVGID,
			"name":        ch.Name,
			"group_title": ch.GroupTitle,
			"logo_url":    livetv.SanitizeLogoURL(ch.LogoURL),
			"sort_order":  ch.SortOrder,
		},
		"urls": map[string]string{
			"proxy":    "/api/live/channels/" + ch.ID.String() + "/stream",
			"external": "/api/live/channels/" + ch.ID.String() + "/external.m3u",
		},
	})
}

func (s *Server) handleLiveExternalPlaylist(w http.ResponseWriter, r *http.Request) {
	if _, err := s.userFromRequest(r); err != nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	ch, err := s.store.LiveChannelByID(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if ch.StreamURL == "" {
		writeErr(w, http.StatusBadGateway, "channel has no stream url")
		return
	}
	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.Header().Set("Content-Disposition", `attachment; filename="stevie-live.m3u"`)
	w.Header().Set("Cache-Control", "no-store")
	_, _ = io.WriteString(w, "#EXTM3U\n#EXTINF:-1,"+ch.Name+"\n"+ch.StreamURL+"\n")
}

func writeLiveLogoPlaceholder(w http.ResponseWriter) {
	// 404 so <img onError> can fall back to initials — a 200 transparent PNG
	// was getting cached and showing as empty badges (e.g. OKC).
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNotFound)
}

func (s *Server) handleLiveLogo(w http.ResponseWriter, r *http.Request) {
	raw := strings.TrimSpace(r.URL.Query().Get("url"))
	if raw == "" {
		writeLiveLogoPlaceholder(w)
		return
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		writeLiveLogoPlaceholder(w)
		return
	}
	// Keep this short — slow/dead logo hosts were blocking the UI for ~15s.
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		writeLiveLogoPlaceholder(w)
		return
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; StevieSports/1.0)")
	req.Header.Set("Accept", "image/avif,image/webp,image/apng,image/*,*/*;q=0.8")
	client := &http.Client{
		Timeout: 5 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("too many redirects")
			}
			if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
				return errors.New("refusing non-http logo redirect")
			}
			return nil
		},
	}
	res, err := client.Do(req)
	if err != nil {
		writeLiveLogoPlaceholder(w)
		return
	}
	defer res.Body.Close()
	if res.StatusCode >= 400 {
		writeLiveLogoPlaceholder(w)
		return
	}
	ct := res.Header.Get("Content-Type")
	if ct == "" || !strings.HasPrefix(strings.ToLower(ct), "image/") {
		ct = "image/jpeg"
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, io.LimitReader(res.Body, 5<<20))
}

func liveIPTVClient() *http.Client {
	return &http.Client{
		Timeout: 0,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return errors.New("too many redirects")
			}
			return nil
		},
	}
}

func preferLiveHLSURL(raw string) string {
	u := strings.TrimSpace(raw)
	if u == "" {
		return u
	}
	if strings.HasSuffix(strings.ToLower(u), ".ts") {
		return u[:len(u)-3] + ".m3u8"
	}
	return u
}

func isHLSPlaylist(contentType, streamURL string, body []byte) bool {
	ct := strings.ToLower(contentType)
	if strings.Contains(ct, "mpegurl") || strings.Contains(ct, "m3u8") {
		return true
	}
	if strings.Contains(strings.ToLower(streamURL), ".m3u8") {
		return true
	}
	trim := strings.TrimSpace(string(body))
	return strings.HasPrefix(trim, "#EXTM3U")
}

func rewriteLiveM3U8(playlist []byte, playlistURL string, channelID uuid.UUID) []byte {
	base, err := url.Parse(playlistURL)
	if err != nil {
		return playlist
	}
	relayPrefix := "/api/live/channels/" + channelID.String() + "/relay?u="
	lines := strings.Split(string(playlist), "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			out = append(out, line)
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			out = append(out, rewriteM3U8TagURIs(line, base, relayPrefix))
			continue
		}
		ref, err := url.Parse(trimmed)
		if err != nil {
			out = append(out, line)
			continue
		}
		abs := base.ResolveReference(ref).String()
		out = append(out, relayPrefix+url.QueryEscape(abs))
	}
	return []byte(strings.Join(out, "\n"))
}

func rewriteM3U8TagURIs(line string, base *url.URL, relayPrefix string) string {
	// Rewrite URI="..." on #EXT-X-KEY / #EXT-X-MAP / #EXT-X-MEDIA / #EXT-X-I-FRAME-STREAM-INF
	const marker = `URI="`
	idx := strings.Index(line, marker)
	if idx < 0 {
		return line
	}
	start := idx + len(marker)
	end := strings.Index(line[start:], `"`)
	if end < 0 {
		return line
	}
	raw := line[start : start+end]
	ref, err := url.Parse(raw)
	if err != nil {
		return line
	}
	abs := base.ResolveReference(ref).String()
	return line[:start] + relayPrefix + url.QueryEscape(abs) + line[start+end:]
}

func (s *Server) proxyLiveURL(w http.ResponseWriter, r *http.Request, channelID uuid.UUID, upstream string, rewritePlaylist bool) {
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, upstream, nil)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	req.Header.Set("User-Agent", "VLC/3.0.20 LibVLC/3.0.20")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Connection", "keep-alive")

	res, err := liveIPTVClient().Do(req)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "upstream connect failed: "+err.Error())
		return
	}
	defer res.Body.Close()

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 4096))
		msg := "IPTV provider rejected the stream (HTTP " + strconv.Itoa(res.StatusCode) + ")"
		switch res.StatusCode {
		case 401, 403:
			msg += ". Check playlist credentials or IP lock."
		case 404:
			msg += ". Channel may be offline or removed."
		case 456:
			msg += ". Usually max connections, expired subscription, or this server IP is not allowed."
		case 429, 503:
			msg += ". Provider is rate-limiting or overloaded — try again shortly."
		}
		writeErr(w, http.StatusBadGateway, msg)
		return
	}

	ct := res.Header.Get("Content-Type")
	if rewritePlaylist {
		body, err := io.ReadAll(io.LimitReader(res.Body, 4<<20))
		if err != nil {
			writeErr(w, http.StatusBadGateway, "failed reading playlist")
			return
		}
		if isHLSPlaylist(ct, upstream, body) {
			// Resolve relative segment URLs against the final URL after CDN redirects.
			baseURL := upstream
			if res.Request != nil && res.Request.URL != nil {
				baseURL = res.Request.URL.String()
			}
			if finalHost := hostOf(baseURL); finalHost != "" {
				rememberLiveRelayHost(channelID, finalHost)
			}
			if originHost := hostOf(upstream); originHost != "" {
				rememberLiveRelayHost(channelID, originHost)
			}
			rewritten := rewriteLiveM3U8(body, baseURL, channelID)
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(rewritten)
			return
		}
		// Not a playlist — stream the buffered body then continue (shouldn't happen often).
		if ct != "" {
			w.Header().Set("Content-Type", ct)
		} else {
			w.Header().Set("Content-Type", "video/mp2t")
		}
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
		return
	}

	if ct != "" {
		w.Header().Set("Content-Type", ct)
	} else if strings.Contains(strings.ToLower(upstream), ".m3u8") {
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	} else {
		w.Header().Set("Content-Type", "video/mp2t")
	}
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, res.Body)
}

func (s *Server) handleLiveStream(w http.ResponseWriter, r *http.Request) {
	if err := s.authorizeLiveMedia(r); err != nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	ch, err := s.store.LiveChannelByID(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if ch.StreamURL == "" {
		writeErr(w, http.StatusBadGateway, "channel has no stream url")
		return
	}
	upstream := preferLiveHLSURL(ch.StreamURL)
	s.proxyLiveURL(w, r, id, upstream, true)
}

func (s *Server) handleLiveRelay(w http.ResponseWriter, r *http.Request) {
	if err := s.authorizeLiveMedia(r); err != nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	ch, err := s.store.LiveChannelByID(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	raw := strings.TrimSpace(r.URL.Query().Get("u"))
	if raw == "" {
		writeErr(w, http.StatusBadRequest, "u required")
		return
	}
	target, err := url.Parse(raw)
	if err != nil || (target.Scheme != "http" && target.Scheme != "https") || target.Host == "" {
		writeErr(w, http.StatusBadRequest, "invalid url")
		return
	}
	base, err := url.Parse(preferLiveHLSURL(ch.StreamURL))
	if err != nil || base.Host == "" {
		writeErr(w, http.StatusBadGateway, "invalid channel stream url")
		return
	}
	rememberLiveRelayHost(id, base.Host)
	// Allow the channel host plus CDN hosts learned from playlist redirects.
	if !liveRelayHostAllowed(id, target.Host) && !strings.EqualFold(target.Host, base.Host) {
		writeErr(w, http.StatusForbidden, "relay host mismatch")
		return
	}
	rewrite := strings.Contains(strings.ToLower(target.Path), ".m3u8")
	s.proxyLiveURL(w, r, id, target.String(), rewrite)
}
