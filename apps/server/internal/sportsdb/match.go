package sportsdb

import (
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/stevie-media/stevie/apps/server/internal/store"
)

type channelCandidate struct {
	ID    uuid.UUID
	Name  string
	Key   string
	Pkg   string // "US", "CA", "NBA", "TV", …
	Brand string // normalized brand after package / quality strip
}

// ChannelMatch is one scored live-channel hit for a broadcast label.
type ChannelMatch struct {
	ID    uuid.UUID
	Name  string
	Score float64
}

// MatchOpts controls multi-channel matching for a broadcast label or team feeds.
type MatchOpts struct {
	AliasID  uuid.UUID
	HasAlias bool
	Limit    int
	// Home/Away (optional): drop library channels tagged with a different team.
	Home string
	Away string
}

// MatchChannel finds the best live channel for a broadcast label.
func MatchChannel(label string, channels []channelCandidate, aliasID uuid.UUID, hasAlias bool) (uuid.UUID, float64) {
	all := MatchAllChannels(label, channels, MatchOpts{AliasID: aliasID, HasAlias: hasAlias, Limit: 1})
	if len(all) == 0 {
		return uuid.Nil, 0
	}
	return all[0].ID, all[0].Score
}

// MatchAllChannels returns every strong hit for a label (NBA TV → many regional feeds).
func MatchAllChannels(label string, channels []channelCandidate, opts MatchOpts) []ChannelMatch {
	label = strings.TrimSpace(label)
	if label == "" {
		return nil
	}
	limit := opts.Limit
	if limit <= 0 {
		limit = 32
	}
	if opts.HasAlias && opts.AliasID != uuid.Nil {
		name := ""
		for _, ch := range channels {
			if ch.ID == opts.AliasID {
				name = ch.Name
				break
			}
		}
		return []ChannelMatch{{ID: opts.AliasID, Name: name, Score: 1.0}}
	}
	want := normalizeKey(label)
	if want == "" {
		return nil
	}
	wantBrand := stripQualifiers(want)
	wants := []string{wantBrand}
	for _, alt := range broadcastKeyAliases(wantBrand) {
		wants = append(wants, stripQualifiers(alt))
	}

	allowedTeams := teamNeedleSet(opts.Home, opts.Away)

	var scored []ChannelMatch
	for _, ch := range channels {
		if IsCategoryHeaderChannel(ch.Name) || isStaleEventChannel(ch.Name, want) {
			continue
		}
		if mentionsForeignTeam(ch.Name, allowedTeams) {
			continue
		}
		score := 0.0
		strong := false
		for _, w := range wants {
			if s := channelScore(w, ch.Key, ch.Name); s > score {
				score = s
			}
			if brandsMatch(w, ch.Brand) {
				strong = true
			}
		}
		// Gate on raw score so package bias cannot drop real regional twins.
		if !strong || score < 0.7 {
			continue
		}
		score *= regionBias(ch.Name, want)
		scored = append(scored, ChannelMatch{ID: ch.ID, Name: ch.Name, Score: score})
	}
	sort.Slice(scored, func(i, j int) bool {
		if scored[i].Score != scored[j].Score {
			return scored[i].Score > scored[j].Score
		}
		return scored[i].Name < scored[j].Name
	})
	return dedupeQualityTwins(scored, limit)
}

// TeamFeedMatches finds league team channels and RSNs for a matchup (any Big-4 sport).
func TeamFeedMatches(sport, home, away string, channels []channelCandidate, limit int) []ChannelMatch {
	if limit <= 0 {
		limit = 20
	}
	league := leagueChannelPrefix(sport)
	needles := teamFeedNeedles(home, away)
	if len(needles) == 0 {
		return nil
	}
	allowed := teamNeedleSet(home, away)

	var scored []ChannelMatch
	for _, ch := range channels {
		if IsCategoryHeaderChannel(ch.Name) || isJunkTeamFeed(ch.Name) {
			continue
		}
		if mentionsForeignTeam(ch.Name, allowed) {
			continue
		}
		bestNeedle := ""
		for _, n := range needles {
			if n != "" && strings.Contains(ch.Key, n) && len(n) > len(bestNeedle) {
				bestNeedle = n
			}
		}
		if bestNeedle == "" {
			continue
		}
		leagueNamed := league != "" && (strings.EqualFold(normalizeKey(ch.Pkg), league) ||
			strings.HasPrefix(ch.Key, league) || strings.Contains(strings.ToLower(ch.Name), league+":"))
		rsnNamed := isRegionalSportsFeed(ch.Name)
		// Any "PKG: Full Team Name" feed (AT&T:, US:, …) when brand is the team.
		brandIsTeam := brandsMatch(bestNeedle, ch.Brand) || strings.HasSuffix(ch.Brand, bestNeedle)
		fullTeam := len(bestNeedle) >= 10
		if !leagueNamed && !rsnNamed && !(brandIsTeam && fullTeam) {
			continue
		}
		// Short nicknames only on league-prefixed feeds; RSN / full-name brands need ≥6.
		minNeedle := 8
		if rsnNamed || brandIsTeam {
			minNeedle = 6
		}
		if len(bestNeedle) < minNeedle && !leagueNamed && !fullTeam {
			continue
		}
		score := 0.8 + 0.02*float64(len(bestNeedle))
		if leagueNamed {
			score += 0.15
		}
		if rsnNamed {
			score += 0.1
		}
		score *= regionBias(ch.Name, bestNeedle)
		scored = append(scored, ChannelMatch{ID: ch.ID, Name: ch.Name, Score: score})
	}
	sort.Slice(scored, func(i, j int) bool { return scored[i].Score > scored[j].Score })
	return dedupeQualityTwins(scored, limit)
}

