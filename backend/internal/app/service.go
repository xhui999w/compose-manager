package app

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/compose-manager/compose-manager/backend/internal/compose"
	"github.com/compose-manager/compose-manager/backend/internal/config"
	dockerapi "github.com/compose-manager/compose-manager/backend/internal/docker"
	"github.com/compose-manager/compose-manager/backend/internal/model"
	"github.com/compose-manager/compose-manager/backend/internal/store"
	"github.com/compose-manager/compose-manager/backend/internal/update"
)

type Service struct {
	config    config.Config
	docker    *dockerapi.Engine
	discovery *compose.Discovery
	guard     *compose.PathGuard
	runner    *compose.Runner
	editor    *compose.Editor
	store     *store.Store
	checker   *update.RegistryChecker
	updateMu  sync.RWMutex
	updateMap map[string]string
}

func New(cfg config.Config, engine *dockerapi.Engine, guard *compose.PathGuard, runner *compose.Runner, editor *compose.Editor, store *store.Store) *Service {
	return &Service{config: cfg, docker: engine, discovery: compose.NewDiscovery(guard), guard: guard, runner: runner, editor: editor, store: store, checker: update.NewRegistryChecker(), updateMap: map[string]string{}}
}

func (s *Service) Health(ctx context.Context) map[string]any {
	status := "ok"
	dockerStatus := "available"
	if s.config.DemoMode {
		dockerStatus = "demo"
	} else if _, err := s.docker.Info(ctx); err != nil {
		status = "degraded"
		dockerStatus = err.Error()
	}
	return map[string]any{"status": status, "docker": dockerStatus, "time": time.Now().UTC(), "version": "0.2.0"}
}

