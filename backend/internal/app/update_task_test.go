package app

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/compose-manager/compose-manager/backend/internal/config"
	"github.com/compose-manager/compose-manager/backend/internal/store"
)

func TestDemoUpdateTaskLifecycle(t *testing.T) {
	database, err := store.Open(filepath.Join(t.TempDir(), "compose-manager.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	service := New(config.Config{DemoMode: true, NASIP: "127.0.0.1", OperationTimeout: time.Second}, nil, nil, nil, nil, database)

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

func TestSanitizeTaskLine(t *testing.T) {
	got := sanitizeTaskLine("\x1b[32mPulling\x1b[0m token=secret-value")
	if got != "Pulling token=[已隐藏]" {
		t.Fatalf("unexpected sanitized output %q", got)
	}
}
