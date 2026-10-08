package sportsdb

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/stevie-media/stevie/apps/server/internal/store"
)

type epgMatch struct {
	sport, home, away, title string
	starts, ends             time.Time
	channels                 map[uuid.UUID]store.EPGSportsHit
}

func (s *SyncService) fillFromEPG(ctx context.Context) (int, error) {
	s.pruneJunkEPGEvents(ctx)

	from := time.Now().Add(-4 * time.Hour)
	to := time.Now().Add(36 * time.Hour)
	hits, err := s.store.ListEPGSportsHits(ctx, from, to)
	if err != nil {
		return 0, err
	}
	if len(hits) == 0 {
		return 0, nil
	}

	byKey := map[string]*epgMatch{}
	generics := make([]store.EPGSportsHit, 0)

	now := time.Now()
	for _, h := range hits {
		if isReplayOrFillerEPG(h.Title, h.Description) {
			continue
		}
		if isMovieOrCinemaChannel(h.ChannelName) || isMovieOrCinemaChannel(h.ChannelTVGID) {
			continue
		}
		if isNonSportsEPGCategory(h.Category) {
			continue
		}
		sport := DetectSportFromTitle(h.Title, h.Category)
		if sport == "" {
			// Bare "Team vs Team" only when airing *and* the guide still looks sports-related.
			live := h.StartTime.Before(now.Add(time.Minute)) && h.EndTime.After(now)
			if !live {
				continue
			}
			if DetectSportFromTitle(h.Title+" "+h.Description, h.Category) == "" &&
				!categoryLooksSports(h.Category) {
				continue
			}
			sport = "Football" // default for international vs cards; refined below if parse fails
		}
		// Descriptions sometimes carry the real matchup while the title is generic.
		if home, away := ParseTeamsFromDescription(h.Description); home != "" && away != "" {
			if sport == "" {
				sport = DetectSportFromTitle(h.Title+" "+h.Description, "")
				if sport == "" {
					sport = DetectSportFromTitle(h.Title+" "+h.Description, h.Category)
				}
				if sport == "" {
					sport = "Football"
				}
			}
			title := home + " vs " + away
			if !isPlausibleSportsMatchup(sport, home, away, title) {
				// fall through — title may still parse cleanly
			} else {
				key := matchKey(sport, home, away, h.StartTime)
				mergeEPGMatch(byKey, key, sport, home, away, title, h)
				continue
			}
		}
		if sport != "" && IsGenericLeagueTitle(h.Title, sport) {
			// Keep generics only as channel hints for named matchups — never as cards.
			generics = append(generics, h)
			continue
		}
		if isProseEPGTitle(h.Title) {
			continue
		}
		home, away := ParseEventTeams(h.Title)
		if home == "" || away == "" {
			continue
		}
		if sport == "" || sport == "Football" || sport == "Soccer" {
			if s := DetectSportFromTitle(h.Title+" "+h.Description, ""); s != "" {
				sport = s
			} else if s := DetectSportFromTitle(h.Title+" "+h.Description, h.Category); s != "" {
				sport = s
			} else if sport == "" || sport == "Soccer" {
				sport = "Football"
			}
		}
		title := home + " vs " + away
		if strings.Contains(strings.ToLower(h.Title), " @ ") || strings.Contains(strings.ToLower(h.Title), " at ") {
			title = away + " @ " + home
		}
		if !isPlausibleSportsMatchup(sport, home, away, h.Title) {
			continue
		}
		key := matchKey(sport, home, away, h.StartTime)
		mergeEPGMatch(byKey, key, sport, home, away, title, h)
	}

	imported := 0
	for key, m := range byKey {
		// Cards require a real two-sided matchup — never "MLB Baseball" / single-team venue stubs.
		if m.sport == "" || m.home == "" || m.away == "" {
			continue
		}
		if !isPlausibleSportsMatchup(m.sport, m.home, m.away, m.title) {
			continue
		}
		ext := "epg:" + key
		ends := m.ends
		eventID, err := s.store.UpsertSportsEvent(ctx, store.SportsEventUpsert{
			ExternalID: ext,
			Sport:      m.sport,
			Title:      m.title,
			HomeTeam:   m.home,
			AwayTeam:   m.away,
			StartsAt:   m.starts,
			EndsAt:     &ends,
			Source:     "epg",
		})
		if err != nil {
			slog.Warn("sports epg upsert", "title", m.title, "err", err)
			continue
		}
		imported++
		if m.starts.Before(now.Add(time.Minute)) && m.ends.After(now) {
			_ = s.store.UpdateSportsEventScore(ctx, ext, "", "", "", "in")
		}

		// Local badge link only here — API name search happens in resolveMissingTeams.
		if m.home != "" {
			if t, err := s.store.FindSportsTeamByName(ctx, m.sport, m.home); err == nil {
				_ = s.store.LinkSportsEventTeam(ctx, eventID, true, t.ID)
			}
		}
		if m.away != "" {
			if t, err := s.store.FindSportsTeamByName(ctx, m.sport, m.away); err == nil {
				_ = s.store.LinkSportsEventTeam(ctx, eventID, false, t.ID)
			}
		}

		// Rebuild channel set from this EPG pass (drop stale cross-attached feeds).
		_ = s.store.ClearSportsEventChannels(ctx, eventID)
		linked := 0
		for _, h := range m.channels {
			if rejectChannelForSport(m.sport, h.ChannelName) {
				continue
			}
			chID := h.ChannelID
			label := h.ChannelName
			if label == "" {
				label = h.ChannelTVGID
			}
			if err := s.store.UpsertSportsEventChannel(ctx, eventID, label, &chID, 1.0); err == nil {
				linked++
			}
		}
		// Cinema-only / fully rejected channel sets → drop the card.
		if linked == 0 {
			_ = s.store.DeleteSportsEvent(ctx, eventID)
			imported--
			continue
		}

		// Attach overlapping Big-4 league slots that mention this matchup (not soccer).
		if allowsGenericLeagueAttach(m.sport) {
			for _, g := range generics {
				if DetectSportFromTitle(g.Title, g.Category) != m.sport {
					continue
				}
				if !overlap(m.starts, m.ends, g.StartTime, g.EndTime) {
					continue
				}
				if !isPrioritySportsNetwork(g.ChannelName, g.ChannelTVGID) {
					continue
				}
				if rejectChannelForSport(m.sport, g.ChannelName) {
					continue
				}
				if !genericMentionsMatchup(g, m.home, m.away) {
					continue
				}
				chID := g.ChannelID
				label := g.ChannelName
				if label == "" {
					label = g.ChannelTVGID
				}
				_ = s.store.UpsertSportsEventChannel(ctx, eventID, label, &chID, 0.9)
			}
		}
	}

	// Link generics onto named SportsDB/ESPN events (FS1/Sportsnet…) — never create orphan league cards.
	usedChannels := s.attachGenericsToExisting(ctx, generics)

	slog.Info("sports epg fill", "named_events", imported, "epg_hits", len(hits),
		"generic_slots", len(generics), "generics_attached_channels", len(usedChannels))
	return imported, nil
}

