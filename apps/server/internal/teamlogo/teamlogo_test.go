package teamlogo

import (
	"path/filepath"
	"testing"
)

func TestOpenBundledPack(t *testing.T) {
	dir := filepath.Join("..", "..", "assets", "team-logos")
	cat, err := Open(dir)
	if err != nil {
		t.Skip("logo pack not present:", err)
	}
	if len(cat.Teams()) < 100 {
		t.Fatalf("expected 100+ teams, got %d", len(cat.Teams()))
	}
	key := cat.LookupKey("Baseball", "Chicago White Sox")
	if key != "mlb/chw" {
		t.Fatalf("white sox key=%q", key)
	}
	path, err := cat.ResolveFile(key)
	if err != nil {
		t.Fatal(err)
	}
	if path == "" {
		t.Fatal("empty path")
	}
	if PublicURL(key) != "/api/sports/logo?key=mlb%2Fchw" {
		t.Fatalf("public url %q", PublicURL(key))
	}
	lg := cat.LookupLeague("NHL", "Ice Hockey")
	if lg != "leagues/nhl" {
		t.Fatalf("nhl league key=%q", lg)
	}
	if _, err := cat.ResolveFile(lg); err != nil {
		t.Fatal("nhl league file:", err)
	}
	// Unknown / empty league must NOT fall back to NHL/NBA/etc. by sport.
	if got := cat.LookupLeague("", "Ice Hockey"); got != "" {
		t.Fatalf("empty league must not default to NHL, got %q", got)
	}
	if got := cat.LookupLeague("Optibet hokeja līga", "Ice Hockey"); got != "" {
		t.Fatalf("unknown league must not inherit NHL mark, got %q", got)
	}
	if got := cat.LeaguePublicURL("", "Basketball"); got != "" {
		t.Fatalf("empty basketball league must not default to NBA url, got %q", got)
	}
	if got := cat.LookupLeague("Formula 1", "MotorSport"); got != "leagues/f1" {
		t.Fatalf("f1 league key=%q", got)
	}
	if got := cat.LookupLeague("UFC", "Fighting"); got != "leagues/ufc" {
		t.Fatalf("ufc league key=%q", got)
	}
	if got := cat.LookupLeague("PGA Tour", "Golf"); got != "leagues/pgatour" {
		t.Fatalf("pga league key=%q", got)
	}
	if got := cat.LookupLeague("CFL", "American Football"); got != "leagues/cfl" {
		t.Fatalf("cfl league key=%q", got)
	}
	if got := cat.LookupKey("American Football", "Hamilton Tiger-Cats"); got != "cfl/htc" {
		t.Fatalf("tiger-cats key=%q", got)
	}
	if got := cat.LookupKey("American Football", "Ottawa RedBlacks"); got != "cfl/orb" {
		t.Fatalf("redblacks key=%q", got)
	}
}
