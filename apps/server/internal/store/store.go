package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stevie-media/stevie/apps/server/internal/ffprobe"
	"github.com/stevie-media/stevie/apps/server/internal/parser"
)

var ErrNotFound = errors.New("not found")

type Store struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

func (s *Store) Pool() *pgxpool.Pool { return s.pool }

func (s *Store) EnsureAdmin(ctx context.Context, username, passwordHash string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO users (username, password_hash, is_admin)
		VALUES ($1, $2, TRUE)
		ON CONFLICT (username) DO UPDATE
		SET password_hash = EXCLUDED.password_hash, is_admin = TRUE
	`, username, passwordHash)
	return err
}

func (s *Store) UserByUsername(ctx context.Context, username string) (User, error) {
	var u User
	err := s.pool.QueryRow(ctx, `
		SELECT id, username, password_hash, is_admin FROM users WHERE username = $1
	`, username).Scan(&u.ID, &u.Username, &u.PasswordHash, &u.IsAdmin)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrNotFound
	}
	return u, err
}

func (s *Store) UserByID(ctx context.Context, id uuid.UUID) (User, error) {
	var u User
	err := s.pool.QueryRow(ctx, `
		SELECT id, username, password_hash, is_admin FROM users WHERE id = $1
	`, id).Scan(&u.ID, &u.Username, &u.PasswordHash, &u.IsAdmin)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrNotFound
	}
	return u, err
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func (s *Store) CreateSession(ctx context.Context, userID uuid.UUID, token string, expiresAt time.Time) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO sessions (user_id, token_hash, expires_at) VALUES ($1, $2, $3)
	`, userID, hashToken(token), expiresAt)
	return err
}

func (s *Store) SessionUser(ctx context.Context, token string) (User, error) {
	var u User
	err := s.pool.QueryRow(ctx, `
		SELECT u.id, u.username, u.password_hash, u.is_admin
		FROM sessions s
		JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = $1 AND s.expires_at > now()
	`, hashToken(token)).Scan(&u.ID, &u.Username, &u.PasswordHash, &u.IsAdmin)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrNotFound
	}
	return u, err
}