func dedupeQualityTwins(scored []ChannelMatch, limit int) []ChannelMatch {
	out := make([]ChannelMatch, 0, limit)
	seenCore := map[string]struct{}{}
	for _, m := range scored {
		core := stripQualifiers(normalizeKey(m.Name))
		core = strings.TrimSuffix(core, "sd")
		if _, ok := seenCore[core]; ok {
			continue
		}
		if strings.HasSuffix(strings.ToUpper(strings.TrimSpace(m.Name)), " SD") ||
			strings.HasSuffix(normalizeKey(m.Name), "sd") {
			continue
		}
		seenCore[core] = struct{}{}
		out = append(out, m)
		if len(out) >= limit {
			break
		}
	}
	if len(out) == 0 && len(scored) > 0 {
		for _, m := range scored {
			out = append(out, m)
			if len(out) >= limit {
				break
			}
		}
	}
	return out
}

func leagueChannelPrefix(sport string) string {
	switch strings.ToLower(strings.TrimSpace(sport)) {
	case "basketball":
		return "nba"
	case "ice hockey", "hockey":
		return "nhl"
	case "baseball":
		return "mlb"
	case "american football":
		return "nfl"
	default:
		return ""
	}
}

func teamFeedNeedles(home, away string) []string {
	var out []string
	seen := map[string]struct{}{}
	add := func(s string) {
		s = normalizeKey(s)
		if len(s) < 4 {
			return
		}
		if _, ok := seen[s]; ok {
			return
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	for _, name := range []string{home, away} {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		add(name)
		add(teamToken(name))
		fields := strings.Fields(name)
		if len(fields) >= 2 {
			add(strings.Join(fields[len(fields)-2:], ""))
		}
	}
	return out
}

func teamNeedleSet(home, away string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, n := range teamFeedNeedles(home, away) {
		out[n] = struct{}{}
	}
	for _, t := range []string{teamToken(home), teamToken(away)} {
		if t != "" {
			out[t] = struct{}{}
		}
	}
	return out
}

// broadcastKeyAliases maps short TV labels onto longer channel-name brands.
// Keep this to true abbreviations — not team- or market-specific hacks.
func broadcastKeyAliases(want string) []string {
	switch want {
	case "fs1":
		return []string{"foxsports1", "usfoxsports1"}
	case "fs2":
		return []string{"foxsports2", "usfoxsports2"}
	case "foxone":
		return []string{"foxsports1", "fs1"}
	case "nbatv":
		return []string{"nbanbatv"}
	case "spectrumsports":
		return []string{"spectrumsportsnet"}
	case "nhltv", "nhlnetwork":
		return []string{"nhlnetwork", "nhlnet"}
	case "mlbn", "mlbnetwork":
		return []string{"mlbnetwork", "mlbnet"}
	case "nfln", "nflnetwork":
		return []string{"nflnetwork", "nflnet"}
	default:
		return nil
	}
}

// regionBias prefers US/CA feeds over Latin-American/VO duplicates of the same brand.
func regionBias(name, wantKey string) float64 {
	upper := strings.ToUpper(strings.TrimSpace(name))
	bias := 1.0
	switch {
	case strings.HasPrefix(upper, "US:"):
		bias = 1.2
	case strings.HasPrefix(upper, "CA:"):
		bias = 1.1
	case strings.HasPrefix(upper, "ARG:"), strings.HasPrefix(upper, "VO:"),
		strings.HasPrefix(upper, "CO:"), strings.HasPrefix(upper, "MX:"),
		strings.HasPrefix(upper, "AT&T:"):
		bias = 0.75
	}
	if strings.Contains(upper, "PPV") && !strings.Contains(wantKey, "ppv") {
		bias *= 0.45
	}
	return bias
}

func BuildCandidates(channels []store.LiveChannel) []channelCandidate {
	out := make([]channelCandidate, 0, len(channels))
	for _, ch := range channels {
		if IsCategoryHeaderChannel(ch.Name) {
			continue
		}
		pkg, brand := splitPackageBrand(ch.Name)
		out = append(out, channelCandidate{
			ID:    ch.ID,
			Name:  ch.Name,
			Key:   normalizeKey(ch.Name),
			Pkg:   pkg,
			Brand: brand,
		})
	}
	return out
}

// splitPackageBrand parses "US: ESPN HD" → ("US", "espn"), "NBA TV" → ("", "nbatv").
func splitPackageBrand(name string) (pkg, brand string) {
	raw := strings.TrimSpace(name)
	brandSrc := raw
	if i := strings.Index(raw, ":"); i > 0 && i <= 16 {
		left := strings.TrimSpace(raw[:i])
		right := strings.TrimSpace(raw[i+1:])
		if right != "" && isPackagePrefix(left) {
			pkg = left
			brandSrc = right
		}
	}
	return pkg, stripQualifiers(normalizeKey(brandSrc))
}

func isPackagePrefix(s string) bool {
	if s == "" || strings.Contains(s, " ") {
		return false
	}
	letters := 0
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '&' || r == '+' {
			letters++
			continue
		}
		return false
	}
	return letters >= 1 && letters <= 12
}

// IsCategoryHeaderChannel reports IPTV group separators like "####### ESPN PPV #######"
// that are not playable linear channels.
func IsCategoryHeaderChannel(name string) bool {
	n := strings.TrimSpace(name)
	if n == "" {
		return true
	}
	if strings.Count(n, "#") >= 3 {
		return true
	}
	trimmed := strings.Trim(n, " \t#-*=_•·")
	if trimmed == "" {
		return true
	}
	letters := 0
	for _, r := range trimmed {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			letters++
		}
	}
	if letters < 3 {
		return true
	}
	upper := strings.ToUpper(trimmed)
	switch upper {
	case "SPORTS", "SPORT", "PPV", "LIVE", "24/7", "REPLAY", "EVENTS":
		return true
	}
	if (strings.HasPrefix(n, "#") || strings.HasPrefix(n, "=") || strings.HasPrefix(n, "-") || strings.HasPrefix(n, "*")) &&
		(strings.HasSuffix(n, "#") || strings.HasSuffix(n, "=") || strings.HasSuffix(n, "-") || strings.HasSuffix(n, "*")) &&
		strings.Count(n, trimmed) == 1 {
		if len(trimmed) <= 24 && !strings.Contains(trimmed, ":") {
			return true
		}
	}
	return false
}

