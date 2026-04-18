package daemon

import (
	"context"
	"errors"
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
	"github.com/racterub/gobrrr/internal/security"
)

// ErrTimeout is returned by runWorker when the worker process exceeds its timeout.
var ErrTimeout = errors.New("worker: timeout")

// WorkerConfig holds the configuration for a single worker execution.
type WorkerConfig struct {
	Command    string
	Args       []string
	TimeoutSec int
	WorkDir    string
	Env        []string
	LogPath    string
}

// runWorker executes the command described by cfg and returns stdout output.
// It uses custom timeout handling: on timeout it sends SIGTERM, waits 10s,
// then sends SIGKILL. Returns ErrTimeout on timeout, or the process error on
// non-zero exit.
func runWorker(ctx context.Context, cfg *WorkerConfig) (string, error) {
	cmd := exec.Command(cfg.Command, cfg.Args...) //nolint:gosec

	if cfg.Env != nil {
		cmd.Env = cfg.Env
	}
	if cfg.WorkDir != "" {
		cmd.Dir = cfg.WorkDir
	}

	// Set up stderr log file.
	if cfg.LogPath != "" {
		if err := os.MkdirAll(filepath.Dir(cfg.LogPath), 0700); err == nil {
			logFile, err := os.OpenFile(cfg.LogPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
			if err == nil {
				cmd.Stderr = logFile
				defer logFile.Close()
			}
		}
	}

	// Capture stdout via pipe.
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}

	if err := cmd.Start(); err != nil {
		return "", err
	}

	// Set up timeout timer.
	timeout := time.Duration(cfg.TimeoutSec) * time.Second
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	// Read stdout in a goroutine.
	type readResult struct {
		data string
		err  error
	}
	readCh := make(chan readResult, 1)
	go func() {
		data, err := io.ReadAll(stdoutPipe)
		readCh <- readResult{data: string(data), err: err}
	}()

	// Wait for process completion in a goroutine.
	waitCh := make(chan error, 1)
	go func() {
		waitCh <- cmd.Wait()
	}()

	select {
	case <-ctx.Done():
		// Context cancelled — terminate the process.
		_ = cmd.Process.Signal(syscall.SIGTERM)
		select {
		case <-waitCh:
		case <-time.After(10 * time.Second):
			_ = cmd.Process.Kill()
			<-waitCh
		}
		return "", ctx.Err()

	case <-timer.C:
		// Timeout — graceful SIGTERM then SIGKILL.
		_ = cmd.Process.Signal(syscall.SIGTERM)
		select {
		case <-waitCh:
		case <-time.After(10 * time.Second):
			_ = cmd.Process.Kill()
			<-waitCh
		}
		return "", ErrTimeout

	case waitErr := <-waitCh:
		// Process finished normally (or with error).
		rr := <-readCh
		if waitErr != nil {
			return "", waitErr
		}
		if rr.err != nil {
			return "", rr.err
		}
		return rr.data, nil
	}
}

// WorkerPool manages a pool of concurrent workers that consume tasks from a Queue.
type WorkerPool struct {
	mu            sync.Mutex
	active        int
	maxWorkers    int
	spawnInterval time.Duration
	lastSpawn     time.Time
	queue         *Queue
	cfg           *config.Config
	gobrrDir      string
	memStore      *memory.Store
	buildCommand  func(task *Task) *WorkerConfig
	warmWorkers   []*WarmWorker
	warmCommand   string // override for tests, empty = "claude"
	// onResult is called after a task completes or fails. It is optional.
	onResult func(task *Task, result string)
}

// NewWorkerPool creates a new WorkerPool. spawnInterval is the minimum duration
// between spawning successive workers. Pass 0 for no rate limiting.
//
// cfg provides the workspace path used as the worker CWD. The workspace
// directory is created if missing so worker spawns never fail on a stale
// config pointing at a non-existent path.
func NewWorkerPool(queue *Queue, cfg *config.Config, maxWorkers int, spawnInterval time.Duration, gobrrDir string, ms *memory.Store) *WorkerPool {
	if cfg != nil && cfg.WorkspacePath != "" {
		_ = os.MkdirAll(cfg.WorkspacePath, 0o700)
	}
	wp := &WorkerPool{
		maxWorkers:    maxWorkers,
		spawnInterval: spawnInterval,
		queue:         queue,
		cfg:           cfg,
		gobrrDir:      gobrrDir,
		memStore:      ms,
	}
	wp.buildCommand = wp.defaultBuildCommand
	return wp
}

