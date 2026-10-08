package sportsdb

import "testing"

func TestParseEPGNationsLeague(t *testing.T) {
	home, away := ParseEventTeams("Estonia vs Iceland - UEFA Nations League 2026/27 - Match Day 4")
	if home != "Estonia" || away != "Iceland" {
		t.Fatalf("got home=%q away=%q", home, away)
	}
	if g := DetectSportFromTitle("Estonia vs Iceland - UEFA Nations League", ""); g != "Football" {
		t.Fatalf("sport=%q", g)
	}
	h2, a2 := ParseTeamsFromDescription("Estonia vs Iceland\r\nAt 6-10-2026, MECCA 21:45\r\nUEFA Nations League")
	if h2 != "Estonia" || a2 != "Iceland" {
		t.Fatalf("desc parse got home=%q away=%q", h2, a2)
	}
}

func TestParseEPGNationalTeamsDash(t *testing.T) {
	home, away := ParseEventTeams(`SPAIN - CZECHIA 3\/10\/26`)
	if home != "SPAIN" || away != "CZECHIA" {
		t.Fatalf("got home=%q away=%q", home, away)
	}
}

func TestParseEPGClubDash(t *testing.T) {
	home, away := ParseEventTeams("Bayern Mnichov - Union Berlín")
	if home != "Bayern Mnichov" || away != "Union Berlín" {
		t.Fatalf("got home=%q away=%q", home, away)
	}
}

func TestReplayEPGFiltered(t *testing.T) {
	if !isReplayOrFillerEPG("Bayern Mnichov - Union Berlín", "Záznam 4. kola německé fotbalové Bundesligy.") {
		t.Fatal("expected replay filter")
	}
	if isReplayOrFillerEPG("Estonia vs Iceland - UEFA Nations League", "UEFA Nations League") {
		t.Fatal("live nations league should not be filtered")
	}
}

func TestGenericLiveBlocks(t *testing.T) {
	if !IsGenericLeagueTitle("Ishockey: NHL", "Ice Hockey") {
		t.Fatal("expected NHL block")
	}
	if !IsGenericLeagueTitle("Fotboll: Engelska Championship", "Football") {
		t.Fatal("expected Championship block")
	}
	if IsGenericLeagueTitle("Estonia vs Iceland - UEFA Nations League 2026/27", "Football") {
		t.Fatal("named Nations League matchup must not be treated as generic")
	}
	if cleanGenericTitle("Ishockey: NHL", "") != "NHL Hockey" {
		t.Fatalf("title=%q", cleanGenericTitle("Ishockey: NHL", ""))
	}
}
