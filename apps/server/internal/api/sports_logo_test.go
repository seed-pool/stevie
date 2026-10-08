package api

import "testing"

func TestInferLeagueFromTitle(t *testing.T) {
	if got := inferLeagueFromTitle("Optibet hokeja līga: Rīgas HS/Dinaburga vs HC Energija", "Ice Hockey"); got != "Optibet hokeja līga" {
		t.Fatalf("optibet prefix: got %q", got)
	}
	if got := inferLeagueFromTitle("NHL: Maple Leafs vs Canadiens", "Ice Hockey"); got != "NHL" {
		t.Fatalf("nhl token: got %q", got)
	}
	if got := inferLeagueFromTitle("Rīgas HS/Dinaburga vs HC Energija", "Ice Hockey"); got != "" {
		t.Fatalf("bare matchup should not invent a league, got %q", got)
	}
	if got := inferLeagueFromTitle("Lakers vs Celtics", "Basketball"); got != "" {
		t.Fatalf("NBA teams without league label must not invent NBA, got %q", got)
	}
	if got := inferLeagueFromTitle("Formula 1: Bahrain Grand Prix", "MotorSport"); got != "Formula 1" {
		t.Fatalf("f1: got %q", got)
	}
	if got := inferLeagueFromTitle("UFC 300: Pereira vs Hill", "Fighting"); got != "UFC" {
		t.Fatalf("ufc: got %q", got)
	}
	if got := inferLeagueFromTitle("PGA Tour: The Masters", "Golf"); got != "PGA Tour" {
		t.Fatalf("pga: got %q", got)
	}
}
