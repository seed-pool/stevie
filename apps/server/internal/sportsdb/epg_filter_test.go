package sportsdb

import (
	"testing"

	"github.com/stevie-media/stevie/apps/server/internal/store"
)

func TestRejectBeINForBig4(t *testing.T) {
	names := []string{
		"FR: BEIN SPORTS 3 RAW",
		"8K: beIN SP⚽RTS 2 ENGLISH HD",
		"US: BEIN SPORTS 2 HD",
		"◉: beIN Sp⚽rts 2 HEVC",
	}
	for _, n := range names {
		if !isBeINChannel(n) {
			t.Fatalf("expected BeIN detect for %q", n)
		}
		if !RejectChannelForSport("Baseball", n) {
			t.Fatalf("expected reject BeIN for Baseball: %q", n)
		}
		if !RejectChannelForSport("Ice Hockey", n) {
			t.Fatalf("expected reject BeIN for Ice Hockey: %q", n)
		}
		// Soccer still allows BeIN.
		if RejectChannelForSport("Football", n) {
			t.Fatalf("Football should keep BeIN: %q", n)
		}
	}
	if isPrioritySportsNetwork("FR: BEIN SPORTS 3", "") {
		t.Fatal("BeIN must not be a priority sports network")
	}
}

func TestPlausibleSportsMatchup(t *testing.T) {
	if !IsPlausibleSportsMatchup("Baseball", "Chicago White Sox", "Cleveland Guardians", "Cleveland Guardians @ Chicago White Sox") {
		t.Fatal("expected real MLB matchup")
	}
	if IsPlausibleSportsMatchup("American Football", "Američki fudbal", "NFL: San Francisco 49ers", "Američki fudbal vs NFL: San Francisco 49ers") {
		t.Fatal("league-label split must be rejected")
	}
	if IsPlausibleSportsMatchup("American Football", "Mayday", "Alarm im Cockpit", "Mayday vs Alarm im Cockpit") {
		t.Fatal("TV show vs title must be rejected")
	}
	if IsPlausibleSportsMatchup("Basketball", "Košarka", "NBA liga: New York", "Košarka vs NBA liga: New York") {
		t.Fatal("kosarka/nba liga junk must be rejected")
	}
	// Dual-language movie titles on cinema EPG ("English vs local").
	if IsPlausibleSportsMatchup("Basketball", "Jerry Maguire", "A nagy hátraarc", "Jerry Maguire vs A nagy hátraarc") {
		t.Fatal("Jerry Maguire dual title must be rejected")
	}
	if IsPlausibleSportsMatchup("Football", "Jerry Maguire", "A nagy hátraarc", "Jerry Maguire vs A nagy hátraarc") {
		t.Fatal("Jerry Maguire dual title must be rejected even as Football")
	}
	if !IsPlausibleSportsMatchup("Football", "Manchester United", "Real Madrid", "Manchester United vs Real Madrid") {
		t.Fatal("real club matchup must remain plausible")
	}
	// "St. Louis" / "N.Y." abbreviations must not look like prose sentence ends.
	if isProseEPGTitle("San Jose Sharks @ St. Louis Blues") {
		t.Fatal("St. Louis team title must not be treated as prose")
	}
	if isProseEPGTitle("N.Y. Rangers @ Washington Capitals") {
		t.Fatal("N.Y. team title must not be treated as prose")
	}
	if !IsPlausibleSportsMatchup("Ice Hockey", "St. Louis Blues", "San Jose Sharks", "San Jose Sharks @ St. Louis Blues") {
		t.Fatal("NHL St. Louis Blues matchup must remain plausible")
	}
	if !IsPlausibleSportsMatchup("Baseball", "St. Louis Cardinals", "Chicago Cubs", "Chicago Cubs @ St. Louis Cardinals") {
		t.Fatal("MLB St. Louis Cardinals matchup must remain plausible")
	}
	if !isProseEPGTitle("Tonight tip-off delayed. Coverage continues shortly") {
		t.Fatal("real prose blurb with sentence end must still be rejected")
	}
}

func TestRejectMovieCinemaChannels(t *testing.T) {
	for _, n := range []string{"HU: Cinemax", "US: HBO", "UK: Sky Cinema Premiere", "DE: FilmBox"} {
		if !isMovieOrCinemaChannel(n) {
			t.Fatalf("expected cinema reject for %q", n)
		}
		if !RejectChannelForSport("Basketball", n) {
			t.Fatalf("RejectChannelForSport should drop %q", n)
		}
	}
	if isMovieOrCinemaChannel("US: ESPN") {
		t.Fatal("ESPN is not a cinema channel")
	}
	if isMovieOrCinemaChannel("CA: TSN 1") {
		t.Fatal("TSN is not a cinema channel")
	}
}

func TestNonSportsEPGCategory(t *testing.T) {
	if !isNonSportsEPGCategory("Movie") {
		t.Fatal("Movie category should be non-sports")
	}
	if !isNonSportsEPGCategory("Film / Cinema") {
		t.Fatal("Film category should be non-sports")
	}
	if isNonSportsEPGCategory("Sports") {
		t.Fatal("Sports category must remain allowed")
	}
	if isNonSportsEPGCategory("Basketball") {
		t.Fatal("Basketball category must remain allowed")
	}
}

func TestGenericMentionsRequiresBothOrSeries(t *testing.T) {
	g := store.EPGSportsHit{Title: "MLB Baseball", Description: "Coverage of the Guardians"}
	if genericMentionsMatchup(g, "Cleveland Guardians", "Chicago White Sox") {
		t.Fatal("one-team mention without series marker should not attach")
	}
	g2 := store.EPGSportsHit{Title: "MLB · ALDS", Description: "Guardians host White Sox in ALDS"}
	if !genericMentionsMatchup(g2, "Cleveland Guardians", "Chicago White Sox") {
		t.Fatal("series + one team should attach")
	}
	g3 := store.EPGSportsHit{Title: "MLB Baseball", Description: "Guardians vs White Sox live"}
	if !genericMentionsMatchup(g3, "Cleveland Guardians", "Chicago White Sox") {
		t.Fatal("both team tokens should attach")
	}
}
