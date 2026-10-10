package sportsdb

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/stevie-media/stevie/apps/server/internal/livetv"
	"github.com/stevie-media/stevie/apps/server/internal/store"
	"github.com/stevie-media/stevie/apps/server/internal/streamed"
	"github.com/stevie-media/stevie/apps/server/internal/teamlogo"
)

// Prioritize commonly televised NA sports; keep the list short so free-tier
// rate limits can finish schedule + badge resolution in one pass.
var defaultSports = []string{
	"Ice Hockey",
	"Basketball",
	"Baseball",
	"American Football",
	"Football",
	"Tennis",
	"Fighting",
	"Golf",
	"MotorSport",
}

// SyncService periodically pulls schedules (ESPN/MLB primary, SportsDB/EPG fill) and resolves logos/channels.
type SyncService struct {
	store     *store.Store
	client    *Client
	espn      *ESPNClient
	mlb       *MLBClient
	streamed  *streamed.Client
	logos     *teamlogo.Catalog
	countries []string
	sports    []string
	interval  time.Duration

	mu           sync.Mutex
	running      bool
	premiumOK    *bool // nil unknown
	lastSync     time.Time
	lastError    string
	lastImported int
}

func (s *SyncService) SetTeamLogoCatalog(cat *teamlogo.Catalog) {
	if s == nil {
		return
	}
	s.logos = cat
}

func NewSyncService(st *store.Store, client *Client, countries []string) *SyncService {
	if len(countries) == 0 {
		countries = []string{"Canada", "United_States"}
	}
	return &SyncService{
		store:     st,
		client:    client,
		espn:      NewESPNClient(),
		mlb:       NewMLBClient(),
		countries: countries,
		sports:    append([]string{}, defaultSports...),
		interval:  45 * time.Minute,
	}
}

func (s *SyncService) Status() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	premium := "unknown"
	if s.premiumOK != nil {
		if *s.premiumOK {
			premium = "yes"
		} else {
			premium = "no"
		}
	}
	return map[string]any{
		"running":       s.running,
		"last_sync":     s.lastSync,
		"last_error":    s.lastError,
		"last_imported": s.lastImported,
		"premium_scores": premium,
		"interval_min":  int(s.interval / time.Minute),
	}
}

// Run loops until ctx is cancelled. Triggers an immediate sync on start.
func (s *SyncService) Run(ctx context.Context) {
	s.Trigger(ctx)
	t := time.NewTicker(s.interval)
	defer t.Stop()
	// ESPN scoreboards only — keep cards near real-time without hammering SportsDB.
	espnScoreTick := time.NewTicker(15 * time.Second)
	defer espnScoreTick.Stop()
	sportsDBScoreTick := time.NewTicker(2 * time.Minute)
	defer sportsDBScoreTick.Stop()
	// Re-scan EPG often so currently-airing international feeds stay on the Sports tab.
	epgHarvestTick := time.NewTicker(5 * time.Minute)
	defer epgHarvestTick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.Trigger(ctx)
		case <-espnScoreTick.C:
			s.pollESPNScores(ctx)
		case <-sportsDBScoreTick.C:
			s.pollScores(ctx) // SportsDB premium livescores if available
		case <-epgHarvestTick.C:
			s.harvestEPG(ctx)
		}
	}
}

// harvestEPG rescans the guide for live/upcoming matchups without a full SportsDB pass.
func (s *SyncService) harvestEPG(parent context.Context) {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	n, err := s.fillFromEPG(ctx)
	if err != nil {
		slog.Warn("sports epg harvest failed", "err", err)
		return
	}
	n += s.fillFromEventChannels(ctx)
	n += s.fillFromStreamedCFL(ctx)
	_ = s.fillMotorSportFeeds(ctx)
	if n > 0 {
		slog.Info("sports epg harvest", "events", n)
	}
	_ = parent
}

