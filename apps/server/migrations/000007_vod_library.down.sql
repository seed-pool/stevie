DROP TABLE IF EXISTS vod_series;
DROP TABLE IF EXISTS vod_movies;
DROP TABLE IF EXISTS vod_categories;
DELETE FROM app_settings WHERE key IN (
    'vod_imported_movie_category_ids',
    'vod_imported_series_category_ids'
);
