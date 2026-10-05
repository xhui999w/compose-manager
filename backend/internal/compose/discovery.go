package compose

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

type DiscoveredProject struct {
	Key        string
	Name       string
	File       string
	WorkingDir string
	Images     []string
	// Nested is true when the file lives below a directory that already has a
	// Compose file. NAS applications often keep old export/install files in
	// such nested directories; the service can hide those when no Docker
	// project with the same key exists, while still retaining them in the
	// deletion reference inventory.
	Nested bool
}

type composeDocument struct {
	Name     string `yaml:"name"`
	Services map[string]struct {
		Image string `yaml:"image"`
	} `yaml:"services"`
}

type Discovery struct {
	guard    *PathGuard
	maxDepth int
	cacheMu  sync.Mutex
	cached   []DiscoveredProject
	cachedAt time.Time
}

func NewDiscovery(guard *PathGuard) *Discovery { return &Discovery{guard: guard, maxDepth: 6} }

// CachedScan is for display only. Mutation targets and deletion inventories
// always use Scan / RemovalInventory, which do not consult this snapshot.
func (d *Discovery) CachedScan(ctx context.Context) ([]DiscoveredProject, error) {
	d.cacheMu.Lock()
	defer d.cacheMu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if d.cachedAt.IsZero() || time.Since(d.cachedAt) >= 30*time.Second {
		projects, err := d.Scan(ctx)
		if err != nil {
			return nil, err
		}
		d.cached, d.cachedAt = projects, time.Now()
	}
	result := append([]DiscoveredProject(nil), d.cached...)
	for i := range result {
		result[i].Images = append([]string(nil), result[i].Images...)
	}
	return result, nil
}

func (d *Discovery) Invalidate() {
	d.cacheMu.Lock()
	d.cached, d.cachedAt = nil, time.Time{}
	d.cacheMu.Unlock()
}

func (d *Discovery) Scan(ctx context.Context) ([]DiscoveredProject, error) {
	seen := map[string]DiscoveredProject{}
	for _, root := range d.guard.Roots() {
		rootDepth := strings.Count(filepath.Clean(root), string(filepath.Separator))
		walkErr := walkDiscovery(ctx, root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				if os.IsNotExist(err) || os.IsPermission(err) {
					return nil
				}
				return err
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
			if entry.IsDir() {
				if path != root && ignoredDiscoveryDir(entry.Name()) {
					return filepath.SkipDir
				}
				depth := strings.Count(filepath.Clean(path), string(filepath.Separator)) - rootDepth
				if depth > d.maxDepth {
					return filepath.SkipDir
				}
				return nil
			}
			if !isComposeFilename(entry.Name()) {
				return nil
			}
			resolved, err := d.guard.ResolveComposeFile(path)
			if err != nil {
				return nil
			}
			project, err := readDiscoveredProject(resolved)
			if err == nil {
				project.Nested = d.nestedUnderCompose(root, resolved)
				seen[resolved] = project
			}
			return nil
		})
		if walkErr != nil && !os.IsNotExist(walkErr) {
			return nil, walkErr
		}
	}
	result := make([]DiscoveredProject, 0, len(seen))
	for _, project := range seen {
		result = append(result, project)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Name == result[j].Name {
			return result[i].File < result[j].File
		}
		return result[i].Name < result[j].Name
	})
	return result, nil
}

func (d *Discovery) nestedUnderCompose(root, path string) bool {
	dir := filepath.Dir(filepath.Clean(path))
	root = filepath.Clean(root)
	for dir != root && PathContains(root, dir) {
		entries, err := os.ReadDir(dir)
		if err == nil {
			for _, entry := range entries {
				candidate := filepath.Join(dir, entry.Name())
				if candidate != filepath.Clean(path) && !entry.IsDir() && isComposeFilename(entry.Name()) {
					return true
				}
			}
		}
		next := filepath.Dir(dir)
		if next == dir {
			break
		}
		dir = next
	}
	return false
}

