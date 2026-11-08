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

// fillNBAEventFeeds attaches NBA Pass PPV slots and NBA TEAM| linear feeds onto
// Basketball cards. These IPTV titles encode the matchup but are skipped by
// normal rematch (event-slot / no EPG on team feeds).
func (s *SyncService) fillNBAEventFeeds(ctx context.Context) int {
	if s == nil || s.store == nil {
		return 0
	}
	from := time.Now().Add(-6 * time.Hour)
	to := time.Now().Add(36 * time.Hour)
	events, err := s.store.ListSportsEvents(ctx, store.SportsListOpts{
		Sport: "Basketball",
		From:  from,
		To:    to,
		Limit: 300,
	})
	if err != nil || len(events) == 0 {
		return 0
	}
	liveChannels, err := s.store.ListLiveChannelsForMatch(ctx)
	if err != nil {
		return 0
	}

	var pass []store.LiveChannel
	var teams []store.LiveChannel
	for _, ch := range liveChannels {
		if IsCategoryHeaderChannel(ch.Name) {
			continue
		}
		switch {
		case isNBAPassPPVChannel(ch.Name):
			pass = append(pass, ch)
		case isNBATeamFeedChannel(ch.Name):
			teams = append(teams, ch)
		}
	}
	if len(pass) == 0 && len(teams) == 0 {
		return 0
	}

	attached := 0
	for _, ev := range events {
		if err := ctx.Err(); err != nil {
			break
		}
		if strings.TrimSpace(ev.HomeTeam) == "" || strings.TrimSpace(ev.AwayTeam) == "" {
			continue
		}
		if isBareNBACityCard(ev.Sport, ev.HomeTeam, ev.AwayTeam) {
			continue
		}
		homeNeedles := nbaTeamNeedles(ev.HomeTeam)
		awayNeedles := nbaTeamNeedles(ev.AwayTeam)
		if len(homeNeedles) == 0 || len(awayNeedles) == 0 {
			continue
		}

		seen := map[uuid.UUID]struct{}{}
		for _, ch := range pass {
			if _, ok := seen[ch.ID]; ok {
				continue
			}
			if !nbaPassMatchesPair(ch.Name, homeNeedles, awayNeedles) {
				continue
			}
			id := ch.ID
			if err := s.store.UpsertSportsEventChannel(ctx, ev.ID, ch.Name, &id, 0.97); err == nil {
				seen[ch.ID] = struct{}{}
				attached++
			}
		}
		for _, ch := range teams {
			if _, ok := seen[ch.ID]; ok {
				continue
			}
			if !nbaTeamFeedMatchesSide(ch.Name, homeNeedles) && !nbaTeamFeedMatchesSide(ch.Name, awayNeedles) {
				continue
			}
			id := ch.ID
			if err := s.store.UpsertSportsEventChannel(ctx, ev.ID, ch.Name, &id, 0.92); err == nil {
				seen[ch.ID] = struct{}{}
				attached++
			}
		}
	}
	if attached > 0 {
		slog.Info("sports nba feed attach", "links", attached, "events", len(events),
			"pass", len(pass), "teams", len(teams))
	}
	return attached
}

func isNBAPassPPVChannel(name string) bool {
	u := strings.ToUpper(strings.TrimSpace(name))
	if u == "" || strings.Contains(u, "NO EVENT STREAMING") || strings.Contains(u, "ENDED |") ||
		strings.HasPrefix(u, "END |") {
		return false
	}
	if strings.Contains(u, "#######") {
		return false
	}
	return strings.Contains(u, "NBA PASS")
}

func isNBATeamFeedChannel(name string) bool {
	u := strings.ToUpper(strings.TrimSpace(name))
	return strings.HasPrefix(u, "NBA TEAM|") || strings.HasPrefix(u, "NBA TEAM:") ||
		strings.HasPrefix(u, "NBA TEAM ")
}

func nbaPassMatchesPair(name string, home, away []string) bool {
	blob := nbaCompact(name)
	return nbaNeedlesHit(blob, home) && nbaNeedlesHit(blob, away)
}

func nbaTeamFeedMatchesSide(name string, side []string) bool {
	// "NBA TEAM| PORTLAND TRAIL BLAZERS" → brand after package.
	_, brand := splitPackageBrand(name)
	if brand == "" {
		brand = nbaCompact(name)
	}
	return nbaNeedlesHit(brand, side)
}

func nbaNeedlesHit(blob string, needles []string) bool {
	for _, n := range needles {
		if n != "" && strings.Contains(blob, n) {
			return true
		}
	}
	return false
}

func nbaCompact(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// nbaTeamNeedles returns compact tokens that identify a club in IPTV titles
// (full name, nickname, and common ESPN abbreviations).
func nbaTeamNeedles(name string) []string {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	tok := teamToken(name)
	full := nbaCompact(name)
	out := make([]string, 0, 6)
	seen := map[string]struct{}{}
	add := func(s string) {
		s = nbaCompact(s)
		if len(s) < 3 {
			return
		}
		if _, ok := seen[s]; ok {
			return
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	add(full)
	add(tok)
	if fields := strings.Fields(foldTeam(name)); len(fields) >= 2 {
		add(strings.Join(fields[len(fields)-2:], ""))
	}
	if abbr := nbaAbbrev(tok, full); abbr != "" {
		add(abbr)
	}
	return out
}

func nbaAbbrev(tok, full string) string {
	switch tok {
	case "hawks":
		return "atl"
	case "celtics":
		return "bos"
	case "nets":
		return "bkn"
	case "hornets":
		return "cha"
	case "bulls":
		return "chi"
	case "cavaliers":
		return "cle"
	case "mavericks":
		return "dal"
	case "nuggets":
		return "den"
	case "pistons":
		return "det"
	case "warriors":
		return "gsw"
	case "rockets":
		return "hou"
	case "pacers":
		return "ind"
	case "clippers":
		return "lac"
	case "lakers":
		return "lal"
	case "grizzlies":
		return "mem"
	case "heat":
		return "mia"
	case "bucks":
		return "mil"
	case "timberwolves":
		return "min"
	case "pelicans":
		return "nop"
	case "knicks":
		return "nyk"
	case "thunder":
		return "okc"
	case "magic":
		return "orl"
	case "76ers", "sixers":
		return "phi"
	case "suns":
		return "phx"
	case "blazers":
		return "por"
	case "kings":
		return "sac"
	case "spurs":
		return "sas"
	case "raptors":
		return "tor"
	case "jazz":
		return "uta"
	case "wizards":
		return "was"
	}
	_ = full
	return ""
}
