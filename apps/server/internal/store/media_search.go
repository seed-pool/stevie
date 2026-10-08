package store

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
)

// MediaSearchHitProgramme is a programme match with its channel.
type MediaSearchHitProgramme struct {
	ID           uuid.UUID `json:"id"`
	Title        string    `json:"title"`
	Description  string    `json:"description,omitempty"`
	StartTime    time.Time `json:"start_time"`
	EndTime      time.Time `json:"end_time"`
	Category     string    `json:"category,omitempty"`
	ChannelID    uuid.UUID `json:"channel_id"`
	ChannelName  string    `json:"channel_name"`
	ChannelLogo  string    `json:"channel_logo,omitempty"`
	GroupTitle   string    `json:"group_title,omitempty"`
	ChannelTVGID string    `json:"channel_tvg_id,omitempty"`
}

// MediaSearchResult is the unified top-bar search payload.
type MediaSearchResult struct {
	Query string `json:"q"`

	Movies     []VodMovieRow `json:"movies"`
	MovieTotal int           `json:"movie_total"`

	Series      []VodSeriesRow `json:"series"`
	SeriesTotal int            `json:"series_total"`

	Channels     []LiveChannel `json:"channels"`
	ChannelTotal int           `json:"channel_total"`

	Programmes     []MediaSearchHitProgramme `json:"programmes"`
	ProgrammeTotal int                       `json:"programme_total"`
}

// MediaSearch searches VOD + Live (channels and upcoming/recent programmes).
func (s *Store) MediaSearch(ctx context.Context, q string, limitPerKind int) (MediaSearchResult, error) {
	q = strings.TrimSpace(q)
	out := MediaSearchResult{Query: q}
	if q == "" {
		out.Movies = []VodMovieRow{}
		out.Series = []VodSeriesRow{}
		out.Channels = []LiveChannel{}
		out.Programmes = []MediaSearchHitProgramme{}
		return out, nil
	}
	if limitPerKind <= 0 || limitPerKind > 200 {
		limitPerKind = 100
	}

	movies, movieTotal, err := s.ListVodMovies(ctx, VodListOpts{Q: q, Sort: "name", Limit: limitPerKind})
	if err != nil {
		return out, err
	}
	out.Movies = movies
	out.MovieTotal = movieTotal

	series, seriesTotal, err := s.ListVodSeries(ctx, VodListOpts{Q: q, Sort: "name", Limit: limitPerKind})
	if err != nil {
		return out, err
	}
	out.Series = series
	out.SeriesTotal = seriesTotal

	chLimit := limitPerKind
	if chLimit > 100 {
		chLimit = 100
	}
	channels, err := s.ListLiveChannels(ctx, LiveListOpts{Query: q, Limit: chLimit})
	if err != nil {
		return out, err
	}
	out.Channels = channels
	chTotal, err := s.CountLiveChannelsOpts(ctx, LiveListOpts{Query: q})
	if err != nil {
		return out, err
	}
	out.ChannelTotal = chTotal

	progs, progTotal, err := s.searchLiveProgrammes(ctx, q, chLimit)
	if err != nil {
		return out, err
	}
	out.Programmes = progs
	out.ProgrammeTotal = progTotal
	return out, nil
}

func (s *Store) searchLiveProgrammes(ctx context.Context, q string, limit int) ([]MediaSearchHitProgramme, int, error) {
	pattern := "%" + q + "%"
	from := time.Now().Add(-2 * time.Hour)
	to := time.Now().Add(7 * 24 * time.Hour)

	var total int
	if err := s.pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM live_programs p
		WHERE (p.title ILIKE $1 OR p.description ILIKE $1)
		  AND p.end_time > $2
		  AND p.start_time < $3
	`, pattern, from, to).Scan(&total); err != nil {
		return nil, 0, err
	}

	rows, err := s.pool.Query(ctx, `
		SELECT p.id, p.channel_tvg_id, p.title, COALESCE(p.description, ''), p.start_time, p.end_time, COALESCE(p.category, ''),
		       c.id, c.name, COALESCE(c.logo_url, ''), COALESCE(c.group_title, '')
		FROM live_programs p
		INNER JOIN live_channels c
		  ON p.channel_tvg_id = c.tvg_id
		  OR (c.epg_channel_id <> '' AND p.channel_tvg_id = c.epg_channel_id)
		WHERE (p.title ILIKE $1 OR p.description ILIKE $1)
		  AND p.end_time > $2
		  AND p.start_time < $3
		ORDER BY p.start_time ASC, c.name ASC
		LIMIT $4
	`, pattern, from, to, limit)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	out := []MediaSearchHitProgramme{}
	seen := map[uuid.UUID]struct{}{}
	for rows.Next() {
		var h MediaSearchHitProgramme
		if err := rows.Scan(
			&h.ID, &h.ChannelTVGID, &h.Title, &h.Description, &h.StartTime, &h.EndTime, &h.Category,
			&h.ChannelID, &h.ChannelName, &h.ChannelLogo, &h.GroupTitle,
		); err != nil {
			return nil, 0, err
		}
		// Deduplicate when a programme matches multiple channel EPG keys.
		if _, ok := seen[h.ID]; ok {
			continue
		}
		seen[h.ID] = struct{}{}
		out = append(out, h)
	}
	return out, total, rows.Err()
}
