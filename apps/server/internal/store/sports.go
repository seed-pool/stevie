package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type SportsTeam struct {
	ID              uuid.UUID `json:"id"`
	ExternalID      string    `json:"external_id"`
	Sport           string    `json:"sport"`
	League          string    `json:"league"`
	Name            string    `json:"name"`
	NameAlternates  string    `json:"name_alternates"`
	BadgeURL        string    `json:"badge_url"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type SportsEvent struct {
	ID             uuid.UUID  `json:"id"`
	ExternalID     string     `json:"external_id"`
	Sport          string     `json:"sport"`
	Title          string     `json:"title"`
	HomeTeam       string     `json:"home_team"`
	AwayTeam       string     `json:"away_team"`
	HomeTeamID     *uuid.UUID `json:"home_team_id,omitempty"`
	AwayTeamID     *uuid.UUID `json:"away_team_id,omitempty"`
	StartsAt       time.Time  `json:"starts_at"`
	EndsAt         *time.Time `json:"ends_at,omitempty"`
	Source         string     `json:"source"`
	HomeScore      string     `json:"home_score"`
	AwayScore      string     `json:"away_score"`
	Period         string     `json:"period"`
	Status         string     `json:"status"`
	ScoreUpdatedAt *time.Time `json:"score_updated_at,omitempty"`
	HomeBadgeURL   string     `json:"home_badge_url,omitempty"`
	AwayBadgeURL   string     `json:"away_badge_url,omitempty"`
	// League from linked team rows (home preferred), e.g. NHL / English Premier League.
	League         string     `json:"league,omitempty"`
	Channels       []SportsEventChannel `json:"channels,omitempty"`
}

type SportsEventChannel struct {
	ID             uuid.UUID  `json:"id"`
	EventID        uuid.UUID  `json:"event_id"`
	BroadcastLabel string     `json:"broadcast_label"`
	LiveChannelID  *uuid.UUID `json:"live_channel_id,omitempty"`
	MatchScore     float64    `json:"match_score"`
	ChannelName    string     `json:"channel_name,omitempty"`
	ChannelLogoURL string     `json:"channel_logo_url,omitempty"`
	Playable       bool       `json:"playable"`
}

type SportsSportMeta struct {
	Sport string `json:"sport"`
	Count int    `json:"count"`
}

type SportsListOpts struct {
	Sport     string
	Favorites []string
	From      time.Time
	To        time.Time
	Limit     int
}

func (s *Store) UpsertSportsTeam(ctx context.Context, t SportsTeam) (SportsTeam, error) {
	err := s.pool.QueryRow(ctx, `
		INSERT INTO sports_teams (external_id, sport, league, name, name_alternates, badge_url, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, now())
		ON CONFLICT (external_id) DO UPDATE SET
			sport = EXCLUDED.sport,
			league = COALESCE(NULLIF(EXCLUDED.league, ''), sports_teams.league),
			name = COALESCE(NULLIF(EXCLUDED.name, ''), sports_teams.name),
			name_alternates = CASE
				WHEN EXCLUDED.name_alternates <> '' THEN EXCLUDED.name_alternates
				ELSE sports_teams.name_alternates
			END,
			badge_url = CASE
				WHEN EXCLUDED.badge_url <> '' THEN EXCLUDED.badge_url
				ELSE sports_teams.badge_url
			END,
			updated_at = now()
		RETURNING id, external_id, sport, league, name, name_alternates, badge_url, updated_at
	`, t.ExternalID, t.Sport, t.League, t.Name, t.NameAlternates, t.BadgeURL).Scan(
		&t.ID, &t.ExternalID, &t.Sport, &t.League, &t.Name, &t.NameAlternates, &t.BadgeURL, &t.UpdatedAt,
	)
	return t, err
}

func (s *Store) SportsTeamByExternalID(ctx context.Context, externalID string) (SportsTeam, error) {
	var t SportsTeam
	err := s.pool.QueryRow(ctx, `
		SELECT id, external_id, sport, league, name, name_alternates, badge_url, updated_at
		FROM sports_teams WHERE external_id = $1
	`, externalID).Scan(&t.ID, &t.ExternalID, &t.Sport, &t.League, &t.Name, &t.NameAlternates, &t.BadgeURL, &t.UpdatedAt)
	return t, err
}

func (s *Store) FindSportsTeamByName(ctx context.Context, sport, name string) (SportsTeam, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return SportsTeam{}, pgx.ErrNoRows
	}
	folded := foldSportsName(name)
	var t SportsTeam
	err := s.pool.QueryRow(ctx, `
		SELECT id, external_id, sport, league, name, name_alternates, badge_url, updated_at
		FROM sports_teams
		WHERE ($1 = '' OR lower(sport) = lower($1))
		  AND (
			lower(name) = lower($2)
			OR lower(name) = lower($3)
			OR translate(lower(name), 'éèêëáàäâíìîóòôöúùûüçñ', 'eeeeaaaaiioooouuuucn') = lower($3)
			OR lower(name_alternates) LIKE '%' || lower($2) || '%'
			OR translate(lower(name_alternates), 'éèêëáàäâíìîóòôöúùûüçñ', 'eeeeaaaaiioooouuuucn') LIKE '%' || lower($3) || '%'
		  )
		ORDER BY CASE
			WHEN lower(name) = lower($2) OR lower(name) = lower($3) THEN 0
			ELSE 1
		END, updated_at DESC
		LIMIT 1
	`, sport, name, folded).Scan(&t.ID, &t.ExternalID, &t.Sport, &t.League, &t.Name, &t.NameAlternates, &t.BadgeURL, &t.UpdatedAt)
	return t, err
}

func foldSportsName(s string) string {
	repl := strings.NewReplacer(
		"é", "e", "è", "e", "ê", "e", "ë", "e",
		"á", "a", "à", "a", "ä", "a", "â", "a",
		"í", "i", "ì", "i", "î", "i",
		"ó", "o", "ò", "o", "ô", "o", "ö", "o",
		"ú", "u", "ù", "u", "û", "u", "ü", "u",
		"ç", "c", "ñ", "n",
		"É", "E", "È", "E", "Ê", "E",
		"Á", "A", "À", "A",
		"Ó", "O", "Ú", "U", "Ç", "C",
	)
	return repl.Replace(s)
}

func (s *Store) ListSportsTeamsMissingBadge(ctx context.Context, limit int) ([]SportsTeam, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, external_id, sport, league, name, name_alternates, badge_url, updated_at
		FROM sports_teams
		WHERE badge_url = '' AND external_id <> ''
		ORDER BY updated_at ASC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SportsTeam
	for rows.Next() {
		var t SportsTeam
		if err := rows.Scan(&t.ID, &t.ExternalID, &t.Sport, &t.League, &t.Name, &t.NameAlternates, &t.BadgeURL, &t.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

type SportsEventUpsert struct {
	ExternalID string
	Sport      string
	Title      string
	HomeTeam   string
	AwayTeam   string
	HomeTeamID *uuid.UUID
	AwayTeamID *uuid.UUID
	StartsAt   time.Time
	EndsAt     *time.Time
	Source     string
	Channels   []string // broadcast labels
}

func (s *Store) UpsertSportsEvent(ctx context.Context, in SportsEventUpsert) (uuid.UUID, error) {
	if in.Source == "" {
		in.Source = "thesportsdb"
	}
	var id uuid.UUID
	err := s.pool.QueryRow(ctx, `
		INSERT INTO sports_events (
			external_id, sport, title, home_team, away_team, home_team_id, away_team_id,
			starts_at, ends_at, source, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10, now())
		ON CONFLICT (external_id) DO UPDATE SET
			sport = EXCLUDED.sport,
			title = EXCLUDED.title,
			home_team = EXCLUDED.home_team,
			away_team = EXCLUDED.away_team,
			home_team_id = COALESCE(EXCLUDED.home_team_id, sports_events.home_team_id),
			away_team_id = COALESCE(EXCLUDED.away_team_id, sports_events.away_team_id),
			starts_at = EXCLUDED.starts_at,
			ends_at = COALESCE(EXCLUDED.ends_at, sports_events.ends_at),
			source = EXCLUDED.source,
			updated_at = now()
		RETURNING id
	`, in.ExternalID, in.Sport, in.Title, in.HomeTeam, in.AwayTeam, in.HomeTeamID, in.AwayTeamID,
		in.StartsAt, in.EndsAt, in.Source).Scan(&id)
	if err != nil {
		return uuid.Nil, err
	}

	seen := map[string]struct{}{}
	for _, label := range in.Channels {
		label = strings.TrimSpace(label)
		if label == "" {
			continue
		}
		key := strings.ToLower(label)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		if _, err := s.pool.Exec(ctx, `
			INSERT INTO sports_event_channels (event_id, broadcast_label)
			VALUES ($1, $2)
			ON CONFLICT (event_id, broadcast_label) DO NOTHING
		`, id, label); err != nil {
			return uuid.Nil, err
		}
	}
	return id, nil
}

// DeleteSportsEventByExternalID removes an event and cascaded channel rows.
func (s *Store) DeleteSportsEventByExternalID(ctx context.Context, externalID string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM sports_events WHERE external_id = $1`, externalID)
	return err
}

