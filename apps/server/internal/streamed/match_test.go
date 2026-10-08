package streamed

import (
	"strings"
	"testing"
	"time"
)

func TestFindMatchTeamsEitherOrder(t *testing.T) {
	matches := []Match{
		{
			ID:       "1",
			Title:    "Memphis Grizzlies vs Orlando Magic",
			Category: "basketball",
			Date:     time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC).UnixMilli(),
			Teams: &MatchTeams{
				Home: &MatchTeam{Name: "Memphis Grizzlies"},
				Away: &MatchTeam{Name: "Orlando Magic"},
			},
			Sources: []MatchSource{{Source: "delta", ID: "x"}},
		},
	}
	got := findMatch(matches, "Basketball", "Memphis Grizzlies", "Orlando Magic",
		time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC))
	if got == nil {
		t.Fatal("expected match")
	}
	got = findMatch(matches, "Basketball", "Orlando Magic", "Memphis Grizzlies",
		time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC))
	if got == nil {
		t.Fatal("swapped home/away should still match")
	}
}

func TestStreamsToChannelsPrefersHD(t *testing.T) {
	ch := streamsToChannels([]Stream{
		{Language: "English", HD: false, EmbedURL: "https://embed.st/a/2", StreamNo: 2, Source: "delta"},
		{Language: "English", HD: true, EmbedURL: "https://embed.st/a/1", StreamNo: 1, Source: "delta"},
	}, 1)
	if len(ch) != 1 || !strings.Contains(ch[0].Name, "HD") {
		t.Fatalf("expected HD first, got %#v", ch)
	}
}