func (s *Store) DeleteSession(ctx context.Context, token string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE token_hash = $1`, hashToken(token))
	return err
}

func (s *Store) CreateLibrary(ctx context.Context, name, rootPath, libType string) (Library, error) {
	var lib Library
	err := s.pool.QueryRow(ctx, `
		INSERT INTO libraries (name, root_path, type)
		VALUES ($1, $2, $3)
		RETURNING id, name, root_path, type::text, created_at, updated_at
	`, name, rootPath, libType).Scan(&lib.ID, &lib.Name, &lib.RootPath, &lib.Type, &lib.CreatedAt, &lib.UpdatedAt)
	return lib, err
}

// EnsureDefaultLibrary keeps a single mixed library rooted at mountPath.
// Any other library rows are removed so Settings cannot drift from .env.
func (s *Store) EnsureDefaultLibrary(ctx context.Context, name, mountPath string) (Library, error) {
	if _, err := s.pool.Exec(ctx, `DELETE FROM libraries WHERE root_path <> $1`, mountPath); err != nil {
		return Library{}, fmt.Errorf("prune libraries: %w", err)
	}
	var lib Library
	err := s.pool.QueryRow(ctx, `
		INSERT INTO libraries (name, root_path, type)
		VALUES ($1, $2, 'mixed')
		ON CONFLICT (root_path) DO UPDATE SET
			name = EXCLUDED.name,
			type = 'mixed',
			updated_at = now()
		RETURNING id, name, root_path, type::text, created_at, updated_at
	`, name, mountPath).Scan(&lib.ID, &lib.Name, &lib.RootPath, &lib.Type, &lib.CreatedAt, &lib.UpdatedAt)
	return lib, err
}

func (s *Store) DeleteLibrary(ctx context.Context, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM libraries WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) ListLibraries(ctx context.Context) ([]Library, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, name, root_path, type::text, created_at, updated_at
		FROM libraries ORDER BY name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Library
	for rows.Next() {
		var lib Library
		if err := rows.Scan(&lib.ID, &lib.Name, &lib.RootPath, &lib.Type, &lib.CreatedAt, &lib.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, lib)
	}
	return out, rows.Err()
}

func (s *Store) LibraryByID(ctx context.Context, id uuid.UUID) (Library, error) {
	var lib Library
	err := s.pool.QueryRow(ctx, `
		SELECT id, name, root_path, type::text, created_at, updated_at
		FROM libraries WHERE id = $1
	`, id).Scan(&lib.ID, &lib.Name, &lib.RootPath, &lib.Type, &lib.CreatedAt, &lib.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Library{}, ErrNotFound
	}
	return lib, err
}

type UpsertFileInput struct {
	LibraryID uuid.UUID
	Path      string
	SizeBytes int64
	Mtime     time.Time
	Inode     int64
	Parsed    parser.Result
	Probe     ffprobe.Probe
}

func (s *Store) UpsertMediaFile(ctx context.Context, in UpsertFileInput) (uuid.UUID, error) {
	formatTags, _ := json.Marshal(in.Probe.FormatTags)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	defer tx.Rollback(ctx)

	var fileID uuid.UUID
	err = tx.QueryRow(ctx, `
		INSERT INTO media_files (
			library_id, path, size_bytes, mtime, inode, container, duration_ms, bitrate,
			format_name, format_tags, probe_json, parsed_title, parsed_year, parsed_season,
			parsed_episode, soft_deleted_at, updated_at
		) VALUES (
			$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,NULL,now()
		)
		ON CONFLICT (library_id, path) DO UPDATE SET
			size_bytes = EXCLUDED.size_bytes,
			mtime = EXCLUDED.mtime,
			inode = EXCLUDED.inode,
			container = EXCLUDED.container,
			duration_ms = EXCLUDED.duration_ms,
			bitrate = EXCLUDED.bitrate,
			format_name = EXCLUDED.format_name,
			format_tags = EXCLUDED.format_tags,
			probe_json = EXCLUDED.probe_json,
			parsed_title = EXCLUDED.parsed_title,
			parsed_year = EXCLUDED.parsed_year,
			parsed_season = EXCLUDED.parsed_season,
			parsed_episode = EXCLUDED.parsed_episode,
			soft_deleted_at = NULL,
			updated_at = now()
		RETURNING id
	`,
		in.LibraryID, in.Path, in.SizeBytes, in.Mtime, in.Inode,
		nullStr(in.Probe.Container), nullInt64(in.Probe.DurationMS), nullInt64(in.Probe.BitRate),
		nullStr(in.Probe.FormatName), formatTags, in.Probe.Raw,
		nullStr(in.Parsed.Title), nullInt(in.Parsed.Year), nullInt(in.Parsed.Season), nullInt(in.Parsed.Episode),
	).Scan(&fileID)
	if err != nil {
		return uuid.Nil, fmt.Errorf("upsert media_files: %w", err)
	}

	if _, err := tx.Exec(ctx, `DELETE FROM media_streams WHERE media_file_id = $1`, fileID); err != nil {
		return uuid.Nil, err
	}

	for _, st := range in.Probe.Streams {
		raw := st.Raw
		if len(raw) == 0 {
			raw = json.RawMessage(`{}`)
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO media_streams (
				media_file_id, stream_index, codec_type, codec_name, profile, width, height,
				pix_fmt, fps, bit_rate, channels, channel_layout, language, title,
				disposition_default, disposition_forced, disposition_hearing_impaired,
				color_range, color_space, color_transfer, bit_depth, raw
			) VALUES (
				$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22
			)
		`,
			fileID, st.Index, st.CodecType, nullStr(st.CodecName), nullStr(st.Profile),
			nullInt(st.Width), nullInt(st.Height), nullStr(st.PixFmt), nullStr(st.FPS),
			nullInt64(st.BitRate), nullInt(st.Channels), nullStr(st.ChannelLayout),
			nullStr(st.Language), nullStr(st.Title),
			st.DispositionDefault, st.DispositionForced, st.DispositionHearingImpaired,
			nullStr(st.ColorRange), nullStr(st.ColorSpace), nullStr(st.ColorTransfer),
			nullInt(st.BitDepth), raw,
		)
		if err != nil {
			return uuid.Nil, fmt.Errorf("insert stream: %w", err)
		}
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO media_items (media_file_id, match_status)
		VALUES ($1, 'unmatched')
		ON CONFLICT (media_file_id) DO NOTHING
	`, fileID)
	if err != nil {
		return uuid.Nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return uuid.Nil, err
	}
	return fileID, nil
}

func (s *Store) SoftDeleteMissing(ctx context.Context, libraryID uuid.UUID, seenPaths []string) error {
	if len(seenPaths) == 0 {
		_, err := s.pool.Exec(ctx, `
			UPDATE media_files SET soft_deleted_at = now(), updated_at = now()
			WHERE library_id = $1 AND soft_deleted_at IS NULL
		`, libraryID)
		return err
	}
	_, err := s.pool.Exec(ctx, `
		UPDATE media_files SET soft_deleted_at = now(), updated_at = now()
		WHERE library_id = $1 AND soft_deleted_at IS NULL AND NOT (path = ANY($2))
	`, libraryID, seenPaths)
	return err
}

func (s *Store) UnmatchedFiles(ctx context.Context, libraryID uuid.UUID) ([]MediaFile, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT f.id, f.library_id, f.path, f.size_bytes, f.parsed_title, f.parsed_year,
		       f.parsed_season, f.parsed_episode
		FROM media_files f
		JOIN media_items i ON i.media_file_id = f.id
		WHERE f.library_id = $1 AND f.soft_deleted_at IS NULL
		  AND i.match_status = 'unmatched'
	`, libraryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MediaFile
	for rows.Next() {
		var f MediaFile
		if err := rows.Scan(&f.ID, &f.LibraryID, &f.Path, &f.SizeBytes, &f.ParsedTitle, &f.ParsedYear, &f.ParsedSeason, &f.ParsedEpisode); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func (s *Store) MediaFileByID(ctx context.Context, id uuid.UUID) (MediaFile, error) {
	var f MediaFile
	var formatTags, probe []byte
	err := s.pool.QueryRow(ctx, `
		SELECT id, library_id, path, size_bytes, mtime, inode, container, duration_ms, bitrate,
		       format_name, format_tags, probe_json, parsed_title, parsed_year, parsed_season,
		       parsed_episode, soft_deleted_at, created_at, updated_at
		FROM media_files WHERE id = $1
	`, id).Scan(
		&f.ID, &f.LibraryID, &f.Path, &f.SizeBytes, &f.Mtime, &f.Inode, &f.Container, &f.DurationMS,
		&f.Bitrate, &f.FormatName, &formatTags, &probe, &f.ParsedTitle, &f.ParsedYear,
		&f.ParsedSeason, &f.ParsedEpisode, &f.SoftDeletedAt, &f.CreatedAt, &f.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return MediaFile{}, ErrNotFound
	}
	if err != nil {
		return MediaFile{}, err
	}
	f.FormatTags = formatTags
	f.ProbeJSON = probe

	rows, err := s.pool.Query(ctx, `
		SELECT id, media_file_id, stream_index, codec_type, codec_name, profile, width, height,
		       pix_fmt, fps, bit_rate, channels, channel_layout, language, title,
		       disposition_default, disposition_forced, disposition_hearing_impaired,
		       color_range, color_space, color_transfer, bit_depth, raw
		FROM media_streams WHERE media_file_id = $1 ORDER BY stream_index
	`, id)
	if err != nil {
		return MediaFile{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var st MediaStream
		var raw []byte
		if err := rows.Scan(
			&st.ID, &st.MediaFileID, &st.StreamIndex, &st.CodecType, &st.CodecName, &st.Profile,
			&st.Width, &st.Height, &st.PixFmt, &st.FPS, &st.BitRate, &st.Channels, &st.ChannelLayout,
			&st.Language, &st.Title, &st.DispositionDefault, &st.DispositionForced,
			&st.DispositionHearingImpaired, &st.ColorRange, &st.ColorSpace, &st.ColorTransfer,
			&st.BitDepth, &raw,
		); err != nil {
			return MediaFile{}, err
		}
		st.Raw = raw
		f.Streams = append(f.Streams, st)
	}
	return f, rows.Err()
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullInt(n int) any {
	if n == 0 {
		return nil
	}
	return n
}

func nullInt64(n int64) any {
	if n == 0 {
		return nil
	}
	return n
}
