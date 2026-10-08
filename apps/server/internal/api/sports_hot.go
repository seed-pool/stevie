package api

import (
	"context"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/stevie-media/stevie/apps/server/internal/livetv"
	"github.com/stevie-media/stevie/apps/server/internal/streamed"
)

const (
	hotLimit       = 8
	hotMinScore    = 28
	hotMinCards    = 1
	hotFightWindow = 7 * 24 * time.Hour
)

type hotScored struct {
	ev    sportsEventDTO
	score int
}

// pickHotEvents ranks currently live cards for the Sports "Hot" strip.
// streamed.pk has no viewer counts — we use live membership, source count,
// popular flag, channel demand, major leagues, and an explicit UFC/PPV boost.
func (s *Server) pickHotEvents(ctx context.Context, events []sportsEventDTO, sportFilter string) []sportsEventDTO {
	var matches []streamed.Match
	var liveIDs map[string]struct{}
	if s.streamed != nil {
		if m, err := s.streamed.AllMatches(ctx); err == nil {
			matches = m
		}
		liveIDs, _ = s.streamed.LiveMatchIDs(ctx)
	}
	var inject []sportsEventDTO
	now := time.Now().UTC()
	for _, m := range matches {
		if !isHotFightOrPPVMatch(m) {
			continue
		}
		if sportFilter != "" && !sportFilterAllowsFight(sportFilter) {
			continue
		}
		dto := s.streamedFightToDTO(ctx, m, liveIDs, now)
		if dto.ID == "" || !dto.Live {
			continue
		}
		// Always-on PPV stubs without a start time only count when streamed lists them live.
		if _, hasStart := streamed.MatchStart(m); !hasStart {
			if _, onLive := liveIDs[m.ID]; !onLive {
				continue
			}
		}
		inject = append(inject, dto)
	}
	return mergeHotCandidates(events, inject, matches, liveIDs, sportFilter, now)
}

func mergeHotCandidates(
	events, injected []sportsEventDTO,
	matches []streamed.Match,
	liveIDs map[string]struct{},
	sportFilter string,
	now time.Time,
) []sportsEventDTO {
	var cands []hotScored
	seen := map[string]struct{}{}

	add := func(ev sportsEventDTO, sc int) {
		if !ev.Live || sc < hotMinScore {
			return
		}
		key := hotDedupeKey(ev)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		cands = append(cands, hotScored{ev: ev, score: sc})
	}

	for _, ev := range events {
		if strings.EqualFold(ev.Sport, streamed.ExternalSportLabel) {
			continue
		}
		if sportFilter != "" && !strings.EqualFold(ev.Sport, sportFilter) {
			continue
		}
		add(ev, scoreHotEvent(ev, matches, liveIDs, now))
	}
	for _, dto := range injected {
		if !dto.Live {
			continue
		}
		// Injected rows are already fight/PPV DTOs; score with a synthetic match when possible.
		sc := 140 // live base
		if isFightOrPPVEvent(dto) {
			sc += 55
		}
		add(dto, sc)
	}

	sort.SliceStable(cands, func(i, j int) bool {
		if cands[i].score != cands[j].score {
			return cands[i].score > cands[j].score
		}
		if cands[i].ev.Live != cands[j].ev.Live {
			return cands[i].ev.Live
		}
		return cands[i].ev.StartsAt < cands[j].ev.StartsAt
	})
	out := selectHotWithFightReserve(cands)
	if len(out) < hotMinCards {
		return nil
	}
	return out
}

// selectHotWithFightReserve keeps top Hot cards but always reserves up to 2
// slots for UFC/PPV/fight so live Big-4 nights don't hide the fight slate.
func selectHotWithFightReserve(cands []hotScored) []sportsEventDTO {
	if len(cands) == 0 {
		return nil
	}
	const fightReserve = 2
	generalSlots := hotLimit - fightReserve
	if generalSlots < 4 {
		generalSlots = hotLimit
	}

	var general, fights []hotScored
	for _, c := range cands {
		if isFightOrPPVEvent(c.ev) {
			fights = append(fights, c)
		} else {
			general = append(general, c)
		}
	}

	picked := make([]hotScored, 0, hotLimit)
	seen := map[string]struct{}{}
	take := func(list []hotScored, n int) {
		for _, c := range list {
			if n <= 0 {
				return
			}
			key := hotDedupeKey(c.ev)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			picked = append(picked, c)
			n--
		}
	}

	take(general, generalSlots)
	take(fights, fightReserve)
	// Fill remaining from overall ranked list.
	take(cands, hotLimit-len(picked))

	sort.SliceStable(picked, func(i, j int) bool {
		if picked[i].score != picked[j].score {
			return picked[i].score > picked[j].score
		}
		if picked[i].ev.Live != picked[j].ev.Live {
			return picked[i].ev.Live
		}
		return picked[i].ev.StartsAt < picked[j].ev.StartsAt
	})

	out := make([]sportsEventDTO, 0, len(picked))
	for _, c := range picked {
		out = append(out, c.ev)
	}
	return out
}

