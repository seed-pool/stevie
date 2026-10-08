package store

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stevie-media/stevie/apps/server/internal/livetv"
)

const (
	SettingVodImportedMovieCategoryIDs  = "vod_imported_movie_category_ids"
	SettingVodImportedSeriesCategoryIDs = "vod_imported_series_category_ids"
)

type VodCategory struct {
	ID         uuid.UUID `json:"id"`
	Kind       string    `json:"kind"`
	ExternalID string    `json:"external_id"`
	Name       string    `json:"name"`
	Imported   bool      `json:"imported"`
	TitleCount int       `json:"title_count"`
	SortOrder  int       `json:"sort_order"`
}

type VodMovieRow struct {
	ID                 uuid.UUID  `json:"id"`
	ExternalID         string     `json:"external_id"`
	CategoryID         *uuid.UUID `json:"category_id,omitempty"`
	CategoryExternalID string     `json:"category_external_id"`
	CategoryName       string     `json:"category_name,omitempty"`
	Name               string     `json:"name"`
	Plot               string     `json:"plot"`
	PosterURL          string     `json:"poster_url"`
	BackdropURL        string     `json:"backdrop_url"`
	TMDBID             string     `json:"tmdb_id"`
	Rating             float64    `json:"rating"`
	Year               string     `json:"year"`
	ReleaseDate        string     `json:"release_date"`
	Genre              string     `json:"genre"`
	Director           string     `json:"director"`
	Cast               string     `json:"cast"`
	Duration           string     `json:"duration"`
	Container          string     `json:"container"`
	Trailer            string     `json:"trailer"`
	Resolution         string     `json:"resolution,omitempty"`
	VideoCodec         string     `json:"video_codec,omitempty"`
	AudioCodec         string     `json:"audio_codec,omitempty"`
	SourceQuality      string     `json:"source_quality,omitempty"`
	HDR                string     `json:"hdr,omitempty"`
	BitrateKbps        int        `json:"bitrate_kbps,omitempty"`
	Width              int        `json:"width,omitempty"`
	Height             int        `json:"height,omitempty"`
	AddedAt            *time.Time `json:"added_at,omitempty"`
}

// EnrichVodMovieTech fills empty tech fields from the stream name (for pre-migration rows).
func EnrichVodMovieTech(m *VodMovieRow) {
	if m == nil {
		return
	}
	tech := livetv.ParseVodTech(m.Name)
	if m.Resolution == "" {
		m.Resolution = tech.Resolution
	}
	if m.VideoCodec == "" {
		m.VideoCodec = tech.VideoCodec
	}
	if m.AudioCodec == "" {
		m.AudioCodec = tech.AudioCodec
	}
	if m.SourceQuality == "" {
		m.SourceQuality = tech.Source
	}
	if m.HDR == "" {
		m.HDR = tech.HDR
	}
	if m.Height == 0 {
		m.Height = tech.Height
	}
	if m.Width == 0 {
		m.Width = tech.Width
	}
	if m.BitrateKbps == 0 {
		m.BitrateKbps = tech.BitrateKbps
	}
}

type VodSeriesRow struct {
	ID                 uuid.UUID  `json:"id"`
	ExternalID         string     `json:"external_id"`
	CategoryID         *uuid.UUID `json:"category_id,omitempty"`
	CategoryExternalID string     `json:"category_external_id"`
	CategoryName       string     `json:"category_name,omitempty"`
	Name               string     `json:"name"`
	Plot               string     `json:"plot"`
	PosterURL          string     `json:"poster_url"`
	BackdropURL        string     `json:"backdrop_url"`
	TMDBID             string     `json:"tmdb_id"`
	Rating             float64    `json:"rating"`
	Year               string     `json:"year"`
	ReleaseDate        string     `json:"release_date"`
	Genre              string     `json:"genre"`
	Director           string     `json:"director"`
	Cast               string     `json:"cast"`
	EpisodeRunTime     string     `json:"episode_run_time"`
	Trailer            string     `json:"trailer"`
	LastModified       *time.Time `json:"last_modified,omitempty"`
}

type VodListOpts struct {
	Category string
	Q        string
	Sort     string // name | recent
	Limit    int
	Offset   int
}

