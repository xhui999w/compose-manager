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
				name := strings.ToLower(entry.Name())
				if path != root && (strings.HasPrefix(name, ".") || name == "node_modules" || name == "backups" || name == "vendor") {
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
