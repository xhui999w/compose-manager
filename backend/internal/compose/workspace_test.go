package compose

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

type workspaceRunnerStub struct {
	actions []string
}

func (s *workspaceRunnerStub) Run(_ context.Context, _, _, action, _ string, _ int) (string, error) {
	s.actions = append(s.actions, action)
	return "ok", nil
}

func TestWorkspaceCreatesValidatedProject(t *testing.T) {
	root := t.TempDir()
	backup := t.TempDir()
	guard, err := NewPathGuard([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	runner := &workspaceRunnerStub{}
	workspace := NewWorkspace(guard, runner, backup)
	content := "services:\n  web:\n    image: nginx:alpine\n"
	created, err := workspace.CreateProject(context.Background(), 0, "apps/demo", "demo", content, true)
	if err != nil {
		t.Fatal(err)
	}
	if created.File != "apps/demo/compose.yaml" {
		t.Fatalf("unexpected file %q", created.File)
	}
	data, err := os.ReadFile(filepath.Join(root, "apps", "demo", "compose.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != content {
		t.Fatalf("unexpected content %q", string(data))
	}
	if len(runner.actions) != 2 || runner.actions[0] != "config" || runner.actions[1] != "apply" {
		t.Fatalf("unexpected actions %#v", runner.actions)
	}
}

func TestWorkspaceRejectsTraversal(t *testing.T) {
	root := t.TempDir()
	guard, _ := NewPathGuard([]string{root})
	workspace := NewWorkspace(guard, &workspaceRunnerStub{}, t.TempDir())
	if _, err := workspace.List(0, "../"); err == nil {
		t.Fatal("expected traversal to be rejected")
	}
}

func TestWorkspaceSaveCreatesBackupAndChecksSHA(t *testing.T) {
	root := t.TempDir()
	backup := t.TempDir()
	path := filepath.Join(root, "demo", ".env")
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("PORT=8080\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	guard, _ := NewPathGuard([]string{root})
	workspace := NewWorkspace(guard, &workspaceRunnerStub{}, backup)
	current, err := workspace.ReadFile(0, "demo/.env")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := workspace.SaveFile(context.Background(), 0, current.Path, "PORT=9090\n", "wrong"); err != ErrWorkspaceConflict {
		t.Fatalf("expected conflict, got %v", err)
	}
	if _, err := workspace.SaveFile(context.Background(), 0, current.Path, "PORT=9090\n", current.SHA256); err != nil {
		t.Fatal(err)
	}
	backups, err := filepath.Glob(filepath.Join(backup, "workspace", "root-0", "demo", ".env.*.bak"))
	if err != nil || len(backups) != 1 {
		t.Fatalf("expected one backup, got %#v, %v", backups, err)
	}
}
