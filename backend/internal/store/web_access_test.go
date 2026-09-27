package store

import (
	"context"
	"github.com/compose-manager/compose-manager/backend/internal/access"
	"path/filepath"
	"testing"
)

func TestWebAccessSurvivesReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "test.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg := access.WebConfig{Service: "transmission", PublicPort: 9091, PrivatePort: 9091, Scheme: "http", Path: "/transmission/web/"}
	if err = s.PutWebAccess(ctx, "transmission2", cfg); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	values, err := s.WebAccess(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if values["transmission2"] != cfg {
		t.Fatal(values)
	}
	cfg.PublicPort = 19091
	if err = s.PutWebAccess(ctx, "transmission2", cfg); err != nil {
		t.Fatal(err)
	}
	values, err = s.WebAccess(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 1 || values["transmission2"] != cfg {
		t.Fatal(values)
	}
}
