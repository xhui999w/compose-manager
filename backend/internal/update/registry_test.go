package update

import "testing"

func TestParseReference(t *testing.T) {
	tests := []struct{ input, registry, repository, tag string }{
		{"postgres:17.6", "registry-1.docker.io", "library/postgres", "17.6"},
		{"ghcr.io/acme/app:latest", "ghcr.io", "acme/app", "latest"},
		{"linuxserver/jellyfin", "registry-1.docker.io", "linuxserver/jellyfin", "latest"},
		{"docker.io/library/nginx:stable", "registry-1.docker.io", "library/nginx", "stable"},
	}
	for _, test := range tests {
		registry, repository, tag, err := parseReference(test.input)
		if err != nil || registry != test.registry || repository != test.repository || tag != test.tag {
			t.Fatalf("parseReference(%q) = %q, %q, %q, %v", test.input, registry, repository, tag, err)
		}
	}
}

func TestRegistryEndpointsPreferConfiguredDockerHubMirrors(t *testing.T) {
	endpoints := registryEndpoints("registry-1.docker.io", []string{
		"https://docker.1ms.run/",
		"http://insecure.example.com",
		"https://docker.1ms.run",
	})
	want := []string{"https://docker.1ms.run", "https://registry-1.docker.io"}
	if len(endpoints) != len(want) {
		t.Fatalf("registryEndpoints() = %#v", endpoints)
	}
	for index := range want {
		if endpoints[index] != want[index] {
			t.Fatalf("registryEndpoints()[%d] = %q, want %q", index, endpoints[index], want[index])
		}
	}
}

func TestRegistryEndpointsDoNotApplyDockerHubMirrorsElsewhere(t *testing.T) {
	endpoints := registryEndpoints("ghcr.io", []string{"https://docker.1ms.run"})
	if len(endpoints) != 1 || endpoints[0] != "https://ghcr.io" {
		t.Fatalf("registryEndpoints() = %#v", endpoints)
	}
}
