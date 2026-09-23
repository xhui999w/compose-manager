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
	"strings"
	"time"

	"github.com/compose-manager/compose-manager/backend/internal/model"
	"github.com/compose-manager/compose-manager/backend/internal/store"
	"github.com/pmezard/go-difflib/difflib"
)

var ErrConflict = errors.New("Compose file changed since it was opened")

type Editor struct {
	guard     *PathGuard
	runner    *Runner
	store     *store.Store
	backupDir string
}

type FileContent struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	SHA256  string `json:"sha256"`
}

type SaveResult struct {
	FileContent
	Diff    string               `json:"diff"`
	Version model.ComposeVersion `json:"version"`
	Output  string               `json:"output,omitempty"`
}

func NewEditor(guard *PathGuard, runner *Runner, store *store.Store, backupDir string) *Editor {
	return &Editor{guard: guard, runner: runner, store: store, backupDir: backupDir}
}

func (e *Editor) Read(path string) (FileContent, error) {
	resolved, err := e.guard.ResolveComposeFile(path)
	if err != nil {
		return FileContent{}, err
	}
	data, err := os.ReadFile(resolved)
	if err != nil {
		return FileContent{}, err
	}
	return FileContent{Path: resolved, Content: string(data), SHA256: hash(data)}, nil
}

func (e *Editor) Validate(ctx context.Context, projectKey, originalPath, content string) (string, error) {
	resolved, err := e.guard.ResolveComposeFile(originalPath)
	if err != nil {
		return "", err
	}
	if err := validateComposeDocument(content); err != nil {
		return "", err
	}
	temp, err := os.CreateTemp(filepath.Dir(resolved), ".cm-candidate-*.yaml")
	if err != nil {
		return "", err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	mode := os.FileMode(0o600)
	if info, statErr := os.Stat(resolved); statErr == nil {
		mode = info.Mode().Perm()
	}
	if err := temp.Chmod(mode); err != nil {
		temp.Close()
		return "", err
	}
	if _, err := io.WriteString(temp, content); err != nil {
		temp.Close()
		return "", err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return "", err
	}
	if err := temp.Close(); err != nil {
		return "", err
	}
	return e.runner.Run(ctx, projectKey, tempPath, "config", "", 0)
}

func (e *Editor) Diff(path, content string) (string, error) {
	current, err := e.Read(path)
	if err != nil {
		return "", err
	}
	return unifiedDiff(current.Content, content), nil
}

func (e *Editor) Save(ctx context.Context, projectKey, path, content, baseSHA string, apply bool) (SaveResult, error) {
	current, err := e.Read(path)
	if err != nil {
		return SaveResult{}, err
	}
	if baseSHA == "" || current.SHA256 != baseSHA {
		return SaveResult{}, ErrConflict
	}
	if _, err := e.Validate(ctx, projectKey, current.Path, content); err != nil {
		return SaveResult{}, err
	}
	version, err := e.backup(ctx, projectKey, current)
	if err != nil {
		return SaveResult{}, err
	}
	temp, err := os.CreateTemp(filepath.Dir(current.Path), ".cm-save-*.yaml")
	if err != nil {
		return SaveResult{}, err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	mode := os.FileMode(0o600)
	if info, statErr := os.Stat(current.Path); statErr == nil {
		mode = info.Mode().Perm()
	}
	if err := temp.Chmod(mode); err != nil {
		temp.Close()
		return SaveResult{}, err
	}
	if _, err := io.WriteString(temp, content); err != nil {
		temp.Close()
		return SaveResult{}, err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return SaveResult{}, err
	}
	if err := temp.Close(); err != nil {
		return SaveResult{}, err
	}
	if err := atomicReplace(tempPath, current.Path); err != nil {
		return SaveResult{}, fmt.Errorf("atomic replace failed: %w", err)
	}
	result := SaveResult{FileContent: FileContent{Path: current.Path, Content: content, SHA256: hash([]byte(content))}, Diff: unifiedDiff(current.Content, content), Version: version}
	if apply {
		result.Output, err = e.runner.Run(ctx, projectKey, current.Path, "apply", "", 0)
		if err != nil {
			return result, err
		}
	}
	return result, nil
}

func (e *Editor) Restore(ctx context.Context, projectKey string, id int64, currentSHA string, apply bool) (SaveResult, error) {
	version, err := e.store.Version(ctx, projectKey, id)
	if err != nil {
		return SaveResult{}, err
	}
	backup, err := os.ReadFile(version.BackupPath)
	if err != nil {
		return SaveResult{}, err
	}
	return e.Save(ctx, projectKey, version.FilePath, string(backup), currentSHA, apply)
}

func (e *Editor) backup(ctx context.Context, projectKey string, current FileContent) (model.ComposeVersion, error) {
	dir := filepath.Join(e.backupDir, safeSegment(projectKey))
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return model.ComposeVersion{}, err
	}
	name := time.Now().UTC().Format("20060102T150405.000000000Z") + "-" + current.SHA256[:12] + ".yaml"
	backupPath := filepath.Join(dir, name)
	if err := os.WriteFile(backupPath, []byte(current.Content), 0o600); err != nil {
		return model.ComposeVersion{}, err
	}
	return e.store.AddVersion(ctx, model.ComposeVersion{ProjectKey: projectKey, FilePath: current.Path, SHA256: current.SHA256, BackupPath: backupPath})
}

func hash(data []byte) string {
	value := sha256.Sum256(data)
	return hex.EncodeToString(value[:])
}

func unifiedDiff(before, after string) string {
	diff := difflib.UnifiedDiff{A: difflib.SplitLines(before), B: difflib.SplitLines(after), FromFile: "before", ToFile: "after", Context: 3}
	text, _ := difflib.GetUnifiedDiffString(diff)
	return text
}

func safeSegment(value string) string {
	value = normalizeProjectKey(value)
	return strings.Trim(value, ".")
}
