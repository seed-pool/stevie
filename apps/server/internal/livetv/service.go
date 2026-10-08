package livetv

import (
	"context"
	"fmt"
	"log/slog"
)

// Store is the persistence surface the M3U refresh service needs.
type Store interface {
	GetSetting(ctx context.Context, key string) (string, error)
	ReplaceM3UChannels(ctx context.Context, channels []Channel) error
	ReplaceLiveChannels(ctx context.Context, channels []Channel) error
	ReplaceLivePrograms(ctx context.Context, programs []Program) error
}

const (
	SettingM3U   = "live_m3u_source"
	SettingXMLTV = "live_xmltv_source"
)

// Service refreshes M3U channels and coordinates EPG via SyncService when set.
type Service struct {
	store Store
	roots AllowedLocalRoots
	resolve func(string) string
	sync    *SyncService
}

func NewService(store Store, roots AllowedLocalRoots, resolve func(string) string) *Service {
	if resolve == nil {
		resolve = func(s string) string { return s }
	}
	return &Service{store: store, roots: roots, resolve: resolve}
}

func (s *Service) SetSync(sync *SyncService) {
	s.sync = sync
}

type RefreshResult struct {
	Channels int `json:"channels"`
	Programs int `json:"programs"`
}

func (s *Service) RefreshAll(ctx context.Context) (RefreshResult, error) {
	var out RefreshResult
	n, err := s.RefreshPlaylist(ctx)
	if err != nil {
		return out, err
	}
	out.Channels = n
	p, err := s.RefreshEPG(ctx)
	if err != nil {
		return out, err
	}
	out.Programs = p
	return out, nil
}

func (s *Service) RefreshPlaylist(ctx context.Context) (int, error) {
	raw, err := s.store.GetSetting(ctx, SettingM3U)
	if err != nil {
		return 0, err
	}
	raw = s.resolve(raw)
	if raw == "" {
		if err := s.store.ReplaceM3UChannels(ctx, nil); err != nil {
			return 0, err
		}
		return 0, nil
	}
	rc, _, err := OpenSource(raw, s.roots)
	if err != nil {
		return 0, fmt.Errorf("m3u source: %w", err)
	}
	defer rc.Close()
	channels, err := ParseM3U(rc)
	if err != nil {
		return 0, fmt.Errorf("parse m3u: %w", err)
	}
	if err := s.store.ReplaceM3UChannels(ctx, channels); err != nil {
		return 0, err
	}
	slog.Info("live playlist refreshed", "channels", len(channels))
	return len(channels), nil
}

func (s *Service) RefreshEPG(ctx context.Context) (int, error) {
	if s.sync != nil {
		n, err := s.sync.RefreshEPGMerge(ctx)
		if err != nil {
			return 0, err
		}
		slog.Info("live epg refreshed", "programs", n)
		return n, nil
	}
	// Fallback: Settings XMLTV only (no Xtream merge).
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
		return 0, fmt.Errorf("xmltv source: %w", err)
	}
	defer rc.Close()
	programs, err := ParseXMLTV(rc)
	if err != nil {
		return 0, fmt.Errorf("parse xmltv: %w", err)
	}
	if err := s.store.ReplaceLivePrograms(ctx, programs); err != nil {
		return 0, err
	}
	slog.Info("live epg refreshed", "programs", len(programs))
	return len(programs), nil
}
