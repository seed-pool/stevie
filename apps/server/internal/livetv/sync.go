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

const xtreamProgressKey = "live:xtream:progress"
const epgProgressKey = "live:epg:progress"
const xtreamCategoriesCacheKey = "live:xtream:categories"

// SyncProgress is published to Redis while an Xtream sync runs.
type SyncProgress struct {
	Phase           string    `json:"phase"`
	CategoriesTotal int       `json:"categories_total"`
	CategoriesDone  int       `json:"categories_done"`
	ChannelsWritten int       `json:"channels_written"`
	ProgramsWritten int       `json:"programs_written"`
	Selected        int       `json:"selected"`
	Message         string    `json:"message"`
	Error           string    `json:"error,omitempty"`
	Done            bool      `json:"done"`
	StartedAt       time.Time `json:"started_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// EPGProgress is published to Redis while an EPG-only refresh runs.
type EPGProgress struct {
	Phase           string    `json:"phase"`
	ProgramsWritten int       `json:"programs_written"`
	Message         string    `json:"message"`
	Error           string    `json:"error,omitempty"`
	Done            bool      `json:"done"`
	StartedAt       time.Time `json:"started_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// SyncStore is the persistence surface the Xtream sync needs.
type SyncStore interface {
	UpsertXtreamCategories(ctx context.Context, cats []CategoryInfo, importedIDs []string) error
	UpsertXtreamChannels(ctx context.Context, channels []Channel, keepCategoryExtIDs []string) (int, error)
	SetImportedXtreamCategoryIDs(ctx context.Context, ids []string) error
	EPGChannelKeys(ctx context.Context) (map[string]struct{}, []string, error)
	MergeLivePrograms(ctx context.Context, programs []Program, usedChannelKeys []string) error
	AppendLivePrograms(ctx context.Context, programs []Program) error
	UpsertProgramsOverride(ctx context.Context, programs []Program) error
	GetSetting(ctx context.Context, key string) (string, error)
}

// SyncService runs async Xtream category/channel/EPG imports.
type SyncService struct {
	store   SyncStore
	xtream  *XtreamClient
	rdb     *redis.Client
	roots   AllowedLocalRoots
	resolve func(string) string

	mu         sync.Mutex
	running    bool
	epgRunning bool
}

func NewSyncService(st SyncStore, xtream *XtreamClient, rdb *redis.Client, roots AllowedLocalRoots, resolve func(string) string) *SyncService {
	if resolve == nil {
		resolve = func(s string) string { return s }
	}
	return &SyncService{store: st, xtream: xtream, rdb: rdb, roots: roots, resolve: resolve}
}

func (s *SyncService) Configured() bool {
	return s != nil && s.xtream != nil && s.xtream.Configured()
}

func (s *SyncService) GetProgress(ctx context.Context) (SyncProgress, error) {
	if s.rdb == nil {
		return SyncProgress{Phase: "idle", Done: true}, nil
	}
	b, err := s.rdb.Get(ctx, xtreamProgressKey).Bytes()
	if err == redis.Nil {
		return SyncProgress{Phase: "idle", Done: true}, nil
	}
	if err != nil {
		return SyncProgress{}, err
	}
	var p SyncProgress
	if err := json.Unmarshal(b, &p); err != nil {
		return SyncProgress{}, err
	}
	return p, nil
}

func (s *SyncService) publish(ctx context.Context, p SyncProgress) {
	p.UpdatedAt = time.Now().UTC()
	if s.rdb == nil {
		return
	}
	b, _ := json.Marshal(p)
	_ = s.rdb.Set(ctx, xtreamProgressKey, b, 6*time.Hour).Err()
}

func (s *SyncService) GetEPGProgress(ctx context.Context) (EPGProgress, error) {
	if s.rdb == nil {
		return EPGProgress{Phase: "idle", Done: true}, nil
	}
	b, err := s.rdb.Get(ctx, epgProgressKey).Bytes()
	if err == redis.Nil {
		return EPGProgress{Phase: "idle", Done: true}, nil
	}
	if err != nil {
		return EPGProgress{}, err
	}
	var p EPGProgress
	if err := json.Unmarshal(b, &p); err != nil {
		return EPGProgress{}, err
	}
	return p, nil
}

func (s *SyncService) publishEPG(ctx context.Context, p EPGProgress) {
	p.UpdatedAt = time.Now().UTC()
	if s.rdb == nil {
		return
	}
	b, _ := json.Marshal(p)
	_ = s.rdb.Set(ctx, epgProgressKey, b, 6*time.Hour).Err()
}

// ListCategories returns panel categories (Redis-cached ~5 minutes).
func (s *SyncService) ListCategories(ctx context.Context) ([]XtreamCategory, error) {
	if !s.Configured() {
		return nil, fmt.Errorf("xtream not configured")
	}
	if s.rdb != nil {
		if b, err := s.rdb.Get(ctx, xtreamCategoriesCacheKey).Bytes(); err == nil {
			var cats []XtreamCategory
			if json.Unmarshal(b, &cats) == nil {
				return cats, nil
			}
		}
	}
	if _, err := s.xtream.Authenticate(ctx); err != nil {
		return nil, err
	}
	cats, err := s.xtream.LiveCategories(ctx)
	if err != nil {
		return nil, err
	}
	if s.rdb != nil {
		b, _ := json.Marshal(cats)
		_ = s.rdb.Set(ctx, xtreamCategoriesCacheKey, b, 5*time.Minute).Err()
	}
	return cats, nil
}

// StartSync kicks off an async import.
func (s *SyncService) StartSync(ctx context.Context, categoryIDs []string, selectAll bool) error {
	if !s.Configured() {
		return fmt.Errorf("xtream not configured")
	}
	s.mu.Lock()
	if s.running || s.epgRunning {
		s.mu.Unlock()
		return fmt.Errorf("live sync already running")
	}
	s.running = true
	s.mu.Unlock()

	go func() {
		defer func() {
			s.mu.Lock()
			s.running = false
			s.mu.Unlock()
		}()
		bg, cancel := context.WithTimeout(context.Background(), 45*time.Minute)
		defer cancel()
		if err := s.runSync(bg, categoryIDs, selectAll); err != nil {
			slog.Error("xtream sync failed", "err", err)
		}
	}()
	return nil
}

// StartEPGRefresh reloads provider XMLTV (+ optional override) without touching channels/VOD.
func (s *SyncService) StartEPGRefresh(ctx context.Context) error {
	if s == nil {
		return fmt.Errorf("epg sync unavailable")
	}
	s.mu.Lock()
	if s.running || s.epgRunning {
		s.mu.Unlock()
		return fmt.Errorf("live sync already running")
	}
	s.epgRunning = true
	s.mu.Unlock()

	go func() {
		defer func() {
			s.mu.Lock()
			s.epgRunning = false
			s.mu.Unlock()
		}()
		bg, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
		defer cancel()
		started := time.Now().UTC()
		prog := EPGProgress{
			Phase:     "starting",
			StartedAt: started,
			Message:   "Preparing EPG refresh…",
		}
		s.publishEPG(bg, prog)
		n, err := s.refreshEPGMerge(bg, func(p EPGProgress) {
			p.StartedAt = started
			s.publishEPG(bg, p)
		})
		if err != nil {
			prog.Phase = "error"
			prog.Error = err.Error()
			prog.Done = true
			prog.ProgramsWritten = n
			prog.Message = "EPG refresh failed"
			s.publishEPG(bg, prog)
			slog.Error("epg refresh failed", "err", err)
			return
		}
		prog.Phase = "done"
		prog.Done = true
		prog.ProgramsWritten = n
		prog.Message = fmt.Sprintf("EPG refreshed · %d programmes", n)
		s.publishEPG(bg, prog)
		slog.Info("epg refresh complete", "programs", n)
	}()
	return nil
}

func (s *SyncService) runSync(ctx context.Context, categoryIDs []string, selectAll bool) error {
	started := time.Now().UTC()
	prog := SyncProgress{Phase: "auth", StartedAt: started, Message: "Authenticating with Xtream panel"}
	s.publish(ctx, prog)

	if _, err := s.xtream.Authenticate(ctx); err != nil {
		prog.Phase = "error"
		prog.Error = err.Error()
		prog.Done = true
		prog.Message = "Authentication failed"
		s.publish(ctx, prog)
		return err
	}

	prog.Phase = "categories"
	prog.Message = "Fetching live categories"
	s.publish(ctx, prog)

	cats, err := s.xtream.LiveCategories(ctx)
	if err != nil {
		prog.Phase = "error"
		prog.Error = err.Error()
		prog.Done = true
		s.publish(ctx, prog)
		return err
	}
	if s.rdb != nil {
		b, _ := json.Marshal(cats)
		_ = s.rdb.Set(ctx, xtreamCategoriesCacheKey, b, 5*time.Minute).Err()
	}

	selected := map[string]string{}
	if selectAll {
		for _, c := range cats {
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
	prog.CategoriesTotal = len(cats)
	prog.CategoriesDone = len(importedIDs)

	storeCats := make([]CategoryInfo, 0, len(cats))
	for i, c := range cats {
		storeCats = append(storeCats, CategoryInfo{
			ExternalID: c.CategoryID,
			Name:       c.CategoryName,
			SortOrder:  i,
		})
	}
	if err := s.store.UpsertXtreamCategories(ctx, storeCats, importedIDs); err != nil {
		prog.Phase = "error"
		prog.Error = err.Error()
		prog.Done = true
		s.publish(ctx, prog)
		return err
	}
	_ = s.store.SetImportedXtreamCategoryIDs(ctx, importedIDs)

	prog.Phase = "streams"
	prog.Message = "Downloading live streams from panel"
	s.publish(ctx, prog)

	streams, err := s.xtream.LiveStreams(ctx)
	if err != nil {
		prog.Phase = "error"
		prog.Error = err.Error()
		prog.Done = true
		s.publish(ctx, prog)
		return err
	}

	channels := s.xtream.ChannelsFromStreams(streams, selected)
	prog.Phase = "upsert"
	prog.Message = fmt.Sprintf("Saving %d channels", len(channels))
	s.publish(ctx, prog)

	written, err := s.store.UpsertXtreamChannels(ctx, channels, importedIDs)
	if err != nil {
		prog.Phase = "error"
		prog.Error = err.Error()
		prog.Done = true
		s.publish(ctx, prog)
		return err
	}
	prog.ChannelsWritten = written

	allowed, keys, err := s.store.EPGChannelKeys(ctx)
	if err != nil {
		prog.Phase = "error"
		prog.Error = err.Error()
		prog.Done = true
		s.publish(ctx, prog)
		return err
	}

	prog.Phase = "epg_provider"
	prog.Message = "Loading provider EPG (xmltv.php)"
	s.publish(ctx, prog)

	programsWritten := 0
	if len(importedIDs) > 0 {
		rc, err := s.xtream.OpenXMLTV(ctx)
		if err != nil {
			slog.Warn("xtream xmltv unavailable", "err", err)
			prog.Message = "Provider EPG unavailable: " + err.Error()
			s.publish(ctx, prog)
		} else {
			if err := s.store.MergeLivePrograms(ctx, nil, keys); err != nil {
				rc.Close()
				prog.Phase = "error"
				prog.Error = err.Error()
				prog.Done = true
				s.publish(ctx, prog)
				return err
			}
			batch := make([]Program, 0, 5000)
			err = StreamXMLTVFiltered(rc, allowed, func(p Program) error {
				batch = append(batch, p)
				if len(batch) >= 5000 {
					if err := s.store.AppendLivePrograms(ctx, batch); err != nil {
						return err
					}
					programsWritten += len(batch)
					batch = batch[:0]
					prog.ProgramsWritten = programsWritten
					s.publish(ctx, prog)
				}
				return nil
			})
			rc.Close()
			if err != nil {
				prog.Phase = "error"
				prog.Error = err.Error()
				prog.Done = true
				s.publish(ctx, prog)
				return err
			}
			if len(batch) > 0 {
				if err := s.store.AppendLivePrograms(ctx, batch); err != nil {
					prog.Phase = "error"
					prog.Error = err.Error()
					prog.Done = true
					s.publish(ctx, prog)
					return err
				}
				programsWritten += len(batch)
			}
		}
	}

	prog.Phase = "epg_override"
	prog.Message = "Applying optional XMLTV override"
	prog.ProgramsWritten = programsWritten
	s.publish(ctx, prog)

	n, err := s.applyXMLTVOverride(ctx, allowed)
	if err != nil {
		prog.Phase = "error"
		prog.Error = err.Error()
		prog.Done = true
		s.publish(ctx, prog)
		return err
	}
	programsWritten += n

	prog.Phase = "done"
	prog.Done = true
	prog.ProgramsWritten = programsWritten
	prog.Message = fmt.Sprintf("Imported %d channels, %d programmes", written, programsWritten)
	s.publish(ctx, prog)
	slog.Info("xtream sync complete", "channels", written, "programs", programsWritten, "categories", len(importedIDs))
	return nil
}

func (s *SyncService) applyXMLTVOverride(ctx context.Context, allowed map[string]struct{}) (int, error) {
	raw, err := s.store.GetSetting(ctx, SettingXMLTV)
	if err != nil {
		return 0, err
	}
	raw = s.resolve(raw)
	if raw == "" {
		return 0, nil
	}
	rc, _, err := OpenSource(raw, s.roots)
	if err != nil {
		return 0, fmt.Errorf("xmltv override: %w", err)
	}
	defer rc.Close()
	batch := make([]Program, 0, 2000)
	total := 0
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		if err := s.store.UpsertProgramsOverride(ctx, batch); err != nil {
			return err
		}
		total += len(batch)
		batch = batch[:0]
		return nil
	}
	err = StreamXMLTVFiltered(rc, allowed, func(p Program) error {
		batch = append(batch, p)
		if len(batch) >= 2000 {
			return flush()
		}
		return nil
	})
	if err != nil {
		return total, err
	}
	if err := flush(); err != nil {
		return total, err
	}
	return total, nil
}

// RefreshEPGMerge reloads provider EPG (if configured) then applies Settings XMLTV override.
func (s *SyncService) RefreshEPGMerge(ctx context.Context) (int, error) {
	return s.refreshEPGMerge(ctx, nil)
}

func (s *SyncService) refreshEPGMerge(ctx context.Context, report func(EPGProgress)) (int, error) {
	emit := func(phase, msg string, written int) {
		if report == nil {
			return
		}
		report(EPGProgress{Phase: phase, Message: msg, ProgramsWritten: written})
	}

	emit("prepare", "Resolving channel EPG keys…", 0)
	allowed, keys, err := s.store.EPGChannelKeys(ctx)
	if err != nil {
		return 0, err
	}
	total := 0
	cleared := false
	if s.Configured() && len(keys) > 0 {
		phaseStart := time.Now()
		stopBeat := make(chan struct{})
		var beatMu sync.Mutex
		beatPhase := "download"
		beatMsg := "Downloading provider EPG (xmltv.php)…"
		beatWritten := 0
		if report != nil {
			go func() {
				t := time.NewTicker(750 * time.Millisecond)
				defer t.Stop()
				for {
					select {
					case <-stopBeat:
						return
					case <-ctx.Done():
						return
					case <-t.C:
						beatMu.Lock()
						ph, msg, w := beatPhase, beatMsg, beatWritten
						beatMu.Unlock()
						elapsed := time.Since(phaseStart).Round(time.Second)
						report(EPGProgress{
							Phase:           ph,
							Message:         fmt.Sprintf("%s · %s", msg, elapsed),
							ProgramsWritten: w,
						})
					}
				}
			}()
		}
		setBeat := func(phase, msg string, written int, publishNow bool) {
			beatMu.Lock()
			beatPhase, beatMsg, beatWritten = phase, msg, written
			beatMu.Unlock()
			if publishNow {
				emit(phase, msg, written)
			}
		}
		setBeat("download", "Downloading provider EPG (xmltv.php)…", 0, true)
		rc, err := s.xtream.OpenXMLTV(ctx)
		if err == nil {
			setBeat("clear", "Clearing previous guide entries…", 0, true)
			if err := s.store.MergeLivePrograms(ctx, nil, keys); err != nil {
				close(stopBeat)
				rc.Close()
				return 0, err
			}
			cleared = true
			setBeat("import", "Parsing & importing programmes…", 0, true)
			batch := make([]Program, 0, 5000)
			err = StreamXMLTVFiltered(rc, allowed, func(p Program) error {
				batch = append(batch, p)
				if len(batch)%250 == 0 {
					setBeat("import", fmt.Sprintf("Importing programmes… %d matched", total+len(batch)), total+len(batch), false)
				}
				if len(batch) >= 5000 {
					if err := s.store.AppendLivePrograms(ctx, batch); err != nil {
						return err
					}
					total += len(batch)
					batch = batch[:0]
					setBeat("import", fmt.Sprintf("Importing programmes… %d saved", total), total, true)
				}
				return nil
			})
			rc.Close()
			close(stopBeat)
			if err != nil {
				return total, err
			}
			if len(batch) > 0 {
				if err := s.store.AppendLivePrograms(ctx, batch); err != nil {
					return total, err
				}
				total += len(batch)
				emit("import", fmt.Sprintf("Imported %d programmes from provider", total), total)
			}
		} else {
			close(stopBeat)
			slog.Warn("provider epg skip", "err", err)
			emit("download", "Provider EPG unavailable — trying XMLTV override…", 0)
		}
	}
	// If no provider EPG, still clear+apply override-only when XMLTV is set.
	if !cleared {
		raw, _ := s.store.GetSetting(ctx, SettingXMLTV)
		if s.resolve(raw) != "" && len(keys) > 0 {
			emit("clear", "Clearing previous guide entries…", total)
			if err := s.store.MergeLivePrograms(ctx, nil, keys); err != nil {
				return total, err
			}
		}
	}
	emit("override", "Applying optional XMLTV override…", total)
	n, err := s.applyXMLTVOverride(ctx, allowed)
	if err != nil {
		return total + n, err
	}
	emit("finalize", fmt.Sprintf("Finishing… %d programmes", total+n), total+n)
	return total + n, nil
}
