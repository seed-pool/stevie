package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds process configuration loaded from the environment.
type Config struct {
	HTTPAddr         string
	DatabaseURL      string
	RedisURL         string
	DataDir          string
	TMDBAPIKey       string
	TMDBCacheTTL     time.Duration
	TMDBRPS          float64
	StevieDomain     string
	StevieHTTPSPort  string
	StevieListenPort string // public cleartext / local front-door port
	AdminUsername    string
	AdminPassword    string
	SessionTTL       time.Duration
	SecureCookies    bool
	TrustedProxies   bool

	ScanWorkerCount    int
	MediaExtensions    []string
	MediaLibraryHost   string // host path from MEDIA_LIBRARY_PATH (.env)
	MediaLibraryMount  string // in-container mount path used by the scanner
	LiveSourcesMount   string // in-container mount for local M3U/XMLTV files
	LiveSourcesHost    string // optional host path mirrored at LiveSourcesMount
	RecordingsMount    string // in-container mount for live TV recordings
	RecordingsHost     string // host path mirrored at RecordingsMount
	XtreamURL          string
	XtreamUser         string
	XtreamPassword     string
	TheSportsDBAPIKey    string
	TheSportsDBCountries []string
	StreamedRelayURL     string // Node unlock+HLS relay for streamed.pk (ad-free)
	StreamedBaseURL      string // streamed.pk API origin
	StreamedEmbedURL     string // embed.st origin used by unlock relay
	TeamLogosDir         string // bundled team crests (Docker image /app/team-logos)
}

func Load() (Config, error) {
	cfg := Config{
		HTTPAddr:          getenv("STEVIE_HTTP_ADDR", ":8080"),
		DatabaseURL:       os.Getenv("DATABASE_URL"),
		RedisURL:          getenv("REDIS_URL", "redis://redis:6379/0"),
		DataDir:           getenv("STEVIE_DATA_DIR", "/data"),
		TMDBAPIKey:        os.Getenv("TMDB_API_KEY"),
		TMDBCacheTTL:      durationEnv("TMDB_CACHE_TTL", 24*time.Hour),
		TMDBRPS:           floatEnv("TMDB_RPS", 20),
		// Empty / localhost / 127.0.0.1 → local mode (see IsLocal).
		StevieDomain:      strings.TrimSpace(os.Getenv("STEVIE_DOMAIN")),
		StevieHTTPSPort:   getenv("STEVIE_HTTPS_PORT", "8443"),
		StevieListenPort:  getenv("STEVIE_LISTEN_PORT", getenv("STEVIE_HTTP_PORT", "28413")),
		AdminUsername:     getenv("STEVIE_ADMIN_USER", "admin"),
		AdminPassword:     os.Getenv("STEVIE_ADMIN_PASSWORD"),
		SessionTTL:        durationEnv("STEVIE_SESSION_TTL", 7*24*time.Hour),
		SecureCookies:     boolEnv("STEVIE_SECURE_COOKIES", true),
		TrustedProxies:    boolEnv("STEVIE_TRUSTED_PROXIES", true),
		ScanWorkerCount:   intEnv("STEVIE_SCAN_WORKERS", 4),
		MediaExtensions:   splitCSV(getenv("STEVIE_MEDIA_EXTENSIONS", ".mkv,.mp4,.avi,.m4v,.mov,.ts,.m2ts")),
		MediaLibraryHost:  getenv("MEDIA_LIBRARY_PATH", ""),
		MediaLibraryMount: getenv("STEVIE_MEDIA_MOUNT", "/media/library"),
		LiveSourcesMount:  getenv("STEVIE_LIVE_MOUNT", "/live-sources"),
		LiveSourcesHost:   getenv("LIVE_SOURCES_PATH", ""),
		RecordingsMount:   getenv("STEVIE_RECORDINGS_MOUNT", "/recordings"),
		RecordingsHost:    getenv("STEVIE_RECORDINGS_PATH", ""),
		XtreamURL:            getenv("XTREAM_URL", ""),
		XtreamUser:           getenv("XTREAM_USER", ""),
		XtreamPassword:       os.Getenv("XTREAM_PASSWORD"),
		TheSportsDBAPIKey:    getenv("THESPORTSDB_API_KEY", "123"),
		TheSportsDBCountries: splitCSVPreserve(getenv("THESPORTSDB_COUNTRIES", "Canada,United_States")),
		StreamedRelayURL:     getenv("STEVIE_STREAMED_RELAY_URL", "http://127.0.0.1:8091"),
		StreamedBaseURL:      strings.TrimRight(getenv("STEVIE_STREAMED_BASE_URL", "https://streamed.pk"), "/"),
		StreamedEmbedURL:     strings.TrimRight(getenv("STEVIE_STREAMED_EMBED_URL", "https://embed.st"), "/"),
		TeamLogosDir:         getenv("STEVIE_TEAM_LOGOS_DIR", "/app/team-logos"),
	}
	if cfg.RecordingsMount == "" {
		cfg.RecordingsMount = strings.TrimRight(cfg.DataDir, "/") + "/recordings"
	}
	if cfg.TeamLogosDir == "" {
		cfg.TeamLogosDir = "/app/team-logos"
	}

	if cfg.IsLocal() {
		if cfg.StevieDomain == "" {
			cfg.StevieDomain = "localhost"
		}
		// HTTP localhost cannot use Secure cookies; only override when unset.
		if os.Getenv("STEVIE_SECURE_COOKIES") == "" {
			cfg.SecureCookies = false
		}
	}

	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required")
	}
	if cfg.AdminPassword == "" {
		return Config{}, fmt.Errorf("STEVIE_ADMIN_PASSWORD is required")
	}
	return cfg, nil
}

