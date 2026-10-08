package store

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const (
	ScheduleScheduled = "scheduled"
	ScheduleStarting  = "starting"
	ScheduleRecording = "recording"
	ScheduleCompleted = "completed"
	ScheduleCancelled = "cancelled"
	ScheduleFailed    = "failed"
)

type ScheduledRecording struct {
	ID          uuid.UUID  `json:"id"`
	ChannelID   uuid.UUID  `json:"channel_id"`
	ChannelName string     `json:"channel_name"`
	LogoURL     string     `json:"logo_url,omitempty"`
	ProgramID   *uuid.UUID `json:"program_id,omitempty"`
	Title       string     `json:"title"`
	Description string     `json:"description"`
	Category    string     `json:"category"`
	StartTime   time.Time  `json:"start_time"`
	EndTime     time.Time  `json:"end_time"`
	Status      string     `json:"status"`
	Error       string     `json:"error,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

type CreateScheduledRecording struct {
	ChannelID   uuid.UUID
	ProgramID   *uuid.UUID
	Title       string
	Description string
	Category    string
	StartTime   time.Time
	EndTime     time.Time
}

type scheduledScanner interface {
	Scan(dest ...any) error
}

func scanScheduled(row scheduledScanner) (ScheduledRecording, error) {
	var s ScheduledRecording
	err := row.Scan(
		&s.ID, &s.ChannelID, &s.ChannelName, &s.LogoURL, &s.ProgramID,
		&s.Title, &s.Description, &s.Category,
		&s.StartTime, &s.EndTime, &s.Status, &s.Error,
		&s.CreatedAt, &s.UpdatedAt,
	)
	return s, err
}

const scheduledSelect = `
	SELECT s.id, s.channel_id, c.name, COALESCE(c.logo_url, ''), s.program_id,
	       s.title, s.description, s.category,
	       s.start_time, s.end_time, s.status, s.error,
	       s.created_at, s.updated_at
	FROM live_scheduled_recordings s
	JOIN live_channels c ON c.id = s.channel_id
`

func (s *Store) CreateScheduledRecording(ctx context.Context, in CreateScheduledRecording) (ScheduledRecording, error) {
	row := s.pool.QueryRow(ctx, `
		INSERT INTO live_scheduled_recordings (
			channel_id, program_id, title, description, category, start_time, end_time, status
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id
	`, in.ChannelID, in.ProgramID, in.Title, in.Description, in.Category, in.StartTime, in.EndTime, ScheduleScheduled)
	var id uuid.UUID
	if err := row.Scan(&id); err != nil {
		return ScheduledRecording{}, err
	}
	return s.ScheduledRecordingByID(ctx, id)
}

func (s *Store) ScheduledRecordingByID(ctx context.Context, id uuid.UUID) (ScheduledRecording, error) {
	row := s.pool.QueryRow(ctx, scheduledSelect+` WHERE s.id = $1`, id)
	out, err := scanScheduled(row)
	if err == pgx.ErrNoRows {
		return ScheduledRecording{}, ErrNotFound
	}
	return out, err
}

func (s *Store) ListScheduledRecordings(ctx context.Context, includePast bool) ([]ScheduledRecording, error) {
	q := scheduledSelect + ` WHERE s.status IN ('scheduled', 'starting', 'recording')`
	if includePast {
		q = scheduledSelect + ` WHERE s.status IN ('scheduled', 'starting', 'recording', 'completed', 'failed', 'cancelled')
			AND s.start_time > now() - interval '7 days'`
	}
	q += ` ORDER BY s.start_time ASC`
	rows, err := s.pool.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ScheduledRecording
	for rows.Next() {
		item, err := scanScheduled(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) CancelScheduledRecording(ctx context.Context, id uuid.UUID) (ScheduledRecording, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE live_scheduled_recordings
		SET status = $2, updated_at = now()
		WHERE id = $1 AND status IN ('scheduled', 'starting')
	`, id, ScheduleCancelled)
	if err != nil {
		return ScheduledRecording{}, err
	}
	if tag.RowsAffected() == 0 {
		// Allow cancel of recording status from UI? Prefer stop via active recording.
		return ScheduledRecording{}, ErrNotFound
	}
	return s.ScheduledRecordingByID(ctx, id)
}

func (s *Store) UpdateScheduledStatus(ctx context.Context, id uuid.UUID, status, errMsg string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE live_scheduled_recordings
		SET status = $2, error = $3, updated_at = now()
		WHERE id = $1
	`, id, status, errMsg)
	return err
}

// CompleteActiveSchedulesForChannel marks in-progress schedules for a channel as completed
// (e.g. when the user stops the recording early).
func (s *Store) CompleteActiveSchedulesForChannel(ctx context.Context, channelID uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE live_scheduled_recordings
		SET status = $2, updated_at = now()
		WHERE channel_id = $1 AND status IN ('starting', 'recording')
	`, channelID, ScheduleCompleted)
	return err
}

// DueScheduledRecordings returns schedules that should start now (within a small lead window).
func (s *Store) DueScheduledRecordings(ctx context.Context, lead time.Duration) ([]ScheduledRecording, error) {
	now := time.Now()
	rows, err := s.pool.Query(ctx, scheduledSelect+`
		WHERE s.status = 'scheduled'
		  AND s.start_time <= $1
		  AND s.end_time > $2
		ORDER BY s.start_time ASC
		LIMIT 20
	`, now.Add(lead), now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ScheduledRecording
	for rows.Next() {
		item, err := scanScheduled(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// ActiveScheduledRecordingsPastEnd returns in-progress schedules past their end time.
func (s *Store) ActiveScheduledRecordingsPastEnd(ctx context.Context) ([]ScheduledRecording, error) {
	rows, err := s.pool.Query(ctx, scheduledSelect+`
		WHERE s.status = 'recording'
		  AND s.end_time <= now()
		ORDER BY s.end_time ASC
		LIMIT 20
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ScheduledRecording
	for rows.Next() {
		item, err := scanScheduled(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// ExpireMissedSchedules fails schedules that can never complete usefully.
func (s *Store) ExpireMissedSchedules(ctx context.Context) error {
	if _, err := s.pool.Exec(ctx, `
		UPDATE live_scheduled_recordings
		SET status = 'failed', error = 'missed start window', updated_at = now()
		WHERE status = 'scheduled'
		  AND end_time <= now()
	`); err != nil {
		return err
	}
	// starting stuck (crash mid-start, or start never progressed).
	_, err := s.pool.Exec(ctx, `
		UPDATE live_scheduled_recordings
		SET status = 'failed', error = 'stuck starting', updated_at = now()
		WHERE status = 'starting'
		  AND (end_time <= now() OR updated_at < now() - interval '2 minutes')
	`)
	return err
}

// RequeueInterruptedSchedules recovers jobs left in starting/recording after a process restart.
// Still-airing windows become scheduled again so the poller can resume; past windows fail.
func (s *Store) RequeueInterruptedSchedules(ctx context.Context) error {
	if _, err := s.pool.Exec(ctx, `
		UPDATE live_scheduled_recordings
		SET status = 'scheduled', error = '', updated_at = now()
		WHERE status IN ('starting', 'recording')
		  AND end_time > now()
	`); err != nil {
		return err
	}
	_, err := s.pool.Exec(ctx, `
		UPDATE live_scheduled_recordings
		SET status = 'failed', error = 'interrupted by server restart', updated_at = now()
		WHERE status IN ('starting', 'recording')
		  AND end_time <= now()
	`)
	return err
}