// DeleteSportsEvent removes an event by id (channels cascade).
func (s *Store) DeleteSportsEvent(ctx context.Context, id uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM sports_events WHERE id = $1`, id)
	return err
}

// MoveSportsEventChannels copies broadcast rows from one event onto another, preferring better matches.
func (s *Store) MoveSportsEventChannels(ctx context.Context, fromID, toID uuid.UUID) error {
	if fromID == toID {
		return nil
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO sports_event_channels (event_id, broadcast_label, live_channel_id, match_score)
		SELECT $2, broadcast_label, live_channel_id, match_score
		FROM sports_event_channels
		WHERE event_id = $1
		ON CONFLICT (event_id, broadcast_label) DO UPDATE SET
			live_channel_id = CASE
				WHEN EXCLUDED.match_score > sports_event_channels.match_score
					OR sports_event_channels.live_channel_id IS NULL
				THEN COALESCE(EXCLUDED.live_channel_id, sports_event_channels.live_channel_id)
				ELSE sports_event_channels.live_channel_id
			END,
			match_score = GREATEST(sports_event_channels.match_score, EXCLUDED.match_score)
	`, fromID, toID)
	return err
}

func (s *Store) UpdateSportsEventScore(ctx context.Context, externalID, home, away, period, status string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE sports_events
		SET home_score = $2, away_score = $3, period = $4, status = $5, score_updated_at = now(), updated_at = now()
		WHERE external_id = $1
	`, externalID, home, away, period, status)
	return err
}

// UpdateSportsEventScoreByTeams updates score fields for events matching sport/teams near start.
func (s *Store) UpdateSportsEventScoreByTeams(ctx context.Context, sport, home, away string, start time.Time, homeScore, awayScore, period, status string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE sports_events
		SET home_score = $5, away_score = $6, period = $7, status = $8, score_updated_at = now(), updated_at = now()
		WHERE lower(sport) = lower($1)
		  AND (
			(lower(home_team) = lower($2) AND lower(away_team) = lower($3))
			OR (lower(home_team) = lower($3) AND lower(away_team) = lower($2))
		  )
		  AND starts_at BETWEEN $4::timestamptz - interval '4 hours' AND $4::timestamptz + interval '4 hours'
	`, sport, home, away, start, homeScore, awayScore, period, status)
	return err
}

