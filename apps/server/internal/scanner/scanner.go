package scanner

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stevie-media/stevie/apps/server/internal/ffprobe"
	"github.com/stevie-media/stevie/apps/server/internal/parser"
	"github.com/stevie-media/stevie/apps/server/internal/store"
	"github.com/stevie-media/stevie/apps/server/internal/tmdb"
)

type Service struct {
	store      *store.Store
	tmdb       *tmdb.Client
	rdb        *redis.Client
	extensions map[string]struct{}
	workers    int
	mu         sync.Mutex
	running    map[uuid.UUID]bool
}

type Progress struct {
	LibraryID uuid.UUID `json:"library_id"`
	Phase     string    `json:"phase"`
	Current   int       `json:"current"`
	Total     int       `json:"total"`
	Path      string    `json:"path,omitempty"`
	Message   string    `json:"message,omitempty"`
	Done      bool      `json:"done"`
	Error     string    `json:"error,omitempty"`
}

func New(st *store.Store, tm *tmdb.Client, rdb *redis.Client, exts []string, workers int) *Service {
	m := make(map[string]struct{}, len(exts))
	for _, e := range exts {
		m[strings.ToLower(e)] = struct{}{}
	}
	if workers < 1 {
		workers = 2
	}
	return &Service{
		store:      st,
		tmdb:       tm,
		rdb:        rdb,
		extensions: m,
		workers:    workers,
		running:    map[uuid.UUID]bool{},
	}
}

func (s *Service) IsRunning(id uuid.UUID) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running[id]
}

func (s *Service) ScanLibrary(ctx context.Context, libraryID uuid.UUID) error {
	s.mu.Lock()
	if s.running[libraryID] {
		s.mu.Unlock()
		return fmt.Errorf("scan already running")
	}
	s.running[libraryID] = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.running, libraryID)
		s.mu.Unlock()
	}()

	lib, err := s.store.LibraryByID(ctx, libraryID)
	if err != nil {
		return err
	}
	root, err := filepath.Abs(lib.RootPath)
	if err != nil {
		return err
	}
	if st, err := os.Stat(root); err != nil || !st.IsDir() {
		return fmt.Errorf("library root not accessible: %s", root)
	}

	var paths []string
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if _, ok := s.extensions[ext]; !ok {
			return nil
		}
		// Path jail: reject escapes via symlinks outside root when possible.
		abs, err := filepath.Abs(path)
		if err != nil {
			return nil
		}
		if !strings.HasPrefix(abs, root+string(os.PathSeparator)) && abs != root {
			return nil
		}
		paths = append(paths, abs)
		return nil
	})
	if err != nil {
		s.publish(ctx, Progress{LibraryID: libraryID, Phase: "error", Error: err.Error(), Done: true})
		return err
	}

	s.publish(ctx, Progress{LibraryID: libraryID, Phase: "probe", Total: len(paths)})

	type job struct {
		path string
		idx  int
	}
	jobs := make(chan job)
	var wg sync.WaitGroup
	var mu sync.Mutex
	seen := make([]string, 0, len(paths))

	worker := func() {
		defer wg.Done()
		for j := range jobs {
			if err := s.ingestFile(ctx, lib, j.path); err != nil {
				slog.Warn("ingest file failed", "path", j.path, "err", err)
			}
			mu.Lock()
			seen = append(seen, j.path)
			mu.Unlock()
			s.publish(ctx, Progress{
				LibraryID: libraryID,
				Phase:     "probe",
				Current:   j.idx + 1,
				Total:     len(paths),
				Path:      j.path,
			})
		}
	}
	for i := 0; i < s.workers; i++ {
		wg.Add(1)
		go worker()
	}
	for i, p := range paths {
		select {
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			return ctx.Err()
		case jobs <- job{path: p, idx: i}:
		}
	}
	close(jobs)
	wg.Wait()

	if err := s.store.SoftDeleteMissing(ctx, libraryID, seen); err != nil {
		s.publish(ctx, Progress{LibraryID: libraryID, Phase: "error", Error: err.Error(), Done: true})
		return err
	}

	s.publish(ctx, Progress{LibraryID: libraryID, Phase: "match", Message: "matching metadata"})
	if err := s.matchLibrary(ctx, lib); err != nil {
		slog.Warn("metadata match incomplete", "library", libraryID, "err", err)
	}

	s.publish(ctx, Progress{LibraryID: libraryID, Phase: "done", Current: len(paths), Total: len(paths), Done: true})
	return nil
}

func (s *Service) ingestFile(ctx context.Context, lib store.Library, path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	var inode int64
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		inode = int64(st.Ino)
	}
	parsed := parser.Parse(path, lib.Type)
	probe, err := ffprobe.Run(ctx, path)
	if err != nil {
		return err
	}
	_, err = s.store.UpsertMediaFile(ctx, store.UpsertFileInput{
		LibraryID: lib.ID,
		Path:      path,
		SizeBytes: info.Size(),
		Mtime:     info.ModTime().UTC(),
		Inode:     inode,
		Parsed:    parsed,
		Probe:     probe,
	})
	return err
}

