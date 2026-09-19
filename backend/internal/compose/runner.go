package compose

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"sync"
	"time"
)

var identifierPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$`)

type Runner struct {
	binary  string
	timeout time.Duration
	locks   sync.Map
}

func NewRunner(timeout time.Duration) *Runner { return &Runner{binary: "docker", timeout: timeout} }

func (r *Runner) Run(ctx context.Context, projectKey, file, action, service string, tail int) (string, error) {
	if !identifierPattern.MatchString(projectKey) {
		return "", errors.New("invalid project identifier")
	}
	if service != "" && !identifierPattern.MatchString(service) {
		return "", errors.New("invalid service identifier")
	}
	args := []string{"compose", "-f", file, "--project-directory", filepath.Dir(file), "--project-name", projectKey}
	switch action {
	case "start", "apply":
		args = append(args, "up", "-d")
	case "stop":
		args = append(args, "stop")
	case "restart":
		args = append(args, "restart")
	case "pull":
		args = append(args, "pull")
		if service != "" {
			args = append(args, service)
		}
	case "logs":
		if tail < 1 {
			tail = 200
		}
		if tail > 5000 {
			tail = 5000
		}
		args = append(args, "logs", "--no-color", "--tail", strconv.Itoa(tail))
		if service != "" {
			args = append(args, service)
		}
	case "config":
		args = append(args, "config", "--quiet")
	default:
		return "", fmt.Errorf("unsupported Compose action %q", action)
	}
	lockValue, _ := r.locks.LoadOrStore(projectKey, &sync.Mutex{})
	lock := lockValue.(*sync.Mutex)
	lock.Lock()
	defer lock.Unlock()
	commandCtx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	cmd := exec.CommandContext(commandCtx, r.binary, args...)
	var output bytes.Buffer
	cmd.Stdout = &limitedWriter{writer: &output, remaining: 1 << 20}
	cmd.Stderr = &limitedWriter{writer: &output, remaining: 1 << 20}
	err := cmd.Run()
	if commandCtx.Err() == context.DeadlineExceeded {
		return output.String(), fmt.Errorf("Compose action timed out after %s", r.timeout)
	}
	if err != nil {
		return output.String(), fmt.Errorf("Compose action failed: %w", err)
	}
	return output.String(), nil
}

type limitedWriter struct {
	writer    *bytes.Buffer
	remaining int
}

func (w *limitedWriter) Write(p []byte) (int, error) {
	original := len(p)
	if w.remaining <= 0 {
		return original, nil
	}
	if len(p) > w.remaining {
		p = p[:w.remaining]
	}
	w.remaining -= len(p)
	_, _ = w.writer.Write(p)
	return original, nil
}
