package sportsdb

import (
	"context"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/stevie-media/stevie/apps/server/internal/store"
)

var (
	eventChannelDateRe = regexp.MustCompile(`(?i)\b(mon|tue|wed|thu|fri|sat|sun)[a-z]*\s+(\d{1,2})(?:st|nd|rd|th)?\s+(jan|feb|mar|apr|may|jun|jul|aug|sep|oct|nov|dec)[a-z]*\s+(\d{1,2}):(\d{2})\s*(am|pm)?\s*(et|est|edt|pt|pst|pdt|ct|cst|cdt|mt|mst|mdt)?`)
	cflSlotRe          = regexp.MustCompile(`(?i)\bcfl\s*\d+`)
	eventChannelSlotRe = regexp.MustCompile(`(?i)^(?:[a-z]{1,4}\s*[|:]\s*)?(?:cfl|nfl)\s*\d*\s*[|:xⓧ]*\s*`)
)

// fillFromEventChannels builds sports cards from IPTV titles like
// "CA | CFL 00: Montreal Alouettes vs Hamilton Tiger-Cats | Sat 8th Nov 3:00 PM ET".
func (s *SyncService) fillFromEventChannels(ctx context.Context) int {
	s.pruneJunkEventChannelCards(ctx)

	channels, err := s.store.ListLiveChannelsForMatch(ctx)
	if err != nil {
		slog.Warn("sports event-channel list", "err", err)
		return 0
	}
	imported := 0
	now := time.Now()
	for _, ch := range channels {
		if IsCategoryHeaderChannel(ch.Name) || isJunkFootballEventChannel(ch.Name) {
			continue
		}
		sport, home, away, starts, ok := parseEventChannelName(ch.Name, now)
		if !ok {
			continue
		}
		// CFL playoffs can sit a few weeks out on static slot channels.
		if starts.Before(now.Add(-6*time.Hour)) || starts.After(now.Add(60*24*time.Hour)) {
			continue
		}
		ends := starts.Add(DefaultDuration(sport))
		title := away + " @ " + home
		if strings.Contains(strings.ToLower(ch.Name), " vs ") {
			title = home + " vs " + away
		}
		key := matchKey(sport, home, away, starts)
		ext := "ch:" + key
		eventID, err := s.store.UpsertSportsEvent(ctx, store.SportsEventUpsert{
			ExternalID: ext,
			Sport:      sport,
			Title:      title,
			HomeTeam:   home,
			AwayTeam:   away,
			StartsAt:   starts,
			EndsAt:     &ends,
			Source:     "epg",
		})
		if err != nil {
			continue
		}
		imported++
		if !starts.After(now) && ends.After(now) {
			_ = s.store.UpdateSportsEventScore(ctx, ext, "", "", "", "in")
		}
		chID := ch.ID
		_ = s.store.UpsertSportsEventChannel(ctx, eventID, ch.Name, &chID, 1.0)
		if t, err := s.store.FindSportsTeamByName(ctx, sport, home); err == nil {
			_ = s.store.LinkSportsEventTeam(ctx, eventID, true, t.ID)
		}
		if t, err := s.store.FindSportsTeamByName(ctx, sport, away); err == nil {
			_ = s.store.LinkSportsEventTeam(ctx, eventID, false, t.ID)
		}
	}
	if imported > 0 {
		slog.Info("sports event-channel fill", "events", imported)
	}
	return imported
}

func parseEventChannelName(name string, now time.Time) (sport, home, away string, starts time.Time, ok bool) {
	raw := strings.TrimSpace(name)
	upper := strings.ToUpper(raw)
	if strings.Contains(upper, "NO SCHEDULED EVENT") || isJunkFootballEventChannel(raw) {
		return "", "", "", time.Time{}, false
	}
	// Only numbered CFL event slots for now (NFL comes from ESPN; IPTV NFL titles are mostly PPV/replays).
	if !cflSlotRe.MatchString(raw) {
		return "", "", "", time.Time{}, false
	}
	sport = "American Football"

	body := raw
	if i := strings.LastIndex(body, "|"); i > 0 {
		tail := strings.TrimSpace(body[i+1:])
		if eventChannelDateRe.MatchString(tail) {
			body = strings.TrimSpace(body[:i])
		}
	}
	body = eventChannelSlotRe.ReplaceAllString(body, "")
	body = strings.TrimSpace(body)
	body = strings.TrimLeft(body, ":|xⓧ ")
	body = strings.TrimSpace(body)

	home, away = ParseEventTeams(body)
	if home == "" || away == "" || !looksLikeMatchupSide(home) || !looksLikeMatchupSide(away) {
		return "", "", "", time.Time{}, false
	}

	m := eventChannelDateRe.FindStringSubmatch(raw)
	if len(m) < 7 {
		return "", "", "", time.Time{}, false
	}
	t, err := parseEventChannelDate(m, now)
	if err != nil {
		return "", "", "", time.Time{}, false
	}
	return sport, home, away, t, true
}

