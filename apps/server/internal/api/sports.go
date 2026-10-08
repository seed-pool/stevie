package api

import (
	"context"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/stevie-media/stevie/apps/server/internal/livetv"
	"github.com/stevie-media/stevie/apps/server/internal/sportsdb"
	"github.com/stevie-media/stevie/apps/server/internal/store"
	"github.com/stevie-media/stevie/apps/server/internal/streamed"
)

type sportsTeamDTO struct {
	Name     string `json:"name"`
	BadgeURL string `json:"badge_url,omitempty"`
}

type sportsScoreDTO struct {
	Home   string `json:"home"`
	Away   string `json:"away"`
	Period string `json:"period,omitempty"`
	Status string `json:"status,omitempty"`
}

type sportsChannelDTO struct {
	BroadcastLabel string `json:"broadcast_label"`
	ID             string `json:"id,omitempty"`
	Name           string `json:"name,omitempty"`
	LogoURL        string `json:"logo_url,omitempty"`
	Playable       bool   `json:"playable"`
	// Confirmed when ESPN/MLB listed this network for the game.
	Confirmed bool `json:"confirmed,omitempty"`
	// Source is "streamed" for streamed.pk rows; empty means IPTV library.
	Source string `json:"source,omitempty"`
	// EmbedURL is a fallback (ad-heavy); prefer streamed_source/id via /sports/streamed/play.
	EmbedURL       string `json:"embed_url,omitempty"`
	StreamedSource string `json:"streamed_source,omitempty"`
	StreamedID     string `json:"streamed_id,omitempty"`
	StreamedNo     int    `json:"streamed_no,omitempty"`
}

type sportsEventDTO struct {
	ID               string             `json:"id"`
	ExternalID       string             `json:"external_id"`
	Sport            string             `json:"sport"`
	Title            string             `json:"title"`
	Home             sportsTeamDTO      `json:"home"`
	Away             sportsTeamDTO      `json:"away"`
	StartsAt         string             `json:"starts_at"`
	EndsAt           string             `json:"ends_at"`
	Live             bool               `json:"live"`
	Upcoming         bool               `json:"upcoming"`
	Score            *sportsScoreDTO    `json:"score,omitempty"`
	Channels         []sportsChannelDTO `json:"channels"`
	StreamedCategory string             `json:"streamed_category,omitempty"`
	League           string             `json:"league,omitempty"`
	LeagueLogoURL    string             `json:"league_logo_url,omitempty"`
}

func (s *Server) handleSportsMeta(w http.ResponseWriter, r *http.Request) {
	from := time.Now().Add(-2 * time.Hour)
	// NFL/CFL week + international slates need more than a 24h horizon.
	to := time.Now().Add(7 * 24 * time.Hour)
	meta, err := s.store.SportsMeta(r.Context(), from, to)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if meta == nil {
		meta = []store.SportsSportMeta{}
	}
	// Always surface the full streamed.pk catalog as its own Sports category.
	extCount := 0
	if s.streamed != nil {
		if matches, err := s.streamed.AllMatches(r.Context()); err == nil {
			extCount = len(matches)
		}
	}
	meta = append(meta, store.SportsSportMeta{
		Sport: streamed.ExternalSportLabel,
		Count: extCount,
	})
	status := map[string]any{}
	if s.sports != nil {
		status = s.sports.Status()
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"sports": meta,
		"sync":   status,
	})
}

