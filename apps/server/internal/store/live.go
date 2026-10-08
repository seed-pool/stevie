package store

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stevie-media/stevie/apps/server/internal/livetv"
)

const (
	SettingLiveM3U                 = "live_m3u_source"
	SettingLiveXMLTV               = "live_xmltv_source"
	SettingLiveImportedCategoryIDs = "live_imported_category_ids"
	// FavoritesGroup is the synthetic category filter for favorited channels.
	FavoritesGroup = "__favorites__"
)

type LiveChannel struct {
	ID           uuid.UUID  `json:"id"`
	TVGID        string     `json:"tvg_id"`
	Name         string     `json:"name"`
	GroupTitle   string     `json:"group_title"`
	LogoURL      string     `json:"logo_url"`
	StreamURL    string     `json:"-"`
	SortOrder    int        `json:"sort_order"`
	Source       string     `json:"source"`
	ExternalID   string     `json:"external_id"`
	CategoryID   *uuid.UUID `json:"category_id,omitempty"`
	EPGChannelID string     `json:"epg_channel_id"`
	Num          int        `json:"num"`
	Favorite     bool       `json:"favorite"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

type LiveProgram struct {
	ID           uuid.UUID `json:"id"`
	ChannelTVGID string    `json:"channel_tvg_id"`
	Title        string    `json:"title"`
	Description  string    `json:"description"`
	StartTime    time.Time `json:"start_time"`
	EndTime      time.Time `json:"end_time"`
	Category     string    `json:"category"`
}

type LiveChannelGuide struct {
	LiveChannel
	Programs []LiveProgram `json:"programs"`
}

type LiveCategory struct {
	ID           uuid.UUID `json:"id"`
	Source       string    `json:"source"`
	ExternalID   string    `json:"external_id"`
	Name         string    `json:"name"`
	Imported     bool      `json:"imported"`
	ChannelCount int       `json:"channel_count"`
	SortOrder    int       `json:"sort_order"`
}

func (s *Store) GetSetting(ctx context.Context, key string) (string, error) {
	var value string
	err := s.pool.QueryRow(ctx, `SELECT value FROM app_settings WHERE key = $1`, key).Scan(&value)
	if err == pgx.ErrNoRows {
		return "", nil
	}
	return value, err
}

func (s *Store) SetSetting(ctx context.Context, key, value string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO app_settings (key, value, updated_at)
		VALUES ($1, $2, now())
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now()
	`, key, value)
	return err
}

func (s *Store) LiveSettings(ctx context.Context) (m3u, xmltv string, err error) {
	m3u, err = s.GetSetting(ctx, SettingLiveM3U)
	if err != nil {
		return "", "", err
	}
	xmltv, err = s.GetSetting(ctx, SettingLiveXMLTV)
	return m3u, xmltv, err
}

