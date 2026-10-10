package streamed

import (
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
)

func sportCategory(sport string) string {
	switch strings.ToLower(strings.TrimSpace(sport)) {
	case "basketball":
		return "basketball"
	case "baseball":
		return "baseball"
	case "ice hockey", "hockey":
		return "hockey"
	case "american football":
		return "american-football"
	case "football", "soccer":
		return "football"
	case "fighting", "fight", "mma", "ufc", "boxing":
		return "fight"
	case "motor sport", "motorsport", "motor-sports", "motor sports":
		return "motor-sports"
	case "golf":
		return "golf"
	case "tennis":
		return "tennis"
	default:
		return ""
	}
}

func isEventSport(sport string) bool {
	switch sportCategory(sport) {
	case "motor-sports", "golf", "fight", "tennis":
		return true
	default:
		return false
	}
}

// FindMatch locates a streamed.pk row for a Stevie sports event.
func FindMatch(matches []Match, sport, home, away, title string, startsAt time.Time) *Match {
	return findMatch(matches, sport, home, away, title, startsAt)
}

func findMatch(matches []Match, sport, home, away, title string, startsAt time.Time) *Match {
	wantCat := sportCategory(sport)
	// F1 / golf / fight cards are event titles, not club matchups. Never fall back to
	// last-word team tokens ("Sprint Race" → "race") — that false-matches other GPs.
	if isEventSport(sport) {
		return findEventMatch(matches, wantCat, home, away, title, startsAt)
	}

	homeTok := teamToken(home)
	awayTok := teamToken(away)
	if homeTok == "" || awayTok == "" {
		return nil
	}
	var best *Match
	bestScore := -1.0
	for i := range matches {
		m := &matches[i]
		if wantCat != "" && !strings.EqualFold(m.Category, wantCat) {
			continue
		}
		mh, ma := matchTeams(m)
		score := 0.0
		if mh != "" && ma != "" {
			if samePair(mh, ma, home, away) {
				score = 3
			} else {
				continue
			}
		} else {
			blob := normalizeKey(m.Title)
			if !(strings.Contains(blob, homeTok) && strings.Contains(blob, awayTok)) {
				continue
			}
			score = 2
		}
		if !startsAt.IsZero() && m.Date > 0 {
			diff := absDuration(startsAt.Sub(time.UnixMilli(m.Date)))
			if diff > 6*time.Hour {
				continue
			}
			// Prefer closer tip-offs.
			score += 1.0 - float64(diff)/float64(6*time.Hour)
		}
		if score > bestScore {
			bestScore = score
			best = m
		}
	}
	return best
}

// findEventMatch matches F1 / golf / fight cards by shared title tokens + time
// (these are not home/away club matchups).
func findEventMatch(matches []Match, wantCat, home, away, title string, startsAt time.Time) *Match {
	eventToks := eventTokens(title, home, away)
	if len(eventToks) == 0 {
		return nil
	}
	eventSession := sessionKind(title + " " + home + " " + away)

	var best *Match
	bestScore := -1.0
	for i := range matches {
		m := &matches[i]
		if wantCat != "" && !strings.EqualFold(m.Category, wantCat) {
			continue
		}
		if strings.TrimSpace(m.Title) == "" {
			continue
		}
		mToks := eventTokens(m.Title, "", "")
		shared := intersectTokens(eventToks, mToks)
		if len(shared) < 2 {
			// One strong venue/event token + close tip-off can still match.
			if len(shared) < 1 {
				continue
			}
		}
		mSession := sessionKind(m.Title)
		if eventSession != "" && mSession != "" && eventSession != mSession {
			continue
		}
		score := float64(len(shared))
		if eventSession != "" && eventSession == mSession {
			score += 1.5
		}
		if !startsAt.IsZero() && m.Date > 0 {
			diff := absDuration(startsAt.Sub(time.UnixMilli(m.Date)))
			if diff > 6*time.Hour {
				continue
			}
			score += 1.0 - float64(diff)/float64(6*time.Hour)
		} else if m.Date == 0 {
			// Always-on stubs (Rally TV) — only if many tokens overlap.
			if len(shared) < 3 {
				continue
			}
		}
		// Require at least one distinctive (non-generic) shared token.
		if !hasDistinctiveToken(shared) && len(shared) < 3 {
			continue
		}
		if score > bestScore {
			bestScore = score
			best = m
		}
	}
	return best
}

func eventTokens(parts ...string) []string {
	blob := fold(strings.Join(parts, " "))
	// Localize common GP spellings before tokenization.
	blob = strings.NewReplacer(
		"singapur", "singapore",
		"formel", "formula",
		"grande premio", "grand prix",
		"gran premio", "grand prix",
	).Replace(blob)
	fields := strings.Fields(blob)
	seen := map[string]struct{}{}
	var out []string
	for _, f := range fields {
		tok := normalizeKey(f)
		if !keepEventToken(tok) {
			continue
		}
		if _, ok := seen[tok]; ok {
			continue
		}
		seen[tok] = struct{}{}
		out = append(out, tok)
	}
	return out
}

func keepEventToken(tok string) bool {
	if tok == "" {
		return false
	}
	switch tok {
	case "vs", "at", "the", "and", "for", "with", "from", "live", "race",
		"round", "hours", "free", "tv", "gp", "prix", "grand", "series",
		"championship", "cup", "airlines", "mundial", "codigo", "code":
		return false
	}
	// Keep short sport codes (f1, f2, f3, ufc) and longer words.
	if len(tok) <= 2 {
		return tok == "f1" || tok == "f2" || tok == "f3"
	}
	return true
}