// WalkDir reads and sorts the entire directory before visiting it. Data folders
// can contain hundreds of thousands of entries; bounded batches keep the live
// allocation independent of directory width. Symlink directories are not followed.
func walkDiscovery(ctx context.Context, path string, visit fs.WalkDirFunc) error {
	info, err := os.Lstat(path)
	if err != nil {
		return visit(path, nil, err)
	}
	return walkDiscoveryEntry(ctx, path, fs.FileInfoToDirEntry(info), visit)
}

func walkDiscoveryEntry(ctx context.Context, path string, entry fs.DirEntry, visit fs.WalkDirFunc) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := visit(path, entry, nil); err != nil {
		if err == filepath.SkipDir && entry.IsDir() {
			return nil
		}
		return err
	}
	if !entry.IsDir() {
		return nil
	}
	directory, err := os.Open(path)
	if err != nil {
		return visit(path, entry, err)
	}
	defer directory.Close()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		batch, readErr := directory.ReadDir(64)
		for _, child := range batch {
			if err := walkDiscoveryEntry(ctx, filepath.Join(path, child.Name()), child, visit); err != nil {
				return err
			}
		}
		if readErr == io.EOF {
			return nil
		}
		if readErr != nil {
			return visit(path, entry, readErr)
		}
	}
}

// Backup snapshots may contain valid Compose files, but are not runnable projects.
// Keep this narrow so an ordinary project whose name contains "backup" stays visible.
func ignoredDiscoveryDir(name string) bool {
	name = strings.ToLower(name)
	if strings.HasPrefix(name, ".") {
		return true
	}
	// Build/export workflows commonly leave source snapshots beside the real
	// project. Their Compose files are reference material, not runnable projects.
	if strings.HasPrefix(name, "source-") {
		return true
	}
	switch name {
	case "node_modules", "backups", "vendor":
		return true
	}
	return strings.HasPrefix(name, "_") && (strings.Contains(name, "-backup-") || strings.Contains(name, "-preswitch-"))
}

// A hidden snapshot must not become a deletion target through a direct API call.
// An explicitly configured nested scan root still takes precedence.
func (d *Discovery) ExcludedFromProjects(path string) bool {
	for _, root := range d.guard.Roots() {
		if !PathContains(root, path) {
			continue
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			continue
		}
		parts := strings.Split(relative, string(filepath.Separator))
		ignored := false
		for _, part := range parts[:len(parts)-1] {
			if ignoredDiscoveryDir(part) {
				ignored = true
				break
			}
		}
		if !ignored {
			return false
		}
	}
	return true
}

func readDiscoveredProject(path string) (DiscoveredProject, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return DiscoveredProject{}, err
	}
	var document composeDocument
	if err := yaml.Unmarshal(data, &document); err != nil {
		return DiscoveredProject{}, err
	}
	name := strings.TrimSpace(document.Name)
	if name == "" {
		name = filepath.Base(filepath.Dir(path))
	}
	key := normalizeProjectKey(name)
	images := make([]string, 0, len(document.Services))
	for _, service := range document.Services {
		if image := strings.TrimSpace(service.Image); image != "" {
			images = append(images, image)
		}
	}
	sort.Strings(images)
	return DiscoveredProject{Key: key, Name: name, File: path, WorkingDir: filepath.Dir(path), Images: images}, nil
}

func normalizeProjectKey(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	var builder strings.Builder
	for _, char := range name {
		if (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') || char == '-' || char == '_' || char == '.' {
			builder.WriteRune(char)
		} else if builder.Len() > 0 {
			builder.WriteByte('-')
		}
	}
	result := strings.Trim(builder.String(), "-_.")
	if result == "" {
		hash := sha256.Sum256([]byte(name))
		result = "project-" + hex.EncodeToString(hash[:4])
	}
	if len(result) > 128 {
		result = result[:128]
	}
	return result
}