func (s *Store) ImportedXtreamCategoryIDs(ctx context.Context) ([]string, error) {
	raw, err := s.GetSetting(ctx, SettingLiveImportedCategoryIDs)
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

func (s *Store) SetImportedXtreamCategoryIDs(ctx context.Context, ids []string) error {
	if ids == nil {
		ids = []string{}
	}
	b, err := json.Marshal(ids)
	if err != nil {
		return err
	}
	return s.SetSetting(ctx, SettingLiveImportedCategoryIDs, string(b))
}

// ReplaceM3UChannels replaces only source=m3u rows and syncs m3u category rows.
func (s *Store) ReplaceM3UChannels(ctx context.Context, channels []livetv.Channel) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `DELETE FROM live_channels WHERE source = $1`, livetv.SourceM3U); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM live_categories WHERE source = $1`, livetv.SourceM3U); err != nil {
		return err
	}

	// Group titles → categories
	type catKey struct {
		ext  string
		name string
	}
	catOrder := []catKey{}
	catSeen := map[string]struct{}{}
	counts := map[string]int{}
	for _, ch := range channels {
		ext := ch.GroupTitle
		if ext == "" {
			ext = "_ungrouped"
		}
		counts[ext]++
		if _, ok := catSeen[ext]; ok {
			continue
		}
		catSeen[ext] = struct{}{}
		name := ch.GroupTitle
		if name == "" {
			name = "Ungrouped"
		}
		catOrder = append(catOrder, catKey{ext: ext, name: name})
	}
	catIDs := map[string]uuid.UUID{}
	for i, c := range catOrder {
		id := uuid.New()
		_, err := tx.Exec(ctx, `
			INSERT INTO live_categories (id, source, external_id, name, imported, channel_count, sort_order)
			VALUES ($1, $2, $3, $4, true, $5, $6)
		`, id, livetv.SourceM3U, c.ext, c.name, counts[c.ext], i)
		if err != nil {
			return err
		}
		catIDs[c.ext] = id
	}

	const chunk = 500
	for i := 0; i < len(channels); i += chunk {
		end := i + chunk
		if end > len(channels) {
			end = len(channels)
		}
		batch := &pgx.Batch{}
		for _, ch := range channels[i:end] {
			ext := ch.GroupTitle
			if ext == "" {
				ext = "_ungrouped"
			}
			catID := catIDs[ext]
			epg := ch.EPGChannelID
			if epg == "" {
				epg = ch.TVGID
			}
			src := ch.Source
			if src == "" {
				src = livetv.SourceM3U
			}
			extID := ch.ExternalID
			if extID == "" {
				extID = uuid.New().String()
			}
			batch.Queue(`
				INSERT INTO live_channels (
					tvg_id, name, group_title, logo_url, stream_url, sort_order,
					source, external_id, category_id, epg_channel_id, num
				) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
			`, ch.TVGID, ch.Name, ch.GroupTitle, ch.LogoURL, ch.StreamURL, ch.SortOrder,
				src, extID, catID, epg, ch.Num)
		}
		br := tx.SendBatch(ctx, batch)
		if err := br.Close(); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// UpsertXtreamCategories upserts panel categories and marks imported ones.
func (s *Store) UpsertXtreamCategories(ctx context.Context, cats []livetv.CategoryInfo, importedIDs []string) error {
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
		UPDATE live_categories SET imported = false, updated_at = now()
		WHERE source = $1
	`, livetv.SourceXtream); err != nil {
		return err
	}

	for i, c := range cats {
		_, ok := imported[c.ExternalID]
		sortOrder := c.SortOrder
		if sortOrder == 0 {
			sortOrder = i
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO live_categories (source, external_id, name, imported, channel_count, sort_order)
			VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (source, external_id) DO UPDATE SET
				name = EXCLUDED.name,
				imported = EXCLUDED.imported,
				channel_count = EXCLUDED.channel_count,
				sort_order = EXCLUDED.sort_order,
				updated_at = now()
		`, livetv.SourceXtream, c.ExternalID, c.Name, ok, c.ChannelCount, sortOrder)
		if err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// CategoryIDByExternal returns category UUID for a source+external_id.
func (s *Store) CategoryIDByExternal(ctx context.Context, source, externalID string) (uuid.UUID, error) {
	var id uuid.UUID
	err := s.pool.QueryRow(ctx, `
		SELECT id FROM live_categories WHERE source = $1 AND external_id = $2
	`, source, externalID).Scan(&id)
	if err == pgx.ErrNoRows {
		return uuid.Nil, ErrNotFound
	}
	return id, err
}

// UpsertXtreamChannels upserts selected streams and removes deselected Xtream channels.
func (s *Store) UpsertXtreamChannels(ctx context.Context, channels []livetv.Channel, keepCategoryExtIDs []string) (int, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	catMap := map[string]uuid.UUID{}
	rows, err := tx.Query(ctx, `
		SELECT id, external_id FROM live_categories WHERE source = $1
	`, livetv.SourceXtream)
	if err != nil {
		return 0, err
	}
	for rows.Next() {
		var id uuid.UUID
		var ext string
		if err := rows.Scan(&id, &ext); err != nil {
			rows.Close()
			return 0, err
		}
		catMap[ext] = id
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	keepSet := map[string]struct{}{}
	for _, id := range keepCategoryExtIDs {
		keepSet[id] = struct{}{}
	}

	// Drop Xtream channels whose category is no longer imported.
	if len(keepCategoryExtIDs) == 0 {
		if _, err := tx.Exec(ctx, `DELETE FROM live_channels WHERE source = $1`, livetv.SourceXtream); err != nil {
			return 0, err
		}
	} else {
		if _, err := tx.Exec(ctx, `
			DELETE FROM live_channels
			WHERE source = $1
			  AND (
				category_id IS NULL
				OR category_id NOT IN (
					SELECT id FROM live_categories
					WHERE source = $1 AND external_id = ANY($2)
				)
			)
		`, livetv.SourceXtream, keepCategoryExtIDs); err != nil {
			return 0, err
		}
	}

	written := 0
	const chunk = 400
	for i := 0; i < len(channels); i += chunk {
		end := i + chunk
		if end > len(channels) {
			end = len(channels)
		}
		batch := &pgx.Batch{}
		for _, ch := range channels[i:end] {
			if _, ok := keepSet[ch.CategoryExternalID]; !ok && len(keepSet) > 0 {
				continue
			}
			var catPtr *uuid.UUID
			if id, ok := catMap[ch.CategoryExternalID]; ok {
				catPtr = &id
			}
			epg := ch.EPGChannelID
			if epg == "" {
				epg = ch.TVGID
			}
			batch.Queue(`
				INSERT INTO live_channels (
					tvg_id, name, group_title, logo_url, stream_url, sort_order,
					source, external_id, category_id, epg_channel_id, num
				) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
				ON CONFLICT (source, external_id) DO UPDATE SET
					tvg_id = EXCLUDED.tvg_id,
					name = EXCLUDED.name,
					group_title = EXCLUDED.group_title,
					logo_url = EXCLUDED.logo_url,
					stream_url = EXCLUDED.stream_url,
					sort_order = EXCLUDED.sort_order,
					category_id = EXCLUDED.category_id,
					epg_channel_id = EXCLUDED.epg_channel_id,
					num = EXCLUDED.num,
					updated_at = now()
			`, ch.TVGID, ch.Name, ch.GroupTitle, ch.LogoURL, ch.StreamURL, ch.SortOrder,
				livetv.SourceXtream, ch.ExternalID, catPtr, epg, ch.Num)
			written++
		}
		br := tx.SendBatch(ctx, batch)
		if err := br.Close(); err != nil {
			return 0, err
		}
	}

	// Refresh channel_count on xtream categories.
	if _, err := tx.Exec(ctx, `
		UPDATE live_categories c SET channel_count = COALESCE(sub.n, 0), updated_at = now()
		FROM (
			SELECT category_id, COUNT(*) AS n
			FROM live_channels
			WHERE source = $1 AND category_id IS NOT NULL
			GROUP BY category_id
		) sub
		WHERE c.id = sub.category_id AND c.source = $1
	`, livetv.SourceXtream); err != nil {
		return 0, err
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return written, nil
}

// ReplaceLivePrograms is a full wipe+insert (used only when clearing).
func (s *Store) ReplaceLivePrograms(ctx context.Context, programs []livetv.Program) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `DELETE FROM live_programs`); err != nil {
		return err
	}
	if err := insertProgramBatch(ctx, tx, programs); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// MergeLivePrograms deletes programmes for the given channel keys then inserts
// the provided set. usedChannelKeys is the set of tvg/epg ids being rewritten.
func (s *Store) MergeLivePrograms(ctx context.Context, programs []livetv.Program, usedChannelKeys []string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if len(usedChannelKeys) > 0 {
		if _, err := tx.Exec(ctx, `
			DELETE FROM live_programs WHERE channel_tvg_id = ANY($1)
		`, usedChannelKeys); err != nil {
			return err
		}
	}
	if err := insertProgramBatch(ctx, tx, programs); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// AppendLivePrograms inserts programmes without deleting (for override merge in a second pass).
func (s *Store) AppendLivePrograms(ctx context.Context, programs []livetv.Program) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := insertProgramBatch(ctx, tx, programs); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// DeleteProgramsForChannels removes programmes matching any of the keys.
func (s *Store) DeleteProgramsForChannels(ctx context.Context, keys []string) error {
	if len(keys) == 0 {
		return nil
	}
	_, err := s.pool.Exec(ctx, `DELETE FROM live_programs WHERE channel_tvg_id = ANY($1)`, keys)
	return err
}

// UpsertProgramsOverride deletes overlapping (channel, start) then inserts.
func (s *Store) UpsertProgramsOverride(ctx context.Context, programs []livetv.Program) error {
	if len(programs) == 0 {
		return nil
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	const chunk = 500
	for i := 0; i < len(programs); i += chunk {
		end := i + chunk
		if end > len(programs) {
			end = len(programs)
		}
		batch := &pgx.Batch{}
		for _, p := range programs[i:end] {
			batch.Queue(`
				DELETE FROM live_programs
				WHERE channel_tvg_id = $1 AND start_time = $2
			`, p.ChannelTVGID, p.Start)
			batch.Queue(`
				INSERT INTO live_programs (channel_tvg_id, title, description, start_time, end_time, category)
				VALUES ($1, $2, $3, $4, $5, $6)
			`, p.ChannelTVGID, p.Title, p.Description, p.Start, p.End, p.Category)
		}
		br := tx.SendBatch(ctx, batch)
		if err := br.Close(); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func insertProgramBatch(ctx context.Context, tx pgx.Tx, programs []livetv.Program) error {
	const chunk = 500
	for i := 0; i < len(programs); i += chunk {
		end := i + chunk
		if end > len(programs) {
			end = len(programs)
		}
		batch := &pgx.Batch{}
		for _, p := range programs[i:end] {
			batch.Queue(`
				INSERT INTO live_programs (channel_tvg_id, title, description, start_time, end_time, category)
				VALUES ($1, $2, $3, $4, $5, $6)
			`, p.ChannelTVGID, p.Title, p.Description, p.Start, p.End, p.Category)
		}
		br := tx.SendBatch(ctx, batch)
		if err := br.Close(); err != nil {
			return err
		}
	}
	return nil
}

// EPGChannelKeys returns distinct tvg_id and epg_channel_id values for filtering XMLTV.
func (s *Store) EPGChannelKeys(ctx context.Context) (map[string]struct{}, []string, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT DISTINCT key FROM (
			SELECT NULLIF(tvg_id, '') AS key FROM live_channels
			UNION
			SELECT NULLIF(epg_channel_id, '') FROM live_channels
		) t WHERE key IS NOT NULL
	`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	allowed := map[string]struct{}{}
	list := []string{}
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, nil, err
		}
		if _, ok := allowed[k]; ok {
			continue
		}
		allowed[k] = struct{}{}
		list = append(list, k)
	}
	return allowed, list, rows.Err()
}