func (s *Service) matchLibrary(ctx context.Context, lib store.Library) error {
	files, err := s.store.UnmatchedFiles(ctx, lib.ID)
	if err != nil {
		return err
	}
	for i, f := range files {
		s.publish(ctx, Progress{
			LibraryID: lib.ID,
			Phase:     "match",
			Current:   i + 1,
			Total:     len(files),
			Path:      f.Path,
		})
		if err := s.matchFile(ctx, lib, f); err != nil {
			slog.Warn("match failed", "path", f.Path, "err", err)
		}
	}
	return nil
}

func (s *Service) matchFile(ctx context.Context, lib store.Library, f store.MediaFile) error {
	title := ""
	if f.ParsedTitle != nil {
		title = *f.ParsedTitle
	}
	if title == "" {
		return fmt.Errorf("no parsed title")
	}
	year := 0
	if f.ParsedYear != nil {
		year = *f.ParsedYear
	}
	season, episode := 0, 0
	if f.ParsedSeason != nil {
		season = *f.ParsedSeason
	}
	if f.ParsedEpisode != nil {
		episode = *f.ParsedEpisode
	}

	// Prefer episode markers from the filename so mixed libraries split correctly.
	isTV := season > 0 && episode > 0
	if lib.Type == "tv" {
		isTV = true
	}
	if lib.Type == "movie" && !isTV {
		isTV = false
	}

	if isTV {
		if season == 0 || episode == 0 {
			return fmt.Errorf("missing season/episode")
		}
		if s.tmdb != nil && s.tmdb.Enabled() {
			results, err := s.tmdb.SearchTV(ctx, title, year)
			if err == nil && len(results) > 0 {
				if err := s.MatchEpisode(ctx, f.ID, results[0].ID, season, episode); err == nil {
					return nil
				} else {
					slog.Warn("tmdb episode match failed, using local title", "title", title, "err", err)
				}
			} else if err != nil {
				slog.Warn("tmdb tv search failed, using local title", "title", title, "err", err)
			}
		}
		return s.MatchLocalEpisode(ctx, f.ID, title, year, season, episode)
	}

	if s.tmdb != nil && s.tmdb.Enabled() {
		results, err := s.tmdb.SearchMovie(ctx, title, year)
		if err == nil && len(results) > 0 {
			if err := s.MatchMovie(ctx, f.ID, results[0].ID); err == nil {
				return nil
			} else {
				slog.Warn("tmdb movie match failed, using local title", "title", title, "err", err)
			}
		} else if err != nil {
			slog.Warn("tmdb movie search failed, using local title", "title", title, "err", err)
		}
	}
	return s.MatchLocalMovie(ctx, f.ID, title, year)
}

func (s *Service) MatchMovie(ctx context.Context, mediaFileID uuid.UUID, tmdbID int) error {
	if s.tmdb == nil || !s.tmdb.Enabled() {
		return fmt.Errorf("tmdb not configured")
	}
	details, err := s.tmdb.GetMovie(ctx, tmdbID)
	if err != nil {
		return err
	}
	genres, _ := json.Marshal(details.Genres)
	castCrew := buildCastCrew(details.Credits.Cast, details.Credits.Crew)
	ext, _ := json.Marshal(details.ExternalIDs)
	movieID, err := s.store.UpsertMovie(ctx, store.MovieUpsert{
		TMDBID:         details.ID,
		Title:          details.Title,
		OriginalTitle:  details.OriginalTitle,
		Tagline:        details.Tagline,
		Overview:       details.Overview,
		ReleaseDate:    details.ReleaseDate,
		RuntimeMinutes: details.Runtime,
		VoteAverage:    details.VoteAverage,
		VoteCount:      details.VoteCount,
		Popularity:     details.Popularity,
		PosterPath:     details.PosterPath,
		BackdropPath:   details.BackdropPath,
		Genres:         genres,
		CastCrew:       castCrew,
		ExternalIDs:    ext,
	})
	if err != nil {
		return err
	}
	return s.store.LinkMovie(ctx, mediaFileID, movieID)
}

