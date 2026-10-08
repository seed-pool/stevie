package sportsdb

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/stevie-media/stevie/apps/server/internal/store"
)

// rematchSportsChannels clears IPTV category-header links and re-scores broadcasts
// against real linear channels (preferring US:/CA: feeds), expanding to all strong hits.
func (s *SyncService) rematchSportsChannels(ctx context.Context) {
	cleared, err := s.store.ClearCategoryHeaderSportsMatches(ctx)
	if err != nil {
		slog.Warn("sports clear category headers", "err", err)
	} else if cleared > 0 {
		slog.Info("sports cleared category-header channel links", "n", cleared)
	}

	liveChannels, err := s.store.ListLiveChannelsForMatch(ctx)
	if err != nil {
		slog.Warn("sports rematch list channels", "err", err)
		return
	}
	cands := BuildCandidates(liveChannels)
	if len(cands) == 0 {
		return
	}

	from := time.Now().Add(-6 * time.Hour)
	to := time.Now().Add(36 * time.Hour)
	events, err := s.store.ListSportsEvents(ctx, store.SportsListOpts{
		From:  from,
		To:    to,
		Limit: 500,
	})
	if err != nil {
		slog.Warn("sports rematch list events", "err", err)
		return
	}

	for _, ev := range events {
		if err := ctx.Err(); err != nil {
			return
		}
		// EPG cards already carry the airing channel id(s) from fillFromEPG — don't wipe them.
		if strings.EqualFold(ev.Source, "epg") {
			continue
		}
		labels := originalBroadcastLabels(ev.Channels)
		preserved := preserveDirectChannelLinks(ev.Channels)
		_ = s.store.ClearSportsEventChannels(ctx, ev.ID)
		fromProvider := strings.HasPrefix(ev.ExternalID, "espn:") || strings.HasPrefix(ev.ExternalID, "mlb:") ||
			strings.EqualFold(ev.Source, "espn") || strings.EqualFold(ev.Source, "mlb")
		for _, label := range labels {
			confirmed := fromProvider && isProviderBroadcastLabel(label)
			s.attachLabelChannels(ctx, ev.ID, ev.Sport, label, ev.HomeTeam, ev.AwayTeam, cands, confirmed)
		}
		ends := ev.StartsAt.Add(DefaultDuration(ev.Sport))
		if ev.EndsAt != nil && ev.EndsAt.After(ev.StartsAt) {
			ends = *ev.EndsAt
		}
		s.attachTeamChannels(ctx, ev.ID, ev.Sport, ev.HomeTeam, ev.AwayTeam, cands, ev.StartsAt, ends)
		// Keep any direct EPG-linked rows that were merged onto ESPN/MLB cards.
		for _, p := range preserved {
			id := p.LiveChannelID
			if id == nil {
				continue
			}
			label := p.BroadcastLabel
			if label == "" {
				label = p.ChannelName
			}
			_ = s.store.UpsertSportsEventChannel(ctx, ev.ID, label, id, p.MatchScore)
		}
	}
	if len(events) > 0 {
		slog.Info("sports rematch channels", "events", len(events), "candidates", len(cands))
	}
}

func originalBroadcastLabels(chs []store.SportsEventChannel) []string {
	var out []string
	seen := map[string]struct{}{}
	for _, ch := range chs {
		label := strings.TrimSpace(ch.BroadcastLabel)
		if label == "" {
			continue
		}
		// Expanded rows use the live channel's own name as the label.
		if ch.ChannelName != "" && strings.EqualFold(label, ch.ChannelName) {
			continue
		}
		// Heuristic: library channel titles are long / colon-prefixed.
		if strings.Count(label, ":") >= 1 && len(label) > 22 {
			continue
		}
		if strings.Contains(label, " | ") || strings.Contains(strings.ToUpper(label), "FLSP") {
			continue
		}
		key := strings.ToLower(label)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, label)
	}
	return out
}

// preserveDirectChannelLinks keeps EPG-linked rows (scores 0.85–1.0) whose label is
// the channel name. Speculative team feeds score ~1.1–1.4 and must NOT be preserved —
// they are re-evaluated via attachTeamChannels (EPG-confirmed only).
func preserveDirectChannelLinks(chs []store.SportsEventChannel) []store.SportsEventChannel {
	var out []store.SportsEventChannel
	seen := map[uuid.UUID]struct{}{}
	for _, ch := range chs {
		if ch.LiveChannelID == nil || *ch.LiveChannelID == uuid.Nil {
			continue
		}
		// fillFromEPG / event_channels use 0.85, 0.9, 1.0 — nothing above 1.0.
		if ch.MatchScore < 0.85 || ch.MatchScore > 1.0 {
			continue
		}
		if ch.ChannelName == "" || !strings.EqualFold(ch.BroadcastLabel, ch.ChannelName) {
			continue
		}
		if _, ok := seen[*ch.LiveChannelID]; ok {
			continue
		}
		seen[*ch.LiveChannelID] = struct{}{}
		out = append(out, ch)
	}
	return out
}
