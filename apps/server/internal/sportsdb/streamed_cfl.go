package sportsdb

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/stevie-media/stevie/apps/server/internal/store"
	"github.com/stevie-media/stevie/apps/server/internal/streamed"
)

// ESPN abandoned CFL after ~2023 (scoreboards only return the 2022 Grey Cup).
// streamed.pk still carries the live slate — import those matchups as American Football / CFL.

func (s *SyncService) SetStreamed(client *streamed.Client) {
	if s == nil {
		return
	}
	s.streamed = client
}

func isCFLTeamName(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	if n == "" {
		return false
	}
	for _, tok := range []string{
		"alouettes", "tiger-cats", "tigercats", "tiger cats",
		"roughriders", "blue bombers", "bluebombers",
		"stampeders", "argonauts", "redblacks", "red blacks", "redblack",
		"edmonton elks", " elks", "bc lions", "b.c. lions", "b.c lions",
		"hamilton tiger", "saskatchewan", "winnipeg blue", "calgary stamp",
		"montreal alou", "toronto argonaut", "ottawa red",
	} {
		if strings.Contains(n, tok) {
			return true
		}
	}
	return false
}

func isCFLMatchup(home, away, title string) bool {
	blob := strings.ToLower(home + " " + away + " " + title)
	if strings.Contains(blob, "cfl") {
		return true
	}
	return isCFLTeamName(home) && isCFLTeamName(away)
}

func (s *SyncService) findAFTeamID(ctx context.Context, name string) *uuid.UUID {
	if s.store == nil || strings.TrimSpace(name) == "" {
		return nil
	}
	t, err := s.store.FindSportsTeamByName(ctx, "American Football", name)
	if err != nil {
		// Ensure CFL clubs exist so league watermarks / badges can resolve.
		if isCFLTeamName(name) {
			ext := "cfl:" + strings.ToLower(strings.ReplaceAll(strings.TrimSpace(name), " ", "_"))
			upserted, uerr := s.store.UpsertSportsTeam(ctx, store.SportsTeam{
				ExternalID: ext,
				Sport:      "American Football",
				League:     "CFL",
				Name:       strings.TrimSpace(name),
			})
			if uerr == nil {
				id := upserted.ID
				return &id
			}
		}
		return nil
	}
	id := t.ID
	return &id
}

// fillFromStreamedCFL upserts CFL games from streamed.pk american-football.
func (s *SyncService) fillFromStreamedCFL(ctx context.Context) int {
	if s == nil || s.streamed == nil || s.store == nil {
		return 0
	}
	matches, err := s.streamed.AllMatches(ctx)
	if err != nil {
		slog.Warn("sports streamed cfl", "err", err)
		return 0
	}
	now := time.Now()
	imported := 0
	for _, m := range matches {
		if !strings.EqualFold(strings.TrimSpace(m.Category), "american-football") {
			continue
		}
		home, away := "", ""
		if m.Teams != nil {
			if m.Teams.Home != nil {
				home = strings.TrimSpace(m.Teams.Home.Name)
			}
			if m.Teams.Away != nil {
				away = strings.TrimSpace(m.Teams.Away.Name)
			}
		}
		title := strings.TrimSpace(m.Title)
		if home == "" || away == "" {
			h2, a2 := ParseEventTeams(title)
			if home == "" {
				home = h2
			}
			if away == "" {
				away = a2
			}
		}
		if home == "" || away == "" || !isCFLMatchup(home, away, title) {
			continue
		}
		if m.Date <= 0 {
			continue
		}
		starts := time.UnixMilli(m.Date).UTC()
		// Skip long-finished and far-future stubs.
		if starts.Before(now.Add(-6*time.Hour)) || starts.After(now.Add(14*24*time.Hour)) {
			continue
		}
		if title == "" {
			title = away + " vs " + home
		}
		ends := starts.Add(DefaultDuration("American Football"))
		ext := "streamed:cfl:" + strings.TrimSpace(m.ID)
		if ext == "streamed:cfl:" {
			continue
		}
		_, err := s.store.UpsertSportsEvent(ctx, store.SportsEventUpsert{
			ExternalID: ext,
			Sport:      "American Football",
			Title:      title,
			HomeTeam:   home,
			AwayTeam:   away,
			HomeTeamID: s.findAFTeamID(ctx, home),
			AwayTeamID: s.findAFTeamID(ctx, away),
			StartsAt:   starts,
			EndsAt:     &ends,
			Source:     "streamed",
		})
		if err != nil {
			slog.Warn("sports streamed cfl upsert", "title", title, "err", err)
			continue
		}
		imported++
	}
	if imported > 0 {
		slog.Info("sports streamed cfl fill", "events", imported)
	}
	return imported
}