type LiveListOpts struct {
	Group         string
	Query         string // channel name / tvg_id search
	ProgramQuery  string // programme title / description search
	Limit         int
	Offset        int
	Source        string
	From          time.Time
	To            time.Time
	FavoritesOnly bool
}

func scanLiveChannel(rows pgx.Rows) (LiveChannel, error) {
	var ch LiveChannel
	var catID *uuid.UUID
	err := rows.Scan(
		&ch.ID, &ch.TVGID, &ch.Name, &ch.GroupTitle, &ch.LogoURL, &ch.StreamURL,
		&ch.SortOrder, &ch.Source, &ch.ExternalID, &catID, &ch.EPGChannelID, &ch.Num,
		&ch.Favorite, &ch.CreatedAt, &ch.UpdatedAt,
	)
	ch.CategoryID = catID
	return ch, err
}

const liveChannelSelect = `
	SELECT c.id, c.tvg_id, c.name, c.group_title, c.logo_url, c.stream_url, c.sort_order,
	       c.source, c.external_id, c.category_id, c.epg_channel_id, c.num,
	       (f.source IS NOT NULL) AS favorite,
	       c.created_at, c.updated_at
	FROM live_channels c
	LEFT JOIN live_favorites f
	  ON f.source = c.source AND f.external_id = c.external_id
`

