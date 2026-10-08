CREATE TABLE IF NOT EXISTS live_scheduled_recordings (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    channel_id UUID NOT NULL REFERENCES live_channels (id) ON DELETE CASCADE,
    program_id UUID,
    title TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    category TEXT NOT NULL DEFAULT '',
    start_time TIMESTAMPTZ NOT NULL,
    end_time TIMESTAMPTZ NOT NULL,
    status TEXT NOT NULL DEFAULT 'scheduled',
    error TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT live_scheduled_recordings_window CHECK (end_time > start_time)
);

CREATE INDEX IF NOT EXISTS live_scheduled_recordings_due_idx
    ON live_scheduled_recordings (start_time)
    WHERE status = 'scheduled';

CREATE INDEX IF NOT EXISTS live_scheduled_recordings_active_idx
    ON live_scheduled_recordings (end_time)
    WHERE status = 'recording';

CREATE UNIQUE INDEX IF NOT EXISTS live_scheduled_recordings_unique_slot
    ON live_scheduled_recordings (channel_id, start_time, end_time)
    WHERE status IN ('scheduled', 'recording', 'starting');
