package sportsdb

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/stevie-media/stevie/apps/server/internal/livetv"
	"github.com/stevie-media/stevie/apps/server/internal/store"
	"github.com/stevie-media/stevie/apps/server/internal/teamlogo"
)

// syncESPNPrimary pulls Big-4 schedules from ESPN (+ MLB StatsAPI broadcasts/scores).
func (s *SyncService) syncESPNPrimary(ctx context.Context) (int, error) {
	if s.espn == nil {
		s.espn = NewESPNClient()
	}
	if s.mlb == nil {
		s.mlb = NewMLBClient()
	}

	liveChannels, err := s.store.ListLiveChannelsForMatch(ctx)
	if err != nil {
		return 0, err
	}
	cands := BuildCandidates(liveChannels)

	now := time.Now().UTC()
	days := scoreboardDays(now, false)
	imported := 0

	// MLB StatsAPI first — best broadcasts + live state for baseball.
	mlbByKey := map[string]MLBGame{}
	for _, day := range days {
		games, err := s.mlb.Schedule(ctx, day)
		if err != nil {
			slog.Warn("mlb statsapi schedule", "day", day.Format("2006-01-02"), "err", err)
			continue
		}
		for _, g := range games {
			key := teamPairKey(g.Home, g.Away, g.StartsAt)
			mlbByKey[key] = g
		}
		time.Sleep(200 * time.Millisecond)
	}
	coveredMLB := map[string]struct{}{}

	for _, league := range ESPNBig4 {
		if err := ctx.Err(); err != nil {
			return imported, err
		}
		// Default board (live/today) + calendar days. NFL/CFL need a full week window.
		seenIDs := map[string]struct{}{}
		footballWeek := league.LeaguePath == "nfl" || league.LeaguePath == "cfl"
		for _, day := range scoreboardDays(now, footballWeek) {
			events, err := s.espn.Scoreboard(ctx, league, day)
			if err != nil {
				slog.Warn("espn scoreboard", "league", league.LeaguePath, "err", err)
				time.Sleep(500 * time.Millisecond)
				continue
			}
			for _, ev := range events {
				if _, ok := seenIDs[ev.ID]; ok {
					continue
				}
				seenIDs[ev.ID] = struct{}{}
				// Drop stale ESPN boards (idle CFL seasons still return old finals).
				if !ev.StartsAt.IsZero() && ev.StartsAt.Before(now.Add(-72*time.Hour)) &&
					!strings.EqualFold(ev.Status, "in") &&
					!strings.Contains(strings.ToLower(ev.Status), "progress") {
					continue
				}

				broadcasts := append([]string{}, ev.Broadcasts...)
				homeScore, awayScore, period, status := ev.HomeScore, ev.AwayScore, ev.Period, ev.Status
				starts := ev.StartsAt

				if league.LeaguePath == "mlb" {
					key := teamPairKey(ev.Home, ev.Away, ev.StartsAt)
					if g, ok := mlbByKey[key]; ok {
						coveredMLB[key] = struct{}{}
						// Prefer a single card — drop any prior mlb:-only row.
						_ = s.store.DeleteSportsEventByExternalID(ctx, fmt.Sprintf("mlb:%d", g.GamePk))
						if len(g.Broadcasts) > 0 {
							broadcasts = g.Broadcasts
						}
						if g.HomeScore != "" || g.AwayScore != "" {
							homeScore, awayScore = g.HomeScore, g.AwayScore
						}
						if g.Period != "" {
							period = g.Period
						}
						if g.Status != "" {
							status = g.Status
						}
						// Keep ESPN tip-off; MLB is used for broadcasts/scores only.
					}
				}

				ends := starts.Add(DefaultDuration(ev.Sport))
				if strings.EqualFold(status, "in") || strings.Contains(strings.ToLower(status), "progress") ||
					strings.EqualFold(status, "live") {
					// Keep live games in the active window.
					if ends.Before(time.Now().Add(90 * time.Minute)) {
						ends = time.Now().Add(2 * time.Hour)
					}
				}

				homeID := s.ensureESPNTeam(ctx, ev.Sport, ev.League, ev.Home, ev.HomeAbbr, ev.HomeLogo)
				awayID := s.ensureESPNTeam(ctx, ev.Sport, ev.League, ev.Away, ev.AwayAbbr, ev.AwayLogo)

				labels := uniqueBroadcasts(broadcasts)
				ext := fmt.Sprintf("espn:%s:%s", league.LeaguePath, ev.ID)
				eventID, err := s.store.UpsertSportsEvent(ctx, store.SportsEventUpsert{
					ExternalID: ext,
					Sport:      ev.Sport,
					Title:      ev.Away + " @ " + ev.Home,
					HomeTeam:   ev.Home,
					AwayTeam:   ev.Away,
					HomeTeamID: homeID,
					AwayTeamID: awayID,
					StartsAt:   starts,
					EndsAt:     &ends,
					Source:     "espn",
					Channels:   labels,
				})
				if err != nil {
					slog.Warn("espn upsert event", "id", ext, "err", err)
					continue
				}
				imported++

				for _, label := range labels {
					s.attachLabelChannels(ctx, eventID, ev.Sport, label, ev.Home, ev.Away, cands, true)
				}
				s.attachTeamChannels(ctx, eventID, ev.Sport, ev.Home, ev.Away, cands, starts, ends)

				if homeScore != "" || awayScore != "" || period != "" || status != "" {
					_ = s.store.UpdateSportsEventScore(ctx, ext, homeScore, awayScore, period, status)
				}
			}
			time.Sleep(300 * time.Millisecond)
		}
	}

	// MLB-only leftovers ESPN didn't return (rare).
	for key, g := range mlbByKey {
		if _, ok := coveredMLB[key]; ok {
			continue
		}
		ext := fmt.Sprintf("mlb:%d", g.GamePk)
		ends := g.StartsAt.Add(DefaultDuration("Baseball"))
		if strings.Contains(strings.ToLower(g.Status), "progress") || strings.EqualFold(g.Status, "Live") {
			if ends.Before(time.Now().Add(90 * time.Minute)) {
				ends = time.Now().Add(2 * time.Hour)
			}
		}
		homeID := s.ensureESPNTeam(ctx, "Baseball", "MLB", g.Home, "", "")
		awayID := s.ensureESPNTeam(ctx, "Baseball", "MLB", g.Away, "", "")
		labels := uniqueBroadcasts(g.Broadcasts)
		eventID, err := s.store.UpsertSportsEvent(ctx, store.SportsEventUpsert{
			ExternalID: ext,
			Sport:      "Baseball",
			Title:      g.Away + " @ " + g.Home,
			HomeTeam:   g.Home,
			AwayTeam:   g.Away,
			HomeTeamID: homeID,
			AwayTeamID: awayID,
			StartsAt:   g.StartsAt,
			EndsAt:     &ends,
			Source:     "mlb",
			Channels:   labels,
		})
		if err != nil {
			continue
		}
		imported++
		for _, label := range labels {
			s.attachLabelChannels(ctx, eventID, "Baseball", label, g.Home, g.Away, cands, true)
		}
		s.attachTeamChannels(ctx, eventID, "Baseball", g.Home, g.Away, cands, g.StartsAt, ends)
		if g.HomeScore != "" || g.AwayScore != "" || g.Period != "" {
			_ = s.store.UpdateSportsEventScore(ctx, ext, g.HomeScore, g.AwayScore, g.Period, g.Status)
		}
	}

	slog.Info("espn/mlb sync ok", "events", imported)
	return imported, nil
}