func hasDistinctiveToken(toks []string) bool {
	for _, t := range toks {
		switch t {
		case "sprint", "qualifying", "quali", "practice", "warmup", "formula",
			"nascar", "motogp", "moto3", "moto2", "indycar", "supercars",
			"dtm", "wec", "imsa":
			continue
		default:
			// Venue / event name (singapore, bahrain, bathurst…).
			if len(t) >= 5 {
				return true
			}
		}
	}
	return false
}

func intersectTokens(a, b []string) []string {
	set := map[string]struct{}{}
	for _, t := range b {
		set[t] = struct{}{}
	}
	var out []string
	for _, t := range a {
		if _, ok := set[t]; ok {
			out = append(out, t)
		}
	}
	return out
}

func sessionKind(s string) string {
	t := normalizeKey(fold(s))
	switch {
	case strings.Contains(t, "sprint"):
		return "sprint"
	case strings.Contains(t, "qualifying"), strings.Contains(t, "quali"), strings.Contains(t, "superpole"):
		return "qualifying"
	case strings.Contains(t, "practice"), strings.Contains(t, "fp1"), strings.Contains(t, "fp2"), strings.Contains(t, "fp3"):
		return "practice"
	case strings.Contains(t, "warmup"), strings.Contains(t, "warm"):
		return "warmup"
	case strings.Contains(t, "race"):
		return "race"
	default:
		return ""
	}
}

func matchTeams(m *Match) (home, away string) {
	if m == nil || m.Teams == nil {
		return "", ""
	}
	if m.Teams.Home != nil {
		home = strings.TrimSpace(m.Teams.Home.Name)
	}
	if m.Teams.Away != nil {
		away = strings.TrimSpace(m.Teams.Away.Name)
	}
	return home, away
}

func samePair(aHome, aAway, bHome, bAway string) bool {
	ah, aa := teamToken(aHome), teamToken(aAway)
	bh, ba := teamToken(bHome), teamToken(bAway)
	if ah == "" || aa == "" || bh == "" || ba == "" {
		return false
	}
	return (ah == bh && aa == ba) || (ah == ba && aa == bh)
}

func teamToken(name string) string {
	name = fold(strings.TrimSpace(name))
	fields := strings.Fields(name)
	if len(fields) == 0 {
		return ""
	}
	return normalizeKey(fields[len(fields)-1])
}

func normalizeKey(s string) string {
	s = fold(s)
	var b strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func fold(s string) string {
	repl := strings.NewReplacer(
		"é", "e", "è", "e", "ê", "e", "ë", "e",
		"á", "a", "à", "a", "ä", "a", "â", "a",
		"í", "i", "ó", "o", "ú", "u", "ç", "c", "ñ", "n",
		"É", "e", "È", "e", "Á", "a", "Ó", "o", "Ú", "u", "Ç", "c",
	)
	return repl.Replace(strings.ToLower(s))
}

func absDuration(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}

// Prefer reliable iframe sources first; admin works in-browser but is last resort.
func orderSources(in []MatchSource) []MatchSource {
	rank := map[string]int{
		"delta": 1, "hotel": 2, "golf": 3, "bravo": 4, "charlie": 5,
		"echo": 6, "foxtrot": 7, "alpha": 8, "intel": 9, "admin": 10,
	}
	out := append([]MatchSource(nil), in...)
	sort.SliceStable(out, func(i, j int) bool {
		ri, rj := rank[strings.ToLower(out[i].Source)], rank[strings.ToLower(out[j].Source)]
		if ri == 0 {
			ri = 50
		}
		if rj == 0 {
			rj = 50
		}
		return ri < rj
	})
	return out
}

func streamsToChannels(streams []Stream, limit int) []Channel {
	if limit <= 0 {
		limit = 3
	}
	sort.SliceStable(streams, func(i, j int) bool {
		if streams[i].HD != streams[j].HD {
			return streams[i].HD
		}
		return streams[i].StreamNo < streams[j].StreamNo
	})
	out := make([]Channel, 0, limit)
	seen := map[string]struct{}{}
	for _, s := range streams {
		embed := strings.TrimSpace(s.EmbedURL)
		if embed == "" || !strings.HasPrefix(embed, "https://") {
			continue
		}
		lang := strings.TrimSpace(s.Language)
		if lang == "" {
			lang = "Stream"
		}
		label := lang
		if s.HD {
			label += " HD"
		}
		name := "Streamed · " + label
		key := strings.ToLower(name + "|" + embed)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		src, sid, sno := parseEmbedSlot(embed, s.Source, s.StreamNo)
		out = append(out, Channel{
			Name:     name,
			EmbedURL: embed,
			Language: lang,
			HD:       s.HD,
			Source:   src,
			SourceID: sid,
			StreamNo: sno,
		})
		if len(out) >= limit {
			break
		}
	}
	if len(out) > 1 {
		for i := range out {
			if out[i].Source != "" {
				out[i].Name = fmt.Sprintf("%s · %s", out[i].Name, out[i].Source)
			}
		}
	}
	return out
}

// parseEmbedSlot extracts source/id/streamNo from
// https://embed.st/embed/{source}/{id}/{streamNo}
func parseEmbedSlot(embed, fallbackSource string, fallbackNo int) (source, id string, streamNo int) {
	source = strings.TrimSpace(fallbackSource)
	streamNo = fallbackNo
	if streamNo <= 0 {
		streamNo = 1
	}
	u, err := url.Parse(strings.TrimSpace(embed))
	if err != nil {
		return source, id, streamNo
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	// embed / {source} / {id} / {stream}
	if len(parts) >= 4 && parts[0] == "embed" {
		if parts[1] != "" {
			source = parts[1]
		}
		id = parts[2]
		if n, err := strconv.Atoi(parts[3]); err == nil && n > 0 {
			streamNo = n
		}
	}
	return source, id, streamNo
}