func (s *Service) Projects(ctx context.Context) ([]model.Project, error) {
	if s.config.DemoMode {
		return demoProjects(s.config.NASIP), nil
	}
	files, scanErr := s.discovery.Scan(ctx)
	containers, dockerErr := s.docker.Containers(ctx)
	if scanErr != nil && dockerErr != nil {
		return nil, errors.Join(scanErr, dockerErr)
	}
	s.sampleStats(ctx, containers)
	projects := map[string]*model.Project{}
	for _, file := range files {
		project := &model.Project{Key: file.Key, Name: file.Name, Status: "not-running", UpdateStatus: "unknown", UpdatePolicy: "check-only", ConfigFile: file.File, WorkingDir: file.WorkingDir, DiscoverySource: "scan", Editable: true, Containers: []model.Container{}}
		projects[file.Key] = project
	}
	for _, container := range containers {
		key := strings.TrimSpace(container.Project)
		if key == "" {
			continue
		}
		project := projects[key]
		if project == nil {
			project = &model.Project{Key: key, Name: key, Status: "not-running", UpdateStatus: "unknown", UpdatePolicy: "check-only", DiscoverySource: "labels", Containers: []model.Container{}}
			if configPath := firstConfigPath(container.Labels["com.docker.compose.project.config_files"]); configPath != "" {
				if resolved, err := s.guard.ResolveComposeFile(configPath); err == nil {
					project.ConfigFile = resolved
					project.WorkingDir = filepath.Dir(resolved)
					project.Editable = true
					project.DiscoverySource = "labels+file"
				}
			}
			projects[key] = project
		}
		project.Containers = append(project.Containers, container)
	}
	result := make([]model.Project, 0, len(projects))
	for _, project := range projects {
		enrichProject(project, s.config.NASIP, s.config.DefaultScheme)
		result = append(result, *project)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, nil
}

func (s *Service) Overview(ctx context.Context) (model.SystemInfo, error) {
	if s.config.DemoMode {
		projects := demoProjects(s.config.NASIP)
		return model.SystemInfo{DockerAvailable: true, DockerVersion: "demo", CPUs: 8, CPUPercent: 8.2, MemoryUsed: 5800000000, MemoryTotal: 16000000000, ContainersRun: 37, ContainersTotal: 42, Images: 68, Projects: len(projects), Reclaimable: 12600000000}, nil
	}
	info, infoErr := s.docker.Info(ctx)
	projects, projectErr := s.Projects(ctx)
	if infoErr != nil {
		return model.SystemInfo{DockerAvailable: false, Projects: len(projects)}, infoErr
	}
	info.Projects = len(projects)
	for _, project := range projects {
		info.CPUPercent += project.CPUPercent
		info.MemoryUsed += project.MemoryBytes
	}
	return info, projectErr
}

func (s *Service) Containers(ctx context.Context) ([]model.Container, error) {
	if s.config.DemoMode {
		var result []model.Container
		for _, project := range demoProjects(s.config.NASIP) {
			result = append(result, project.Containers...)
		}
		return result, nil
	}
	items, err := s.docker.Containers(ctx)
	if err != nil {
		return nil, err
	}
	s.sampleStats(ctx, items)
	return items, nil
}

func (s *Service) Project(ctx context.Context, key string) (model.Project, error) {
	projects, err := s.Projects(ctx)
	if err != nil && len(projects) == 0 {
		return model.Project{}, err
	}
	for _, project := range projects {
		if project.Key == key {
			return project, nil
		}
	}
	return model.Project{}, fmt.Errorf("project %q not found", key)
}

func (s *Service) ProjectAction(ctx context.Context, key, action, service string) (string, error) {
	if s.config.DemoMode {
		return "演示模式未执行真实操作", nil
	}
	project, err := s.Project(ctx, key)
	if err != nil {
		return "", err
	}
	if project.ConfigFile == "" {
		return "", errors.New("project has no validated Compose file")
	}
	output, err := s.runner.Run(ctx, project.Key, project.ConfigFile, action, service, 0)
	result := "success"
	if err != nil {
		result = "failed"
	}
	_ = s.store.Audit(ctx, "compose."+action, "project", key, result, output)
	return output, err
}

func (s *Service) ProjectLogs(ctx context.Context, key, service string, tail int) (string, error) {
	if s.config.DemoMode {
		return "2026-09-19T10:00:00Z service ready\n2026-09-19T10:00:01Z health check passed\n", nil
	}
	project, err := s.Project(ctx, key)
	if err != nil {
		return "", err
	}
	return s.runner.Run(ctx, project.Key, project.ConfigFile, "logs", service, tail)
}

func (s *Service) File(ctx context.Context, key string) (compose.FileContent, error) {
	project, err := s.Project(ctx, key)
	if err != nil {
		return compose.FileContent{}, err
	}
	if s.config.DemoMode {
		content := "services:\n  web:\n    image: nginx:1.27\n    restart: unless-stopped\n    ports:\n      - 8080:80\n"
		return compose.FileContent{Path: "/compose/" + key + "/compose.yaml", Content: content, SHA256: "demo"}, nil
	}
	return s.editor.Read(project.ConfigFile)
}

func (s *Service) ValidateFile(ctx context.Context, key, content string) (string, string, error) {
	current, err := s.File(ctx, key)
	if err != nil {
		return "", "", err
	}
	if s.config.DemoMode {
		return "", "--- before\n+++ after\n", nil
	}
	output, err := s.editor.Validate(ctx, key, current.Path, content)
	if err != nil {
		return output, "", err
	}
	diff, err := s.editor.Diff(current.Path, content)
	return output, diff, err
}

func (s *Service) SaveFile(ctx context.Context, key, content, baseSHA string, apply bool) (compose.SaveResult, error) {
	if s.config.DemoMode {
		return compose.SaveResult{}, errors.New("demo mode is read-only")
	}
	project, err := s.Project(ctx, key)
	if err != nil {
		return compose.SaveResult{}, err
	}
	result, err := s.editor.Save(ctx, key, project.ConfigFile, content, baseSHA, apply)
	status := "success"
	if err != nil {
		status = "failed"
	}
	_ = s.store.Audit(ctx, "compose.save", "project", key, status, fmt.Sprintf("apply=%t", apply))
	return result, err
}

func (s *Service) Versions(ctx context.Context, key string) ([]model.ComposeVersion, error) {
	return s.store.Versions(ctx, key)
}

func (s *Service) Restore(ctx context.Context, key string, id int64, baseSHA string, apply bool) (compose.SaveResult, error) {
	if s.config.DemoMode {
		return compose.SaveResult{}, errors.New("demo mode is read-only")
	}
	result, err := s.editor.Restore(ctx, key, id, baseSHA, apply)
	status := "success"
	if err != nil {
		status = "failed"
	}
	_ = s.store.Audit(ctx, "compose.restore", "project", key, status, fmt.Sprintf("version=%d apply=%t", id, apply))
	return result, err
}

func (s *Service) ContainerAction(ctx context.Context, id, action string) error {
	if s.config.DemoMode {
		return nil
	}
	err := s.docker.ContainerAction(ctx, id, action)
	status := "success"
	if err != nil {
		status = "failed"
	}
	_ = s.store.Audit(ctx, "container."+action, "container", id, status, "")
	return err
}

func (s *Service) InspectContainer(ctx context.Context, id string) (map[string]any, error) {
	if s.config.DemoMode {
		return map[string]any{"Id": id, "Name": "/demo-container", "State": map[string]any{"Status": "running", "Running": true}, "Config": map[string]any{"Image": "ghcr.io/example/demo:latest"}}, nil
	}
	return s.docker.InspectContainer(ctx, id)
}

func (s *Service) ContainerLogs(ctx context.Context, id string, tail int) (string, error) {
	if s.config.DemoMode {
		return "2026-09-19T10:00:00Z container started\n2026-09-19T10:00:01Z health check passed\n", nil
	}
	return s.docker.ContainerLogs(ctx, id, tail)
}

func (s *Service) Images(ctx context.Context) ([]model.ImageReference, error) {
	if s.config.DemoMode {
		return demoImages(), nil
	}
	images, err := s.docker.Images(ctx)
	if err != nil {
		return nil, err
	}
	containers, _ := s.docker.Containers(ctx)
	projects, _ := s.discovery.Scan(ctx)
	for index := range images {
		image := &images[index]
		s.updateMu.RLock()
		if cached, ok := s.updateMap[image.Repository+":"+image.Tag]; ok {
			image.UpdateStatus = cached
		}
		s.updateMu.RUnlock()
		for _, container := range containers {
			if container.ImageID != image.ID {
				continue
			}
			if container.State == "running" {
				image.RunningReferences = append(image.RunningReferences, container.Name)
			} else {
				image.StoppedReferences = append(image.StoppedReferences, container.Name)
			}
		}
		ref := image.Repository + ":" + image.Tag
		for _, project := range projects {
			for _, composeImage := range project.Images {
				if imageRefEqual(ref, composeImage) {
					image.ComposeReferences = append(image.ComposeReferences, project.Name)
				}
			}
		}
		classifyImage(image)
	}
	return images, nil
}

func (s *Service) DeleteImage(ctx context.Context, id string) error {
	images, err := s.Images(ctx)
	if err != nil {
		return err
	}
	for _, image := range images {
		if image.ID != id {
			continue
		}
		if len(image.RunningReferences)+len(image.StoppedReferences)+len(image.ComposeReferences) > 0 {
			return fmt.Errorf("image is still referenced by running=%d stopped=%d compose=%d", len(image.RunningReferences), len(image.StoppedReferences), len(image.ComposeReferences))
		}
		if s.config.DemoMode {
			return errors.New("demo mode is read-only")
		}
		err := s.docker.DeleteImage(ctx, id)
		status := "success"
		if err != nil {
			status = "failed"
		}
		_ = s.store.Audit(ctx, "image.delete", "image", id, status, "reference counts rechecked")
		return err
	}
	return errors.New("image not found")
}

func (s *Service) CheckImageUpdates(ctx context.Context) ([]model.ImageReference, error) {
	images, err := s.Images(ctx)
	if err != nil {
		return nil, err
	}
	if s.config.DemoMode {
		return images, nil
	}
	semaphore := make(chan struct{}, 4)
	var wait sync.WaitGroup
	for index := range images {
		if images[index].Repository == "<none>" {
			continue
		}
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()
			remote, digestErr := s.checker.Digest(ctx, images[index].Repository+":"+images[index].Tag)
			if digestErr != nil {
				images[index].UpdateStatus = "unknown"
				return
			}
			local := images[index].Digest
			if marker := strings.LastIndex(local, "@"); marker >= 0 {
				local = local[marker+1:]
			}
			if local != "" && local != remote {
				images[index].UpdateStatus = "available"
			} else {
				images[index].UpdateStatus = "current"
			}
		}(index)
	}
	wait.Wait()
	s.updateMu.Lock()
	for _, image := range images {
		s.updateMap[image.Repository+":"+image.Tag] = image.UpdateStatus
	}
	s.updateMu.Unlock()
	return images, nil
}

