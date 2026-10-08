package store

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type MovieUpsert struct {
	TMDBID         int
	Title          string
	OriginalTitle  string
	Tagline        string
	Overview       string
	ReleaseDate    string
	RuntimeMinutes int
	VoteAverage    float64
	VoteCount      int
	Popularity     float64
	PosterPath     string
	BackdropPath   string
	Genres         json.RawMessage
	CastCrew       json.RawMessage
	ExternalIDs    json.RawMessage
}

type ShowUpsert struct {
	TMDBID       int
	Name         string
	OriginalName string
	Tagline      string
	Overview     string
	FirstAirDate string
	VoteAverage  float64
	VoteCount    int
	Popularity   float64
	PosterPath   string
	BackdropPath string
	Genres       json.RawMessage
	CastCrew     json.RawMessage
	ExternalIDs  json.RawMessage
}

type EpisodeUpsert struct {
	ShowTMDBID     int
	TMDBID         int
	SeasonNumber   int
	EpisodeNumber  int
	Name           string
	Overview       string
	StillPath      string
	AirDate        string
	RuntimeMinutes int
	VoteAverage    float64
	SeasonName     string
	SeasonOverview string
	SeasonPoster   string
	SeasonAirDate  string
	SeasonTMDBID   int
}

func (s *Store) UpsertMovie(ctx context.Context, m MovieUpsert) (uuid.UUID, error) {
	if m.Genres == nil {
		m.Genres = json.RawMessage("[]")
	}
	if m.CastCrew == nil {
		m.CastCrew = json.RawMessage("[]")
	}
	if m.ExternalIDs == nil {
		m.ExternalIDs = json.RawMessage("{}")
	}
	var id uuid.UUID
	err := s.pool.QueryRow(ctx, `
		INSERT INTO movies (
			tmdb_id, title, original_title, tagline, overview, release_date, runtime_minutes,
			vote_average, vote_count, popularity, poster_path, backdrop_path, genres, cast_crew,
			external_ids, updated_at
		) VALUES (
			$1,$2,$3,$4,$5,NULLIF($6,'')::date,NULLIF($7,0),$8,$9,$10,$11,$12,$13,$14,$15,now()
		)
		ON CONFLICT (tmdb_id) DO UPDATE SET
			title = EXCLUDED.title,
			original_title = EXCLUDED.original_title,
			tagline = EXCLUDED.tagline,
			overview = EXCLUDED.overview,
			release_date = EXCLUDED.release_date,
			runtime_minutes = EXCLUDED.runtime_minutes,
			vote_average = EXCLUDED.vote_average,
			vote_count = EXCLUDED.vote_count,
			popularity = EXCLUDED.popularity,
			poster_path = EXCLUDED.poster_path,
			backdrop_path = EXCLUDED.backdrop_path,
			genres = EXCLUDED.genres,
			cast_crew = EXCLUDED.cast_crew,
			external_ids = EXCLUDED.external_ids,
			updated_at = now()
		RETURNING id
	`,
		m.TMDBID, m.Title, nullStr(m.OriginalTitle), nullStr(m.Tagline), nullStr(m.Overview),
		m.ReleaseDate, m.RuntimeMinutes, m.VoteAverage, m.VoteCount, m.Popularity,
		nullStr(m.PosterPath), nullStr(m.BackdropPath), m.Genres, m.CastCrew, m.ExternalIDs,
	).Scan(&id)
	return id, err
}