func (s *Server) handleSportsEvents(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	sport := normalizeSportsQueryLabel(strings.TrimSpace(q.Get("sport")))
	favRaw := strings.TrimSpace(q.Get("favorites"))
	var favorites []string
	if favRaw != "" {
		for _, p := range strings.Split(favRaw, ",") {
			p = normalizeSportsQueryLabel(strings.TrimSpace(p))
			if p != "" {
				favorites = append(favorites, p)
			}
		}
	}
	wantExternalOnly := sport == streamed.ExternalSportLabel
	externalPinned := false
	for _, f := range favorites {
		if f == streamed.ExternalSportLabel {
			externalPinned = true
			break
		}
	}

	from := time.Now().Add(-2 * time.Hour)
	to := time.Now().Add(7 * 24 * time.Hour)
	if v := q.Get("from"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			from = t
		}
	}
	if v := q.Get("to"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			to = t
		}
	}

	var out []sportsEventDTO
	if wantExternalOnly {
		out = s.streamedExternalEvents(r.Context())
		writeJSON(w, http.StatusOK, map[string]any{"events": out})
		return
	}

	// Strip External from favorites so the DB query stays on real sports labels.
	var dbFavorites []string
	for _, f := range favorites {
		if f != streamed.ExternalSportLabel {
			dbFavorites = append(dbFavorites, f)
		}
	}
	// Favorites view with only External pinned → catalog only.
	if len(favorites) > 0 && len(dbFavorites) == 0 && externalPinned {
		out = s.streamedExternalEvents(r.Context())
		writeJSON(w, http.StatusOK, map[string]any{"events": out})
		return
	}

	events, err := s.store.ListSportsEvents(r.Context(), store.SportsListOpts{
		Sport:     sport,
		Favorites: dbFavorites,
		From:      from,
		To:        to,
		Limit:     300,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Prefer scored ESPN/MLB cards over scoreless SportsDB/EPG duplicates.
	events = sportsdb.DedupedSportsEvents(events)

	now := time.Now()
	// Most sports: next 24h. American Football (NFL/CFL week) needs a full slate.
	upcomingHorizon := now.Add(24 * time.Hour)
	afHorizon := now.Add(7 * 24 * time.Hour)
	// Broadcasts usually start before tip-off (pregame / warm-up).
	const pregame = 30 * time.Minute
	out = make([]sportsEventDTO, 0, len(events))
	for _, e := range events {
		// Sports tab is matchup-only — skip league blocks / single-team stubs.
		if strings.TrimSpace(e.AwayTeam) == "" || strings.TrimSpace(e.HomeTeam) == "" {
			continue
		}
		if sportsdb.IsGenericLeagueTitle(e.Title, e.Sport) {
			continue
		}
		if !sportsdb.IsPlausibleSportsMatchup(e.Sport, e.HomeTeam, e.AwayTeam, e.Title) {
			continue
		}
		ends := e.StartsAt.Add(3 * time.Hour)
		if e.EndsAt != nil {
			ends = *e.EndsAt
		}
		airStart := e.StartsAt.Add(-pregame)
		// Baseball (esp. playoffs) routinely overruns the EPG end time.
		grace := time.Duration(0)
		if strings.EqualFold(e.Sport, "Baseball") {
			grace = 90 * time.Minute
		}
		live := !now.Before(airStart) && now.Before(ends.Add(grace))
		upcoming := now.Before(airStart)
		horizon := upcomingHorizon
		if strings.EqualFold(e.Sport, "American Football") {
			horizon = afHorizon
		}
		if upcoming && e.StartsAt.After(horizon) {
			continue
		}
		if !live && !upcoming {
			// Drop long-finished cards outside the live window.
			continue
		}
		league := strings.TrimSpace(e.League)
		if league == "" {
			league = s.inferLeagueName(r.Context(), e.Sport, e.HomeTeam, e.AwayTeam)
		}
		if league == "" {
			league = inferLeagueFromTitle(e.Title, e.Sport)
		}
		dto := sportsEventDTO{
			ID:         e.ID.String(),
			ExternalID: e.ExternalID,
			Sport:      e.Sport,
			Title:      e.Title,
			Home: sportsTeamDTO{
				Name:     e.HomeTeam,
				BadgeURL: s.sportsBadgeURL(e.HomeBadgeURL, e.Sport, e.HomeTeam),
			},
			Away: sportsTeamDTO{
				Name:     e.AwayTeam,
				BadgeURL: s.sportsBadgeURL(e.AwayBadgeURL, e.Sport, e.AwayTeam),
			},
			StartsAt:      e.StartsAt.UTC().Format(time.RFC3339),
			EndsAt:        ends.UTC().Format(time.RFC3339),
			Live:          live,
			Upcoming:      upcoming,
			Channels:      []sportsChannelDTO{},
			League:        league,
			LeagueLogoURL: s.leagueLogoURL(league, e.Sport),
		}
		if e.HomeScore != "" || e.AwayScore != "" || e.Period != "" || e.Status != "" {
			dto.Score = &sportsScoreDTO{
				Home:   e.HomeScore,
				Away:   e.AwayScore,
				Period: e.Period,
				Status: e.Status,
			}
		}
		for _, ch := range e.Channels {
			// Category separators (### ESPN PPV ###) are not playable channels.
			header := sportsdb.IsCategoryHeaderChannel(ch.ChannelName)
			if header || sportsdb.RejectChannelForSport(e.Sport, ch.ChannelName) ||
				sportsdb.RejectChannelForSport(e.Sport, ch.BroadcastLabel) {
				continue
			}
			cd := sportsChannelDTO{
				BroadcastLabel: ch.BroadcastLabel,
				Name:           ch.ChannelName,
				LogoURL:        livetv.SanitizeLogoURL(ch.ChannelLogoURL),
				Playable:       ch.Playable,
				Confirmed:      sportsdb.IsConfirmedBroadcast(ch.MatchScore),
			}
			if ch.LiveChannelID != nil {
				cd.ID = ch.LiveChannelID.String()
				if cd.Name == "" {
					cd.Name = ch.BroadcastLabel
				}
			} else {
				cd.ID = ""
				cd.Name = ch.BroadcastLabel
				cd.LogoURL = ""
				cd.Playable = false
			}
			dto.Channels = append(dto.Channels, cd)
		}
		out = append(out, dto)
	}
	// Supplement with streamed.pk embeds (appended after IPTV rows per event).
	s.enrichStreamedChannels(r.Context(), out)
	hot := s.pickHotEvents(r.Context(), out, sport)
	if externalPinned {
		out = append(out, s.streamedExternalEvents(r.Context())...)
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": out, "hot": hot})
}

func (s *Server) streamedExternalEvents(ctx context.Context) []sportsEventDTO {
	if s.streamed == nil {
		return nil
	}
	matches, err := s.streamed.AllMatches(ctx)
	if err != nil || len(matches) == 0 {
		return nil
	}
	liveIDs, _ := s.streamed.LiveMatchIDs(ctx)
	now := time.Now().UTC()
	const pregame = 30 * time.Minute
	const defaultLen = 3 * time.Hour

	out := make([]sportsEventDTO, 0, len(matches))
	for _, m := range matches {
		home, away := streamed.MatchSideNames(m)
		title := strings.TrimSpace(m.Title)
		if title == "" {
			if home != "" && away != "" {
				title = away + " @ " + home
			} else {
				title = m.ID
			}
		}
		cat := streamed.PrettyCategory(m.Category)
		starts, hasStart := streamed.MatchStart(m)
		_, onLive := liveIDs[m.ID]
		var live, upcoming bool
		var ends time.Time
		if !hasStart {
			// Always-on / schedule stubs (NFL Network, etc.).
			starts = now
			ends = now.Add(24 * time.Hour)
			live = true
		} else {
			ends = starts.Add(defaultLen)
			airStart := starts.Add(-pregame)
			live = onLive || (!now.Before(airStart) && now.Before(ends))
			upcoming = now.Before(airStart)
			if !live && !upcoming {
				// Keep far-future cards in this catalog (unlike IPTV Sports).
				if starts.After(now) {
					upcoming = true
				} else {
					continue
				}
			}
		}
		poster := s.streamed.AbsoluteURL(m.Poster)
		homeBadge := s.sportsBadgeURL("", "", home)
		awayBadge := s.sportsBadgeURL("", "", away)
		if homeBadge == "" {
			if u := livetv.SanitizeLogoURL(poster); u != "" {
				homeBadge = "/api/live/logo?url=" + url.QueryEscape(u)
			}
		}
		league := s.inferLeagueName(ctx, streamedCategorySport(cat), home, away)
		if league == "" {
			league = inferLeagueFromTitle(title, streamedCategorySport(cat))
		}
		dto := sportsEventDTO{
			ID:               "streamed:" + m.ID,
			ExternalID:       m.ID,
			Sport:            streamed.ExternalSportLabel,
			Title:            title,
			Home:             sportsTeamDTO{Name: home, BadgeURL: homeBadge},
			Away:             sportsTeamDTO{Name: away, BadgeURL: awayBadge},
			StartsAt:         starts.Format(time.RFC3339),
			EndsAt:           ends.Format(time.RFC3339),
			Live:             live,
			Upcoming:         upcoming && !live,
			Channels:         []sportsChannelDTO{},
			StreamedCategory: cat,
			League:           league,
			LeagueLogoURL:    s.leagueLogoURL(league, streamedCategorySport(cat)),
		}
		for _, ch := range streamed.ChannelsForMatch(m) {
			dto.Channels = append(dto.Channels, sportsChannelDTO{
				BroadcastLabel: "streamed.pk",
				Name:           ch.Name,
				Playable:       ch.SourceID != "",
				Source:         "streamed",
				EmbedURL:       ch.EmbedURL,
				StreamedSource: ch.Source,
				StreamedID:     ch.SourceID,
				StreamedNo:     ch.StreamNo,
			})
		}
		out = append(out, dto)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Live != out[j].Live {
			return out[i].Live
		}
		if out[i].Upcoming != out[j].Upcoming {
			return out[i].Upcoming
		}
		return out[i].StartsAt < out[j].StartsAt
	})
	return out
}

func (s *Server) enrichStreamedChannels(ctx context.Context, events []sportsEventDTO) {
	if s.streamed == nil || len(events) == 0 {
		return
	}
	type job struct {
		i        int
		startsAt time.Time
	}
	var jobs []job
	for i := range events {
		// Live + near tip-off only — avoid N stream lookups for far-future cards.
		if !events[i].Live && !events[i].Upcoming {
			continue
		}
		starts, _ := time.Parse(time.RFC3339, events[i].StartsAt)
		jobs = append(jobs, job{i: i, startsAt: starts})
	}
	if len(jobs) == 0 {
		return
	}
	// Cap work per request; matches cache is shared.
	if len(jobs) > 40 {
		jobs = jobs[:40]
	}
	for _, j := range jobs {
		ev := &events[j.i]
		chs, err := s.streamed.ChannelsForMatchup(ctx, ev.Sport, ev.Home.Name, ev.Away.Name, j.startsAt)
		if err != nil || len(chs) == 0 {
			continue
		}
		for _, ch := range chs {
			ev.Channels = append(ev.Channels, sportsChannelDTO{
				BroadcastLabel: "streamed.pk",
				Name:           ch.Name,
				Playable:       ch.SourceID != "",
				Source:         "streamed",
				EmbedURL:       ch.EmbedURL,
				StreamedSource: ch.Source,
				StreamedID:     ch.SourceID,
				StreamedNo:     ch.StreamNo,
			})
		}
	}
}

func (s *Server) handleSportsSync(w http.ResponseWriter, r *http.Request) {
	if s.sports == nil {
		writeErr(w, http.StatusServiceUnavailable, "sports sync unavailable")
		return
	}
	s.sports.Trigger(r.Context())
	writeJSON(w, http.StatusAccepted, s.sports.Status())
}

func normalizeSportsQueryLabel(sport string) string {
	sport = strings.TrimSpace(sport)
	if sport == streamed.ExternalSportLabel {
		return sport
	}
	switch strings.ToLower(sport) {
	case "soccer":
		return "Football"
	default:
		return sport
	}
}