func isStaleEventChannel(name, want string) bool {
	if isEventSlotChannel(name) && !strings.Contains(want, "ppv") {
		return true
	}
	upper := strings.ToUpper(name)
	if strings.Contains(upper, "8K EXCLUSIVE") && !strings.Contains(want, "ppv") {
		return true
	}
	return false
}

// isEventSlotChannel reports dated/PPV playlist titles, not linear network feeds.
func isEventSlotChannel(name string) bool {
	n := strings.TrimSpace(name)
	if n == "" {
		return false
	}
	// "US (ESPN+ 444) | NHL: Avalanche vs. Flames …"
	if strings.Contains(n, " | ") {
		return true
	}
	lower := strings.ToLower(n)
	if strings.Contains(lower, "live |") || strings.Contains(lower, "ended |") ||
		strings.Contains(lower, "end |") || strings.Contains(lower, "start:") {
		return true
	}
	// "NHL: Jets @ Red Wings @ Oct 4 7:00 PM :Disney+ SE"
	if strings.Contains(n, " @ ") || strings.Contains(lower, " vs ") || strings.Contains(lower, " vs. ") {
		return true
	}
	return false
}

func isJunkTeamFeed(name string) bool {
	if isEventSlotChannel(name) {
		return true
	}
	if isBeINChannel(name) {
		return true
	}
	upper := strings.ToUpper(name)
	for _, bad := range []string{
		"8K EXCLUSIVE", "LEAGUE PASS", "PPV", "SUMMER LEAGUE",
		"FLSP", "FLO (", "UFC", "PEACOCK",
	} {
		if strings.Contains(upper, bad) {
			return true
		}
	}
	return false
}

// isRegionalSportsFeed reports RSN / team-network style channel titles.
func isRegionalSportsFeed(name string) bool {
	n := strings.ToLower(name)
	for _, p := range []string{
		"sportsnet", "spectrum sport", "bally", "yes network", "nesn", "msg",
		"altitude", "fan duel", "fanduel", "space city", "masn", "root sports",
		"monumental", "sny", "marquee", "chwsn", "nbc sports",
	} {
		if strings.Contains(n, p) {
			return true
		}
	}
	return false
}

// mentionsForeignTeam is true when the channel is explicitly tagged with a team
// that is not playing (e.g. Spectrum SportsNet [LA Dodgers] on a Lakers card).
func mentionsForeignTeam(name string, allowed map[string]struct{}) bool {
	if len(allowed) == 0 {
		return false
	}
	tag := taggedTeamNickname(name)
	if tag == "" {
		return false
	}
	if _, ok := allowed[tag]; ok {
		return false
	}
	// Tag might be a city abbreviation ("la"); only treat as foreign when longer.
	return len(tag) >= 4
}

func taggedTeamNickname(name string) string {
	for _, pair := range [][2]string{{"[", "]"}, {"(", ")"}} {
		open, close := pair[0], pair[1]
		if i := strings.Index(name, open); i >= 0 {
			rest := name[i+len(open):]
			if j := strings.Index(rest, close); j > 0 {
				inner := strings.TrimSpace(rest[:j])
				fields := strings.Fields(inner)
				if len(fields) == 0 {
					continue
				}
				return normalizeKey(fields[len(fields)-1])
			}
		}
	}
	return ""
}