// attachGenericsToExisting adds FS1/Sportsnet/etc. onto SportsDB rows that only had "MLB.tv".
func (s *SyncService) attachGenericsToExisting(ctx context.Context, generics []store.EPGSportsHit) map[uuid.UUID]struct{} {
	used := map[uuid.UUID]struct{}{}
	if len(generics) == 0 {
		return used
	}
	events, err := s.store.ListSportsEvents(ctx, store.SportsListOpts{
		From:  time.Now().Add(-6 * time.Hour),
		To:    time.Now().Add(36 * time.Hour),
		Limit: 400,
	})
	if err != nil {
		return used
	}
	for _, ev := range events {
		// Only auto-attach onto named matchups (not other generic cards).
		if ev.HomeTeam == "" || ev.AwayTeam == "" {
			continue
		}
		if strings.HasPrefix(ev.ExternalID, "epg:") && (ev.AwayTeam == "" || strings.EqualFold(ev.HomeTeam, ev.Title)) {
			continue
		}
		ends := ev.StartsAt.Add(DefaultDuration(ev.Sport))
		if ev.EndsAt != nil {
			ends = *ev.EndsAt
		}
		ends = ends.Add(90 * time.Minute)
		start := ev.StartsAt.Add(-30 * time.Minute)
		if !allowsGenericLeagueAttach(ev.Sport) {
			continue
		}
		for _, g := range generics {
			if !strings.EqualFold(DetectSportFromTitle(g.Title, g.Category), ev.Sport) {
				continue
			}
			if !overlap(start, ends, g.StartTime, g.EndTime) {
				continue
			}
			if !isPrioritySportsNetwork(g.ChannelName, g.ChannelTVGID) {
				continue
			}
			if rejectChannelForSport(ev.Sport, g.ChannelName) {
				continue
			}
			// Named matchups: only attach generics that mention a playing team (or ALDS/NLDS windows).
			if ev.AwayTeam != "" && !genericMentionsMatchup(g, ev.HomeTeam, ev.AwayTeam) &&
				!strings.Contains(strings.ToLower(g.Title+" "+g.Description), "alds") &&
				!strings.Contains(strings.ToLower(g.Title+" "+g.Description), "nlds") {
				continue
			}
			// Keep ALDS generics off NLDS-named events and vice versa when labels disagree.
			gt := cleanGenericTitle(g.Title, g.Description)
			if seriesConflict(ev.Title+" "+ev.HomeTeam+" "+ev.AwayTeam, gt, g.Title, g.Description) {
				continue
			}
			chID := g.ChannelID
			label := g.ChannelName
			if label == "" {
				label = g.ChannelTVGID
			}
			if err := s.store.UpsertSportsEventChannel(ctx, ev.ID, label, &chID, 0.85); err == nil {
				used[g.ChannelID] = struct{}{}
			}
		}
	}
	return used
}

