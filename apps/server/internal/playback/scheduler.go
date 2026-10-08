package playback

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/stevie-media/stevie/apps/server/internal/store"
)

// ScheduleStarter starts/stops a live remux for a due schedule.
type ScheduleStarter interface {
	StartScheduled(ctx context.Context, sched store.ScheduledRecording) error
	StopScheduled(ctx context.Context, sched store.ScheduledRecording) error
}

// Scheduler polls for due / ending scheduled recordings.
type Scheduler struct {
	store   *store.Store
	starter ScheduleStarter
	every   time.Duration
	lead    time.Duration
}

func NewScheduler(st *store.Store, starter ScheduleStarter) *Scheduler {
	return &Scheduler{
		store:   st,
		starter: starter,
		every:   10 * time.Second,
		lead:    5 * time.Second,
	}
}

func (s *Scheduler) Run(ctx context.Context) {
	if s == nil || s.store == nil || s.starter == nil {
		return
	}
	if err := s.store.RequeueInterruptedSchedules(ctx); err != nil {
		slog.Warn("schedule restart recovery failed", "err", err)
	} else {
		slog.Info("schedule restart recovery checked")
	}
	t := time.NewTicker(s.every)
	defer t.Stop()
	s.tick(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.tick(ctx)
		}
	}
}

func (s *Scheduler) tick(ctx context.Context) {
	_ = s.store.ExpireMissedSchedules(ctx)

	due, err := s.store.DueScheduledRecordings(ctx, s.lead)
	if err != nil {
		slog.Warn("schedule due query failed", "err", err)
	} else {
		for _, job := range due {
			if err := s.store.UpdateScheduledStatus(ctx, job.ID, store.ScheduleStarting, ""); err != nil {
				continue
			}
			if err := s.starter.StartScheduled(ctx, job); err != nil {
				slog.Warn("scheduled recording start failed", "id", job.ID, "channel", job.ChannelName, "err", err)
				_ = s.store.UpdateScheduledStatus(ctx, job.ID, store.ScheduleFailed, err.Error())
				continue
			}
			_ = s.store.UpdateScheduledStatus(ctx, job.ID, store.ScheduleRecording, "")
			slog.Info("scheduled recording started", "id", job.ID, "channel", job.ChannelName, "title", job.Title)
		}
	}

	ending, err := s.store.ActiveScheduledRecordingsPastEnd(ctx)
	if err != nil {
		slog.Warn("schedule end query failed", "err", err)
		return
	}
	for _, job := range ending {
		if err := s.starter.StopScheduled(ctx, job); err != nil {
			slog.Warn("scheduled recording stop failed", "id", job.ID, "err", err)
		}
		_ = s.store.UpdateScheduledStatus(ctx, job.ID, store.ScheduleCompleted, "")
		slog.Info("scheduled recording completed", "id", job.ID, "channel", job.ChannelName)
	}
}

// ScheduleBridge wires the DB scheduler to the ffmpeg Recorder.
type ScheduleBridge struct {
	recorder *Recorder
	store    *store.Store
	baseURL  string // loopback HTTP base, e.g. http://127.0.0.1:8080

	mu        sync.Mutex
	byChannel map[uuid.UUID]uuid.UUID // channelID → scheduleID
}

func NewScheduleBridge(rec *Recorder, st *store.Store, loopbackBase string) *ScheduleBridge {
	return &ScheduleBridge{
		recorder:  rec,
		store:     st,
		baseURL:   strings.TrimRight(strings.TrimSpace(loopbackBase), "/"),
		byChannel: make(map[uuid.UUID]uuid.UUID),
	}
}

func (b *ScheduleBridge) StartScheduled(ctx context.Context, sched store.ScheduledRecording) error {
	if b == nil || b.recorder == nil {
		return fmt.Errorf("recorder unavailable")
	}
	ch, err := b.store.LiveChannelByID(ctx, sched.ChannelID)
	if err != nil {
		return err
	}
	if strings.TrimSpace(ch.StreamURL) == "" {
		return fmt.Errorf("channel has no stream URL")
	}
	internal := b.baseURL + "/api/live/channels/" + sched.ChannelID.String() + "/stream"
	title := strings.TrimSpace(sched.Title)
	if title == "" {
		title = ch.Name
	}
	// Prefer linked sports matchup over generic EPG/schedule titles.
	if matchup, err := b.store.SportsMatchupForChannel(ctx, sched.ChannelID, time.Now()); err == nil && matchup != "" {
		title = matchup
	}
	opt := StartOpts{
		ChannelID:    ch.ID,
		ChannelName:  ch.Name,
		StreamURL:    internal,
		ProgramTitle: title,
		Description:  sched.Description,
		Category:     sched.Category,
		Network:      ch.GroupTitle,
		Date:         sched.StartTime,
		WindowStart:  sched.StartTime,
		WindowEnd:    sched.EndTime,
	}
	if _, err := b.recorder.Start(opt); err != nil {
		return err
	}
	b.mu.Lock()
	b.byChannel[sched.ChannelID] = sched.ID
	b.mu.Unlock()
	return nil
}

func (b *ScheduleBridge) StopScheduled(_ context.Context, sched store.ScheduledRecording) error {
	if b == nil || b.recorder == nil {
		return fmt.Errorf("recorder unavailable")
	}
	_, err := b.recorder.Stop(sched.ChannelID)
	b.mu.Lock()
	delete(b.byChannel, sched.ChannelID)
	b.mu.Unlock()
	if err != nil && err.Error() == "not recording" {
		return nil
	}
	return err
}

// ScheduleIDForChannel returns the schedule owning an active channel recording, if any.
func (b *ScheduleBridge) ScheduleIDForChannel(channelID uuid.UUID) (uuid.UUID, bool) {
	if b == nil {
		return uuid.Nil, false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	id, ok := b.byChannel[channelID]
	return id, ok
}
