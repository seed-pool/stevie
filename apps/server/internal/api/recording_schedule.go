package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stevie-media/stevie/apps/server/internal/store"
)

type scheduleRequest struct {
	ChannelID   string `json:"channel_id"`
	ProgramID   string `json:"program_id,omitempty"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Category    string `json:"category"`
	StartTime   string `json:"start_time"`
	EndTime     string `json:"end_time"`
}

func (s *Server) handleCreateScheduledRecording(w http.ResponseWriter, r *http.Request) {
	if s.recorder == nil {
		writeErr(w, http.StatusServiceUnavailable, "recordings path not configured")
		return
	}
	var req scheduleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	channelID, err := uuid.Parse(strings.TrimSpace(req.ChannelID))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid channel_id")
		return
	}
	start, err := time.Parse(time.RFC3339, strings.TrimSpace(req.StartTime))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid start_time")
		return
	}
	end, err := time.Parse(time.RFC3339, strings.TrimSpace(req.EndTime))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid end_time")
		return
	}
	if !end.After(start) {
		writeErr(w, http.StatusBadRequest, "end_time must be after start_time")
		return
	}
	if !end.After(time.Now()) {
		writeErr(w, http.StatusBadRequest, "slot has already ended")
		return
	}
	ch, err := s.store.LiveChannelByID(r.Context(), channelID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "channel not found")
			return
		}
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	_ = ch

	in := store.CreateScheduledRecording{
		ChannelID:   channelID,
		Title:       strings.TrimSpace(req.Title),
		Description: strings.TrimSpace(req.Description),
		Category:    strings.TrimSpace(req.Category),
		StartTime:   start.UTC(),
		EndTime:     end.UTC(),
	}
	if in.Title == "" {
		in.Title = ch.Name
	}
	// If this channel is linked to a sports matchup around the slot, store the teams.
	if matchup, err := s.store.SportsMatchupForChannel(r.Context(), channelID, start); err == nil && matchup != "" {
		in.Title = matchup
	}
	if pid := strings.TrimSpace(req.ProgramID); pid != "" {
		id, err := uuid.Parse(pid)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "invalid program_id")
			return
		}
		in.ProgramID = &id
	}

	out, err := s.store.CreateScheduledRecording(r.Context(), in)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			writeErr(w, http.StatusConflict, "already scheduled")
			return
		}
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleListScheduledRecordings(w http.ResponseWriter, r *http.Request) {
	includePast := r.URL.Query().Get("include_past") == "1" || r.URL.Query().Get("include_past") == "true"
	items, err := s.store.ListScheduledRecordings(r.Context(), includePast)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if items == nil {
		items = []store.ScheduledRecording{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"scheduled": items})
}

func (s *Server) handleCancelScheduledRecording(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	job, err := s.store.ScheduledRecordingByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "not found")
			return
		}
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	switch job.Status {
	case store.ScheduleScheduled, store.ScheduleStarting:
		out, err := s.store.CancelScheduledRecording(r.Context(), id)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				writeErr(w, http.StatusConflict, "cannot cancel")
				return
			}
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, out)
	case store.ScheduleRecording:
		if s.recorder != nil {
			_, _ = s.recorder.Stop(job.ChannelID)
		}
		_ = s.store.UpdateScheduledStatus(r.Context(), id, store.ScheduleCancelled, "cancelled while recording")
		out, err := s.store.ScheduledRecordingByID(r.Context(), id)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, out)
	default:
		writeErr(w, http.StatusConflict, "schedule is not active")
	}
}
