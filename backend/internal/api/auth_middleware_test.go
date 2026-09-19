package api

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	authsvc "github.com/compose-manager/compose-manager/backend/internal/auth"
	"github.com/compose-manager/compose-manager/backend/internal/store"
)

func TestAuthenticationMiddleware(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	database, err := store.Open(filepath.Join(dir, "compose-manager.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	manager, err := authsvc.New(ctx, database, authsvc.Config{DataDir: dir, SetupToken: "setup-secret-1234567890", SessionTTL: time.Hour}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	result, err := manager.Setup(ctx, "setup-secret-1234567890", "admin", "a secure password 123")
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{auth: manager}
	handler := server.authenticate(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))

	request := httptest.NewRequest(http.MethodGet, "/api/v1/settings", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthenticated request rejection, got %d", response.Code)
	}

	request = httptest.NewRequest(http.MethodPost, "/api/v1/settings", nil)
	request.AddCookie(&http.Cookie{Name: manager.CookieName(), Value: result.SessionToken})
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("expected missing CSRF rejection, got %d", response.Code)
	}

	request = httptest.NewRequest(http.MethodPost, "/api/v1/settings", nil)
	request.AddCookie(&http.Cookie{Name: manager.CookieName(), Value: result.SessionToken})
	request.Header.Set("X-CSRF-Token", result.CSRFToken)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("expected authenticated request success, got %d", response.Code)
	}
}
