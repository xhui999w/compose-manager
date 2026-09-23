package compose

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var ErrPathOutsideRoots = errors.New("path is outside allowed Compose roots")

type PathGuard struct{ roots []string }

func NewPathGuard(roots []string) (*PathGuard, error) {
	resolved := make([]string, 0, len(roots))
	for _, root := range roots {
		absolute, err := filepath.Abs(root)
		if err != nil {
			return nil, err
		}
		absolute = filepath.Clean(absolute)
		if evaluated, err := filepath.EvalSymlinks(absolute); err == nil {
			absolute = evaluated
		}
		resolved = append(resolved, absolute)
	}
	if len(resolved) == 0 {
		return nil, errors.New("at least one Compose root is required")
	}
	return &PathGuard{roots: resolved}, nil
}

func (g *PathGuard) Roots() []string { return append([]string(nil), g.roots...) }

func (g *PathGuard) Resolve(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	absolute = filepath.Clean(absolute)
	absolute, err = evalSymlinksWithMissingTail(absolute)
	if err != nil {
		return "", err
	}
	for _, root := range g.roots {
		relative, err := filepath.Rel(root, absolute)
		if err != nil {
			continue
		}
		if relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative) {
			return absolute, nil
		}
	}
	return "", fmt.Errorf("%w: %s", ErrPathOutsideRoots, path)
}

func evalSymlinksWithMissingTail(path string) (string, error) {
	current := path
	missing := []string{}
	for {
		evaluated, err := filepath.EvalSymlinks(current)
		if err == nil {
			for index := len(missing) - 1; index >= 0; index-- {
				evaluated = filepath.Join(evaluated, missing[index])
			}
			return filepath.Clean(evaluated), nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", err
		}
		missing = append(missing, filepath.Base(current))
		current = parent
	}
}

func (g *PathGuard) ResolveComposeFile(path string) (string, error) {
	resolved, err := g.Resolve(path)
	if err != nil {
		return "", err
	}
	if !isComposeFilename(filepath.Base(resolved)) {
		return "", fmt.Errorf("unsupported Compose filename %q", filepath.Base(resolved))
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("Compose path is not a regular file")
	}
	return resolved, nil
}

func isComposeFilename(name string) bool {
	switch strings.ToLower(name) {
	case "compose.yaml", "compose.yml", "docker-compose.yaml", "docker-compose.yml":
		return true
	default:
		return false
	}
}