func channelScore(want, haveKey, haveName string) float64 {
	if want == "" || haveKey == "" {
		return 0
	}
	if want == haveKey {
		return 1.0
	}
	w := stripQualifiers(want)
	h := stripQualifiers(haveKey)
	if w == h && w != "" {
		return 0.95
	}
	_, haveBrand := splitPackageBrand(haveName)
	if brandsMatch(w, haveBrand) {
		return 0.93
	}
	if wc, hc := brandCore(w), brandCore(h); wc != "" && brandsMatch(wc, hc) {
		return 0.93
	}
	if strings.HasPrefix(haveKey, want) || strings.HasPrefix(want, haveKey) {
		shorter := len(want)
		if len(haveKey) < shorter {
			shorter = len(haveKey)
		}
		longer := len(want)
		if len(haveKey) > longer {
			longer = len(haveKey)
		}
		if shorter >= 4 {
			return 0.7 + 0.2*(float64(shorter)/float64(longer))
		}
	}
	if strings.Contains(haveKey, want) && len(want) >= 3 {
		if brandContains(haveKey, want) {
			return 0.72
		}
		if len(want) >= 5 {
			return 0.65
		}
	}
	if strings.Contains(want, haveKey) && len(haveKey) >= 5 {
		return 0.6
	}
	wt := tokens(want)
	ht := tokens(haveKey)
	if len(wt) == 0 || len(ht) == 0 {
		return 0
	}
	overlap := 0
	for _, a := range wt {
		for _, b := range ht {
			if a == b && len(a) >= 2 {
				overlap++
				break
			}
		}
	}
	if overlap == 0 {
		return 0
	}
	ratio := float64(overlap) / float64(max(len(wt), len(ht)))
	if ratio >= 0.75 && overlap >= 2 {
		return 0.6 + 0.2*ratio
	}
	return 0
}

