package sportsdb

import "testing"

func TestIsCFLMatchup(t *testing.T) {
	if !isCFLMatchup("Hamilton Tiger-Cats", "Edmonton Elks", "Hamilton Tiger-Cats vs Edmonton Elks") {
		t.Fatal("expected CFL matchup")
	}
	if !isCFLMatchup("Ottawa RedBlacks", "BC Lions", "Ottawa RedBlacks at BC Lions") {
		t.Fatal("expected RedBlacks/Lions CFL")
	}
	if isCFLMatchup("Dallas Cowboys", "Tampa Bay Buccaneers", "Dallas Cowboys vs Tampa Bay Buccaneers") {
		t.Fatal("NFL must not count as CFL")
	}
	if isCFLMatchup("Hamilton Tiger-Cats", "Dallas Cowboys", "cross") {
		t.Fatal("mixed CFL/NFL must not count")
	}
}