func (s *SyncService) Trigger(parent context.Context) {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.running = true
	s.mu.Unlock()

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
		defer cancel()
		n, err := s.syncOnce(ctx)
		s.mu.Lock()
		s.running = false
		s.lastSync = time.Now()
		s.lastImported = n
		if err != nil {
			s.lastError = err.Error()
			slog.Warn("sports sync failed", "err", err)
		} else {
			s.lastError = ""
			slog.Info("sports sync ok", "events", n)
		}
		s.mu.Unlock()
		_ = parent
	}()
}

func (s *SyncService) syncOnce(ctx context.Context) (int, error) {
	s.normalizeSportNames(ctx)
	s.applyBundledRosters(ctx)

	// 1) ESPN + MLB StatsAPI — accurate Big-4 schedules, scores, broadcasts (FS1…).
	espnN, err := s.syncESPNPrimary(ctx)
	if err != nil {
		slog.Warn("espn/mlb sync failed", "err", err)
	}

	// 2) EPG fill — local titles/venues + attach generics onto named events.
	epgN, err := s.fillFromEPG(ctx)
	if err != nil {
		slog.Warn("sports epg fill failed", "err", err)
	}
	// 2b) CFL/NFL event channels encoded in IPTV titles (common when EPG is sparse).
	chN := s.fillFromEventChannels(ctx)
	epgN += chN
	// 2c) CFL from streamed.pk — ESPN's CFL board is stale (stuck on 2022).
	epgN += s.fillFromStreamedCFL(ctx)
	// 2d) Sky/DAZN F1 feeds onto MotorSport cards (EPG matchups are often beIN-only).
	_ = s.fillMotorSportFeeds(ctx)
	s.alignTimesFromEPG(ctx)

	// Drop scoreless SportsDB/EPG duplicates of ESPN/MLB cards ASAP (before slow SportsDB TV).
	s.dedupeSportsEvents(ctx)
	// Re-link broadcasts onto real channels (skip ### category headers).
	s.rematchSportsChannels(ctx)

	now := time.Now().UTC()
	// Include yesterday so late-evening NA games still import after UTC midnight.
	days := []time.Time{now.Add(-24 * time.Hour), now, now.Add(24 * time.Hour)}

	type agg struct {
		sport, title, home, away string
		homeExt, awayExt         string
		league                   string
		starts                   time.Time
		channels                 map[string]struct{}
	}
	byID := map[string]*agg{}

	// Big-4 come from ESPN/MLB — skip SportsDB TV for those to avoid dupes + rate limits.
	sportsDBSports := make([]string, 0, len(s.sports))
	for _, sport := range s.sports {
		switch sport {
		case "Baseball", "Basketball", "Ice Hockey", "American Football", "Football", "Soccer":
			continue
		default:
			sportsDBSports = append(sportsDBSports, sport)
		}
	}

	for _, day := range days {
		for _, sport := range sportsDBSports {
			for _, country := range s.countries {
				if err := ctx.Err(); err != nil {
					return 0, err
				}
				rows, err := s.client.EventsTV(ctx, day, sport, country)
				if err != nil {
					slog.Warn("sports eventstv", "sport", sport, "country", country, "err", err)
					// Free tier / Cloudflare 429 — back off hard.
					if strings.Contains(err.Error(), "429") {
						time.Sleep(8 * time.Second)
					} else {
						time.Sleep(2 * time.Second)
					}
					continue
				}
				for _, row := range rows {
					id := strings.TrimSpace(row.IDEvent)
					if id == "" {
						continue
					}
					starts, ok := parseTS(row.StrTimeStamp, row.DateEvent, row.StrTime)
					if !ok {
						continue
					}
					a := byID[id]
					if a == nil {
						home := strings.TrimSpace(row.StrHomeTeam)
						away := strings.TrimSpace(row.StrAwayTeam)
						if home == "" || away == "" {
							h, aw := ParseEventTeams(row.StrEvent)
							if home == "" {
								home = h
							}
							if away == "" {
								away = aw
							}
						}
						a = &agg{
							sport:    firstNonEmpty(row.StrSport, sport),
							title:    strings.TrimSpace(row.StrEvent),
							home:     home,
							away:     away,
							homeExt:  strings.TrimSpace(row.IDHomeTeam),
							awayExt:  strings.TrimSpace(row.IDAwayTeam),
							league:   strings.TrimSpace(row.StrLeague),
							starts:   starts,
							channels: map[string]struct{}{},
						}
						byID[id] = a
					}
					if ch := strings.TrimSpace(row.StrChannel); ch != "" {
						a.channels[ch] = struct{}{}
					}
				}
				time.Sleep(2 * time.Second) // free-tier ~30 req/min
			}
		}
	}

	liveChannels, err := s.store.ListLiveChannelsForMatch(ctx)
	if err != nil {
		return 0, err
	}
	cands := BuildCandidates(liveChannels)

	imported := 0
	leaguesSeen := map[string]struct{}{}
	for id, a := range byID {
		ends := a.starts.Add(DefaultDuration(a.sport))
		var homeID, awayID *uuid.UUID
		if a.homeExt != "" {
			if tid, e := s.ensureTeam(ctx, a.homeExt, a.sport, a.league, a.home); e == nil {
				homeID = &tid
			}
		}
		if a.awayExt != "" {
			if tid, e := s.ensureTeam(ctx, a.awayExt, a.sport, a.league, a.away); e == nil {
				awayID = &tid
			}
		}
		labels := make([]string, 0, len(a.channels))
		for ch := range a.channels {
			labels = append(labels, ch)
		}
		eventID, err := s.store.UpsertSportsEvent(ctx, store.SportsEventUpsert{
			ExternalID: id,
			Sport:      a.sport,
			Title:      a.title,
			HomeTeam:   a.home,
			AwayTeam:   a.away,
			HomeTeamID: homeID,
			AwayTeamID: awayID,
			StartsAt:   a.starts,
			EndsAt:     &ends,
			Source:     "thesportsdb",
			Channels:   labels,
		})
		if err != nil {
			slog.Warn("sports upsert event", "id", id, "err", err)
			continue
		}
		imported++
		for _, label := range labels {
			s.attachLabelChannels(ctx, eventID, a.sport, label, a.home, a.away, cands, false)
		}
		s.attachTeamChannels(ctx, eventID, a.sport, a.home, a.away, cands, a.starts, ends)
		if a.league != "" {
			leaguesSeen[a.league] = struct{}{}
		}
	}

	// Snap tip-off times to real broadcast windows from EPG when possible.
	s.alignTimesFromEPG(ctx)

	// Resolve teams missing FKs / badges (name search).
	s.resolveMissingTeams(ctx)
	// Opportunistic league badge fills (limited).
	n := 0
	for league := range leaguesSeen {
		if n >= 8 {
			break
		}
		s.cacheLeagueTeams(ctx, league)
		n++
		time.Sleep(2 * time.Second)
	}

	// Prefer scored ESPN/MLB cards over scoreless SportsDB/EPG duplicates.
	s.dedupeSportsEvents(ctx)

	_, _ = s.store.DeleteOldSportsEvents(ctx, time.Now().Add(-7*24*time.Hour))
	return imported + epgN + espnN, nil
}

