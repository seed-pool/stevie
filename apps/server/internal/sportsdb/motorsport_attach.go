package sportsdb

import (
	"context"
	"log/slog"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/stevie-media/stevie/apps/server/internal/store"
)

// fillMotorSportFeeds attaches dedicated F1 / motorsport IPTV channels (Sky Sports F1,
// DAZN F1, …) onto MotorSport cards when overlapping EPG names the same GP/session.
// EPG "Sprint Race vs F1 Singapore…" cards only keep the source beIN rows otherwise.
func (s *SyncService) fillMotorSportFeeds(ctx context.Context) int {
	if s == nil || s.store == nil {
		return 0
	}
	from := time.Now().Add(-6 * time.Hour)
	to := time.Now().Add(36 * time.Hour)
	events, err := s.store.ListSportsEvents(ctx, store.SportsListOpts{
		Sport: "MotorSport",
		From:  from,
		To:    to,
		Limit: 200,
	})
	if err != nil || len(events) == 0 {
		return 0
	}
	liveChannels, err := s.store.ListLiveChannelsForMatch(ctx)
	if err != nil {
		return 0
	}
	var feedIDs []uuid.UUID
	byID := map[uuid.UUID]store.LiveChannel{}
	for _, ch := range liveChannels {
		if !isMotorSportFeedChannel(ch.Name) {
			continue
		}
		feedIDs = append(feedIDs, ch.ID)
		byID[ch.ID] = ch
	}
	if len(feedIDs) == 0 {
		return 0
	}

	attached := 0
	for _, ev := range events {
		if err := ctx.Err(); err != nil {
			break
		}
		ends := ev.StartsAt.Add(DefaultDuration(ev.Sport))
		if ev.EndsAt != nil && ev.EndsAt.After(ev.StartsAt) {
			ends = *ev.EndsAt
		}
		// Guides disagree on session windows (beIN vs Sky) — pad generously.
		windowFrom := ev.StartsAt.Add(-90 * time.Minute)
		windowTo := ends.Add(90 * time.Minute)
		hits, err := s.store.ListEPGHitsForChannels(ctx, feedIDs, windowFrom, windowTo)
		if err != nil || len(hits) == 0 {
			continue
		}
		seen := map[uuid.UUID]struct{}{}
		for _, h := range hits {
			if _, ok := seen[h.ChannelID]; ok {
				continue
			}
			if !motorEventRelated(ev.Title, ev.HomeTeam, ev.AwayTeam, h.Title, h.Description) {
				continue
			}
			ch, ok := byID[h.ChannelID]
			if !ok {
				continue
			}
			label := ch.Name
			if label == "" {
				label = h.ChannelName
			}
			id := h.ChannelID
			if err := s.store.UpsertSportsEventChannel(ctx, ev.ID, label, &id, 0.95); err == nil {
				seen[h.ChannelID] = struct{}{}
				attached++
			}
		}
	}
	if attached > 0 {
		slog.Info("sports motorsport feed attach", "links", attached, "events", len(events), "feeds", len(feedIDs))
	}
	return attached
}

func isMotorSportFeedChannel(name string) bool {
	compact := channelNameCompact(name)
	if compact == "" {
		return false
	}
	// Dedicated motorsport linear channels — not general Sky Sports 1/Main Event.
	needles := []string{
		"skysportsf1", "skysportf1", "skysportformula",
		"daznf1", "f1tv", "formula1",
		"supersportmotorsport", "motorsporttv",
		"skysportmotogp", "skysportsmotogp",
	}
	for _, n := range needles {
		if strings.Contains(compact, n) {
			return true
		}
	}
	// "SKY SPORT F1", "DAZN F1", "F1 TV" with separators stripped already in compact.
	if strings.Contains(compact, "f1") &&
		(strings.Contains(compact, "sky") || strings.Contains(compact, "dazn") ||
			strings.Contains(compact, "bein") || strings.Contains(compact, "now") ||
			strings.Contains(compact, "vip") || strings.Contains(compact, "wow") ||
			strings.Contains(compact, "skygo")) {
		return true
	}
	return false
}

