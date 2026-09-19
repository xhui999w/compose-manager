package auth

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/compose-manager/compose-manager/backend/internal/store"
)

func TestManagerSetupLoginAndLogout(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	database, err := store.Open(filepath.Join(dir, "compose-manager.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	manager, err := New(ctx, database, Config{DataDir: dir, SetupToken: "setup-secret-1234567890", SessionTTL: time.Hour}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	required, err := manager.SetupRequired(ctx)
	if err != nil || !required {
		t.Fatalf("expected setup required: required=%t err=%v", required, err)
	}
	if _, err := manager.Setup(ctx, "wrong-token", "admin", "a secure password 123"); !errors.Is(err, ErrInvalidSetupToken) {
		t.Fatalf("expected invalid setup token, got %v", err)
	}
	result, err := manager.Setup(ctx, "setup-secret-1234567890", "admin", "a secure password 123")
	if err != nil {
		t.Fatal(err)
	}
	if result.Username != "admin" || result.SessionToken == "" || result.CSRFToken == "" {
		t.Fatalf("incomplete setup result: %+v", result)
	}
	session, err := manager.Authenticate(ctx, result.SessionToken)
	if err != nil {
		t.Fatal(err)
	}
	if session.Username != "admin" || !manager.ValidCSRF(session, result.CSRFToken) || manager.ValidCSRF(session, "wrong") {
		t.Fatal("session identity or CSRF validation failed")
	}
	if _, err := manager.Login(ctx, "admin", "wrong password"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("expected invalid credentials, got %v", err)
	}
	login, err := manager.Login(ctx, "ADMIN", "a secure password 123")
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Logout(ctx, login.SessionToken); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Authenticate(ctx, login.SessionToken); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expected logged out session to be missing, got %v", err)
	}
}

func TestLimiter(t *testing.T) {
	limiter := NewLimiter(2, time.Minute)
	if allowed, _ := limiter.Allow("client"); !allowed {
		t.Fatal("new client should be allowed")
	}
	limiter.Failure("client")
	limiter.Failure("client")
	if allowed, retry := limiter.Allow("client"); allowed || retry <= 0 {
		t.Fatalf("expected client to be limited: allowed=%t retry=%s", allowed, retry)
	}
	limiter.Success("client")
	if allowed, _ := limiter.Allow("client"); !allowed {
		t.Fatal("successful login should clear limit")
	}
}