func (s *Store) UpsertShow(ctx context.Context, sh ShowUpsert) (uuid.UUID, error) {
	if sh.Genres == nil {
		sh.Genres = json.RawMessage("[]")
	}
	if sh.CastCrew == nil {
		sh.CastCrew = json.RawMessage("[]")
	}
	if sh.ExternalIDs == nil {
		sh.ExternalIDs = json.RawMessage("{}")
	}
	var id uuid.UUID
	err := s.pool.QueryRow(ctx, `
		INSERT INTO shows (
			tmdb_id, name, original_name, tagline, overview, first_air_date, vote_average,
			vote_count, popularity, poster_path, backdrop_path, genres, cast_crew, external_ids, updated_at
		) VALUES (
			$1,$2,$3,$4,$5,NULLIF($6,'')::date,$7,$8,$9,$10,$11,$12,$13,$14,now()
		)
		ON CONFLICT (tmdb_id) DO UPDATE SET
			name = EXCLUDED.name,
			original_name = EXCLUDED.original_name,
			tagline = EXCLUDED.tagline,
			overview = EXCLUDED.overview,
			first_air_date = EXCLUDED.first_air_date,
			vote_average = EXCLUDED.vote_average,
			vote_count = EXCLUDED.vote_count,
			popularity = EXCLUDED.popularity,
			poster_path = EXCLUDED.poster_path,
			backdrop_path = EXCLUDED.backdrop_path,
			genres = EXCLUDED.genres,
			cast_crew = EXCLUDED.cast_crew,
			external_ids = EXCLUDED.external_ids,
			updated_at = now()
		RETURNING id
	`,
		sh.TMDBID, sh.Name, nullStr(sh.OriginalName), nullStr(sh.Tagline), nullStr(sh.Overview),
		sh.FirstAirDate, sh.VoteAverage, sh.VoteCount, sh.Popularity,
		nullStr(sh.PosterPath), nullStr(sh.BackdropPath), sh.Genres, sh.CastCrew, sh.ExternalIDs,
	).Scan(&id)
	return id, err
}

func (s *Store) UpsertEpisode(ctx context.Context, showID uuid.UUID, ep EpisodeUpsert) (uuid.UUID, error) {
	var seasonID uuid.UUID
	err := s.pool.QueryRow(ctx, `
		INSERT INTO seasons (show_id, tmdb_id, season_number, name, overview, poster_path, air_date)
		VALUES ($1, NULLIF($2,0), $3, $4, $5, $6, NULLIF($7,'')::date)
		ON CONFLICT (show_id, season_number) DO UPDATE SET
			tmdb_id = COALESCE(EXCLUDED.tmdb_id, seasons.tmdb_id),
			name = COALESCE(EXCLUDED.name, seasons.name),
			overview = COALESCE(EXCLUDED.overview, seasons.overview),
			poster_path = COALESCE(EXCLUDED.poster_path, seasons.poster_path),
			air_date = COALESCE(EXCLUDED.air_date, seasons.air_date)
		RETURNING id
	`, showID, ep.SeasonTMDBID, ep.SeasonNumber, nullStr(ep.SeasonName), nullStr(ep.SeasonOverview),
		nullStr(ep.SeasonPoster), ep.SeasonAirDate,
	).Scan(&seasonID)
	if err != nil {
		return uuid.Nil, err
	}

	var episodeID uuid.UUID
	err = s.pool.QueryRow(ctx, `
		INSERT INTO episodes (
			show_id, season_id, tmdb_id, season_number, episode_number, name, overview,
			still_path, air_date, runtime_minutes, vote_average
		) VALUES (
			$1,$2,NULLIF($3,0),$4,$5,$6,$7,$8,NULLIF($9,'')::date,NULLIF($10,0),$11
		)
		ON CONFLICT (show_id, season_number, episode_number) DO UPDATE SET
			season_id = EXCLUDED.season_id,
			tmdb_id = COALESCE(EXCLUDED.tmdb_id, episodes.tmdb_id),
			name = COALESCE(EXCLUDED.name, episodes.name),
			overview = COALESCE(EXCLUDED.overview, episodes.overview),
			still_path = COALESCE(EXCLUDED.still_path, episodes.still_path),
			air_date = COALESCE(EXCLUDED.air_date, episodes.air_date),
			runtime_minutes = COALESCE(EXCLUDED.runtime_minutes, episodes.runtime_minutes),
			vote_average = COALESCE(EXCLUDED.vote_average, episodes.vote_average)
		RETURNING id
	`, showID, seasonID, ep.TMDBID, ep.SeasonNumber, ep.EpisodeNumber, nullStr(ep.Name),
		nullStr(ep.Overview), nullStr(ep.StillPath), ep.AirDate, ep.RuntimeMinutes, ep.VoteAverage,
	).Scan(&episodeID)
	return episodeID, err
}

