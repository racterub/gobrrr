package daemon

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/racterub/gobrrr/internal/config"
)

func TestWorkerCapturesOutput(t *testing.T) {
	dir := t.TempDir()
	cfg := &WorkerConfig{
		Command:    "echo",
		Args:       []string{"hello"},
		TimeoutSec: 5,
		WorkDir:    dir,
		LogPath:    filepath.Join(dir, "test.log"),
	}

	out, err := runWorker(context.Background(), cfg)
	require.NoError(t, err)
	assert.Equal(t, "hello\n", out)
}

func TestWorkerTimeout(t *testing.T) {
	dir := t.TempDir()
	cfg := &WorkerConfig{
		Command:    "sleep",
		Args:       []string{"999"},
		TimeoutSec: 1,
		WorkDir:    dir,
		LogPath:    filepath.Join(dir, "test.log"),
	}

	_, err := runWorker(context.Background(), cfg)
	assert.True(t, errors.Is(err, ErrTimeout), "expected ErrTimeout, got: %v", err)
}

func TestWorkerFailure(t *testing.T) {
	dir := t.TempDir()
	cfg := &WorkerConfig{
		Command:    "false",
		TimeoutSec: 5,
		WorkDir:    dir,
		LogPath:    filepath.Join(dir, "test.log"),
	}

	_, err := runWorker(context.Background(), cfg)
	assert.Error(t, err)
	assert.False(t, errors.Is(err, ErrTimeout))
}

func TestWorkerPoolConcurrencyLimit(t *testing.T) {
	dir := t.TempDir()
	queuePath := filepath.Join(dir, "queue.json")
	q := NewQueue(queuePath)

	// Use sleep long enough that the pool will be at max concurrency while we poll.
	pool := NewWorkerPool(q, &config.Config{WorkspacePath: dir}, 2, 0, dir, nil)
	pool.buildCommand = func(task *Task) *WorkerConfig {
		return &WorkerConfig{
			Command:    "sleep",
			Args:       []string{"1"},
			TimeoutSec: 10,
			WorkDir:    dir,
			LogPath:    filepath.Join(dir, task.ID+".log"),
		}
	}

	// Submit 4 tasks — more than maxWorkers.
	for i := 0; i < 4; i++ {
		_, err := q.Submit("test prompt", "", 0, false, 10, false)
		require.NoError(t, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	poolDone := make(chan struct{})
	go func() {
		defer close(poolDone)
		pool.Run(ctx)
	}()

	// Sample Active() repeatedly and record the max observed.
	var maxObserved int64
	samplerDone := make(chan struct{})
	go func() {
		defer close(samplerDone)
		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				cur := int64(pool.Active())
				if cur > atomic.LoadInt64(&maxObserved) {
					atomic.StoreInt64(&maxObserved, cur)
				}
			}
		}
	}()

	// Wait until at least 2 tasks are running (pool should be full).
	require.Eventually(t, func() bool {
		return pool.Active() == 2
	}, 5*time.Second, 50*time.Millisecond, "pool should reach max_workers=2 concurrency")

	// Confirm pool never exceeds 2 by waiting a bit more and checking again.
	time.Sleep(200 * time.Millisecond)
	assert.LessOrEqual(t, pool.Active(), 2, "active workers should not exceed maxWorkers=2")

	cancel()
	<-poolDone
	<-samplerDone

	assert.LessOrEqual(t, atomic.LoadInt64(&maxObserved), int64(2), "max observed concurrent workers should be <= 2")
}

func TestWorkerPoolTaskResultStored(t *testing.T) {
	dir := t.TempDir()
	queuePath := filepath.Join(dir, "queue.json")
	q := NewQueue(queuePath)

	pool := NewWorkerPool(q, &config.Config{WorkspacePath: dir}, 2, 0, dir, nil)
	pool.buildCommand = func(task *Task) *WorkerConfig {
		return &WorkerConfig{
			Command:    "echo",
			Args:       []string{"task-output"},
			TimeoutSec: 5,
			WorkDir:    dir,
			LogPath:    filepath.Join(dir, task.ID+".log"),
		}
	}

	task, err := q.Submit("test prompt", "", 0, false, 5, false)
	require.NoError(t, err)
	taskID := task.ID

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	poolDone := make(chan struct{})
	go func() {
		defer close(poolDone)
		pool.Run(ctx)
	}()

	require.Eventually(t, func() bool {
		t2, err := q.Get(taskID)
		if err != nil {
			return false
		}
		return t2.Status == "completed"
	}, 5*time.Second, 50*time.Millisecond)

	cancel()
	<-poolDone

	completed, err := q.Get(taskID)
	require.NoError(t, err)
	assert.Equal(t, "completed", completed.Status)
	require.NotNil(t, completed.Result)
	assert.Equal(t, "task-output\n", *completed.Result)

	// Verify log file exists
	logPath := filepath.Join(dir, taskID+".log")
	_, statErr := os.Stat(logPath)
	assert.NoError(t, statErr, "log file should exist")
}