func motorEventRelated(eventTitle, home, away, hitTitle, hitDesc string) bool {
	evBlob := motorFold(eventTitle + " " + home + " " + away)
	hitBlob := motorFold(hitTitle + " " + hitDesc)
	if evBlob == "" || hitBlob == "" {
		return false
	}
	evSession := motorSessionKind(evBlob)
	hitSession := motorSessionKind(hitBlob)
	if evSession != "" && hitSession != "" && evSession != hitSession {
		return false
	}
	evToks := motorTokens(evBlob)
	hitToks := motorTokens(hitBlob)
	shared := 0
	distinctive := false
	hitSet := map[string]struct{}{}
	for _, t := range hitToks {
		hitSet[t] = struct{}{}
	}
	for _, t := range evToks {
		if _, ok := hitSet[t]; !ok {
			continue
		}
		shared++
		if motorDistinctiveToken(t) {
			distinctive = true
		}
	}
	if shared >= 2 && distinctive {
		return true
	}
	// Same session on an F1 feed with one venue token (singapore + sprint).
	if distinctive && evSession != "" && evSession == hitSession && shared >= 1 {
		return true
	}
	// Generic "Formel 1" / "Live: Fórmula 1" on an F1 channel during the window —
	// only when the event is clearly F1 and the hit is not a different series.
	if isF1EventBlob(evBlob) && isGenericF1EPG(hitBlob) && !otherSeriesConflict(evBlob, hitBlob) {
		return evSession == "" || hitSession == "" || evSession == hitSession
	}
	return false
}

func motorFold(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	repl := strings.NewReplacer(
		"é", "e", "è", "e", "ê", "e", "ë", "e",
		"á", "a", "à", "a", "ä", "a", "â", "a",
		"í", "i", "ó", "o", "ú", "u", "ç", "c", "ñ", "n",
		"singapur", "singapore",
		"formel", "formula",
		"fórmula", "formula",
	)
	return repl.Replace(s)
}

func motorTokens(blob string) []string {
	var b strings.Builder
	for _, r := range blob {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		} else {
			b.WriteByte(' ')
		}
	}
	seen := map[string]struct{}{}
	var out []string
	for _, f := range strings.Fields(b.String()) {
		switch f {
		case "vs", "at", "the", "and", "for", "with", "from", "live", "race",
			"gp", "prix", "grand", "mundial", "codigo", "code", "airlines",
			"de", "di", "la", "el", "le":
			continue
		}
		if len(f) < 2 {
			continue
		}
		if len(f) == 2 && f != "f1" && f != "f2" && f != "f3" {
			continue
		}
		if _, ok := seen[f]; ok {
			continue
		}
		seen[f] = struct{}{}
		out = append(out, f)
	}
	return out
}

func motorDistinctiveToken(t string) bool {
	switch t {
	case "sprint", "qualifying", "quali", "practice", "warmup", "formula", "f1":
		return false
	default:
		return len(t) >= 5
	}
}

func motorSessionKind(blob string) string {
	compact := strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return r
		}
		return -1
	}, blob)
	switch {
	case strings.Contains(compact, "sprint"):
		return "sprint"
	case strings.Contains(compact, "qualifying"), strings.Contains(compact, "quali"), strings.Contains(compact, "superpole"):
		return "qualifying"
	case strings.Contains(compact, "practice"), strings.Contains(compact, "fp1"), strings.Contains(compact, "fp2"), strings.Contains(compact, "fp3"):
		return "practice"
	case strings.Contains(compact, "warmup"):
		return "warmup"
	default:
		return ""
	}
}

func isF1EventBlob(blob string) bool {
	return strings.Contains(blob, "f1") || strings.Contains(blob, "formula 1") ||
		strings.Contains(blob, "formula1") || strings.Contains(blob, "grand prix")
}

func isGenericF1EPG(blob string) bool {
	compact := strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return r
		}
		return -1
	}, blob)
	return compact == "formula1" || compact == "f1" || compact == "liveformula1" ||
		compact == "livef1" || strings.HasPrefix(compact, "formula1") && len(compact) <= 12
}

func otherSeriesConflict(ev, hit string) bool {
	for _, series := range []string{"nascar", "motogp", "moto3", "moto2", "indycar", "supercars", "dtm", "wec"} {
		hitHas := strings.Contains(hit, series)
		evHas := strings.Contains(ev, series)
		if hitHas && !evHas {
			return true
		}
	}
	return false
}