func (s *Service) MatchEpisode(ctx context.Context, mediaFileID uuid.UUID, showTMDBID, season, episode int) error {
	if s.tmdb == nil || !s.tmdb.Enabled() {
		return fmt.Errorf("tmdb not configured")
	}
	show, err := s.tmdb.GetShow(ctx, showTMDBID)
	if err != nil {
		return err
	}
	genres, _ := json.Marshal(show.Genres)
	castCrew := buildCastCrew(show.Credits.Cast, show.Credits.Crew)
	ext, _ := json.Marshal(show.ExternalIDs)
	showID, err := s.store.UpsertShow(ctx, store.ShowUpsert{
		TMDBID:       show.ID,
		Name:         show.Name,
		OriginalName: show.OriginalName,
		Tagline:      show.Tagline,
		Overview:     show.Overview,
		FirstAirDate: show.FirstAirDate,
		VoteAverage:  show.VoteAverage,
		VoteCount:    show.VoteCount,
		Popularity:   show.Popularity,
		PosterPath:   show.PosterPath,
		BackdropPath: show.BackdropPath,
		Genres:       genres,
		CastCrew:     castCrew,
		ExternalIDs:  ext,
	})
	if err != nil {
		return err
	}
	seasonDetails, _ := s.tmdb.GetSeason(ctx, showTMDBID, season)
	epUpsert := store.EpisodeUpsert{
		ShowTMDBID:     showTMDBID,
		SeasonNumber:   season,
		EpisodeNumber:  episode,
		Name:           fmt.Sprintf("S%02dE%02d", season, episode),
		SeasonName:     seasonDetails.Name,
		SeasonOverview: seasonDetails.Overview,
		SeasonPoster:   seasonDetails.PosterPath,
		SeasonAirDate:  seasonDetails.AirDate,
		SeasonTMDBID:   seasonDetails.ID,
	}
	if ep, err := s.tmdb.GetEpisode(ctx, showTMDBID, season, episode); err == nil {
		epUpsert.TMDBID = ep.ID
		epUpsert.Name = ep.Name
		epUpsert.Overview = ep.Overview
		epUpsert.StillPath = ep.StillPath
		epUpsert.AirDate = ep.AirDate
		epUpsert.RuntimeMinutes = ep.Runtime
		epUpsert.VoteAverage = ep.VoteAverage
	}
	episodeID, err := s.store.UpsertEpisode(ctx, showID, epUpsert)
	if err != nil {
		return err
	}
	return s.store.LinkEpisode(ctx, mediaFileID, episodeID)
}

func (s *Service) MatchLocalMovie(ctx context.Context, mediaFileID uuid.UUID, title string, year int) error {
	movieID, err := s.store.UpsertMovie(ctx, store.MovieUpsert{
		TMDBID:      localTMDBID("movie", title, strconv.Itoa(year)),
		Title:       title,
		ReleaseDate: yearDate(year),
		Genres:      json.RawMessage("[]"),
		CastCrew:    json.RawMessage(`{"cast":[],"crew":[]}`),
		ExternalIDs: json.RawMessage("{}"),
	})
	if err != nil {
		return err
	}
	return s.store.LinkMovieLocal(ctx, mediaFileID, movieID)
}

func (s *Service) MatchLocalEpisode(ctx context.Context, mediaFileID uuid.UUID, title string, year, season, episode int) error {
	showID, err := s.store.UpsertShow(ctx, store.ShowUpsert{
		TMDBID:       localTMDBID("tv", title, strconv.Itoa(year)),
		Name:         title,
		FirstAirDate: yearDate(year),
		Genres:       json.RawMessage("[]"),
		CastCrew:     json.RawMessage(`{"cast":[],"crew":[]}`),
		ExternalIDs:  json.RawMessage("{}"),
	})
	if err != nil {
		return err
	}
	episodeID, err := s.store.UpsertEpisode(ctx, showID, store.EpisodeUpsert{
		ShowTMDBID:    localTMDBID("tv", title, strconv.Itoa(year)),
		SeasonNumber:  season,
		EpisodeNumber: episode,
		Name:          fmt.Sprintf("S%02dE%02d", season, episode),
		SeasonName:    fmt.Sprintf("Season %d", season),
	})
	if err != nil {
		return err
	}
	return s.store.LinkEpisodeLocal(ctx, mediaFileID, episodeID)
}

func (s *Service) GetProgress(ctx context.Context, libraryID uuid.UUID) (Progress, error) {
	key := progressKey(libraryID)
	b, err := s.rdb.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return Progress{LibraryID: libraryID, Phase: "idle", Done: true}, nil
	}
	if err != nil {
		return Progress{}, err
	}
	var p Progress
	if err := json.Unmarshal(b, &p); err != nil {
		return Progress{}, err
	}
	return p, nil
}

func (s *Service) publish(ctx context.Context, p Progress) {
	if s.rdb == nil {
		return
	}
	b, _ := json.Marshal(p)
	_ = s.rdb.Set(ctx, progressKey(p.LibraryID), b, 2*time.Hour).Err()
}

func progressKey(id uuid.UUID) string {
	return "scan:progress:" + id.String()
}

func buildCastCrew(cast any, crew any) json.RawMessage {
	payload := map[string]any{
		"cast": limitAnySlice(cast, 12),
		"crew": filterCrew(crew),
	}
	b, _ := json.Marshal(payload)
	return b
}

func limitAnySlice(v any, n int) any {
	b, err := json.Marshal(v)
	if err != nil {
		return []any{}
	}
	var items []json.RawMessage
	if err := json.Unmarshal(b, &items); err != nil {
		return []any{}
	}
	if len(items) > n {
		items = items[:n]
	}
	return items
}

func filterCrew(v any) any {
	b, err := json.Marshal(v)
	if err != nil {
		return []any{}
	}
	var items []map[string]any
	if err := json.Unmarshal(b, &items); err != nil {
		return []any{}
	}
	var out []map[string]any
	for _, c := range items {
		job, _ := c["job"].(string)
		if job == "Director" || job == "Writer" || job == "Creator" {
			out = append(out, c)
		}
	}
	return out
}