func (s *Store) ListLiveChannels(ctx context.Context, opts LiveListOpts) ([]LiveChannel, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 2000 {
		limit = 2000
	}
	offset := opts.Offset
	if offset < 0 {
		offset = 0
	}
	group := opts.Group
	favOnly := opts.FavoritesOnly || group == FavoritesGroup
	if favOnly {
		group = ""
	}
	pq := strings.TrimSpace(opts.ProgramQuery)
	if pq != "" {
		from, to := opts.From, opts.To
		if from.IsZero() {
			from = time.Now().Add(-30 * time.Minute)
		}
		if to.IsZero() {
			to = time.Now().Add(6 * time.Hour)
		}
		rows, err := s.pool.Query(ctx, `
			SELECT DISTINCT c.id, c.tvg_id, c.name, c.group_title, c.logo_url, c.stream_url, c.sort_order,
			       c.source, c.external_id, c.category_id, c.epg_channel_id, c.num,
			       (f.source IS NOT NULL) AS favorite,
			       c.created_at, c.updated_at
			FROM live_channels c
			LEFT JOIN live_favorites f
			  ON f.source = c.source AND f.external_id = c.external_id
			INNER JOIN live_programs p
			  ON p.channel_tvg_id = c.tvg_id
			  OR (c.epg_channel_id <> '' AND p.channel_tvg_id = c.epg_channel_id)
			WHERE ($1 = '' OR c.group_title = $1)
			  AND ($2 = '' OR c.name ILIKE '%' || $2 || '%' OR c.tvg_id ILIKE '%' || $2 || '%' OR c.group_title ILIKE '%' || $2 || '%')
			  AND ($3 = '' OR c.source = $3)
			  AND (NOT $8 OR f.source IS NOT NULL)
			  AND (p.title ILIKE '%' || $4 || '%' OR p.description ILIKE '%' || $4 || '%')
			  AND p.start_time < $6
			  AND p.end_time > $5
			ORDER BY c.sort_order, c.name
			LIMIT $7 OFFSET $9
		`, group, opts.Query, opts.Source, pq, from, to, limit, favOnly, offset)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var out []LiveChannel
		for rows.Next() {
			ch, err := scanLiveChannel(rows)
			if err != nil {
				return nil, err
			}
			out = append(out, ch)
		}
		return out, rows.Err()
	}
	rows, err := s.pool.Query(ctx, liveChannelSelect+`
		WHERE ($1 = '' OR c.group_title = $1)
		  AND ($2 = '' OR c.name ILIKE '%' || $2 || '%' OR c.tvg_id ILIKE '%' || $2 || '%' OR c.group_title ILIKE '%' || $2 || '%')
		  AND ($3 = '' OR c.source = $3)
		  AND (NOT $5 OR f.source IS NOT NULL)
		ORDER BY CASE WHEN f.source IS NOT NULL THEN 0 ELSE 1 END, c.sort_order, c.name
		LIMIT $4 OFFSET $6
	`, group, opts.Query, opts.Source, limit, favOnly, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LiveChannel
	for rows.Next() {
		ch, err := scanLiveChannel(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, ch)
	}
	return out, rows.Err()
}

func (s *Store) LiveChannelByID(ctx context.Context, id uuid.UUID) (LiveChannel, error) {
	row := s.pool.QueryRow(ctx, liveChannelSelect+` WHERE c.id = $1`, id)
	var ch LiveChannel
	var catID *uuid.UUID
	err := row.Scan(
		&ch.ID, &ch.TVGID, &ch.Name, &ch.GroupTitle, &ch.LogoURL, &ch.StreamURL,
		&ch.SortOrder, &ch.Source, &ch.ExternalID, &catID, &ch.EPGChannelID, &ch.Num,
		&ch.Favorite, &ch.CreatedAt, &ch.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		return LiveChannel{}, ErrNotFound
	}
	ch.CategoryID = catID
	return ch, err
}

// LiveChannelLogoIndex maps normalized channel names → logo URLs.
func (s *Store) LiveChannelLogoIndex(ctx context.Context) (map[string]string, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT name, COALESCE(logo_url, '') FROM live_channels WHERE COALESCE(logo_url, '') <> ''
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var name, logo string
		if err := rows.Scan(&name, &logo); err != nil {
			return nil, err
		}
		out[NormalizeChannelKey(name)] = logo
	}
	return out, rows.Err()
}

