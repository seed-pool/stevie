package livetv

import (
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// AllowedLocalRoots limits filesystem reads for configured sources.
type AllowedLocalRoots []string

// OpenSource opens an HTTP(S) URL or a local file under allowed roots.
func OpenSource(raw string, roots AllowedLocalRoots) (io.ReadCloser, string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, "", fmt.Errorf("empty source")
	}
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		u, err := url.Parse(raw)
		if err != nil {
			return nil, "", err
		}
		client := &http.Client{Timeout: 3 * time.Minute}
		req, err := http.NewRequest(http.MethodGet, u.String(), nil)
		if err != nil {
			return nil, "", err
		}
		req.Header.Set("User-Agent", "Stevie/1.0")
		res, err := client.Do(req)
		if err != nil {
			return nil, "", err
		}
		if res.StatusCode >= 400 {
			res.Body.Close()
			return nil, "", fmt.Errorf("fetch failed: %s", res.Status)
		}
		body, name := maybeGunzip(res.Body, u.Path)
		return body, name, nil
	}

	path, err := roots.resolve(raw)
	if err != nil {
		return nil, "", err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, "", err
	}
	body, name := maybeGunzip(f, path)
	return body, name, nil
}

func (roots AllowedLocalRoots) resolve(raw string) (string, error) {
	path := filepath.Clean(raw)
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("local path must be absolute (got %q)", raw)
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("path is a directory")
	}
	if len(roots) == 0 {
		return path, nil
	}
	for _, root := range roots {
		root = filepath.Clean(root)
		if root == "" {
			continue
		}
		rel, err := filepath.Rel(root, path)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			return path, nil
		}
	}
	return "", fmt.Errorf("path %q is outside allowed directories", path)
}

func maybeGunzip(r io.ReadCloser, name string) (io.ReadCloser, string) {
	lower := strings.ToLower(name)
	if strings.HasSuffix(lower, ".gz") {
		gz, err := gzip.NewReader(r)
		if err != nil {
			return r, name
		}
		return &gzipReadCloser{gz: gz, src: r}, strings.TrimSuffix(name, filepath.Ext(name))
	}
	return r, name
}

type gzipReadCloser struct {
	gz  *gzip.Reader
	src io.ReadCloser
}

func (g *gzipReadCloser) Read(p []byte) (int, error) { return g.gz.Read(p) }
func (g *gzipReadCloser) Close() error {
	_ = g.gz.Close()
	return g.src.Close()
}
