package api

import (
	"context"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/stevie-media/stevie/apps/server/internal/livetv"
	"github.com/stevie-media/stevie/apps/server/internal/teamlogo"
)

func (s *Server) handleSportsLogo(w http.ResponseWriter, r *http.Request) {
	if s.teamLogos == nil {
		writeErr(w, http.StatusNotFound, "team logos unavailable")
		return
	}
	key := strings.TrimSpace(r.URL.Query().Get("key"))
	if key == "" {
		writeErr(w, http.StatusBadRequest, "key required")
		return
	}
	path, err := s.teamLogos.ResolveFile(key)
	if err != nil {
		writeErr(w, http.StatusNotFound, "logo not found")
		return
	}
	ext := strings.ToLower(filepath.Ext(path))
	ct := "image/png"
	switch ext {
	case ".jpg", ".jpeg":
		ct = "image/jpeg"
	case ".webp":
		ct = "image/webp"
	case ".svg":
		ct = "image/svg+xml"
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "public, max-age=604800, immutable")
	http.ServeFile(w, r, path)
}

// sportsBadgeURL resolves a stored badge (logo:key, remote URL, or empty) to a
// same-origin path the browser can load. Falls back to name lookup in the pack.
func (s *Server) sportsBadgeURL(stored, sport, teamName string) string {
	if key := teamlogo.ParseKey(stored); key != "" {
		if s.teamLogos != nil {
			if _, err := s.teamLogos.ResolveFile(key); err == nil {
				return teamlogo.PublicURL(key)
			}
		}
	}
	if s.teamLogos != nil {
		if key := s.teamLogos.LookupKey(sport, teamName); key != "" {
			return teamlogo.PublicURL(key)
		}
	}
	// Last resort: keep proxying known http(s) badges (not seeded).
	if u := livetv.SanitizeLogoURL(stored); u != "" {
		return "/api/live/logo?url=" + url.QueryEscape(u)
	}
	return ""
}

func (s *Server) leagueLogoURL(league, sport string) string {
	if s.teamLogos == nil {
		return ""
	}
	return s.teamLogos.LeaguePublicURL(league, sport)
}

func (s *Server) inferLeagueName(ctx context.Context, sport, home, away string) string {
	if s.store == nil {
		return ""
	}
	for _, name := range []string{home, away} {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if t, err := s.store.FindSportsTeamByName(ctx, sport, name); err == nil {
			if lg := strings.TrimSpace(t.League); lg != "" {
				return lg
			}
		}
	}
	return ""
}

// inferLeagueFromTitle picks an explicit league label from a matchup title.
// Examples: "NHL: Leafs vs Canadiens", "Optibet hokeja līga: Rīga vs Energija".
func inferLeagueFromTitle(title, sport string) string {
	t := strings.TrimSpace(title)
	if t == "" {
		return ""
	}
	lower := strings.ToLower(t)
	// Explicit Big-4 / major tokens in the title itself.
	type hit struct {
		needle, league string
	}
	for _, h := range []hit{
		{"nhl", "NHL"},
		{"wnba", "WNBA"},
		{"nba", "NBA"},
		{"mlb", "MLB"},
		{"nfl", "NFL"},
		{"cfl", "CFL"},
		{"champions league", "UEFA Champions League"},
		{"premier league", "English Premier League"},
		// Motorsport / fighting / golf — longest / most specific needles first.
		{"formel 1", "Formula 1"},
		{"formula 1", "Formula 1"},
		{"formula one", "Formula 1"},
		{"motogp", "MotoGP"},
		{"nascar", "NASCAR"},
		{"indycar", "IndyCar"},
		{"ufc", "UFC"},
		{"bellator", "Bellator"},
		{"pfl", "PFL"},
		{"lpga", "LPGA"},
		{"dp world", "DP World Tour"},
		{"european tour", "DP World Tour"},
		{"champions tour", "PGA Tour Champions"},
		{"pga tour", "PGA Tour"},
		{"pgatour", "PGA Tour"},
		{"pga", "PGA Tour"},
		{"boxing", "Boxing"},
		{"f1", "Formula 1"},
	} {
		if strings.Contains(lower, h.needle) {
			return h.league
		}
	}
	_ = sport
	// "League Name: Home vs Away" — use the prefix when it looks like a league, not a team.
	if i := strings.Index(t, ":"); i >= 4 && i <= 64 {
		prefix := strings.TrimSpace(t[:i])
		pl := strings.ToLower(prefix)
		if strings.Contains(pl, " vs ") || strings.Contains(pl, " @ ") {
			return ""
		}
		// Prefer prefixes that look league-like (liga/league/liiga/līga/cup/series).
		leagueish := false
		for _, tok := range []string{"liga", "līga", "liiga", "league", "cup", "series", "touruito", "divisjon", "division"} {
			if strings.Contains(pl, tok) {
				leagueish = true
				break
			}
		}
		if leagueish || len(strings.Fields(prefix)) >= 2 {
			return prefix
		}
	}
	return ""
}

func streamedCategorySport(cat string) string {
	switch strings.ToLower(strings.TrimSpace(cat)) {
	case "hockey":
		return "Ice Hockey"
	case "basketball":
		return "Basketball"
	case "baseball":
		return "Baseball"
	case "american-football", "american football":
		return "American Football"
	case "football":
		return "Football"
	default:
		return ""
	}
}