func (s *Service) RunUpdate(ctx context.Context, key, service string) (model.UpdateRecord, error) {
	project, err := s.Project(ctx, key)
	if err != nil {
		return model.UpdateRecord{}, err
	}
	record := model.UpdateRecord{Project: key, Service: service, Status: "running"}
	if s.config.DemoMode {
		record.Status = "success"
		record.OldImage = "demo/app:latest"
		record.NewImage = "demo/app:latest"
		return s.store.AddUpdate(ctx, record)
	}
	beforeContainers, _ := s.docker.Containers(ctx)
	beforeImages, _ := s.docker.Images(ctx)
	record.OldImage, record.OldDigest = updateSnapshot(key, service, beforeContainers, beforeImages)
	output, pullErr := s.runner.Run(ctx, key, project.ConfigFile, "pull", service, 0)
	if pullErr == nil {
		output, pullErr = s.runner.Run(ctx, key, project.ConfigFile, "apply", "", 0)
	}
	if pullErr != nil {
		record.Status = "failed"
		record.Error = sanitizeError(pullErr.Error() + " " + output)
	} else {
		record.Status = "success"
		afterContainers, _ := s.docker.Containers(ctx)
		afterImages, _ := s.docker.Images(ctx)
		record.NewImage, record.NewDigest = updateSnapshot(key, service, afterContainers, afterImages)
	}
	record, storeErr := s.store.AddUpdate(ctx, record)
	if storeErr != nil {
		return record, storeErr
	}
	return record, pullErr
}

