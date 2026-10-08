package sportsdb

import (
	"strings"
	"testing"

	"github.com/google/uuid"
)

func cand(name string) channelCandidate {
	pkg, brand := splitPackageBrand(name)
	return channelCandidate{
		ID:    uuid.New(),
		Name:  name,
		Key:   normalizeKey(name),
		Pkg:   pkg,
		Brand: brand,
	}
}

func TestESPNMatchesUS(t *testing.T) {
	us := cand("US: ESPN HD")
	cands := []channelCandidate{
		us,
		cand("US: ESPN 2 HD"),
		cand("####### ESPN PPV #######"),
	}
	got, score := MatchChannel("ESPN", cands, uuid.Nil, false)
	if got != us.ID {
		t.Fatalf("want US: ESPN HD, score=%v got=%v", score, got)
	}
	plus, plusScore := MatchChannel("ESPN+", cands, uuid.Nil, false)
	if plus != uuid.Nil {
		t.Fatalf("ESPN+ should not match linear ESPN, got score=%v", plusScore)
	}
}

func TestMatchAllNBATVRegions(t *testing.T) {
	cands := []channelCandidate{
		cand("CA: NBA TV"),
		cand("CA EN: NBA TV HD"),
		cand("NBA: NBA TV HD"),
		cand("TV: NBA TV"),
		cand("AF: NBA TV"),
		cand("PH: NBA TV PH"),
		cand("PRIME: NBA TV INTERNATIONAL"),
		cand("US: ESPN HD"),
		cand("####### ESPN PPV #######"),
	}
	got := MatchAllChannels("NBA TV", cands, MatchOpts{Limit: 16})
	if len(got) < 5 {
		t.Fatalf("expected many NBA TV regional feeds, got %d %#v", len(got), namesOf(got))
	}
	for _, m := range got {
		if strings.Contains(strings.ToUpper(m.Name), "ESPN") && !strings.Contains(strings.ToUpper(m.Name), "NBA") {
			t.Fatalf("unexpected ESPN in NBA TV matches: %s", m.Name)
		}
	}
}

func TestMatchAllESPNKeepsNumberedDistinct(t *testing.T) {
	cands := []channelCandidate{
		cand("US: ESPN HD"),
		cand("CA: ESPN HD"),
		cand("US: ESPN 2 HD"),
		cand("US: ESPNU HD"),
		cand("AT&T: ESPN DEPORTES"),
		cand("BR: DISNEY+ ESPN"),
	}
	got := MatchAllChannels("ESPN", cands, MatchOpts{Limit: 8})
	for _, m := range got {
		u := strings.ToUpper(m.Name)
		if strings.Contains(u, "ESPN 2") || strings.Contains(u, "ESPN2") ||
			strings.Contains(u, "ESPNU") || strings.Contains(u, "DEPORTES") ||
			strings.Contains(u, "DISNEY") {
			t.Fatalf("ESPN should not match %s", m.Name)
		}
	}
	if len(got) < 2 {
		t.Fatalf("expected US+CA ESPN, got %#v", namesOf(got))
	}
}

func TestTeamFeedsBasketball(t *testing.T) {
	cands := []channelCandidate{
		cand("NBA: LOS ANGELES LAKERS HD"),
		cand("NBA: LOS ANGELES LAKERS SD"),
		cand("NBA: GOLDEN STATE WARRIORS HD"),
		cand("US: SPORTSNET LA LAKERS HD"),
		cand("AT&T: LOS ANGELES LAKERS"),
		cand("NBA: BOSTON CELTICS HD"),
		cand("US: SPECTRUM SPORTSNET [LA DODGERS]"),
	}
	got := TeamFeedMatches("Basketball", "Golden State Warriors", "Los Angeles Lakers", cands, 12)
	if len(got) < 3 {
		t.Fatalf("expected Lakers+Warriors+Sportsnet feeds, got %d %#v", len(got), namesOf(got))
	}
	joined := strings.Join(namesOf(got), "|")
	if !strings.Contains(joined, "SPORTSNET LA LAKERS") {
		t.Fatalf("expected Sportsnet LA Lakers, got %#v", namesOf(got))
	}
	if strings.Contains(joined, "DODGERS") {
		t.Fatalf("Dodgers feed should not attach: %#v", namesOf(got))
	}
	if strings.Contains(joined, "CELTICS") {
		t.Fatalf("Celtics feed should not attach: %#v", namesOf(got))
	}
}

func TestTeamFeedsHockey(t *testing.T) {
	cands := []channelCandidate{
		cand("NHL: MONTREAL CANADIENS HD"),
		cand("NHL: TORONTO MAPLE LEAFS HD"),
		cand("CA: SPORTSNET 360"),
		cand("NHL: BOSTON BRUINS HD"),
	}
	got := TeamFeedMatches("Ice Hockey", "Toronto Maple Leafs", "Montreal Canadiens", cands, 12)
	if len(got) < 2 {
		t.Fatalf("expected Canadiens+Leafs feeds, got %d %#v", len(got), namesOf(got))
	}
	for _, m := range got {
		if strings.Contains(strings.ToUpper(m.Name), "BRUINS") {
			t.Fatalf("Bruins should not attach: %s", m.Name)
		}
	}
}

func TestTeamFeedsBaseball(t *testing.T) {
	cands := []channelCandidate{
		cand("MLB: LOS ANGELES DODGERS HD"),
		cand("MLB: SAN DIEGO PADRES HD"),
		cand("US: SPORTSNET LA DODGERS HD"),
		cand("MLB: NEW YORK YANKEES HD"),
	}
	got := TeamFeedMatches("Baseball", "San Diego Padres", "Los Angeles Dodgers", cands, 12)
	if len(got) < 2 {
		t.Fatalf("expected Dodgers+Padres feeds, got %d %#v", len(got), namesOf(got))
	}
	joined := strings.Join(namesOf(got), "|")
	if strings.Contains(joined, "YANKEES") {
		t.Fatalf("Yankees should not attach: %#v", namesOf(got))
	}
}

func TestMatchAllSpectrumSportsGeneric(t *testing.T) {
	cands := []channelCandidate{
		cand("TV: SPECTRUM SPORTSNET"),
		cand("US: SPECTRUM SPORTSNET [LA DODGERS]"),
		cand("US: SPORTSNET LA LAKERS HD"),
		cand("US: ESPN HD"),
	}
	// Without team context, Dodgers-tagged SportsNet is still Spectrum SportsNet.
	got := MatchAllChannels("Spectrum Sports", cands, MatchOpts{Limit: 8})
	if len(got) < 1 {
		t.Fatalf("expected Spectrum SportsNet match, got %#v", got)
	}
	// With Lakers context, drop Dodgers-tagged feed.
	got = MatchAllChannels("Spectrum Sports", cands, MatchOpts{
		Limit: 8, Home: "Los Angeles Lakers", Away: "Golden State Warriors",
	})
	for _, m := range got {
		if strings.Contains(strings.ToUpper(m.Name), "DODGERS") {
			t.Fatalf("Dodgers-tagged feed should be filtered for Lakers game: %s", m.Name)
		}
	}
	if len(got) < 1 {
		t.Fatalf("expected generic Spectrum SportsNet still, got %#v", namesOf(got))
	}
}

func namesOf(ms []ChannelMatch) []string {
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = m.Name
	}
	return out
}
