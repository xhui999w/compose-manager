package docker

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"regexp"
)

type RemovalMount struct{ Type, Source, Destination, Name string }
type RemovalContainer struct {
	ID                    string `json:"Id"`
	Names                 []string
	Image, ImageID, State string
	Labels                map[string]string
	Mounts                []RemovalMount
}
type RemovalVolume struct {
	Name   string
	Labels map[string]string
}
type RemovalNetwork struct {
	ID     string `json:"Id"`
	Name   string
	Labels map[string]string
}
type RemovalResources struct {
	Containers []RemovalContainer
	Volumes    []RemovalVolume
	Networks   []RemovalNetwork
}

func (e *Engine) RemovalResources(ctx context.Context) (RemovalResources, error) {
	var result RemovalResources
	if err := e.request(ctx, http.MethodGet, "/containers/json?all=1", nil, &result.Containers); err != nil {
		return result, err
	}
	var volumes struct{ Volumes []RemovalVolume }
	if err := e.request(ctx, http.MethodGet, "/volumes", nil, &volumes); err != nil {
		return result, err
	}
	result.Volumes = volumes.Volumes
	err := e.request(ctx, http.MethodGet, "/networks", nil, &result.Networks)
	return result, err
}

var resourceID = regexp.MustCompile(`^[a-f0-9]{64}$`)
var volumeName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,255}$`)

// Never force deletion or implicitly remove volumes/parent images.
func (e *Engine) RemoveProjectResource(ctx context.Context, kind, id string) error {
	var path string
	switch kind {
	case "container":
		if !resourceID.MatchString(id) {
			return errors.New("invalid container ID")
		}
		path = "/containers/" + id + "?force=false&v=false"
	case "network":
		if !resourceID.MatchString(id) {
			return errors.New("invalid network ID")
		}
		path = "/networks/" + id
	case "volume":
		if !volumeName.MatchString(id) {
			return errors.New("invalid volume name")
		}
		path = "/volumes/" + url.PathEscape(id) + "?force=false"
	case "image":
		if len(id) != 71 || id[:7] != "sha256:" || !resourceID.MatchString(id[7:]) {
			return errors.New("invalid image ID")
		}
		path = "/images/" + url.PathEscape(id) + "?force=false&noprune=true"
	default:
		return errors.New("unsupported resource kind")
	}
	return e.request(ctx, http.MethodDelete, path, nil, nil)
}