func (s *Store) ImportedVodCategoryIDs(ctx context.Context, kind string) ([]string, error) {
	key := SettingVodImportedMovieCategoryIDs
	if kind == livetv.VodKindSeries {
		key = SettingVodImportedSeriesCategoryIDs
	}
	raw, err := s.GetSetting(ctx, key)
	if err != nil {
		return nil, err
	}
	if raw == "" {
		return []string{}, nil
	}
	var ids []string
	if err := json.Unmarshal([]byte(raw), &ids); err != nil {
		return []string{}, nil
	}
	return ids, nil
}

func (s *Store) SetImportedVodCategoryIDs(ctx context.Context, kind string, ids []string) error {
	if ids == nil {
		ids = []string{}
	}
	key := SettingVodImportedMovieCategoryIDs
	if kind == livetv.VodKindSeries {
		key = SettingVodImportedSeriesCategoryIDs
	}
	b, err := json.Marshal(ids)
	if err != nil {
		return err
	}
	return s.SetSetting(ctx, key, string(b))
}

func (s *Store) UpsertVodCategories(ctx context.Context, kind string, cats []livetv.VodCategoryInfo, importedIDs []string) error {
	imported := map[string]struct{}{}
	for _, id := range importedIDs {
		imported[id] = struct{}{}
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `
		UPDATE vod_categories SET imported = false, updated_at = now()
		WHERE kind = $1
	`, kind); err != nil {
		return err
	}

	for i, c := range cats {
		_, ok := imported[c.ExternalID]
		sortOrder := c.SortOrder
		if sortOrder == 0 {
			sortOrder = i
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO vod_categories (kind, external_id, name, imported, title_count, sort_order)
			VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (kind, external_id) DO UPDATE SET
				name = EXCLUDED.name,
				imported = EXCLUDED.imported,
				title_count = EXCLUDED.title_count,
				sort_order = EXCLUDED.sort_order,
				updated_at = now()
		`, kind, c.ExternalID, c.Name, ok, c.TitleCount, sortOrder)
		if err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *Store) vodCategoryMap(ctx context.Context, tx pgx.Tx, kind string) (map[string]uuid.UUID, error) {
	rows, err := tx.Query(ctx, `SELECT id, external_id FROM vod_categories WHERE kind = $1`, kind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]uuid.UUID{}
	for rows.Next() {
		var id uuid.UUID
		var ext string
		if err := rows.Scan(&id, &ext); err != nil {
			return nil, err
		}
		out[ext] = id
	}
	return out, rows.Err()
}

func (s *Store) pruneVodMovies(ctx context.Context, tx pgx.Tx, keepCategoryExtIDs []string) error {
	if keepCategoryExtIDs == nil {
		return nil // caller asked not to prune
	}
	if len(keepCategoryExtIDs) == 0 {
		_, err := tx.Exec(ctx, `DELETE FROM vod_movies`)
		return err
	}
	_, err := tx.Exec(ctx, `
		DELETE FROM vod_movies
		WHERE category_external_id = ''
		   OR category_external_id NOT IN (SELECT unnest($1::text[]))
	`, keepCategoryExtIDs)
	return err
}

func (s *Store) pruneVodSeries(ctx context.Context, tx pgx.Tx, keepCategoryExtIDs []string) error {
	if keepCategoryExtIDs == nil {
		return nil
	}
	if len(keepCategoryExtIDs) == 0 {
		_, err := tx.Exec(ctx, `DELETE FROM vod_series`)
		return err
	}
	_, err := tx.Exec(ctx, `
		DELETE FROM vod_series
		WHERE category_external_id = ''
		   OR category_external_id NOT IN (SELECT unnest($1::text[]))
	`, keepCategoryExtIDs)
	return err
}

// UpsertVodMovies upserts movie rows. Pass keepCategoryExtIDs to prune; nil skips prune.
func (s *Store) UpsertVodMovies(ctx context.Context, movies []livetv.VodMovie, keepCategoryExtIDs []string) (int, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	if err := s.pruneVodMovies(ctx, tx, keepCategoryExtIDs); err != nil {
		return 0, err
	}
	if len(movies) == 0 {
		if err := s.refreshVodCategoryCounts(ctx, tx, livetv.VodKindMovie); err != nil {
			return 0, err
		}
		return 0, tx.Commit(ctx)
	}

	catMap, err := s.vodCategoryMap(ctx, tx, livetv.VodKindMovie)
	if err != nil {
		return 0, err
	}

	written := 0
	const chunk = 400
	for i := 0; i < len(movies); i += chunk {
		end := i + chunk
		if end > len(movies) {
			end = len(movies)
		}
		batch := &pgx.Batch{}
		for _, m := range movies[i:end] {
			var catPtr *uuid.UUID
			if id, ok := catMap[m.CategoryExternalID]; ok {
				catPtr = &id
			}
			batch.Queue(`
				INSERT INTO vod_movies (
					external_id, category_id, category_external_id, name, plot,
					poster_url, backdrop_url, tmdb_id, rating, year, release_date,
					genre, director, cast_text, duration, container, trailer, added_at, sort_order,
					resolution, video_codec, audio_codec, source_quality, hdr, bitrate_kbps, width, height
				) VALUES (
					$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,
					$20,$21,$22,$23,$24,$25,$26,$27
				)
				ON CONFLICT (external_id) DO UPDATE SET
					category_id = EXCLUDED.category_id,
					category_external_id = EXCLUDED.category_external_id,
					name = EXCLUDED.name,
					plot = CASE WHEN EXCLUDED.plot <> '' THEN EXCLUDED.plot ELSE vod_movies.plot END,
					poster_url = CASE WHEN EXCLUDED.poster_url <> '' THEN EXCLUDED.poster_url ELSE vod_movies.poster_url END,
					backdrop_url = CASE WHEN EXCLUDED.backdrop_url <> '' THEN EXCLUDED.backdrop_url ELSE vod_movies.backdrop_url END,
					tmdb_id = CASE WHEN EXCLUDED.tmdb_id <> '' THEN EXCLUDED.tmdb_id ELSE vod_movies.tmdb_id END,
					rating = CASE WHEN EXCLUDED.rating > 0 THEN EXCLUDED.rating ELSE vod_movies.rating END,
					year = CASE WHEN EXCLUDED.year <> '' THEN EXCLUDED.year ELSE vod_movies.year END,
					release_date = CASE WHEN EXCLUDED.release_date <> '' THEN EXCLUDED.release_date ELSE vod_movies.release_date END,
					genre = CASE WHEN EXCLUDED.genre <> '' THEN EXCLUDED.genre ELSE vod_movies.genre END,
					director = CASE WHEN EXCLUDED.director <> '' THEN EXCLUDED.director ELSE vod_movies.director END,
					cast_text = CASE WHEN EXCLUDED.cast_text <> '' THEN EXCLUDED.cast_text ELSE vod_movies.cast_text END,
					duration = CASE WHEN EXCLUDED.duration <> '' THEN EXCLUDED.duration ELSE vod_movies.duration END,
					container = CASE WHEN EXCLUDED.container <> '' THEN EXCLUDED.container ELSE vod_movies.container END,
					trailer = CASE WHEN EXCLUDED.trailer <> '' THEN EXCLUDED.trailer ELSE vod_movies.trailer END,
					resolution = CASE WHEN EXCLUDED.resolution <> '' THEN EXCLUDED.resolution ELSE vod_movies.resolution END,
					video_codec = CASE WHEN EXCLUDED.video_codec <> '' THEN EXCLUDED.video_codec ELSE vod_movies.video_codec END,
					audio_codec = CASE WHEN EXCLUDED.audio_codec <> '' THEN EXCLUDED.audio_codec ELSE vod_movies.audio_codec END,
					source_quality = CASE WHEN EXCLUDED.source_quality <> '' THEN EXCLUDED.source_quality ELSE vod_movies.source_quality END,
					hdr = CASE WHEN EXCLUDED.hdr <> '' THEN EXCLUDED.hdr ELSE vod_movies.hdr END,
					bitrate_kbps = CASE WHEN EXCLUDED.bitrate_kbps > 0 THEN EXCLUDED.bitrate_kbps ELSE vod_movies.bitrate_kbps END,
					width = CASE WHEN EXCLUDED.width > 0 THEN EXCLUDED.width ELSE vod_movies.width END,
					height = CASE WHEN EXCLUDED.height > 0 THEN EXCLUDED.height ELSE vod_movies.height END,
					added_at = COALESCE(EXCLUDED.added_at, vod_movies.added_at),
					sort_order = EXCLUDED.sort_order,
					updated_at = now()
			`, m.ExternalID, catPtr, m.CategoryExternalID, m.Name, m.Plot,
				m.PosterURL, m.BackdropURL, m.TMDBID, m.Rating, m.Year, m.ReleaseDate,
				m.Genre, m.Director, m.Cast, m.Duration, m.Container, m.Trailer, m.AddedAt, m.SortOrder,
				m.Resolution, m.VideoCodec, m.AudioCodec, m.SourceQuality, m.HDR, m.BitrateKbps, m.Width, m.Height)
			written++
		}
		br := tx.SendBatch(ctx, batch)
		if err := br.Close(); err != nil {
			return 0, err
		}
	}
	if err := s.refreshVodCategoryCounts(ctx, tx, livetv.VodKindMovie); err != nil {
		return 0, err
	}
	return written, tx.Commit(ctx)
}

// UpsertVodSeries upserts series rows. Pass keepCategoryExtIDs to prune; nil skips prune.
func (s *Store) UpsertVodSeries(ctx context.Context, series []livetv.VodSeries, keepCategoryExtIDs []string) (int, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	if err := s.pruneVodSeries(ctx, tx, keepCategoryExtIDs); err != nil {
		return 0, err
	}
	if len(series) == 0 {
		if err := s.refreshVodCategoryCounts(ctx, tx, livetv.VodKindSeries); err != nil {
			return 0, err
		}
		return 0, tx.Commit(ctx)
	}

	catMap, err := s.vodCategoryMap(ctx, tx, livetv.VodKindSeries)
	if err != nil {
		return 0, err
	}

	written := 0
	const chunk = 400
	for i := 0; i < len(series); i += chunk {
		end := i + chunk
		if end > len(series) {
			end = len(series)
		}
		batch := &pgx.Batch{}
		for _, m := range series[i:end] {
			var catPtr *uuid.UUID
			if id, ok := catMap[m.CategoryExternalID]; ok {
				catPtr = &id
			}
			batch.Queue(`
				INSERT INTO vod_series (
					external_id, category_id, category_external_id, name, plot,
					poster_url, backdrop_url, tmdb_id, rating, year, release_date,
					genre, director, cast_text, episode_run_time, trailer, last_modified, sort_order
				) VALUES (
					$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18
				)
				ON CONFLICT (external_id) DO UPDATE SET
					category_id = EXCLUDED.category_id,
					category_external_id = EXCLUDED.category_external_id,
					name = EXCLUDED.name,
					plot = CASE WHEN EXCLUDED.plot <> '' THEN EXCLUDED.plot ELSE vod_series.plot END,
					poster_url = CASE WHEN EXCLUDED.poster_url <> '' THEN EXCLUDED.poster_url ELSE vod_series.poster_url END,
					backdrop_url = CASE WHEN EXCLUDED.backdrop_url <> '' THEN EXCLUDED.backdrop_url ELSE vod_series.backdrop_url END,
					tmdb_id = CASE WHEN EXCLUDED.tmdb_id <> '' THEN EXCLUDED.tmdb_id ELSE vod_series.tmdb_id END,
					rating = CASE WHEN EXCLUDED.rating > 0 THEN EXCLUDED.rating ELSE vod_series.rating END,
					year = CASE WHEN EXCLUDED.year <> '' THEN EXCLUDED.year ELSE vod_series.year END,
					release_date = CASE WHEN EXCLUDED.release_date <> '' THEN EXCLUDED.release_date ELSE vod_series.release_date END,
					genre = CASE WHEN EXCLUDED.genre <> '' THEN EXCLUDED.genre ELSE vod_series.genre END,
					director = CASE WHEN EXCLUDED.director <> '' THEN EXCLUDED.director ELSE vod_series.director END,
					cast_text = CASE WHEN EXCLUDED.cast_text <> '' THEN EXCLUDED.cast_text ELSE vod_series.cast_text END,
					episode_run_time = CASE WHEN EXCLUDED.episode_run_time <> '' THEN EXCLUDED.episode_run_time ELSE vod_series.episode_run_time END,
					trailer = CASE WHEN EXCLUDED.trailer <> '' THEN EXCLUDED.trailer ELSE vod_series.trailer END,
					last_modified = COALESCE(EXCLUDED.last_modified, vod_series.last_modified),
					sort_order = EXCLUDED.sort_order,
					updated_at = now()
			`, m.ExternalID, catPtr, m.CategoryExternalID, m.Name, m.Plot,
				m.PosterURL, m.BackdropURL, m.TMDBID, m.Rating, m.Year, m.ReleaseDate,
				m.Genre, m.Director, m.Cast, m.EpisodeRunTime, m.Trailer, m.LastModified, m.SortOrder)
			written++
		}
		br := tx.SendBatch(ctx, batch)
		if err := br.Close(); err != nil {
			return 0, err
		}
	}
	if err := s.refreshVodCategoryCounts(ctx, tx, livetv.VodKindSeries); err != nil {
		return 0, err
	}
	return written, tx.Commit(ctx)
}

func (s *Store) refreshVodCategoryCounts(ctx context.Context, tx pgx.Tx, kind string) error {
	if kind == livetv.VodKindSeries {
		_, err := tx.Exec(ctx, `
			UPDATE vod_categories c SET title_count = COALESCE(s.cnt, 0), updated_at = now()
			FROM (
				SELECT category_external_id AS ext, COUNT(*)::int AS cnt
				FROM vod_series GROUP BY category_external_id
			) s
			WHERE c.kind = $1 AND c.external_id = s.ext
		`, kind)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `
			UPDATE vod_categories SET title_count = 0, updated_at = now()
			WHERE kind = $1 AND external_id NOT IN (
				SELECT DISTINCT category_external_id FROM vod_series WHERE category_external_id <> ''
			)
		`, kind)
		return err
	}
	_, err := tx.Exec(ctx, `
		UPDATE vod_categories c SET title_count = COALESCE(s.cnt, 0), updated_at = now()
		FROM (
			SELECT category_external_id AS ext, COUNT(*)::int AS cnt
			FROM vod_movies GROUP BY category_external_id
		) s
		WHERE c.kind = $1 AND c.external_id = s.ext
	`, kind)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		UPDATE vod_categories SET title_count = 0, updated_at = now()
		WHERE kind = $1 AND external_id NOT IN (
			SELECT DISTINCT category_external_id FROM vod_movies WHERE category_external_id <> ''
		)
	`, kind)
	return err
}

func (s *Store) ListVodCategories(ctx context.Context, kind string, importedOnly bool) ([]VodCategory, error) {
	q := `
		SELECT id, kind, external_id, name, imported, title_count, sort_order
		FROM vod_categories WHERE kind = $1
	`
	args := []any{kind}
	if importedOnly {
		q += ` AND imported = true`
	}
	q += ` ORDER BY name`
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []VodCategory{}
	for rows.Next() {
		var c VodCategory
		if err := rows.Scan(&c.ID, &c.Kind, &c.ExternalID, &c.Name, &c.Imported, &c.TitleCount, &c.SortOrder); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.SliceStable(out, func(i, j int) bool {
		return livetv.CategoryNameLess(out[i].Name, out[j].Name)
	})
	return out, nil
}

func (s *Store) ListVodMovies(ctx context.Context, opts VodListOpts) ([]VodMovieRow, int, error) {
	where := []string{"1=1"}
	args := []any{}
	n := 1
	if opts.Category != "" {
		where = append(where, "m.category_external_id = $"+strconv.Itoa(n))
		args = append(args, opts.Category)
		n++
	}
	if q := strings.TrimSpace(opts.Q); q != "" {
		where = append(where, "m.name ILIKE $"+strconv.Itoa(n))
		args = append(args, "%"+q+"%")
		n++
	}
	limit := opts.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	offset := opts.Offset
	if offset < 0 {
		offset = 0
	}
	order := "m.name ASC"
	if opts.Sort == "recent" {
		order = "m.added_at DESC NULLS LAST, m.name ASC"
	}

	countQ := `SELECT COUNT(*) FROM vod_movies m WHERE ` + strings.Join(where, " AND ")
	var total int
	if err := s.pool.QueryRow(ctx, countQ, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	args = append(args, limit, offset)
	listQ := `
		SELECT m.id, m.external_id, m.category_id, m.category_external_id, COALESCE(c.name, ''),
			m.name, m.plot, m.poster_url, m.backdrop_url, m.tmdb_id, m.rating, m.year, m.release_date,
			m.genre, m.director, m.cast_text, m.duration, m.container, m.trailer, m.added_at,
			m.resolution, m.video_codec, m.audio_codec, m.source_quality, m.hdr, m.bitrate_kbps, m.width, m.height
		FROM vod_movies m
		LEFT JOIN vod_categories c ON c.id = m.category_id
		WHERE ` + strings.Join(where, " AND ") + `
		ORDER BY ` + order + `
		LIMIT $` + strconv.Itoa(n) + ` OFFSET $` + strconv.Itoa(n+1)

	rows, err := s.pool.Query(ctx, listQ, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []VodMovieRow{}
	for rows.Next() {
		var m VodMovieRow
		if err := rows.Scan(
			&m.ID, &m.ExternalID, &m.CategoryID, &m.CategoryExternalID, &m.CategoryName,
			&m.Name, &m.Plot, &m.PosterURL, &m.BackdropURL, &m.TMDBID, &m.Rating, &m.Year, &m.ReleaseDate,
			&m.Genre, &m.Director, &m.Cast, &m.Duration, &m.Container, &m.Trailer, &m.AddedAt,
			&m.Resolution, &m.VideoCodec, &m.AudioCodec, &m.SourceQuality, &m.HDR, &m.BitrateKbps, &m.Width, &m.Height,
		); err != nil {
			return nil, 0, err
		}
		EnrichVodMovieTech(&m)
		out = append(out, m)
	}
	return out, total, rows.Err()
}

func (s *Store) ListVodSeries(ctx context.Context, opts VodListOpts) ([]VodSeriesRow, int, error) {
	where := []string{"1=1"}
	args := []any{}
	n := 1
	if opts.Category != "" {
		where = append(where, "m.category_external_id = $"+strconv.Itoa(n))
		args = append(args, opts.Category)
		n++
	}
	if q := strings.TrimSpace(opts.Q); q != "" {
		where = append(where, "m.name ILIKE $"+strconv.Itoa(n))
		args = append(args, "%"+q+"%")
		n++
	}
	limit := opts.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	offset := opts.Offset
	if offset < 0 {
		offset = 0
	}
	order := "m.name ASC"
	if opts.Sort == "recent" {
		order = "m.last_modified DESC NULLS LAST, m.name ASC"
	}

	countQ := `SELECT COUNT(*) FROM vod_series m WHERE ` + strings.Join(where, " AND ")
	var total int
	if err := s.pool.QueryRow(ctx, countQ, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	args = append(args, limit, offset)
	listQ := `
		SELECT m.id, m.external_id, m.category_id, m.category_external_id, COALESCE(c.name, ''),
			m.name, m.plot, m.poster_url, m.backdrop_url, m.tmdb_id, m.rating, m.year, m.release_date,
			m.genre, m.director, m.cast_text, m.episode_run_time, m.trailer, m.last_modified
		FROM vod_series m
		LEFT JOIN vod_categories c ON c.id = m.category_id
		WHERE ` + strings.Join(where, " AND ") + `
		ORDER BY ` + order + `
		LIMIT $` + strconv.Itoa(n) + ` OFFSET $` + strconv.Itoa(n+1)

	rows, err := s.pool.Query(ctx, listQ, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []VodSeriesRow{}
	for rows.Next() {
		var m VodSeriesRow
		if err := rows.Scan(
			&m.ID, &m.ExternalID, &m.CategoryID, &m.CategoryExternalID, &m.CategoryName,
			&m.Name, &m.Plot, &m.PosterURL, &m.BackdropURL, &m.TMDBID, &m.Rating, &m.Year, &m.ReleaseDate,
			&m.Genre, &m.Director, &m.Cast, &m.EpisodeRunTime, &m.Trailer, &m.LastModified,
		); err != nil {
			return nil, 0, err
		}
		out = append(out, m)
	}
	return out, total, rows.Err()
}

func (s *Store) VodMovieByID(ctx context.Context, id uuid.UUID) (VodMovieRow, error) {
	var m VodMovieRow
	err := s.pool.QueryRow(ctx, `
		SELECT m.id, m.external_id, m.category_id, m.category_external_id, COALESCE(c.name, ''),
			m.name, m.plot, m.poster_url, m.backdrop_url, m.tmdb_id, m.rating, m.year, m.release_date,
			m.genre, m.director, m.cast_text, m.duration, m.container, m.trailer, m.added_at,
			m.resolution, m.video_codec, m.audio_codec, m.source_quality, m.hdr, m.bitrate_kbps, m.width, m.height
		FROM vod_movies m
		LEFT JOIN vod_categories c ON c.id = m.category_id
		WHERE m.id = $1
	`, id).Scan(
		&m.ID, &m.ExternalID, &m.CategoryID, &m.CategoryExternalID, &m.CategoryName,
		&m.Name, &m.Plot, &m.PosterURL, &m.BackdropURL, &m.TMDBID, &m.Rating, &m.Year, &m.ReleaseDate,
		&m.Genre, &m.Director, &m.Cast, &m.Duration, &m.Container, &m.Trailer, &m.AddedAt,
		&m.Resolution, &m.VideoCodec, &m.AudioCodec, &m.SourceQuality, &m.HDR, &m.BitrateKbps, &m.Width, &m.Height,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return VodMovieRow{}, ErrNotFound
	}
	if err != nil {
		return VodMovieRow{}, err
	}
	EnrichVodMovieTech(&m)
	return m, nil
}

func (s *Store) UpdateVodMovieTech(ctx context.Context, id uuid.UUID, tech livetv.VodTech, container string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE vod_movies SET
			resolution = CASE WHEN $2 <> '' THEN $2 ELSE resolution END,
			video_codec = CASE WHEN $3 <> '' THEN $3 ELSE video_codec END,
			audio_codec = CASE WHEN $4 <> '' THEN $4 ELSE audio_codec END,
			source_quality = CASE WHEN $5 <> '' THEN $5 ELSE source_quality END,
			hdr = CASE WHEN $6 <> '' THEN $6 ELSE hdr END,
			bitrate_kbps = CASE WHEN $7 > 0 THEN $7 ELSE bitrate_kbps END,
			width = CASE WHEN $8 > 0 THEN $8 ELSE width END,
			height = CASE WHEN $9 > 0 THEN $9 ELSE height END,
			container = CASE WHEN $10 <> '' THEN $10 ELSE container END,
			updated_at = now()
		WHERE id = $1
	`, id, tech.Resolution, tech.VideoCodec, tech.AudioCodec, tech.Source, tech.HDR,
		tech.BitrateKbps, tech.Width, tech.Height, container)
	return err
}

// ListVodMovieVersions returns other streams that share a TMDB id (alternate encodes).
func (s *Store) ListVodMovieVersions(ctx context.Context, tmdbID string, exclude uuid.UUID, limit int) ([]VodMovieRow, error) {
	tmdbID = strings.TrimSpace(tmdbID)
	if tmdbID == "" || tmdbID == "0" {
		return nil, nil
	}
	if limit <= 0 || limit > 40 {
		limit = 20
	}
	rows, err := s.pool.Query(ctx, `
		SELECT m.id, m.external_id, m.category_id, m.category_external_id, COALESCE(c.name, ''),
			m.name, m.plot, m.poster_url, m.backdrop_url, m.tmdb_id, m.rating, m.year, m.release_date,
			m.genre, m.director, m.cast_text, m.duration, m.container, m.trailer, m.added_at,
			m.resolution, m.video_codec, m.audio_codec, m.source_quality, m.hdr, m.bitrate_kbps, m.width, m.height
		FROM vod_movies m
		LEFT JOIN vod_categories c ON c.id = m.category_id
		WHERE m.tmdb_id = $1 AND m.id <> $2
		ORDER BY m.height DESC NULLS LAST, m.bitrate_kbps DESC, m.name ASC
		LIMIT $3
	`, tmdbID, exclude, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []VodMovieRow{}
	for rows.Next() {
		var m VodMovieRow
		if err := rows.Scan(
			&m.ID, &m.ExternalID, &m.CategoryID, &m.CategoryExternalID, &m.CategoryName,
			&m.Name, &m.Plot, &m.PosterURL, &m.BackdropURL, &m.TMDBID, &m.Rating, &m.Year, &m.ReleaseDate,
			&m.Genre, &m.Director, &m.Cast, &m.Duration, &m.Container, &m.Trailer, &m.AddedAt,
			&m.Resolution, &m.VideoCodec, &m.AudioCodec, &m.SourceQuality, &m.HDR, &m.BitrateKbps, &m.Width, &m.Height,
		); err != nil {
			return nil, err
		}
		EnrichVodMovieTech(&m)
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) VodSeriesByID(ctx context.Context, id uuid.UUID) (VodSeriesRow, error) {
	var m VodSeriesRow
	err := s.pool.QueryRow(ctx, `
		SELECT m.id, m.external_id, m.category_id, m.category_external_id, COALESCE(c.name, ''),
			m.name, m.plot, m.poster_url, m.backdrop_url, m.tmdb_id, m.rating, m.year, m.release_date,
			m.genre, m.director, m.cast_text, m.episode_run_time, m.trailer, m.last_modified
		FROM vod_series m
		LEFT JOIN vod_categories c ON c.id = m.category_id
		WHERE m.id = $1
	`, id).Scan(
		&m.ID, &m.ExternalID, &m.CategoryID, &m.CategoryExternalID, &m.CategoryName,
		&m.Name, &m.Plot, &m.PosterURL, &m.BackdropURL, &m.TMDBID, &m.Rating, &m.Year, &m.ReleaseDate,
		&m.Genre, &m.Director, &m.Cast, &m.EpisodeRunTime, &m.Trailer, &m.LastModified,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return VodSeriesRow{}, ErrNotFound
	}
	return m, err
}

func (s *Store) UpdateVodMovieDetails(ctx context.Context, id uuid.UUID, plot, backdrop, genre, director, cast, duration, trailer, year, releaseDate string, rating float64, tmdbID string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE vod_movies SET
			plot = CASE WHEN $2 <> '' THEN $2 ELSE plot END,
			backdrop_url = CASE WHEN $3 <> '' THEN $3 ELSE backdrop_url END,
			genre = CASE WHEN $4 <> '' THEN $4 ELSE genre END,
			director = CASE WHEN $5 <> '' THEN $5 ELSE director END,
			cast_text = CASE WHEN $6 <> '' THEN $6 ELSE cast_text END,
			duration = CASE WHEN $7 <> '' THEN $7 ELSE duration END,
			trailer = CASE WHEN $8 <> '' THEN $8 ELSE trailer END,
			year = CASE WHEN $9 <> '' THEN $9 ELSE year END,
			release_date = CASE WHEN $10 <> '' THEN $10 ELSE release_date END,
			rating = CASE WHEN $11 > 0 THEN $11 ELSE rating END,
			tmdb_id = CASE WHEN $12 <> '' THEN $12 ELSE tmdb_id END,
			updated_at = now()
		WHERE id = $1
	`, id, plot, backdrop, genre, director, cast, duration, trailer, year, releaseDate, rating, tmdbID)
	return err
}

func (s *Store) UpdateVodSeriesDetails(ctx context.Context, id uuid.UUID, plot, backdrop, genre, director, cast, trailer, year, releaseDate string, rating float64, tmdbID string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE vod_series SET
			plot = CASE WHEN $2 <> '' THEN $2 ELSE plot END,
			backdrop_url = CASE WHEN $3 <> '' THEN $3 ELSE backdrop_url END,
			genre = CASE WHEN $4 <> '' THEN $4 ELSE genre END,
			director = CASE WHEN $5 <> '' THEN $5 ELSE director END,
			cast_text = CASE WHEN $6 <> '' THEN $6 ELSE cast_text END,
			trailer = CASE WHEN $7 <> '' THEN $7 ELSE trailer END,
			year = CASE WHEN $8 <> '' THEN $8 ELSE year END,
			release_date = CASE WHEN $9 <> '' THEN $9 ELSE release_date END,
			rating = CASE WHEN $10 > 0 THEN $10 ELSE rating END,
			tmdb_id = CASE WHEN $11 <> '' THEN $11 ELSE tmdb_id END,
			updated_at = now()
		WHERE id = $1
	`, id, plot, backdrop, genre, director, cast, trailer, year, releaseDate, rating, tmdbID)
	return err
}

func (s *Store) VodMovieCount(ctx context.Context) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM vod_movies`).Scan(&n)
	return n, err
}

func (s *Store) VodSeriesCount(ctx context.Context) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM vod_series`).Scan(&n)
	return n, err
}