func seriesConflict(eventBlob, genericTitle, rawTitle, desc string) bool {
	ev := strings.ToLower(eventBlob)
	gMeta := strings.ToLower(genericTitle + " " + rawTitle + " " + desc)
	gALDS := strings.Contains(gMeta, "alds") || strings.Contains(gMeta, "american league")
	gNLDS := strings.Contains(gMeta, "nlds") || strings.Contains(gMeta, "national league")
	// If the generic is clearly ALDS but the event looks NL (or vice versa), skip.
	if gALDS && (strings.Contains(ev, "dodger") || strings.Contains(ev, "brave") ||
		strings.Contains(ev, "padre") || strings.Contains(ev, "brewer") || strings.Contains(ev, "phillie")) {
		return true
	}
	if gNLDS && (strings.Contains(ev, "yankee") || strings.Contains(ev, "guardian") ||
		strings.Contains(ev, "white sox") || strings.Contains(ev, "tiger") ||
		strings.Contains(ev, "astro") || strings.Contains(ev, "mariner")) {
		return true
	}
	return false
}

func allowsGenericLeagueAttach(sport string) bool {
	switch strings.ToLower(strings.TrimSpace(sport)) {
	case "ice hockey", "hockey", "basketball", "baseball", "american football":
		return true
	default:
		return false
	}
}

func genericMentionsMatchup(g store.EPGSportsHit, home, away string) bool {
	blob := strings.ToLower(g.Title + " " + g.Description)
	homeTok := teamToken(home)
	awayTok := teamToken(away)
	homeHit := homeTok != "" && homeTok != "tbd" && strings.Contains(blob, homeTok)
	awayHit := awayTok != "" && awayTok != "tbd" && strings.Contains(blob, awayTok)
	// Prefer both sides; allow one side only for short nicknames on series windows.
	if homeHit && awayHit {
		return true
	}
	series := strings.Contains(blob, "alds") || strings.Contains(blob, "nlds") ||
		strings.Contains(blob, "division series") || strings.Contains(blob, "divisional")
	return series && (homeHit || awayHit)
}

func (s *SyncService) pruneJunkEPGEvents(ctx context.Context) {
	events, err := s.store.ListSportsEvents(ctx, store.SportsListOpts{
		From:  time.Now().Add(-24 * time.Hour),
		To:    time.Now().Add(48 * time.Hour),
		Limit: 500,
	})
	if err != nil {
		return
	}
	n := 0
	for _, ev := range events {
		if !strings.EqualFold(ev.Source, "epg") {
			continue
		}
		drop := isProseEPGTitle(ev.Title) || isReplayOrFillerEPG(ev.Title, "")
		// Drop league-block / single-team stubs — Sports tab is matchup-only.
		if !drop && (strings.TrimSpace(ev.AwayTeam) == "" || strings.EqualFold(ev.HomeTeam, ev.Title)) {
			drop = true
		}
		if !drop && IsGenericLeagueTitle(ev.Title, ev.Sport) {
			drop = true
		}
		if !drop && !isPlausibleSportsMatchup(ev.Sport, ev.HomeTeam, ev.AwayTeam, ev.Title) {
			drop = true
		}
		if !drop {
			continue
		}
		if err := s.store.DeleteSportsEvent(ctx, ev.ID); err == nil {
			n++
		}
	}
	if n > 0 {
		slog.Info("sports epg pruned junk cards", "n", n)
	}
}

