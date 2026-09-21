package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const manifestAccept = "application/vnd.docker.distribution.manifest.v2+json, application/vnd.oci.image.manifest.v1+json, application/vnd.docker.distribution.manifest.list.v2+json, application/vnd.oci.image.index.v1+json"

type RegistryChecker struct {
	mu     sync.RWMutex
	client *http.Client
}

func NewRegistryChecker(proxyURL ...string) *RegistryChecker {
	checker := &RegistryChecker{client: registryHTTPClient(nil)}
	if len(proxyURL) > 0 {
		_ = checker.SetProxy(proxyURL[0])
	}
	return checker
}

func (c *RegistryChecker) SetProxy(value string) error {
	proxy, err := ParseProxyURL(value)
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.client = registryHTTPClient(proxy)
	c.mu.Unlock()
	return nil
}

func ParseProxyURL(value string) (*url.URL, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	parsed, err := url.Parse(value)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("代理地址必须使用 http:// 或 https://，且只能包含主机和端口")
	}
	return parsed, nil
}

func registryHTTPClient(proxy *url.URL) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if proxy != nil {
		transport.Proxy = http.ProxyURL(proxy)
	}
	return &http.Client{Timeout: 15 * time.Second, Transport: transport}
}

func (c *RegistryChecker) httpClient() *http.Client {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.client
}

func (c *RegistryChecker) Digest(ctx context.Context, image string, dockerHubMirrors ...string) (string, error) {
	registry, repository, tag, err := parseReference(image)
	if err != nil {
		return "", err
	}
	var failures []error
	for _, baseURL := range registryEndpoints(registry, dockerHubMirrors) {
		digest, digestErr := c.digestFromEndpoint(ctx, baseURL, repository, tag)
		if digestErr == nil {
			return digest, nil
		}
		failures = append(failures, digestErr)
	}
	return "", errors.Join(failures...)
}

func (c *RegistryChecker) digestFromEndpoint(ctx context.Context, baseURL, repository, tag string) (string, error) {
	endpoint := strings.TrimRight(baseURL, "/") + "/v2/" + repository + "/manifests/" + url.PathEscape(tag)
	request, _ := http.NewRequestWithContext(ctx, http.MethodHead, endpoint, nil)
	request.Header.Set("Accept", manifestAccept)
	client := c.httpClient()
	response, err := client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusUnauthorized {
		challenge := response.Header.Get("WWW-Authenticate")
		token, tokenErr := c.token(ctx, challenge)
		if tokenErr != nil {
			return "", tokenErr
		}
		request, _ = http.NewRequestWithContext(ctx, http.MethodHead, endpoint, nil)
		request.Header.Set("Accept", manifestAccept)
		request.Header.Set("Authorization", "Bearer "+token)
		response, err = client.Do(request)
		if err != nil {
			return "", err
		}
		defer response.Body.Close()
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("registry returned %s", response.Status)
	}
	digest := strings.TrimSpace(response.Header.Get("Docker-Content-Digest"))
	if digest == "" {
		return "", errors.New("registry did not return Docker-Content-Digest")
	}
	return digest, nil
}

func registryEndpoints(registry string, dockerHubMirrors []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(dockerHubMirrors)+1)
	add := func(value string) {
		value = strings.TrimRight(strings.TrimSpace(value), "/")
		parsed, err := url.Parse(value)
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.Path != "" {
			return
		}
		key := strings.ToLower(parsed.String())
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		result = append(result, parsed.String())
	}
	if registry == "registry-1.docker.io" {
		for _, mirror := range dockerHubMirrors {
			add(mirror)
		}
	}
	add("https://" + registry)
	return result
}

func (c *RegistryChecker) token(ctx context.Context, challenge string) (string, error) {
	if !strings.HasPrefix(strings.ToLower(challenge), "bearer ") {
		return "", errors.New("unsupported registry authentication challenge")
	}
	values := map[string]string{}
	for _, part := range strings.Split(challenge[len("Bearer "):], ",") {
		key, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		if ok {
			values[strings.ToLower(key)] = strings.Trim(value, `"`)
		}
	}
	realm := values["realm"]
	parsed, err := url.Parse(realm)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return "", errors.New("registry token realm is invalid")
	}
	query := parsed.Query()
	if values["service"] != "" {
		query.Set("service", values["service"])
	}
	if values["scope"] != "" {
		query.Set("scope", values["scope"])
	}
	parsed.RawQuery = query.Encode()
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	response, err := c.httpClient().Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("registry token service returned %s", response.Status)
	}
	var payload struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&payload); err != nil {
		return "", err
	}
	if payload.Token != "" {
		return payload.Token, nil
	}
	if payload.AccessToken != "" {
		return payload.AccessToken, nil
	}
	return "", errors.New("registry token response was empty")
}

func parseReference(image string) (registry, repository, tag string, err error) {
	image = strings.TrimSpace(strings.Split(image, "@")[0])
	if image == "" || strings.ContainsAny(image, " \t\r\n") {
		return "", "", "", errors.New("invalid image reference")
	}
	parts := strings.Split(image, "/")
	registry = "registry-1.docker.io"
	if len(parts) > 1 && (strings.Contains(parts[0], ".") || strings.Contains(parts[0], ":") || parts[0] == "localhost") {
		registry = parts[0]
		parts = parts[1:]
	}
	if registry == "docker.io" || registry == "index.docker.io" {
		registry = "registry-1.docker.io"
	}
	last := parts[len(parts)-1]
	if index := strings.LastIndex(last, ":"); index >= 0 {
		tag = last[index+1:]
		parts[len(parts)-1] = last[:index]
	} else {
		tag = "latest"
	}
	repository = strings.Join(parts, "/")
	if registry == "registry-1.docker.io" && !strings.Contains(repository, "/") {
		repository = "library/" + repository
	}
	if repository == "" || tag == "" {
		return "", "", "", errors.New("invalid image reference")
	}
	return registry, repository, tag, nil
}
