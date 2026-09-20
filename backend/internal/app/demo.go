package app

import (
	"fmt"
	"time"

	"github.com/compose-manager/compose-manager/backend/internal/model"
)

func demoProjects(nasIP string) []model.Project {
	type seed struct {
		name, status, update string
		total                int
		cpu                  float64
		memory               uint64
		port                 uint16
	}
	seeds := []seed{
		{"media-stack", "running", "available", 5, 2.1, 512 << 20, 8096},
		{"home-assistant", "running", "current", 3, 1.6, 728 << 20, 8123},
		{"immich", "running", "available", 4, 3.8, 1200 << 20, 2283},
		{"postgres-prod", "running", "current", 1, .6, 256 << 20, 5432},
		{"uptime-kuma", "running", "current", 1, .3, 64 << 20, 3001},
		{"cloudflared", "stopped", "available", 1, 0, 0, 0},
		{"paperless", "running", "current", 2, 1.1, 384 << 20, 8000},
		{"redis-cache", "degraded", "current", 1, 0, 0, 6379},
	}
	result := make([]model.Project, 0, len(seeds))
	for _, item := range seeds {
		containers := make([]model.Container, 0, item.total)
		for index := 0; index < item.total; index++ {
			state := "running"
			if item.status == "stopped" || item.status == "degraded" {
				state = "exited"
			}
			ports := []model.Port{}
			if index == 0 && item.port > 0 {
				ports = append(ports, model.Port{PrivatePort: 80, PublicPort: item.port, Type: "tcp"})
			}
			containers = append(containers, model.Container{ID: fmt.Sprintf("demo-%s-%d", item.name, index), Name: fmt.Sprintf("%s-%d", item.name, index+1), Image: fmt.Sprintf("ghcr.io/example/%s:latest", item.name), ImageID: fmt.Sprintf("sha256:%012d", index+1), State: state, Status: state, Project: item.name, Service: "app", CPUPercent: item.cpu / float64(item.total), MemoryBytes: item.memory / uint64(item.total), MemoryLimit: 2 << 30, CreatedAt: time.Now().Add(-24 * time.Hour), Ports: ports})
		}
		url := ""
		if item.port > 0 {
			url = fmt.Sprintf("http://%s:%d", nasIP, item.port)
		}
		result = append(result, model.Project{Key: item.name, Name: item.name, Status: item.status, Healthy: map[bool]int{true: item.total, false: 0}[item.status == "running"], Total: item.total, CPUPercent: item.cpu, MemoryBytes: item.memory, UpdateStatus: item.update, UpdateCount: map[bool]int{true: 1, false: 0}[item.update == "available"], InternalURL: url, ConfigFile: "/compose/" + item.name + "/compose.yaml", WorkingDir: "/compose/" + item.name, DiscoverySource: "demo", Editable: true, Containers: containers})
	}
	return result
}

func demoImages() []model.ImageReference {
	now := time.Now().Add(-72 * time.Hour)
	return []model.ImageReference{
		{ID: "sha256:1", Repository: "postgres", Tag: "17.6", Digest: "sha256:abc", Size: 421 << 20, CreatedAt: now, RunningReferences: []string{"postgres-prod-1"}, ComposeReferences: []string{"postgres-prod"}, UpdateStatus: "current", Category: "in-use"},
		{ID: "sha256:2", Repository: "redis", Tag: "7.4", Digest: "sha256:def", Size: 118 << 20, CreatedAt: now, ComposeReferences: []string{"redis-cache"}, UpdateStatus: "available", Category: "compose-referenced"},
		{ID: "sha256:3", Repository: "nginx", Tag: "1.26", Digest: "sha256:ghi", Size: 192 << 20, CreatedAt: now, UpdateStatus: "unknown", Category: "unused", Reclaimable: 192 << 20},
		{ID: "sha256:4", Repository: "<none>", Tag: "<none>", Size: 87 << 20, CreatedAt: now, UpdateStatus: "unknown", Category: "dangling", Reclaimable: 87 << 20},
	}
}