func (s *SyncService) ensureESPNTeam(ctx context.Context, sport, league, name, abbr, logo string) *uuid.UUID {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	badge := ""
	if s.logos != nil {
		if key := s.logos.LookupKey(sport, name); key != "" {
			badge = teamlogo.BadgeRef(key)
		} else if abbr != "" {
			// espn league path often matches pack keys (mlb/chw).
			lp := strings.ToLower(strings.TrimSpace(league))
			if lp != "" {
				try := lp + "/" + strings.ToLower(strings.TrimSpace(abbr))
				if _, err := s.logos.ResolveFile(try); err == nil {
					badge = teamlogo.BadgeRef(try)
				}
			}
		}
	}
	if badge == "" {
		badge = livetv.SanitizeLogoURL(logo)
	}
	if t, err := s.store.FindSportsTeamByName(ctx, sport, name); err == nil {
		need := badge != "" && (t.BadgeURL == "" || strings.Contains(t.BadgeURL, "/scoreboard/") ||
			(!strings.HasPrefix(t.BadgeURL, teamlogo.BadgePrefix) && strings.HasPrefix(badge, teamlogo.BadgePrefix)))
		if need {
			_, _ = s.store.UpsertSportsTeam(ctx, store.SportsTeam{
				ExternalID:     t.ExternalID,
				Sport:          sport,
				League:         league,
				Name:           name,
				NameAlternates: abbr,
				BadgeURL:       badge,
			})
		}
		id := t.ID
		return &id
	}
	ext := fmt.Sprintf("espn-team:%s:%s", strings.ToLower(league), normalizeKey(name))
	if abbr != "" {
		ext = fmt.Sprintf("espn-team:%s:%s", strings.ToLower(league), strings.ToLower(abbr))
	}
	t, err := s.store.UpsertSportsTeam(ctx, store.SportsTeam{
		ExternalID:     ext,
		Sport:          sport,
		League:         league,
		Name:           name,
		NameAlternates: abbr,
		BadgeURL:       badge,
	})
	if err != nil {
		return nil
	}
	return &t.ID
}

