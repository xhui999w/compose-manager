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
var progressLineBreakPattern = regexp.MustCompile(`\r\n?|\n`)

type Runner struct {
	binary    string
	timeoutMu sync.RWMutex
	timeout   time.Duration
	locks     sync.Map
}

func NewRunner(timeout time.Duration) *Runner { return &Runner{binary: "docker", timeout: timeout} }

func (r *Runner) SetTimeout(timeout time.Duration) {
	if timeout <= 0 {
		return
	}
	r.timeoutMu.Lock()
	r.timeout = timeout
	r.timeoutMu.Unlock()
}

func (r *Runner) Timeout() time.Duration {
	r.timeoutMu.RLock()
	defer r.timeoutMu.RUnlock()
	return r.timeout
}

func (r *Runner) Run(ctx context.Context, projectKey, file, action, service string, tail int) (string, error) {
	return r.RunWithProgress(ctx, projectKey, file, action, service, tail, nil)
}

func (r *Runner) RunWithProgress(ctx context.Context, projectKey, file, action, service string, tail int, onOutput func(string)) (string, error) {
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
	timeout := r.Timeout()
	commandCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(commandCtx, r.binary, args...)
	output := &progressWriter{remaining: 1 << 20, onOutput: onOutput}
	cmd.Stdout = output
	cmd.Stderr = output
	err := cmd.Run()
	output.Flush()
	if commandCtx.Err() == context.DeadlineExceeded {
		return output.String(), fmt.Errorf("Compose action timed out after %s", timeout)
	}
	if err != nil {
		return output.String(), fmt.Errorf("Compose action failed: %w", err)
	}
	return output.String(), nil
}

type progressWriter struct {
	mu        sync.Mutex
	output    bytes.Buffer
	pending   string
	remaining int
	onOutput  func(string)
}

func (w *progressWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	original := len(p)
	if w.remaining <= 0 {
		return original, nil
	}
	if len(p) > w.remaining {
		p = p[:w.remaining]
	}
	w.remaining -= len(p)
	_, _ = w.output.Write(p)
	if w.onOutput != nil {
		w.pending += string(p)
		w.emitLines(false)
	}
	return original, nil
}

func (w *progressWriter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.emitLines(true)
}

func (w *progressWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.output.String()
}

func (w *progressWriter) emitLines(flush bool) {
	w.pending = progressLineBreakPattern.ReplaceAllString(w.pending, "\n")
	parts := bytes.Split([]byte(w.pending), []byte("\n"))
	limit := len(parts) - 1
	if flush {
		limit = len(parts)
	}
	for index := 0; index < limit; index++ {
		if line := string(parts[index]); line != "" {
			w.onOutput(line)
		}
	}
	if flush {
		w.pending = ""
	} else {
		w.pending = string(parts[len(parts)-1])
	}
}
