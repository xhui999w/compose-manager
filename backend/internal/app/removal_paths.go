package app

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/compose-manager/compose-manager/backend/internal/compose"
	dockerapi "github.com/compose-manager/compose-manager/backend/internal/docker"
)

type removalPathMapper struct {
	mounts  []dockerapi.RemovalMount
	project string
}

func newRemovalPathMapper(containers []dockerapi.RemovalContainer, hostname string) removalPathMapper {
	var mapper removalPathMapper
	for _, c := range containers {
		if len(hostname) < 12 || !strings.HasPrefix(c.ID, hostname) {
			continue
		}
		mapper.project = c.Labels["com.docker.compose.project"]
		for _, m := range c.Mounts {
			if m.Type == "bind" {
				mapper.mounts = append(mapper.mounts, m)
			}
		}
		break
	}
	sort.Slice(mapper.mounts, func(i, j int) bool { return len(mapper.mounts[i].Destination) > len(mapper.mounts[j].Destination) })
	return mapper
}

func (m removalPathMapper) host(path string) string {
	path = filepath.Clean(path)
	for _, mount := range m.mounts {
		if compose.PathContains(mount.Destination, path) {
			rel, _ := filepath.Rel(mount.Destination, path)
			return filepath.Join(mount.Source, rel)
		}
	}
	return path
}

func (m removalPathMapper) scanRootMount(source string, roots []string) bool {
	for _, root := range roots {
		if m.host(source) == m.host(root) {
			return true
		}
	}
	return false
}
