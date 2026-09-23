package compose

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPathGuardAllowsComposeInsideRoot(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "stack", "compose.yaml")
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("services: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	guard, err := NewPathGuard([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := guard.ResolveComposeFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if resolved != file {
		t.Fatalf("got %q, want %q", resolved, file)
	}
}

func TestPathGuardRejectsTraversal(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(filepath.Dir(root), "compose.yaml")
	if err := os.WriteFile(outside, []byte("services: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(outside) })
	guard, err := NewPathGuard([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := guard.ResolveComposeFile(filepath.Join(root, "..", "compose.yaml")); err == nil {
		t.Fatal("expected traversal to be rejected")
	}
}

func TestPathGuardRejectsWrongFilename(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "other.yaml")
	if err := os.WriteFile(file, []byte("services: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	guard, _ := NewPathGuard([]string{root})
	if _, err := guard.ResolveComposeFile(file); err == nil {
		t.Fatal("expected unsupported filename to be rejected")
	}
}

func TestPathGuardRejectsSymlinkOutsideRoot(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "compose.yaml"), []byte("services: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "external")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink is unavailable: %v", err)
	}
	guard, _ := NewPathGuard([]string{root})
	if _, err := guard.Resolve(filepath.Join(link, "compose.yaml")); err == nil {
		t.Fatal("expected symlink escape to be rejected")
	}
	if _, err := guard.Resolve(filepath.Join(link, "missing", "nested", "compose.yaml")); err == nil {
		t.Fatal("expected symlink escape with missing tail to be rejected")
	}
}