// applyBundledRosters upserts every crest from the Docker-baked logo pack so
// EPG/unlinked events can resolve badges by name without waiting on SportsDB.
func (s *SyncService) applyBundledRosters(ctx context.Context) {
	if s.logos == nil {
		return
	}
	n := 0
	for _, t := range s.logos.Teams() {
		if err := ctx.Err(); err != nil {
			return
		}
		ext := strings.TrimSpace(t.ExternalID)
		if ext == "" {
			ext = "logo:" + t.Key
		}
		alts := strings.Join(t.Aliases, ", ")
		badge := teamlogo.BadgeRef(t.Key)
		// Prefer updating an existing row (SportsDB / prior ESPN) over creating a duplicate.
		if existing, err := s.store.FindSportsTeamByName(ctx, t.Sport, t.Name); err == nil {
			ext = existing.ExternalID
			if strings.TrimSpace(existing.NameAlternates) != "" {
				alts = existing.NameAlternates
				for _, a := range t.Aliases {
					if a != "" && !strings.Contains(strings.ToLower(alts), strings.ToLower(a)) {
						alts = alts + ", " + a
					}
				}
			}
		}
		_, err := s.store.UpsertSportsTeam(ctx, store.SportsTeam{
			ExternalID:     ext,
			Sport:          t.Sport,
			League:         t.League,
			Name:           t.Name,
			NameAlternates: alts,
			BadgeURL:       badge,
		})
		if err == nil {
			n++
		}
	}
	slog.Info("sports bundled rosters", "teams", n)
}