// ExtendSportsEventEnd pushes ends_at forward when a game is still live.
func (s *Store) ExtendSportsEventEnd(ctx context.Context, externalID string, ends time.Time) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE sports_events
		SET ends_at = GREATEST(COALESCE(ends_at, $2), $2), updated_at = now()
		WHERE external_id = $1
	`, externalID, ends)
	return err
}

// ExtendSportsEventEndByTeams is the team-pair variant of ExtendSportsEventEnd.
func (s *Store) ExtendSportsEventEndByTeams(ctx context.Context, sport, home, away string, start, ends time.Time) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE sports_events
		SET ends_at = GREATEST(COALESCE(ends_at, $5), $5), updated_at = now()
		WHERE lower(sport) = lower($1)
		  AND (
			(lower(home_team) = lower($2) AND lower(away_team) = lower($3))
			OR (lower(home_team) = lower($3) AND lower(away_team) = lower($2))
		  )
		  AND starts_at BETWEEN $4::timestamptz - interval '4 hours' AND $4::timestamptz + interval '4 hours'
	`, sport, home, away, start, ends)
	return err
}

func (s *Store) SetSportsEventChannelMatch(ctx context.Context, eventID uuid.UUID, broadcastLabel string, channelID *uuid.UUID, score float64) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE sports_event_channels
		SET live_channel_id = $3, match_score = $4
		WHERE event_id = $1 AND broadcast_label = $2
	`, eventID, broadcastLabel, channelID, score)
	return err
}

// ClearSportsEventChannels removes all broadcast rows for an event (before re-attach).
func (s *Store) ClearSportsEventChannels(ctx context.Context, eventID uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM sports_event_channels WHERE event_id = $1`, eventID)
	return err
}