func normalizeKey(name string) string {
	name = strings.ReplaceAll(strings.ToLower(strings.TrimSpace(name)), "+", "plus")
	var b strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func stripQualifiers(key string) string {
	suffixes := []string{"uhd", "4k", "hdr", "hdtv", "fhd", "hd", "sd", "us", "ca", "uk", "hevc", "h265", "raw"}
	out := key
	changed := true
	for changed {
		changed = false
		for _, s := range suffixes {
			if strings.HasSuffix(out, s) && len(out) > len(s)+2 {
				out = strings.TrimSuffix(out, s)
				changed = true
			}
		}
	}
	return out
}

// brandCore strips a single package-like prefix when the key has no colon context.
func brandCore(key string) string {
	k := stripQualifiers(strings.ToLower(strings.TrimSpace(key)))
	// Prefer structural parse when callers pass a display name via channelScore path;
	// for bare keys, peel one short alphabetic prefix (us/ca/tv/…).
	for _, p := range []string{
		"us", "ca", "uk", "arg", "vo", "co", "mx", "br", "au", "nl", "amp", "att",
		"tv", "vip", "af", "nz", "ph", "th", "cg", "meo", "gold", "prime", "trvip", "caen",
	} {
		if strings.HasPrefix(k, p) && len(k) > len(p)+2 {
			return stripQualifiers(k[len(p):])
		}
	}
	return k
}

// brandsMatch reports whether two normalized brand keys are the same network,
// allowing soft suffixes (nbatv≈nbatvph, spectrumsports≈spectrumsportsnet)
// but not numbered siblings (espn≠espn2).
func brandsMatch(want, have string) bool {
	w := stripQualifiers(want)
	h := stripQualifiers(have)
	if w == "" || h == "" {
		return false
	}
	if w == h {
		return true
	}
	if strings.HasPrefix(h, w) {
		return isSoftBrandSuffix(h[len(w):])
	}
	if strings.HasPrefix(w, h) && len(h) >= 5 {
		return isSoftBrandSuffix(w[len(h):])
	}
	return false
}

func isSoftBrandSuffix(rest string) bool {
	if rest == "" {
		return true
	}
	// Numbered networks / sub-brands are distinct (ESPN2, ESPNU, ESPN+, ESPN Deportes).
	if rest[0] >= '0' && rest[0] <= '9' {
		return false
	}
	for _, bad := range []string{"plus", "deportes", "news", "u", "2", "3"} {
		if rest == bad || strings.HasPrefix(rest, bad) {
			return false
		}
	}
	for _, s := range []string{
		"network", "net", "tv", "international", "intl", "philippines", "philipnes", "ph",
	} {
		if rest == s {
			return true
		}
		if strings.HasPrefix(rest, s) && isSoftBrandSuffix(rest[len(s):]) {
			return true
		}
	}
	// Explicit region crumbs only (nbatvca) — never treat arbitrary 1–2 letter tails as soft.
	switch rest {
	case "ca", "uk", "us", "mx", "br", "nz", "th", "af", "au", "nl", "ph":
		return true
	}
	return false
}

// brandContains is true when want appears as its own brand token inside haveKey
// (espn in usespnhd yes; espn in usespn2hd / usespnu no).
func brandContains(haveKey, want string) bool {
	if want == "" || haveKey == "" {
		return false
	}
	core := brandCore(haveKey)
	if brandsMatch(want, core) {
		return true
	}
	if strings.HasPrefix(core, want) {
		rest := core[len(want):]
		if rest == "" {
			return true
		}
		if rest[0] >= '0' && rest[0] <= '9' {
			return false
		}
		for _, extra := range []string{"plus", "news", "u", "deportes", "premium", "extra", "play"} {
			if rest == extra || strings.HasPrefix(rest, extra) {
				return false
			}
		}
		return false
	}
	return false
}

func tokens(key string) []string {
	s := key
	for _, brand := range []string{"sportsnet", "tsn", "espn", "nbc", "cbs", "fox", "abc", "cbc", "tnt", "nba", "nhl", "mlb", "nfl", "bein", "dazn", "paramount"} {
		if strings.HasPrefix(s, brand) && len(s) > len(brand) {
			rest := s[len(brand):]
			out := []string{brand}
			if rest != "" {
				out = append(out, rest)
			}
			return out
		}
	}
	return []string{s}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// ParseEventTeams extracts home/away from titles like "Away @ Home", "Home vs Away",
// or EPG forms such as "Montreal Canadiens - Carolina Hurricanes".
func ParseEventTeams(title string) (home, away string) {
	title = strings.TrimSpace(title)
	if title == "" {
		return "", ""
	}
	title = stripEPGMatchupDecorations(title)
	lower := strings.ToLower(title)
	for _, prefix := range []string{"live: ", "live - ", "hokej - ", "hokej na ledu – ", "hokej na ledu - "} {
		if strings.HasPrefix(lower, prefix) {
			title = strings.TrimSpace(title[len(prefix):])
			lower = strings.ToLower(title)
			break
		}
	}
	for _, prefix := range []string{"nhl: ", "nhl - ", "nhl – ", "nba: ", "mlb: ", "nfl: "} {
		if strings.HasPrefix(lower, prefix) {
			title = strings.TrimSpace(title[len(prefix):])
			lower = strings.ToLower(title)
			break
		}
	}
	for _, sep := range []string{" @ ", " at ", " AT "} {
		if i := strings.Index(title, sep); i > 0 {
			away = cleanTeamName(title[:i])
			home = cleanTeamName(title[i+len(sep):])
			if looksLikeMatchupSide(home) && looksLikeMatchupSide(away) {
				return home, away
			}
		}
	}
	for _, sep := range []string{" vs. ", " vs ", " v ", " VS ", " Vs ", " x ", " / "} {
		if i := strings.Index(title, sep); i > 0 {
			left := cleanTeamName(title[:i])
			right := cleanTeamName(title[i+len(sep):])
			if looksLikeMatchupSide(left) && looksLikeMatchupSide(right) {
				return left, right
			}
		}
	}
	for _, sep := range []string{" - ", " – ", " — "} {
		parts := strings.Split(title, sep)
		if len(parts) < 2 {
			continue
		}
		// Prefer the first pair for "Bayern - Union" / "SPAIN - CZECHIA".
		left := cleanTeamName(parts[0])
		right := cleanTeamName(parts[1])
		if looksLikeMatchupSide(left) && looksLikeMatchupSide(right) {
			return left, right
		}
		left = cleanTeamName(parts[len(parts)-2])
		right = cleanTeamName(parts[len(parts)-1])
		if looksLikeMatchupSide(left) && looksLikeMatchupSide(right) {
			return left, right
		}
	}
	return "", ""
}

// stripEPGMatchupDecorations keeps "Estonia vs Iceland" from
// "Estonia vs Iceland - UEFA Nations League 2026/27 - Match Day 4".
func stripEPGMatchupDecorations(title string) string {
	title = strings.TrimSpace(title)
	lower := strings.ToLower(title)
	for _, marker := range []string{" - uefa", " – uefa", " - fifa", " - match day", " - md ", " - round "} {
		if i := strings.Index(lower, marker); i > 0 {
			title = strings.TrimSpace(title[:i])
			lower = strings.ToLower(title)
			break
		}
	}
	// "SPAIN - CZECHIA 3/10/26" / "SPAIN - CZECHIA 3\/10\/26"
	title = strings.ReplaceAll(title, `\/`, `/`)
	fields := strings.Fields(title)
	if len(fields) >= 2 {
		last := fields[len(fields)-1]
		if strings.Contains(last, "/") || (len(last) >= 6 && looksLikeDateToken(last)) {
			title = strings.TrimSpace(strings.Join(fields[:len(fields)-1], " "))
		}
	}
	return title
}

func looksLikeDateToken(s string) bool {
	digits := 0
	for _, r := range s {
		if r >= '0' && r <= '9' {
			digits++
		}
	}
	return digits >= 4 && digits >= len(s)/2
}

func cleanTeamName(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	s = strings.TrimSuffix(s, "(Direto)")
	s = strings.TrimSpace(s)
	if i := strings.Index(s, "("); i > 0 {
		s = strings.TrimSpace(s[:i])
	}
	for _, p := range []string{"wnba:", "nba:", "nhl:", "mlb:", "nfl:", "cfl:", "basketball:", "football:"} {
		if strings.HasPrefix(strings.ToLower(s), p) {
			s = strings.TrimSpace(s[len(p):])
		}
	}
	s = strings.TrimRight(s, " .;,-")
	return strings.TrimSpace(s)
}

func isProseEPGTitle(title string) bool {
	t := strings.TrimSpace(title)
	if len(t) > 90 {
		return true
	}
	// ". " usually means a sentence blurb — but team names use St./N.Y./etc.
	if proseSentencePeriodCount(t) >= 1 || strings.Count(t, ",") >= 3 {
		return true
	}
	// Long descriptive blurbs used as XMLTV titles.
	if len(strings.Fields(t)) > 12 {
		return true
	}
	lower := strings.ToLower(t)
	for _, bad := range []string{
		"verslag van", "a look", "the upcoming", "holds a", "host the",
		"medical detectives", "geheimnisse", "barbapapa", "bonsoir",
		"mayday", "genitori", "influencer", "ultimate cut", "calígula", "caligula",
		"jerry maguire", "hátraarc", "hatraarc",
	} {
		if strings.Contains(lower, bad) {
			return true
		}
	}
	return false
}

// proseSentencePeriodCount counts ". " separators that look like sentence ends,
// ignoring common title abbreviations (St. Louis, N.Y. Rangers, vs. …).
func proseSentencePeriodCount(s string) int {
	n := 0
	lower := strings.ToLower(s)
	for i := 0; i+1 < len(lower); i++ {
		if lower[i] != '.' || lower[i+1] != ' ' {
			continue
		}
		j := i - 1
		for j >= 0 && lower[j] >= 'a' && lower[j] <= 'z' {
			j--
		}
		tok := lower[j+1 : i]
		switch tok {
		case "st", "mt", "ft", "pt", "jr", "sr", "vs", "fc", "sc", "ac",
			"dr", "mr", "mrs", "ms", "u", "s", "n", "y", "e", "w", "d", "c":
			// St. Louis, N.Y., U.S., D.C., vs., etc.
			continue
		}
		if tok == "" {
			continue
		}
		n++
	}
	return n
}

func sideHasNonASCIILetter(s string) bool {
	for _, r := range s {
		if r > unicode.MaxASCII && unicode.IsLetter(r) {
			return true
		}
	}
	return false
}

func looksLikeLocalTitleArticle(s string) bool {
	lower := strings.ToLower(strings.TrimSpace(s)) + " "
	for _, a := range []string{
		"a ", "az ", "der ", "die ", "das ", "le ", "la ", "les ", "el ", "los ", "las ",
		"il ", "lo ", "gli ", "un ", "una ", "une ", "ein ", "eine ", "the ",
	} {
		if strings.HasPrefix(lower, a) {
			return true
		}
	}
	return false
}

func looksSportsClubby(s string) bool {
	lower := strings.ToLower(s)
	for _, tok := range []string{
		" fc", "fc ", " united", " city", " cf", " sc ", " ac ", " afc", " club",
		"ers", "ics", "sox", "jays", "mets", "nicks", "lakers", "celtics",
		"rangers", "bruins", "leafs", "canadiens", "oilers", "flames",
		"blues", "sharks", "kings", "ducks", "jets", "wild", "knights",
		"predators", "hurricanes", "avalanche", "sabres", "senators",
		"flyers", "penguins", "islanders", "capitals", "devils", "panthers",
		"lightning", "blackhawks", "red wings", "maple leafs", "blue jackets",
		"cardinals", "yankees", "dodgers", "cubs", "giants", "padres",
	} {
		if strings.Contains(lower, tok) {
			return true
		}
	}
	return false
}

// isLikelyDualLanguageMovieTitle catches EPG dual titles like
// "Jerry Maguire vs A nagy hátraarc" (English vs local film title).
func isLikelyDualLanguageMovieTitle(home, away, title string) bool {
	if DetectSportFromTitle(title, "") != "" || DetectSportFromTitle(home+" vs "+away, "") != "" {
		return false
	}
	if looksSportsClubby(home) || looksSportsClubby(away) {
		return false
	}
	hNon := sideHasNonASCIILetter(home)
	aNon := sideHasNonASCIILetter(away)
	if hNon != aNon {
		return true
	}
	// Same script, but one side is a localized "A/The …" title and the other is not.
	hArt := looksLikeLocalTitleArticle(home)
	aArt := looksLikeLocalTitleArticle(away)
	if hArt != aArt {
		return true
	}
	return false
}

// IsPlausibleSportsMatchup rejects TV-show "vs" titles and league-label splits
// ("Američki fudbal vs NFL: 49ers") that are not real team matchups.
func IsPlausibleSportsMatchup(sport, home, away, title string) bool {
	return isPlausibleSportsMatchup(sport, home, away, title)
}

func isPlausibleSportsMatchup(sport, home, away, title string) bool {
	if !looksLikeMatchupSide(home) || !looksLikeMatchupSide(away) {
		return false
	}
	if isProseEPGTitle(title) || isReplayOrFillerEPG(title, "") {
		return false
	}
	if isLikelyDualLanguageMovieTitle(home, away, title) {
		return false
	}
	blob := strings.ToLower(home + " | " + away + " | " + title)
	for _, bad := range []string{
		"američki fudbal", "americki fudbal", "košarka", "kosarka", "nba liga",
		"the upcoming", "a look", "verslag", "holds a", "host the",
		"chase center", "state farm arena", "pre- season", "pre-season",
		"medical detectives", "geheimnisse", "barbapapa", "bonsoir", "mayday",
		"genitori", "influencer", "ultimate cut", "décodage", "decodage",
		"alarm im cockpit", "una gran familia",
		"hokej vs", "hokej:", "liga mistrzów vs", "liga prvakov",
		"jerry maguire", "hátraarc", "hatraarc",
	} {
		if strings.Contains(blob, bad) {
			return false
		}
	}
	for _, side := range []string{home, away} {
		sl := strings.ToLower(strings.TrimSpace(side))
		if sl == "" {
			return false
		}
		if strings.HasPrefix(sl, "nfl:") || strings.HasPrefix(sl, "nba:") ||
			strings.HasPrefix(sl, "mlb:") || strings.HasPrefix(sl, "nhl:") ||
			strings.HasPrefix(sl, "nba liga") || strings.HasPrefix(sl, "basketball:") ||
			strings.HasPrefix(sl, "hokej:") || strings.HasPrefix(sl, "live basketball:") ||
			sl == "basketball" || sl == "football" || sl == "baseball" || sl == "hockey" ||
			sl == "hokej" || sl == "košarka" || sl == "kosarka" {
			return false
		}
		// Trailing junk after a dash often means a bad split ("Indiana - Pre- Season NBA - 2026").
		if strings.Contains(sl, "pre-") || strings.Contains(sl, "season nba") ||
			strings.Contains(sl, "gespeeld") || strings.Contains(sl, "wedstrijd") {
			return false
		}
	}
	homeWords := len(strings.Fields(home))
	awayWords := len(strings.Fields(away))
	sportHint := DetectSportFromTitle(title, "") != "" || DetectSportFromTitle(home+" vs "+away, "") != ""
	// Multi-word sides are common for clubs — but only trust them with a sport
	// signal, otherwise dual-language movie titles sail through.
	if homeWords >= 2 && awayWords >= 2 {
		if sportHint {
			return true
		}
		switch strings.ToLower(strings.TrimSpace(sport)) {
		case "football", "soccer", "ice hockey", "hockey", "basketball",
			"baseball", "american football", "tennis", "rugby", "cricket", "volleyball":
			// Caller already classified a sport (EPG category / ESPN). Allow clubs.
			return true
		default:
			return false
		}
	}
	if sportHint {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(sport)) {
	case "football", "soccer":
		// National teams are often single tokens (Spain vs Czechia).
		return homeWords >= 1 && awayWords >= 1 && len(home) >= 4 && len(away) >= 4
	default:
		return false
	}
}

func looksLikeTeam(s string) bool {
	s = strings.TrimSpace(s)
	if len(s) < 3 || len(s) > 60 {
		return false
	}
	letters := 0
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			letters++
		}
	}
	return letters >= 3
}

