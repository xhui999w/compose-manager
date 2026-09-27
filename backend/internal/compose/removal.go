package compose

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type RemovalFile struct {
	DiscoveredProject
	SHA            string
	Binds, Volumes []string
	Uncertain      bool
}

const maxRemovalScanEntries = 500000

// Deletion must fail closed; the normal discovery scan tolerates errors for display.
func (d *Discovery) RemovalInventory(ctx context.Context) ([]RemovalFile, error) {
	result := []RemovalFile{}
	seen := map[string]bool{}
	count := 0
	for _, root := range d.guard.Roots() {
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			count++
			if count > maxRemovalScanEntries {
				return errors.New("扫描文件过多，无法安全确认引用")
			}
			if entry.IsDir() {
				if path != root && (strings.HasPrefix(entry.Name(), ".") || entry.Name() == "backups" || entry.Name() == "node_modules" || entry.Name() == "vendor") {
					return filepath.SkipDir
				}
				return nil
			}
			if !isComposeFilename(entry.Name()) {
				return nil
			}
			resolved, err := d.guard.ResolveComposeFile(path)
			if err != nil {
				return err
			}
			if seen[resolved] {
				return nil
			}
			seen[resolved] = true
			p, err := readDiscoveredProject(resolved)
			if err != nil {
				return fmt.Errorf("无法解析 Compose 文件：%s", resolved)
			}
			data, err := os.ReadFile(resolved)
			if err != nil {
				return err
			}
			var doc struct {
				Services map[string]struct {
					Volumes []yaml.Node `yaml:"volumes"`
				} `yaml:"services"`
				Volumes map[string]struct {
					Name     string    `yaml:"name"`
					External yaml.Node `yaml:"external"`
				} `yaml:"volumes"`
			}
			if err := yaml.Unmarshal(data, &doc); err != nil {
				return err
			}
			sum := sha256.Sum256(data)
			file := RemovalFile{DiscoveredProject: p, SHA: hex.EncodeToString(sum[:])}
			for _, service := range doc.Services {
				for _, mount := range service.Volumes {
					var source, kind string
					if mount.Kind == yaml.ScalarNode {
						parts := strings.Split(mount.Value, ":")
						if len(parts) < 2 {
							continue
						}
						source = parts[0]
						if strings.HasPrefix(source, ".") || filepath.IsAbs(source) {
							kind = "bind"
						} else {
							kind = "volume"
						}
					} else {
						var value struct{ Type, Source string }
						if mount.Decode(&value) != nil {
							file.Uncertain = true
							continue
						}
						kind, source = value.Type, value.Source
					}
					if strings.Contains(source, "$") {
						file.Uncertain = true
						continue
					}
					if kind == "bind" {
						if !filepath.IsAbs(source) {
							source = filepath.Join(p.WorkingDir, source)
						}
						file.Binds = append(file.Binds, filepath.Clean(source))
					}
					if kind == "volume" {
						definition := doc.Volumes[source]
						if definition.Name != "" {
							source = definition.Name
						} else if definition.External.Kind == yaml.ScalarNode && definition.External.Value == "true" { /* external name is literal */
						} else if definition.External.Kind != 0 && definition.External.Kind != yaml.ScalarNode {
							file.Uncertain = true
						} else {
							source = p.Key + "_" + source
						}
						if strings.Contains(source, "$") {
							file.Uncertain = true
						}
						file.Volumes = append(file.Volumes, source)
					}
				}
			}
			// Indirections need full Compose evaluation; do not infer safe ownership.
			var generic map[string]any
			if yaml.Unmarshal(data, &generic) == nil {
				if generic["include"] != nil {
					file.Uncertain = true
				}
			}
			if strings.Contains(string(data), "extends:") {
				file.Uncertain = true
			}
			result = append(result, file)
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("无法完成安全扫描：%w", err)
		}
	}
	return result, nil
}

func PathContains(parent, child string) bool {
	rel, err := filepath.Rel(filepath.Clean(parent), filepath.Clean(child))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// Only a real project subdirectory is eligible. Root and nested roots are protected.
func (g *PathGuard) RemovalDirectory(path string) (string, error) {
	resolved, err := g.Resolve(path)
	if err != nil {
		return "", err
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if filepath.Clean(absolute) != resolved {
		return "", errors.New("符号链接目录不能整体删除")
	}
	for _, root := range g.Roots() {
		if PathContains(resolved, root) {
			return "", errors.New("禁止删除 Docker 根目录或包含扫描根目录的目录")
		}
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", errors.New("项目目录不存在")
	}
	if err := checkRemovalMounts(resolved); err != nil {
		return "", err
	}
	return resolved, nil
}

// No shell, no following symlinks. The caller must revalidate ownership first.
func (g *PathGuard) RemoveProjectDirectory(path string) error {
	resolved, err := g.RemovalDirectory(path)
	if err != nil {
		return err
	}
	return os.RemoveAll(resolved)
}