func (s *SyncService) ensureTeam(ctx context.Context, extID, sport, league, name string) (uuid.UUID, error) {
	if t, err := s.store.SportsTeamByExternalID(ctx, extID); err == nil {
		if t.BadgeURL == "" {
			if remote, e := s.client.LookupTeam(ctx, extID); e == nil && remote != nil {
				_, _ = s.store.UpsertSportsTeam(ctx, store.SportsTeam{
					ExternalID:     extID,
					Sport:          firstNonEmpty(remote.StrSport, sport),
					League:         firstNonEmpty(remote.StrLeague, league),
					Name:           firstNonEmpty(remote.StrTeam, name),
					NameAlternates: remote.StrTeamAlternate,
					BadgeURL:       livetv.SanitizeLogoURL(remote.Badge()),
				})
			}
			time.Sleep(2 * time.Second)
		}
		t2, err := s.store.SportsTeamByExternalID(ctx, extID)
		return t2.ID, err
	}
	badge := ""
	alt := ""
	if remote, e := s.client.LookupTeam(ctx, extID); e == nil && remote != nil {
		name = firstNonEmpty(remote.StrTeam, name)
		sport = firstNonEmpty(remote.StrSport, sport)
		league = firstNonEmpty(remote.StrLeague, league)
		badge = livetv.SanitizeLogoURL(remote.Badge())
		alt = remote.StrTeamAlternate
		time.Sleep(2 * time.Second)
	}
	t, err := s.store.UpsertSportsTeam(ctx, store.SportsTeam{
		ExternalID:     extID,
		Sport:          sport,
		League:         league,
		Name:           name,
		NameAlternates: alt,
		BadgeURL:       badge,
	})
	return t.ID, err
}

func (s *SyncService) resolveMissingTeams(ctx context.Context) {
	events, err := s.store.SportsEventsNeedingTeams(ctx, 80)
	if err != nil {
		return
	}
	for _, ev := range events {
		if err := ctx.Err(); err != nil {
			return
		}
		if ev.HomeTeam != "" && ev.HomeTeamID == nil {
			if id, ok := s.resolveTeamByName(ctx, ev.Sport, ev.HomeTeam); ok {
				_ = s.store.LinkSportsEventTeam(ctx, ev.ID, true, id)
			}
		}
		if ev.AwayTeam != "" && ev.AwayTeamID == nil {
			if id, ok := s.resolveTeamByName(ctx, ev.Sport, ev.AwayTeam); ok {
				_ = s.store.LinkSportsEventTeam(ctx, ev.ID, false, id)
			}
		}
	}
}

func (s *SyncService) resolveTeamByName(ctx context.Context, sport, name string) (uuid.UUID, bool) {
	if t, err := s.store.FindSportsTeamByName(ctx, sport, name); err == nil {
		return t.ID, true
	}
	teams, err := s.client.SearchTeams(ctx, name)
	time.Sleep(2 * time.Second)
	if err != nil || len(teams) == 0 {
		return uuid.Nil, false
	}
	best := teams[0]
	for _, t := range teams {
		if strings.EqualFold(t.StrSport, sport) || sport == "" {
			best = t
			break
		}
	}
	if best.IDTeam == "" {
		return uuid.Nil, false
	}
	up, err := s.store.UpsertSportsTeam(ctx, store.SportsTeam{
		ExternalID:     best.IDTeam,
		Sport:          firstNonEmpty(best.StrSport, sport),
		League:         best.StrLeague,
		Name:           firstNonEmpty(best.StrTeam, name),
		NameAlternates: best.StrTeamAlternate,
		BadgeURL:       livetv.SanitizeLogoURL(best.Badge()),
	})
	if err != nil {
		return uuid.Nil, false
	}
	return up.ID, true
}