func (s *Store) LinkMovie(ctx context.Context, mediaFileID, movieID uuid.UUID) error {
	return s.linkMovieStatus(ctx, mediaFileID, movieID, "matched")
}

func (s *Store) LinkMovieLocal(ctx context.Context, mediaFileID, movieID uuid.UUID) error {
	return s.linkMovieStatus(ctx, mediaFileID, movieID, "local")
}

func (s *Store) linkMovieStatus(ctx context.Context, mediaFileID, movieID uuid.UUID, status string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE media_items
		SET movie_id = $2, episode_id = NULL, match_status = $3, matched_at = now(), updated_at = now()
		WHERE media_file_id = $1
	`, mediaFileID, movieID, status)
	return err
}

func (s *Store) LinkEpisode(ctx context.Context, mediaFileID, episodeID uuid.UUID) error {
	return s.linkEpisodeStatus(ctx, mediaFileID, episodeID, "matched")
}

func (s *Store) LinkEpisodeLocal(ctx context.Context, mediaFileID, episodeID uuid.UUID) error {
	return s.linkEpisodeStatus(ctx, mediaFileID, episodeID, "local")
}

func (s *Store) linkEpisodeStatus(ctx context.Context, mediaFileID, episodeID uuid.UUID, status string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE media_items
		SET episode_id = $2, movie_id = NULL, match_status = $3, matched_at = now(), updated_at = now()
		WHERE media_file_id = $1
	`, mediaFileID, episodeID, status)
	return err
}

type ListOpts struct {
	Sort  string // "title" (default) or "recent"
	Limit int
}

