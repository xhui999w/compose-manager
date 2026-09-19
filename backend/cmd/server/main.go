package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/compose-manager/compose-manager/backend/internal/api"
	"github.com/compose-manager/compose-manager/backend/internal/app"
	"github.com/compose-manager/compose-manager/backend/internal/auth"
	"github.com/compose-manager/compose-manager/backend/internal/compose"
	"github.com/compose-manager/compose-manager/backend/internal/config"
	dockerapi "github.com/compose-manager/compose-manager/backend/internal/docker"
	"github.com/compose-manager/compose-manager/backend/internal/store"
	webassets "github.com/compose-manager/compose-manager/backend/web"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	cfg := config.Load()
	if err := os.MkdirAll(cfg.BackupDir, 0o750); err != nil {
		logger.Error("create backup directory", "error", err)
		os.Exit(1)
	}
	database, err := store.Open(cfg.DatabasePath())
	if err != nil {
		logger.Error("open database", "error", err)
		os.Exit(1)
	}
	defer database.Close()
	authManager, err := auth.New(context.Background(), database, auth.Config{DataDir: cfg.DataDir, SessionTTL: cfg.SessionTTL, SecureCookie: cfg.SecureCookie, SetupToken: cfg.SetupToken}, logger)
	if err != nil {
		logger.Error("configure authentication", "error", err)
		os.Exit(1)
	}
	if persisted, settingsErr := database.Settings(context.Background()); settingsErr == nil {
		cfg = config.ApplySettings(cfg, persisted)
	} else {
		logger.Warn("load persisted settings", "error", settingsErr)
	}
	guard, err := compose.NewPathGuard(cfg.ComposeRoots)
	if err != nil {
		logger.Error("configure Compose roots", "error", err)
		os.Exit(1)
	}
	engine, err := dockerapi.New(cfg.DockerHost)
	if err != nil {
		logger.Error("configure Docker client", "error", err)
		os.Exit(1)
	}
	runner := compose.NewRunner(cfg.OperationTimeout)
	editor := compose.NewEditor(guard, runner, database, cfg.BackupDir)
	service := app.New(cfg, engine, guard, runner, editor, database)
	server := &http.Server{Addr: cfg.ListenAddr, Handler: api.New(service, authManager, logger, webassets.Handler()), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 3 * time.Minute, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 1 << 20}

	go func() {
		logger.Info("Compose Manager listening", "address", cfg.ListenAddr, "demo", cfg.DemoMode)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server stopped", "error", err)
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		logger.Error("graceful shutdown", "error", err)
	}
}
