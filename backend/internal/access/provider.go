package access

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

type ProviderConfig map[string]string

type Target struct {
	Scheme string `json:"scheme"`
	Host   string `json:"host"`
	Port   uint16 `json:"port"`
	Path   string `json:"path"`
	Name   string `json:"name"`
}

type Link struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

type RemoteAccessProvider interface {
	ID() string
	Validate(context.Context, ProviderConfig) error
	Resolve(context.Context, Target, ProviderConfig) ([]Link, error)
}

type ManualProvider struct{}

func (ManualProvider) ID() string { return "manual" }

func (ManualProvider) Validate(_ context.Context, config ProviderConfig) error {
	if raw := strings.TrimSpace(config["url"]); raw != "" {
		parsed, err := url.ParseRequestURI(raw)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			return fmt.Errorf("manual URL must be an absolute http(s) URL")
		}
	}
	return nil
}

func (p ManualProvider) Resolve(ctx context.Context, target Target, config ProviderConfig) ([]Link, error) {
	if err := p.Validate(ctx, config); err != nil {
		return nil, err
	}
	if raw := strings.TrimSpace(config["url"]); raw != "" {
		return []Link{{Name: target.Name, URL: raw}}, nil
	}
	host := strings.TrimSpace(config["domain"])
	if host == "" {
		host = target.Host
	}
	scheme := strings.TrimSpace(config["scheme"])
	if scheme == "" {
		scheme = target.Scheme
	}
	port := target.Port
	if configured := strings.TrimSpace(config["port"]); configured != "" {
		value, err := strconv.ParseUint(configured, 10, 16)
		if err != nil || value == 0 {
			return nil, fmt.Errorf("invalid external port")
		}
		port = uint16(value)
	}
	path := target.Path
	if path == "" {
		path = "/"
	}
	result := &url.URL{Scheme: scheme, Host: host, Path: "/" + strings.TrimLeft(path, "/")}
	if port > 0 && !((scheme == "http" && port == 80) || (scheme == "https" && port == 443)) {
		result.Host = netJoinHostPort(host, port)
	}
	return []Link{{Name: target.Name, URL: result.String()}}, nil
}

func netJoinHostPort(host string, port uint16) string {
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		return "[" + host + "]:" + strconv.Itoa(int(port))
	}
	return host + ":" + strconv.Itoa(int(port))
}
