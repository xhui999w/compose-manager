package compose

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverySkipsSnapshotsButKeepsRealProjects(t *testing.T) {
	root := t.TempDir()
	makeFile := func(directory string) string {
		t.Helper()
		path := filepath.Join(root, directory, "docker-compose.yaml")
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("services:\n  silnav:\n    image: ghcr.io/example/silnav:latest\n"), 0600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	current := makeFile("silnav")
	snapshot := makeFile(filepath.Join("_silnav-preswitch-20260915-1215", "verify"))
	backup := makeFile(filepath.Join("_openlist-backup-20260913-1528", "verify"))
	makeFile(filepath.Join("backups", "verify"))
	makeFile(filepath.Join(".snapshot", "verify"))
	makeFile("backup-helper")
	makeFile("_legitimate")
	guard, err := NewPathGuard([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	discovery := NewDiscovery(guard)
	projects, err := discovery.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"silnav": true, "backup-helper": true, "_legitimate": true}
	if len(projects) != len(want) {
		t.Fatalf("discovered %d projects, want %d: %+v", len(projects), len(want), projects)
	}
	for _, project := range projects {
		if !want[project.Name] {
			t.Fatalf("backup was discovered as a project: %+v", project)
		}
	}
	if discovery.ExcludedFromProjects(current) || !discovery.ExcludedFromProjects(snapshot) || !discovery.ExcludedFromProjects(backup) {
		t.Fatal("project/deletion path classification differs from the scanner")
	}
	// Backups still participate in the conservative deletion reference inventory.
	inventory, err := discovery.RemovalInventory(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	seenBackup := false
	for _, file := range inventory {
		if file.File == snapshot {
			seenBackup = true
		}
	}
	if !seenBackup {
		t.Fatal("backup reference disappeared from deletion safety inventory")
	}
}

func TestExplicitSnapshotRootCanStillBeManaged(t *testing.T) {
	root := t.TempDir()
	snapshotRoot := filepath.Join(root, "_app-backup-20260915-1215")
	file := filepath.Join(snapshotRoot, "compose.yaml")
	if err := os.MkdirAll(snapshotRoot, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("services: {}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	guard, err := NewPathGuard([]string{root, snapshotRoot})
	if err != nil {
		t.Fatal(err)
	}
	discovery := NewDiscovery(guard)
	if discovery.ExcludedFromProjects(file) {
		t.Fatal("an explicitly configured root must remain manageable")
	}
	projects, err := discovery.Scan(context.Background())
	if err != nil || len(projects) != 1 {
		t.Fatalf("explicit root discovery failed: %+v, %v", projects, err)
	}
}
