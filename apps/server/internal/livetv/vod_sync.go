package livetv

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	vodProgressKey            = "vod:xtream:progress"
	vodMovieCategoriesCache   = "vod:xtream:movie_categories"
	vodSeriesCategoriesCache  = "vod:xtream:series_categories"
)

// VodSyncProgress is published to Redis while a VOD sync runs.
type VodSyncProgress struct {
	Phase           string    `json:"phase"`
	Kind            string    `json:"kind"` // movie | series | both
	CategoriesTotal int       `json:"categories_total"`
	CategoriesDone  int       `json:"categories_done"`
	TitlesWritten   int       `json:"titles_written"`
	Selected        int       `json:"selected"`
	Message         string    `json:"message"`
	Error           string    `json:"error,omitempty"`
	Done            bool      `json:"done"`
	StartedAt       time.Time `json:"started_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// VodSyncStore is the persistence surface the VOD sync needs.
type VodSyncStore interface {
	UpsertVodCategories(ctx context.Context, kind string, cats []VodCategoryInfo, importedIDs []string) error
	UpsertVodMovies(ctx context.Context, movies []VodMovie, keepCategoryExtIDs []string) (int, error)
	UpsertVodSeries(ctx context.Context, series []VodSeries, keepCategoryExtIDs []string) (int, error)
	SetImportedVodCategoryIDs(ctx context.Context, kind string, ids []string) error
	GetSetting(ctx context.Context, key string) (string, error)
}

// VodSyncService runs async Xtream VOD/series category imports.
type VodSyncService struct {
	store  VodSyncStore
	xtream *XtreamClient
	rdb    *redis.Client

	mu      sync.Mutex
	running bool
}

func NewVodSyncService(st VodSyncStore, xtream *XtreamClient, rdb *redis.Client) *VodSyncService {
	return &VodSyncService{store: st, xtream: xtream, rdb: rdb}
}

func (s *VodSyncService) Configured() bool {
	return s != nil && s.xtream != nil && s.xtream.Configured()
}

func (s *VodSyncService) Client() *XtreamClient {
	if s == nil {
		return nil
	}
	return s.xtream
}

func (s *VodSyncService) GetProgress(ctx context.Context) (VodSyncProgress, error) {
	if s.rdb == nil {
		return VodSyncProgress{Phase: "idle", Done: true}, nil
	}
	b, err := s.rdb.Get(ctx, vodProgressKey).Bytes()
	if err == redis.Nil {
		return VodSyncProgress{Phase: "idle", Done: true}, nil
	}
	if err != nil {
		return VodSyncProgress{}, err
	}
	var p VodSyncProgress
	if err := json.Unmarshal(b, &p); err != nil {
		return VodSyncProgress{}, err
	}
	return p, nil
}

func (s *VodSyncService) publish(ctx context.Context, p VodSyncProgress) {
	p.UpdatedAt = time.Now().UTC()
	if s.rdb == nil {
		return
	}
	b, _ := json.Marshal(p)
	_ = s.rdb.Set(ctx, vodProgressKey, b, 6*time.Hour).Err()
}

func (s *VodSyncService) ListCategories(ctx context.Context, kind string) ([]XtreamCategory, error) {
	if !s.Configured() {
		return nil, fmt.Errorf("xtream not configured")
	}
	cacheKey := vodMovieCategoriesCache
	if kind == VodKindSeries {
		cacheKey = vodSeriesCategoriesCache
	}
	if s.rdb != nil {
		if b, err := s.rdb.Get(ctx, cacheKey).Bytes(); err == nil {
			var cats []XtreamCategory
			if json.Unmarshal(b, &cats) == nil {
				return cats, nil
			}
		}
	}
	if _, err := s.xtream.Authenticate(ctx); err != nil {
		return nil, err
	}
	var cats []XtreamCategory
	var err error
	if kind == VodKindSeries {
		cats, err = s.xtream.SeriesCategories(ctx)
	} else {
		cats, err = s.xtream.VodCategories(ctx)
	}
	if err != nil {
		return nil, err
	}
	if s.rdb != nil {
		b, _ := json.Marshal(cats)
		_ = s.rdb.Set(ctx, cacheKey, b, 5*time.Minute).Err()
	}
	return cats, nil
}

// StartSync kicks off an async VOD import for movie and/or series categories.
func (s *VodSyncService) StartSync(ctx context.Context, movieIDs, seriesIDs []string, selectAllMovies, selectAllSeries bool) error {
	if !s.Configured() {
		return fmt.Errorf("xtream not configured")
	}
	if !selectAllMovies && !selectAllSeries && len(movieIDs) == 0 && len(seriesIDs) == 0 {
		return fmt.Errorf("select at least one category")
	}
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return fmt.Errorf("vod sync already running")
	}
	s.running = true
	s.mu.Unlock()

	go func() {
		defer func() {
			s.mu.Lock()
			s.running = false
			s.mu.Unlock()
		}()
		bg, cancel := context.WithTimeout(context.Background(), 3*time.Hour)
		defer cancel()
		if err := s.runSync(bg, movieIDs, seriesIDs, selectAllMovies, selectAllSeries); err != nil {
			slog.Error("vod sync failed", "err", err)
		}
	}()
	return nil
}

func (s *VodSyncService) runSync(ctx context.Context, movieIDs, seriesIDs []string, selectAllMovies, selectAllSeries bool) error {
	started := time.Now().UTC()
	kind := "both"
	if (selectAllMovies || len(movieIDs) > 0) && !(selectAllSeries || len(seriesIDs) > 0) {
		kind = VodKindMovie
	} else if (selectAllSeries || len(seriesIDs) > 0) && !(selectAllMovies || len(movieIDs) > 0) {
		kind = VodKindSeries
	}
	prog := VodSyncProgress{Phase: "auth", Kind: kind, StartedAt: started, Message: "Authenticating with Xtream panel"}
	s.publish(ctx, prog)

	if _, err := s.xtream.Authenticate(ctx); err != nil {
		prog.Phase = "error"
		prog.Error = err.Error()
		prog.Done = true
		prog.Message = "Authentication failed"
		s.publish(ctx, prog)
		return err
	}

	titlesWritten := 0

	if selectAllMovies || len(movieIDs) > 0 {
		n, err := s.syncKind(ctx, &prog, VodKindMovie, movieIDs, selectAllMovies, &titlesWritten)
		if err != nil {
			return err
		}
		_ = n
	}
	if selectAllSeries || len(seriesIDs) > 0 {
		n, err := s.syncKind(ctx, &prog, VodKindSeries, seriesIDs, selectAllSeries, &titlesWritten)
		if err != nil {
			return err
		}
		_ = n
	}

	prog.Phase = "done"
	prog.Done = true
	prog.TitlesWritten = titlesWritten
	prog.Message = fmt.Sprintf("Imported %d titles", titlesWritten)
	s.publish(ctx, prog)
	return nil
}

func (s *VodSyncService) syncKind(ctx context.Context, prog *VodSyncProgress, kind string, categoryIDs []string, selectAll bool, titlesWritten *int) (int, error) {
	prog.Phase = "categories"
	prog.Kind = kind
	prog.Message = fmt.Sprintf("Fetching %s categories", kind)
	s.publish(ctx, *prog)

	var cats []XtreamCategory
	var err error
	if kind == VodKindSeries {
		cats, err = s.xtream.SeriesCategories(ctx)
	} else {
		cats, err = s.xtream.VodCategories(ctx)
	}
	if err != nil {
		prog.Phase = "error"
		prog.Error = err.Error()
		prog.Done = true
		s.publish(ctx, *prog)
		return 0, err
	}
	if s.rdb != nil {
		key := vodMovieCategoriesCache
		if kind == VodKindSeries {
			key = vodSeriesCategoriesCache
		}
		b, _ := json.Marshal(cats)
		_ = s.rdb.Set(ctx, key, b, 5*time.Minute).Err()
	}

	selected := map[string]string{}
	if selectAll {
		for _, c := range cats {
			if IsAdultCategoryName(c.CategoryName) {
				continue
			}
			selected[c.CategoryID] = c.CategoryName
		}
	} else {
		want := map[string]struct{}{}
		for _, id := range categoryIDs {
			want[id] = struct{}{}
		}
		for _, c := range cats {
			if _, ok := want[c.CategoryID]; ok {
				selected[c.CategoryID] = c.CategoryName
			}
		}
	}
	importedIDs := make([]string, 0, len(selected))
	for id := range selected {
		importedIDs = append(importedIDs, id)
	}
	prog.Selected = len(importedIDs)
	prog.CategoriesTotal = len(importedIDs)
	prog.CategoriesDone = 0

	storeCats := make([]VodCategoryInfo, 0, len(cats))
	for i, c := range cats {
		storeCats = append(storeCats, VodCategoryInfo{
			Kind:       kind,
			ExternalID: c.CategoryID,
			Name:       cleanUTF8(c.CategoryName),
			SortOrder:  i,
		})
	}
	if err := s.store.UpsertVodCategories(ctx, kind, storeCats, importedIDs); err != nil {
		prog.Phase = "error"
		prog.Error = err.Error()
		prog.Done = true
		s.publish(ctx, *prog)
		return 0, err
	}
	_ = s.store.SetImportedVodCategoryIDs(ctx, kind, importedIDs)

	if len(importedIDs) == 0 {
		if kind == VodKindSeries {
			_, _ = s.store.UpsertVodSeries(ctx, nil, importedIDs)
		} else {
			_, _ = s.store.UpsertVodMovies(ctx, nil, importedIDs)
		}
		return 0, nil
	}

	prog.Phase = "titles"
	prog.Message = fmt.Sprintf("Downloading %s titles", kind)
	s.publish(ctx, *prog)

	written := 0
	// First pass: prune titles for deselected categories once, then upsert per category.
	if kind == VodKindSeries {
		if _, err := s.store.UpsertVodSeries(ctx, nil, importedIDs); err != nil {
			prog.Phase = "error"
			prog.Error = err.Error()
			prog.Done = true
			s.publish(ctx, *prog)
			return 0, err
		}
	} else {
		if _, err := s.store.UpsertVodMovies(ctx, nil, importedIDs); err != nil {
			prog.Phase = "error"
			prog.Error = err.Error()
			prog.Done = true
			s.publish(ctx, *prog)
			return 0, err
		}
	}

	for i, catID := range importedIDs {
		name := selected[catID]
		prog.CategoriesDone = i
		prog.Message = fmt.Sprintf("%s: %s (%d/%d)", kind, name, i+1, len(importedIDs))
		s.publish(ctx, *prog)

		var n int
		if kind == VodKindSeries {
			items, err := s.xtream.SeriesList(ctx, catID)
			if err != nil {
				slog.Warn("vod series category failed", "category", catID, "err", err)
				continue
			}
			rows := s.xtream.SeriesFromItems(items, catID, name)
			n, err = s.store.UpsertVodSeries(ctx, rows, nil) // nil keep = no prune
			if err != nil {
				// Don't abort the whole import for one bad category (e.g. panel encoding junk).
				slog.Warn("vod series upsert failed", "category", catID, "err", err)
				continue
			}
		} else {
			streams, err := s.xtream.VodStreams(ctx, catID)
			if err != nil {
				slog.Warn("vod movie category failed", "category", catID, "err", err)
				continue
			}
			rows := s.xtream.MoviesFromStreams(streams, catID, name)
			n, err = s.store.UpsertVodMovies(ctx, rows, nil)
			if err != nil {
				slog.Warn("vod movie upsert failed", "category", catID, "err", err)
				continue
			}
		}
		written += n
		*titlesWritten += n
		prog.TitlesWritten = *titlesWritten
		select {
		case <-ctx.Done():
			prog.Phase = "error"
			prog.Error = ctx.Err().Error()
			prog.Done = true
			s.publish(ctx, *prog)
			return written, ctx.Err()
		case <-time.After(150 * time.Millisecond):
		}
	}

	prog.CategoriesDone = len(importedIDs)
	prog.TitlesWritten = *titlesWritten
	s.publish(ctx, *prog)
	return written, nil
}
