package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stevie-media/stevie/apps/server/internal/api"
	"github.com/stevie-media/stevie/apps/server/internal/artwork"
	"github.com/stevie-media/stevie/apps/server/internal/auth"
	"github.com/stevie-media/stevie/apps/server/internal/config"
	"github.com/stevie-media/stevie/apps/server/internal/db"
	"github.com/stevie-media/stevie/apps/server/internal/playback"
	"github.com/stevie-media/stevie/apps/server/internal/scanner"
	"github.com/stevie-media/stevie/apps/server/internal/store"
	"github.com/stevie-media/stevie/apps/server/internal/tmdb"
)

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	cfg, err := config.Load()
	if err != nil {
		slog.Error("config", "err", err)
		os.Exit(1)
	}

	ctx := context.Background()
	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		slog.Error("database", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	opt, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		slog.Error("redis url", "err", err)
		os.Exit(1)
	}
	rdb := redis.NewClient(opt)
	if err := rdb.Ping(ctx).Err(); err != nil {
		slog.Error("redis", "err", err)
		os.Exit(1)
	}
	defer rdb.Close()

	st := store.New(pool)
	hash, err := auth.HashPassword(cfg.AdminPassword)
	if err != nil {
		slog.Error("admin password hash", "err", err)
		os.Exit(1)
	}
	if err := st.EnsureAdmin(ctx, cfg.AdminUsername, hash); err != nil {
		slog.Error("ensure admin", "err", err)
		os.Exit(1)
	}
	if _, err := st.EnsureDefaultLibrary(ctx, "Library", cfg.MediaLibraryMount); err != nil {
		slog.Error("ensure default library", "err", err)
		os.Exit(1)
	}

	art, err := artwork.New(cfg.ArtworkDir())
	if err != nil {
		slog.Error("artwork cache", "err", err)
		os.Exit(1)
	}

	tm := tmdb.NewWithRPS(cfg.TMDBAPIKey, rdb, cfg.TMDBCacheTTL, cfg.TMDBRPS)
	scan := scanner.New(st, tm, rdb, cfg.MediaExtensions, cfg.ScanWorkerCount)
	watchCtx, watchCancel := context.WithCancel(context.Background())
	defer watchCancel()
	if err := scan.WatchLibraries(watchCtx); err != nil {
		slog.Warn("library watcher disabled", "err", err)
	}
	handler, stopRecordings := api.New(cfg, st, rdb, scan, tm, art, watchCtx)

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		slog.Info("listening", "addr", cfg.HTTPAddr, "domain", cfg.StevieDomain)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("http server", "err", err)
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	slog.Info("shutting down")
	// Stop the scheduler first so it cannot start new jobs while we finalize.
	watchCancel()
	// Graceful recording stop writes Matroska cues; avoid SIGKILL orphans.
	stopRecordings()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
	playback.DefaultProcs.KillAll()
}
