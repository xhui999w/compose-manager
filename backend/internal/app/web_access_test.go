package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/compose-manager/compose-manager/backend/internal/access"
	"github.com/compose-manager/compose-manager/backend/internal/config"
	dockerapi "github.com/compose-manager/compose-manager/backend/internal/docker"
	"github.com/compose-manager/compose-manager/backend/internal/model"
	"github.com/compose-manager/compose-manager/backend/internal/store"
)

func TestSaveWebAccessRequiresCurrentProjectPort(t *testing.T) {
	engineServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[{"Id":"container1","Names":["/transmission2"],"State":"running","Labels":{"com.docker.compose.project":"transmission2","com.docker.compose.service":"transmission"},"Ports":[{"IP":"0.0.0.0","PublicPort":9091,"PrivatePort":9091,"Type":"tcp"},{"PublicPort":51413,"PrivatePort":51413,"Type":"udp"}]}]`))
	}))
	defer engineServer.Close()
	docker, err := dockerapi.New(engineServer.URL)
	if err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s := &Service{config: config.Config{NASIP: "192.168.31.126"}, docker: docker, store: db}
	cfg := access.WebConfig{Service: "transmission", PublicPort: 9091, PrivatePort: 9091, Scheme: "http", Path: "/transmission/web/"}
	if err := s.SaveProjectWebAccess(context.Background(), "transmission2", cfg); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveProjectWebAccess(context.Background(), "other-project", cfg); err == nil {
		t.Fatal("accepted another project's port")
	}
	cfg.PublicPort = 51413
	cfg.PrivatePort = 51413
	if err := s.SaveProjectWebAccess(context.Background(), "transmission2", cfg); err == nil {
		t.Fatal("accepted UDP-only port")
	}
	cfg.PublicPort = 2375
	if err := s.SaveProjectWebAccess(context.Background(), "transmission2", cfg); err == nil {
		t.Fatal("accepted unpublished port")
	}
}

func TestEnrichmentDoesNotGuessWebPort(t *testing.T) {
	p := model.Project{Containers: []model.Container{{State: "running", Ports: []model.Port{{PublicPort: 51413, PrivatePort: 51413, Type: "tcp"}, {PublicPort: 9091, PrivatePort: 9091, Type: "tcp"}}}}}
	enrichProject(&p, "192.168.31.126", "http")
	if p.InternalURL != "" {
		t.Fatal("must not assume first TCP port is a website", p.InternalURL)
	}
}
