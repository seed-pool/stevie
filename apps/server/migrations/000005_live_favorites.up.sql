-- Favorites keyed by stable (source, external_id) so they survive playlist/Xtream refresh.
CREATE TABLE live_favorites (
    source TEXT NOT NULL,
    external_id TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (source, external_id)
);

CREATE INDEX live_favorites_created_idx ON live_favorites (created_at DESC);
