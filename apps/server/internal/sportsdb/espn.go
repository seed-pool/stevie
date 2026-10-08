package sportsdb

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const espnSiteAPI = "https://site.api.espn.com/apis/site/v2/sports"

// ESPNLeague maps our sport name onto ESPN path segments.
type ESPNLeague struct {
	SportPath  string // baseball, basketball, hockey, football
	LeaguePath string // mlb, nba, nhl, nfl
	SportName  string // Ice Hockey, Baseball, …
}

var ESPNBig4 = []ESPNLeague{
	{SportPath: "baseball", LeaguePath: "mlb", SportName: "Baseball"},
	{SportPath: "basketball", LeaguePath: "nba", SportName: "Basketball"},
	{SportPath: "hockey", LeaguePath: "nhl", SportName: "Ice Hockey"},
	{SportPath: "football", LeaguePath: "nfl", SportName: "American Football"},
	{SportPath: "football", LeaguePath: "cfl", SportName: "American Football"},
}

type ESPNClient struct {
	HTTP *http.Client
}

func NewESPNClient() *ESPNClient {
	return &ESPNClient{HTTP: &http.Client{Timeout: 25 * time.Second}}
}

type ESPNEvent struct {
	ID        string
	Sport     string
	League    string
	Title     string
	Home      string
	Away      string
	HomeAbbr  string
	AwayAbbr  string
	HomeLogo  string
	AwayLogo  string
	HomeScore string
	AwayScore string
	Period    string
	Status    string // scheduled, in, post
	StartsAt  time.Time
	Broadcasts []string
}

type espnScoreboard struct {
	Events []espnEventJSON `json:"events"`
}

type espnEventJSON struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	ShortName    string `json:"shortName"`
	Date         string `json:"date"`
	Competitions []struct {
		Competitors []struct {
			HomeAway string `json:"homeAway"`
			Score    string `json:"score"`
			Team     struct {
				ID           string `json:"id"`
				DisplayName  string `json:"displayName"`
				Abbreviation string `json:"abbreviation"`
				Logo         string `json:"logo"`
				Logos        []struct {
					Href string `json:"href"`
				} `json:"logos"`
			} `json:"team"`
		} `json:"competitors"`
		Status struct {
			Period int `json:"period"`
			Type   struct {
				Name        string `json:"name"`
				State       string `json:"state"`
				Description string `json:"description"`
				Detail      string `json:"detail"`
				ShortDetail string `json:"shortDetail"`
				Completed   bool   `json:"completed"`
			} `json:"type"`
		} `json:"status"`
		Broadcasts []struct {
			Market string   `json:"market"`
			Names  []string `json:"names"`
		} `json:"broadcasts"`
		GeoBroadcasts []struct {
			Type struct {
				ShortName string `json:"shortName"`
			} `json:"type"`
			Media struct {
				ShortName string `json:"shortName"`
			} `json:"media"`
		} `json:"geoBroadcasts"`
	} `json:"competitions"`
}

func (c *ESPNClient) Scoreboard(ctx context.Context, league ESPNLeague, day time.Time) ([]ESPNEvent, error) {
	u := fmt.Sprintf("%s/%s/%s/scoreboard", espnSiteAPI, league.SportPath, league.LeaguePath)
	if !day.IsZero() {
		u += "?dates=" + day.UTC().Format("20060102")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "StevieSports/1.0")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("espn: HTTP %d", resp.StatusCode)
	}
	var sb espnScoreboard
	if err := json.Unmarshal(body, &sb); err != nil {
		return nil, err
	}
	out := make([]ESPNEvent, 0, len(sb.Events))
	for _, e := range sb.Events {
		ev, ok := parseESPNEvent(e, league)
		if ok {
			out = append(out, ev)
		}
	}
	return out, nil
}