func mergeEPGMatch(byKey map[string]*epgMatch, key, sport, home, away, title string, h store.EPGSportsHit) {
	m := byKey[key]
	if m == nil {
		m = &epgMatch{
			sport: sport, home: home, away: away, title: title,
			starts: h.StartTime, ends: h.EndTime,
			channels: map[uuid.UUID]store.EPGSportsHit{},
		}
		byKey[key] = m
	} else {
		if len(home) > len(m.home) {
			m.home = home
		}
		if len(away) > len(m.away) {
			m.away = away
		}
		if len(title) > len(m.title) {
			m.title = title
		}
		if sport != "" {
			m.sport = sport
		}
	}
	if h.EndTime.After(m.ends) {
		m.ends = h.EndTime
	}
	if h.StartTime.Before(m.starts) {
		m.starts = h.StartTime
	}
	m.channels[h.ChannelID] = h
}

func isReplayOrFillerEPG(title, desc string) bool {
	t := strings.ToLower(title + " " + desc)
	for _, bad := range []string{
		"highlight", "replay", "záznam", "zaznam", "genudsendelse", "classic games",
		"sendepause", "coming soon", "fin des programmes", "programmes start",
		"najava", "sleepin", "documentary", "magazine", "le mag", "sportscentre",
		"sportscenter", "basketcast", "playback", "top 50", "top plays",
		"postgame", "pregame show", "epic moments", "in 30", "superstars",
		"inside the nba", "the jump", "around the horn",
	} {
		if strings.Contains(t, bad) {
			return true
		}
	}
	return false
}

func cleanGenericTitle(title, desc string) string {
	t := strings.TrimSpace(title)
	t = strings.TrimPrefix(t, "Live: ")
	t = strings.TrimPrefix(t, "Live - ")
	dl := strings.ToLower(desc)
	tl := strings.ToLower(t)
	switch {
	case strings.Contains(dl, "national league divisional") || strings.Contains(dl, "nlds") ||
		strings.Contains(dl, "ligue nationale") || strings.Contains(dl, "liga nacional"):
		return "MLB · NLDS"
	case strings.Contains(dl, "american league divisional") || strings.Contains(dl, "alds") ||
		strings.Contains(dl, "ligue américaine"):
		return "MLB · ALDS"
	case strings.Contains(tl, "alds"):
		return "MLB · ALDS"
	case strings.Contains(tl, "nlds"):
		return "MLB · NLDS"
	case strings.EqualFold(t, "MLB Baseball") || strings.EqualFold(t, "Baseball MLB") ||
		strings.EqualFold(t, "MLB") || strings.EqualFold(t, "Baseball: MLB"):
		return "MLB Baseball"
	case strings.Contains(tl, "ishockey") && strings.Contains(tl, "nhl"):
		return "NHL Hockey"
	case strings.Contains(tl, "engelska championship") || strings.HasSuffix(tl, "championship") && strings.Contains(tl, "fotboll"):
		return "EFL Championship"
	case strings.Contains(tl, "liga europy") || strings.Contains(tl, "europa league"):
		return "UEFA Europa League"
	case strings.Contains(tl, "liga konferencji") || strings.Contains(tl, "conference league"):
		return "UEFA Conference League"
	case strings.Contains(tl, "nations league"):
		return "UEFA Nations League"
	case strings.Contains(tl, "formel 1") || tl == "formula 1" || tl == "f1":
		return "Formula 1"
	case tl == "mma":
		return "MMA"
	case strings.Contains(tl, "koszyk"):
		return "Basketball"
	case strings.Contains(tl, "siatk"):
		return "Volleyball"
	case strings.Contains(tl, "m+ laliga") || strings.Contains(tl, "laliga"):
		return "La Liga"
	case strings.Contains(tl, "liga de campeones") || strings.Contains(tl, "campeones"):
		return "UEFA Champions League"
	default:
		if t == "" {
			return "Live Sports"
		}
		return t
	}
}

