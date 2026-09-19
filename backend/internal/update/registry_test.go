package update

import "testing"

func TestParseReference(t *testing.T) {
	tests := []struct{ input, registry, repository, tag string }{
		{"postgres:17.6", "registry-1.docker.io", "library/postgres", "17.6"},
		{"ghcr.io/acme/app:latest", "ghcr.io", "acme/app", "latest"},
		{"linuxserver/jellyfin", "registry-1.docker.io", "linuxserver/jellyfin", "latest"},
	}
	for _, test := range tests {
		registry, repository, tag, err := parseReference(test.input)
		if err != nil || registry != test.registry || repository != test.repository || tag != test.tag {
			t.Fatalf("parseReference(%q) = %q, %q, %q, %v", test.input, registry, repository, tag, err)
		}
	}
}
