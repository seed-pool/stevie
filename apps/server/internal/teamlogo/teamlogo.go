package teamlogo

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const BadgePrefix = "logo:"

// ManifestTeam is one row from assets/team-logos/manifest.json.
type ManifestTeam struct {
	Key        string   `json:"key"`
	File       string   `json:"file"`
	Sport      string   `json:"sport"`
	League     string   `json:"league"`
	Name       string   `json:"name"`
	Abbr       string   `json:"abbr"`
	Aliases    []string `json:"aliases"`
	ExternalID string   `json:"external_id"`
}

type manifestFile struct {
	Teams []ManifestTeam `json:"teams"`
}

// ManifestLeague is one row from assets/team-logos/leagues.json.
type ManifestLeague struct {
	Key     string   `json:"key"`
	File    string   `json:"file"`
	Name    string   `json:"name"`
	Sport   string   `json:"sport"`
	Aliases []string `json:"aliases"`
}

type leaguesFile struct {
	Leagues []ManifestLeague `json:"leagues"`
}

// Catalog serves bundled team crest files and name→key lookup.
type Catalog struct {
	dir   string
	mu    sync.RWMutex
	byKey map[string]ManifestTeam
	// fold(sport)|fold(name) → key
	byName map[string]string
	// fold(league name) → leagues/nhl
	byLeague map[string]string
}

func Open(dir string) (*Catalog, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return nil, fmt.Errorf("team logos dir empty")
	}
	c := &Catalog{
		dir:      dir,
		byKey:    map[string]ManifestTeam{},
		byName:   map[string]string{},
		byLeague: map[string]string{},
	}
	raw, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return nil, err
	}
	var mf manifestFile
	if err := json.Unmarshal(raw, &mf); err != nil {
		return nil, err
	}
	for _, t := range mf.Teams {
		key := strings.TrimSpace(t.Key)
		if key == "" {
			continue
		}
		c.byKey[key] = t
		c.indexName(t.Sport, t.Name, key)
		for _, a := range t.Aliases {
			c.indexName(t.Sport, a, key)
		}
		c.indexName("", t.Name, key)
	}
	c.loadLeagues()
	return c, nil
}

func (c *Catalog) loadLeagues() {
	raw, err := os.ReadFile(filepath.Join(c.dir, "leagues.json"))
	if err != nil {
		return
	}
	var lf leaguesFile
	if err := json.Unmarshal(raw, &lf); err != nil {
		return
	}
	for _, lg := range lf.Leagues {
		key := strings.TrimSpace(lg.Key)
		if key == "" {
			continue
		}
		c.byKey[key] = ManifestTeam{Key: key, File: lg.File, Sport: lg.Sport, League: lg.Name, Name: lg.Name}
		c.indexLeague(lg.Name, key)
		for _, a := range lg.Aliases {
			c.indexLeague(a, key)
		}
		// Note: do NOT map sport→Big-4 defaults. Latvian Optibet hockey, Euroleague,
		// etc. share a sport with NHL/NBA and must not inherit those watermarks.
	}
}

func (c *Catalog) indexLeague(name, key string) {
	name = strings.TrimSpace(name)
	if name == "" || key == "" {
		return
	}
	k := fold(name)
	if _, ok := c.byLeague[k]; !ok {
		c.byLeague[k] = key
	}
}

// LookupLeague resolves an explicit league display name to a pack key.
// Sport is unused for fallbacks (kept for call-site compatibility) — unknown
// leagues must not inherit NHL/NBA/MLB/NFL watermarks.
func (c *Catalog) LookupLeague(league, sport string) string {
	if c == nil {
		return ""
	}
	_ = sport
	c.mu.RLock()
	defer c.mu.RUnlock()
	if league = strings.TrimSpace(league); league != "" {
		if k := c.byLeague[fold(league)]; k != "" {
			return k
		}
	}
	return ""
}

// LeaguePublicURL returns /api/sports/logo?key=leagues/nhl when league is known.
func (c *Catalog) LeaguePublicURL(league, sport string) string {
	key := c.LookupLeague(league, sport)
	if key == "" {
		return ""
	}
	return PublicURL(key)
}

func (c *Catalog) indexName(sport, name, key string) {
	name = strings.TrimSpace(name)
	if name == "" || key == "" {
		return
	}
	k := fold(sport) + "|" + fold(name)
	if _, ok := c.byName[k]; !ok {
		c.byName[k] = key
	}
}

func (c *Catalog) Dir() string {
	if c == nil {
		return ""
	}
	return c.dir
}

func (c *Catalog) Teams() []ManifestTeam {
	if c == nil {
		return nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]ManifestTeam, 0, len(c.byKey))
	for _, t := range c.byKey {
		out = append(out, t)
	}
	return out
}

func (c *Catalog) LookupKey(sport, name string) string {
	if c == nil {
		return ""
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if sport != "" {
		if k := c.byName[fold(sport)+"|"+fold(name)]; k != "" {
			return k
		}
	}
	return c.byName["|"+fold(name)]
}

func (c *Catalog) ResolveFile(key string) (string, error) {
	if c == nil {
		return "", fmt.Errorf("no catalog")
	}
	key = strings.TrimSpace(key)
	key = strings.TrimPrefix(key, BadgePrefix)
	if key == "" || strings.Contains(key, "..") || strings.HasPrefix(key, "/") {
		return "", fmt.Errorf("invalid key")
	}
	c.mu.RLock()
	t, ok := c.byKey[key]
	c.mu.RUnlock()
	rel := key + ".png"
	if ok && strings.TrimSpace(t.File) != "" {
		rel = t.File
	}
	rel = filepath.Clean(rel)
	if rel == "." || strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("invalid path")
	}
	full := filepath.Join(c.dir, rel)
	root := filepath.Clean(c.dir) + string(os.PathSeparator)
	if full != filepath.Clean(c.dir) && !strings.HasPrefix(full, root) {
		return "", fmt.Errorf("path escape")
	}
	st, err := os.Stat(full)
	if err != nil || st.IsDir() || st.Size() < 100 {
		return "", fmt.Errorf("logo not found")
	}
	return full, nil
}

// BadgeRef returns the DB/storage form logo:<key>.
func BadgeRef(key string) string {
	key = strings.TrimSpace(strings.TrimPrefix(key, BadgePrefix))
	if key == "" {
		return ""
	}
	return BadgePrefix + key
}

// ParseKey extracts logo key from logo:… or bare key (mlb/chw, leagues/nhl).
func ParseKey(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, BadgePrefix) {
		return strings.TrimSpace(strings.TrimPrefix(raw, BadgePrefix))
	}
	// Pack keys like mlb/chw or leagues/nhl — no scheme, one slash.
	if strings.Count(raw, "/") == 1 && !strings.Contains(raw, "://") && !strings.HasPrefix(raw, "/") {
		return raw
	}
	return ""
}

// PublicURL is the same-origin API path for a logo key.
func PublicURL(key string) string {
	key = ParseKey(key)
	if key == "" {
		return ""
	}
	return "/api/sports/logo?key=" + url.QueryEscape(key)
}

func fold(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	repl := strings.NewReplacer(
		"é", "e", "è", "e", "ê", "e", "ë", "e",
		"á", "a", "à", "a", "ä", "a", "â", "a",
		"í", "i", "ó", "o", "ú", "u", "ç", "c", "ñ", "n",
	)
	return repl.Replace(s)
}
