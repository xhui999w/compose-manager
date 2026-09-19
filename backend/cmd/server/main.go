package main

import (
	"context"
	"errors"
	"fmt"
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
	// 口令哈希生成开关：让用户不必把明文口令写进 .env，也无需额外安装工具。
	if handled := hashPasswordFlag(os.Args[1:]); handled {
		return
	}
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
	authenticator := newAuthenticator(cfg, logger)
	server := &http.Server{Addr: cfg.ListenAddr, Handler: api.New(service, logger, webassets.Handler(), authenticator), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 3 * time.Minute, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 1 << 20}

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

// hashPasswordFlag 处理 -hash-password <口令>，输出 bcrypt 哈希后直接退出。
//
// 返回值表示「已经处理完参数，不应继续启动服务」。
func hashPasswordFlag(args []string) bool {
	if len(args) == 0 || (args[0] != "-hash-password" && args[0] != "--hash-password") {
		return false
	}
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "用法: compose-manager -hash-password <口令>")
		os.Exit(2)
	}
	hash, err := auth.HashPassword(args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, "生成口令哈希失败:", err)
		os.Exit(1)
	}
	fmt.Println(hash)
	return true
}

// newAuthenticator 装配登录认证。
//
// 会话密钥优先取 CM_SESSION_SECRET；未配置时读取或自动生成落盘密钥，使容器重启后登录态仍然有效。
// 口令与哈希都未配置时返回「未启用」的实例，此时面板不鉴权（保持与既有部署兼容）。
func newAuthenticator(cfg config.Config, logger *slog.Logger) *auth.Authenticator {
	source := "CM_SESSION_SECRET"
	secret, err := auth.ParseSecret(cfg.Auth.SessionSecret)
	if err != nil {
		logger.Error("configure session secret", "error", err)
		os.Exit(1)
	}
	if len(secret) == 0 {
		source = cfg.Auth.SessionSecretPath
		var warning error
		secret, _, warning = auth.LoadOrCreateSecret(cfg.Auth.SessionSecretPath)
		if warning != nil {
			logger.Warn("session secret", "error", warning)
		}
	}
	authenticator, err := auth.New(auth.Config{
		Username:     cfg.Auth.Username,
		Password:     cfg.Auth.Password,
		PasswordHash: cfg.Auth.PasswordHash,
		Secret:       secret,
		TTL:          cfg.Auth.SessionTTL,
		CookieSecure: cfg.Auth.CookieSecure,
	})
	if err != nil {
		logger.Error("configure authentication", "error", err)
		os.Exit(1)
	}
	if authenticator.Enabled() {
		logger.Info("login authentication enabled",
			"user", authenticator.Username(),
			"ttl", authenticator.TTL().String(),
			"secretSource", source,
			"cookieSecure", cfg.Auth.CookieSecure,
		)
	} else {
		logger.Warn("login authentication disabled: set CM_AUTH_PASSWORD or CM_AUTH_PASSWORD_HASH to require login")
	}
	return authenticator
}
