DROP INDEX IF EXISTS live_programs_tvg_start_idx;
DROP INDEX IF EXISTS live_channels_epg_idx;
DROP INDEX IF EXISTS live_channels_source_idx;
DROP INDEX IF EXISTS live_channels_category_idx;
DROP INDEX IF EXISTS live_channels_source_external_uidx;

ALTER TABLE live_channels
    DROP COLUMN IF EXISTS num,
    DROP COLUMN IF EXISTS epg_channel_id,
    DROP COLUMN IF EXISTS category_id,
    DROP COLUMN IF EXISTS external_id,
    DROP COLUMN IF EXISTS source;

DROP TABLE IF EXISTS live_categories;

DELETE FROM app_settings WHERE key = 'live_imported_category_ids';