// looksLikeMatchupSide accepts club names and single-word national teams (Estonia, Spain).
func looksLikeMatchupSide(s string) bool {
	s = strings.TrimSpace(s)
	if !looksLikeTeam(s) {
		return false
	}
	fields := strings.Fields(s)
	if len(fields) >= 2 {
		return len(s) >= 5 && len(s) <= 60
	}
	// National teams / short club tokens.
	return len(s) >= 4 && len(s) <= 24
}

// DetectSportFromTitle maps EPG titles/categories onto TheSportsDB sport names.
func DetectSportFromTitle(title, category string) string {
	t := strings.ToLower(title + " " + category)
	switch {
	case strings.Contains(t, "nhl") || strings.Contains(t, "hockey") || strings.Contains(t, "hokej") ||
		strings.Contains(t, "ishockey") || strings.Contains(t, "canadiens") || strings.Contains(t, "maple leafs"):
		return "Ice Hockey"
	case strings.Contains(t, "nba") || strings.Contains(t, "wnba") || strings.Contains(t, "basketball") ||
		strings.Contains(t, "koszykówka") || strings.Contains(t, "koszykowka"):
		return "Basketball"
	case strings.Contains(t, "mlb") || strings.Contains(t, "baseball") || strings.Contains(t, "beisbol") || strings.Contains(t, "béisbol") ||
		strings.Contains(t, "alds") || strings.Contains(t, "nlds") || strings.Contains(t, "world series"):
		return "Baseball"
	case strings.Contains(t, "nfl") || strings.Contains(t, "cfl") || strings.Contains(t, "american football") ||
		strings.Contains(t, "ncaaf") || strings.Contains(t, "alouettes") || strings.Contains(t, "roughriders") ||
		strings.Contains(t, "stampeders") || strings.Contains(t, "tiger-cats") || strings.Contains(t, "blue bombers"):
		return "American Football"
	case strings.Contains(t, "mls") || strings.Contains(t, "soccer") || strings.Contains(t, "premier league") ||
		strings.Contains(t, "uefa") || strings.Contains(t, "fifa") || strings.Contains(t, "nations league") ||
		strings.Contains(t, "bundesliga") || strings.Contains(t, "laliga") || strings.Contains(t, "la liga") ||
		strings.Contains(t, "campeones") || strings.Contains(t, "championship") || strings.Contains(t, "fotboll") ||
		strings.Contains(t, "piłka") || strings.Contains(t, "pilka") || strings.Contains(t, "futebol") ||
		strings.Contains(t, "fútbol") || strings.Contains(t, "futbol") || strings.Contains(t, "europa league") ||
		strings.Contains(t, "conference league") || strings.Contains(t, "ligue 1") || strings.Contains(t, "serie a"):
		return "Football"
	case strings.Contains(t, "ufc") || strings.Contains(t, "mma") || strings.Contains(t, "boxing") ||
		strings.Contains(t, "boks") || strings.Contains(t, "fighting") || strings.Contains(t, "bellator") ||
		strings.Contains(t, "pfl"):
		return "Fighting"
	case strings.Contains(t, "tennis") || strings.Contains(t, "atp") || strings.Contains(t, "wta"):
		return "Tennis"
	case strings.Contains(t, "golf") || strings.Contains(t, "pga") || strings.Contains(t, "lpga") ||
		strings.Contains(t, "dp world"):
		return "Golf"
	case strings.Contains(t, "formula") || strings.Contains(t, "formel") || strings.Contains(t, "f1") ||
		strings.Contains(t, "motogp") || strings.Contains(t, "nascar") || strings.Contains(t, "indycar") ||
		strings.Contains(t, "wrc") || strings.Contains(t, "rally"):
		return "MotorSport"
	case strings.Contains(t, "volleyball") || strings.Contains(t, "siatkówka") || strings.Contains(t, "siatkowka"):
		return "Volleyball"
	case strings.Contains(t, "rugby"):
		return "Rugby"
	case strings.Contains(t, "cricket"):
		return "Cricket"
	default:
		return ""
	}
}

