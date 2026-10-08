package api

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stevie-media/stevie/apps/server/internal/playback"
	"github.com/stevie-media/stevie/apps/server/internal/store"
)

func (s *Server) handleGetPlayback(w http.ResponseWriter, r *http.Request) {
	file, err := s.mediaFileParam(r)
	if err != nil {
		writeMediaErr(w, err)
		return
	}
	caps := playback.CapsFromQuery(queryMap(r))
	preferAudio, _ := strconv.Atoi(r.URL.Query().Get("audio"))
	if r.URL.Query().Get("audio") == "" {
		preferAudio = -1
	}
	d := playback.Decide(file, caps, preferAudio)
	writeJSON(w, http.StatusOK, map[string]any{
		"media_id": file.ID,
		"decision": d,
		"urls": map[string]string{
			"stream":   fmt.Sprintf("/api/media/%s/stream", file.ID),
			"remux":    fmt.Sprintf("/api/media/%s/remux", file.ID),
			"external": fmt.Sprintf("/api/media/%s/external.m3u", file.ID),
		},
	})
}

func (s *Server) handleCreatePlaybackToken(w http.ResponseWriter, r *http.Request) {
	u, err := s.userFromRequest(r)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	file, err := s.mediaFileParam(r)
	if err != nil {
		writeMediaErr(w, err)
		return
	}
	token, exp, err := s.tokens.Issue(r.Context(), u.ID, file.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "token error")
		return
	}
	base := s.publicBase(r)
	writeJSON(w, http.StatusOK, map[string]any{
		"token":      token,
		"expires_at": exp.UTC().Format(time.RFC3339),
		"stream_url": fmt.Sprintf("%s/api/media/%s/stream?token=%s", base, file.ID, token),
		"remux_url":  fmt.Sprintf("%s/api/media/%s/remux?token=%s", base, file.ID, token),
		"external_playlist_url": fmt.Sprintf("%s/api/media/%s/external.m3u?token=%s", base, file.ID, token),
	})
}

func (s *Server) handleStreamMedia(w http.ResponseWriter, r *http.Request) {
	file, err := s.authorizeMediaAccess(r)
	if err != nil {
		writeMediaErr(w, err)
		return
	}
	if err := ensureReadable(file.Path); err != nil {
		writeErr(w, http.StatusNotFound, "media file missing on disk")
		return
	}
	f, err := os.Open(file.Path)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", mimeForContainer(strPtr(file.Container), file.Path))
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Cache-Control", "no-store")
	http.ServeContent(w, r, filepath.Base(file.Path), stat.ModTime(), f)
}

func (s *Server) handleRemuxMedia(w http.ResponseWriter, r *http.Request) {
	file, err := s.authorizeMediaAccess(r)
	if err != nil {
		writeMediaErr(w, err)
		return
	}
	if err := ensureReadable(file.Path); err != nil {
		writeErr(w, http.StatusNotFound, "media file missing on disk")
		return
	}
	caps := playback.CapsFromQuery(queryMap(r))
	preferAudio, _ := strconv.Atoi(r.URL.Query().Get("audio"))
	if r.URL.Query().Get("audio") == "" {
		preferAudio = -1
	}
	d := playback.Decide(file, caps, preferAudio)
	if d.Mode == playback.ModeUnsupported {
		writeErr(w, http.StatusUnsupportedMediaType, d.Reason)
		return
	}
	start, _ := strconv.ParseFloat(r.URL.Query().Get("start"), 64)
	if start < 0 {
		start = 0
	}

	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Accept-Ranges", "none")
	// Flush headers before the long remux body so clients see the response start.
	if f, ok := w.(http.Flusher); ok {
		w.WriteHeader(http.StatusOK)
		f.Flush()
	} else {
		w.WriteHeader(http.StatusOK)
	}

	_ = playback.RunRemuxFMP4(r.Context(), playback.RemuxOptions{
		InputPath:      file.Path,
		AudioIndex:     d.SelectedAudio,
		StartSec:       start,
		TranscodeAudio: d.TranscodeAudio,
	}, w)
}

