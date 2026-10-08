package api

import (
	"sync"
	"time"
)

// recordingMetaCacheEntry holds ffprobe-derived tags for a finished recording file.
// Validated against size+mtime so finalize/rename/rewrite naturally miss the cache.
type recordingMetaCacheEntry struct {
	SizeBytes    int64
	ModUnixNano  int64
	ProgramTitle string
	Description  string
	Category     string
	ChannelName  string
	DurationMS   int64
}

type recordingMetaCache struct {
	mu    sync.Mutex
	byName map[string]recordingMetaCacheEntry
}

func newRecordingMetaCache() *recordingMetaCache {
	return &recordingMetaCache{byName: map[string]recordingMetaCacheEntry{}}
}

func (c *recordingMetaCache) get(name string, size int64, mod time.Time) (recordingMetaCacheEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.byName[name]
	if !ok || e.SizeBytes != size || e.ModUnixNano != mod.UnixNano() {
		return recordingMetaCacheEntry{}, false
	}
	return e, true
}

func (c *recordingMetaCache) put(name string, size int64, mod time.Time, e recordingMetaCacheEntry) {
	e.SizeBytes = size
	e.ModUnixNano = mod.UnixNano()
	c.mu.Lock()
	c.byName[name] = e
	c.mu.Unlock()
}

func (c *recordingMetaCache) delete(name string) {
	c.mu.Lock()
	delete(c.byName, name)
	c.mu.Unlock()
}

// retain drops entries for filenames that no longer exist on disk.
func (c *recordingMetaCache) retain(names map[string]struct{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for name := range c.byName {
		if _, ok := names[name]; !ok {
			delete(c.byName, name)
		}
	}
}

type logoIndexCache struct {
	mu    sync.Mutex
	logos map[string]string
	at    time.Time
	ttl   time.Duration
}

func newLogoIndexCache(ttl time.Duration) *logoIndexCache {
	if ttl <= 0 {
		ttl = time.Minute
	}
	return &logoIndexCache{ttl: ttl}
}

func (c *logoIndexCache) get() (map[string]string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.logos == nil || time.Since(c.at) > c.ttl {
		return nil, false
	}
	return c.logos, true
}

func (c *logoIndexCache) put(logos map[string]string) {
	c.mu.Lock()
	c.logos = logos
	c.at = time.Now()
	c.mu.Unlock()
}