// NormalizeChannelKey collapses punctuation/spacing so filename-derived names match DB names.
func NormalizeChannelKey(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func (s *Store) SetLiveFavorite(ctx context.Context, channelID uuid.UUID, favorite bool) (LiveChannel, error) {
	ch, err := s.LiveChannelByID(ctx, channelID)
	if err != nil {
		return LiveChannel{}, err
	}
	if favorite {
		_, err = s.pool.Exec(ctx, `
			INSERT INTO live_favorites (source, external_id)
			VALUES ($1, $2)
			ON CONFLICT (source, external_id) DO NOTHING
		`, ch.Source, ch.ExternalID)
	} else {
		_, err = s.pool.Exec(ctx, `
			DELETE FROM live_favorites WHERE source = $1 AND external_id = $2
		`, ch.Source, ch.ExternalID)
	}
	if err != nil {
		return LiveChannel{}, err
	}
	ch.Favorite = favorite
	return ch, nil
}

func (s *Store) CountLiveFavorites(ctx context.Context) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM live_channels c
		INNER JOIN live_favorites f
		  ON f.source = c.source AND f.external_id = c.external_id
	`).Scan(&n)
	return n, err
}

// LiveGroups returns distinct group titles (legacy string list).
func (s *Store) LiveGroups(ctx context.Context) ([]string, error) {
	cats, err := s.ListLiveCategories(ctx, true)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(cats))
	seen := map[string]struct{}{}
	for _, c := range cats {
		if _, ok := seen[c.Name]; ok {
			continue
		}
		// Prefer group_title matching category name for filter compatibility.
		seen[c.Name] = struct{}{}
		out = append(out, c.Name)
	}
	// Also include any group_title not represented (edge cases).
	rows, err := s.pool.Query(ctx, `
		SELECT group_title, COUNT(*) FROM live_channels
		GROUP BY group_title
		ORDER BY group_title
	`)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var g string
		var n int
		if err := rows.Scan(&g, &n); err != nil {
			return nil, err
		}
		if _, ok := seen[g]; ok {
			continue
		}
		seen[g] = struct{}{}
		out = append(out, g)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	livetv.SortNamed(out)
	return out, nil
}

func (s *Store) ListLiveCategories(ctx context.Context, importedOnly bool) ([]LiveCategory, error) {
	q := `
		SELECT id, source, external_id, name, imported, channel_count, sort_order
		FROM live_categories
	`
	if importedOnly {
		q += ` WHERE imported = true OR source = 'm3u'`
	}
	q += ` ORDER BY name`
	rows, err := s.pool.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LiveCategory
	for rows.Next() {
		var c LiveCategory
		if err := rows.Scan(&c.ID, &c.Source, &c.ExternalID, &c.Name, &c.Imported, &c.ChannelCount, &c.SortOrder); err != nil {
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

func (s *Store) CountLiveChannels(ctx context.Context) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM live_channels`).Scan(&n)
	return n, err
}