func (s *Server) handleSubtitleVTT(w http.ResponseWriter, r *http.Request) {
	file, err := s.authorizeMediaAccess(r)
	if err != nil {
		writeMediaErr(w, err)
		return
	}
	idx, err := strconv.Atoi(chi.URLParam(r, "index"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid subtitle index")
		return
	}
	var track *store.MediaStream
	for i := range file.Streams {
		st := &file.Streams[i]
		if st.StreamIndex == idx && st.CodecType == "subtitle" {
			track = st
			break
		}
	}
	if track == nil {
		writeErr(w, http.StatusNotFound, "subtitle not found")
		return
	}
	codec := ""
	if track.CodecName != nil {
		codec = strings.ToLower(*track.CodecName)
	}
	d := playback.Decide(file, playback.ClientCaps{}, -1)
	ok := false
	for _, t := range d.SubtitleTracks {
		if t.Index == idx && t.TextBased {
			ok = true
			break
		}
	}
	if !ok {
		writeErr(w, http.StatusUnsupportedMediaType, "subtitle codec not text-based: "+codec)
		return
	}
	start, _ := strconv.ParseFloat(r.URL.Query().Get("start"), 64)
	if start < 0 {
		start = 0
	}
	var buf strings.Builder
	if err := playback.ExtractWebVTT(r.Context(), file.Path, idx, start, &buf); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/vtt; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = io.WriteString(w, buf.String())
}

func (s *Server) handleExternalPlaylist(w http.ResponseWriter, r *http.Request) {
	file, err := s.authorizeMediaAccess(r)
	if err != nil {
		writeMediaErr(w, err)
		return
	}
	token := r.URL.Query().Get("token")
	if token == "" {
		// Issue is awkward here without user; require token for external playlists.
		writeErr(w, http.StatusUnauthorized, "token required")
		return
	}
	title := filepath.Base(file.Path)
	url := fmt.Sprintf("%s/api/media/%s/stream?token=%s", s.publicBase(r), file.ID, token)
	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"stevie-%s.m3u\"", file.ID.String()[:8]))
	w.Header().Set("Cache-Control", "no-store")
	_, _ = fmt.Fprintf(w, "#EXTM3U\n#EXTINF:-1,%s\n%s\n", title, url)
}

func (s *Server) mediaFileParam(r *http.Request) (store.MediaFile, error) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		return store.MediaFile{}, errBadID
	}
	file, err := s.store.MediaFileByID(r.Context(), id)
	if err != nil {
		return store.MediaFile{}, err
	}
	return file, nil
}

func (s *Server) authorizeMediaAccess(r *http.Request) (store.MediaFile, error) {
	file, err := s.mediaFileParam(r)
	if err != nil {
		return store.MediaFile{}, err
	}
	if _, err := s.userFromRequest(r); err == nil {
		return file, nil
	}
	token := r.URL.Query().Get("token")
	if token == "" {
		return store.MediaFile{}, errUnauthorized
	}
	st, err := s.tokens.Get(r.Context(), token)
	if err != nil || st.MediaID != file.ID {
		return store.MediaFile{}, errUnauthorized
	}
	return file, nil
}

func (s *Server) publicBase(r *http.Request) string {
	scheme := "https"
	if r.TLS == nil && r.Header.Get("X-Forwarded-Proto") != "https" {
		if r.Header.Get("X-Forwarded-Proto") == "http" {
			scheme = "http"
		}
	}
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		scheme = proto
	}
	host := r.Header.Get("X-Forwarded-Host")
	if host == "" {
		host = r.Host
	}
	if host == "" {
		if s.cfg.IsLocal() {
			host = fmt.Sprintf("localhost:%s", s.cfg.StevieListenPort)
			scheme = "http"
		} else {
			host = fmt.Sprintf("%s:%s", s.cfg.StevieDomain, s.cfg.StevieHTTPSPort)
		}
	}
	return scheme + "://" + host
}

func queryMap(r *http.Request) map[string]string {
	out := map[string]string{}
	for k, vals := range r.URL.Query() {
		if len(vals) > 0 {
			out[k] = vals[0]
		}
	}
	return out
}

func ensureReadable(path string) error {
	st, err := os.Stat(path)
	if err != nil {
		return err
	}
	if st.IsDir() {
		return fmt.Errorf("path is directory")
	}
	return nil
}

func mimeForContainer(container, path string) string {
	c := strings.ToLower(container + " " + filepath.Ext(path))
	switch {
	case strings.Contains(c, "webm"):
		return "video/webm"
	case strings.Contains(c, "mp4"), strings.Contains(c, "m4v"), strings.Contains(c, "mov"):
		return "video/mp4"
	case strings.Contains(c, "mkv"), strings.Contains(c, "matroska"):
		return "video/x-matroska"
	default:
		return "application/octet-stream"
	}
}

func strPtr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

var (
	errBadID        = errors.New("invalid id")
	errUnauthorized = errors.New("unauthorized")
)

func writeMediaErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errBadID):
		writeErr(w, http.StatusBadRequest, "invalid id")
	case errors.Is(err, store.ErrNotFound):
		writeErr(w, http.StatusNotFound, "not found")
	case errors.Is(err, errUnauthorized):
		writeErr(w, http.StatusUnauthorized, "unauthorized")
	default:
		writeErr(w, http.StatusInternalServerError, err.Error())
	}
}
