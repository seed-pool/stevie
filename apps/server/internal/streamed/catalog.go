package streamed

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// ExternalSportLabel is the Sports tab category for the full ppv.st catalog.
const ExternalSportLabel = "External (ppv.st)"

// SportInfo is a ppv.st category row (derived from /api/streams).
type SportInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ListSports returns ppv.st sport categories from the cached catalog.
func (c *Client) ListSports(ctx context.Context) ([]SportInfo, error) {
	if c == nil {
		return nil, nil
	}
	matches, err := c.refreshMatches(ctx)
	if err != nil {
		return nil, err
	}
	seen := map[string]string{}
	for _, m := range matches {
		slug := strings.TrimSpace(m.Category)
		if slug == "" {
			continue
		}
		if _, ok := seen[slug]; ok {
			continue
		}
		seen[slug] = PrettyCategory(slug)
	}
	out := make([]SportInfo, 0, len(seen))
	for id, name := range seen {
		out = append(out, SportInfo{ID: id, Name: name})
	}
	return out, nil
}

// AllMatches returns the full catalog, cached.
func (c *Client) AllMatches(ctx context.Context) ([]Match, error) {
	return c.refreshMatches(ctx)
}

// LiveMatchIDs returns ids currently considered live (always-on or in window).
func (c *Client) LiveMatchIDs(ctx context.Context) (map[string]struct{}, error) {
	if c == nil {
		return nil, nil
	}
	if _, err := c.refreshMatches(ctx); err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(map[string]struct{}, len(c.liveIDs))
	for id := range c.liveIDs {
		out[id] = struct{}{}
	}
	// If cache had no live set, derive from matches.
	if len(out) == 0 {
		now := time.Now()
		for _, m := range c.matches {
			if isPPVLive(m, now) {
				out[m.ID] = struct{}{}
			}
		}
	}
	return out, nil
}

// ChannelsForMatch builds playable rows from match sources (no per-stream HTTP).
func ChannelsForMatch(m Match) []Channel {
	return sourcesToChannels(m.Sources)
}

func sourcesToChannels(sources []MatchSource) []Channel {
	out := make([]Channel, 0, len(sources))
	seen := map[string]struct{}{}
	for _, s := range sources {
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
		label := strings.TrimSpace(s.Label)
		if label == "" {
			label = "Stream"
		}
		embed := strings.TrimSpace(s.Embed)
		if embed == "" {
			embed = "https://" + src + "/embed/" + strings.TrimPrefix(id, "/")
		}
		out = append(out, Channel{
			Name:     fmt.Sprintf("ppv.st · %s", label),
			EmbedURL: embed,
			Language: label,
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
		parts := strings.Split(cat, "-")
		for i, p := range parts {
			if p == "" {
				continue
			}
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
		return strings.Join(parts, " ")
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

// AbsoluteURL joins a relative image path onto the API base (posters are usually absolute).
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