func parseESPNEvent(e espnEventJSON, league ESPNLeague) (ESPNEvent, bool) {
	if e.ID == "" || len(e.Competitions) == 0 {
		return ESPNEvent{}, false
	}
	comp := e.Competitions[0]
	starts, err := time.Parse(time.RFC3339, normalizeESPNDate(e.Date))
	if err != nil {
		starts, err = time.Parse("2006-01-02T15:04Z", e.Date)
		if err != nil {
			return ESPNEvent{}, false
		}
	}
	ev := ESPNEvent{
		ID:       e.ID,
		Sport:    league.SportName,
		League:   strings.ToUpper(league.LeaguePath),
		Title:    strings.TrimSpace(e.Name),
		StartsAt: starts.UTC(),
		Status:   strings.ToLower(comp.Status.Type.State),
		Period:   firstNonEmpty(comp.Status.Type.ShortDetail, comp.Status.Type.Detail, comp.Status.Type.Description),
	}
	for _, c := range comp.Competitors {
		logo := preferESPNTeamLogo(c.Team.Logo, c.Team.Logos, league.LeaguePath, c.Team.Abbreviation)
		name := strings.TrimSpace(c.Team.DisplayName)
		switch strings.ToLower(c.HomeAway) {
		case "home":
			ev.Home = name
			ev.HomeAbbr = c.Team.Abbreviation
			ev.HomeLogo = logo
			ev.HomeScore = strings.TrimSpace(c.Score)
		case "away":
			ev.Away = name
			ev.AwayAbbr = c.Team.Abbreviation
			ev.AwayLogo = logo
			ev.AwayScore = strings.TrimSpace(c.Score)
		}
	}
	if ev.Home == "" || ev.Away == "" {
		return ESPNEvent{}, false
	}
	if ev.Title == "" {
		ev.Title = ev.Away + " at " + ev.Home
	}
	seen := map[string]struct{}{}
	for _, b := range comp.Broadcasts {
		for _, n := range b.Names {
			n = strings.TrimSpace(n)
			if n == "" {
				continue
			}
			if _, ok := seen[strings.ToLower(n)]; ok {
				continue
			}
			seen[strings.ToLower(n)] = struct{}{}
			ev.Broadcasts = append(ev.Broadcasts, n)
		}
	}
	for _, g := range comp.GeoBroadcasts {
		if !strings.EqualFold(g.Type.ShortName, "TV") {
			continue
		}
		n := strings.TrimSpace(g.Media.ShortName)
		if n == "" {
			continue
		}
		if _, ok := seen[strings.ToLower(n)]; ok {
			continue
		}
		seen[strings.ToLower(n)] = struct{}{}
		ev.Broadcasts = append(ev.Broadcasts, n)
	}
	return ev, true
}

func normalizeESPNDate(s string) string {
	s = strings.TrimSpace(s)
	// ESPN often returns 2026-10-07T01:30Z without seconds.
	if len(s) == len("2006-01-02T15:04Z") && strings.HasSuffix(s, "Z") {
		return s[:len(s)-1] + ":00Z"
	}
	return s
}

// preferESPNTeamLogo picks a full-color badge over dark scoreboard marks when possible.
func preferESPNTeamLogo(primary string, logos []struct {
	Href string `json:"href"`
}, leaguePath, abbr string) string {
	abbr = strings.ToLower(strings.TrimSpace(abbr))
	leaguePath = strings.ToLower(strings.TrimSpace(leaguePath))
	if abbr != "" && leaguePath != "" {
		return fmt.Sprintf("https://a.espncdn.com/i/teamlogos/%s/500/%s.png", leaguePath, abbr)
	}
	for _, lg := range logos {
		href := strings.TrimSpace(lg.Href)
		if href == "" {
			continue
		}
		if !strings.Contains(href, "/scoreboard/") {
			return href
		}
	}
	primary = strings.TrimSpace(primary)
	if primary != "" {
		return strings.Replace(primary, "/scoreboard/", "/", 1)
	}
	if len(logos) > 0 {
		return strings.TrimSpace(logos[0].Href)
	}
	return ""
}
