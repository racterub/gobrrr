package daemon

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"os/exec"
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

	var cmd *exec.Cmd
	if ww.command != "claude" {
		// Test mode: run the mock script directly.
		cmd = exec.Command("bash", ww.command) //nolint:gosec
	} else {
		cmd = exec.Command("claude", "-p",
			"--input-format", "stream-json",
			"--output-format", "stream-json",
			"--dangerously-skip-permissions",
			"--verbose",
		)
	}
	cmd.Dir = workDir

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

	// Read until system/init.
	if err := readUntilInit(ww.scanner); err != nil {
		ww.killLocked()
		return fmt.Errorf("warm worker %d: init: %w", ww.id, err)
	}

	// Send identity as the first message.
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

	// Read until result (discard the ack).
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
