package access

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/compose-manager/compose-manager/backend/internal/model"
)

// WebConfig identifies a published port, not a container ID, so it survives recreation.
type WebConfig struct {
	Service     string `json:"service"`
	PrivatePort uint16 `json:"privatePort"`
	PublicPort  uint16 `json:"publicPort"`
	Scheme      string `json:"scheme"`
	Path        string `json:"path"`
}

type WebCandidate struct {
	WebConfig
	Container  string `json:"container"`
	URL        string `json:"url"`
	Status     string `json:"status"`
	HTTPStatus int    `json:"httpStatus,omitempty"`
}

type WebResult struct {
	Candidates     []WebCandidate `json:"candidates"`
	Saved          *WebConfig     `json:"saved,omitempty"`
	RecommendedURL string         `json:"recommendedUrl,omitempty"`
}

func ValidateWebConfig(c WebConfig) error {
	if c.PublicPort == 0 || c.PrivatePort == 0 || (c.Scheme != "http" && c.Scheme != "https") {
		return errors.New("请选择有效的 TCP 映射端口和 HTTP/HTTPS 协议")
	}
	p, err := url.Parse(c.Path)
	if err != nil || len(c.Path) > 1024 || !strings.HasPrefix(c.Path, "/") || strings.HasPrefix(c.Path, "//") || strings.ContainsAny(c.Path, "\\\r\n\x00") || p.IsAbs() || p.Host != "" || p.User != nil {
		return errors.New("访问路径必须是以 / 开头的站内路径，例如 /transmission/web/")
	}
	return nil
}

func WebURL(host string, c WebConfig) string {
	if ValidateWebConfig(c) != nil || net.ParseIP(host) == nil {
		return ""
	}
	p, _ := url.Parse(c.Path)
	p.Scheme, p.Host = c.Scheme, net.JoinHostPort(host, strconv.Itoa(int(c.PublicPort)))
	return p.String()
}

func SamePort(a, b WebConfig) bool {
	return a.Service == b.Service && a.PrivatePort == b.PrivatePort && a.PublicPort == b.PublicPort
}

// Only Docker-published TCP ports bound to the configured NAS address are candidates.
// No arbitrary destination, DNS resolution or environment proxy is used.
func Candidates(containers []model.Container, host, scheme string) []WebCandidate {
	result := []WebCandidate{}
	seen := map[string]bool{}
	if scheme != "https" {
		scheme = "http"
	}
	for _, c := range containers {
		for _, p := range c.Ports {
			if p.PublicPort == 0 || (p.Type != "tcp" && p.Type != "") {
				continue
			}
			if p.IP != "" && p.IP != "0.0.0.0" && p.IP != "::" && !net.ParseIP(p.IP).Equal(net.ParseIP(host)) {
				continue
			}
			service := c.Service
			if service == "" {
				service = c.Name
			}
			key := fmt.Sprintf("%s:%d:%d", service, p.PrivatePort, p.PublicPort)
			if seen[key] {
				continue
			}
			seen[key] = true
			cfg := WebConfig{Service: service, PrivatePort: p.PrivatePort, PublicPort: p.PublicPort, Scheme: scheme, Path: "/"}
			if p.PrivatePort == 443 || p.PrivatePort == 8443 {
				cfg.Scheme = "https"
			}
			status := "unreachable"
			if c.State != "running" {
				status = "stopped"
			}
			result = append(result, WebCandidate{WebConfig: cfg, Container: c.Name, URL: WebURL(host, cfg), Status: status})
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].PublicPort != result[j].PublicPort {
			return result[i].PublicPort < result[j].PublicPort
		}
		return result[i].Service < result[j].Service
	})
	return result
}

func ResolveSaved(containers []model.Container, host string, cfg WebConfig) string {
	for _, c := range Candidates(containers, host, cfg.Scheme) {
		if SamePort(c.WebConfig, cfg) {
			return WebURL(host, cfg)
		}
	}
	return ""
}

// A scan runs only when the access dialog is opened. Total duration and concurrency
// are bounded; overview refreshes never wait for probes against application ports.
func Detect(ctx context.Context, candidates []WebCandidate, host string, saved *WebConfig) WebResult {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	transport := &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: time.Second}).DialContext, TLSHandshakeTimeout: time.Second, MaxResponseHeaderBytes: 16 << 10, DisableKeepAlives: true}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 2 * time.Second, CheckRedirect: sameOriginRedirect}
	result := WebResult{Candidates: candidates, Saved: saved}
	var wg sync.WaitGroup
	semaphore := make(chan struct{}, 6)
	for index := range result.Candidates {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			select {
			case semaphore <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-semaphore }()
			c := &result.Candidates[i]
			if saved != nil && SamePort(c.WebConfig, *saved) {
				c.WebConfig = *saved
				c.URL = WebURL(host, *saved)
			}
			if c.Status == "stopped" || c.URL == "" {
				return
			}
			first := c.Scheme
			second := "https"
			if first == "https" {
				second = "http"
			}
			best := *c
			for _, scheme := range []string{first, second} {
				candidate := *c
				candidate.Scheme = scheme
				candidate.URL = WebURL(host, candidate.WebConfig)
				candidate.Status, candidate.HTTPStatus = probe(ctx, client, candidate.URL)
				if webRank(candidate.Status) > webRank(best.Status) {
					best = candidate
				}
				if candidate.Status == "web" {
					break
				}
			}
			*c = best
		}(index)
	}
	wg.Wait()
	// A responding HTTP API is not proof of a browser UI. Only a unique HTML
	// page is recommended; authentication, redirects and APIs stay explicit choices.
	count := 0
	for _, c := range result.Candidates {
		if c.Status == "web" {
			count++
			result.RecommendedURL = c.URL
		}
	}
	if count != 1 {
		result.RecommendedURL = ""
	}
	return result
}

func webRank(status string) int {
	switch status {
	case "web":
		return 7
	case "auth":
		return 6
	case "redirect":
		return 5
	case "http":
		return 3
	case "certificate":
		return 4
	default:
		return 0
	}
}

func sameOriginRedirect(req *http.Request, via []*http.Request) error {
	if len(via) == 0 || len(via) >= 3 || req.URL.Scheme != via[0].URL.Scheme || req.URL.Host != via[0].URL.Host || req.URL.User != nil {
		return http.ErrUseLastResponse
	}
	return nil
}

func probe(ctx context.Context, client *http.Client, rawURL string) (string, int) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "unreachable", 0
	}
	req.Header.Set("User-Agent", "Compose-Manager-Web-Check/1.0")
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	resp, err := client.Do(req)
	if err != nil {
		var unknown x509.UnknownAuthorityError
		var hostname x509.HostnameError
		var invalid x509.CertificateInvalidError
		if errors.As(err, &unknown) || errors.As(err, &hostname) || errors.As(err, &invalid) {
			return "certificate", 0
		}
		return "unreachable", 0
	}
	defer resp.Body.Close()
	code := resp.StatusCode
	if code == 401 || code == 403 {
		return "auth", code
	}
	if code >= 300 && code < 400 && resp.Header.Get("Location") != "" {
		return "redirect", code
	}
	if code >= 200 && code < 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		contentType := strings.ToLower(resp.Header.Get("Content-Type"))
		if contentType == "" {
			contentType = http.DetectContentType(body)
		}
		if strings.HasPrefix(contentType, "text/html") || strings.HasPrefix(contentType, "application/xhtml+xml") {
			return "web", code
		}
	}
	return "http", code
}
