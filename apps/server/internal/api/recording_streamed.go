package api

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/stevie-media/stevie/apps/server/internal/playback"
)

// Stable namespace so the same streamed.pk source/id/stream maps to one recorder key.
var streamedRecordingNS = uuid.MustParse("6ba7b810-9dad-11d1-80b4-00c04fd430c8")

func streamedRecordingID(source, id, stream string) uuid.UUID {
	key := strings.TrimSpace(source) + "|" + strings.TrimSpace(id) + "|" + strings.TrimSpace(stream)
	return uuid.NewSHA1(streamedRecordingNS, []byte(key))
}

func parseStreamedRecordingParams(r *http.Request) (source, id, stream string, ok bool) {
	source = strings.TrimSpace(r.URL.Query().Get("source"))
	id = strings.TrimSpace(r.URL.Query().Get("id"))
	stream = strings.TrimSpace(r.URL.Query().Get("stream"))
	if stream == "" {
		stream = "1"
	}
	ok = source != "" && id != ""
	return source, id, stream, ok
}

func (s *Server) handleStartStreamedRecording(w http.ResponseWriter, r *http.Request) {
	if s.recorder == nil {
		writeErr(w, http.StatusServiceUnavailable, "recordings path not configured")
		return
	}
	source, id, stream, ok := parseStreamedRecordingParams(r)
	if !ok {
		writeErr(w, http.StatusBadRequest, "source and id required")
		return
	}
	// Warm unlock so ffmpeg does not race the first playlist fetch.
	if _, err := s.resolveStreamed(source, id, stream); err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}

	var req struct {
		ProgramTitle string `json:"program_title"`
		ChannelName  string `json:"channel_name"`
	}
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}

	recID := streamedRecordingID(source, id, stream)
	internal := loopbackHTTPBase(s.cfg.HTTPAddr) + "/api/sports/streamed/playlist?" + url.Values{
		"source": {source},
		"id":     {id},
		"stream": {stream},
	}.Encode()

	name := strings.TrimSpace(req.ChannelName)
	if name == "" {
		name = "streamed.pk · " + source
	}
	opt := playback.StartOpts{
		ChannelID:    recID,
		ChannelName:  name,
		StreamURL:    internal,
		Network:      "streamed.pk",
		Category:     "Sports",
		Date:         time.Now(),
		WindowStart:  time.Now(),
		ProgramTitle: strings.TrimSpace(req.ProgramTitle),
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

func (s *Server) handleStopStreamedRecording(w http.ResponseWriter, r *http.Request) {
	if s.recorder == nil {
		writeErr(w, http.StatusServiceUnavailable, "recordings path not configured")
		return
	}
	source, id, stream, ok := parseStreamedRecordingParams(r)
	if !ok {
		writeErr(w, http.StatusBadRequest, "source and id required")
		return
	}
	meta, err := s.recorder.Stop(streamedRecordingID(source, id, stream))
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.recordingResponse(meta))
}

func (s *Server) handleStreamedRecordingStatus(w http.ResponseWriter, r *http.Request) {
	if s.recorder == nil {
		writeJSON(w, http.StatusOK, map[string]any{"recording": false})
		return
	}
	source, id, stream, ok := parseStreamedRecordingParams(r)
	if !ok {
		writeErr(w, http.StatusBadRequest, "source and id required")
		return
	}
	meta, active := s.recorder.Status(streamedRecordingID(source, id, stream))
	if !active {
		writeJSON(w, http.StatusOK, map[string]any{"recording": false})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"recording": true,
		"job":       s.recordingResponse(meta),
	})
}
