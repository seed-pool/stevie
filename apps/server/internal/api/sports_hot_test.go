package api

import (
	"testing"
	"time"

	"github.com/stevie-media/stevie/apps/server/internal/streamed"
)

func TestIsHotFightOrPPVMatch(t *testing.T) {
	if !isHotFightOrPPVMatch(streamed.Match{Category: "fight", Title: "UFC 300", ID: "ufc-300"}) {
		t.Fatal("fight category")
	}
	if !isHotFightOrPPVMatch(streamed.Match{Category: "fight", Title: "TNA Impact", ID: "ppv-tna-impact"}) {
		t.Fatal("ppv fight")
	}
	if isHotFightOrPPVMatch(streamed.Match{Category: "american-football", Title: "NFL Network", ID: "ppv-nfl-network"}) {
		t.Fatal("linear network stub must not count as fight PPV")
	}
	if !isHotFightOrPPVMatch(streamed.Match{Category: "other", Title: "UFC Fight Night", ID: "x"}) {
		t.Fatal("ufc title")
	}
}

func TestPickHotLiveOnly(t *testing.T) {
	now := time.Now().UTC()
	events := []sportsEventDTO{
		{
			ID: "1", Sport: "Basketball", Title: "A vs B",
			Home: sportsTeamDTO{Name: "A"}, Away: sportsTeamDTO{Name: "B"},
			StartsAt: now.Add(2 * time.Hour).Format(time.RFC3339),
			Upcoming: true, League: "NBA",
			Channels: []sportsChannelDTO{{Playable: true}, {Playable: true}},
		},
		{
			ID: "2", Sport: "Fighting", Title: "UFC 301: Jones vs Aspinall",
			Home: sportsTeamDTO{Name: "Jones"}, Away: sportsTeamDTO{Name: "Aspinall"},
			StartsAt: now.Add(-time.Hour).Format(time.RFC3339),
			Live: true, League: "UFC",
			Channels: []sportsChannelDTO{{Playable: true, Source: "streamed"}},
		},
	}
	hot := mergeHotCandidates(events, nil, nil, nil, "", now)
	if len(hot) != 1 {
		t.Fatalf("expected only live Hot cards, got %d", len(hot))
	}
	if hot[0].Sport != "Fighting" {
		t.Fatalf("expected Fighting, got %s (%s)", hot[0].Sport, hot[0].Title)
	}
}

func TestHotReservesFightSlots(t *testing.T) {
	now := time.Now().UTC()
	var events []sportsEventDTO
	for i := 0; i < 8; i++ {
		events = append(events, sportsEventDTO{
			ID: string(rune('a' + i)), Sport: "Basketball", Title: "Live Tip",
			Home: sportsTeamDTO{Name: "Home" + string(rune('A'+i))},
			Away: sportsTeamDTO{Name: "Away" + string(rune('A'+i))},
			StartsAt: now.Add(-time.Hour).Format(time.RFC3339),
			Live: true, League: "NBA",
			Channels: []sportsChannelDTO{{Playable: true}, {Playable: true}, {Source: "streamed", Playable: true}},
		})
	}
	ufc := sportsEventDTO{
		ID: "ufc", Sport: "Fighting", Title: "UFC Fight Night",
		Home: sportsTeamDTO{Name: "Fighter A"}, Away: sportsTeamDTO{Name: "Fighter B"},
		StartsAt: now.Add(-30 * time.Minute).Format(time.RFC3339),
		Live: true, League: "UFC",
		Channels: []sportsChannelDTO{{Source: "streamed", Playable: true}},
	}
	hot := mergeHotCandidates(events, []sportsEventDTO{ufc}, nil, nil, "", now)
	found := false
	for _, h := range hot {
		if !h.Live {
			t.Fatalf("Hot must be live-only, got upcoming/finished %s", h.Title)
		}
		if h.Sport == "Fighting" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected reserved UFC/fight slot in Hot despite live Big-4 flood")
	}
}
