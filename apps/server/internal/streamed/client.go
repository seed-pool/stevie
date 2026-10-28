package streamed

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const defaultBase = "https://api.ppv.st"

// Client talks to the public ppv.st REST API (no auth).
// Catalog: GET {base}/api/streams
type Client struct {
	base string
	http *http.Client

	mu        sync.Mutex
	matches   []Match
	matchesAt time.Time
	liveIDs   map[string]struct{}
}

func NewClient(baseURL string) *Client {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		base = defaultBase
	}
	// Accept either https://api.ppv.st or https://api.ppv.st/api.
	if strings.HasSuffix(base, "/api") {
		base = strings.TrimSuffix(base, "/api")
	}
	return &Client{
		base:  base,
		http:  &http.Client{Timeout: 20 * time.Second},
		liveIDs: map[string]struct{}{},
	}
}

type Match struct {
	ID       string        `json:"id"`
	Title    string        `json:"title"`
	Category string        `json:"category"`
	Date     int64         `json:"date"` // unix ms
	Poster   string        `json:"poster,omitempty"`
	Popular  bool          `json:"popular"`
	Teams    *MatchTeams   `json:"teams,omitempty"`
	Sources  []MatchSource `json:"sources"`
	Viewers  int           `json:"viewers,omitempty"`
	EndsAt   int64         `json:"ends_at,omitempty"` // unix ms; 0 unknown
	AlwaysOn bool          `json:"always_live,omitempty"`
}

type MatchTeams struct {
	Home *MatchTeam `json:"home,omitempty"`
	Away *MatchTeam `json:"away,omitempty"`
}

type MatchTeam struct {
	Name  string `json:"name"`
	Badge string `json:"badge"`
}

// MatchSource is one playable embed slot (default + substreams).
// Source is the embed hostname (e.g. taifood-blog.asia); ID is the /embed/ path.
type MatchSource struct {
	Source string `json:"source"`
	ID     string `json:"id"`
	Label  string `json:"label,omitempty"`
	Embed  string `json:"embed,omitempty"`
}

// Stream is retained for tests / older helpers that build channels from embed URLs.
type Stream struct {
	ID       string `json:"id"`
	StreamNo int    `json:"streamNo"`
	Language string `json:"language"`
	HD       bool   `json:"hd"`
	EmbedURL string `json:"embedUrl"`
	Source   string `json:"source"`
}

// Channel is a synthetic sports-menu row backed by ppv.st embeds.
type Channel struct {
	Name     string
	EmbedURL string
	Language string
	HD       bool
	Source   string // embed hostname
	SourceID string // embed path (uri_name)
	StreamNo int
}

type ppvStreamsResponse struct {
	Success bool          `json:"success"`
	Streams []ppvCategory `json:"streams"`
	Error   string        `json:"error"`
}

type ppvCategory struct {
	Category   string      `json:"category"`
	ID         int         `json:"id"`
	AlwaysLive any         `json:"always_live"`
	Streams    []ppvStream `json:"streams"`
}

type ppvStream struct {
	ID           int          `json:"id"`
	Name         string       `json:"name"`
	Tag          string       `json:"tag"`
	SourceTag    string       `json:"source_tag"`
	Poster       string       `json:"poster"`
	URIName      string       `json:"uri_name"`
	StartsAt     int64        `json:"starts_at"`
	EndsAt       int64        `json:"ends_at"`
	AlwaysLive   any          `json:"always_live"`
	CategoryName string       `json:"category_name"`
	Iframe       string       `json:"iframe"`
	Substreams   []ppvStream  `json:"substreams"`
	Viewers      any          `json:"viewers"`
}

func (c *Client) getJSON(ctx context.Context, path string, dest any) error {
	u := strings.TrimRight(c.base, "/") + path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "StevieSports/1.0")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Origin", "https://ppv.st")
	req.Header.Set("Referer", "https://ppv.st/")
	res, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 16<<20))
	if err != nil {
		return err
	}
	if res.StatusCode >= 400 {
		return fmt.Errorf("streamed: HTTP %d", res.StatusCode)
	}
	return json.Unmarshal(body, dest)
}

