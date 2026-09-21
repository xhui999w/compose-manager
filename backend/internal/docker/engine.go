package docker

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/compose-manager/compose-manager/backend/internal/model"
)

const apiVersion = "v1.43"

var ErrUnavailable = errors.New("docker engine unavailable")

type Engine struct {
	client  *http.Client
	baseURL string
}

func New(host string) (*Engine, error) {
	transport := &http.Transport{MaxIdleConns: 16, IdleConnTimeout: 30 * time.Second}
	baseURL := "http://docker"
	switch {
	case strings.HasPrefix(host, "unix://"):
		socket := strings.TrimPrefix(host, "unix://")
		transport.DialContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, "unix", socket)
		}
	case strings.HasPrefix(host, "tcp://"):
		baseURL = "http://" + strings.TrimPrefix(host, "tcp://")
	case strings.HasPrefix(host, "http://") || strings.HasPrefix(host, "https://"):
		baseURL = strings.TrimRight(host, "/")
	default:
		return nil, fmt.Errorf("unsupported Docker host %q", host)
	}
	return &Engine{client: &http.Client{Transport: transport, Timeout: 12 * time.Second}, baseURL: baseURL}, nil
}

func (e *Engine) request(ctx context.Context, method, path string, body io.Reader, output any) error {
	req, err := http.NewRequestWithContext(ctx, method, e.baseURL+"/"+apiVersion+path, body)
	if err != nil {
		return err
	}
	resp, err := e.client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		message, _ := io.ReadAll(io.LimitReader(resp.Body, 32<<10))
		return fmt.Errorf("docker API %s: %s", resp.Status, strings.TrimSpace(string(message)))
	}
	if output == nil || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(output)
}

type infoResponse struct {
	Containers        int    `json:"Containers"`
	ContainersRunning int    `json:"ContainersRunning"`
	Images            int    `json:"Images"`
	NCPU              int    `json:"NCPU"`
	MemTotal          uint64 `json:"MemTotal"`
	ServerVersion     string `json:"ServerVersion"`
	RegistryConfig    struct {
		Mirrors []string `json:"Mirrors"`
	} `json:"RegistryConfig"`
}

func (e *Engine) Info(ctx context.Context) (model.SystemInfo, error) {
	var data infoResponse
	if err := e.request(ctx, http.MethodGet, "/info", nil, &data); err != nil {
		return model.SystemInfo{}, err
	}
	return model.SystemInfo{DockerAvailable: true, DockerVersion: data.ServerVersion, CPUs: data.NCPU, MemoryTotal: data.MemTotal, ContainersRun: data.ContainersRunning, ContainersTotal: data.Containers, Images: data.Images}, nil
}

func (e *Engine) RegistryMirrors(ctx context.Context) ([]string, error) {
	var data infoResponse
	if err := e.request(ctx, http.MethodGet, "/info", nil, &data); err != nil {
		return nil, err
	}
	return append([]string(nil), data.RegistryConfig.Mirrors...), nil
}

type portResponse struct {
	IP          string `json:"IP"`
	PrivatePort uint16 `json:"PrivatePort"`
	PublicPort  uint16 `json:"PublicPort"`
	Type        string `json:"Type"`
}

type containerResponse struct {
	ID      string            `json:"Id"`
	Names   []string          `json:"Names"`
	Image   string            `json:"Image"`
	ImageID string            `json:"ImageID"`
	State   string            `json:"State"`
	Status  string            `json:"Status"`
	Created int64             `json:"Created"`
	Ports   []portResponse    `json:"Ports"`
	Labels  map[string]string `json:"Labels"`
}

func (e *Engine) Containers(ctx context.Context) ([]model.Container, error) {
	var data []containerResponse
	if err := e.request(ctx, http.MethodGet, "/containers/json?all=1", nil, &data); err != nil {
		return nil, err
	}
	result := make([]model.Container, 0, len(data))
	for _, item := range data {
		name := strings.TrimPrefix(first(item.Names), "/")
		ports := make([]model.Port, 0, len(item.Ports))
		for _, port := range item.Ports {
			ports = append(ports, model.Port{IP: port.IP, PrivatePort: port.PrivatePort, PublicPort: port.PublicPort, Type: port.Type})
		}
		result = append(result, model.Container{
			ID: item.ID, Name: name, Image: item.Image, ImageID: item.ImageID, State: item.State, Status: item.Status,
			Project: item.Labels["com.docker.compose.project"], Service: item.Labels["com.docker.compose.service"],
			CreatedAt: time.Unix(item.Created, 0).UTC(), Ports: ports, Labels: item.Labels,
		})
	}
	return result, nil
}

type statsResponse struct {
	CPUStats struct {
		CPUUsage struct {
			TotalUsage uint64 `json:"total_usage"`
		} `json:"cpu_usage"`
		SystemCPUUsage uint64 `json:"system_cpu_usage"`
		OnlineCPUs     uint64 `json:"online_cpus"`
	} `json:"cpu_stats"`
	PreCPUStats struct {
		CPUUsage struct {
			TotalUsage uint64 `json:"total_usage"`
		} `json:"cpu_usage"`
		SystemCPUUsage uint64 `json:"system_cpu_usage"`
	} `json:"precpu_stats"`
	MemoryStats struct {
		Usage uint64 `json:"usage"`
		Limit uint64 `json:"limit"`
		Stats struct {
			InactiveFile uint64 `json:"inactive_file"`
		} `json:"stats"`
	} `json:"memory_stats"`
}

