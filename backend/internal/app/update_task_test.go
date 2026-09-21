package app

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/compose-manager/compose-manager/backend/internal/compose"
	"github.com/compose-manager/compose-manager/backend/internal/config"
	"github.com/compose-manager/compose-manager/backend/internal/store"
)

func TestDemoUpdateTaskLifecycle(t *testing.T) {
	database, err := store.Open(filepath.Join(t.TempDir(), "compose-manager.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	service := New(config.Config{DemoMode: true, NASIP: "127.0.0.1", OperationTimeout: time.Second}, nil, nil, compose.NewRunner(time.Second), nil, database)

	task, err := service.StartUpdate(context.Background(), "immich", "server")
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != "queued" {
		t.Fatalf("expected queued task, got %q", task.Status)
	}
	if _, err := service.StartUpdate(context.Background(), "immich", "database"); err == nil {
		t.Fatal("expected a second update for the same project to be rejected")
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		tasks := service.UpdateTasks()
		if len(tasks) == 1 && tasks[0].Status == "success" {
			if tasks[0].Progress != 100 || tasks[0].FinishedAt == nil {
				t.Fatalf("completed task has incomplete state: %#v", tasks[0])
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("update task did not complete")
}

func TestPutSettingsSupportsOptionalProxyAndTimeout(t *testing.T) {
	database, err := store.Open(filepath.Join(t.TempDir(), "compose-manager.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	runner := compose.NewRunner(15 * time.Minute)
	service := New(config.Config{DemoMode: true, OperationTimeout: 15 * time.Minute}, nil, nil, runner, nil, database)

	if err := service.PutSettings(context.Background(), map[string]any{"density": "compact"}); err != nil {
		t.Fatalf("partial settings update failed: %v", err)
	}
	if err := service.PutSettings(context.Background(), map[string]any{
		"proxyURL":                "http://192.168.31.126:7890",
		"operationTimeoutMinutes": float64(20),
	}); err != nil {
		t.Fatal(err)
	}
	if runner.Timeout() != 20*time.Minute {
		t.Fatalf("expected a 20 minute timeout, got %s", runner.Timeout())
	}
	values, err := service.Settings(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if values["proxyURL"] != "http://192.168.31.126:7890" || values["operationTimeoutMinutes"] != float64(20) {
		t.Fatalf("unexpected persisted settings: %#v", values)
	}
}

func TestPutSettingsRejectsInvalidProxyAndTimeout(t *testing.T) {
	database, err := store.Open(filepath.Join(t.TempDir(), "compose-manager.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	service := New(config.Config{DemoMode: true, OperationTimeout: 15 * time.Minute}, nil, nil, compose.NewRunner(15*time.Minute), nil, database)

	if err := service.PutSettings(context.Background(), map[string]any{"proxyURL": "socks5://127.0.0.1:7890"}); err == nil {
		t.Fatal("expected an invalid proxy to be rejected")
	}
	if err := service.PutSettings(context.Background(), map[string]any{"operationTimeoutMinutes": float64(1)}); err == nil {
		t.Fatal("expected an invalid timeout to be rejected")
	}
}

func TestSanitizeTaskLine(t *testing.T) {
	got := sanitizeTaskLine("\x1b[32mPulling\x1b[0m token=secret-value")
	if got != "Pulling token=[已隐藏]" {
		t.Fatalf("unexpected sanitized output %q", got)
	}
}
