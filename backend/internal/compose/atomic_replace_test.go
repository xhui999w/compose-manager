package compose

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAtomicReplaceExistingFile(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source.yaml")
	destination := filepath.Join(dir, "compose.yaml")
	if err := os.WriteFile(source, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := atomicReplace(source, destination); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "new" {
		t.Fatalf("got %q, want new", content)
	}
}