func sportFilterAllowsFight(sportFilter string) bool {
	switch strings.ToLower(strings.TrimSpace(sportFilter)) {
	case "fighting", "fight", "mma", "ufc", "boxing":
		return true
	default:
		return false
	}
}

func hotDedupeKey(ev sportsEventDTO) string {
	h := strings.ToLower(strings.TrimSpace(ev.Home.Name))
	a := strings.ToLower(strings.TrimSpace(ev.Away.Name))
	if h != "" && a != "" && h != strings.ToLower(ev.Title) && a != "main card" {
		if h > a {
			h, a = a, h
		}
		day := ev.StartsAt
		if len(day) >= 10 {
			day = day[:10]
		}
		return h + "|" + a + "|" + day
	}
	return strings.ToLower(strings.TrimSpace(ev.Title)) + "|" + ev.StartsAt
}

func scoreHotEvent(ev sportsEventDTO, matches []streamed.Match, liveIDs map[string]struct{}, now time.Time) int {
	score := 0
	if ev.Live {
		score += 100
	}
	starts, _ := time.Parse(time.RFC3339, ev.StartsAt)
	if ev.Upcoming && !starts.IsZero() && starts.Before(now.Add(3*time.Hour)) {
		score += 20
	}

	playable := 0
	for _, ch := range ev.Channels {
		if ch.Playable || ch.Source == "streamed" {
			playable++
		}
	}
	if playable > 6 {
		playable = 6
	}
	score += playable * 5

	if isMajorHotLeague(ev.League, ev.Sport) {
		score += 15
	}
	if isFightOrPPVEvent(ev) {
		score += 55
	}

	if len(matches) > 0 {
		m := streamed.FindMatch(matches, ev.Sport, ev.Home.Name, ev.Away.Name, starts)
		if m != nil {
			if liveIDs != nil {
				if _, ok := liveIDs[m.ID]; ok {
					score += 40
				}
			}
			if m.Popular {
				score += 10
			}
			n := len(m.Sources)
			if n > 4 {
				n = 4
			}
			score += n * 8
		}
	}
	return score
}

func scoreHotStreamedFight(m streamed.Match, liveIDs map[string]struct{}, dto sportsEventDTO, now time.Time) int {
	score := 40 // base — fight/PPV slate is inherently “hot”
	if dto.Live {
		score += 100
	}
	if liveIDs != nil {
		if _, ok := liveIDs[m.ID]; ok {
			score += 40
		}
	}
	if m.Popular {
		score += 10
	}
	n := len(m.Sources)
	if n > 4 {
		n = 4
	}
	score += n * 8
	starts, _ := time.Parse(time.RFC3339, dto.StartsAt)
	if dto.Upcoming && !starts.IsZero() && starts.Before(now.Add(3*time.Hour)) {
		score += 20
	}
	title := strings.ToLower(m.Title + " " + m.ID)
	if strings.Contains(title, "ufc") {
		score += 30
	}
	if strings.Contains(title, "boxing") || strings.Contains(title, "bellator") || strings.Contains(title, "pfl") {
		score += 20
	}
	return score
}

func isMajorHotLeague(league, sport string) bool {
	blob := strings.ToLower(league + " " + sport)
	for _, tok := range []string{
		"nhl", "nba", "mlb", "nfl", "cfl", "wnba",
		"premier league", "champions league",
		"formula 1", "f1", "ufc", "pga",
	} {
		if strings.Contains(blob, tok) {
			return true
		}
	}
	switch strings.ToLower(strings.TrimSpace(sport)) {
	case "ice hockey", "basketball", "baseball", "american football", "fighting":
		return strings.TrimSpace(league) != ""
	default:
		return false
	}
}