func (s *SyncService) cacheLeagueTeams(ctx context.Context, league string) {
	teams, err := s.client.SearchAllTeams(ctx, league)
	if err != nil {
		slog.Debug("sports league teams", "league", league, "err", err)
		return
	}
	for _, t := range teams {
		if t.IDTeam == "" {
			continue
		}
		_, _ = s.store.UpsertSportsTeam(ctx, store.SportsTeam{
			ExternalID:     t.IDTeam,
			Sport:          t.StrSport,
			League:         firstNonEmpty(t.StrLeague, league),
			Name:           t.StrTeam,
			NameAlternates: t.StrTeamAlternate,
			BadgeURL:       livetv.SanitizeLogoURL(t.Badge()),
		})
	}
}

func (s *SyncService) pollScores(ctx context.Context) {
	s.mu.Lock()
	premium := s.premiumOK
	s.mu.Unlock()
	if premium != nil && !*premium {
		return
	}
	// Only poll if we have in-window events.
	from := time.Now().Add(-30 * time.Minute)
	to := time.Now().Add(30 * time.Minute)
	events, err := s.store.ListSportsEvents(ctx, store.SportsListOpts{From: from, To: to, Limit: 5})
	if err != nil || len(events) == 0 {
		return
	}
	sports := map[string]struct{}{}
	for _, e := range events {
		sports[e.Sport] = struct{}{}
	}
	if len(sports) == 0 {
		sports["all"] = struct{}{}
	}
	for sport := range sports {
		scores, err := s.client.Livescores(ctx, sportPath(sport))
		if IsPremiumError(err) {
			f := false
			s.mu.Lock()
			s.premiumOK = &f
			s.mu.Unlock()
			return
		}
		if err != nil {
			slog.Debug("sports livescore", "sport", sport, "err", err)
			continue
		}
		t := true
		s.mu.Lock()
		s.premiumOK = &t
		s.mu.Unlock()
		for _, sc := range scores {
			if sc.IDEvent == "" {
				continue
			}
			_ = s.store.UpdateSportsEventScore(ctx, sc.IDEvent, sc.IntHomeScore, sc.IntAwayScore, sc.StrProgress, sc.StrStatus)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func sportPath(sport string) string {
	s := strings.ToLower(strings.TrimSpace(sport))
	s = strings.ReplaceAll(s, " ", "")
	switch s {
	case "icehockey":
		return "hockey"
	case "americanfootball":
		return "americanfootball"
	default:
		if s == "" {
			return "all"
		}
		return s
	}
}

func parseTS(stamp, dateEvent, strTime string) (time.Time, bool) {
	stamp = strings.TrimSpace(stamp)
	layouts := []string{
		time.RFC3339,
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05Z",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, stamp); err == nil {
			return t.UTC(), true
		}
	}
	dateEvent = strings.TrimSpace(dateEvent)
	strTime = strings.TrimSpace(strTime)
	if dateEvent != "" && strTime != "" {
		combined := dateEvent + " " + strTime
		for _, layout := range []string{"2006-01-02 15:04:05", "2006-01-02 15:04"} {
			if t, err := time.Parse(layout, combined); err == nil {
				return t.UTC(), true
			}
		}
	}
	if dateEvent != "" {
		if t, err := time.Parse("2006-01-02", dateEvent); err == nil {
			return t.UTC(), true
		}
	}
	return time.Time{}, false
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
