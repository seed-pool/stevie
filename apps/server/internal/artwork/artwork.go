package artwork

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Cache downloads remote TMDB images once and serves them from disk thereafter.
type Cache struct {
	dir    string
	client *http.Client
}

func New(dir string) (*Cache, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &Cache{
		dir:    dir,
		client: &http.Client{Timeout: 30 * time.Second},
	}, nil
}

func (c *Cache) Dir() string { return c.dir }

// Ensure fetches kind/path (e.g. poster /abc.jpg) into the local cache and returns the local path.
func (c *Cache) Ensure(ctx context.Context, kind, tmdbPath, size string) (string, error) {
	tmdbPath = strings.TrimSpace(tmdbPath)
	if tmdbPath == "" || !strings.HasPrefix(tmdbPath, "/") {
		return "", fmt.Errorf("invalid artwork path")
	}
	if size == "" {
		size = "original"
	}
	// Keep cache keys path-safe and jail under c.dir.
	rel := filepath.Join(kind, size, strings.TrimPrefix(tmdbPath, "/"))
	local := filepath.Join(c.dir, rel)
	if !strings.HasPrefix(local, filepath.Clean(c.dir)+string(os.PathSeparator)) && local != filepath.Clean(c.dir) {
		return "", fmt.Errorf("artwork path escapes cache root")
	}
	if st, err := os.Stat(local); err == nil && st.Size() > 0 {
		return local, nil
	}
	if err := os.MkdirAll(filepath.Dir(local), 0o755); err != nil {
		return "", err
	}
	url := "https://image.tmdb.org/t/p/" + size + tmdbPath
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("download artwork: status %d", resp.StatusCode)
	}
	tmp := local + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		os.Remove(tmp)
		return "", err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return "", err
	}
	if err := os.Rename(tmp, local); err != nil {
		os.Remove(tmp)
		return "", err
	}
	return local, nil
}
