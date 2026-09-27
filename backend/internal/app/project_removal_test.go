package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/compose-manager/compose-manager/backend/internal/compose"
	"github.com/compose-manager/compose-manager/backend/internal/config"
	dockerapi "github.com/compose-manager/compose-manager/backend/internal/docker"
	"github.com/compose-manager/compose-manager/backend/internal/store"
)

type removalFixture struct {
	service                           *Service
	root, dir, file, image, container string
	containers                        []dockerapi.RemovalContainer
	deleted                           []string
	failRemoval                       bool
}

func newRemovalFixture(t *testing.T) *removalFixture {
	t.Helper()
	base := t.TempDir()
	root := filepath.Join(base, "docker")
	dir := filepath.Join(root, "sample")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "compose.yaml")
	if err := os.WriteFile(file, []byte("name: sample\nservices:\n  web:\n    image: nginx:latest\n"), 0600); err != nil {
		t.Fatal(err)
	}
	f := &removalFixture{root: root, dir: dir, file: file, image: "sha256:" + strings.Repeat("b", 64), container: strings.Repeat("a", 64)}
	f.containers = []dockerapi.RemovalContainer{{ID: f.container, Names: []string{"/sample-web"}, Image: "nginx:latest", ImageID: f.image, State: "exited", Labels: map[string]string{"com.docker.compose.project": "sample", "com.docker.compose.project.working_dir": dir}}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodDelete {
			if r.URL.Query().Get("force") == "true" {
				t.Error("must not force deletion")
			}
			if f.failRemoval {
				http.Error(w, "busy", 409)
				return
			}
			f.deleted = append(f.deleted, r.URL.Path)
			if strings.Contains(r.URL.Path, "/containers/") {
				f.containers = f.containers[1:]
			}
			w.WriteHeader(204)
			return
		}
		switch r.URL.Path {
		case "/v1.43/containers/json":
			json.NewEncoder(w).Encode(f.containers)
		case "/v1.43/images/json":
			json.NewEncoder(w).Encode([]map[string]any{{"Id": f.image, "RepoTags": []string{"nginx:latest"}, "Size": 123}})
		case "/v1.43/volumes":
			json.NewEncoder(w).Encode(map[string]any{"Volumes": []dockerapi.RemovalVolume{}})
		case "/v1.43/networks":
			w.Write([]byte("[]"))
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.Error(w, "unexpected", 500)
		}
	}))
	t.Cleanup(server.Close)
	engine, err := dockerapi.New(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(filepath.Join(base, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	guard, err := compose.NewPathGuard([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	f.service = New(config.Config{DataDir: filepath.Join(base, "state"), BackupDir: filepath.Join(base, "backups")}, engine, guard, compose.NewRunner(time.Second), nil, db)
	return f
}

func TestRemovalConfirmationAndStalePlan(t *testing.T) {
	for _, mode := range []string{"wrong-name", "fake-token", "changed-file", "new-container", "busy"} {
		t.Run(mode, func(t *testing.T) {
			f := newRemovalFixture(t)
			ctx := context.Background()
			plan, err := f.service.PreviewProjectRemoval(ctx, "sample")
			if err != nil {
				t.Fatal(err)
			}
			request := RemovalRequest{Token: plan.Token, Name: plan.Name, Directory: true, Images: true}
			switch mode {
			case "wrong-name":
				request.Name = "docker"
			case "fake-token":
				request.Token = "fake"
			case "changed-file":
				os.WriteFile(f.file, []byte("name: sample\nservices: {}\n"), 0600)
			case "new-container":
				f.containers = append(f.containers, dockerapi.RemovalContainer{ID: strings.Repeat("c", 64), Labels: map[string]string{"com.docker.compose.project": "sample"}})
			case "busy":
				f.service.activeUpdates["sample"] = "active"
			}
			if _, err := f.service.DeleteProject(ctx, "sample", request); err == nil {
				t.Fatal("unsafe request accepted")
			}
			if len(f.deleted) != 0 {
				t.Fatal("mutation occurred before validation")
			}
			if _, err := os.Stat(f.file); err != nil {
				t.Fatal("file removed on rejection")
			}
		})
	}
}

func TestRemovalDirectoryAndImageWithBackup(t *testing.T) {
	f := newRemovalFixture(t)
	ctx := context.Background()
	os.WriteFile(filepath.Join(f.dir, "app-data.txt"), []byte("test data"), 0600)
	plan, err := f.service.PreviewProjectRemoval(ctx, "sample")
	if err != nil {
		t.Fatal(err)
	}
	result, err := f.service.DeleteProject(ctx, "sample", RemovalRequest{Token: plan.Token, Name: plan.Name, Directory: true, Images: true})
	if err != nil || len(result.Errors) != 0 {
		t.Fatal(err, result)
	}
	if _, err := os.Stat(f.dir); !os.IsNotExist(err) {
		t.Fatal("project directory remains")
	}
	if _, err := os.Stat(filepath.Join(result.Backup, "compose.yaml")); err != nil {
		t.Fatal("missing backup", err)
	}
	if len(f.deleted) != 2 {
		t.Fatal("expected exact container and image deletion", f.deleted)
	}
	if _, err := os.Stat(f.root); err != nil {
		t.Fatal("root was removed")
	}
}

func TestRemovalPreservesSharedImageAndData(t *testing.T) {
	f := newRemovalFixture(t)
	ctx := context.Background()
	f.containers = append(f.containers, dockerapi.RemovalContainer{ID: strings.Repeat("c", 64), ImageID: f.image, State: "exited", Labels: map[string]string{"com.docker.compose.project": "other"}, Mounts: []dockerapi.RemovalMount{{Type: "bind", Source: f.dir}}})
	plan, err := f.service.PreviewProjectRemoval(ctx, "sample")
	if err != nil {
		t.Fatal(err)
	}
	if plan.DirectoryBlocked == "" {
		t.Fatal("shared directory not blocked")
	}
	result, err := f.service.DeleteProject(ctx, "sample", RemovalRequest{Token: plan.Token, Name: plan.Name, Images: true})
	if err != nil || len(result.Errors) > 0 {
		t.Fatal(err, result)
	}
	if len(f.deleted) != 1 {
		t.Fatal("shared image was deleted", f.deleted)
	}
	if _, err := os.Stat(f.dir); err != nil {
		t.Fatal("unselected directory removed")
	}
}

func TestRemovalRootFileOnly(t *testing.T) {
	f := newRemovalFixture(t)
	ctx := context.Background()
	os.Remove(f.file)
	f.file = filepath.Join(f.root, "compose.yaml")
	os.WriteFile(f.file, []byte("name: sample\nservices: {}\n"), 0600)
	f.containers = nil
	plan, err := f.service.PreviewProjectRemoval(ctx, "sample")
	if err != nil {
		t.Fatal(err)
	}
	if plan.DirectoryBlocked == "" {
		t.Fatal("root deletion allowed")
	}
	if _, err := f.service.DeleteProject(ctx, "sample", RemovalRequest{Token: plan.Token, Name: plan.Name, Directory: true}); err == nil {
		t.Fatal("root deletion accepted")
	}
	plan, err = f.service.PreviewProjectRemoval(ctx, "sample")
	if err != nil {
		t.Fatal(err)
	}
	result, err := f.service.DeleteProject(ctx, "sample", RemovalRequest{Token: plan.Token, Name: plan.Name})
	if err != nil || len(result.Errors) > 0 {
		t.Fatal(err, result)
	}
	if _, err := os.Stat(f.root); err != nil {
		t.Fatal("root removed")
	}
}

func TestRemovalStopsAfterContainerFailure(t *testing.T) {
	f := newRemovalFixture(t)
	f.failRemoval = true
	ctx := context.Background()
	plan, err := f.service.PreviewProjectRemoval(ctx, "sample")
	if err != nil {
		t.Fatal(err)
	}
	result, err := f.service.DeleteProject(ctx, "sample", RemovalRequest{Token: plan.Token, Name: plan.Name, Directory: true, Images: true})
	if err != nil || len(result.Errors) == 0 {
		t.Fatal("missing partial failure", err, result)
	}
	if _, err := os.Stat(f.file); err != nil {
		t.Fatal("file deleted after Docker failed")
	}
}

func TestRemovalAmbiguousAndUnreadableConfig(t *testing.T) {
	for _, content := range []string{"name: sample\nservices: {}\n", "not: [valid"} {
		t.Run(content, func(t *testing.T) {
			f := newRemovalFixture(t)
			other := filepath.Join(f.root, "other")
			os.Mkdir(other, 0700)
			os.WriteFile(filepath.Join(other, "compose.yaml"), []byte(content), 0600)
			if _, err := f.service.PreviewProjectRemoval(context.Background(), "sample"); err == nil {
				t.Fatal("incomplete or ambiguous scan accepted")
			}
		})
	}
}

func TestRemovalSharedComposeImage(t *testing.T) {
	f := newRemovalFixture(t)
	ctx := context.Background()
	other := filepath.Join(f.root, "other")
	os.Mkdir(other, 0700)
	os.WriteFile(filepath.Join(other, "compose.yaml"), []byte("name: other\nservices:\n  web:\n    image: nginx:latest\n"), 0600)
	plan, err := f.service.PreviewProjectRemoval(ctx, "sample")
	if err != nil {
		t.Fatal(err)
	}
	result, err := f.service.DeleteProject(ctx, "sample", RemovalRequest{Token: plan.Token, Name: plan.Name, Directory: true, Images: true})
	if err != nil || len(result.Errors) > 0 || len(f.deleted) != 1 {
		t.Fatal("referenced image not preserved", err, result, f.deleted)
	}
}
