package daemon

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/racterub/gobrrr/internal/config"
	"github.com/racterub/gobrrr/internal/identity"
	"github.com/racterub/gobrrr/internal/memory"
)

// WarmWorker manages a persistent Claude process for low-latency task dispatch.
type WarmWorker struct {
	mu       sync.Mutex
	id       int
	cmd      *exec.Cmd
	stdin    io.WriteCloser
	scanner  *bufio.Scanner
	busy     bool
	ready    bool
	gobrrDir string
	cfg      *config.Config
	memStore *memory.Store
	command  string // claude binary path, overridable for tests
}

// NewWarmWorker creates a WarmWorker. It is not started until Start() is called.
func NewWarmWorker(id int, gobrrDir string, cfg *config.Config, ms *memory.Store) *WarmWorker {
	return &WarmWorker{
		id:       id,
		gobrrDir: gobrrDir,
		cfg:      cfg,
		memStore: ms,
		command:  "claude",
	}
}

// Available returns true if the worker is ready and not busy.
func (ww *WarmWorker) Available() bool {
	ww.mu.Lock()
	defer ww.mu.Unlock()
	return ww.ready && !ww.busy
}

// Reserve atomically checks availability and marks the worker as busy.
// Returns true if the worker was reserved, false if unavailable.
func (ww *WarmWorker) Reserve() bool {
	ww.mu.Lock()
	defer ww.mu.Unlock()
	if !ww.ready || ww.busy {
		return false
	}
	ww.busy = true
	return true
}

// Release marks the worker as no longer busy.
func (ww *WarmWorker) Release() {
	ww.mu.Lock()
	defer ww.mu.Unlock()
	ww.busy = false
}

