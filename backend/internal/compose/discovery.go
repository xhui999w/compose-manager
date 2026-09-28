package compose

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type DiscoveredProject struct {
	Key        string
	Name       string
	File       string
	WorkingDir string
	Images     []string
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
}

func NewDiscovery(guard *PathGuard) *Discovery { return &Discovery{guard: guard, maxDepth: 6} }

func (d *Discovery) Scan(ctx context.Context) ([]DiscoveredProject, error) {
	seen := map[string]DiscoveredProject{}
	for _, root := range d.guard.Roots() {
		rootDepth := strings.Count(filepath.Clean(root), string(filepath.Separator))
		walkErr := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
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
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, nil
}

// Backup snapshots may contain valid Compose files, but are not runnable projects.
// Keep this narrow so an ordinary project whose name contains "backup" stays visible.
func ignoredDiscoveryDir(name string) bool {
	name = strings.ToLower(name)
	if strings.HasPrefix(name, ".") {
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