func stripLivePrefix(t string) string {
	t = strings.ToLower(strings.TrimSpace(t))
	for _, p := range []string{"live: ", "live - ", "live "} {
		t = strings.TrimPrefix(t, p)
	}
	return strings.TrimSpace(t)
}

func IsGenericLeagueTitle(title, sport string) bool {
	t := stripLivePrefix(title)
	switch sport {
	case "Ice Hockey":
		return t == "nhl hockey" || t == "hockey lnh" || t == "nhl" || t == "ishockey: nhl" ||
			strings.HasPrefix(t, "nhl hockey") || strings.HasPrefix(t, "hockey lnh") ||
			strings.HasPrefix(t, "ishockey:") || strings.Contains(t, "stanley cup")
	case "Basketball":
		return t == "nba basketball" || t == "nba" || strings.HasPrefix(t, "nba basketball") ||
			strings.Contains(t, "koszykówka") || strings.Contains(t, "koszykowka") ||
			strings.Contains(t, "nba ") && (strings.Contains(t, "finals") || strings.Contains(t, "playoff"))
	case "Baseball":
		if t == "mlb" || t == "mlb baseball" || t == "baseball mlb" || t == "baseball: mlb" ||
			t == "béisbol mlb" || t == "beisbol mlb" {
			return true
		}
		if strings.Contains(t, "alds") || strings.Contains(t, "nlds") || strings.Contains(t, "world series") ||
			strings.Contains(t, "division series") || strings.Contains(t, "divisional series") ||
			strings.Contains(t, "serie divisional") || strings.Contains(t, "série de division") {
			return true
		}
		if strings.Contains(t, "mlb") && (strings.Contains(t, "baseball") || strings.Contains(t, "beisbol") ||
			strings.Contains(t, "béisbol") || strings.Contains(t, "play-off") || strings.Contains(t, "playoff")) {
			return true
		}
		return strings.HasPrefix(t, "mlb ") || strings.HasPrefix(t, "baseball:")
	case "American Football":
		return t == "nfl football" || t == "nfl" || t == "cfl" || t == "cfl football" ||
			strings.HasPrefix(t, "nfl football") || strings.HasPrefix(t, "cfl ") ||
			strings.Contains(t, "nfl ") && strings.Contains(t, "football")
	case "Football", "Soccer":
		// "Estonia vs Iceland - UEFA Nations League" is a matchup, not a league block.
		if h, a := ParseEventTeams(title); h != "" && a != "" {
			return false
		}
		return strings.HasPrefix(t, "fotboll:") || strings.HasPrefix(t, "piłka nożna:") ||
			strings.HasPrefix(t, "pilka nozna:") || t == "premier league" || t == "premier league channel" ||
			strings.Contains(t, "europa league") || strings.Contains(t, "conference league") ||
			t == "uefa nations league" || strings.Contains(t, "nations league") && !strings.Contains(t, " vs") ||
			strings.Contains(t, "laliga") || strings.Contains(t, "liga de campeones") ||
			strings.Contains(t, "m+ laliga") || strings.Contains(t, "engelska championship") ||
			(strings.Contains(t, "championship") && !strings.Contains(t, " vs"))
	case "MotorSport":
		return t == "formel 1" || t == "formula 1" || t == "f1" || strings.HasPrefix(t, "formel") ||
			strings.HasPrefix(t, "formula")
	case "Fighting":
		return t == "mma" || t == "ufc" || strings.Contains(t, "boxing") || strings.Contains(t, "boks")
	case "Volleyball":
		return strings.Contains(t, "siatkówka") || strings.Contains(t, "siatkowka") || strings.Contains(t, "volleyball")
	default:
		return false
	}
}

