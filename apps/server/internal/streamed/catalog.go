package streamed

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// ExternalSportLabel is the Sports tab category for the full streamed.pk catalog.
const ExternalSportLabel = "External (streamed.pk)"

// SportInfo is a streamed.pk /api/sports row.
type SportInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ListSports returns streamed.pk sport categories.
func (c *Client) ListSports(ctx context.Context) ([]SportInfo, error) {
	if c == nil {
		return nil, nil
	}
	var out []SportInfo
	if err := c.getJSON(ctx, "/api/sports", &out); err != nil {
		return nil, err
	}
	return out, nil
}

// AllMatches returns the full catalog (live + all + every per-sport list), cached.
func (c *Client) AllMatches(ctx context.Context) ([]Match, error) {
	return c.refreshMatches(ctx)
}

// LiveMatchIDs returns ids currently listed on /api/matches/live.
func (c *Client) LiveMatchIDs(ctx context.Context) (map[string]struct{}, error) {
	if c == nil {
		return nil, nil
	}
	var live []Match
	if err := c.getJSON(ctx, "/api/matches/live", &live); err != nil {
		return nil, err
	}
	out := make(map[string]struct{}, len(live))
	for _, m := range live {
		if m.ID != "" {
			out[m.ID] = struct{}{}
		}
	}
	return out, nil
}

// ChannelsForMatch builds playable rows from match sources (no per-stream HTTP).
func ChannelsForMatch(m Match) []Channel {
	return sourcesToChannels(m.Sources)
}

func sourcesToChannels(sources []MatchSource) []Channel {
	ordered := orderSources(sources)
	out := make([]Channel, 0, len(ordered))
	seen := map[string]struct{}{}
	for _, s := range ordered {
		src := strings.TrimSpace(s.Source)
		id := strings.TrimSpace(s.ID)
		if src == "" || id == "" {
			continue
		}
		key := strings.ToLower(src + "|" + id)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, Channel{
			Name:     fmt.Sprintf("streamed.pk · %s", src),
			Source:   src,
			SourceID: id,
			StreamNo: 1,
		})
	}
	return out
}

// PrettyCategory turns streamed category ids into display labels.
func PrettyCategory(cat string) string {
	switch strings.ToLower(strings.TrimSpace(cat)) {
	case "basketball":
		return "Basketball"
	case "football":
		return "Football"
	case "american-football":
		return "American Football"
	case "hockey":
		return "Hockey"
	case "baseball":
		return "Baseball"
	case "motor-sports":
		return "Motor Sports"
	case "fight":
		return "Fight"
	case "tennis":
		return "Tennis"
	case "rugby":
		return "Rugby"
	case "golf":
		return "Golf"
	case "billiards":
		return "Billiards"
	case "afl":
		return "AFL"
	case "darts":
		return "Darts"
	case "cricket":
		return "Cricket"
	case "other":
		return "Other"
	default:
		cat = strings.TrimSpace(cat)
		if cat == "" {
			return "Other"
		}
		return strings.ReplaceAll(cat, "-", " ")
	}
}

// MatchSideNames returns home/away for UI; non-team rows use title + category.
func MatchSideNames(m Match) (home, away string) {
	home, away = matchTeams(&m)
	if home != "" || away != "" {
		if home == "" {
			home = strings.TrimSpace(m.Title)
		}
		if away == "" {
			away = PrettyCategory(m.Category)
		}
		return home, away
	}
	title := strings.TrimSpace(m.Title)
	if title == "" {
		title = m.ID
	}
	return title, PrettyCategory(m.Category)
}

// MatchStart returns tip-off time; zero/invalid dates yield ok=false.
func MatchStart(m Match) (t time.Time, ok bool) {
	if m.Date < 1_000_000_000_000 { // before ~2001 in ms → junk / always-on
		return time.Time{}, false
	}
	return time.UnixMilli(m.Date).UTC(), true
}

// AbsoluteURL joins a streamed.pk relative image path onto the API base.
func (c *Client) AbsoluteURL(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return strings.TrimRight(c.base, "/") + path
}
