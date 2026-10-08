package sportsdb

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stevie-media/stevie/apps/server/internal/store"
)

func TestEpgConfirmsTeamFeedRequiresBothSides(t *testing.T) {
	sport := "Basketball"
	home, away := "Memphis Grizzlies", "Orlando Magic"
	if epgConfirmsTeamFeed(sport, home, away, nil) {
		t.Fatal("empty hits must not confirm")
	}
	if epgConfirmsTeamFeed(sport, home, away, []store.EPGSportsHit{
		{Title: "NBA Basketball", Description: "Coverage of the Orlando Magic"},
	}) {
		t.Fatal("one-sided / generic EPG must not confirm team feed")
	}
	if epgConfirmsTeamFeed(sport, home, away, []store.EPGSportsHit{
		{Title: "Orlando Magic @ Boston Celtics"},
	}) {
		t.Fatal("different opponent must not confirm")
	}
	if !epgConfirmsTeamFeed(sport, home, away, []store.EPGSportsHit{
		{Title: "Orlando Magic @ Memphis Grizzlies"},
	}) {
		t.Fatal("named matchup should confirm")
	}
	if !epgConfirmsTeamFeed(sport, home, away, []store.EPGSportsHit{
		{Title: "NBA", Description: "Magic visit the Grizzlies in Memphis"},
	}) {
		t.Fatal("both nicknames in description should confirm")
	}
}

func TestSameTeamPairOrderIndependent(t *testing.T) {
	if !sameTeamPair("Memphis Grizzlies", "Orlando Magic", "Orlando Magic", "Memphis Grizzlies") {
		t.Fatal("swapped home/away should match")
	}
	if sameTeamPair("Memphis Grizzlies", "Orlando Magic", "Boston Celtics", "Orlando Magic") {
		t.Fatal("different opponent should not match")
	}
}

func TestPreserveDirectChannelLinksSkipsTeamFeeds(t *testing.T) {
	id1, id2, id3 := uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		uuid.MustParse("22222222-2222-2222-2222-222222222222"),
		uuid.MustParse("33333333-3333-3333-3333-333333333333")
	chs := []store.SportsEventChannel{
		{LiveChannelID: &id1, BroadcastLabel: "NBA: ORLANDO MAGIC", ChannelName: "NBA: ORLANDO MAGIC", MatchScore: 1.19},
		{LiveChannelID: &id2, BroadcastLabel: "US: ESPN", ChannelName: "US: ESPN", MatchScore: 1.0},
		{LiveChannelID: &id3, BroadcastLabel: "FS1", ChannelName: "US: FOX SPORTS 1", MatchScore: 11.2},
	}
	got := preserveDirectChannelLinks(chs)
	if len(got) != 1 || got[0].ChannelName != "US: ESPN" {
		t.Fatalf("expected only EPG score=1.0 ESPN row, got %#v", got)
	}
}