// ParseTeamsFromDescription pulls matchups buried in EPG descriptions.
func ParseTeamsFromDescription(desc string) (home, away string) {
	desc = strings.TrimSpace(desc)
	if desc == "" {
		return "", ""
	}
	// Only the first line — full descriptions are prose and throw false positives.
	first := desc
	if i := strings.IndexAny(desc, "\r\n"); i >= 0 {
		first = strings.TrimSpace(desc[:i])
	}
	if len(first) > 120 {
		first = first[:120]
	}
	if h, a := ParseEventTeams(first); h != "" && a != "" && looksLikeMatchupSide(h) && looksLikeMatchupSide(a) {
		return h, a
	}
	lower := strings.ToLower(desc)
	for _, marker := range []string{"wedstrijd ", "match ", "game ", "partido "} {
		if i := strings.Index(lower, marker); i >= 0 && i < 80 {
			rest := desc[i+len(marker):]
			if j := strings.IndexAny(rest, "\r\n."); j >= 0 {
				rest = rest[:j]
			}
			if h, a := ParseEventTeams(rest); h != "" && a != "" && looksLikeMatchupSide(h) && looksLikeMatchupSide(a) {
				return h, a
			}
		}
	}
	return "", ""
}

func DefaultDuration(sport string) time.Duration {
	switch strings.ToLower(strings.TrimSpace(sport)) {
	case "soccer", "football":
		return 2*time.Hour + 45*time.Minute
	case "basketball":
		return 2*time.Hour + 45*time.Minute
	case "ice hockey", "hockey":
		return 3 * time.Hour
	case "baseball":
		return 3*time.Hour + 30*time.Minute
	case "american football":
		return 3*time.Hour + 45*time.Minute
	case "tennis":
		return 3 * time.Hour
	case "golf":
		return 5 * time.Hour
	default:
		return 3 * time.Hour
	}
}
