package daemon

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
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
	gobrrDir      string
	buildCommand  func(task *Task) *WorkerConfig
}

// NewWorkerPool creates a new WorkerPool. spawnInterval is the minimum duration
// between spawning successive workers. Pass 0 for no rate limiting.
func NewWorkerPool(queue *Queue, maxWorkers int, spawnInterval time.Duration, gobrrDir string) *WorkerPool {
	wp := &WorkerPool{
		maxWorkers:    maxWorkers,
		spawnInterval: spawnInterval,
		queue:         queue,
		gobrrDir:      gobrrDir,
	}
	wp.buildCommand = wp.defaultBuildCommand
	return wp
}

// defaultBuildCommand builds the WorkerConfig for a task using the claude CLI.
func (wp *WorkerPool) defaultBuildCommand(task *Task) *WorkerConfig {
	logDir := filepath.Join(wp.gobrrDir, "logs")
	logPath := filepath.Join(logDir, task.ID+".log")

	args := []string{
		"--print",
		"--output-format", "text",
	}
	if !task.AllowWrites {
		args = append(args, "--allowedTools", "Read,Glob,Grep,Bash")
	}
	args = append(args, task.Prompt)

	return &WorkerConfig{
		Command:    "claude",
		Args:       args,
		TimeoutSec: task.TimeoutSec,
		WorkDir:    wp.gobrrDir,
		LogPath:    logPath,
	}
}

// Run starts the worker pool loop, ticking every second to check for available
// tasks and spawn workers up to maxWorkers. It blocks until ctx is cancelled.
func (wp *WorkerPool) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Inner loop: keep spawning tasks as long as capacity and rate limit allow.
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

				go func(t *Task) {
					defer func() {
						wp.mu.Lock()
						wp.active--
						wp.mu.Unlock()
					}()

					cfg := wp.buildCommand(t)
					result, err := runWorker(ctx, cfg)
					if err != nil {
						msg := strings.TrimSpace(err.Error())
						_ = wp.queue.Fail(t.ID, msg) //nolint:errcheck
						return
					}
					_ = wp.queue.Complete(t.ID, result) //nolint:errcheck
				}(task)
			}
		}
	}
}

// Active returns the number of currently running workers.
func (wp *WorkerPool) Active() int {
	wp.mu.Lock()
	defer wp.mu.Unlock()
	return wp.active
}
