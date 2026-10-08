CREATE TABLE IF NOT EXISTS sports_teams (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    external_id TEXT NOT NULL UNIQUE,
    sport TEXT NOT NULL DEFAULT '',
    league TEXT NOT NULL DEFAULT '',
    name TEXT NOT NULL DEFAULT '',
    name_alternates TEXT NOT NULL DEFAULT '',
    badge_url TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS sports_teams_name_idx ON sports_teams (lower(name));
CREATE INDEX IF NOT EXISTS sports_teams_sport_idx ON sports_teams (sport);

CREATE TABLE IF NOT EXISTS sports_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    external_id TEXT NOT NULL UNIQUE,
    sport TEXT NOT NULL DEFAULT '',
    title TEXT NOT NULL DEFAULT '',
    home_team TEXT NOT NULL DEFAULT '',
    away_team TEXT NOT NULL DEFAULT '',
    home_team_id UUID REFERENCES sports_teams (id) ON DELETE SET NULL,
    away_team_id UUID REFERENCES sports_teams (id) ON DELETE SET NULL,
    starts_at TIMESTAMPTZ NOT NULL,
    ends_at TIMESTAMPTZ,
    source TEXT NOT NULL DEFAULT 'thesportsdb',
    home_score TEXT NOT NULL DEFAULT '',
    away_score TEXT NOT NULL DEFAULT '',
    period TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT '',
    score_updated_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS sports_events_starts_idx ON sports_events (starts_at);
CREATE INDEX IF NOT EXISTS sports_events_sport_starts_idx ON sports_events (sport, starts_at);

CREATE TABLE IF NOT EXISTS sports_event_channels (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_id UUID NOT NULL REFERENCES sports_events (id) ON DELETE CASCADE,
    broadcast_label TEXT NOT NULL DEFAULT '',
    live_channel_id UUID REFERENCES live_channels (id) ON DELETE SET NULL,
    match_score REAL NOT NULL DEFAULT 0,
    UNIQUE (event_id, broadcast_label)
);

CREATE INDEX IF NOT EXISTS sports_event_channels_event_idx ON sports_event_channels (event_id);
CREATE INDEX IF NOT EXISTS sports_event_channels_live_idx ON sports_event_channels (live_channel_id);

CREATE TABLE IF NOT EXISTS sports_channel_aliases (
    broadcast_label TEXT PRIMARY KEY,
    live_channel_id UUID NOT NULL REFERENCES live_channels (id) ON DELETE CASCADE,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
