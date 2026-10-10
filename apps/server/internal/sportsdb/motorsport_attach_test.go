package sportsdb

import "testing"

func TestMotorSportFeedChannel(t *testing.T) {
	for _, name := range []string{
		"UK: SKY SPORTS F1 ᴴᴰ ◉",
		"DE: SKY SPORT F1 HD (720P)",
		"SKYGO: SKY SPORT F1 HD",
		"ES: DAZN F1 ᴴᴰ",
		"VIP: SKY SPORTS F1 SD",
	} {
		if !isMotorSportFeedChannel(name) {
			t.Fatalf("expected motorsport feed: %q", name)
		}
	}
	for _, name := range []string{
		"UK: SKY SPORTS MAIN EVENT",
		"US: ESPN",
		"CA: TSN 1",
		"UK: SKY SPORTS DARTS ᴴᴰ",
	} {
		if isMotorSportFeedChannel(name) {
			t.Fatalf("expected non-motorsport: %q", name)
		}
	}
}

func TestMotorEventRelatedSingaporeSprint(t *testing.T) {
	evTitle := "Sprint Race vs F1 Singapore Grand Prix"
	home, away := "Sprint Race", "F1 Singapore Grand Prix"
	if !motorEventRelated(evTitle, home, away, "F1: Sprint - GP Singapur", "") {
		t.Fatal("Sky DE sprint should relate")
	}
	if !motorEventRelated(evTitle, home, away, "GP Singapore: Sprint", "") {
		t.Fatal("Sky IT sprint should relate")
	}
	if !motorEventRelated(evTitle, home, away, "Singapore F1 GP", "") {
		t.Fatal("UK Singapore F1 GP should relate when session unset on hit")
	}
	if motorEventRelated(evTitle, home, away, "Live F1: Qualifying - GP Singapur", "") {
		t.Fatal("qualifying must not attach to sprint card")
	}
	if motorEventRelated(evTitle, home, away, "Charlotte Motor Speedway — Free Practice", "NASCAR") {
		t.Fatal("NASCAR must not attach to F1 sprint")
	}
}