// alignTimesFromEPG snaps SportsDB tip-off times to actual broadcast windows on matched channels.
func (s *SyncService) alignTimesFromEPG(ctx context.Context) {
	events, err := s.store.ListSportsEvents(ctx, store.SportsListOpts{
		From:  time.Now().Add(-6 * time.Hour),
		To:    time.Now().Add(36 * time.Hour),
		Limit: 400,
	})
	if err != nil {
		slog.Warn("sports align list", "err", err)
		return
	}
	aligned := 0
	for _, ev := range events {
		ids := make([]uuid.UUID, 0, len(ev.Channels))
		for _, ch := range ev.Channels {
			if ch.LiveChannelID != nil {
				ids = append(ids, *ch.LiveChannelID)
			}
		}
		if len(ids) == 0 {
			continue
		}
		hits, err := s.store.ListEPGHitsForChannels(ctx, ids, ev.StartsAt.Add(-2*time.Hour), ev.StartsAt.Add(5*time.Hour))
		if err != nil || len(hits) == 0 {
			continue
		}
		var starts, ends time.Time
		found := false
		for _, h := range hits {
			if !epgHitMatchesEvent(ev, h) {
				continue
			}
			if !found || h.StartTime.Before(starts) {
				starts = h.StartTime
			}
			if !found || h.EndTime.After(ends) {
				ends = h.EndTime
			}
			found = true
		}
		if !found || !ends.After(starts) {
			continue
		}
		// ESPN/MLB tip-off is authoritative — never shift starts_at from EPG pregame windows.
		if strings.EqualFold(ev.Source, "espn") || strings.EqualFold(ev.Source, "mlb") {
			tip := ev.StartsAt
			end := ends
			if ev.EndsAt != nil && ev.EndsAt.After(end) {
				end = *ev.EndsAt
			}
			if ev.EndsAt != nil && end.Sub(*ev.EndsAt).Abs() < 2*time.Minute {
				continue
			}
			if err := s.store.UpdateSportsEventWindow(ctx, ev.ID, tip, end); err != nil {
				continue
			}
			aligned++
			continue
		}
		// Only adjust when EPG air time differs meaningfully (tip-off vs broadcast).
		if starts.Sub(ev.StartsAt).Abs() < 2*time.Minute &&
			(ev.EndsAt != nil && ends.Sub(*ev.EndsAt).Abs() < 2*time.Minute) {
			continue
		}
		if err := s.store.UpdateSportsEventWindow(ctx, ev.ID, starts, ends); err != nil {
			continue
		}
		aligned++
	}
	if aligned > 0 {
		slog.Info("sports epg time align", "updated", aligned)
	}
}

func epgHitMatchesEvent(ev store.SportsEvent, h store.EPGSportsHit) bool {
	sport := DetectSportFromTitle(h.Title, h.Category)
	if sport != "" && ev.Sport != "" && !strings.EqualFold(sport, ev.Sport) {
		return false
	}
	blob := strings.ToLower(h.Title + " " + h.Description)
	homeTok := teamToken(ev.HomeTeam)
	awayTok := teamToken(ev.AwayTeam)
	homeHit := homeTok != "" && homeTok != "tbd" && strings.Contains(blob, homeTok)
	awayHit := awayTok != "" && awayTok != "tbd" && strings.Contains(blob, awayTok)
	if homeHit && awayHit {
		return true
	}
	if venueHome, ok := HomeTeamFromVenueDescription(ev.Sport, h.Description); ok {
		if teamToken(venueHome) == homeTok || strings.EqualFold(venueHome, ev.HomeTeam) {
			return true
		}
	}
	// One named side on a generic league slot is enough (EPG often omits the visitor).
	if (homeHit || awayHit) && IsGenericLeagueTitle(h.Title, ev.Sport) {
		return true
	}
	return false
}

func matchKey(sport, home, away string, start time.Time) string {
	// Nickname token (last word) + accent fold so Montréal/Montreal Canadiens collide.
	a := teamToken(home)
	b := teamToken(away)
	if a > b {
		a, b = b, a
	}
	bucket := start.UTC().Truncate(15 * time.Minute).Format("20060102T1504")
	raw := strings.ToLower(sport) + "|" + a + "|" + b + "|" + bucket
	sum := sha1.Sum([]byte(raw))
	return hex.EncodeToString(sum[:12])
}

func teamToken(name string) string {
	name = foldTeam(strings.TrimSpace(name))
	fields := strings.Fields(name)
	if len(fields) == 0 {
		return ""
	}
	// Last word is usually the nickname (Canadiens, Hurricanes, Lakers).
	return normalizeKey(fields[len(fields)-1])
}

