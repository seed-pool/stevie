CREATE EXTENSION IF NOT EXISTS "pgcrypto";

CREATE TABLE users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    username TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    is_admin BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE sessions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash TEXT NOT NULL UNIQUE,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX sessions_user_id_idx ON sessions(user_id);
CREATE INDEX sessions_expires_at_idx ON sessions(expires_at);

CREATE TYPE library_type AS ENUM ('movie', 'tv');

CREATE TABLE libraries (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL,
    root_path TEXT NOT NULL UNIQUE,
    type library_type NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE media_files (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    library_id UUID NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
    path TEXT NOT NULL,
    size_bytes BIGINT NOT NULL DEFAULT 0,
    mtime TIMESTAMPTZ,
    inode BIGINT,
    container TEXT,
    duration_ms BIGINT,
    bitrate BIGINT,
    format_name TEXT,
    format_tags JSONB NOT NULL DEFAULT '{}'::jsonb,
    probe_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    parsed_title TEXT,
    parsed_year INT,
    parsed_season INT,
    parsed_episode INT,
    soft_deleted_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (library_id, path)
);

CREATE INDEX media_files_library_id_idx ON media_files(library_id);
CREATE INDEX media_files_soft_deleted_at_idx ON media_files(soft_deleted_at);

CREATE TABLE media_streams (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    media_file_id UUID NOT NULL REFERENCES media_files(id) ON DELETE CASCADE,
    stream_index INT NOT NULL,
    codec_type TEXT NOT NULL,
    codec_name TEXT,
    profile TEXT,
    width INT,
    height INT,
    pix_fmt TEXT,
    fps TEXT,
    bit_rate BIGINT,
    channels INT,
    channel_layout TEXT,
    language TEXT,
    title TEXT,
    disposition_default BOOLEAN NOT NULL DEFAULT FALSE,
    disposition_forced BOOLEAN NOT NULL DEFAULT FALSE,
    disposition_hearing_impaired BOOLEAN NOT NULL DEFAULT FALSE,
    color_range TEXT,
    color_space TEXT,
    color_transfer TEXT,
    bit_depth INT,
    raw JSONB NOT NULL DEFAULT '{}'::jsonb,
    UNIQUE (media_file_id, stream_index)
);

CREATE INDEX media_streams_media_file_id_idx ON media_streams(media_file_id);

CREATE TABLE movies (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tmdb_id INT NOT NULL UNIQUE,
    title TEXT NOT NULL,
    original_title TEXT,
    tagline TEXT,
    overview TEXT,
    release_date DATE,
    runtime_minutes INT,
    vote_average DOUBLE PRECISION,
    vote_count INT,
    popularity DOUBLE PRECISION,
    poster_path TEXT,
    backdrop_path TEXT,
    genres JSONB NOT NULL DEFAULT '[]'::jsonb,
    cast_crew JSONB NOT NULL DEFAULT '[]'::jsonb,
    external_ids JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE shows (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tmdb_id INT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    original_name TEXT,
    tagline TEXT,
    overview TEXT,
    first_air_date DATE,
    vote_average DOUBLE PRECISION,
    vote_count INT,
    popularity DOUBLE PRECISION,
    poster_path TEXT,
    backdrop_path TEXT,
    genres JSONB NOT NULL DEFAULT '[]'::jsonb,
    cast_crew JSONB NOT NULL DEFAULT '[]'::jsonb,
    external_ids JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE seasons (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    show_id UUID NOT NULL REFERENCES shows(id) ON DELETE CASCADE,
    tmdb_id INT,
    season_number INT NOT NULL,
    name TEXT,
    overview TEXT,
    poster_path TEXT,
    air_date DATE,
    UNIQUE (show_id, season_number)
);

CREATE TABLE episodes (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    show_id UUID NOT NULL REFERENCES shows(id) ON DELETE CASCADE,
    season_id UUID REFERENCES seasons(id) ON DELETE SET NULL,
    tmdb_id INT,
    season_number INT NOT NULL,
    episode_number INT NOT NULL,
    name TEXT,
    overview TEXT,
    still_path TEXT,
    air_date DATE,
    runtime_minutes INT,
    vote_average DOUBLE PRECISION,
    UNIQUE (show_id, season_number, episode_number)
);

CREATE TABLE media_items (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    media_file_id UUID NOT NULL UNIQUE REFERENCES media_files(id) ON DELETE CASCADE,
    movie_id UUID REFERENCES movies(id) ON DELETE SET NULL,
    episode_id UUID REFERENCES episodes(id) ON DELETE SET NULL,
    match_status TEXT NOT NULL DEFAULT 'unmatched',
    matched_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (
        (movie_id IS NOT NULL AND episode_id IS NULL)
        OR (movie_id IS NULL AND episode_id IS NOT NULL)
        OR (movie_id IS NULL AND episode_id IS NULL)
    )
);

CREATE INDEX media_items_movie_id_idx ON media_items(movie_id);
CREATE INDEX media_items_episode_id_idx ON media_items(episode_id);
CREATE INDEX movies_title_idx ON movies(title);
CREATE INDEX shows_name_idx ON shows(name);
