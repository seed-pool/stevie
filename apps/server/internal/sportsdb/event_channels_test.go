package sportsdb

import (
	"testing"
	"time"
)

func TestParseCFLEventChannel(t *testing.T) {
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	name := "CA | CFL 00ⓧ: Montreal Alouettes vs Hamilton Tiger-Cats | Sat 8th Nov 3:00 PM ET"
	sport, home, away, starts, ok := parseEventChannelName(name, now)
	if !ok {
		t.Fatal("expected parse ok")
	}
	if sport != "American Football" {
		t.Fatalf("sport=%q", sport)
	}
	if home != "Montreal Alouettes" || away != "Hamilton Tiger-Cats" {
		t.Fatalf("teams home=%q away=%q", home, away)
	}
	if starts.Month() != time.November || starts.Day() != 8 {
		t.Fatalf("starts=%v", starts)
	}
}

func TestParseCFLNoEvent(t *testing.T) {
	now := time.Now()
	_, _, _, _, ok := parseEventChannelName("CA | CFL 02ⓧ: No Scheduled Event", now)
	if ok {
		t.Fatal("empty CFL slot should be ignored")
	}
	_, _, _, _, ok = parseEventChannelName("Sunday Night Football: Lions vs. Panthers @ Oct 4 20:15 :TSN+  68", now)
	if ok {
		t.Fatal("TSN+ replay slots must not become CFL/NFL cards")
	}
}

func TestDetectCFLSport(t *testing.T) {
	if g := DetectSportFromTitle("CFL: Alouettes vs Tiger-Cats", ""); g != "American Football" {
		t.Fatalf("sport=%q", g)
	}
	if g := DetectSportFromTitle("UEFA Champions League", ""); g != "Football" {
		t.Fatalf("sport=%q", g)
	}
}
