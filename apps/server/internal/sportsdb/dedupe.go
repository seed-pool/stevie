package sportsdb

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/stevie-media/stevie/apps/server/internal/store"
)

// dedupeSportsEvents collapses same-match cards (e.g. ESPN + SportsDB Dodgers),
// keeping the scored / higher-quality row and moving channels onto it.
func (s *SyncService) dedupeSportsEvents(ctx context.Context) {
	from := time.Now().Add(-12 * time.Hour)
	to := time.Now().Add(72 * time.Hour)
	events, err := s.store.ListSportsEvents(ctx, store.SportsListOpts{
		From:  from,
		To:    to,
		Limit: 2000,
	})
	if err != nil || len(events) < 2 {
		return
	}

	groups := map[string][]store.SportsEvent{}
	for _, e := range events {
		if e.HomeTeam == "" || e.AwayTeam == "" {
			continue
		}
		key := strings.ToLower(e.Sport) + "|" + teamPairKey(e.HomeTeam, e.AwayTeam, e.StartsAt)
		groups[key] = append(groups[key], e)
	}

	dropped := 0
	for _, group := range groups {
		if len(group) < 2 {
			continue
		}
		winner := pickBestSportsEvent(group)
		for _, loser := range group {
			if loser.ID == winner.ID {
				continue
			}
			_ = s.store.MoveSportsEventChannels(ctx, loser.ID, winner.ID)
			if err := s.store.DeleteSportsEvent(ctx, loser.ID); err != nil {
				slog.Warn("sports dedupe delete", "id", loser.ID, "err", err)
				continue
			}
			dropped++
		}
	}
	if dropped > 0 {
		slog.Info("sports dedupe", "dropped", dropped)
	}
}

func pickBestSportsEvent(group []store.SportsEvent) store.SportsEvent {
	best := group[0]
	bestQ := sportsEventQuality(best)
	for _, e := range group[1:] {
		q := sportsEventQuality(e)
		if q > bestQ {
			best, bestQ = e, q
			continue
		}
		if q == bestQ && e.StartsAt.Before(best.StartsAt) {
			// Stable pick — earlier insert often the canonical ESPN row.
			best = e
		}
	}
	return best
}

func sportsEventQuality(e store.SportsEvent) int {
	q := 0
	if hasMeaningfulScore(e) {
		q += 1000
	}
	switch strings.ToLower(strings.TrimSpace(e.Source)) {
	case "espn":
		q += 120
	case "mlb":
		q += 110
	case "thesportsdb":
		q += 40
	case "epg":
		q += 20
	}
	if e.HomeTeamID != nil {
		q += 5
	}
	if e.AwayTeamID != nil {
		q += 5
	}
	playable := 0
	for _, ch := range e.Channels {
		if ch.Playable || ch.LiveChannelID != nil {
			playable++
		}
	}
	q += playable
	return q
}

func hasMeaningfulScore(e store.SportsEvent) bool {
	if strings.TrimSpace(e.HomeScore) != "" || strings.TrimSpace(e.AwayScore) != "" {
		return true
	}
	if strings.TrimSpace(e.Period) != "" {
		return true
	}
	st := strings.ToLower(strings.TrimSpace(e.Status))
	if st == "" {
		return false
	}
	switch {
	case st == "in", st == "post", st == "live", st == "final", st == "scheduled", st == "pre":
		return true
	case strings.Contains(st, "progress"), strings.Contains(st, "final"),
		strings.Contains(st, "live"), strings.Contains(st, "halftime"):
		return true
	default:
		return false
	}
}

// DedupedSportsEvents filters a list the same way sync cleanup does (API safety net).
func DedupedSportsEvents(events []store.SportsEvent) []store.SportsEvent {
	if len(events) < 2 {
		return events
	}
	groups := map[string][]int{}
	order := make([]string, 0)
	for i, e := range events {
		if e.HomeTeam == "" || e.AwayTeam == "" {
			key := "id:" + e.ID.String()
			groups[key] = []int{i}
			order = append(order, key)
			continue
		}
		key := strings.ToLower(e.Sport) + "|" + teamPairKey(e.HomeTeam, e.AwayTeam, e.StartsAt)
		if _, ok := groups[key]; !ok {
			order = append(order, key)
		}
		groups[key] = append(groups[key], i)
	}
	keep := map[uuid.UUID]struct{}{}
	for _, key := range order {
		idxs := groups[key]
		if len(idxs) == 1 {
			keep[events[idxs[0]].ID] = struct{}{}
			continue
		}
		group := make([]store.SportsEvent, 0, len(idxs))
		for _, i := range idxs {
			group = append(group, events[i])
		}
		keep[pickBestSportsEvent(group).ID] = struct{}{}
	}
	out := make([]store.SportsEvent, 0, len(keep))
	for _, e := range events {
		if _, ok := keep[e.ID]; ok {
			out = append(out, e)
		}
	}
	return out
}
