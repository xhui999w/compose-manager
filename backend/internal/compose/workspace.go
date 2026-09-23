package compose

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const maxWorkspaceFileSize = 2 << 20

var ErrWorkspaceConflict = errors.New("workspace file changed since it was opened")

type Workspace struct {
	guard     *PathGuard
	runner    workspaceRunner
	backupDir string
}

type workspaceRunner interface {
	Run(context.Context, string, string, string, string, int) (string, error)
}

type WorkspaceRoot struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Path string `json:"path"`
}

type WorkspaceEntry struct {
	Name        string    `json:"name"`
	Path        string    `json:"path"`
	Kind        string    `json:"kind"`
	Size        int64     `json:"size"`
	ModifiedAt  time.Time `json:"modifiedAt"`
	Editable    bool      `json:"editable"`
	ComposeFile bool      `json:"composeFile"`
}

type WorkspaceFile struct {
	RootID  int    `json:"rootId"`
	Path    string `json:"path"`
	Content string `json:"content"`
	SHA256  string `json:"sha256"`
}

type CreatedProject struct {
	Key       string `json:"key"`
	Name      string `json:"name"`
	RootID    int    `json:"rootId"`
	Directory string `json:"directory"`
	File      string `json:"file"`
	Output    string `json:"output,omitempty"`
}

func NewWorkspace(guard *PathGuard, runner workspaceRunner, backupDir string) *Workspace {
	return &Workspace{guard: guard, runner: runner, backupDir: backupDir}
}

func (w *Workspace) Roots() []WorkspaceRoot {
	roots := w.guard.Roots()
	result := make([]WorkspaceRoot, 0, len(roots))
	for index, root := range roots {
		name := filepath.Base(root)
		if name == "." || name == string(filepath.Separator) {
			name = root
		}
		result = append(result, WorkspaceRoot{ID: index, Name: name, Path: root})
	}
	return result
}

func (w *Workspace) List(rootID int, relative string) ([]WorkspaceEntry, error) {
	resolved, cleaned, err := w.resolve(rootID, relative)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, errors.New("workspace path is not a directory")
	}
	entries, err := os.ReadDir(resolved)
	if err != nil {
		return nil, err
	}
	result := make([]WorkspaceEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		entryInfo, infoErr := entry.Info()
		if infoErr != nil {
			continue
		}
		kind := "file"
		if entryInfo.IsDir() {
			kind = "directory"
		} else if !entryInfo.Mode().IsRegular() {
			continue
		}
		path := filepath.ToSlash(filepath.Join(cleaned, entry.Name()))
		if cleaned == "" {
			path = filepath.ToSlash(entry.Name())
		}
		result = append(result, WorkspaceEntry{Name: entry.Name(), Path: path, Kind: kind, Size: entryInfo.Size(), ModifiedAt: entryInfo.ModTime(), Editable: kind == "file" && isEditableWorkspaceFile(entry.Name()), ComposeFile: kind == "file" && isComposeFilename(entry.Name())})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Kind != result[j].Kind {
			return result[i].Kind == "directory"
		}
		return strings.ToLower(result[i].Name) < strings.ToLower(result[j].Name)
	})
	return result, nil
}

func (w *Workspace) ReadFile(rootID int, relative string) (WorkspaceFile, error) {
	resolved, cleaned, err := w.resolve(rootID, relative)
	if err != nil {
		return WorkspaceFile{}, err
	}
	if !isEditableWorkspaceFile(filepath.Base(resolved)) {
		return WorkspaceFile{}, errors.New("this file type is read-only")
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return WorkspaceFile{}, err
	}
	if !info.Mode().IsRegular() {
		return WorkspaceFile{}, errors.New("workspace path is not a regular file")
	}
	if info.Size() > maxWorkspaceFileSize {
		return WorkspaceFile{}, fmt.Errorf("file exceeds %d MiB limit", maxWorkspaceFileSize>>20)
	}
	data, err := os.ReadFile(resolved)
	if err != nil {
		return WorkspaceFile{}, err
	}
	if strings.IndexByte(string(data), 0) >= 0 {
		return WorkspaceFile{}, errors.New("binary files cannot be edited")
	}
	return WorkspaceFile{RootID: rootID, Path: filepath.ToSlash(cleaned), Content: string(data), SHA256: workspaceHash(data)}, nil
}