func (s *Store) CountLivePrograms(ctx context.Context) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM live_programs`).Scan(&n)
	return n, err
}

// CurrentLiveProgram returns the programme airing now for a channel, if any.
func (s *Store) CurrentLiveProgram(ctx context.Context, ch LiveChannel) (*LiveProgram, error) {
	keys := make([]string, 0, 2)
	if k := strings.TrimSpace(ch.TVGID); k != "" {
		keys = append(keys, k)
	}
	if k := strings.TrimSpace(ch.EPGChannelID); k != "" && (len(keys) == 0 || k != keys[0]) {
		keys = append(keys, k)
	}
	if len(keys) == 0 {
		return nil, nil
	}
	now := time.Now()
	row := s.pool.QueryRow(ctx, `
		SELECT id, channel_tvg_id, title, description, start_time, end_time, category
		FROM live_programs
		WHERE channel_tvg_id = ANY($1)
		  AND start_time <= $2
		  AND end_time > $2
		ORDER BY start_time DESC
		LIMIT 1
	`, keys, now)
	var p LiveProgram
	err := row.Scan(&p.ID, &p.ChannelTVGID, &p.Title, &p.Description, &p.StartTime, &p.EndTime, &p.Category)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (s *Store) CountXtreamChannels(ctx context.Context) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM live_channels WHERE source = $1`, livetv.SourceXtream).Scan(&n)
	return n, err
}

func (s *Store) LiveGuide(ctx context.Context, from, to time.Time, group string, limit int) ([]LiveChannelGuide, error) {
	opts := LiveListOpts{Group: group, Limit: limit}
	if group == FavoritesGroup {
		opts.FavoritesOnly = true
	}
	return s.LiveGuideOpts(ctx, from, to, opts)
}

