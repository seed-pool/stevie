package config

import "testing"

func TestIsLocal(t *testing.T) {
	cases := []struct {
		domain string
		local  bool
	}{
		{"", true},
		{"localhost", true},
		{"LOCALHOST", true},
		{"127.0.0.1", true},
		{"  localhost  ", true},
		{"stevie.example.com", false},
		{"stevie.example.com", false},
	}
	for _, tc := range cases {
		c := Config{StevieDomain: tc.domain}
		if got := c.IsLocal(); got != tc.local {
			t.Fatalf("domain %q: IsLocal=%v want %v", tc.domain, got, tc.local)
		}
	}
}