func isJunkFootballEventChannel(name string) bool {
	u := strings.ToUpper(name)
	for _, bad := range []string{
		"TSN+", "DISNEY+", "GAME PASS", "REDZONE", "RED ZONE",
		"END |", "ENDED |", "NEXT |", "LIVE |", "ON TSN:",
		"GAMEDAY", "FANTASY", "GOOD MORNING FOOTBALL", "NFL BLITZ",
		"8K EXCLUSIVE", "DAZN PPV", "SOCCER PPV",
	} {
		if strings.Contains(u, bad) {
			return true
		}
	}
	return false
}

func (s *SyncService) pruneJunkEventChannelCards(ctx context.Context) {
	events, err := s.store.ListSportsEvents(ctx, store.SportsListOpts{
		From:  time.Now().Add(-48 * time.Hour),
		To:    time.Now().Add(90 * 24 * time.Hour),
		Limit: 800,
	})
	if err != nil {
		return
	}
	n := 0
	for _, ev := range events {
		if !strings.EqualFold(ev.Source, "epg") || !strings.EqualFold(ev.Sport, "American Football") {
			continue
		}
		if strings.HasPrefix(ev.ExternalID, "ch:") && !cflSlotTitle(ev) {
			if err := s.store.DeleteSportsEvent(ctx, ev.ID); err == nil {
				n++
			}
			continue
		}
		// Drop mis-parsed replay titles that landed as epg American Football.
		if isJunkFootballEventChannel(ev.Title) {
			if err := s.store.DeleteSportsEvent(ctx, ev.ID); err == nil {
				n++
			}
		}
	}
	if n > 0 {
		slog.Info("sports pruned junk football event-channels", "n", n)
	}
}

func cflSlotTitle(ev store.SportsEvent) bool {
	for _, ch := range ev.Channels {
		if cflSlotRe.MatchString(ch.BroadcastLabel) || cflSlotRe.MatchString(ch.ChannelName) {
			return true
		}
	}
	u := strings.ToUpper(ev.Title + " " + ev.HomeTeam + " " + ev.AwayTeam)
	return strings.Contains(u, "ALOUETTE") || strings.Contains(u, "ROUGHRIDER") ||
		strings.Contains(u, "STAMPEDER") || strings.Contains(u, "TIGER-CAT") ||
		strings.Contains(u, "BLUE BOMBER") || strings.Contains(u, "ARGONAUT") ||
		strings.Contains(u, "REDBLACK") || strings.Contains(u, "ELK") ||
		strings.Contains(u, "LION") && strings.Contains(u, "B.C")
}

func parseEventChannelDate(m []string, now time.Time) (time.Time, error) {
	day, _ := strconv.Atoi(m[2])
	hour, _ := strconv.Atoi(m[4])
	min, _ := strconv.Atoi(m[5])
	ampm := strings.ToLower(m[6])
	if ampm == "pm" && hour < 12 {
		hour += 12
	}
	if ampm == "am" && hour == 12 {
		hour = 0
	}
	month := monthAbbrev(m[3])
	if month == 0 || day == 0 {
		return time.Time{}, strconv.ErrSyntax
	}
	year := now.Year()
	zone := strings.ToUpper(m[7])
	loc := time.UTC
	switch zone {
	case "ET", "EST", "EDT":
		if l, err := time.LoadLocation("America/New_York"); err == nil {
			loc = l
		}
	case "PT", "PST", "PDT":
		if l, err := time.LoadLocation("America/Los_Angeles"); err == nil {
			loc = l
		}
	case "CT", "CST", "CDT":
		if l, err := time.LoadLocation("America/Chicago"); err == nil {
			loc = l
		}
	case "MT", "MST", "MDT":
		if l, err := time.LoadLocation("America/Denver"); err == nil {
			loc = l
		}
	}
	cand := time.Date(year, month, day, hour, min, 0, 0, loc)
	if cand.Before(now.Add(-48 * time.Hour)) {
		cand = time.Date(year+1, month, day, hour, min, 0, 0, loc)
	}
	return cand, nil
}

func monthAbbrev(s string) time.Month {
	s = strings.ToLower(strings.TrimSpace(s))
	if len(s) < 3 {
		return 0
	}
	switch s[:3] {
	case "jan":
		return time.January
	case "feb":
		return time.February
	case "mar":
		return time.March
	case "apr":
		return time.April
	case "may":
		return time.May
	case "jun":
		return time.June
	case "jul":
		return time.July
	case "aug":
		return time.August
	case "sep":
		return time.September
	case "oct":
		return time.October
	case "nov":
		return time.November
	case "dec":
		return time.December
	default:
		return 0
	}
}

// normalizeSportNames migrates legacy labels (Soccer → Football).
func (s *SyncService) normalizeSportNames(ctx context.Context) {
	n, err := s.store.RenameSportsLabel(ctx, "Soccer", "Football")
	if err != nil {
		slog.Warn("sports rename Soccer→Football", "err", err)
		return
	}
	if n > 0 {
		slog.Info("sports renamed Soccer to Football", "rows", n)
	}
}
