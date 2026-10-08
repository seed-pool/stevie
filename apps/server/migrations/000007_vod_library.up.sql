CREATE TABLE vod_categories (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    kind TEXT NOT NULL CHECK (kind IN ('movie', 'series')),
    external_id TEXT NOT NULL,
    name TEXT NOT NULL,
    imported BOOLEAN NOT NULL DEFAULT false,
    title_count INT NOT NULL DEFAULT 0,
    sort_order INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (kind, external_id)
);

CREATE INDEX vod_categories_imported_idx ON vod_categories (kind, imported);

CREATE TABLE vod_movies (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    external_id TEXT NOT NULL UNIQUE,
    category_id UUID REFERENCES vod_categories(id) ON DELETE SET NULL,
    category_external_id TEXT NOT NULL DEFAULT '',
    name TEXT NOT NULL,
    plot TEXT NOT NULL DEFAULT '',
    poster_url TEXT NOT NULL DEFAULT '',
    backdrop_url TEXT NOT NULL DEFAULT '',
    tmdb_id TEXT NOT NULL DEFAULT '',
    rating DOUBLE PRECISION NOT NULL DEFAULT 0,
    year TEXT NOT NULL DEFAULT '',
    release_date TEXT NOT NULL DEFAULT '',
    genre TEXT NOT NULL DEFAULT '',
    director TEXT NOT NULL DEFAULT '',
    cast_text TEXT NOT NULL DEFAULT '',
    duration TEXT NOT NULL DEFAULT '',
    container TEXT NOT NULL DEFAULT '',
    trailer TEXT NOT NULL DEFAULT '',
    added_at TIMESTAMPTZ,
    sort_order INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX vod_movies_category_idx ON vod_movies (category_external_id);
CREATE INDEX vod_movies_name_idx ON vod_movies (name);
CREATE INDEX vod_movies_added_idx ON vod_movies (added_at DESC NULLS LAST);
CREATE INDEX vod_movies_tmdb_idx ON vod_movies (tmdb_id);

CREATE TABLE vod_series (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    external_id TEXT NOT NULL UNIQUE,
    category_id UUID REFERENCES vod_categories(id) ON DELETE SET NULL,
    category_external_id TEXT NOT NULL DEFAULT '',
    name TEXT NOT NULL,
    plot TEXT NOT NULL DEFAULT '',
    poster_url TEXT NOT NULL DEFAULT '',
    backdrop_url TEXT NOT NULL DEFAULT '',
    tmdb_id TEXT NOT NULL DEFAULT '',
    rating DOUBLE PRECISION NOT NULL DEFAULT 0,
    year TEXT NOT NULL DEFAULT '',
    release_date TEXT NOT NULL DEFAULT '',
    genre TEXT NOT NULL DEFAULT '',
    director TEXT NOT NULL DEFAULT '',
    cast_text TEXT NOT NULL DEFAULT '',
    episode_run_time TEXT NOT NULL DEFAULT '',
    trailer TEXT NOT NULL DEFAULT '',
    last_modified TIMESTAMPTZ,
    sort_order INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX vod_series_category_idx ON vod_series (category_external_id);
CREATE INDEX vod_series_name_idx ON vod_series (name);
CREATE INDEX vod_series_modified_idx ON vod_series (last_modified DESC NULLS LAST);
CREATE INDEX vod_series_tmdb_idx ON vod_series (tmdb_id);

INSERT INTO app_settings (key, value) VALUES
    ('vod_imported_movie_category_ids', '[]'),
    ('vod_imported_series_category_ids', '[]')
ON CONFLICT (key) DO NOTHING;