func (s *Store) CountLiveChannelsOpts(ctx context.Context, opts LiveListOpts) (int, error) {
	group := opts.Group
	favOnly := opts.FavoritesOnly || group == FavoritesGroup
	if favOnly {
		group = ""
	}
	pq := strings.TrimSpace(opts.ProgramQuery)
	if pq != "" {
		from, to := opts.From, opts.To
		if from.IsZero() {
			from = time.Now().Add(-30 * time.Minute)
		}
		if to.IsZero() {
			to = time.Now().Add(6 * time.Hour)
		}
		var n int
		err := s.pool.QueryRow(ctx, `
			SELECT COUNT(DISTINCT c.id)
			FROM live_channels c
			LEFT JOIN live_favorites f
			  ON f.source = c.source AND f.external_id = c.external_id
			INNER JOIN live_programs p
			  ON p.channel_tvg_id = c.tvg_id
			  OR (c.epg_channel_id <> '' AND p.channel_tvg_id = c.epg_channel_id)
			WHERE ($1 = '' OR c.group_title = $1)
			  AND ($2 = '' OR c.name ILIKE '%' || $2 || '%' OR c.tvg_id ILIKE '%' || $2 || '%' OR c.group_title ILIKE '%' || $2 || '%')
			  AND ($3 = '' OR c.source = $3)
			  AND (NOT $7 OR f.source IS NOT NULL)
			  AND (p.title ILIKE '%' || $4 || '%' OR p.description ILIKE '%' || $4 || '%')
			  AND p.start_time < $6
			  AND p.end_time > $5
		`, group, opts.Query, opts.Source, pq, from, to, favOnly).Scan(&n)
		return n, err
	}
	var n int
	err := s.pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM live_channels c
		LEFT JOIN live_favorites f
		  ON f.source = c.source AND f.external_id = c.external_id
		WHERE ($1 = '' OR c.group_title = $1)
		  AND ($2 = '' OR c.name ILIKE '%' || $2 || '%' OR c.tvg_id ILIKE '%' || $2 || '%' OR c.group_title ILIKE '%' || $2 || '%')
		  AND ($3 = '' OR c.source = $3)
		  AND (NOT $4 OR f.source IS NOT NULL)
	`, group, opts.Query, opts.Source, favOnly).Scan(&n)
	return n, err
}

func (s *Store) LiveGuideOpts(ctx context.Context, from, to time.Time, opts LiveListOpts) ([]LiveChannelGuide, error) {
	if opts.Limit <= 0 {
		opts.Limit = 100
	}
	if opts.Offset < 0 {
		opts.Offset = 0
	}
	opts.From = from
	opts.To = to
	if opts.Group == FavoritesGroup {
		opts.FavoritesOnly = true
	}
	channels, err := s.ListLiveChannels(ctx, opts)
	if err != nil {
		return nil, err
	}
	if len(channels) == 0 {
		return []LiveChannelGuide{}, nil
	}

	keys := make([]string, 0, len(channels)*2)
	seen := map[string]struct{}{}
	add := func(k string) {
		if k == "" {
			return
		}
		if _, ok := seen[k]; ok {
			return
		}
		seen[k] = struct{}{}
		keys = append(keys, k)
	}
	for _, ch := range channels {
		add(ch.TVGID)
		add(ch.EPGChannelID)
	}

	byKey := map[string][]LiveProgram{}
	if len(keys) > 0 {
		rows, err := s.pool.Query(ctx, `
			SELECT id, channel_tvg_id, title, description, start_time, end_time, category
			FROM live_programs
			WHERE channel_tvg_id = ANY($1)
			  AND start_time < $3
			  AND end_time > $2
			ORDER BY start_time
		`, keys, from, to)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var p LiveProgram
			if err := rows.Scan(&p.ID, &p.ChannelTVGID, &p.Title, &p.Description, &p.StartTime, &p.EndTime, &p.Category); err != nil {
				return nil, err
			}
			byKey[p.ChannelTVGID] = append(byKey[p.ChannelTVGID], p)
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}

	out := make([]LiveChannelGuide, 0, len(channels))
	for _, ch := range channels {
		progs := byKey[ch.TVGID]
		if len(progs) == 0 && ch.EPGChannelID != "" && ch.EPGChannelID != ch.TVGID {
			progs = byKey[ch.EPGChannelID]
		}
		if progs == nil {
			progs = []LiveProgram{}
		}
		out = append(out, LiveChannelGuide{LiveChannel: ch, Programs: progs})
	}
	return out, nil
}

func (s *Store) ReplaceLiveChannels(ctx context.Context, channels []livetv.Channel) error {
	return s.ReplaceM3UChannels(ctx, channels)
}