// ClearCategoryHeaderSportsMatches unlinks IPTV category separators (### ESPN PPV ###).
func (s *Store) ClearCategoryHeaderSportsMatches(ctx context.Context) (int64, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE sports_event_channels ec
		SET live_channel_id = NULL, match_score = 0
		FROM live_channels c
		WHERE ec.live_channel_id = c.id
		  AND (
			length(c.name) - length(replace(c.name, '#', '')) >= 3
			OR trim(both ' #-=*_•·' from c.name) ~* '^(sports?|ppv|live|24/7|replay|events)$'
		  )
	`)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// ListSportsEventChannelsForRematch returns broadcast rows for events in [from,to].
func (s *Store) ListSportsEventChannelsForRematch(ctx context.Context, from, to time.Time, limit int) ([]SportsEventChannel, error) {
	if limit <= 0 {
		limit = 5000
	}
	rows, err := s.pool.Query(ctx, `
		SELECT ec.id, ec.event_id, ec.broadcast_label, ec.live_channel_id, ec.match_score
		FROM sports_event_channels ec
		JOIN sports_events e ON e.id = ec.event_id
		WHERE e.starts_at < $2
		  AND COALESCE(e.ends_at, e.starts_at + interval '3 hours') > $1
		ORDER BY e.starts_at ASC, ec.broadcast_label
		LIMIT $3
	`, from, to, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SportsEventChannel
	for rows.Next() {
		var c SportsEventChannel
		if err := rows.Scan(&c.ID, &c.EventID, &c.BroadcastLabel, &c.LiveChannelID, &c.MatchScore); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) ListUnmatchedSportsChannels(ctx context.Context, limit int) ([]SportsEventChannel, error) {
	if limit <= 0 {
		limit = 500
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, event_id, broadcast_label, live_channel_id, match_score
		FROM sports_event_channels
		WHERE live_channel_id IS NULL
		ORDER BY broadcast_label
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SportsEventChannel
	for rows.Next() {
		var c SportsEventChannel
		if err := rows.Scan(&c.ID, &c.EventID, &c.BroadcastLabel, &c.LiveChannelID, &c.MatchScore); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) SportsChannelAlias(ctx context.Context, label string) (uuid.UUID, bool, error) {
	var id uuid.UUID
	err := s.pool.QueryRow(ctx, `
		SELECT live_channel_id FROM sports_channel_aliases WHERE lower(broadcast_label) = lower($1)
	`, strings.TrimSpace(label)).Scan(&id)
	if err == pgx.ErrNoRows {
		return uuid.Nil, false, nil
	}
	if err != nil {
		return uuid.Nil, false, err
	}
	return id, true, nil
}

func (s *Store) UpsertSportsChannelAlias(ctx context.Context, label string, channelID uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO sports_channel_aliases (broadcast_label, live_channel_id, updated_at)
		VALUES ($1, $2, now())
		ON CONFLICT (broadcast_label) DO UPDATE SET live_channel_id = EXCLUDED.live_channel_id, updated_at = now()
	`, strings.TrimSpace(label), channelID)
	return err
}

