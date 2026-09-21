package update

import "testing"

func TestParseProxyURL(t *testing.T) {
	valid := []string{"", "http://192.168.31.126:7890", "https://proxy.example.com:8443"}
	for _, value := range valid {
		if _, err := ParseProxyURL(value); err != nil {
			t.Fatalf("ParseProxyURL(%q): %v", value, err)
		}
	}
	invalid := []string{"socks5://192.168.1.2:7890", "http://user:pass@proxy:7890", "http://proxy:7890/path", "not-a-url"}
	for _, value := range invalid {
		if _, err := ParseProxyURL(value); err == nil {
			t.Fatalf("ParseProxyURL(%q) unexpectedly succeeded", value)
		}
	}
}

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
