package access

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/compose-manager/compose-manager/backend/internal/model"
)

func TestWebConfigValidation(t *testing.T) {
	for _, path := range []string{"/", "/transmission/web/", "/admin?view=1#home"} {
		if err := ValidateWebConfig(WebConfig{PublicPort: 9091, PrivatePort: 9091, Scheme: "http", Path: path}); err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range []string{"", "//evil.example/", "https://evil.example/", "/\\evil", "/a\r\nb"} {
		if err := ValidateWebConfig(WebConfig{PublicPort: 9091, PrivatePort: 9091, Scheme: "http", Path: path}); err == nil {
			t.Fatalf("accepted unsafe path %q", path)
		}
	}
	if got := WebURL("::1", WebConfig{PublicPort: 9091, PrivatePort: 9091, Scheme: "http", Path: "/transmission/web/?a=1"}); got != "http://[::1]:9091/transmission/web/?a=1" {
		t.Fatal(got)
	}
}

func TestPublishedPortSelection(t *testing.T) {
	containers := []model.Container{{Name: "transmission2", Service: "transmission", State: "running", Ports: []model.Port{
		{IP: "0.0.0.0", PublicPort: 51413, PrivatePort: 51413, Type: "udp"},
		{IP: "0.0.0.0", PublicPort: 51413, PrivatePort: 51413, Type: "tcp"},
		{IP: "0.0.0.0", PublicPort: 19091, PrivatePort: 9091, Type: "tcp"},
		{IP: "::", PublicPort: 19091, PrivatePort: 9091, Type: "tcp"},
		{IP: "127.0.0.1", PublicPort: 9999, PrivatePort: 9999, Type: "tcp"},
		{PublicPort: 0, PrivatePort: 80, Type: "tcp"},
	}}}
	candidates := Candidates(containers, "192.168.31.126", "http")
	if len(candidates) != 2 {
		t.Fatalf("expected two unique published TCP ports, got %#v", candidates)
	}
	cfg := WebConfig{Service: "transmission", PublicPort: 19091, PrivatePort: 9091, Scheme: "http", Path: "/transmission/web/"}
	if got := ResolveSaved(containers, "192.168.31.126", cfg); got != "http://192.168.31.126:19091/transmission/web/" {
		t.Fatal(got)
	}
	cfg.PublicPort = 9091
	if got := ResolveSaved(containers, "192.168.31.126", cfg); got != "" {
		t.Fatal("stale port must not resolve", got)
	}
}

func TestProbeSeparatesWebFromAPIAndRedirects(t *testing.T) {
	var remoteCalls atomic.Int32
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { remoteCalls.Add(1); w.Write([]byte("never")) }))
	defer remote.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"ok":true}`))
		case "/auth":
			w.WriteHeader(401)
		case "/external":
			http.Redirect(w, r, remote.URL, 302)
		case "/":
			http.Redirect(w, r, "/transmission/web/", 301)
		case "/transmission/web/":
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte("<!doctype html><html>Transmission</html>"))
		default:
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	client := &http.Client{Timeout: time.Second, CheckRedirect: sameOriginRedirect}
	for path, want := range map[string]string{"/": "web", "/api": "http", "/auth": "auth", "/external": "redirect", "/missing": "http"} {
		got, _ := probe(context.Background(), client, server.URL+path)
		if got != want {
			t.Errorf("%s: got %s want %s", path, got, want)
		}
	}
	if remoteCalls.Load() != 0 {
		t.Fatal("followed redirect outside approved port")
	}
}

func TestDetectHTTPSAndAmbiguousWebPorts(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<html>web</html>"))
	})
	a := httptest.NewServer(handler)
	defer a.Close()
	b := httptest.NewServer(handler)
	defer b.Close()
	makeCandidate := func(raw string) WebCandidate {
		u, _ := url.Parse(raw)
		host, p, _ := net.SplitHostPort(u.Host)
		port, _ := strconv.Atoi(p)
		cfg := WebConfig{Service: "web", PrivatePort: 80, PublicPort: uint16(port), Scheme: "http", Path: "/"}
		return WebCandidate{WebConfig: cfg, URL: WebURL(host, cfg), Status: "unreachable"}
	}
	result := Detect(context.Background(), []WebCandidate{makeCandidate(a.URL)}, "127.0.0.1", nil)
	if result.RecommendedURL == "" || result.Candidates[0].Status != "web" {
		t.Fatal(result)
	}
	result = Detect(context.Background(), []WebCandidate{makeCandidate(a.URL), makeCandidate(b.URL)}, "127.0.0.1", nil)
	if result.RecommendedURL != "" {
		t.Fatal("multiple web ports must not silently choose first")
	}
	tls := httptest.NewTLSServer(handler)
	defer tls.Close()
	candidate := makeCandidate(tls.URL)
	candidate.Scheme = "https"
	candidate.URL = tls.URL
	result = Detect(context.Background(), []WebCandidate{candidate}, "127.0.0.1", nil)
	if result.Candidates[0].Status != "certificate" || result.Candidates[0].Scheme != "https" {
		t.Fatal(result)
	}
}
