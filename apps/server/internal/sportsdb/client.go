package sportsdb

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const defaultBase = "https://www.thesportsdb.com/api/v1/json"

// Client talks to TheSportsDB v1 (and optionally v2 livescores).
type Client struct {
	APIKey     string
	HTTPClient *http.Client
	BaseURL    string
	V2BaseURL  string
}

func NewClient(apiKey string) *Client {
	key := strings.TrimSpace(apiKey)
	if key == "" {
		key = "123"
	}
	return &Client{
		APIKey: key,
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		BaseURL:   defaultBase,
		V2BaseURL: "https://www.thesportsdb.com/api/v2/json",
	}
}

type TVEvent struct {
	IDEvent     string `json:"idEvent"`
	StrEvent    string `json:"strEvent"`
	StrSport    string `json:"strSport"`
	StrLeague   string `json:"strLeague"`
	StrChannel  string `json:"strChannel"`
	StrCountry  string `json:"strCountry"`
	StrTimeStamp string `json:"strTimeStamp"`
	DateEvent   string `json:"dateEvent"`
	StrTime     string `json:"strTime"`
	IDHomeTeam  string `json:"idHomeTeam"`
	IDAwayTeam  string `json:"idAwayTeam"`
	StrHomeTeam string `json:"strHomeTeam"`
	StrAwayTeam string `json:"strAwayTeam"`
	StrThumb    string `json:"strThumb"`
	StrPoster   string `json:"strPoster"`
}

type Team struct {
	IDTeam         string `json:"idTeam"`
	StrTeam        string `json:"strTeam"`
	StrTeamAlternate string `json:"strTeamAlternate"`
	StrSport       string `json:"strSport"`
	StrLeague      string `json:"strLeague"`
	StrBadge       string `json:"strBadge"`
	StrTeamBadge   string `json:"strTeamBadge"` // older field name
}

func (t Team) Badge() string {
	if b := strings.TrimSpace(t.StrBadge); b != "" {
		return b
	}
	return strings.TrimSpace(t.StrTeamBadge)
}

type Livescore struct {
	IDEvent     string `json:"idEvent"`
	StrEvent    string `json:"strEvent"`
	IntHomeScore string `json:"intHomeScore"`
	IntAwayScore string `json:"intAwayScore"`
	StrProgress string `json:"strProgress"`
	StrStatus   string `json:"strStatus"`
	StrSport    string `json:"strSport"`
}

func (c *Client) getJSON(ctx context.Context, rawURL string, headers map[string]string, dest any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("thesportsdb: premium required (%d)", resp.StatusCode)
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("thesportsdb: HTTP %d: %s", resp.StatusCode, truncate(string(body), 200))
	}
	if len(strings.TrimSpace(string(body))) == 0 || string(body) == "null" {
		return nil
	}
	return json.Unmarshal(body, dest)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func (c *Client) v1(path string, q url.Values) string {
	base := strings.TrimRight(c.BaseURL, "/")
	u := fmt.Sprintf("%s/%s/%s", base, url.PathEscape(c.APIKey), strings.TrimLeft(path, "/"))
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	return u
}

// EventsTV fetches TV schedule rows for a day/sport/country.
func (c *Client) EventsTV(ctx context.Context, day time.Time, sport, country string) ([]TVEvent, error) {
	q := url.Values{}
	q.Set("d", day.UTC().Format("2006-01-02"))
	if sport != "" {
		q.Set("s", sport)
	}
	if country != "" {
		q.Set("a", country)
	}
	var wrap struct {
		TV []TVEvent `json:"tvevents"`
	}
	if err := c.getJSON(ctx, c.v1("eventstv.php", q), nil, &wrap); err != nil {
		return nil, err
	}
	return wrap.TV, nil
}

func (c *Client) SearchTeams(ctx context.Context, name string) ([]Team, error) {
	q := url.Values{}
	q.Set("t", name)
	var wrap struct {
		Teams []Team `json:"teams"`
	}
	if err := c.getJSON(ctx, c.v1("searchteams.php", q), nil, &wrap); err != nil {
		return nil, err
	}
	return wrap.Teams, nil
}

func (c *Client) LookupTeam(ctx context.Context, id string) (*Team, error) {
	q := url.Values{}
	q.Set("id", id)
	var wrap struct {
		Teams []Team `json:"teams"`
	}
	if err := c.getJSON(ctx, c.v1("lookupteam.php", q), nil, &wrap); err != nil {
		return nil, err
	}
	if len(wrap.Teams) == 0 {
		return nil, nil
	}
	t := wrap.Teams[0]
	return &t, nil
}

func (c *Client) SearchAllTeams(ctx context.Context, league string) ([]Team, error) {
	q := url.Values{}
	q.Set("l", strings.ReplaceAll(league, " ", "_"))
	var wrap struct {
		Teams []Team `json:"teams"`
	}
	if err := c.getJSON(ctx, c.v1("search_all_teams.php", q), nil, &wrap); err != nil {
		return nil, err
	}
	return wrap.Teams, nil
}

// Livescores tries the premium v2 endpoint. Returns ErrPremium if unavailable.
func (c *Client) Livescores(ctx context.Context, sport string) ([]Livescore, error) {
	sport = strings.TrimSpace(sport)
	if sport == "" {
		sport = "all"
	}
	u := fmt.Sprintf("%s/livescore/%s", strings.TrimRight(c.V2BaseURL, "/"), url.PathEscape(sport))
	var wrap struct {
		Events []Livescore `json:"events"`
		List   []Livescore `json:"list"`
	}
	err := c.getJSON(ctx, u, map[string]string{"X-API-KEY": c.APIKey}, &wrap)
	if err != nil {
		return nil, err
	}
	if len(wrap.Events) > 0 {
		return wrap.Events, nil
	}
	return wrap.List, nil
}

func IsPremiumError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "premium required")
}