// defaultBuildCommand builds the WorkerConfig for a task using the claude CLI.
// It loads the identity file and relevant memories to build a full prompt.
// It also generates a per-task settings.json to enforce permissions.
func (wp *WorkerPool) defaultBuildCommand(task *Task) *WorkerConfig {
	logDir := filepath.Join(wp.gobrrDir, "logs")
	logPath := filepath.Join(logDir, task.ID+".log")

	// Build the full prompt with identity + memories.
	prompt := wp.buildFullPrompt(task.Prompt)

	args := []string{
		"--print",
		"--output-format", "text",
	}

	if wp.cfg != nil {
		if m := wp.cfg.Models.ColdWorker.Model; m != "" {
			args = append(args, "--model", m)
		}
		if pm := wp.cfg.Models.ColdWorker.PermissionMode; pm != "" {
			args = append(args, "--permission-mode", pm)
		}
	}

	// Generate per-task settings.json for permission sandboxing.
	workersDir := filepath.Join(wp.gobrrDir, "workers")
	if settingsPath, err := security.Generate(workersDir, task.ID, task.AllowWrites); err == nil {
		args = append(args, "--settings", settingsPath)
	}

	args = append(args, prompt)

	workDir := wp.gobrrDir
	if wp.cfg != nil && wp.cfg.WorkspacePath != "" {
		workDir = wp.cfg.WorkspacePath
	}

	return &WorkerConfig{
		Command:    "claude",
		Args:       args,
		TimeoutSec: task.TimeoutSec,
		WorkDir:    workDir,
		LogPath:    logPath,
	}
}

// buildFullPrompt loads identity and relevant memories and returns the full
// prompt to pass to claude. On any error it falls back to the raw task prompt.
func (wp *WorkerPool) buildFullPrompt(taskPrompt string) string {
	ident, err := identity.Load(wp.gobrrDir)
	if err != nil {
		return taskPrompt
	}

	var memContents []string
	if wp.memStore != nil {
		all, err := wp.memStore.List(0)
		if err == nil && len(all) > 0 {
			relevant := memory.MatchRelevant(all, taskPrompt, 10)
			for _, e := range relevant {
				memContents = append(memContents, e.Content)
			}
		}
	}

	return identity.BuildPrompt(ident, memContents, taskPrompt)
}

// StartWarm pre-spawns warm workers. Safe to call concurrently with Run().
func (wp *WorkerPool) StartWarm(ctx context.Context) {
	warmCount := 0
	if wp.cfg != nil {
		warmCount = wp.cfg.WarmWorkers
	}

	for i := 0; i < warmCount; i++ {
		ww := NewWarmWorker(i, wp.gobrrDir, wp.cfg, wp.memStore)
		if wp.warmCommand != "" {
			ww.command = wp.warmCommand
		}
		// ww.Start takes 7-12s — do not hold wp.mu across it.
		if err := ww.Start(ctx); err != nil {
			log.Printf("warm worker %d: failed to start: %v", i, err)
			continue
		}
		wp.mu.Lock()
		wp.warmWorkers = append(wp.warmWorkers, ww)
		wp.mu.Unlock()
	}
}

// reserveWarmWorker finds an idle warm worker and atomically reserves it.
func (wp *WorkerPool) reserveWarmWorker() *WarmWorker {
	wp.mu.Lock()
	workers := append([]*WarmWorker(nil), wp.warmWorkers...)
	wp.mu.Unlock()
	for _, ww := range workers {
		if ww.Disabled() {
			continue
		}
		if ww.Reserve() {
			return ww
		}
	}
	return nil
}