// IsLocal is true when Stevie is meant to be reached on localhost (no public domain).
func (c Config) IsLocal() bool {
	switch strings.ToLower(strings.TrimSpace(c.StevieDomain)) {
	case "", "localhost", "127.0.0.1":
		return true
	default:
		return false
	}
}

func (c Config) ArtworkDir() string {
	return strings.TrimRight(c.DataDir, "/") + "/artwork"
}

// DisplayPath rewrites an in-container media path to the host path from MEDIA_LIBRARY_PATH.
func (c Config) DisplayPath(path string) string {
	mount := strings.TrimRight(c.MediaLibraryMount, "/")
	host := strings.TrimRight(c.MediaLibraryHost, "/")
	if mount == "" || host == "" || path == "" {
		return path
	}
	if path == mount {
		return host
	}
	prefix := mount + "/"
	if strings.HasPrefix(path, prefix) {
		return host + "/" + strings.TrimPrefix(path, prefix)
	}
	return path
}

// ResolveLiveSource maps a configured host path onto the live-sources mount when possible.
func (c Config) ResolveLiveSource(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return raw
	}
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		return raw
	}
	host := strings.TrimRight(c.LiveSourcesHost, "/")
	mount := strings.TrimRight(c.LiveSourcesMount, "/")
	if host != "" && mount != "" {
		if raw == host {
			return mount
		}
		prefix := host + "/"
		if strings.HasPrefix(raw, prefix) {
			return mount + "/" + strings.TrimPrefix(raw, prefix)
		}
	}
	return raw
}

// DisplayRecordingPath rewrites an in-container recording path to the host path.
func (c Config) DisplayRecordingPath(path string) string {
	mount := strings.TrimRight(c.RecordingsMount, "/")
	host := strings.TrimRight(c.RecordingsHost, "/")
	if mount == "" || host == "" || path == "" {
		return path
	}
	if path == mount {
		return host
	}
	prefix := mount + "/"
	if strings.HasPrefix(path, prefix) {
		return host + "/" + strings.TrimPrefix(path, prefix)
	}
	return path
}

// LiveLocalRoots returns directories the server may read for local playlist/EPG files.
func (c Config) LiveLocalRoots() []string {
	roots := []string{c.DataDir, c.MediaLibraryMount, c.LiveSourcesMount}
	out := make([]string, 0, len(roots))
	for _, r := range roots {
		r = strings.TrimSpace(r)
		if r != "" {
			out = append(out, r)
		}
	}
	return out
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func boolEnv(key string, fallback bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return fallback
	}
	return b
}

func intEnv(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

func floatEnv(key string, fallback float64) float64 {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return fallback
	}
	return n
}

func durationEnv(key string, fallback time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return fallback
	}
	return d
}

// splitCSVPreserve keeps original casing (for country/league names).
func splitCSVPreserve(v string) []string {
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func splitCSV(v string) []string {
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(strings.ToLower(p))
		if p == "" {
			continue
		}
		if !strings.HasPrefix(p, ".") {
			p = "." + p
		}
		out = append(out, p)
	}
	return out
}
