package streamed

import (
	"testing"
	"time"
)

func TestFindMatchWNBABasketball(t *testing.T) {
	matches := []Match{
		{
			ID:       "golden-state-valkyries-vs-atlanta-dream-2617450",
			Title:    "Golden State Valkyries vs Atlanta Dream",
			Category: "basketball",
			Date:     time.Date(2026, 10, 17, 19, 30, 0, 0, time.UTC).UnixMilli(),
			Teams: &MatchTeams{
				Home: &MatchTeam{Name: "Golden State Valkyries"},
				Away: &MatchTeam{Name: "Atlanta Dream"},
			},
			Sources: []MatchSource{{Source: "admin", ID: "ppv-atlanta-dream-vs-golden-state-valkyries"}},
		},
	}
	got := findMatch(
		matches,
		"Basketball",
		"Golden State Valkyries",
		"Atlanta Dream",
		"Atlanta Dream @ Golden State Valkyries",
		time.Date(2026, 10, 17, 19, 30, 0, 0, time.UTC),
	)
	if got == nil {
		t.Fatal("expected ppv.st WNBA match for Basketball event")
	}
	if got.ID != matches[0].ID {
		t.Fatalf("wrong match id %q", got.ID)
	}
	// Swapped home/away (ESPN vs streamed ordering) still matches.
	got = findMatch(
		matches,
		"Basketball",
		"Atlanta Dream",
		"Golden State Valkyries",
		"Golden State Valkyries @ Atlanta Dream",
		time.Date(2026, 10, 17, 19, 30, 0, 0, time.UTC),
	)
	if got == nil {
		t.Fatal("swapped home/away should still match WNBA")
	}
}