func teamPairKey(home, away string, start time.Time) string {
	a := teamToken(home)
	b := teamToken(away)
	if a > b {
		a, b = b, a
	}
	// 3-hour bucket — doubleheaders still usually differ by more.
	bucket := start.UTC().Truncate(3 * time.Hour).Format("20060102T15")
	return a + "|" + b + "|" + bucket
}

// scoreboardDays returns ESPN calendar days to query. Football week covers upcoming NFL/CFL slate.
func scoreboardDays(now time.Time, footballWeek bool) []time.Time {
	// Empty time.Time = ESPN "default" board (live / today).
	days := []time.Time{{}, now.Add(-24 * time.Hour), now, now.Add(24 * time.Hour)}
	if footballWeek {
		for i := 2; i <= 7; i++ {
			days = append(days, now.Add(time.Duration(i)*24*time.Hour))
		}
	}
	return days
}

func uniqueBroadcasts(in []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, raw := range in {
		for _, label := range expandBroadcastLabels(raw) {
			k := strings.ToLower(label)
			if _, ok := seen[k]; ok {
				continue
			}
			seen[k] = struct{}{}
			out = append(out, label)
		}
	}
	return out
}

// pollESPNScores refreshes live Big-4 scoreboards between full syncs.
func (s *SyncService) pollESPNScores(ctx context.Context) {
	if s.espn == nil {
		s.espn = NewESPNClient()
	}
	for _, league := range ESPNBig4 {
		events, err := s.espn.Scoreboard(ctx, league, time.Time{})
		if err != nil {
			continue
		}
		for _, ev := range events {
			ext := fmt.Sprintf("espn:%s:%s", league.LeaguePath, ev.ID)
			live := ev.Status == "in" || strings.Contains(strings.ToLower(ev.Period), "progress") ||
				strings.Contains(strings.ToLower(ev.Status), "progress")
			if live || ev.Status == "post" {
				_ = s.store.UpdateSportsEventScore(ctx, ext, ev.HomeScore, ev.AwayScore, ev.Period, ev.Status)
			}
			if live {
				_ = s.store.ExtendSportsEventEnd(ctx, ext, time.Now().Add(2*time.Hour))
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	if s.mlb == nil {
		s.mlb = NewMLBClient()
	}
	games, err := s.mlb.Schedule(ctx, time.Now().UTC())
	if err != nil {
		return
	}
	for _, g := range games {
		live := strings.Contains(strings.ToLower(g.Status), "progress") || strings.EqualFold(g.Status, "Live")
		_ = s.store.UpdateSportsEventScoreByTeams(ctx, "Baseball", g.Home, g.Away, g.StartsAt, g.HomeScore, g.AwayScore, g.Period, g.Status)
		if live {
			_ = s.store.ExtendSportsEventEndByTeams(ctx, "Baseball", g.Home, g.Away, g.StartsAt, time.Now().Add(2*time.Hour))
		}
	}
}
