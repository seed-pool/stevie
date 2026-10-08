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

const defaultBase = "https://streamed.pk"

// Client talks to the public streamed.pk REST API (no auth).
// Docs: https://streamed.pk/docs
type Client struct {
	base string
	http *http.Client

	mu        sync.Mutex
	matches   []Match
	matchesAt time.Time
	streams   map[string]cacheStreams
}

type cacheStreams struct {
	at    time.Time
	items []Stream
}

func NewClient(baseURL string) *Client {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		base = defaultBase
	}
	return &Client{
		base:    base,
		http:    &http.Client{Timeout: 20 * time.Second},
		streams: make(map[string]cacheStreams),
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
}

type MatchTeams struct {
	Home *MatchTeam `json:"home,omitempty"`
	Away *MatchTeam `json:"away,omitempty"`
}

type MatchTeam struct {
	Name  string `json:"name"`
	Badge string `json:"badge"`
}

type MatchSource struct {
	Source string `json:"source"`
	ID     string `json:"id"`
}

type Stream struct {
	ID       string `json:"id"`
	StreamNo int    `json:"streamNo"`
	Language string `json:"language"`
	HD       bool   `json:"hd"`
	EmbedURL string `json:"embedUrl"`
	Source   string `json:"source"`
}

// Channel is a synthetic sports-menu row backed by streamed.pk / embed.st.
type Channel struct {
	Name     string
	EmbedURL string
	Language string
	HD       bool
	Source   string // e.g. delta
	SourceID string // match id for /api/stream/{source}/{id}
	StreamNo int
}

func (c *Client) getJSON(ctx context.Context, path string, dest any) error {
	u := strings.TrimRight(c.base, "/") + path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "StevieSports/1.0")
	req.Header.Set("Accept", "application/json")
	res, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if err != nil {
		return err
	}
	if res.StatusCode >= 400 {
		return fmt.Errorf("streamed: HTTP %d", res.StatusCode)
	}
	return json.Unmarshal(body, dest)
}

func (c *Client) refreshMatches(ctx context.Context) ([]Match, error) {
	c.mu.Lock()
	if time.Since(c.matchesAt) < 2*time.Minute && len(c.matches) > 0 {
		out := append([]Match(nil), c.matches...)
		c.mu.Unlock()
		return out, nil
	}
	c.mu.Unlock()

	byID := map[string]Match{}
	add := func(items []Match) {
		for _, m := range items {
			if m.ID == "" {
				continue
			}
			byID[m.ID] = m
		}
	}

	var live, all []Match
	errLive := c.getJSON(ctx, "/api/matches/live", &live)
	errAll := c.getJSON(ctx, "/api/matches/all", &all)
	if errAll != nil {
		// Older mirrors used all-today only.
		errAll = c.getJSON(ctx, "/api/matches/all-today", &all)
	}
	if errLive == nil {
		add(live)
	}
	if errAll == nil {
		add(all)
	}

	// Union every per-sport list so we pick up anything missing from /all.
	var sports []SportInfo
	if err := c.getJSON(ctx, "/api/sports", &sports); err == nil {
		for _, sp := range sports {
			id := strings.TrimSpace(sp.ID)
			if id == "" {
				continue
			}
			var items []Match
			if err := c.getJSON(ctx, "/api/matches/"+url.PathEscape(id), &items); err != nil {
				continue
			}
			add(items)
		}
	}

	if len(byID) == 0 {
		if errLive != nil {
			return nil, errLive
		}
		if errAll != nil {
			return nil, errAll
		}
		return nil, fmt.Errorf("streamed: empty catalog")
	}

	out := make([]Match, 0, len(byID))
	for _, m := range byID {
		out = append(out, m)
	}
	c.mu.Lock()
	c.matches = out
	c.matchesAt = time.Now()
	c.mu.Unlock()
	return out, nil
}

func (c *Client) streamsFor(ctx context.Context, source, id string) ([]Stream, error) {
	source = strings.TrimSpace(source)
	id = strings.TrimSpace(id)
	if source == "" || id == "" {
		return nil, nil
	}
	key := source + "/" + id
	c.mu.Lock()
	if cached, ok := c.streams[key]; ok && time.Since(cached.at) < 3*time.Minute {
		out := append([]Stream(nil), cached.items...)
		c.mu.Unlock()
		return out, nil
	}
	c.mu.Unlock()

	path := "/api/stream/" + url.PathEscape(source) + "/" + url.PathEscape(id)
	var items []Stream
	if err := c.getJSON(ctx, path, &items); err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.streams[key] = cacheStreams{at: time.Now(), items: items}
	c.mu.Unlock()
	return items, nil
}

// ChannelsForMatchup returns embed-backed channels for a Stevie sports event.
// Empty when streamed.pk has no matching match or streams.
func (c *Client) ChannelsForMatchup(ctx context.Context, sport, home, away string, startsAt time.Time) ([]Channel, error) {
	if c == nil {
		return nil, nil
	}
	matches, err := c.refreshMatches(ctx)
	if err != nil {
		return nil, err
	}
	m := findMatch(matches, sport, home, away, startsAt)
	if m == nil || len(m.Sources) == 0 {
		return nil, nil
	}
	sources := orderSources(m.Sources)
	var streams []Stream
	for _, src := range sources {
		items, err := c.streamsFor(ctx, src.Source, src.ID)
		if err != nil || len(items) == 0 {
			continue
		}
		streams = items
		break
	}
	if len(streams) == 0 {
		return nil, nil
	}
	return streamsToChannels(streams, 3), nil
}
