package sportsdb

import (
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestPipePackageBrandSplit(t *testing.T) {
	pkg, brand := splitPackageBrand("US| ESPN HD")
	if pkg != "US" || brand != "espn" {
		t.Fatalf("pipe split got pkg=%q brand=%q", pkg, brand)
	}
	pkg, brand = splitPackageBrand("CA| TSN 1 HD")
	if pkg != "CA" || !strings.HasPrefix(brand, "tsn") {
		t.Fatalf("CA TSN got pkg=%q brand=%q", pkg, brand)
	}
}

func TestMatchESPNWithPipeNames(t *testing.T) {
	cands := []channelCandidate{
		cand("US| ESPN HD"),
		cand("US| ESPN ᴴᴰ ⁶⁰ᶠᵖˢ"),
		cand("US| ESPN 2 HD"),
		cand("CA| TSN 1 HD"),
		cand("CA| SPORTSNET ONTARIO HD"),
		cand("####### ESPN PPV #######"),
	}
	got := MatchAllChannels("ESPN", cands, MatchOpts{Limit: 8})
	if len(got) == 0 {
		t.Fatal("ESPN should match US| ESPN* with pipe package")
	}
	for _, m := range got {
		u := strings.ToUpper(m.Name)
		if strings.Contains(u, "ESPN 2") || strings.Contains(u, "TSN") {
			t.Fatalf("unexpected match %s", m.Name)
		}
	}
	tsn := MatchAllChannels("TSN", cands, MatchOpts{Limit: 8})
	if len(tsn) == 0 {
		t.Fatal("TSN should alias onto CA| TSN 1 HD")
	}
	tsn1 := MatchAllChannels("TSN1", cands, MatchOpts{Limit: 8})
	if len(tsn1) == 0 {
		t.Fatal("TSN1 should match CA| TSN 1 HD")
	}
	sn := MatchAllChannels("Sportsnet", cands, MatchOpts{Limit: 8})
	if len(sn) == 0 {
		t.Fatal("Sportsnet should match CA| SPORTSNET ONTARIO HD")
	}
	t.Logf("ESPN=%v TSN=%v TSN1=%v SN=%v", namesOf(got), namesOf(tsn), namesOf(tsn1), namesOf(sn))
	_ = uuid.Nil
}

func TestPreferHomeRegionMatches(t *testing.T) {
	in := []ChannelMatch{
		{Name: "US| NBA TV HD", Score: 1.12},
		{Name: "CA| NBA TV", Score: 1.02},
		{Name: "TH| NBA TV", Score: 0.84},
		{Name: "BR| FOX HD", Score: 0.84},
		{Name: "NESN", Score: 0.9},
	}
	got := preferHomeRegionMatches(in)
	names := namesOf(got)
	if len(got) != 3 {
		t.Fatalf("want US+CA+NESN, got %v", names)
	}
	for _, n := range names {
		if strings.HasPrefix(n, "TH|") || strings.HasPrefix(n, "BR|") {
			t.Fatalf("foreign twin leaked: %v", names)
		}
	}
}

func TestMatchNationalPipeFeeds(t *testing.T) {
	cands := []channelCandidate{
		cand("US| ESPN HD"),
		cand("US| FOX HD"),
		cand("US| CBS HD"),
		cand("US| NBA TV HD"),
		cand("US| NHL NETWORK"),
		cand("CA| FOX EAST HD"),
		cand("CA| CBS EAST HD"),
		cand("CA| NBA TV"),
		cand("CA| TSN 1 HD"),
		cand("CA| SPORTSNET ONTARIO HD"),
	}
	cases := map[string]string{
		"ESPN":        "US| ESPN HD",
		"Fox":         "US| FOX HD",
		"CBS":         "US| CBS HD",
		"NBA TV":      "US| NBA TV HD",
		"NHL Network": "US| NHL NETWORK",
		"NHL Net":     "US| NHL NETWORK",
		"TSN":         "CA| TSN 1 HD",
		"Sportsnet":   "CA| SPORTSNET ONTARIO HD",
		"SN":          "CA| SPORTSNET ONTARIO HD",
	}
	for label, want := range cases {
		got := MatchAllChannels(label, cands, MatchOpts{Limit: 4})
		if len(got) == 0 {
			t.Errorf("%s: no matches", label)
			continue
		}
		if got[0].Name != want {
			t.Errorf("%s: got %q want %q (all=%v)", label, got[0].Name, want, namesOf(got))
		}
	}
}

func TestAMPLabelNotProvider(t *testing.T) {
	if isProviderBroadcastLabel("AMP: CBS") {
		t.Fatal("AMP: CBS should not be a provider label")
	}
	if isProviderBroadcastLabel("US| CBS HD") {
		t.Fatal("US| CBS HD should not be a provider label")
	}
	if !isProviderBroadcastLabel("CBS") {
		t.Fatal("CBS should be a provider label")
	}
}