func (w *Workspace) SaveFile(ctx context.Context, rootID int, relative, content, baseSHA string) (WorkspaceFile, error) {
	if len(content) > maxWorkspaceFileSize || strings.IndexByte(content, 0) >= 0 {
		return WorkspaceFile{}, errors.New("file content is invalid or too large")
	}
	current, err := w.ReadFile(rootID, relative)
	if err != nil {
		return WorkspaceFile{}, err
	}
	if baseSHA == "" || current.SHA256 != baseSHA {
		return WorkspaceFile{}, ErrWorkspaceConflict
	}
	resolved, _, err := w.resolve(rootID, relative)
	if err != nil {
		return WorkspaceFile{}, err
	}
	if isComposeFilename(filepath.Base(resolved)) {
		if err := validateComposeDocument(content); err != nil {
			return WorkspaceFile{}, err
		}
		projectKey := normalizeProjectKey(filepath.Base(filepath.Dir(resolved)))
		temp, tempErr := writeWorkspaceTemp(resolved, content)
		if tempErr != nil {
			return WorkspaceFile{}, tempErr
		}
		defer os.Remove(temp)
		if _, err := w.runner.Run(ctx, projectKey, temp, "config", "", 0); err != nil {
			return WorkspaceFile{}, err
		}
	}
	if err := w.backup(rootID, current, resolved); err != nil {
		return WorkspaceFile{}, err
	}
	temp, err := writeWorkspaceTemp(resolved, content)
	if err != nil {
		return WorkspaceFile{}, err
	}
	defer os.Remove(temp)
	if err := atomicReplace(temp, resolved); err != nil {
		return WorkspaceFile{}, fmt.Errorf("atomic replace failed: %w", err)
	}
	return WorkspaceFile{RootID: rootID, Path: current.Path, Content: content, SHA256: workspaceHash([]byte(content))}, nil
}

func (w *Workspace) CreateDirectory(rootID int, relative string) error {
	resolved, cleaned, err := w.resolve(rootID, relative)
	if err != nil {
		return err
	}
	if cleaned == "" {
		return errors.New("directory name is required")
	}
	return os.MkdirAll(resolved, 0o750)
}

func (w *Workspace) CreateProject(ctx context.Context, rootID int, directory, name, content string, apply bool) (CreatedProject, error) {
	name = strings.TrimSpace(name)
	if !identifierPattern.MatchString(name) || name != strings.ToLower(name) {
		return CreatedProject{}, errors.New("project name may contain only lowercase letters, numbers, dots, underscores and hyphens")
	}
	if err := validateComposeDocument(content); err != nil {
		return CreatedProject{}, err
	}
	targetDir, cleaned, err := w.resolve(rootID, directory)
	if err != nil {
		return CreatedProject{}, err
	}
	if cleaned == "" {
		return CreatedProject{}, errors.New("project directory is required")
	}
	if _, statErr := os.Stat(targetDir); statErr == nil {
		return CreatedProject{}, errors.New("project directory already exists")
	} else if !os.IsNotExist(statErr) {
		return CreatedProject{}, statErr
	}
	if err := os.MkdirAll(targetDir, 0o750); err != nil {
		return CreatedProject{}, err
	}
	createdDir := true
	defer func() {
		if createdDir {
			_ = os.Remove(targetDir)
		}
	}()
	file := filepath.Join(targetDir, "compose.yaml")
	temp, err := writeWorkspaceTemp(file, content)
	if err != nil {
		return CreatedProject{}, err
	}
	defer os.Remove(temp)
	if _, err := w.runner.Run(ctx, name, temp, "config", "", 0); err != nil {
		return CreatedProject{}, err
	}
	if err := atomicReplace(temp, file); err != nil {
		return CreatedProject{}, fmt.Errorf("create Compose file: %w", err)
	}
	createdDir = false
	result := CreatedProject{Key: name, Name: name, RootID: rootID, Directory: filepath.ToSlash(cleaned), File: filepath.ToSlash(filepath.Join(cleaned, "compose.yaml"))}
	if apply {
		result.Output, err = w.runner.Run(ctx, name, file, "apply", "", 0)
	}
	return result, err
}