func (e *Engine) Stats(ctx context.Context, id string) (float64, uint64, uint64, error) {
	var data statsResponse
	path := "/containers/" + url.PathEscape(id) + "/stats?stream=false&one-shot=true"
	if err := e.request(ctx, http.MethodGet, path, nil, &data); err != nil {
		return 0, 0, 0, err
	}
	cpuDelta := data.CPUStats.CPUUsage.TotalUsage - data.PreCPUStats.CPUUsage.TotalUsage
	systemDelta := data.CPUStats.SystemCPUUsage - data.PreCPUStats.SystemCPUUsage
	online := data.CPUStats.OnlineCPUs
	if online == 0 {
		online = 1
	}
	cpu := float64(0)
	if systemDelta > 0 && cpuDelta > 0 {
		cpu = float64(cpuDelta) / float64(systemDelta) * float64(online) * 100
	}
	memory := data.MemoryStats.Usage
	if data.MemoryStats.Stats.InactiveFile < memory {
		memory -= data.MemoryStats.Stats.InactiveFile
	}
	return cpu, memory, data.MemoryStats.Limit, nil
}

func (e *Engine) ContainerAction(ctx context.Context, id, action string) error {
	var path string
	switch action {
	case "start":
		path = "/containers/" + url.PathEscape(id) + "/start"
	case "stop":
		path = "/containers/" + url.PathEscape(id) + "/stop?t=15"
	case "restart":
		path = "/containers/" + url.PathEscape(id) + "/restart?t=15"
	default:
		return fmt.Errorf("unsupported container action %q", action)
	}
	return e.request(ctx, http.MethodPost, path, nil, nil)
}

func (e *Engine) ContainerLogs(ctx context.Context, id string, tail int) (string, error) {
	if tail < 1 {
		tail = 500
	}
	if tail > 5000 {
		tail = 5000
	}
	path := "/containers/" + url.PathEscape(id) + "/logs?stdout=true&stderr=true&timestamps=true&tail=" + strconv.Itoa(tail)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, e.baseURL+"/"+apiVersion+path, nil)
	if err != nil {
		return "", err
	}
	resp, err := e.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		message, _ := io.ReadAll(io.LimitReader(resp.Body, 32<<10))
		return "", fmt.Errorf("docker API %s: %s", resp.Status, strings.TrimSpace(string(message)))
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return "", err
	}
	return demuxLogs(data), nil
}

func demuxLogs(data []byte) string {
	if len(data) < 8 || (data[0] != 1 && data[0] != 2) {
		return string(data)
	}
	var output strings.Builder
	for len(data) >= 8 && (data[0] == 1 || data[0] == 2) {
		length := int(binary.BigEndian.Uint32(data[4:8]))
		if length < 0 || len(data) < 8+length {
			break
		}
		output.Write(data[8 : 8+length])
		data = data[8+length:]
	}
	return output.String()
}

type imageResponse struct {
	ID          string   `json:"Id"`
	RepoTags    []string `json:"RepoTags"`
	RepoDigests []string `json:"RepoDigests"`
	Created     int64    `json:"Created"`
	Size        uint64   `json:"Size"`
}

func (e *Engine) Images(ctx context.Context) ([]model.ImageReference, error) {
	var data []imageResponse
	if err := e.request(ctx, http.MethodGet, "/images/json?all=1", nil, &data); err != nil {
		return nil, err
	}
	result := make([]model.ImageReference, 0, len(data))
	for _, item := range data {
		tags := item.RepoTags
		if len(tags) == 0 {
			tags = []string{"<none>:<none>"}
		}
		for _, ref := range tags {
			repository, tag := splitImage(ref)
			result = append(result, model.ImageReference{ID: item.ID, Repository: repository, Tag: tag, Digest: digestForRepository(repository, item.RepoDigests), Size: item.Size, CreatedAt: time.Unix(item.Created, 0).UTC(), UpdateStatus: "unknown"})
		}
	}
	return result, nil
}

func digestForRepository(repository string, digests []string) string {
	for _, digest := range digests {
		if value, _, ok := strings.Cut(digest, "@"); ok && canonicalRepository(value) == canonicalRepository(repository) {
			return digest
		}
	}
	return ""
}

func canonicalRepository(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	for _, prefix := range []string{"registry-1.docker.io/", "index.docker.io/", "docker.io/"} {
		value = strings.TrimPrefix(value, prefix)
	}
	if !strings.Contains(value, "/") {
		value = "library/" + value
	}
	return value
}

func (e *Engine) DeleteImage(ctx context.Context, id string) error {
	return e.request(ctx, http.MethodDelete, "/images/"+url.PathEscape(id)+"?force=false&noprune=false", nil, nil)
}

func first[T any](values []T) (zero T) {
	if len(values) > 0 {
		return values[0]
	}
	return zero
}

func splitImage(value string) (string, string) {
	lastSlash := strings.LastIndex(value, "/")
	lastColon := strings.LastIndex(value, ":")
	if lastColon > lastSlash {
		return value[:lastColon], value[lastColon+1:]
	}
	return value, "latest"
}

func ShortID(id string) string {
	return strings.TrimPrefix(id, "sha256:")[:min(12, len(strings.TrimPrefix(id, "sha256:")))]
}

func ParseTail(value string) int {
	n, err := strconv.Atoi(value)
	if err != nil || n < 1 {
		return 200
	}
	if n > 5000 {
		return 5000
	}
	return n
}
