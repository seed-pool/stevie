DROP INDEX IF EXISTS vod_movies_resolution_idx;
ALTER TABLE vod_movies
    DROP COLUMN IF EXISTS resolution,
    DROP COLUMN IF EXISTS video_codec,
    DROP COLUMN IF EXISTS audio_codec,
    DROP COLUMN IF EXISTS source_quality,
    DROP COLUMN IF EXISTS hdr,
    DROP COLUMN IF EXISTS bitrate_kbps,
    DROP COLUMN IF EXISTS width,
    DROP COLUMN IF EXISTS height;