func foldTeam(s string) string {
	repl := strings.NewReplacer(
		"é", "e", "è", "e", "ê", "e", "ë", "e",
		"á", "a", "à", "a", "ä", "a", "â", "a",
		"í", "i", "ó", "o", "ú", "u", "ç", "c", "ñ", "n",
		"É", "e", "È", "e", "Á", "a", "Ó", "o", "Ú", "u", "Ç", "c",
	)
	return repl.Replace(strings.ToLower(s))
}

func looksLikeFullTeam(s string) bool {
	s = strings.TrimSpace(s)
	if len(s) < 6 {
		return false
	}
	return len(strings.Fields(s)) >= 2
}

func overlap(a0, a1, b0, b1 time.Time) bool {
	return a0.Before(b1) && b0.Before(a1)
}

func isPrioritySportsNetwork(name, tvg string) bool {
	if isBeINChannel(name) || isBeINChannel(tvg) {
		// BeIN is rarely a usable Big-4 feed on IPTV panels; never treat as priority.
		return false
	}
	s := strings.ToLower(name + " " + tvg)
	for _, needle := range []string{
		"tsn", "rds", "sportsnet", "espn", "tnt", "nbc", "abc", "cbs",
		"fox sports", "fox deportes", "fs1", "fs2",
		"tva sport", "nba tv", "mlb network", "nfl network",
		"onesoccer", "nhl", "spectrum",
		"altitude", "msg", "nesn", "bally", "yes network", "marquee", "sny",
	} {
		if strings.Contains(s, needle) {
			return true
		}
	}
	return false
}

// isBeINChannel reports beIN Sports feeds (including emoji/spacing variants).
func isBeINChannel(name string) bool {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	compact := b.String()
	return strings.Contains(compact, "bein") || strings.Contains(compact, "beinsport")
}

// RejectChannelForSport drops known-bad IPTV brands for a sport (esp. BeIN on Big-4 NA).
func RejectChannelForSport(sport, channelName string) bool {
	return rejectChannelForSport(sport, channelName)
}

func rejectChannelForSport(sport, channelName string) bool {
	if channelName == "" {
		return false
	}
	if isMovieOrCinemaChannel(channelName) {
		return true
	}
	if isBeINChannel(channelName) {
		switch strings.ToLower(strings.TrimSpace(sport)) {
		case "baseball", "basketball", "ice hockey", "hockey", "american football":
			return true
		}
	}
	return false
}

func channelNameCompact(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// isMovieOrCinemaChannel rejects movie networks that publish dual-language "Title vs Local" EPG.
func isMovieOrCinemaChannel(name string) bool {
	if name == "" {
		return false
	}
	compact := channelNameCompact(name)
	if compact == "" {
		return false
	}
	// Keep HBO/Sport variants that are actually sports feeds.
	if strings.Contains(compact, "sport") || strings.Contains(compact, "espn") {
		return false
	}
	for _, needle := range []string{
		"cinemax", "cinema", "cinemundo", "cinemaxx",
		"hbo", "hbomax", "maxcinema",
		"showtime", "starz", "starzencore",
		"skycinema", "canalcinema", "filmbox", "filmcafe",
		"mgm", "tcm", "epix", "paramountmovie", "sonymovies",
		"axnmovie", "warnertvfilm",
	} {
		if strings.Contains(compact, needle) {
			return true
		}
	}
	lower := strings.ToLower(name)
	for _, needle := range []string{"cinema", "cinemax", " film", "movies", "movie "} {
		if strings.Contains(lower, needle) {
			return true
		}
	}
	return false
}

func isNonSportsEPGCategory(cat string) bool {
	c := strings.ToLower(strings.TrimSpace(cat))
	if c == "" {
		return false
	}
	if categoryLooksSports(c) || DetectSportFromTitle("", c) != "" {
		return false
	}
	for _, bad := range []string{
		"movie", "movies", "film", "films", "cinema", "series", "drama",
		"comedy", "thriller", "horror", "romance", "telenovela", "soap",
		"entertainment", "kids", "children", "animation", "anime", "documentary",
	} {
		if strings.Contains(c, bad) {
			return true
		}
	}
	return false
}

func categoryLooksSports(cat string) bool {
	c := strings.ToLower(strings.TrimSpace(cat))
	if c == "" {
		return false
	}
	if DetectSportFromTitle("", c) != "" {
		return true
	}
	for _, tok := range []string{"sport", "hockey", "basketball", "baseball", "football", "soccer", "tennis", "golf", "motor", "nfl", "nba", "mlb", "nhl", "uefa", "fifa"} {
		if strings.Contains(c, tok) {
			return true
		}
	}
	return false
}
