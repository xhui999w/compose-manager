package compose

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRemovalDirectoryProtectsRootsAndSymlinks(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "docker")
	child := filepath.Join(root, "app")
	nested := filepath.Join(child, "nested")
	os.MkdirAll(nested, 0700)
	guard, err := NewPathGuard([]string{root, nested})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{root, child, nested, base, filepath.Join(root, "..", "outside")} {
		if _, err := guard.RemovalDirectory(path); err == nil {
			t.Fatal("unsafe directory accepted", path)
		}
	}
	guard, err = NewPathGuard([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := guard.RemovalDirectory(child); err != nil {
		t.Fatal("valid subdirectory rejected", err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(child, link); err != nil {
		t.Skip("symlink privileges unavailable")
	}
	if _, err := guard.RemovalDirectory(link); err == nil {
		t.Fatal("symlink accepted")
	}
}