func (s *Store) ListLiveChannelsForMatch(ctx context.Context) ([]LiveChannel, error) {
	rows, err := s.pool.Query(ctx, liveChannelSelect+` ORDER BY c.name`)
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

// RenameSportsLabel rewrites sport on events and teams (e.g. Soccer → Football).
func (s *Store) RenameSportsLabel(ctx context.Context, from, to string) (int64, error) {
	from, to = strings.TrimSpace(from), strings.TrimSpace(to)
	if from == "" || to == "" || strings.EqualFold(from, to) {
		return 0, nil
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE sports_events SET sport = $2, updated_at = now()
		WHERE lower(sport) = lower($1)
	`, from, to)
	if err != nil {
		return 0, err
	}
	n := tag.RowsAffected()
	_, _ = s.pool.Exec(ctx, `
		UPDATE sports_teams SET sport = $2, updated_at = now()
		WHERE lower(sport) = lower($1)
	`, from, to)
	return n, nil
}

func (s *Store) SportsMeta(ctx context.Context, from, to time.Time) ([]SportsSportMeta, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT sport, COUNT(*)::int
		FROM sports_events
		WHERE starts_at < $2 AND COALESCE(ends_at, starts_at + interval '3 hours') > $1
		  AND trim(away_team) <> '' AND trim(home_team) <> ''
		GROUP BY sport
		ORDER BY sport
	`, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SportsSportMeta
	for rows.Next() {
		var m SportsSportMeta
		if err := rows.Scan(&m.Sport, &m.Count); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) ListSportsEvents(ctx context.Context, opts SportsListOpts) ([]SportsEvent, error) {
	if opts.Limit <= 0 {
		opts.Limit = 200
	}
	if opts.From.IsZero() {
		opts.From = time.Now().Add(-2 * time.Hour)
	}
	if opts.To.IsZero() {
		opts.To = time.Now().Add(48 * time.Hour)
	}

	args := []any{opts.From, opts.To}
	where := []string{`e.starts_at < $2`, `COALESCE(e.ends_at, e.starts_at + interval '3 hours') > $1`}
	if sport := strings.TrimSpace(opts.Sport); sport != "" {
		args = append(args, sport)
		where = append(where, fmt.Sprintf(`lower(e.sport) = lower($%d)`, len(args)))
	}
	if len(opts.Favorites) > 0 {
		args = append(args, opts.Favorites)
		where = append(where, fmt.Sprintf(`e.sport = ANY($%d)`, len(args)))
	}
	args = append(args, opts.Limit)

	q := `
		SELECT e.id, e.external_id, e.sport, e.title, e.home_team, e.away_team,
			e.home_team_id, e.away_team_id, e.starts_at, e.ends_at, e.source,
			e.home_score, e.away_score, e.period, e.status, e.score_updated_at,
			COALESCE(ht.badge_url, ''), COALESCE(at.badge_url, ''),
			COALESCE(NULLIF(ht.league, ''), NULLIF(at.league, ''), '')
		FROM sports_events e
		LEFT JOIN sports_teams ht ON ht.id = e.home_team_id
		LEFT JOIN sports_teams at ON at.id = e.away_team_id
		WHERE ` + strings.Join(where, ` AND `) + `
		ORDER BY e.starts_at ASC
		LIMIT $` + fmt.Sprint(len(args))

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []SportsEvent
	ids := make([]uuid.UUID, 0)
	for rows.Next() {
		var e SportsEvent
		if err := rows.Scan(
			&e.ID, &e.ExternalID, &e.Sport, &e.Title, &e.HomeTeam, &e.AwayTeam,
			&e.HomeTeamID, &e.AwayTeamID, &e.StartsAt, &e.EndsAt, &e.Source,
			&e.HomeScore, &e.AwayScore, &e.Period, &e.Status, &e.ScoreUpdatedAt,
			&e.HomeBadgeURL, &e.AwayBadgeURL, &e.League,
		); err != nil {
			return nil, err
		}
		events = append(events, e)
		ids = append(ids, e.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return events, nil
	}

	chRows, err := s.pool.Query(ctx, `
		SELECT ec.id, ec.event_id, ec.broadcast_label, ec.live_channel_id, ec.match_score,
			COALESCE(c.name, ''), COALESCE(c.logo_url, '')
		FROM sports_event_channels ec
		LEFT JOIN live_channels c ON c.id = ec.live_channel_id
		WHERE ec.event_id = ANY($1)
		ORDER BY
			(ec.live_channel_id IS NOT NULL) DESC,
			ec.match_score DESC,
			ec.broadcast_label
	`, ids)
	if err != nil {
		return nil, err
	}
	defer chRows.Close()

	byEvent := map[uuid.UUID][]SportsEventChannel{}
	for chRows.Next() {
		var c SportsEventChannel
		if err := chRows.Scan(&c.ID, &c.EventID, &c.BroadcastLabel, &c.LiveChannelID, &c.MatchScore, &c.ChannelName, &c.ChannelLogoURL); err != nil {
			return nil, err
		}
		c.Playable = c.LiveChannelID != nil
		byEvent[c.EventID] = append(byEvent[c.EventID], c)
	}
	if err := chRows.Err(); err != nil {
		return nil, err
	}
	for i := range events {
		events[i].Channels = byEvent[events[i].ID]
		if events[i].Channels == nil {
			events[i].Channels = []SportsEventChannel{}
		}
	}
	return events, nil
}

func (s *Store) SportsEventsNeedingTeams(ctx context.Context, limit int) ([]SportsEvent, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, external_id, sport, title, home_team, away_team, home_team_id, away_team_id,
			starts_at, ends_at, source, home_score, away_score, period, status, score_updated_at
		FROM sports_events
		WHERE (home_team <> '' AND home_team_id IS NULL)
		   OR (away_team <> '' AND away_team_id IS NULL)
		ORDER BY starts_at ASC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SportsEvent
	for rows.Next() {
		var e SportsEvent
		if err := rows.Scan(
			&e.ID, &e.ExternalID, &e.Sport, &e.Title, &e.HomeTeam, &e.AwayTeam,
			&e.HomeTeamID, &e.AwayTeamID, &e.StartsAt, &e.EndsAt, &e.Source,
			&e.HomeScore, &e.AwayScore, &e.Period, &e.Status, &e.ScoreUpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) LinkSportsEventTeam(ctx context.Context, eventID uuid.UUID, home bool, teamID uuid.UUID) error {
	col := "away_team_id"
	if home {
		col = "home_team_id"
	}
	_, err := s.pool.Exec(ctx, fmt.Sprintf(`
		UPDATE sports_events SET %s = $2, updated_at = now() WHERE id = $1
	`, col), eventID, teamID)
	return err
}

func (s *Store) DeleteOldSportsEvents(ctx context.Context, before time.Time) (int64, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM sports_events WHERE starts_at < $1`, before)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// EPGSportsHit is a live programme that may represent a sports matchup or league slot.
type EPGSportsHit struct {
	Title        string
	Description  string
	StartTime    time.Time
	EndTime      time.Time
	Category     string
	ChannelID    uuid.UUID
	ChannelName  string
	ChannelLogo  string
	ChannelTVGID string
}

// ListEPGSportsHits returns programmes in [from,to] whose titles look sports-related.
func (s *Store) ListEPGSportsHits(ctx context.Context, from, to time.Time) ([]EPGSportsHit, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT DISTINCT ON (c.id, p.start_time, p.title)
			p.title, COALESCE(p.description, ''), p.start_time, p.end_time, COALESCE(p.category, ''),
			c.id, c.name, COALESCE(c.logo_url, ''), c.tvg_id
		FROM live_programs p
		INNER JOIN live_channels c
		  ON c.tvg_id = p.channel_tvg_id
		  OR (c.epg_channel_id <> '' AND c.epg_channel_id = p.channel_tvg_id)
		WHERE p.end_time > $1 AND p.start_time < $2
		  AND (
			p.title ~* '(nhl|nba|mlb|nfl|cfl|mls|wnba|uefa|fifa|hockey|hokej|ishockey|canadiens|basketball|koszykówka|koszykowka|baseball|soccer|football|fotboll|piłka|pilka|futebol|fútbol|futbol|ufc|mma|boxing|boks|tennis|golf|formel|formula|f1|laliga|la liga|bundesliga|premier league|championship|nations league|campeones|europa league|conference league|rugby|cricket|volleyball|siatkówka|siatkowka|deportes)'
			OR p.title ~* '( vs\.? | @ | v )'
			OR p.category ~* '(sport|hockey|basketball|baseball|football|soccer|tennis|golf|motor)'
			OR p.description ~* '(uefa|nhl|nba|mlb|nfl|cfl|nations league|bundesliga|premier league|vs\.? )'
		  )
		  AND p.title !~* '^(sendepause|fin des programmes|programmes coming soon|\.\.programmes|info|najava|kraj programa|sleep)'
		ORDER BY c.id, p.start_time, p.title
		LIMIT 8000
	`, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EPGSportsHit
	for rows.Next() {
		var h EPGSportsHit
		if err := rows.Scan(&h.Title, &h.Description, &h.StartTime, &h.EndTime, &h.Category, &h.ChannelID, &h.ChannelName, &h.ChannelLogo, &h.ChannelTVGID); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// ListEPGHitsForChannels returns programmes on the given channels overlapping [from,to].
func (s *Store) ListEPGHitsForChannels(ctx context.Context, channelIDs []uuid.UUID, from, to time.Time) ([]EPGSportsHit, error) {
	if len(channelIDs) == 0 {
		return nil, nil
	}
	rows, err := s.pool.Query(ctx, `
		SELECT p.title, COALESCE(p.description, ''), p.start_time, p.end_time, COALESCE(p.category, ''),
			c.id, c.name, COALESCE(c.logo_url, ''), c.tvg_id
		FROM live_programs p
		INNER JOIN live_channels c
		  ON c.tvg_id = p.channel_tvg_id
		  OR (c.epg_channel_id <> '' AND c.epg_channel_id = p.channel_tvg_id)
		WHERE c.id = ANY($1)
		  AND p.end_time > $2 AND p.start_time < $3
		ORDER BY p.start_time
		LIMIT 2000
	`, channelIDs, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EPGSportsHit
	for rows.Next() {
		var h EPGSportsHit
		if err := rows.Scan(&h.Title, &h.Description, &h.StartTime, &h.EndTime, &h.Category, &h.ChannelID, &h.ChannelName, &h.ChannelLogo, &h.ChannelTVGID); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// UpdateSportsEventWindow updates start/end times for an event.
func (s *Store) UpdateSportsEventWindow(ctx context.Context, id uuid.UUID, starts, ends time.Time) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE sports_events
		SET starts_at = $2, ends_at = $3, updated_at = now()
		WHERE id = $1
	`, id, starts, ends)
	return err
}

// SportsMatchupForChannel returns "Away @ Home" for a sports event linked to the
// channel around at (pregame through end). Empty when no named matchup is linked.
func (s *Store) SportsMatchupForChannel(ctx context.Context, channelID uuid.UUID, at time.Time) (string, error) {
	if channelID == uuid.Nil {
		return "", nil
	}
	if at.IsZero() {
		at = time.Now()
	}
	var home, away string
	err := s.pool.QueryRow(ctx, `
		SELECT e.home_team, e.away_team
		FROM sports_event_channels ec
		JOIN sports_events e ON e.id = ec.event_id
		WHERE ec.live_channel_id = $1
			AND e.home_team <> '' AND e.away_team <> ''
			AND e.starts_at <= $2 + interval '30 minutes'
			AND COALESCE(e.ends_at, e.starts_at + interval '4 hours') >= $2 - interval '15 minutes'
		ORDER BY ec.match_score DESC, e.starts_at DESC
		LIMIT 1
	`, channelID, at).Scan(&home, &away)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", nil
		}
		return "", err
	}
	home = strings.TrimSpace(home)
	away = strings.TrimSpace(away)
	if home == "" || away == "" {
		return "", nil
	}
	return away + " @ " + home, nil
}

// UpsertSportsEventChannel links a broadcast row and optionally a live channel.
func (s *Store) UpsertSportsEventChannel(ctx context.Context, eventID uuid.UUID, label string, channelID *uuid.UUID, score float64) error {
	label = strings.TrimSpace(label)
	if label == "" {
		return nil
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO sports_event_channels (event_id, broadcast_label, live_channel_id, match_score)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (event_id, broadcast_label) DO UPDATE SET
			live_channel_id = CASE
				WHEN EXCLUDED.match_score > sports_event_channels.match_score
					OR sports_event_channels.live_channel_id IS NULL
				THEN COALESCE(EXCLUDED.live_channel_id, sports_event_channels.live_channel_id)
				ELSE sports_event_channels.live_channel_id
			END,
			match_score = GREATEST(sports_event_channels.match_score, EXCLUDED.match_score)
	`, eventID, label, channelID, score)
	return err
}
