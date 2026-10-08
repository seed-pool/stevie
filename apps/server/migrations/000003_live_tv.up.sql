CREATE TABLE app_settings (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE live_channels (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tvg_id TEXT NOT NULL DEFAULT '',
    name TEXT NOT NULL,
    group_title TEXT NOT NULL DEFAULT '',
    logo_url TEXT NOT NULL DEFAULT '',
    stream_url TEXT NOT NULL,
    sort_order INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX live_channels_tvg_id_idx ON live_channels (tvg_id);
CREATE INDEX live_channels_group_idx ON live_channels (group_title);
CREATE INDEX live_channels_sort_idx ON live_channels (sort_order, name);

CREATE TABLE live_programs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    channel_tvg_id TEXT NOT NULL,
    title TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    start_time TIMESTAMPTZ NOT NULL,
    end_time TIMESTAMPTZ NOT NULL,
    category TEXT NOT NULL DEFAULT ''
);

CREATE INDEX live_programs_channel_time_idx ON live_programs (channel_tvg_id, start_time, end_time);
CREATE INDEX live_programs_window_idx ON live_programs (start_time, end_time);

INSERT INTO app_settings (key, value) VALUES
    ('live_m3u_source', ''),
    ('live_xmltv_source', '');
