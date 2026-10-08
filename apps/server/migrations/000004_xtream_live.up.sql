CREATE TABLE live_categories (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    source TEXT NOT NULL,
    external_id TEXT NOT NULL,
    name TEXT NOT NULL,
    imported BOOLEAN NOT NULL DEFAULT false,
    channel_count INT NOT NULL DEFAULT 0,
    sort_order INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (source, external_id)
);

CREATE INDEX live_categories_imported_idx ON live_categories (source, imported);

ALTER TABLE live_channels
    ADD COLUMN source TEXT NOT NULL DEFAULT 'm3u',
    ADD COLUMN external_id TEXT NOT NULL DEFAULT '',
    ADD COLUMN category_id UUID REFERENCES live_categories(id) ON DELETE SET NULL,
    ADD COLUMN epg_channel_id TEXT NOT NULL DEFAULT '',
    ADD COLUMN num INT NOT NULL DEFAULT 0;

-- Stable keys for existing M3U rows (UUID string is unique enough).
UPDATE live_channels
SET external_id = id::text
WHERE external_id = '';

CREATE UNIQUE INDEX live_channels_source_external_uidx ON live_channels (source, external_id);
CREATE INDEX live_channels_category_idx ON live_channels (category_id);
CREATE INDEX live_channels_source_idx ON live_channels (source);
CREATE INDEX live_channels_epg_idx ON live_channels (epg_channel_id);

CREATE INDEX IF NOT EXISTS live_programs_tvg_start_idx ON live_programs (channel_tvg_id, start_time);

INSERT INTO app_settings (key, value) VALUES
    ('live_imported_category_ids', '[]')
ON CONFLICT (key) DO NOTHING;
