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
	default:
		return ""
	}
}

// FindMatch locates a streamed.pk row for a Stevie sports event.
func FindMatch(matches []Match, sport, home, away string, startsAt time.Time) *Match {
	return findMatch(matches, sport, home, away, startsAt)
}

func findMatch(matches []Match, sport, home, away string, startsAt time.Time) *Match {
	wantCat := sportCategory(sport)
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