func truthy(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case float64:
		return t != 0
	case int:
		return t != 0
	case int64:
		return t != 0
	case string:
		s := strings.TrimSpace(strings.ToLower(t))
		return s == "1" || s == "true" || s == "yes"
	default:
		return false
	}
}

func asInt(v any) int {
	switch t := v.(type) {
	case float64:
		return int(t)
	case int:
		return t
	case int64:
		return int(t)
	case string:
		var n int
		fmt.Sscanf(strings.TrimSpace(t), "%d", &n)
		return n
	default:
		return 0
	}
}

func categorySlug(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = strings.ReplaceAll(s, "&", " and ")
	var b strings.Builder
	prevDash := false
	for _, r := range s {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			prevDash = false
		case r == ' ' || r == '/' || r == '_' || r == '-':
			if !prevDash && b.Len() > 0 {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	switch out {
	case "combat-sports", "mma", "boxing", "wrestling":
		return "fight"
	case "soccer":
		return "football"
	case "motorsport", "motorsports", "motor-sport":
		return "motor-sports"
	case "ice-hockey":
		return "hockey"
	case "australian-football", "afl":
		return "afl"
	default:
		return out
	}
}

func embedHostPath(iframe string) (host, path string, ok bool) {
	iframe = strings.TrimSpace(iframe)
	if iframe == "" {
		return "", "", false
	}
	u, err := url.Parse(iframe)
	if err != nil || u.Host == "" {
		return "", "", false
	}
	const marker = "/embed/"
	idx := strings.Index(u.Path, marker)
	if idx < 0 {
		return "", "", false
	}
	path = strings.TrimPrefix(u.Path[idx+len(marker):], "/")
	if path == "" {
		return "", "", false
	}
	return u.Host, path, true
}

func sourcesFromPPV(s ppvStream) []MatchSource {
	var out []MatchSource
	seen := map[string]struct{}{}
	add := func(iframe, label string) {
		host, path, ok := embedHostPath(iframe)
		if !ok {
			return
		}
		key := strings.ToLower(host + "|" + path)
		if _, dup := seen[key]; dup {
			return
		}
		seen[key] = struct{}{}
		if label == "" {
			label = "Stream"
		}
		out = append(out, MatchSource{
			Source: host,
			ID:     path,
			Label:  label,
			Embed:  strings.TrimSpace(iframe),
		})
	}
	label := strings.TrimSpace(s.SourceTag)
	if label == "" {
		label = strings.TrimSpace(s.Tag)
	}
	add(s.Iframe, label)
	for _, sub := range s.Substreams {
		subLabel := strings.TrimSpace(sub.SourceTag)
		if subLabel == "" {
			subLabel = strings.TrimSpace(sub.Tag)
		}
		if subLabel == "" {
			subLabel = label
		}
		iframe := sub.Iframe
		if iframe == "" && sub.URIName != "" {
			// Synthesize from parent host when API omits iframe.
			if host, _, ok := embedHostPath(s.Iframe); ok {
				iframe = "https://" + host + "/embed/" + strings.TrimPrefix(sub.URIName, "/")
			}
		}
		add(iframe, subLabel)
	}
	return out
}

func matchFromPPV(cat ppvCategory, s ppvStream) Match {
	catName := strings.TrimSpace(s.CategoryName)
	if catName == "" {
		catName = strings.TrimSpace(cat.Category)
	}
	id := strings.TrimSpace(s.URIName)
	if id == "" {
		id = fmt.Sprintf("%d", s.ID)
	}
	var dateMs int64
	if s.StartsAt > 1_000_000_000_000 {
		dateMs = s.StartsAt
	} else if s.StartsAt > 1_000_000_000 {
		dateMs = s.StartsAt * 1000
	}
	var endsMs int64
	if s.EndsAt > 1_000_000_000_000 {
		endsMs = s.EndsAt
	} else if s.EndsAt > 1_000_000_000 {
		endsMs = s.EndsAt * 1000
	}
	home, away := parseTitleTeams(s.Name)
	var teams *MatchTeams
	if home != "" && away != "" {
		teams = &MatchTeams{
			Home: &MatchTeam{Name: home},
			Away: &MatchTeam{Name: away},
		}
	}
	return Match{
		ID:       id,
		Title:    strings.TrimSpace(s.Name),
		Category: categorySlug(catName),
		Date:     dateMs,
		Poster:   strings.TrimSpace(s.Poster),
		Teams:    teams,
		Sources:  sourcesFromPPV(s),
		Viewers:  asInt(s.Viewers),
		EndsAt:   endsMs,
		AlwaysOn: truthy(s.AlwaysLive) || truthy(cat.AlwaysLive),
	}
}

func parseTitleTeams(title string) (home, away string) {
	title = strings.TrimSpace(title)
	if title == "" {
		return "", ""
	}
	for _, sep := range []string{" vs. ", " vs ", " VS ", " v ", " @ ", " at "} {
		if i := strings.Index(title, sep); i > 0 {
			left := strings.TrimSpace(title[:i])
			right := strings.TrimSpace(title[i+len(sep):])
			if left != "" && right != "" {
				// "Away @ Home" / "Away at Home" → home=right; "Home vs Away" → home=left.
				if sep == " @ " || sep == " at " {
					return right, left
				}
				return left, right
			}
		}
	}
	return "", ""
}

func (c *Client) refreshMatches(ctx context.Context) ([]Match, error) {
	c.mu.Lock()
	if time.Since(c.matchesAt) < 2*time.Minute && len(c.matches) > 0 {
		out := append([]Match(nil), c.matches...)
		c.mu.Unlock()
		return out, nil
	}
	c.mu.Unlock()

	var payload ppvStreamsResponse
	if err := c.getJSON(ctx, "/api/streams", &payload); err != nil {
		return nil, err
	}
	if !payload.Success {
		msg := strings.TrimSpace(payload.Error)
		if msg == "" {
			msg = "ppv streams unsuccessful"
		}
		return nil, fmt.Errorf("streamed: %s", msg)
	}
	if len(payload.Streams) == 0 {
		return nil, fmt.Errorf("streamed: empty catalog")
	}

	now := time.Now()
	byID := map[string]Match{}
	live := map[string]struct{}{}
	for _, cat := range payload.Streams {
		for _, s := range cat.Streams {
			m := matchFromPPV(cat, s)
			if m.ID == "" || len(m.Sources) == 0 {
				continue
			}
			byID[m.ID] = m
			if isPPVLive(m, now) {
				live[m.ID] = struct{}{}
			}
		}
	}
	if len(byID) == 0 {
		return nil, fmt.Errorf("streamed: empty catalog")
	}
	out := make([]Match, 0, len(byID))
	for _, m := range byID {
		out = append(out, m)
	}
	c.mu.Lock()
	c.matches = out
	c.matchesAt = time.Now()
	c.liveIDs = live
	c.mu.Unlock()
	return out, nil
}

func isPPVLive(m Match, now time.Time) bool {
	if m.AlwaysOn {
		return true
	}
	if m.Date <= 0 {
		return false
	}
	start := time.UnixMilli(m.Date)
	if now.Before(start.Add(-15 * time.Minute)) {
		return false
	}
	if m.EndsAt > 0 {
		return now.Before(time.UnixMilli(m.EndsAt).Add(30 * time.Minute))
	}
	return now.Before(start.Add(6 * time.Hour))
}

// ChannelsForMatchup returns embed-backed channels for a Stevie sports event.
// Empty when ppv.st has no matching match.
func (c *Client) ChannelsForMatchup(ctx context.Context, sport, home, away, title string, startsAt time.Time) ([]Channel, error) {
	if c == nil {
		return nil, nil
	}
	matches, err := c.refreshMatches(ctx)
	if err != nil {
		return nil, err
	}
	m := findMatch(matches, sport, home, away, title, startsAt)
	if m == nil || len(m.Sources) == 0 {
		return nil, nil
	}
	return ChannelsForMatch(*m), nil
}
