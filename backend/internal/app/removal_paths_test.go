package app

import (
	"path/filepath"
	"strings"
	"testing"

	dockerapi "github.com/compose-manager/compose-manager/backend/internal/docker"
)

func TestRemovalPathMapperMatchesNASBindAndProtectsOtherMounts(t *testing.T) {
	id := strings.Repeat("a", 64)
	root := filepath.FromSlash("/volume1/docker")
	bind := dockerapi.RemovalContainer{
		ID: id, Labels: map[string]string{"com.docker.compose.project": "compose-manager"},
		Mounts: []dockerapi.RemovalMount{
			{Type: "bind", Source: root, Destination: "/compose"},
			{Type: "bind", Source: "/volume1/docker/compose-manager/data", Destination: "/data"},
		},
	}
	mapper := newRemovalPathMapper([]dockerapi.RemovalContainer{bind}, id[:12])
	if got := mapper.host("/compose/iyuu/compose.yaml"); got != filepath.Join(root, "iyuu", "compose.yaml") {
		t.Fatal(got)
	}
	if got := mapper.host("/data/compose-manager.db"); got != filepath.FromSlash("/volume1/docker/compose-manager/data/compose-manager.db") {
		t.Fatal(got)
	}
	if !mapper.scanRootMount(root, []string{"/compose"}) {
		t.Fatal("manager scan mount not recognized")
	}
	if !pathsOverlap(mapper.host("/compose/iyuu"), "/volume1/docker/iyuu/iyuu") {
		t.Fatal("NAS sibling dependency not detected")
	}
	if mapper.host("/volume1/downloads") != filepath.Clean("/volume1/downloads") {
		t.Fatal("unrelated path remapped")
	}
}
