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
	"time"
)

const manifestAccept = "application/vnd.docker.distribution.manifest.v2+json, application/vnd.oci.image.manifest.v1+json, application/vnd.docker.distribution.manifest.list.v2+json, application/vnd.oci.image.index.v1+json"

type RegistryChecker struct{ client *http.Client }

func NewRegistryChecker() *RegistryChecker {
	return &RegistryChecker{client: &http.Client{Timeout: 12 * time.Second}}
}

func (c *RegistryChecker) Digest(ctx context.Context, image string) (string, error) {
	registry, repository, tag, err := parseReference(image)
	if err != nil {
		return "", err
	}
	endpoint := "https://" + registry + "/v2/" + repository + "/manifests/" + url.PathEscape(tag)
	request, _ := http.NewRequestWithContext(ctx, http.MethodHead, endpoint, nil)
	request.Header.Set("Accept", manifestAccept)
	response, err := c.client.Do(request)
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
		response, err = c.client.Do(request)
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
	response, err := c.client.Do(request)
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