// Start spawns the Claude process, performs the init handshake, and injects
// identity. The worker is ready for task dispatch after Start returns nil.
func (ww *WarmWorker) Start(ctx context.Context) error {
	ww.mu.Lock()
	defer ww.mu.Unlock()

	workDir := ww.gobrrDir
	if ww.cfg != nil && ww.cfg.WorkspacePath != "" {
		workDir = ww.cfg.WorkspacePath
	}

	settingsPath, model, mode, err := resolveWarmArgs(ww.gobrrDir, ww.cfg)
	if err != nil {
		return fmt.Errorf("warm worker %d: ensure warm settings: %w", ww.id, err)
	}

	var cmd *exec.Cmd
	if ww.command != "claude" {
		// Test mode: run mock script with the same flags so argv-capture tests work.
		cmd = exec.Command("bash", ww.command, //nolint:gosec
			"--model", model,
			"--permission-mode", mode,
			"--settings", settingsPath,
		)
	} else {
		cmd = exec.Command("claude", "-p",
			"--model", model,
			"--permission-mode", mode,
			"--settings", settingsPath,
			"--input-format", "stream-json",
			"--output-format", "stream-json",
			"--verbose",
		)
	}
	cmd.Dir = workDir

	logDir := filepath.Join(ww.gobrrDir, "logs")
	if err := os.MkdirAll(logDir, 0700); err != nil {
		log.Printf("warm worker %d: stderr log dir unavailable: %v", ww.id, err)
	} else {
		logPath := filepath.Join(logDir, fmt.Sprintf("warm-%d.log", ww.id))
		if logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600); err != nil {
			log.Printf("warm worker %d: stderr log open failed: %v", ww.id, err)
		} else {
			cmd.Stderr = logFile
			// logFile stays open for the lifetime of the process. It is closed
			// when the process exits via Go's process-cleanup reaper.
		}
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("warm worker %d: stdin pipe: %w", ww.id, err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("warm worker %d: stdout pipe: %w", ww.id, err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("warm worker %d: start: %w", ww.id, err)
	}

	ww.cmd = cmd
	ww.stdin = stdin
	ww.scanner = bufio.NewScanner(stdout)
	ww.scanner.Buffer(make([]byte, 0, 4096), 10*1024*1024) // max 10MB per line

	// Send identity as the first message. Claude does not emit system/init
	// until it receives the first stdin message, so we must write before reading.
	ident, err := identity.Load(ww.gobrrDir)
	if err != nil {
		ww.killLocked()
		return fmt.Errorf("warm worker %d: identity: %w", ww.id, err)
	}

	initPrompt := identity.BuildPrompt(ident, nil, "You are a warm worker. Acknowledge and await tasks.")
	if err := writeUserMessage(ww.stdin, initPrompt); err != nil {
		ww.killLocked()
		return fmt.Errorf("warm worker %d: identity send: %w", ww.id, err)
	}

	// Read system/init (emitted after claude receives the first message).
	if err := readUntilInit(ww.scanner); err != nil {
		ww.killLocked()
		return fmt.Errorf("warm worker %d: init: %w", ww.id, err)
	}

	// Read until result (discard the identity ack).
	if _, err := readUntilResult(ww.scanner); err != nil {
		ww.killLocked()
		return fmt.Errorf("warm worker %d: identity ack: %w", ww.id, err)
	}

	ww.ready = true

	// Kill process on context cancellation (daemon shutdown).
	go func() {
		<-ctx.Done()
		ww.Stop()
	}()

	log.Printf("warm worker %d: ready", ww.id)
	return nil
}

// Stop terminates the warm worker process. Safe to call without holding the mutex.
func (ww *WarmWorker) Stop() {
	ww.mu.Lock()
	defer ww.mu.Unlock()
	ww.killLocked()
}

// Run sends a task prompt to the warm worker and returns the result.
// The caller must Reserve() before calling Run() and Release() after.
// Run does not manage the busy flag.
func (ww *WarmWorker) Run(task *Task) (string, error) {
	ww.mu.Lock()
	stdin, scanner := ww.stdin, ww.scanner
	ready := ww.ready
	ww.mu.Unlock()
	if !ready || stdin == nil || scanner == nil {
		return "", fmt.Errorf("warm worker %d: not ready (worker stopped or shutdown in progress)", ww.id)
	}

	prompt := ww.buildTaskPrompt(task.Prompt)

	if err := writeUserMessage(stdin, prompt); err != nil {
		return "", fmt.Errorf("warm worker %d: write: %w", ww.id, err)
	}

	result, err := readUntilResult(scanner)
	if err != nil {
		return "", fmt.Errorf("warm worker %d: read: %w", ww.id, err)
	}

	if result.IsError {
		return "", fmt.Errorf("warm worker %d: %s", ww.id, strings.Join(result.Errors, "; "))
	}

	return result.Result, nil
}

// buildTaskPrompt builds the per-task prompt with relevant memories (no identity).
func (ww *WarmWorker) buildTaskPrompt(taskPrompt string) string {
	var sb strings.Builder

	if ww.memStore != nil {
		all, err := ww.memStore.List(0)
		if err == nil && len(all) > 0 {
			relevant := memory.MatchRelevant(all, taskPrompt, 10)
			if len(relevant) > 0 {
				sb.WriteString("<memories>\n")
				for _, e := range relevant {
					sb.WriteString(e.Content)
					sb.WriteString("\n")
				}
				sb.WriteString("</memories>\n\n")
			}
		}
	}

	sb.WriteString("<task>\n")
	sb.WriteString(taskPrompt)
	sb.WriteString("\n</task>")

	return sb.String()
}

// resolveWarmArgs computes the settings file path, model, and permission
// mode for a warm worker invocation. Defaults to sonnet/auto if cfg or
// the relevant fields are unset.
func resolveWarmArgs(gobrrDir string, cfg *config.Config) (settingsPath, model, mode string, err error) {
	settingsPath, err = EnsureWarmSettings(gobrrDir)
	if err != nil {
		return "", "", "", err
	}
	model, mode = "sonnet", "auto"
	if cfg != nil {
		if m := cfg.Models.WarmWorker.Model; m != "" {
			model = m
		}
		if pm := cfg.Models.WarmWorker.PermissionMode; pm != "" {
			mode = pm
		}
	}
	return settingsPath, model, mode, nil
}

// killLocked terminates the process. Caller must hold ww.mu.
func (ww *WarmWorker) killLocked() {
	ww.ready = false
	ww.busy = false
	if ww.stdin != nil {
		ww.stdin.Close()
	}
	if ww.cmd == nil || ww.cmd.Process == nil {
		return
	}
	_ = ww.cmd.Process.Signal(syscall.SIGTERM)
	done := make(chan error, 1)
	go func() { done <- ww.cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		_ = ww.cmd.Process.Kill()
		<-done
	}
	ww.cmd = nil
	ww.stdin = nil
	ww.scanner = nil
}