func isFightOrPPVEvent(ev sportsEventDTO) bool {
	blob := strings.ToLower(ev.Sport + " " + ev.League + " " + ev.Title + " " + ev.ExternalID)
	if strings.EqualFold(ev.Sport, "Fighting") {
		return true
	}
	for _, tok := range []string{"ufc", "bellator", "pfl ", " pfl", "boxing", "ppv", "fight night", "wwe", "aew"} {
		if strings.Contains(blob, tok) {
			return true
		}
	}
	for _, ch := range ev.Channels {
		n := strings.ToLower(ch.Name + " " + ch.BroadcastLabel)
		if strings.Contains(n, "ppv") || strings.Contains(n, "ufc") {
			return true
		}
	}
	return false
}

func isHotFightOrPPVMatch(m streamed.Match) bool {
	cat := strings.ToLower(strings.TrimSpace(m.Category))
	id := strings.ToLower(m.ID)
	title := strings.ToLower(m.Title)
	blob := title + " " + id

	if cat == "fight" {
		return true
	}
	for _, tok := range []string{"ufc", "bellator", "boxing", "fight night", "pfl africa", "pfl:"} {
		if strings.Contains(blob, tok) {
			return true
		}
	}
	// PPV fight/wrestling cards — skip linear “Network” stubs.
	if strings.HasPrefix(id, "ppv-") {
		if strings.Contains(blob, "network") || strings.Contains(blob, "sky sports") ||
			strings.Contains(blob, "fox league") || strings.Contains(blob, "fox cricket") {
			return false
		}
		if strings.Contains(blob, "wwe") || strings.Contains(blob, "aew") ||
			strings.Contains(blob, "ufc") || strings.Contains(blob, "boxing") ||
			strings.Contains(blob, "impact") || cat == "fight" {
			return true
		}
	}
	return false
}

func (s *Server) streamedFightToDTO(ctx context.Context, m streamed.Match, liveIDs map[string]struct{}, now time.Time) sportsEventDTO {
	home, away := streamed.MatchSideNames(m)
	title := strings.TrimSpace(m.Title)
	if title == "" {
		if home != "" && away != "" {
			title = away + " vs " + home
		} else {
			title = m.ID
		}
	}
	starts, hasStart := streamed.MatchStart(m)
	_, onLive := liveIDs[m.ID]
	const pregame = 30 * time.Minute
	const defaultLen = 4 * time.Hour
	var live, upcoming bool
	var ends time.Time
	if !hasStart {
		starts = now
		ends = now.Add(12 * time.Hour)
		live = onLive
		if !live {
			// Always-on PPV stubs still count as available.
			live = true
		}
	} else {
		if starts.Before(now.Add(-6*time.Hour)) || starts.After(now.Add(hotFightWindow)) {
			return sportsEventDTO{}
		}
		ends = starts.Add(defaultLen)
		airStart := starts.Add(-pregame)
		live = onLive || (!now.Before(airStart) && now.Before(ends))
		upcoming = now.Before(airStart)
		if !live && !upcoming {
			return sportsEventDTO{}
		}
	}
	if home == "" {
		home = title
	}
	if away == "" {
		away = "Main card"
	}
	league := inferLeagueFromTitle(title, "Fighting")
	if league == "" && strings.Contains(strings.ToLower(title+" "+m.ID), "ufc") {
		league = "UFC"
	}
	homeBadge := s.sportsBadgeURL("", "Fighting", home)
	awayBadge := s.sportsBadgeURL("", "Fighting", away)
	if homeBadge == "" && s.streamed != nil {
		if u := livetv.SanitizeLogoURL(s.streamed.AbsoluteURL(m.Poster)); u != "" {
			homeBadge = "/api/live/logo?url=" + url.QueryEscape(u)
		}
	}
	dto := sportsEventDTO{
		ID:            "streamed:" + m.ID,
		ExternalID:    m.ID,
		Sport:         "Fighting",
		Title:         title,
		Home:          sportsTeamDTO{Name: home, BadgeURL: homeBadge},
		Away:          sportsTeamDTO{Name: away, BadgeURL: awayBadge},
		StartsAt:      starts.UTC().Format(time.RFC3339),
		EndsAt:        ends.UTC().Format(time.RFC3339),
		Live:          live,
		Upcoming:      upcoming && !live,
		Channels:      []sportsChannelDTO{},
		League:        league,
		LeagueLogoURL: s.leagueLogoURL(league, "Fighting"),
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
	_ = ctx
	return dto
}
