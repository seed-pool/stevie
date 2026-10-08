package scanner

import (
	"context"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

// WatchLibraries watches configured library roots and ingests new/changed media files.
// Full rescans remain authoritative for deletions.
func (s *Service) WatchLibraries(ctx context.Context) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}

	go func() {
		defer watcher.Close()
		debounce := make(map[string]*time.Timer)

		flush := func(path string) {
			libs, err := s.store.ListLibraries(context.Background())
			if err != nil {
				return
			}
			for _, lib := range libs {
				root, err := filepath.Abs(lib.RootPath)
				if err != nil {
					continue
				}
				if path == root || strings.HasPrefix(path, root+string(os.PathSeparator)) {
					if err := s.ingestFile(context.Background(), lib, path); err != nil {
						slog.Debug("watch ingest failed", "path", path, "err", err)
					}
					return
				}
			}
		}

		for {
			select {
			case <-ctx.Done():
				return
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				slog.Warn("fsnotify error", "err", err)
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if event.Op&(fsnotify.Create|fsnotify.Write|fsnotify.Rename) == 0 {
					continue
				}
				path := event.Name
				if info, err := os.Stat(path); err == nil && info.IsDir() {
					_ = addWatchRecursive(watcher, path)
					continue
				}
				ext := strings.ToLower(filepath.Ext(path))
				if _, ok := s.extensions[ext]; !ok {
					continue
				}
				if t, exists := debounce[path]; exists {
					t.Stop()
				}
				p := path
				debounce[p] = time.AfterFunc(2*time.Second, func() {
					flush(p)
				})
			}
		}
	}()

	libs, err := s.store.ListLibraries(ctx)
	if err != nil {
		return err
	}
	for _, lib := range libs {
		if err := addWatchRecursive(watcher, lib.RootPath); err != nil {
			slog.Warn("watch library failed", "path", lib.RootPath, "err", err)
		}
	}

	go func() {
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				libs, err := s.store.ListLibraries(ctx)
				if err != nil {
					continue
				}
				for _, lib := range libs {
					_ = addWatchRecursive(watcher, lib.RootPath)
				}
			}
		}
	}()

	return nil
}

func addWatchRecursive(w *fsnotify.Watcher, root string) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			_ = w.Add(path)
		}
		return nil
	})
}