func (s *Service) Updates(ctx context.Context) ([]model.UpdateRecord, error) {
	return s.store.Updates(ctx)
}
func (s *Service) Settings(ctx context.Context) (map[string]any, error) { return s.store.Settings(ctx) }
func (s *Service) PutSettings(ctx context.Context, values map[string]any) error {
	allowed := map[string]bool{"dockerHost": true, "composeRoots": true, "nasIP": true, "updateInterval": true, "defaultScheme": true, "externalURL": true, "density": true}
	for key := range values {
		if !allowed[key] {
			return fmt.Errorf("unsupported setting %q", key)
		}
	}
	return s.store.PutSettings(ctx, values)
}

func (s *Service) sampleStats(ctx context.Context, containers []model.Container) {
	if s.docker == nil {
		return
	}
	semaphore := make(chan struct{}, 6)
	var wait sync.WaitGroup
	for index := range containers {
		if containers[index].State != "running" {
			continue
		}
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()
			statCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
			defer cancel()
			cpu, memory, limit, err := s.docker.Stats(statCtx, containers[index].ID)
			if err == nil {
				containers[index].CPUPercent, containers[index].MemoryBytes, containers[index].MemoryLimit = cpu, memory, limit
			}
		}(index)
	}
	wait.Wait()
}

func enrichProject(project *model.Project, nasIP, scheme string) {
	project.Total = len(project.Containers)
	running := 0
	for _, container := range project.Containers {
		project.CPUPercent += container.CPUPercent
		project.MemoryBytes += container.MemoryBytes
		if container.State == "running" {
			running++
			project.Healthy++
		}
		if project.InternalURL == "" {
			for _, port := range container.Ports {
				if port.PublicPort > 0 && (port.Type == "tcp" || port.Type == "") {
					project.InternalURL = (&url.URL{Scheme: scheme, Host: fmt.Sprintf("%s:%d", nasIP, port.PublicPort)}).String()
					break
				}
			}
		}
	}
	switch {
	case project.Total == 0:
		project.Status = "not-running"
	case running == project.Total:
		project.Status = "running"
	case running == 0:
		project.Status = "stopped"
	default:
		project.Status = "degraded"
	}
}

func firstConfigPath(value string) string {
	if value == "" {
		return ""
	}
	return strings.TrimSpace(strings.Split(value, ",")[0])
}

func imageRefEqual(a, b string) bool {
	normalize := func(value string) string {
		value = strings.TrimSpace(strings.TrimPrefix(value, "docker.io/"))
		if !strings.Contains(value[strings.LastIndex(value, "/")+1:], ":") {
			value += ":latest"
		}
		return value
	}
	return normalize(a) == normalize(b)
}

func classifyImage(image *model.ImageReference) {
	switch {
	case len(image.RunningReferences) > 0:
		image.Category = "in-use"
	case len(image.ComposeReferences) > 0:
		image.Category = "compose-referenced"
	case image.Repository == "<none>":
		image.Category = "dangling"
		image.Reclaimable = image.Size
	case len(image.StoppedReferences) > 0:
		image.Category = "old-version"
	default:
		image.Category = "unused"
		image.Reclaimable = image.Size
	}
}

func sanitizeError(value string) string {
	value = strings.ReplaceAll(value, "\x00", "")
	if len(value) > 2048 {
		return value[:2048]
	}
	return value
}

func updateSnapshot(projectKey, service string, containers []model.Container, images []model.ImageReference) (string, string) {
	for _, container := range containers {
		if container.Project != projectKey || (service != "" && container.Service != service) {
			continue
		}
		for _, image := range images {
			if image.ID == container.ImageID {
				return container.Image, image.Digest
			}
		}
		return container.Image, container.ImageID
	}
	return "", ""
}