// WarmStatus returns the total, ready, busy, and disabled counts for warm workers.
func (wp *WorkerPool) WarmStatus() (total, ready, busy, disabled int) {
	wp.mu.Lock()
	workers := append([]*WarmWorker(nil), wp.warmWorkers...)
	wp.mu.Unlock()
	for _, ww := range workers {
		total++
		r, b, d := ww.Status()
		if d {
			disabled++
		} else if r {
			ready++
		}
		if b {
			busy++
		}
	}
	return
}

// Run starts the worker pool loop. It uses a two-pass dispatch:
// pass 1 routes warm tasks to warm workers, pass 2 dispatches remaining
// tasks (including warm fallback) via cold spawn. Blocks until ctx is cancelled.
func (wp *WorkerPool) Run(ctx context.Context) {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Pass 1: dispatch warm tasks to warm workers.
			for {
				ww := wp.reserveWarmWorker()
				if ww == nil {
					break
				}
				task, err := wp.queue.NextWarm()
				if err != nil || task == nil {
					ww.Release()
					break
				}
				go wp.dispatchWarm(ctx, ww, task)
			}

			// Pass 2: dispatch any remaining tasks cold.
			for {
				wp.mu.Lock()
				active := wp.active
				canSpawn := active < wp.maxWorkers
				rateLimitOk := wp.spawnInterval == 0 || time.Since(wp.lastSpawn) >= wp.spawnInterval
				wp.mu.Unlock()

				if !canSpawn || !rateLimitOk {
					break
				}

				task, err := wp.queue.Next()
				if err != nil || task == nil {
					break
				}

				wp.mu.Lock()
				wp.active++
				wp.lastSpawn = time.Now()
				wp.mu.Unlock()

				go wp.dispatchCold(ctx, task)
			}
		}
	}
}

// dispatchWarm sends a task to a warm worker, handles result routing and
// crash recovery.
func (wp *WorkerPool) dispatchWarm(ctx context.Context, ww *WarmWorker, task *Task) {
	defer ww.Release()

	result, err := ww.Run(task)
	if err != nil {
		msg := strings.TrimSpace(err.Error())
		_ = wp.queue.Fail(task.ID, msg)
		if wp.onResult != nil && task.ReplyTo == "telegram" {
			wp.onResult(task, "Task failed: "+msg)
		}
		// Respawn crashed warm worker if daemon is still running, unless the slot
		// has flapped (repeated aborts within the anti-flap window).
		if ctx.Err() == nil {
			if !ww.RecordRespawnAttempt() {
				log.Printf("warm worker %d: flap detected, slot disabled until manual restart", ww.id)
				ww.Stop()
				return
			}
			log.Printf("warm worker %d: crash detected, respawning", ww.id)
			ww.Stop()
			if startErr := ww.Start(ctx); startErr != nil {
				log.Printf("warm worker %d: respawn failed: %v", ww.id, startErr)
			}
		}
		return
	}

	_ = wp.queue.Complete(task.ID, result)
	if wp.onResult != nil {
		wp.onResult(task, result)
	}
}

// dispatchCold runs a task via cold spawn (existing behavior).
func (wp *WorkerPool) dispatchCold(ctx context.Context, task *Task) {
	defer func() {
		wp.mu.Lock()
		wp.active--
		wp.mu.Unlock()
		workersDir := filepath.Join(wp.gobrrDir, "workers")
		_ = security.Cleanup(workersDir, task.ID)
	}()

	cfg := wp.buildCommand(task)
	result, err := runWorker(ctx, cfg)
	if err != nil {
		msg := strings.TrimSpace(err.Error())
		_ = wp.queue.Fail(task.ID, msg)
		if wp.onResult != nil && task.ReplyTo == "telegram" {
			wp.onResult(task, "Task failed: "+msg)
		}
		return
	}
	_ = wp.queue.Complete(task.ID, result)
	if wp.onResult != nil {
		wp.onResult(task, result)
	}
}

// Active returns the number of currently running workers.
func (wp *WorkerPool) Active() int {
	wp.mu.Lock()
	defer wp.mu.Unlock()
	return wp.active
}
