package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

type streamedResolve struct {
	M3U8     string
	CachedAt time.Time
}

var (
	streamedResolveMu sync.Mutex
	streamedResolveBy = map[string]streamedResolve{}
)

func (s *Server) streamedRelayBase() string {
	base := strings.TrimSpace(s.cfg.StreamedRelayURL)
	if base == "" {
		base = "http://127.0.0.1:8091"
	}
	return strings.TrimRight(base, "/")
}

func (s *Server) resolveStreamed(source, id, stream string) (string, error) {
	key := source + "|" + id + "|" + stream
	streamedResolveMu.Lock()
	if cur, ok := streamedResolveBy[key]; ok && time.Since(cur.CachedAt) < 45*time.Second && cur.M3U8 != "" {
		streamedResolveMu.Unlock()
		return cur.M3U8, nil
	}
	streamedResolveMu.Unlock()

	body, _ := json.Marshal(map[string]any{
		"source": source,
		"id":     id,
		"stream": stream,
	})
	req, err := http.NewRequest(http.MethodPost, s.streamedRelayBase()+"/resolve", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 30 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("streamed relay: %w", err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode >= 400 {
		return "", fmt.Errorf("streamed resolve HTTP %d: %s", res.StatusCode, truncate(string(raw), 200))
	}
	var out struct {
		OK    bool   `json:"ok"`
		M3U8  string `json:"m3u8"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", err
	}
	if !out.OK || strings.TrimSpace(out.M3U8) == "" {
		msg := out.Error
		if msg == "" {
			msg = "resolve failed"
		}
		return "", fmt.Errorf("%s", msg)
	}
	streamedResolveMu.Lock()
	streamedResolveBy[key] = streamedResolve{M3U8: out.M3U8, CachedAt: time.Now()}
	streamedResolveMu.Unlock()
	return out.M3U8, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func proxyToStreamedRelay(w http.ResponseWriter, r *http.Request, upstreamURL string) {
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, upstreamURL, nil)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	client := &http.Client{Timeout: 45 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 16<<20))
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	if res.StatusCode >= 400 {
		writeErr(w, http.StatusBadGateway, "relay HTTP "+strconv.Itoa(res.StatusCode))
		return
	}
	ct := res.Header.Get("Content-Type")
	isPlaylist := strings.Contains(ct, "mpegurl") || strings.HasPrefix(string(body), "#EXTM3U")
	if isPlaylist {
		text := rewritePlaylistThroughStevie(string(body))
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(text))
		return
	}
	if ct != "" {
		w.Header().Set("Content-Type", ct)
	} else {
		w.Header().Set("Content-Type", "video/mp2t")
	}
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

func rewritePlaylistThroughStevie(playlist string) string {
	lines := strings.Split(playlist, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			if strings.Contains(line, `URI="`) {
				lines[i] = rewriteURIAttr(line)
			}
			continue
		}
		lines[i] = "/api/sports/streamed/hls?url=" + url.QueryEscape(trimmed)
	}
	return strings.Join(lines, "\n")
}

func rewriteURIAttr(line string) string {
	const marker = `URI="`
	start := strings.Index(line, marker)
	if start < 0 {
		return line
	}
	rest := line[start+len(marker):]
	end := strings.Index(rest, `"`)
	if end < 0 {
		return line
	}
	u := rest[:end]
	return line[:start] + `URI="/api/sports/streamed/hls?url=` + url.QueryEscape(u) + `"` + rest[end+1:]
}

// handleStreamedPlay returns a same-origin HLS playlist URL (ad-free via relay unlock).
func (s *Server) handleStreamedPlay(w http.ResponseWriter, r *http.Request) {
	source := strings.TrimSpace(r.URL.Query().Get("source"))
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	stream := strings.TrimSpace(r.URL.Query().Get("stream"))
	if stream == "" {
		stream = "1"
	}
	if source == "" || id == "" {
		writeErr(w, http.StatusBadRequest, "source and id required")
		return
	}
	if _, err := s.resolveStreamed(source, id, stream); err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	playURL := "/api/sports/streamed/playlist?source=" + url.QueryEscape(source) +
		"&id=" + url.QueryEscape(id) +
		"&stream=" + url.QueryEscape(stream)
	writeJSON(w, http.StatusOK, map[string]any{
		"stream_url": playURL,
		"source":     source,
		"id":         id,
		"stream":     stream,
	})
}

func (s *Server) handleStreamedPlaylist(w http.ResponseWriter, r *http.Request) {
	if err := s.authorizeLiveMedia(r); err != nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	source := strings.TrimSpace(r.URL.Query().Get("source"))
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	stream := strings.TrimSpace(r.URL.Query().Get("stream"))
	if stream == "" {
		stream = "1"
	}
	m3u8, err := s.resolveStreamed(source, id, stream)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	proxyToStreamedRelay(w, r, s.streamedRelayBase()+"/hls?url="+url.QueryEscape(m3u8))
}

func (s *Server) handleStreamedHLS(w http.ResponseWriter, r *http.Request) {
	if err := s.authorizeLiveMedia(r); err != nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	target := strings.TrimSpace(r.URL.Query().Get("url"))
	if target == "" || !strings.HasPrefix(target, "https://") {
		writeErr(w, http.StatusBadRequest, "url required")
		return
	}
	proxyToStreamedRelay(w, r, s.streamedRelayBase()+"/hls?url="+url.QueryEscape(target))
}