// TestDefaultBuildCommandUsesWorkspacePath verifies that workers run with CWD
// set to the configured workspace path, not the gobrrr data directory.
func TestDefaultBuildCommandUsesWorkspacePath(t *testing.T) {
	gobrrDir := t.TempDir()
	workspace := t.TempDir()

	qPath := filepath.Join(gobrrDir, "queue.json")
	q := NewQueue(qPath)
	pool := NewWorkerPool(q, &config.Config{WorkspacePath: workspace}, 1, 0, gobrrDir, nil)

	cfg := pool.defaultBuildCommand(&Task{ID: "t_1", Prompt: "hi", TimeoutSec: 5})
	assert.Equal(t, workspace, cfg.WorkDir, "worker CWD should be the configured workspace path")
	assert.NotEqual(t, gobrrDir, cfg.WorkDir, "worker CWD should not be the gobrrr data directory")
}

// TestNewWorkerPoolCreatesWorkspace verifies the workspace directory is
// created on pool construction if missing.
func TestNewWorkerPoolCreatesWorkspace(t *testing.T) {
	gobrrDir := t.TempDir()
	workspace := filepath.Join(gobrrDir, "nested", "workspace")

	qPath := filepath.Join(gobrrDir, "queue.json")
	q := NewQueue(qPath)
	_ = NewWorkerPool(q, &config.Config{WorkspacePath: workspace}, 1, 0, gobrrDir, nil)

	info, err := os.Stat(workspace)
	require.NoError(t, err, "workspace directory should be created")
	assert.True(t, info.IsDir())
}

func TestWorkerPoolRoutesWarmTask(t *testing.T) {
	dir := t.TempDir()
	queuePath := filepath.Join(dir, "queue.json")
	q := NewQueue(queuePath)

	// Write mock identity for warm worker.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "identity.md"), []byte("test identity"), 0644))

	// Write mock Claude script.
	script := filepath.Join(dir, "mock-claude.sh")
	content := `#!/bin/bash
echo '{"type":"system","subtype":"init","session_id":"pool-session"}'
while IFS= read -r line; do
  echo '{"type":"result","subtype":"success","result":"warm result","is_error":false,"duration_ms":5}'
done
`
	require.NoError(t, os.WriteFile(script, []byte(content), 0755))

	cfg := &config.Config{WorkspacePath: dir, WarmWorkers: 1}
	pool := NewWorkerPool(q, cfg, 2, 0, dir, nil)

	// Override warm worker command for testing.
	pool.warmCommand = script

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Start warm workers.
	pool.StartWarm(ctx)

	// Submit a warm task.
	task, err := q.Submit("warm prompt", "", 0, false, 10, true)
	require.NoError(t, err)

	go pool.Run(ctx)

	// Wait for task completion.
	require.Eventually(t, func() bool {
		t2, err := q.Get(task.ID)
		if err != nil {
			return false
		}
		return t2.Status == "completed"
	}, 5*time.Second, 50*time.Millisecond)

	completed, err := q.Get(task.ID)
	require.NoError(t, err)
	require.NotNil(t, completed.Result)
	assert.Equal(t, "warm result", *completed.Result)

	cancel()
}

func TestWorkerPoolWarmFallbackToCold(t *testing.T) {
	dir := t.TempDir()
	queuePath := filepath.Join(dir, "queue.json")
	q := NewQueue(queuePath)

	cfg := &config.Config{WorkspacePath: dir, WarmWorkers: 0} // no warm workers
	pool := NewWorkerPool(q, cfg, 2, 0, dir, nil)
	pool.buildCommand = func(task *Task) *WorkerConfig {
		return &WorkerConfig{
			Command:    "echo",
			Args:       []string{"cold fallback"},
			TimeoutSec: 5,
			WorkDir:    dir,
			LogPath:    filepath.Join(dir, task.ID+".log"),
		}
	}

	// Submit a warm task — should fall back to cold since no warm workers.
	task, err := q.Submit("warm prompt", "", 0, false, 10, true)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	go pool.Run(ctx)

	require.Eventually(t, func() bool {
		t2, err := q.Get(task.ID)
		if err != nil {
			return false
		}
		return t2.Status == "completed"
	}, 5*time.Second, 50*time.Millisecond)

	completed, err := q.Get(task.ID)
	require.NoError(t, err)
	require.NotNil(t, completed.Result)
	assert.Equal(t, "cold fallback\n", *completed.Result)

	cancel()
}

func TestColdWorkerBuildCommandUsesConfiguredModelAndMode(t *testing.T) {
	dir := t.TempDir()
	q := NewQueue(filepath.Join(dir, "queue.json"))
	cfg := &config.Config{
		WorkspacePath: dir,
		Models: config.ModelsConfig{
			ColdWorker: config.ModelConfig{Model: "opus", PermissionMode: "auto"},
		},
	}
	pool := NewWorkerPool(q, cfg, 1, 0, dir, nil)

	task := &Task{ID: "t_test", Prompt: "hello", TimeoutSec: 10}
	wc := pool.defaultBuildCommand(task)

	// Expect --model opus --permission-mode auto somewhere in args.
	joined := strings.Join(wc.Args, " ")
	assert.Contains(t, joined, "--model opus")
	assert.Contains(t, joined, "--permission-mode auto")
	assert.NotContains(t, joined, "--dangerously-skip-permissions")
}
