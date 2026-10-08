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

const mlbStatsAPI = "https://statsapi.mlb.com/api/v1"

type MLBClient struct {
	HTTP *http.Client
}

func NewMLBClient() *MLBClient {
	return &MLBClient{HTTP: &http.Client{Timeout: 25 * time.Second}}
}

type MLBGame struct {
	GamePk     int
	StartsAt   time.Time
	Home       string
	Away       string
	HomeScore  string
	AwayScore  string
	Period     string
	Status     string // Live, Final, Preview, …
	Broadcasts []string
}

func (c *MLBClient) Schedule(ctx context.Context, day time.Time) ([]MLBGame, error) {
	q := url.Values{}
	q.Set("sportId", "1")
	q.Set("date", day.UTC().Format("2006-01-02"))
	q.Set("hydrate", "team,linescore,broadcasts")
	u := mlbStatsAPI + "/schedule?" + q.Encode()
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
		return nil, fmt.Errorf("mlb statsapi: HTTP %d", resp.StatusCode)
	}
	var payload struct {
		Dates []struct {
			Games []struct {
				GamePk   int    `json:"gamePk"`
				GameDate string `json:"gameDate"`
				Status   struct {
					AbstractGameState string `json:"abstractGameState"`
					DetailedState     string `json:"detailedState"`
				} `json:"status"`
				Teams struct {
					Away struct {
						Score *int `json:"score"`
						Team  struct {
							Name string `json:"name"`
						} `json:"team"`
					} `json:"away"`
					Home struct {
						Score *int `json:"score"`
						Team  struct {
							Name string `json:"name"`
						} `json:"team"`
					} `json:"home"`
				} `json:"teams"`
				Linescore struct {
					CurrentInning      int    `json:"currentInning"`
					CurrentInningOrdinal string `json:"currentInningOrdinal"`
					InningState        string `json:"inningState"`
					Teams              struct {
						Home struct {
							Runs *int `json:"runs"`
						} `json:"home"`
						Away struct {
							Runs *int `json:"runs"`
						} `json:"away"`
					} `json:"teams"`
				} `json:"linescore"`
				Broadcasts []struct {
					Name string `json:"name"`
					Type string `json:"type"`
				} `json:"broadcasts"`
			} `json:"games"`
		} `json:"dates"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	var out []MLBGame
	for _, day := range payload.Dates {
		for _, g := range day.Games {
			starts, err := time.Parse(time.RFC3339, g.GameDate)
			if err != nil {
				continue
			}
			mg := MLBGame{
				GamePk:   g.GamePk,
				StartsAt: starts.UTC(),
				Home:     strings.TrimSpace(g.Teams.Home.Team.Name),
				Away:     strings.TrimSpace(g.Teams.Away.Team.Name),
				Status:   firstNonEmpty(g.Status.DetailedState, g.Status.AbstractGameState),
			}
			if g.Linescore.Teams.Home.Runs != nil {
				mg.HomeScore = fmt.Sprintf("%d", *g.Linescore.Teams.Home.Runs)
			} else if g.Teams.Home.Score != nil {
				mg.HomeScore = fmt.Sprintf("%d", *g.Teams.Home.Score)
			}
			if g.Linescore.Teams.Away.Runs != nil {
				mg.AwayScore = fmt.Sprintf("%d", *g.Linescore.Teams.Away.Runs)
			} else if g.Teams.Away.Score != nil {
				mg.AwayScore = fmt.Sprintf("%d", *g.Teams.Away.Score)
			}
			if g.Linescore.CurrentInning > 0 {
				mg.Period = strings.TrimSpace(g.Linescore.InningState + " " + g.Linescore.CurrentInningOrdinal)
			}
			seen := map[string]struct{}{}
			for _, b := range g.Broadcasts {
				if !strings.EqualFold(b.Type, "TV") {
					continue
				}
				for _, label := range expandBroadcastLabels(b.Name) {
					k := strings.ToLower(label)
					if _, ok := seen[k]; ok {
						continue
					}
					seen[k] = struct{}{}
					mg.Broadcasts = append(mg.Broadcasts, label)
				}
			}
			if mg.Home != "" && mg.Away != "" {
				out = append(out, mg)
			}
		}
	}
	return out, nil
}

// expandBroadcastLabels splits "FS1 / FOX ONE" into matchable aliases.
func expandBroadcastLabels(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == '/' || r == ',' || r == '|'
	})
	out := []string{raw}
	seen := map[string]struct{}{strings.ToLower(raw): {}}
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		k := strings.ToLower(s)
		if _, ok := seen[k]; ok {
			return
		}
		seen[k] = struct{}{}
		out = append(out, s)
	}
	for _, p := range parts {
		add(p)
	}
	lower := strings.ToLower(raw)
	switch {
	case strings.Contains(lower, "fs1") || strings.Contains(lower, "fox one"):
		add("FS1")
		add("FOX SPORTS 1")
		add("FOX SPORTS1")
		add("FOX ONE")
	case strings.Contains(lower, "fs2"):
		add("FS2")
		add("FOX SPORTS 2")
	case strings.Contains(lower, "fox deportes"):
		add("FOX DEPORTES")
	case strings.Contains(lower, "espn+") || strings.Contains(lower, "espnplus") || strings.Contains(lower, "espn plus"):
		add("ESPN+")
	case strings.Contains(lower, "espn2"):
		add("ESPN2")
	case strings.Contains(lower, "espnu"):
		add("ESPNU")
	case strings.Contains(lower, "espn deportes"):
		add("ESPN Deportes")
	case strings.Contains(lower, "espn"):
		add("ESPN")
	case strings.Contains(lower, "tbs"):
		add("TBS")
	case strings.Contains(lower, "tru tv") || strings.Contains(lower, "trutv"):
		add("TruTV")
	case strings.Contains(lower, "nba tv"):
		add("NBA TV")
	case strings.Contains(lower, "nhl network"):
		add("NHL Network")
	case strings.Contains(lower, "mlb network"):
		add("MLB Network")
	}
	return out
}
