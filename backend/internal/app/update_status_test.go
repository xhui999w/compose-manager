package app

import "testing"

func TestCanonicalImageRef(t *testing.T) {
	tests := map[string]string{
		"nginx":                         "docker.io/library/nginx:latest",
		"docker.io/nginx:latest":        "docker.io/library/nginx:latest",
		"postgres:17.6":                 "docker.io/library/postgres:17.6",
		"linuxserver/jellyfin":          "docker.io/linuxserver/jellyfin:latest",
		"ghcr.io/example/app:stable":    "ghcr.io/example/app:stable",
		"localhost:5000/example/app:v1": "localhost:5000/example/app:v1",
	}
	for input, want := range tests {
		if got := canonicalImageRef(input); got != want {
			t.Errorf("canonicalImageRef(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestSummarizeProjectUpdates(t *testing.T) {
	references := map[string]struct{}{
		canonicalImageRef("nginx:latest"):  {},
		canonicalImageRef("postgres:17.6"): {},
	}
	tests := []struct {
		name       string
		statuses   map[string]string
		wantStatus string
		wantCount  int
	}{
		{name: "waiting before automatic check", statuses: map[string]string{}, wantStatus: "unknown"},
		{name: "all current", statuses: map[string]string{canonicalImageRef("nginx"): "current", canonicalImageRef("postgres:17.6"): "current"}, wantStatus: "current"},
		{name: "one available", statuses: map[string]string{canonicalImageRef("nginx"): "available", canonicalImageRef("postgres:17.6"): "current"}, wantStatus: "available", wantCount: 1},
		{name: "partial registry failure", statuses: map[string]string{canonicalImageRef("nginx"): "current", canonicalImageRef("postgres:17.6"): "unknown"}, wantStatus: "unknown"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			status, count := summarizeProjectUpdates(references, test.statuses)
			if status != test.wantStatus || count != test.wantCount {
				t.Fatalf("summarizeProjectUpdates() = %q, %d, want %q, %d", status, count, test.wantStatus, test.wantCount)
			}
		})
	}
}