func (w *Workspace) resolve(rootID int, relative string) (string, string, error) {
	roots := w.guard.Roots()
	if rootID < 0 || rootID >= len(roots) {
		return "", "", errors.New("invalid workspace root")
	}
	cleaned, err := cleanWorkspaceRelative(relative)
	if err != nil {
		return "", "", err
	}
	resolved, err := w.guard.Resolve(filepath.Join(roots[rootID], cleaned))
	if err != nil {
		return "", "", err
	}
	return resolved, cleaned, nil
}

func (w *Workspace) backup(rootID int, current WorkspaceFile, resolved string) error {
	directory := filepath.Join(w.backupDir, "workspace", fmt.Sprintf("root-%d", rootID), filepath.Dir(filepath.FromSlash(current.Path)))
	if err := os.MkdirAll(directory, 0o750); err != nil {
		return err
	}
	name := filepath.Base(resolved) + "." + time.Now().UTC().Format("20060102T150405.000000000Z") + "." + current.SHA256[:12] + ".bak"
	return os.WriteFile(filepath.Join(directory, name), []byte(current.Content), 0o600)
}

func cleanWorkspaceRelative(value string) (string, error) {
	if strings.IndexByte(value, 0) >= 0 || filepath.IsAbs(value) {
		return "", errors.New("workspace path must be relative")
	}
	cleaned := filepath.Clean(strings.TrimSpace(value))
	if cleaned == "." || cleaned == "" {
		return "", nil
	}
	if cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", ErrPathOutsideRoots
	}
	return cleaned, nil
}

func isEditableWorkspaceFile(name string) bool {
	if isComposeFilename(name) || strings.EqualFold(name, ".env") || strings.EqualFold(name, "Dockerfile") {
		return true
	}
	switch strings.ToLower(filepath.Ext(name)) {
	case ".yaml", ".yml", ".json", ".toml", ".ini", ".conf", ".cfg", ".properties", ".txt":
		return true
	default:
		return false
	}
}

func validateComposeDocument(content string) error {
	var node yaml.Node
	if err := yaml.Unmarshal([]byte(content), &node); err != nil {
		return fmt.Errorf("YAML validation failed: %w", err)
	}
	var document composeDocument
	if err := yaml.Unmarshal([]byte(content), &document); err != nil {
		return fmt.Errorf("Compose structure validation failed: %w", err)
	}
	if len(document.Services) == 0 {
		return errors.New("Compose file must define at least one service")
	}
	return nil
}

func writeWorkspaceTemp(destination, content string) (string, error) {
	temp, err := os.CreateTemp(filepath.Dir(destination), ".cm-workspace-*")
	if err != nil {
		return "", err
	}
	path := temp.Name()
	mode := os.FileMode(0o600)
	if info, statErr := os.Stat(destination); statErr == nil {
		mode = info.Mode().Perm()
	}
	if err := temp.Chmod(mode); err != nil {
		temp.Close()
		os.Remove(path)
		return "", err
	}
	if _, err := io.WriteString(temp, content); err != nil {
		temp.Close()
		os.Remove(path)
		return "", err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		os.Remove(path)
		return "", err
	}
	if err := temp.Close(); err != nil {
		os.Remove(path)
		return "", err
	}
	return path, nil
}

func workspaceHash(data []byte) string {
	value := sha256.Sum256(data)
	return hex.EncodeToString(value[:])
}
