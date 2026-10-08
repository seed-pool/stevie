package parser

import "testing"

func TestParseMovie(t *testing.T) {
	got := Parse("/media/movies/Dune (2021)/Dune.2021.2160p.mkv", "movie")
	if got.Kind != KindMovie {
		t.Fatalf("kind=%s", got.Kind)
	}
	if got.Title != "Dune" {
		t.Fatalf("title=%q", got.Title)
	}
	if got.Year != 2021 {
		t.Fatalf("year=%d", got.Year)
	}
}

func TestParseMixedMovieRelease(t *testing.T) {
	got := Parse("/media/library/Doing.Life.2026.2160p.NF.WEB-DL.DDP5.1.Atmos.H.265-Kitsune.mkv", "mixed")
	if got.Kind != KindMovie {
		t.Fatalf("kind=%s", got.Kind)
	}
	if got.Title != "Doing Life" {
		t.Fatalf("title=%q", got.Title)
	}
	if got.Year != 2026 {
		t.Fatalf("year=%d", got.Year)
	}
}

func TestParseTV(t *testing.T) {
	got := Parse("/media/tv/Severance/Season 01/Severance - S01E02 - Half Loop.mkv", "tv")
	if got.Kind != KindTV {
		t.Fatalf("kind=%s", got.Kind)
	}
	if got.Title != "Severance" {
		t.Fatalf("title=%q", got.Title)
	}
	if got.Season != 1 || got.Episode != 2 {
		t.Fatalf("S%02dE%02d", got.Season, got.Episode)
	}
}

func TestParseOutsideSeasonPackEpisode(t *testing.T) {
	got := Parse("/media/library/Outside.2026.S01.1080p.NF.WEB-DL.DDP5.1.H.264-Kitsune/Outside.2026.S01E01.Losers.Will.Get.Punished.1080p.NF.WEB-DL.DDP5.1.H.264-Kitsune.mkv", "mixed")
	if got.Kind != KindTV {
		t.Fatalf("kind=%s", got.Kind)
	}
	if got.Title != "Outside" {
		t.Fatalf("title=%q", got.Title)
	}
	if got.Year != 2026 {
		t.Fatalf("year=%d", got.Year)
	}
	if got.Season != 1 || got.Episode != 1 {
		t.Fatalf("S%02dE%02d", got.Season, got.Episode)
	}
}

func TestParseTVDotNotation(t *testing.T) {
	got := Parse("/media/tv/Show.Name.S02E09.1080p.mkv", "tv")
	if got.Title != "Show Name" {
		t.Fatalf("title=%q", got.Title)
	}
	if got.Season != 2 || got.Episode != 9 {
		t.Fatalf("S%02dE%02d", got.Season, got.Episode)
	}
}

func TestParseTVXNotation(t *testing.T) {
	got := Parse("/media/library/Show.Name.1x03.1080p.mkv", "mixed")
	if got.Kind != KindTV || got.Season != 1 || got.Episode != 3 {
		t.Fatalf("%+v", got)
	}
}
