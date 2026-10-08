package api

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stevie-media/stevie/apps/server/internal/playback"
	"github.com/stevie-media/stevie/apps/server/internal/store"
)

// authorizeLiveMedia allows session cookies or loopback (in-container ffmpeg).
func (s *Server) authorizeLiveMedia(r *http.Request) error {
	if isLoopbackAddr(r.RemoteAddr) {
		return nil
	}
	_, err := s.userFromRequest(r)
	return err
}

func isLoopbackAddr(remote string) bool {
	host := remote
	if h, _, err := net.SplitHostPort(remote); err == nil {
		host = h
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func loopbackHTTPBase(httpAddr string) string {
	addr := strings.TrimSpace(httpAddr)
	if addr == "" {
		addr = ":8080"
	}
	if strings.HasPrefix(addr, ":") {
		return "http://127.0.0.1" + addr
	}
	if strings.HasPrefix(addr, "127.0.0.1") || strings.HasPrefix(addr, "localhost") {
		return "http://" + addr
	}
	// Bind address like 0.0.0.0:8080 → still dial loopback.
	if _, port, err := net.SplitHostPort(addr); err == nil && port != "" {
		return "http://127.0.0.1:" + port
	}
	return "http://127.0.0.1:8080"
}

func (s *Server) recordingResponse(meta playback.RecordingMeta) map[string]any {
	out := map[string]any{
		"channel_id":    meta.ChannelID,
		"channel_name":  meta.ChannelName,
		"file_name":     meta.FileName,
		"path":          meta.FileName, // never expose host filesystem paths to the browser
		"started_at":    meta.StartedAt.UTC(),
		"status":        meta.Status,
		"program_title": meta.ProgramTitle,
	}
	if meta.Error != "" {
		out["error"] = meta.Error
	}
	return out
}

func (s *Server) handleStartLiveRecording(w http.ResponseWriter, r *http.Request) {
	if s.recorder == nil {
		writeErr(w, http.StatusServiceUnavailable, "recordings path not configured")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	ch, err := s.store.LiveChannelByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "channel not found")
			return
		}
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if strings.TrimSpace(ch.StreamURL) == "" {
		writeErr(w, http.StatusBadRequest, "channel has no stream URL")
		return
	}
	// Record via the local HLS proxy (same rewrite/CDN path as the browser player).
	internal := loopbackHTTPBase(s.cfg.HTTPAddr) + "/api/live/channels/" + id.String() + "/stream"

	var req struct {
		ProgramTitle string `json:"program_title"`
	}
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}

	opt := playback.StartOpts{
		ChannelID:   ch.ID,
		ChannelName: ch.Name,
		StreamURL:   internal,
		Network:     ch.GroupTitle,
		Date:        time.Now(),
		WindowStart: time.Now(),
	}
	if prog, err := s.store.CurrentLiveProgram(r.Context(), ch); err == nil && prog != nil {
		opt.ProgramTitle = prog.Title
		opt.Description = prog.Description
		opt.Category = prog.Category
		if !prog.StartTime.IsZero() {
			opt.Date = prog.StartTime
		}
		// Provisional name uses the programme window; final name uses actual start/end.
		if !prog.StartTime.IsZero() {
			opt.WindowStart = prog.StartTime
		}
		if !prog.EndTime.IsZero() {
			opt.WindowEnd = prog.EndTime
		}
	}
	// Prefer sports matchup (Away @ Home) over generic EPG titles like "Live MLB Baseball".
	if matchup, err := s.store.SportsMatchupForChannel(r.Context(), ch.ID, time.Now()); err == nil && matchup != "" {
		opt.ProgramTitle = matchup
	} else if t := strings.TrimSpace(req.ProgramTitle); t != "" {
		opt.ProgramTitle = t
	}

	meta, err := s.recorder.Start(opt)
	if err != nil {
		if meta.Status == "recording" {
			writeErr(w, http.StatusConflict, err.Error())
			return
		}
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.recordingResponse(meta))
}

func (s *Server) handleStopLiveRecording(w http.ResponseWriter, r *http.Request) {
	if s.recorder == nil {
		writeErr(w, http.StatusServiceUnavailable, "recordings path not configured")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	meta, err := s.recorder.Stop(id)
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	_ = s.store.CompleteActiveSchedulesForChannel(r.Context(), id)
	writeJSON(w, http.StatusOK, s.recordingResponse(meta))
}

func (s *Server) handleLiveRecordingStatus(w http.ResponseWriter, r *http.Request) {
	if s.recorder == nil {
		writeJSON(w, http.StatusOK, map[string]any{"recording": false})
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	meta, ok := s.recorder.Status(id)
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{"recording": false})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"recording": true,
		"job":       s.recordingResponse(meta),
	})
}

