package sportsdb

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/stevie-media/stevie/apps/server/internal/store"
)

// confirmedBroadcastBoost lifts ESPN/MLB-listed networks above team/RSN guesses.
const confirmedBroadcastBoost = 10.0

// attachLabelChannels links every strong library match for a broadcast label.
// When confirmed is true (ESPN/MLB listed the network), matched feeds sort to the top.
func (s *SyncService) attachLabelChannels(ctx context.Context, eventID uuid.UUID, sport, label, home, away string, cands []channelCandidate, confirmed bool) {
	label = strings.TrimSpace(label)
	if label == "" || eventID == uuid.Nil {
		return
	}
	// Don't expand a BeIN label into library twins for Big-4 — those feeds rarely carry the game.
	if rejectChannelForSport(sport, label) {
		return
	}
	aliasID, hasAlias, _ := s.store.SportsChannelAlias(ctx, label)
	opts := MatchOpts{AliasID: aliasID, HasAlias: hasAlias, Limit: 32, Home: home, Away: away}
	matches := MatchAllChannels(label, cands, opts)
	if len(matches) == 0 {
		for _, alt := range expandBroadcastLabels(label) {
			if alt == label {
				continue
			}
			aliasID, hasAlias, _ = s.store.SportsChannelAlias(ctx, alt)
			opts.AliasID, opts.HasAlias = aliasID, hasAlias
			matches = MatchAllChannels(alt, cands, opts)
			if len(matches) > 0 {
				break
			}
		}
	}
	matches = filterMatchesForSport(sport, matches)
	if len(matches) == 0 {
		score := 0.0
		if confirmed {
			// Keep unplayable ESPN labels visible/sorted above random EPG noise.
			score = confirmedBroadcastBoost
		}
		_ = s.store.UpsertSportsEventChannel(ctx, eventID, label, nil, score)
		return
	}
	boost := 0.0
	if confirmed {
		boost = confirmedBroadcastBoost
	}
	// Primary row keeps the broadcast label → best feed.
	best := matches[0]
	_ = s.store.UpsertSportsEventChannel(ctx, eventID, label, &best.ID, best.Score+boost)
	// Additional regional/HD twins use the channel name as the row label.
	for _, m := range matches[1:] {
		rowLabel := m.Name
		if strings.EqualFold(rowLabel, label) {
			continue
		}
		id := m.ID
		_ = s.store.UpsertSportsEventChannel(ctx, eventID, rowLabel, &id, m.Score+boost)
	}
}

// attachTeamChannels adds league team feeds / RSNs only when EPG on that channel
// confirms this matchup. Name-only matching (NBA: TEAM, AT&T: TEAM ᴿᴬᵂ) is too
// speculative — those feeds often idle or carry a different game.
func (s *SyncService) attachTeamChannels(ctx context.Context, eventID uuid.UUID, sport, home, away string, cands []channelCandidate, startsAt, endsAt time.Time) {
	if eventID == uuid.Nil || home == "" || away == "" {
		return
	}
	matches := filterMatchesForSport(sport, TeamFeedMatches(sport, home, away, cands, 20))
	if len(matches) == 0 {
		return
	}
	ids := make([]uuid.UUID, 0, len(matches))
	for _, m := range matches {
		ids = append(ids, m.ID)
	}
	from := startsAt
	if from.IsZero() {
		from = time.Now().Add(-30 * time.Minute)
	} else {
		from = from.Add(-30 * time.Minute)
	}
	to := endsAt
	if to.IsZero() || !to.After(startsAt) {
		to = startsAt.Add(DefaultDuration(sport))
	}
	to = to.Add(30 * time.Minute)
	hits, err := s.store.ListEPGHitsForChannels(ctx, ids, from, to)
	if err != nil || len(hits) == 0 {
		return
	}
	byCh := map[uuid.UUID][]store.EPGSportsHit{}
	for _, h := range hits {
		byCh[h.ChannelID] = append(byCh[h.ChannelID], h)
	}
	for _, m := range matches {
		if !epgConfirmsTeamFeed(sport, home, away, byCh[m.ID]) {
			continue
		}
		id := m.ID
		_ = s.store.UpsertSportsEventChannel(ctx, eventID, m.Name, &id, m.Score)
	}
}

// epgConfirmsTeamFeed requires the overlapping programme to name both sides of
// the matchup (not a generic "NBA Basketball" slate or a different opponent).
func epgConfirmsTeamFeed(sport, home, away string, hits []store.EPGSportsHit) bool {
	homeTok := teamToken(home)
	awayTok := teamToken(away)
	if homeTok == "" || awayTok == "" || homeTok == "tbd" || awayTok == "tbd" {
		return false
	}
	for _, h := range hits {
		blob := normalizeKey(h.Title + " " + h.Description)
		if strings.Contains(blob, homeTok) && strings.Contains(blob, awayTok) {
			if ph, pa := ParseEventTeams(h.Title); ph != "" && pa != "" {
				if !isPlausibleSportsMatchup(sport, ph, pa, h.Title) {
					continue
				}
			}
			return true
		}
		ph, pa := ParseEventTeams(h.Title)
		if ph == "" || pa == "" || !isPlausibleSportsMatchup(sport, ph, pa, h.Title) {
			continue
		}
		if sameTeamPair(ph, pa, home, away) {
			return true
		}
	}
	return false
}

func sameTeamPair(aHome, aAway, bHome, bAway string) bool {
	ah, aa := teamToken(aHome), teamToken(aAway)
	bh, ba := teamToken(bHome), teamToken(bAway)
	if ah == "" || aa == "" || bh == "" || ba == "" {
		return false
	}
	return (ah == bh && aa == ba) || (ah == ba && aa == bh)
}

func filterMatchesForSport(sport string, matches []ChannelMatch) []ChannelMatch {
	if len(matches) == 0 {
		return matches
	}
	out := matches[:0]
	for _, m := range matches {
		if rejectChannelForSport(sport, m.Name) {
			continue
		}
		out = append(out, m)
	}
	return out
}

// isProviderBroadcastLabel is true for ESPN/MLB network names ("FS1", "TBS"),
// not resolved library titles ("US: FOX SPORTS 1") or team feeds.
func isProviderBroadcastLabel(label string) bool {
	label = strings.TrimSpace(label)
	if label == "" || strings.Contains(label, " | ") || isEventSlotChannel(label) {
		return false
	}
	if i := strings.Index(label, ": "); i > 0 && i <= 8 {
		pkg := label[:i]
		if len(pkg) <= 6 && !strings.Contains(pkg, " ") {
			return false
		}
	}
	return true
}

// IsConfirmedBroadcast reports channels attached from an ESPN/MLB listing (boosted score).
func IsConfirmedBroadcast(matchScore float64) bool {
	return matchScore >= confirmedBroadcastBoost
}
