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
	got := findMatch(matches, "Basketball", "Memphis Grizzlies", "Orlando Magic", "",
		time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC))
	if got == nil {
		t.Fatal("expected match")
	}
	got = findMatch(matches, "Basketball", "Orlando Magic", "Memphis Grizzlies", "",
		time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC))
	if got == nil {
		t.Fatal("swapped home/away should still match")
	}
}

func TestFindMatchMotorSportByTitle(t *testing.T) {
	matches := []Match{
		{
			ID:       "sprint",
			Title:    "Singapore Grand Prix Sprint",
			Category: "motor-sports",
			Date:     time.Date(2026, 10, 10, 13, 0, 0, 0, time.UTC).UnixMilli(),
			Sources:  []MatchSource{{Source: "delta", ID: "1"}},
		},
		{
			ID:       "quali",
			Title:    "Singapore Grand Prix Qualifying",
			Category: "motor-sports",
			Date:     time.Date(2026, 10, 10, 14, 0, 0, 0, time.UTC).UnixMilli(),
			Sources:  []MatchSource{{Source: "delta", ID: "2"}},
		},
	}
	got := findMatch(matches, "MotorSport", "Sprint Race", "F1 Singapore Grand Prix",
		"Sprint Race vs F1 Singapore Grand Prix",
		time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC))
	if got == nil || got.ID != "sprint" {
		t.Fatalf("expected Singapore sprint, got %#v", got)
	}
	// Qualifying must not steal the sprint card.
	got = findMatch(matches, "MotorSport", "Sprint Race", "F1 Singapore Grand Prix",
		"Sprint Race vs F1 Singapore Grand Prix",
		time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC))
	if got != nil && got.ID == "quali" {
		t.Fatal("qualifying must not match sprint event")
	}
	// Fake home/away tokens must not match unrelated "…Race…Prix…" titles.
	matches = append(matches, Match{
		ID:       "moto3",
		Title:    "Mandalika Street Circuit — Race: Grand Prix of Indonesia",
		Category: "motor-sports",
		Date:     time.Date(2026, 10, 10, 21, 0, 0, 0, time.UTC).UnixMilli(),
		Sources:  []MatchSource{{Source: "hotel", ID: "x"}},
	})
	got = findMatch(matches, "MotorSport", "Sprint Race", "F1 Singapore Grand Prix",
		"Sprint Race vs F1 Singapore Grand Prix",
		time.Date(2026, 10, 10, 21, 30, 0, 0, time.UTC))
	if got != nil && got.ID == "moto3" {
		t.Fatal("must not match unrelated Moto3 via race/prix tokens")
	}
}

func TestParseEmbedSlotPPV(t *testing.T) {
	src, id, no := parseEmbedSlot("https://taifood-blog.asia/embed/mlb/2026-10-27/mil-tb", "", 0)
	if src != "taifood-blog.asia" || id != "mlb/2026-10-27/mil-tb" || no != 1 {
		t.Fatalf("ppv slot: got %q %q %d", src, id, no)
	}
	src, id, no = parseEmbedSlot("https://embed.st/embed/golf/2172/1", "golf", 1)
	if src != "golf" || id != "2172" || no != 1 {
		t.Fatalf("legacy slot: got %q %q %d", src, id, no)
	}
}

func TestStreamsToChannelsPrefersHD(t *testing.T) {
	ch := streamsToChannels([]Stream{
		{Language: "English", HD: false, EmbedURL: "https://embed.st/delta/x/2", StreamNo: 2, Source: "delta"},
		{Language: "English", HD: true, EmbedURL: "https://embed.st/delta/x/1", StreamNo: 1, Source: "delta"},
	}, 1)
	if len(ch) != 1 || !strings.Contains(ch[0].Name, "HD") {
		t.Fatalf("expected HD first, got %#v", ch)
	}
}

func TestStreamsToChannelsMultiSource(t *testing.T) {
	ch := streamsToChannels([]Stream{
		{Language: "Main", HD: true, EmbedURL: "https://embed.st/embed/hotel/dead/1", StreamNo: 1, Source: "hotel"},
		{Language: "English", HD: true, EmbedURL: "https://embed.st/embed/golf/2172/1", StreamNo: 1, Source: "golf"},
		{Language: "English", HD: false, EmbedURL: "https://embed.st/embed/golf/2172/2", StreamNo: 2, Source: "golf"},
		{Language: "English - TBS", HD: true, EmbedURL: "https://embed.st/embed/admin/ppv/1", StreamNo: 1, Source: "admin"},
		{Language: "English - TBS", HD: false, EmbedURL: "https://embed.st/embed/admin/ppv/2", StreamNo: 2, Source: "admin"},
	}, 8)
	if len(ch) < 4 {
		t.Fatalf("expected streams from all sources, got %d %#v", len(ch), ch)
	}
	sources := map[string]bool{}
	for _, c := range ch {
		sources[c.Source] = true
		if !strings.Contains(c.Name, c.Source) {
			t.Fatalf("multi-source rows should name the source: %#v", c)
		}
	}
	for _, want := range []string{"golf", "hotel", "admin"} {
		if !sources[want] {
			t.Fatalf("missing source %s in %#v", want, ch)
		}
	}
	// Round-robin: first picks should diversify (golf before second hotel).
	if ch[0].Source != "golf" {
		t.Fatalf("expected golf first (ranked ahead of hotel), got %#v", ch[0])
	}
}
