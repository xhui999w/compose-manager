package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/compose-manager/compose-manager/backend/internal/compose"
	dockerapi "github.com/compose-manager/compose-manager/backend/internal/docker"
)

type RemovalItem struct {
	Kind   string `json:"kind"`
	ID     string `json:"id"`
	Name   string `json:"name"`
	State  string `json:"state,omitempty"`
	Reason string `json:"reason,omitempty"`
}
type RemovalPlan struct {
	Key              string        `json:"key"`
	Name             string        `json:"name"`
	File             string        `json:"file"`
	Directory        string        `json:"directory"`
	HostDirectory    string        `json:"hostDirectory"`
	HostFile         string        `json:"hostFile"`
	DirectoryBlocked string        `json:"directoryBlocked,omitempty"`
	Items            []RemovalItem `json:"items"`
	Retained         []string      `json:"retained"`
	Token            string        `json:"token"`
	Fingerprint      string        `json:"-"`
	ConfigSHA        string        `json:"-"`
}

var projectNamePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$`)

type removalTicket struct {
	Key, Fingerprint string
	Expires          time.Time
}
type RemovalRequest struct {
	Token     string `json:"token"`
	Name      string `json:"name"`
	Directory bool   `json:"directory"`
	Images    bool   `json:"images"`
	Volumes   bool   `json:"volumes"`
}
type RemovalResult struct {
	Deleted  []string `json:"deleted"`
	Retained []string `json:"retained"`
	Errors   []string `json:"errors"`
	Backup   string   `json:"backup"`
}

func (s *Service) PreviewProjectRemoval(ctx context.Context, key string) (RemovalPlan, error) {
	s.removalGate.RLock()
	defer s.removalGate.RUnlock()
	plan, err := s.removalPlan(ctx, key)
	if err != nil {
		return plan, err
	}
	token, err := updateTaskID()
	if err != nil {
		return plan, err
	}
	s.removalTicketMu.Lock()
	defer s.removalTicketMu.Unlock()
	if s.removalTickets == nil {
		s.removalTickets = map[string]removalTicket{}
	}
	for id, ticket := range s.removalTickets {
		if time.Now().After(ticket.Expires) {
			delete(s.removalTickets, id)
		}
	}
	if len(s.removalTickets) >= 64 {
		return plan, errors.New("删除预览过多，请稍后再试")
	}
	s.removalTickets[token] = removalTicket{key, plan.Fingerprint, time.Now().Add(10 * time.Minute)}
	plan.Token = token
	return plan, nil
}

func (s *Service) removalPlan(ctx context.Context, key string) (RemovalPlan, error) {
	plan := RemovalPlan{Key: key, Items: []RemovalItem{}, Retained: []string{}}
	if s.config.DemoMode {
		return plan, errors.New("演示模式禁止删除")
	}
	if !projectNamePattern.MatchString(key) {
		return plan, errors.New("项目标识不合法")
	}
	files, err := s.discovery.RemovalInventory(ctx)
	if err != nil {
		return plan, err
	}
	var target *compose.RemovalFile
	for i := range files {
		if files[i].Key == key {
			if target != nil {
				return plan, errors.New("发现多个同名 Compose 项目，无法确定删除范围")
			}
			target = &files[i]
		}
	}
	if target == nil {
		return plan, errors.New("未定位唯一 Compose 文件，不能安全删除项目")
	}
	if s.discovery.ExcludedFromProjects(target.File) {
		return plan, errors.New("该 Compose 文件位于备份或非项目目录，不允许作为项目删除")
	}
	plan.Name, plan.File, plan.Directory = target.Name, target.File, target.WorkingDir
	plan.ConfigSHA = target.SHA
	resources, err := s.docker.RemovalResources(ctx)
	if err != nil {
		return plan, errors.New("无法读取完整 Docker 引用，已阻止删除")
	}
	images, err := s.docker.Images(ctx)
	if err != nil {
		return plan, errors.New("无法读取镜像引用，已阻止删除")
	}
	hostname, _ := os.Hostname()
	mapper := newRemovalPathMapper(resources.Containers, hostname)
	plan.HostDirectory, plan.HostFile = mapper.host(plan.Directory), mapper.host(plan.File)
	for _, c := range resources.Containers {
		if c.Labels["com.docker.compose.project"] != key {
			continue
		}
		if (len(hostname) >= 12 && strings.HasPrefix(c.ID, hostname)) || strings.Contains(c.Image, "/compose-manager:") {
			return plan, errors.New("禁止通过面板删除 Compose Manager 自身")
		}
		// A matching label alone must not allow removing containers from another directory.
		if working := c.Labels["com.docker.compose.project.working_dir"]; working != "" && mapper.host(working) != plan.HostDirectory {
			return plan, errors.New("容器的 Compose 目录与扫描结果不一致，已阻止删除")
		}
		if configs := c.Labels["com.docker.compose.project.config_files"]; configs != "" {
			for _, file := range strings.Split(configs, ",") {
				if mapper.host(strings.TrimSpace(file)) != plan.HostFile {
					return plan, errors.New("项目使用多个或不同的 Compose 文件，不能确认删除范围")
				}
			}
		}
		plan.Items = append(plan.Items, RemovalItem{Kind: "container", ID: c.ID, Name: strings.Join(c.Names, ", "), State: c.State})
		for _, mount := range c.Mounts {
			if mount.Type == "bind" && !compose.PathContains(plan.HostDirectory, mount.Source) {
				plan.Retained = append(plan.Retained, "目录外挂载保留："+mount.Source)
			}
		}
	}
	if _, err := s.guard.RemovalDirectory(plan.Directory); err != nil {
		plan.DirectoryBlocked = err.Error()
	}
	for _, protected := range []string{s.config.DataDir, s.config.BackupDir} {
		if protected == "" {
			continue
		}
		resolved, err := filepath.Abs(protected)
		if err == nil && compose.PathContains(plan.HostDirectory, mapper.host(resolved)) {
			plan.DirectoryBlocked = "目录包含管理面板数据或备份，禁止整体删除"
		}
	}
	for _, file := range files {
		if file.File != target.File {
			if compose.PathContains(plan.Directory, file.File) {
				plan.DirectoryBlocked = "目录包含其他 Compose 文件，禁止整体删除"
			}
			// A permission-limited path outside this project must not block
			// deleting the project itself. It does, however, keep image/volume
			// candidates below because their references cannot be fully checked.
			if file.Uncertain && pathsOverlap(plan.HostDirectory, mapper.host(file.File)) {
				plan.DirectoryBlocked = "其他 Compose 存在无法解析的挂载引用，不能确认目录独占"
			}
			for _, source := range file.Binds {
				if file.Key == mapper.project && mapper.scanRootMount(source, s.guard.Roots()) {
					continue
				}
				if pathsOverlap(plan.HostDirectory, mapper.host(source)) {
					plan.DirectoryBlocked = "其他 Compose 文件引用了该目录：" + file.Name
				}
			}
		}
	}
	for _, c := range resources.Containers {
		if c.Labels["com.docker.compose.project"] != key {
			for _, mount := range c.Mounts {
				if mount.Type != "bind" || !pathsOverlap(plan.HostDirectory, mount.Source) {
					continue
				}
				// The manager's scan-root mount is expected, but its data/config mounts are protected.
				manager := (len(hostname) >= 12 && strings.HasPrefix(c.ID, hostname)) || strings.Contains(c.Image, "/compose-manager:")
				scanMount := false
				for _, root := range s.guard.Roots() {
					if mount.Source == mapper.host(root) && mount.Destination == root {
						scanMount = true
					}
				}
				if !(manager && scanMount) {
					plan.DirectoryBlocked = "其他容器挂载了该目录：" + strings.Join(c.Names, ", ")
				}
			}
		}
	}
	if plan.DirectoryBlocked == "" {
		count := 0
		walkErr := filepath.WalkDir(plan.Directory, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			count++
			if count > 100000 {
				return errors.New("目录文件过多，不能安全删除")
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return errors.New("目录含符号链接，禁止整体删除")
			}
			if entry.IsDir() {
				resolved, err := filepath.EvalSymlinks(path)
				if err != nil || filepath.Clean(resolved) != filepath.Clean(path) {
					return errors.New("目录含链接或无法解析，禁止整体删除")
				}
			}
			if !entry.IsDir() && !entry.Type().IsRegular() {
				return errors.New("目录含特殊文件，禁止整体删除")
			}
			return nil
		})
		if walkErr != nil {
			plan.DirectoryBlocked = walkErr.Error()
		}
	}
	imageCandidates := map[string]bool{}
	for _, c := range resources.Containers {
		if c.Labels["com.docker.compose.project"] == key {
			imageCandidates[c.ImageID] = true
		}
	}
	for _, image := range images {
		for _, ref := range target.Images {
			if removalImageMatches(image.Repository+":"+image.Tag, image.Digest, ref) {
				imageCandidates[image.ID] = true
			}
		}
	}
	for id := range imageCandidates {
		item := RemovalItem{Kind: "image", ID: id, Name: id}
		for _, image := range images {
			if image.ID == id {
				item.Name = image.Repository + ":" + image.Tag + " (" + dockerapi.ShortID(id) + ")"
				for _, file := range files {
					if file.File == target.File {
						continue
					}
					if file.Uncertain {
						item.Reason = "其他 Compose 包含未解析的引用，已保留"
					}
					for _, ref := range file.Images {
						if strings.Contains(ref, "$") || removalImageMatches(image.Repository+":"+image.Tag, image.Digest, ref) {
							item.Reason = "其他 Compose 引用或引用含变量：" + file.Name
						}
					}
				}
			}
		}
		for _, c := range resources.Containers {
			if c.ImageID == id && c.Labels["com.docker.compose.project"] != key {
				item.Reason = "其他运行或停止容器仍在使用"
			}
		}
		plan.Items = append(plan.Items, item)
	}
	for _, volume := range resources.Volumes {
		if volume.Labels["com.docker.compose.project"] != key {
			for _, c := range resources.Containers {
				if c.Labels["com.docker.compose.project"] == key {
					for _, m := range c.Mounts {
						if m.Type == "volume" && m.Name == volume.Name {
							plan.Retained = append(plan.Retained, "外部或归属不明的数据卷保留："+volume.Name)
						}
					}
				}
			}
			continue
		}
		item := RemovalItem{Kind: "volume", ID: volume.Name, Name: volume.Name}
		for _, c := range resources.Containers {
			if c.Labels["com.docker.compose.project"] != key {
				for _, m := range c.Mounts {
					if m.Name == volume.Name {
						item.Reason = "其他容器正在引用"
					}
				}
			}
		}
		for _, file := range files {
			if file.File != target.File {
				if file.Uncertain {
					item.Reason = "其他 Compose 数据卷引用无法完全确认"
				}
				for _, v := range file.Volumes {
					if v == volume.Name {
						item.Reason = "其他 Compose 引用"
					}
				}
			}
		}
		plan.Items = append(plan.Items, item)
	}
	// Networks are intentionally retained: shared external networks must remain usable.
	for _, n := range resources.Networks {
		if n.Labels["com.docker.compose.project"] == key {
			plan.Retained = append(plan.Retained, "网络保留（可由 Docker 单独清理）："+n.Name)
		}
	}
	sort.Slice(plan.Items, func(i, j int) bool { a, b := plan.Items[i], plan.Items[j]; return a.Kind+a.ID < b.Kind+b.ID })
	sort.Strings(plan.Retained)
	raw, _ := json.Marshal(struct {
		Plan RemovalPlan
		SHA  string
	}{plan, target.SHA})
	sum := sha256.Sum256(raw)
	plan.Fingerprint = hex.EncodeToString(sum[:])
	return plan, nil
}

func pathsOverlap(a, b string) bool { return compose.PathContains(a, b) || compose.PathContains(b, a) }
func removalImageMatches(tag, digest, ref string) bool {
	return imageRefEqual(tag, ref) || (digest != "" && ref == digest)
}

func (s *Service) DeleteProject(ctx context.Context, key string, request RemovalRequest) (RemovalResult, error) {
	defer s.invalidateReadCaches()
	result := RemovalResult{Deleted: []string{}, Retained: []string{}, Errors: []string{}}
	if !s.removalGate.TryLock() {
		return result, errors.New("有其他操作正在执行，请稍后重试")
	}
	defer s.removalGate.Unlock()
	s.taskMu.RLock()
	updating := len(s.activeUpdates) > 0
	s.taskMu.RUnlock()
	if updating {
		return result, errors.New("更新任务尚未结束，暂不能删除")
	}
	s.removalTicketMu.Lock()
	ticket, ok := s.removalTickets[request.Token]
	delete(s.removalTickets, request.Token)
	s.removalTicketMu.Unlock()
	if !ok || ticket.Key != key || time.Now().After(ticket.Expires) {
		return result, errors.New("删除预览已失效，请重新打开确认")
	}
	plan, err := s.removalPlan(ctx, key)
	if err != nil {
		return result, err
	}
	if ticket.Fingerprint != plan.Fingerprint {
		return result, errors.New("项目或引用已变化，请重新预览，未执行删除")
	}
	if request.Name != plan.Name {
		return result, errors.New("输入的项目名称不匹配，未执行删除")
	}
	if request.Directory && plan.DirectoryBlocked != "" {
		return result, errors.New(plan.DirectoryBlocked)
	}
	if err := s.store.Audit(ctx, "compose.delete", "project", key, "started", "用户确认删除范围，服务端已重新检查"); err != nil {
		return result, errors.New("无法记录审计日志，未执行删除")
	}
	defer func() {
		status := "success"
		if len(result.Errors) > 0 {
			status = "partial"
		}
		raw, _ := json.Marshal(result)
		auditCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.store.Audit(auditCtx, "compose.delete", "project", key, status, string(raw))
	}()
	// Back up only the Compose source; application data and volumes are NOT backed up.
	data, err := os.ReadFile(plan.File)
	if err != nil {
		result.Errors = append(result.Errors, "无法备份 Compose 文件，已停止")
		return result, nil
	}
	backupDir := filepath.Join(s.config.BackupDir, "deleted-projects", request.Token)
	if err = os.MkdirAll(backupDir, 0700); err == nil {
		err = os.WriteFile(filepath.Join(backupDir, filepath.Base(plan.File)), data, 0600)
	}
	if err != nil {
		result.Errors = append(result.Errors, "Compose 备份失败，已停止")
		return result, nil
	}
	result.Backup = backupDir
	result.Retained = append(result.Retained, plan.Retained...)
	for _, item := range plan.Items {
		if item.Kind == "container" {
			// Stop only the exact IDs from the confirmed snapshot; no name-based force removal.
			err = nil
			if item.State == "running" || item.State == "restarting" || item.State == "paused" {
				err = s.docker.ContainerAction(ctx, item.ID, "stop")
			}
			if err == nil {
				err = s.docker.RemoveProjectResource(ctx, "container", item.ID)
			}
			if err != nil {
				result.Errors = append(result.Errors, "容器停止或移除失败："+item.Name+"；文件和后续资源未删除")
				return result, nil
			}
			result.Deleted = append(result.Deleted, "容器："+item.Name)
		}
	}
	// Recheck containers and directory ownership after stopping/removing containers.
	fresh, err := s.removalPlan(ctx, key)
	if err != nil {
		result.Errors = append(result.Errors, "删除文件前复查失败，已停止："+err.Error())
		return result, nil
	}
	if fresh.ConfigSHA != plan.ConfigSHA || fresh.File != plan.File || fresh.Directory != plan.Directory {
		result.Errors = append(result.Errors, "Compose 文件已变化，后续删除已停止")
		return result, nil
	}
	for _, item := range fresh.Items {
		if item.Kind == "container" {
			result.Errors = append(result.Errors, "检测到新容器，文件和后续资源未删除")
			return result, nil
		}
	}
	if request.Directory {
		if fresh.DirectoryBlocked != "" {
			result.Errors = append(result.Errors, fresh.DirectoryBlocked)
			return result, nil
		}
		err = s.guard.RemoveProjectDirectory(plan.Directory)
	} else {
		var path string
		path, err = s.guard.ResolveComposeFile(plan.File)
		if err == nil && path == plan.File {
			err = os.Remove(path)
		} else if err == nil {
			err = errors.New("文件路径已变化")
		}
	}
	if err != nil {
		result.Errors = append(result.Errors, "文件删除失败，可能有部分文件已删除；后续资源未删除")
		return result, nil
	}
	if request.Directory {
		result.Deleted = append(result.Deleted, "项目目录（含全部文件）："+plan.Directory)
	} else {
		result.Deleted = append(result.Deleted, "Compose 文件："+plan.File)
		result.Retained = append(result.Retained, "项目目录及其他文件："+plan.Directory)
	}
	for _, item := range plan.Items {
		if item.Kind == "container" {
			continue
		}
		selected := (item.Kind == "image" && request.Images) || (item.Kind == "volume" && request.Volumes)
		if !selected || item.Reason != "" {
			reason := item.Reason
			if reason == "" {
				reason = "未勾选删除"
			}
			result.Retained = append(result.Retained, item.Name+"："+reason)
			continue
		}
		// Revalidate all current references immediately before each resource deletion.
		if err = s.recheckRemovalResource(ctx, item); err != nil {
			result.Retained = append(result.Retained, item.Name+"："+err.Error())
			continue
		}
		if err = s.docker.RemoveProjectResource(ctx, item.Kind, item.ID); err != nil {
			result.Errors = append(result.Errors, "未能删除："+item.Name+"（Docker 拒绝或连接失败，未强制删除）")
			continue
		}
		label := "镜像"
		if item.Kind == "volume" {
			label = "数据卷"
		}
		result.Deleted = append(result.Deleted, label+"："+item.Name)
	}
	return result, nil
}

func (s *Service) recheckRemovalResource(ctx context.Context, item RemovalItem) error {
	resources, err := s.docker.RemovalResources(ctx)
	if err != nil {
		return errors.New("无法复查 Docker 引用，已保留")
	}
	files, err := s.discovery.RemovalInventory(ctx)
	if err != nil {
		return errors.New("无法复查 Compose 引用，已保留")
	}
	for _, c := range resources.Containers {
		if item.Kind == "image" && c.ImageID == item.ID {
			return errors.New("容器仍在使用，已保留")
		}
		if item.Kind == "volume" {
			for _, m := range c.Mounts {
				if m.Name == item.ID {
					return errors.New("数据卷仍被容器引用，已保留")
				}
			}
		}
	}
	if item.Kind == "image" {
		images, err := s.docker.Images(ctx)
		if err != nil {
			return err
		}
		for _, image := range images {
			if image.ID == item.ID {
				for _, file := range files {
					if file.Uncertain {
						return errors.New("其他 Compose 包含未解析的引用，已保留")
					}
					for _, ref := range file.Images {
						if strings.Contains(ref, "$") || removalImageMatches(image.Repository+":"+image.Tag, image.Digest, ref) {
							return errors.New("仍有 Compose 引用或变量，已保留")
						}
					}
				}
			}
		}
	} else {
		for _, file := range files {
			if file.Uncertain {
				return errors.New("数据卷引用无法完全确认，已保留")
			}
			for _, v := range file.Volumes {
				if v == item.ID {
					return errors.New("Compose 仍引用数据卷，已保留")
				}
			}
		}
	}
	return nil
}