func (s *Store) ListMovies(ctx context.Context, opts ListOpts) ([]Movie, error) {
	order := "m.title"
	if opts.Sort == "recent" {
		order = "m.created_at DESC, m.title"
	}
	limit := 100000
	if opts.Limit > 0 {
		limit = opts.Limit
	}
	// One row per movie. Tech fields come from the largest file when only one
	// release exists; multiple releases are summarized via release_count.
	rows, err := s.pool.Query(ctx, `
		WITH ranked AS (
			SELECT m.id AS movie_id, f.id AS file_id, f.container, f.size_bytes,
			       COUNT(*) OVER (PARTITION BY m.id) AS release_count,
			       ROW_NUMBER() OVER (PARTITION BY m.id ORDER BY f.size_bytes DESC, f.path) AS rn
			FROM movies m
			JOIN media_items i ON i.movie_id = m.id
			JOIN media_files f ON f.id = i.media_file_id AND f.soft_deleted_at IS NULL
		)
		SELECT m.id, m.tmdb_id, m.title, m.original_title, m.tagline, m.overview,
		       to_char(m.release_date, 'YYYY-MM-DD'), m.runtime_minutes, m.vote_average, m.vote_count,
		       m.popularity, m.poster_path, m.backdrop_path, m.genres, m.cast_crew, m.external_ids,
		       m.created_at, m.updated_at,
		       r.file_id, r.container, r.release_count,
		       (SELECT ms.height FROM media_streams ms WHERE ms.media_file_id = r.file_id AND ms.codec_type = 'video' ORDER BY ms.stream_index LIMIT 1),
		       (SELECT ms.codec_name FROM media_streams ms WHERE ms.media_file_id = r.file_id AND ms.codec_type = 'video' ORDER BY ms.stream_index LIMIT 1),
		       (SELECT ms.codec_name FROM media_streams ms WHERE ms.media_file_id = r.file_id AND ms.codec_type = 'audio' ORDER BY ms.stream_index LIMIT 1)
		FROM ranked r
		JOIN movies m ON m.id = r.movie_id
		WHERE r.rn = 1
		ORDER BY `+order+`
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Movie
	for rows.Next() {
		var m Movie
		var release *string
		var height *int
		if err := rows.Scan(
			&m.ID, &m.TMDBID, &m.Title, &m.OriginalTitle, &m.Tagline, &m.Overview, &release,
			&m.RuntimeMinutes, &m.VoteAverage, &m.VoteCount, &m.Popularity, &m.PosterPath,
			&m.BackdropPath, &m.Genres, &m.CastCrew, &m.ExternalIDs, &m.CreatedAt, &m.UpdatedAt,
			&m.MediaFileID, &m.Container, &m.ReleaseCount, &height, &m.VideoCodec, &m.AudioCodec,
		); err != nil {
			return nil, err
		}
		m.ReleaseDate = release
		if m.ReleaseCount <= 1 {
			if height != nil {
				res := resolutionLabel(*height)
				m.Resolution = &res
			}
		} else {
			// Multi-release cards omit single-file tech badges.
			m.Resolution = nil
			m.VideoCodec = nil
			m.AudioCodec = nil
			m.Container = nil
			m.MediaFileID = nil
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) MovieByID(ctx context.Context, id uuid.UUID) (Movie, error) {
	var m Movie
	var release *string
	err := s.pool.QueryRow(ctx, `
		SELECT id, tmdb_id, title, original_title, tagline, overview,
		       to_char(release_date, 'YYYY-MM-DD'), runtime_minutes, vote_average, vote_count,
		       popularity, poster_path, backdrop_path, genres, cast_crew, external_ids, created_at, updated_at
		FROM movies WHERE id = $1
	`, id).Scan(
		&m.ID, &m.TMDBID, &m.Title, &m.OriginalTitle, &m.Tagline, &m.Overview, &release,
		&m.RuntimeMinutes, &m.VoteAverage, &m.VoteCount, &m.Popularity, &m.PosterPath,
		&m.BackdropPath, &m.Genres, &m.CastCrew, &m.ExternalIDs, &m.CreatedAt, &m.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Movie{}, ErrNotFound
	}
	if err != nil {
		return Movie{}, err
	}
	m.ReleaseDate = release
	return m, nil
}

func (s *Store) MediaFilesForMovie(ctx context.Context, movieID uuid.UUID) ([]MediaFile, error) {
	return s.mediaFilesByItem(ctx, `
		SELECT f.id
		FROM media_items i
		JOIN media_files f ON f.id = i.media_file_id AND f.soft_deleted_at IS NULL
		WHERE i.movie_id = $1
		ORDER BY f.size_bytes DESC, f.path
	`, movieID)
}

func (s *Store) MediaFilesForEpisode(ctx context.Context, episodeID uuid.UUID) ([]MediaFile, error) {
	return s.mediaFilesByItem(ctx, `
		SELECT f.id
		FROM media_items i
		JOIN media_files f ON f.id = i.media_file_id AND f.soft_deleted_at IS NULL
		WHERE i.episode_id = $1
		ORDER BY f.size_bytes DESC, f.path
	`, episodeID)
}

func (s *Store) mediaFilesByItem(ctx context.Context, query string, id uuid.UUID) ([]MediaFile, error) {
	rows, err := s.pool.Query(ctx, query, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MediaFile
	for rows.Next() {
		var fileID uuid.UUID
		if err := rows.Scan(&fileID); err != nil {
			return nil, err
		}
		f, err := s.MediaFileByID(ctx, fileID)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func (s *Store) ListShows(ctx context.Context, opts ListOpts) ([]Show, error) {
	order := "s.name"
	if opts.Sort == "recent" {
		order = "s.created_at DESC, s.name"
	}
	limit := 100000
	if opts.Limit > 0 {
		limit = opts.Limit
	}
	rows, err := s.pool.Query(ctx, `
		SELECT s.id, s.tmdb_id, s.name, s.original_name, s.tagline, s.overview,
		       to_char(s.first_air_date, 'YYYY-MM-DD'), s.vote_average, s.vote_count, s.popularity,
		       s.poster_path, s.backdrop_path, s.genres, s.cast_crew, s.external_ids,
		       s.created_at, s.updated_at,
		       COUNT(DISTINCT e.id) FILTER (WHERE f.id IS NOT NULL)
		FROM shows s
		LEFT JOIN episodes e ON e.show_id = s.id
		LEFT JOIN media_items i ON i.episode_id = e.id
		LEFT JOIN media_files f ON f.id = i.media_file_id AND f.soft_deleted_at IS NULL
		GROUP BY s.id
		HAVING COUNT(DISTINCT e.id) FILTER (WHERE f.id IS NOT NULL) > 0
		ORDER BY `+order+`
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Show
	for rows.Next() {
		var sh Show
		var firstAir *string
		if err := rows.Scan(
			&sh.ID, &sh.TMDBID, &sh.Name, &sh.OriginalName, &sh.Tagline, &sh.Overview, &firstAir,
			&sh.VoteAverage, &sh.VoteCount, &sh.Popularity, &sh.PosterPath, &sh.BackdropPath,
			&sh.Genres, &sh.CastCrew, &sh.ExternalIDs, &sh.CreatedAt, &sh.UpdatedAt, &sh.EpisodeCount,
		); err != nil {
			return nil, err
		}
		sh.FirstAirDate = firstAir
		out = append(out, sh)
	}
	return out, rows.Err()
}

func (s *Store) ShowByID(ctx context.Context, id uuid.UUID) (Show, []Episode, error) {
	var sh Show
	var firstAir *string
	err := s.pool.QueryRow(ctx, `
		SELECT id, tmdb_id, name, original_name, tagline, overview,
		       to_char(first_air_date, 'YYYY-MM-DD'), vote_average, vote_count, popularity,
		       poster_path, backdrop_path, genres, cast_crew, external_ids, created_at, updated_at
		FROM shows WHERE id = $1
	`, id).Scan(
		&sh.ID, &sh.TMDBID, &sh.Name, &sh.OriginalName, &sh.Tagline, &sh.Overview, &firstAir,
		&sh.VoteAverage, &sh.VoteCount, &sh.Popularity, &sh.PosterPath, &sh.BackdropPath,
		&sh.Genres, &sh.CastCrew, &sh.ExternalIDs, &sh.CreatedAt, &sh.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Show{}, nil, ErrNotFound
	}
	if err != nil {
		return Show{}, nil, err
	}
	sh.FirstAirDate = firstAir

	rows, err := s.pool.Query(ctx, `
		WITH ranked AS (
			SELECT e.id AS episode_id, f.id AS file_id, f.container, f.size_bytes,
			       COUNT(*) OVER (PARTITION BY e.id) AS release_count,
			       ROW_NUMBER() OVER (PARTITION BY e.id ORDER BY f.size_bytes DESC, f.path) AS rn
			FROM episodes e
			JOIN media_items i ON i.episode_id = e.id
			JOIN media_files f ON f.id = i.media_file_id AND f.soft_deleted_at IS NULL
			WHERE e.show_id = $1
		)
		SELECT e.id, e.show_id, e.season_number, e.episode_number, e.name, e.overview, e.still_path,
		       to_char(e.air_date, 'YYYY-MM-DD'), e.runtime_minutes, e.vote_average,
		       r.file_id, r.container, r.release_count,
		       (SELECT ms.height FROM media_streams ms WHERE ms.media_file_id = r.file_id AND ms.codec_type = 'video' ORDER BY ms.stream_index LIMIT 1),
		       (SELECT ms.codec_name FROM media_streams ms WHERE ms.media_file_id = r.file_id AND ms.codec_type = 'video' ORDER BY ms.stream_index LIMIT 1),
		       (SELECT ms.codec_name FROM media_streams ms WHERE ms.media_file_id = r.file_id AND ms.codec_type = 'audio' ORDER BY ms.stream_index LIMIT 1)
		FROM ranked r
		JOIN episodes e ON e.id = r.episode_id
		WHERE r.rn = 1
		ORDER BY e.season_number, e.episode_number
	`, id)
	if err != nil {
		return Show{}, nil, err
	}
	defer rows.Close()
	var episodes []Episode
	for rows.Next() {
		var ep Episode
		var height *int
		if err := rows.Scan(
			&ep.ID, &ep.ShowID, &ep.SeasonNumber, &ep.EpisodeNumber, &ep.Name, &ep.Overview,
			&ep.StillPath, &ep.AirDate, &ep.RuntimeMinutes, &ep.VoteAverage,
			&ep.MediaFileID, &ep.Container, &ep.ReleaseCount, &height, &ep.VideoCodec, &ep.AudioCodec,
		); err != nil {
			return Show{}, nil, err
		}
		if ep.ReleaseCount <= 1 {
			if height != nil {
				res := resolutionLabel(*height)
				ep.Resolution = &res
			}
		} else {
			ep.Resolution = nil
			ep.VideoCodec = nil
			ep.AudioCodec = nil
			ep.Container = nil
		}
		episodes = append(episodes, ep)
	}
	sh.EpisodeCount = len(episodes)
	return sh, episodes, rows.Err()
}

func (s *Store) Search(ctx context.Context, q string) (movies []Movie, shows []Show, err error) {
	pattern := "%" + q + "%"
	mrows, err := s.pool.Query(ctx, `
		SELECT DISTINCT m.id, m.tmdb_id, m.title, m.poster_path, m.vote_average,
		       to_char(m.release_date, 'YYYY-MM-DD'), m.overview
		FROM movies m
		JOIN media_items i ON i.movie_id = m.id
		JOIN media_files f ON f.id = i.media_file_id AND f.soft_deleted_at IS NULL
		WHERE m.title ILIKE $1 OR m.original_title ILIKE $1
		ORDER BY m.title LIMIT 50
	`, pattern)
	if err != nil {
		return nil, nil, err
	}
	defer mrows.Close()
	for mrows.Next() {
		var m Movie
		if err := mrows.Scan(&m.ID, &m.TMDBID, &m.Title, &m.PosterPath, &m.VoteAverage, &m.ReleaseDate, &m.Overview); err != nil {
			return nil, nil, err
		}
		movies = append(movies, m)
	}
	if err := mrows.Err(); err != nil {
		return nil, nil, err
	}

	srows, err := s.pool.Query(ctx, `
		SELECT DISTINCT s.id, s.tmdb_id, s.name, s.poster_path, s.vote_average,
		       to_char(s.first_air_date, 'YYYY-MM-DD'), s.overview
		FROM shows s
		JOIN episodes e ON e.show_id = s.id
		JOIN media_items i ON i.episode_id = e.id
		JOIN media_files f ON f.id = i.media_file_id AND f.soft_deleted_at IS NULL
		WHERE s.name ILIKE $1 OR s.original_name ILIKE $1
		ORDER BY s.name LIMIT 50
	`, pattern)
	if err != nil {
		return nil, nil, err
	}
	defer srows.Close()
	for srows.Next() {
		var sh Show
		if err := srows.Scan(&sh.ID, &sh.TMDBID, &sh.Name, &sh.PosterPath, &sh.VoteAverage, &sh.FirstAirDate, &sh.Overview); err != nil {
			return nil, nil, err
		}
		shows = append(shows, sh)
	}
	return movies, shows, srows.Err()
}

func (s *Store) MediaFileForItem(ctx context.Context, mediaFileID uuid.UUID) (MediaFile, error) {
	return s.MediaFileByID(ctx, mediaFileID)
}

func resolutionLabel(height int) string {
	switch {
	case height >= 2160:
		return "2160p"
	case height >= 1080:
		return "1080p"
	case height >= 720:
		return "720p"
	case height > 0:
		return "SD"
	default:
		return ""
	}
}
