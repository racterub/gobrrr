# Assistant Migration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Migrate the Telegram assistant from `~/github/dotfiles/assistant/` into the gobrrr daemon so everything runs from a single binary.

**Architecture:** The daemon gains two new subsystems: a session manager (`internal/session/`) that supervises a Claude Code channel-mode process with PTY allocation, rotation, and crash recovery; and an in-process scheduler (`internal/scheduler/`) that evaluates cron expressions and submits tasks to the existing queue. Both integrate via the existing daemon initialization pattern.

**Tech Stack:** Go, `creack/pty` (pure-Go PTY), `robfig/cron` (cron parser), existing Cobra CLI, existing JSON persistence pattern.

**Spec:** `docs/specs/2026-03-30-assistant-migration-design.md`

---

## File Structure

### New files

| File | Responsibility |
|---|---|
| `internal/session/manager.go` | Session lifecycle: PTY spawn, monitor goroutine, rotation, crash recovery |
| `internal/session/manager_test.go` | Unit tests for session manager |
| `internal/scheduler/scheduler.go` | Cron evaluation, catch-up, persistence |
| `internal/scheduler/scheduler_test.go` | Unit tests for scheduler |
| `skills/homelab/SKILL.md` | Homelab health check skill (migrated from assistant) |
| `skills/timer-management/SKILL.md` | Timer management skill (rewritten for `gobrrr timer`) |

### Modified files

| File | Changes |
|---|---|
| `internal/config/config.go` | Add `TelegramSession` struct and defaults |
| `internal/config/config_test.go` | Test new config fields |
| `internal/daemon/daemon.go` | Wire session manager + scheduler in New() and Run() |
| `internal/daemon/daemon_test.go` | Test new wiring |
| `cmd/gobrrr/main.go` | Add `session` and `timer` command groups |
| `internal/setup/wizard.go` | Add telegram session config section |
| `systemd/gobrrr.service` | Root-level unit with dedicated user, 4G memory |
| `go.mod` / `go.sum` | Add `creack/pty`, `robfig/cron` dependencies |

---

## Task 1: Add dependencies

**Files:**
- Modify: `daemon/go.mod`

- [ ] **Step 1: Add PTY and cron libraries**

```bash
cd /home/racterub/github/gobrrr/daemon && go get github.com/creack/pty@latest && go get github.com/robfig/cron/v3@latest
```

- [ ] **Step 2: Verify no cgo requirement**

```bash
cd /home/racterub/github/gobrrr/daemon && CGO_ENABLED=0 go build ./...
```

Expected: Build succeeds with no cgo errors.

- [ ] **Step 3: Commit**

```bash
cd /home/racterub/github/gobrrr/daemon && git add go.mod go.sum
git commit -m "chore: add creack/pty and robfig/cron dependencies"
```

---

## Task 2: Add TelegramSession config

**Files:**
- Modify: `daemon/internal/config/config.go`
- Modify: `daemon/internal/config/config_test.go`

- [ ] **Step 1: Write the failing test**

In `config_test.go`, add:

```go
func TestDefaultTelegramSessionConfig(t *testing.T) {
	cfg := config.Default()

	assert.False(t, cfg.TelegramSession.Enabled)
	assert.Equal(t, 3072, cfg.TelegramSession.MemoryCeilingMB)
	assert.Equal(t, 6, cfg.TelegramSession.MaxUptimeHours)
	assert.Equal(t, 5, cfg.TelegramSession.IdleThresholdMin)
	assert.Equal(t, 6, cfg.TelegramSession.MaxRestartAttempts)
}

func TestLoadConfigPreservesTelegramSessionDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	// Partial telegram_session: only enabled and memory_ceiling_mb set.
	// Other fields should get defaults from applyTelegramSessionDefaults().
	data := []byte(`{
		"telegram_session": {
			"enabled": true,
			"memory_ceiling_mb": 2048,
			"channels": ["plugin:telegram@claude-plugins-official"]
		}
	}`)
	require.NoError(t, os.WriteFile(path, data, 0600))

	cfg, err := config.Load(path)
	require.NoError(t, err)

	assert.True(t, cfg.TelegramSession.Enabled)
	assert.Equal(t, 2048, cfg.TelegramSession.MemoryCeilingMB)
	// Defaults applied for unset fields via applyTelegramSessionDefaults()
	assert.Equal(t, 6, cfg.TelegramSession.MaxUptimeHours)
	assert.Equal(t, 5, cfg.TelegramSession.IdleThresholdMin)
	assert.Equal(t, 6, cfg.TelegramSession.MaxRestartAttempts)
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /home/racterub/github/gobrrr/daemon && go test ./internal/config/ -run "TestDefaultTelegramSession|TestLoadConfigPreserves" -v
```

Expected: FAIL — `TelegramSession` field does not exist.

- [ ] **Step 3: Add TelegramSession to config struct**

In `config.go`, add the struct:

```go
type TelegramSessionConfig struct {
	Enabled            bool     `json:"enabled"`
	MemoryCeilingMB    int      `json:"memory_ceiling_mb"`
	MaxUptimeHours     int      `json:"max_uptime_hours"`
	IdleThresholdMin   int      `json:"idle_threshold_min"`
	MaxRestartAttempts int      `json:"max_restart_attempts"`
	Channels           []string `json:"channels"`
}
```

Add `TelegramSession TelegramSessionConfig` field to `Config` struct:

```go
TelegramSession TelegramSessionConfig `json:"telegram_session"`
```

In `Default()`, set:

```go
TelegramSession: TelegramSessionConfig{
    Enabled:            false,
    MemoryCeilingMB:    3072,
    MaxUptimeHours:     6,
    IdleThresholdMin:   5,
    MaxRestartAttempts: 6,
},
```

**Important:** `json.Unmarshal` into a pre-filled struct overwrites the entire nested struct when the key is present in JSON, zeroing out fields not in the JSON. Add a post-unmarshal step in `Load()` to fill zero-value fields with defaults:

```go
func Load(path string) (*Config, error) {
    cfg := Default()
    data, err := os.ReadFile(path)
    if err != nil {
        if errors.Is(err, os.ErrNotExist) {
            return cfg, nil
        }
        return nil, err
    }
    if err := json.Unmarshal(data, cfg); err != nil {
        return nil, err
    }
    applyTelegramSessionDefaults(cfg)
    return cfg, nil
}

func applyTelegramSessionDefaults(cfg *Config) {
    d := Default().TelegramSession
    ts := &cfg.TelegramSession
    if ts.MemoryCeilingMB == 0 {
        ts.MemoryCeilingMB = d.MemoryCeilingMB
    }
    if ts.MaxUptimeHours == 0 {
        ts.MaxUptimeHours = d.MaxUptimeHours
    }
    if ts.IdleThresholdMin == 0 {
        ts.IdleThresholdMin = d.IdleThresholdMin
    }
    if ts.MaxRestartAttempts == 0 {
        ts.MaxRestartAttempts = d.MaxRestartAttempts
    }
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd /home/racterub/github/gobrrr/daemon && go test ./internal/config/ -v
```

Expected: All PASS.

- [ ] **Step 5: Commit**

```bash
git add daemon/internal/config/config.go daemon/internal/config/config_test.go
git commit -m "feat: add TelegramSession config with defaults"
```

---

## Task 3: Session manager core

**Files:**
- Create: `daemon/internal/session/manager.go`
- Create: `daemon/internal/session/manager_test.go`

- [ ] **Step 1: Write failing tests for session manager**

In `manager_test.go`:

```go
package session_test

import (
	"testing"
	"time"

	"github.com/racterub/gobrrr/internal/config"
	"github.com/racterub/gobrrr/internal/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testConfig() config.TelegramSessionConfig {
	return config.TelegramSessionConfig{
		Enabled:            true,
		MemoryCeilingMB:    3072,
		MaxUptimeHours:     6,
		IdleThresholdMin:   5,
		MaxRestartAttempts: 6,
		Channels:           []string{"plugin:telegram@claude-plugins-official"},
	}
}

func TestNewManager(t *testing.T) {
	m := session.NewManager(testConfig(), nil)
	require.NotNil(t, m)
	assert.False(t, m.Running())
}

func TestBackoffProgression(t *testing.T) {
	m := session.NewManager(testConfig(), nil)

	assert.Equal(t, 30*time.Second, m.BackoffFor(0))
	assert.Equal(t, 60*time.Second, m.BackoffFor(1))
	assert.Equal(t, 120*time.Second, m.BackoffFor(2))
	assert.Equal(t, 300*time.Second, m.BackoffFor(3))
	assert.Equal(t, 300*time.Second, m.BackoffFor(4)) // capped
}

func TestShouldRotateMemory(t *testing.T) {
	m := session.NewManager(testConfig(), nil)

	reason, rotate := m.EvalRotation(4000, 1*time.Hour, 0*time.Minute)
	assert.True(t, rotate)
	assert.Contains(t, reason, "memory")
}

func TestShouldRotateUptimeWhenIdle(t *testing.T) {
	m := session.NewManager(testConfig(), nil)

	reason, rotate := m.EvalRotation(1000, 7*time.Hour, 10*time.Minute)
	assert.True(t, rotate)
	assert.Contains(t, reason, "uptime")
}

func TestShouldNotRotateUptimeWhenActive(t *testing.T) {
	m := session.NewManager(testConfig(), nil)

	_, rotate := m.EvalRotation(1000, 7*time.Hour, 1*time.Minute)
	assert.False(t, rotate)
}

func TestShouldNotRotateNormal(t *testing.T) {
	m := session.NewManager(testConfig(), nil)

	_, rotate := m.EvalRotation(1000, 1*time.Hour, 0*time.Minute)
	assert.False(t, rotate)
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd /home/racterub/github/gobrrr/daemon && go test ./internal/session/ -v
```

Expected: FAIL — package does not exist.

- [ ] **Step 3: Implement session manager**

Create `daemon/internal/session/manager.go`:

```go
package session

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
	"github.com/racterub/gobrrr/internal/config"
)

// readCgroupMemoryMB reads the cgroup MemoryCurrent in MB.
// Falls back to recursive VmRSS if cgroup is unavailable.
func readCgroupMemoryMB() int {
	data, err := os.ReadFile("/sys/fs/cgroup/memory.current")
	if err != nil {
		return 0
	}
	s := strings.TrimSpace(string(data))
	bytes, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0
	}
	return int(bytes / 1024 / 1024)
}

// Notifier sends alert messages (e.g., to Telegram).
type Notifier interface {
	Send(msg string) error
}

// Manager supervises a Claude Code channel-mode process.
type Manager struct {
	mu         sync.Mutex
	cmd        *exec.Cmd
	ptmx       *os.File
	startedAt  time.Time
	lastOutput time.Time
	restarts   int
	running    bool
	config     config.TelegramSessionConfig
	notifier   Notifier
	workDir    string
}

// NewManager creates a session manager. Notifier may be nil.
func NewManager(cfg config.TelegramSessionConfig, notifier Notifier) *Manager {
	return &Manager{
		config:   cfg,
		notifier: notifier,
	}
}

// SetWorkDir sets the working directory for the Claude session.
func (m *Manager) SetWorkDir(dir string) {
	m.workDir = dir
}

// Running returns whether the session process is alive.
func (m *Manager) Running() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.running
}

// Status returns session status info.
func (m *Manager) Status() (pid int, uptime time.Duration, memMB int, idleFor time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.running || m.cmd == nil || m.cmd.Process == nil {
		return 0, 0, 0, 0
	}

	pid = m.cmd.Process.Pid
	uptime = time.Since(m.startedAt)
	memMB = readProcessTreeMemory(pid)
	idleFor = time.Since(m.lastOutput)
	return
}

// BackoffFor returns the backoff duration for the given restart attempt.
func (m *Manager) BackoffFor(attempt int) time.Duration {
	backoffs := []time.Duration{
		30 * time.Second,
		60 * time.Second,
		120 * time.Second,
		300 * time.Second,
	}
	if attempt >= len(backoffs) {
		return backoffs[len(backoffs)-1]
	}
	return backoffs[attempt]
}

// EvalRotation decides whether the session should be rotated.
// Returns a reason string and whether to rotate.
// memMB: current memory usage in MB
// uptime: how long the session has been running
// idleDuration: how long since last output
func (m *Manager) EvalRotation(memMB int, uptime time.Duration, idleDuration time.Duration) (string, bool) {
	cfg := m.config

	if cfg.MemoryCeilingMB > 0 && memMB > cfg.MemoryCeilingMB {
		return fmt.Sprintf("memory %dMB > ceiling %dMB", memMB, cfg.MemoryCeilingMB), true
	}

	maxUptime := time.Duration(cfg.MaxUptimeHours) * time.Hour
	idleThreshold := time.Duration(cfg.IdleThresholdMin) * time.Minute

	if cfg.MaxUptimeHours > 0 && uptime > maxUptime && idleDuration > idleThreshold {
		return fmt.Sprintf("uptime %s > max %s (idle %s)", uptime.Truncate(time.Minute), maxUptime, idleDuration.Truncate(time.Minute)), true
	}

	return "", false
}

// Run starts the session supervisor loop. Blocks until ctx is cancelled.
func (m *Manager) Run(ctx context.Context) {
	if !m.config.Enabled {
		log.Println("session: telegram session disabled")
		return
	}

	for {
		if m.restarts >= m.config.MaxRestartAttempts {
			msg := fmt.Sprintf("session: gave up after %d consecutive failures", m.restarts)
			log.Println(msg)
			m.alert(msg)
			return
		}

		err := m.runOnce(ctx)
		if ctx.Err() != nil {
			return // daemon shutting down
		}

		sessionDuration := time.Since(m.startedAt)
		if sessionDuration > 5*time.Minute {
			m.restarts = 0
		} else {
			m.restarts++
		}

		if err != nil {
			log.Printf("session: Claude exited with error: %v", err)
		}

		backoff := m.BackoffFor(m.restarts)
		if m.restarts >= 3 {
			m.alert(fmt.Sprintf("session: restart attempt %d, backing off %s", m.restarts+1, backoff))
		}

		log.Printf("session: backing off %s before restart (attempt %d)", backoff, m.restarts)

		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
	}
}

// runOnce spawns a single Claude session and monitors it until it exits or is rotated.
func (m *Manager) runOnce(ctx context.Context) error {
	args := []string{"--dangerously-skip-permissions"}
	for _, ch := range m.config.Channels {
		args = append(args, "--channels", ch)
	}

	cmd := exec.CommandContext(ctx, "claude", args...)
	if m.workDir != "" {
		cmd.Dir = m.workDir
	}

	ptmx, err := pty.Start(cmd)
	if err != nil {
		return fmt.Errorf("failed to start claude with pty: %w", err)
	}

	m.mu.Lock()
	m.cmd = cmd
	m.ptmx = ptmx
	m.startedAt = time.Now()
	m.lastOutput = time.Now()
	m.running = true
	m.mu.Unlock()

	defer func() {
		m.mu.Lock()
		m.running = false
		m.cmd = nil
		m.ptmx = nil
		m.mu.Unlock()
		ptmx.Close()
	}()

	log.Printf("session: Claude started (PID %d)", cmd.Process.Pid)

	// Read PTY output to track activity
	var outputWg sync.WaitGroup
	outputWg.Add(1)
	go func() {
		defer outputWg.Done()
		m.readOutput(ptmx)
	}()

	// Signal channel for process exit (avoids ProcessState race in monitor)
	exited := make(chan struct{})

	// Monitor loop
	monitorDone := make(chan struct{})
	go func() {
		defer close(monitorDone)
		m.monitorLoop(ctx, cmd, exited)
	}()

	// Wait for process to exit
	waitErr := cmd.Wait()
	close(exited)

	// Stop monitor and wait for output reader
	<-monitorDone
	outputWg.Wait()

	log.Printf("session: Claude exited (PID %d)", cmd.Process.Pid)
	return waitErr
}

// readOutput reads from the PTY and updates lastOutput timestamp.
func (m *Manager) readOutput(r io.Reader) {
	buf := make([]byte, 4096)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			m.mu.Lock()
			m.lastOutput = time.Now()
			m.mu.Unlock()
		}
		if err != nil {
			return
		}
	}
}

// monitorLoop checks rotation conditions every 60 seconds.
// Exits when ctx is cancelled or when rotation is triggered.
func (m *Manager) monitorLoop(ctx context.Context, cmd *exec.Cmd, exited <-chan struct{}) {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-exited:
			return // process already exited, no need to monitor
		case <-ticker.C:
			m.mu.Lock()
			running := m.running
			startedAt := m.startedAt
			lastOutput := m.lastOutput
			m.mu.Unlock()

			if !running {
				return
			}

			// Use cgroup memory (counts entire process tree under systemd unit)
			memMB := readCgroupMemoryMB()
			uptime := time.Since(startedAt)
			idleDuration := time.Since(lastOutput)

			reason, shouldRotate := m.EvalRotation(memMB, uptime, idleDuration)
			if shouldRotate {
				log.Printf("session: rotating — %s", reason)
				m.alert(fmt.Sprintf("session: rotating — %s", reason))
				m.killProcess(cmd)
				return
			}
		}
	}
}

// killProcess sends SIGTERM, waits 60s, then SIGKILL.
func (m *Manager) killProcess(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	_ = cmd.Process.Signal(syscall.SIGTERM)
	done := make(chan struct{})
	go func() {
		cmd.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(60 * time.Second):
		_ = cmd.Process.Signal(syscall.SIGKILL)
	}
}

// Stop gracefully stops the current session.
func (m *Manager) Stop() {
	m.mu.Lock()
	cmd := m.cmd
	m.mu.Unlock()

	if cmd != nil && cmd.Process != nil {
		m.killProcess(cmd)
	}
}

func (m *Manager) alert(msg string) {
	if m.notifier != nil {
		_ = m.notifier.Send(msg)
	}
}

// Start launches the session supervisor in a new goroutine.
// Can be called after Stop() or after max_restart_attempts to restart the loop.
func (m *Manager) Start(ctx context.Context) {
	m.mu.Lock()
	m.restarts = 0
	m.mu.Unlock()
	go m.Run(ctx)
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd /home/racterub/github/gobrrr/daemon && go test ./internal/session/ -v
```

Expected: All PASS.

- [ ] **Step 5: Commit**

```bash
git add daemon/internal/session/
git commit -m "feat: add session manager with PTY spawn, rotation, and crash recovery"
```

---

## Task 4: Scheduler core

**Files:**
- Create: `daemon/internal/scheduler/scheduler.go`
- Create: `daemon/internal/scheduler/scheduler_test.go`

- [ ] **Step 1: Write failing tests**

In `scheduler_test.go`:

```go
package scheduler_test

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/racterub/gobrrr/internal/scheduler"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateAndListSchedule(t *testing.T) {
	dir := t.TempDir()
	s := scheduler.New(filepath.Join(dir, "schedules.json"), nil)

	sched, err := s.Create("morning", "0 8 * * *", "check email", "telegram", false)
	require.NoError(t, err)

	assert.NotEmpty(t, sched.ID)
	assert.Equal(t, "morning", sched.Name)
	assert.Equal(t, "0 8 * * *", sched.Cron)
	assert.Equal(t, "check email", sched.Prompt)
	assert.Equal(t, "telegram", sched.ReplyTo)
	assert.Nil(t, sched.LastFiredAt)

	list := s.List()
	assert.Len(t, list, 1)
}

func TestRemoveSchedule(t *testing.T) {
	dir := t.TempDir()
	s := scheduler.New(filepath.Join(dir, "schedules.json"), nil)

	sched, err := s.Create("test", "0 8 * * *", "do thing", "telegram", false)
	require.NoError(t, err)

	err = s.Remove(sched.Name)
	require.NoError(t, err)

	assert.Empty(t, s.List())
}

func TestRemoveNonexistent(t *testing.T) {
	dir := t.TempDir()
	s := scheduler.New(filepath.Join(dir, "schedules.json"), nil)

	err := s.Remove("ghost")
	assert.Error(t, err)
}

func TestDuplicateNameRejected(t *testing.T) {
	dir := t.TempDir()
	s := scheduler.New(filepath.Join(dir, "schedules.json"), nil)

	_, err := s.Create("dup", "0 8 * * *", "a", "telegram", false)
	require.NoError(t, err)

	_, err = s.Create("dup", "0 9 * * *", "b", "telegram", false)
	assert.Error(t, err)
}

func TestPersistenceAcrossReload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "schedules.json")

	s1 := scheduler.New(path, nil)
	_, err := s1.Create("persist", "0 8 * * *", "persist test", "telegram", false)
	require.NoError(t, err)

	s2 := scheduler.New(path, nil)
	err = s2.Load()
	require.NoError(t, err)

	list := s2.List()
	assert.Len(t, list, 1)
	assert.Equal(t, "persist", list[0].Name)
}

func TestCorruptedFileStartsEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "schedules.json")
	require.NoError(t, os.WriteFile(path, []byte("not json"), 0600))

	s := scheduler.New(path, nil)
	err := s.Load()
	require.NoError(t, err) // should not error, just warn

	assert.Empty(t, s.List())
}

func TestCatchUpFires(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "schedules.json")

	var mu sync.Mutex
	var submitted []string
	submitFn := func(prompt, replyTo string, allowWrites bool) error {
		mu.Lock()
		submitted = append(submitted, prompt)
		mu.Unlock()
		return nil
	}

	s := scheduler.New(path, submitFn)
	sched, err := s.Create("catchup", "0 * * * *", "hourly task", "telegram", false)
	require.NoError(t, err)

	// Simulate: last fired 90 minutes ago (< 2x 1-hour interval)
	ago := time.Now().Add(-90 * time.Minute)
	sched.LastFiredAt = &ago
	require.NoError(t, s.Save())

	// Reload and catch up
	s2 := scheduler.New(path, submitFn)
	require.NoError(t, s2.Load())
	s2.CatchUp()

	mu.Lock()
	assert.Len(t, submitted, 1)
	assert.Equal(t, "hourly task", submitted[0])
	mu.Unlock()
}

func TestCatchUpSkipsIfTooOld(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "schedules.json")

	var mu sync.Mutex
	var submitted []string
	submitFn := func(prompt, replyTo string, allowWrites bool) error {
		mu.Lock()
		submitted = append(submitted, prompt)
		mu.Unlock()
		return nil
	}

	s := scheduler.New(path, submitFn)
	sched, err := s.Create("old", "0 * * * *", "old task", "telegram", false)
	require.NoError(t, err)

	// Simulate: last fired 3 hours ago (> 2x 1-hour interval)
	ago := time.Now().Add(-3 * time.Hour)
	sched.LastFiredAt = &ago
	require.NoError(t, s.Save())

	s2 := scheduler.New(path, submitFn)
	require.NoError(t, s2.Load())
	s2.CatchUp()

	mu.Lock()
	assert.Empty(t, submitted)
	mu.Unlock()
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd /home/racterub/github/gobrrr/daemon && go test ./internal/scheduler/ -v
```

Expected: FAIL — package does not exist.

- [ ] **Step 3: Implement scheduler**

Create `daemon/internal/scheduler/scheduler.go`:

```go
package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
)

// Schedule represents a recurring task.
type Schedule struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Cron        string     `json:"cron"`
	Prompt      string     `json:"prompt"`
	ReplyTo     string     `json:"reply_to"`
	AllowWrites bool       `json:"allow_writes"`
	LastFiredAt *time.Time `json:"last_fired_at"`
	CreatedAt   time.Time  `json:"created_at"`
}

// SubmitFunc submits a task to the daemon queue.
type SubmitFunc func(prompt, replyTo string, allowWrites bool) error

// Scheduler manages recurring task schedules.
type Scheduler struct {
	mu        sync.Mutex
	schedules []*Schedule
	filePath  string
	submitFn  SubmitFunc
	parser    cron.Parser
}

// New creates a scheduler. submitFn may be nil (for tests that only test CRUD).
func New(filePath string, submitFn SubmitFunc) *Scheduler {
	return &Scheduler{
		filePath: filePath,
		submitFn: submitFn,
		parser:   cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow),
	}
}

// Load reads schedules from disk. If the file is corrupted, logs a warning and starts empty.
func (s *Scheduler) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var schedules []*Schedule
	if err := json.Unmarshal(data, &schedules); err != nil {
		log.Printf("scheduler: corrupted %s, starting empty: %v", s.filePath, err)
		s.schedules = nil
		return nil
	}

	s.schedules = schedules
	return nil
}

// Save persists schedules to disk atomically.
func (s *Scheduler) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.flush()
}

func (s *Scheduler) flush() error {
	data, err := json.MarshalIndent(s.schedules, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.filePath + ".tmp"
	if err := os.MkdirAll(filepath.Dir(s.filePath), 0700); err != nil {
		return err
	}
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, s.filePath)
}

// Create adds a new schedule. Returns error if name already exists or cron is invalid.
func (s *Scheduler) Create(name, cronExpr, prompt, replyTo string, allowWrites bool) (*Schedule, error) {
	if _, err := s.parser.Parse(cronExpr); err != nil {
		return nil, fmt.Errorf("invalid cron expression: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, sched := range s.schedules {
		if sched.Name == name {
			return nil, fmt.Errorf("schedule %q already exists", name)
		}
	}

	sched := &Schedule{
		ID:          fmt.Sprintf("s_%d", time.Now().UnixNano()),
		Name:        name,
		Cron:        cronExpr,
		Prompt:      prompt,
		ReplyTo:     replyTo,
		AllowWrites: allowWrites,
		CreatedAt:   time.Now(),
	}

	s.schedules = append(s.schedules, sched)
	if err := s.flush(); err != nil {
		return nil, err
	}
	return sched, nil
}

// Remove deletes a schedule by name.
func (s *Scheduler) Remove(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, sched := range s.schedules {
		if sched.Name == name {
			s.schedules = append(s.schedules[:i], s.schedules[i+1:]...)
			return s.flush()
		}
	}
	return fmt.Errorf("schedule %q not found", name)
}

// List returns all schedules.
func (s *Scheduler) List() []*Schedule {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]*Schedule, len(s.schedules))
	copy(out, s.schedules)
	return out
}

// CatchUp fires any schedules that missed their last run (within 2x interval).
func (s *Scheduler) CatchUp() {
	s.mu.Lock()
	var toFire []*Schedule
	now := time.Now()
	for _, sched := range s.schedules {
		if sched.LastFiredAt == nil {
			continue
		}

		parsed, err := s.parser.Parse(sched.Cron)
		if err != nil {
			continue
		}

		nextAfterLast := parsed.Next(*sched.LastFiredAt)
		if nextAfterLast.Before(now) {
			interval := parsed.Next(nextAfterLast).Sub(nextAfterLast)
			missedBy := now.Sub(nextAfterLast)

			if missedBy <= 2*interval {
				log.Printf("scheduler: catch-up firing %q (missed by %s)", sched.Name, missedBy.Truncate(time.Second))
				toFire = append(toFire, sched)
			} else {
				log.Printf("scheduler: skipping catch-up for %q (missed by %s, > 2x interval)", sched.Name, missedBy.Truncate(time.Second))
			}
		}
	}
	s.mu.Unlock()

	// Fire outside the lock to avoid holding it during submitFn calls
	for _, sched := range toFire {
		s.fire(sched)
	}
}

// Run starts the scheduler tick loop. Blocks until ctx is cancelled.
func (s *Scheduler) Run(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.tick()
		}
	}
}

func (s *Scheduler) tick() {
	s.mu.Lock()
	var toFire []*Schedule
	now := time.Now()
	for _, sched := range s.schedules {
		parsed, err := s.parser.Parse(sched.Cron)
		if err != nil {
			continue
		}

		var ref time.Time
		if sched.LastFiredAt != nil {
			ref = *sched.LastFiredAt
		} else {
			ref = sched.CreatedAt
		}

		next := parsed.Next(ref)
		if next.Before(now) || next.Equal(now) {
			toFire = append(toFire, sched)
		}
	}
	s.mu.Unlock()

	for _, sched := range toFire {
		s.fire(sched)
	}
}

// fire submits the schedule's task and updates last_fired_at.
// Must be called WITHOUT s.mu held (submitFn may block).
func (s *Scheduler) fire(sched *Schedule) {
	if s.submitFn == nil {
		return
	}
	if err := s.submitFn(sched.Prompt, sched.ReplyTo, sched.AllowWrites); err != nil {
		log.Printf("scheduler: failed to submit %q: %v", sched.Name, err)
		return
	}
	s.mu.Lock()
	now := time.Now()
	sched.LastFiredAt = &now
	_ = s.flush() // best-effort persist
	s.mu.Unlock()
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd /home/racterub/github/gobrrr/daemon && go test ./internal/scheduler/ -v
```

Expected: All PASS.

- [ ] **Step 5: Commit**

```bash
git add daemon/internal/scheduler/
git commit -m "feat: add in-process scheduler with cron evaluation, catch-up, and persistence"
```

---

## Task 5: Wire session manager and scheduler into daemon

**Files:**
- Modify: `daemon/internal/daemon/daemon.go`

- [ ] **Step 1: Import and wire session manager in New()**

Add to `daemon.go` imports:

```go
"github.com/racterub/gobrrr/internal/session"
"github.com/racterub/gobrrr/internal/scheduler"
```

Add fields to `Daemon` struct:

```go
session   *session.Manager
scheduler *scheduler.Scheduler
```

In `New()`, after existing initialization:

```go
// Session manager
// IMPORTANT: avoid nil-interface trap — a nil *telegram.Notifier passed to
// an interface parameter creates a non-nil interface that panics on Send().
if cfg.TelegramSession.Enabled {
    var sessionNotifier session.Notifier
    if d.notifier != nil {
        sessionNotifier = d.notifier
    }
    d.session = session.NewManager(cfg.TelegramSession, sessionNotifier)
    d.session.SetWorkDir(cfg.WorkspacePath)
}

// Scheduler
schedulerPath := filepath.Join(gobrrDir, "schedules.json")
d.scheduler = scheduler.New(schedulerPath, func(prompt, replyTo string, allowWrites bool) error {
    _, err := d.queue.Submit(prompt, replyTo, 5, allowWrites, cfg.DefaultTimeoutSec)
    return err
})
if err := d.scheduler.Load(); err != nil {
    log.Printf("scheduler: failed to load: %v", err)
}
```

- [ ] **Step 2: Start session and scheduler in Run()**

In `Run()`, after existing goroutine launches:

```go
// Start scheduler catch-up and tick loop
d.scheduler.CatchUp()
go d.scheduler.Run(ctx)

// Start telegram session supervisor
if d.session != nil {
    go d.session.Run(ctx)
}
```

- [ ] **Step 3: Register session API endpoints**

Add to route registration in `New()`:

```go
d.mux.HandleFunc("GET /session/status", d.handleSessionStatus)
d.mux.HandleFunc("POST /session/start", d.handleSessionStart)
d.mux.HandleFunc("POST /session/stop", d.handleSessionStop)
d.mux.HandleFunc("POST /session/restart", d.handleSessionRestart)

d.mux.HandleFunc("POST /schedules", d.handleCreateSchedule)
d.mux.HandleFunc("GET /schedules", d.handleListSchedules)
d.mux.HandleFunc("DELETE /schedules/{name}", d.handleRemoveSchedule)
```

- [ ] **Step 4: Implement HTTP handlers**

Add session handlers (matching existing pattern — no `writeJSON` helper):

```go
func (d *Daemon) handleSessionStatus(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Content-Type", "application/json")
    if d.session == nil {
        json.NewEncoder(w).Encode(map[string]any{"enabled": false}) //nolint:errcheck
        return
    }
    pid, uptime, memMB, idle := d.session.Status()
    json.NewEncoder(w).Encode(map[string]any{
        "enabled": true,
        "running": d.session.Running(),
        "pid":     pid,
        "uptime":  uptime.String(),
        "mem_mb":  memMB,
        "idle":    idle.String(),
    }) //nolint:errcheck
}

func (d *Daemon) handleSessionStart(w http.ResponseWriter, r *http.Request) {
    if d.session == nil {
        http.Error(w, `{"error":"session not configured"}`, http.StatusBadRequest)
        return
    }
    if d.session.Running() {
        http.Error(w, `{"error":"session already running"}`, http.StatusConflict)
        return
    }
    d.session.Start(d.ctx) // d.ctx is the daemon's context, stored during Run()
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(map[string]string{"status": "starting"}) //nolint:errcheck
}

func (d *Daemon) handleSessionStop(w http.ResponseWriter, r *http.Request) {
    if d.session == nil {
        http.Error(w, `{"error":"session not configured"}`, http.StatusBadRequest)
        return
    }
    d.session.Stop()
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(map[string]string{"status": "stopped"}) //nolint:errcheck
}

func (d *Daemon) handleSessionRestart(w http.ResponseWriter, r *http.Request) {
    if d.session == nil {
        http.Error(w, `{"error":"session not configured"}`, http.StatusBadRequest)
        return
    }
    d.session.Stop()
    d.session.Start(d.ctx)
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(map[string]string{"status": "restarting"}) //nolint:errcheck
}
```

Note: Store `ctx` on the Daemon struct in `Run()` so handlers can pass it to `session.Start()`:

```go
func (d *Daemon) Run(ctx context.Context) error {
    d.ctx = ctx
    // ... rest of Run()
}
```

Add scheduler handlers:

```go
func (d *Daemon) handleCreateSchedule(w http.ResponseWriter, r *http.Request) {
    var req struct {
        Name        string `json:"name"`
        Cron        string `json:"cron"`
        Prompt      string `json:"prompt"`
        ReplyTo     string `json:"reply_to"`
        AllowWrites bool   `json:"allow_writes"`
    }
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        http.Error(w, `{"error":"invalid JSON body"}`, http.StatusBadRequest)
        return
    }
    sched, err := d.scheduler.Create(req.Name, req.Cron, req.Prompt, req.ReplyTo, req.AllowWrites)
    if err != nil {
        http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusBadRequest)
        return
    }
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(http.StatusCreated)
    json.NewEncoder(w).Encode(sched) //nolint:errcheck
}

func (d *Daemon) handleListSchedules(w http.ResponseWriter, r *http.Request) {
    schedules := d.scheduler.List()
    if schedules == nil {
        schedules = []*scheduler.Schedule{}
    }
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(schedules) //nolint:errcheck
}

func (d *Daemon) handleRemoveSchedule(w http.ResponseWriter, r *http.Request) {
    name := r.PathValue("name")
    if err := d.scheduler.Remove(name); err != nil {
        http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusNotFound)
        return
    }
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(map[string]string{"status": "removed"}) //nolint:errcheck
}
```

- [ ] **Step 5: Run all daemon tests**

```bash
cd /home/racterub/github/gobrrr/daemon && go test ./internal/daemon/ -v
```

Expected: All PASS (existing + no breakage).

- [ ] **Step 6: Build to verify compilation**

```bash
cd /home/racterub/github/gobrrr/daemon && CGO_ENABLED=0 go build -o /dev/null ./cmd/gobrrr/
```

Expected: Build succeeds.

- [ ] **Step 7: Commit**

```bash
git add daemon/internal/daemon/daemon.go
git commit -m "feat: wire session manager and scheduler into daemon"
```

---

## Task 6: Add client methods for new endpoints

Client must come before CLI since CLI depends on these methods.

**Files:**
- Modify: `daemon/internal/client/client.go`

- [ ] **Step 1: Add session client methods**

Follow the existing pattern (typed methods, `http.NewRequest`, decode response). Add to `client.go`:

```go
// SessionStatus returns the current session status.
func (c *Client) SessionStatus() (map[string]any, error) {
	resp, err := c.httpClient.Get(c.baseURL + "/session/status")
	if err != nil {
		return nil, fmt.Errorf("GET /session/status: %w", err)
	}
	defer resp.Body.Close()

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return result, nil
}

// SessionStart starts the Telegram session.
func (c *Client) SessionStart() error {
	resp, err := c.httpClient.Post(c.baseURL+"/session/start", "application/json", nil)
	if err != nil {
		return fmt.Errorf("POST /session/start: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("POST /session/start: %s", string(body))
	}
	return nil
}

// SessionStop stops the Telegram session.
func (c *Client) SessionStop() error {
	resp, err := c.httpClient.Post(c.baseURL+"/session/stop", "application/json", nil)
	if err != nil {
		return fmt.Errorf("POST /session/stop: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("POST /session/stop: %s", string(body))
	}
	return nil
}

// SessionRestart restarts the Telegram session.
func (c *Client) SessionRestart() error {
	resp, err := c.httpClient.Post(c.baseURL+"/session/restart", "application/json", nil)
	if err != nil {
		return fmt.Errorf("POST /session/restart: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("POST /session/restart: %s", string(body))
	}
	return nil
}
```

- [ ] **Step 2: Add scheduler client methods**

```go
// CreateSchedule creates a new recurring schedule.
func (c *Client) CreateSchedule(name, cron, prompt, replyTo string, allowWrites bool) (map[string]any, error) {
	body := struct {
		Name        string `json:"name"`
		Cron        string `json:"cron"`
		Prompt      string `json:"prompt"`
		ReplyTo     string `json:"reply_to"`
		AllowWrites bool   `json:"allow_writes"`
	}{name, cron, prompt, replyTo, allowWrites}

	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshalling request: %w", err)
	}

	resp, err := c.httpClient.Post(c.baseURL+"/schedules", "application/json", bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("POST /schedules: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("POST /schedules: %s", string(respBody))
	}

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return result, nil
}

// ListSchedules returns all scheduled tasks.
func (c *Client) ListSchedules() ([]map[string]any, error) {
	resp, err := c.httpClient.Get(c.baseURL + "/schedules")
	if err != nil {
		return nil, fmt.Errorf("GET /schedules: %w", err)
	}
	defer resp.Body.Close()

	var result []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return result, nil
}

// RemoveSchedule removes a schedule by name.
func (c *Client) RemoveSchedule(name string) error {
	req, err := http.NewRequest(http.MethodDelete, c.baseURL+"/schedules/"+name, nil)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("DELETE /schedules/%s: %w", name, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("DELETE /schedules/%s: %s", name, string(body))
	}
	return nil
}
```

- [ ] **Step 3: Verify build**

```bash
cd /home/racterub/github/gobrrr/daemon && CGO_ENABLED=0 go build ./...
```

Expected: Build succeeds.

- [ ] **Step 4: Commit**

```bash
git add daemon/internal/client/client.go
git commit -m "feat: add client methods for session and scheduler endpoints"
```

---

## Task 7: CLI commands for session and timer

**Files:**
- Modify: `daemon/cmd/gobrrr/main.go`

- [ ] **Step 1: Add session command group**

Note: `newClient()` returns `*client.Client` with no error (existing pattern).

```go
var sessionCmd = &cobra.Command{
    Use:   "session",
    Short: "Manage the Telegram channel session",
}

var sessionStatusCmd = &cobra.Command{
    Use:   "status",
    Short: "Show session status",
    RunE: func(cmd *cobra.Command, args []string) error {
        c := newClient()
        status, err := c.SessionStatus()
        if err != nil {
            return err
        }
        data, _ := json.MarshalIndent(status, "", "  ")
        fmt.Println(string(data))
        return nil
    },
}

var sessionStartCmd = &cobra.Command{
    Use:   "start",
    Short: "Start the Telegram session",
    RunE: func(cmd *cobra.Command, args []string) error {
        c := newClient()
        if err := c.SessionStart(); err != nil {
            return err
        }
        fmt.Println("Session starting")
        return nil
    },
}

var sessionStopCmd = &cobra.Command{
    Use:   "stop",
    Short: "Stop the Telegram session",
    RunE: func(cmd *cobra.Command, args []string) error {
        c := newClient()
        if err := c.SessionStop(); err != nil {
            return err
        }
        fmt.Println("Session stopped")
        return nil
    },
}

var sessionRestartCmd = &cobra.Command{
    Use:   "restart",
    Short: "Restart the Telegram session",
    RunE: func(cmd *cobra.Command, args []string) error {
        c := newClient()
        if err := c.SessionRestart(); err != nil {
            return err
        }
        fmt.Println("Session restarting")
        return nil
    },
}
```

Register in `init()`:

```go
sessionCmd.AddCommand(sessionStatusCmd, sessionStartCmd, sessionStopCmd, sessionRestartCmd)
rootCmd.AddCommand(sessionCmd)
```

- [ ] **Step 2: Add timer command group**

```go
var timerCmd = &cobra.Command{
    Use:   "timer",
    Short: "Manage scheduled tasks",
}

var timerCreateCmd = &cobra.Command{
    Use:   "create",
    Short: "Create a recurring scheduled task",
    RunE: func(cmd *cobra.Command, args []string) error {
        name, _ := cmd.Flags().GetString("name")
        cronExpr, _ := cmd.Flags().GetString("cron")
        prompt, _ := cmd.Flags().GetString("prompt")
        replyTo, _ := cmd.Flags().GetString("reply-to")
        allowWrites, _ := cmd.Flags().GetBool("allow-writes")

        c := newClient()
        result, err := c.CreateSchedule(name, cronExpr, prompt, replyTo, allowWrites)
        if err != nil {
            return err
        }
        data, _ := json.MarshalIndent(result, "", "  ")
        fmt.Println(string(data))
        return nil
    },
}

var timerListCmd = &cobra.Command{
    Use:   "list",
    Short: "List all scheduled tasks",
    RunE: func(cmd *cobra.Command, args []string) error {
        c := newClient()
        schedules, err := c.ListSchedules()
        if err != nil {
            return err
        }
        data, _ := json.MarshalIndent(schedules, "", "  ")
        fmt.Println(string(data))
        return nil
    },
}

var timerRemoveCmd = &cobra.Command{
    Use:   "remove",
    Short: "Remove a scheduled task",
    RunE: func(cmd *cobra.Command, args []string) error {
        name, _ := cmd.Flags().GetString("name")
        c := newClient()
        if err := c.RemoveSchedule(name); err != nil {
            return err
        }
        fmt.Printf("Removed schedule %q\n", name)
        return nil
    },
}
```

Register flags and commands in `init()`:

```go
timerCreateCmd.Flags().String("name", "", "Schedule name (required)")
timerCreateCmd.Flags().String("cron", "", "Cron expression (required)")
timerCreateCmd.Flags().String("prompt", "", "Task prompt (required)")
timerCreateCmd.Flags().String("reply-to", "telegram", "Result destination")
timerCreateCmd.Flags().Bool("allow-writes", false, "Allow write operations")
timerCreateCmd.MarkFlagRequired("name")
timerCreateCmd.MarkFlagRequired("cron")
timerCreateCmd.MarkFlagRequired("prompt")

timerRemoveCmd.Flags().String("name", "", "Schedule name (required)")
timerRemoveCmd.MarkFlagRequired("name")

timerCmd.AddCommand(timerCreateCmd, timerListCmd, timerRemoveCmd)
rootCmd.AddCommand(timerCmd)
```

- [ ] **Step 3: Verify build**

```bash
cd /home/racterub/github/gobrrr/daemon && CGO_ENABLED=0 go build -o /tmp/gobrrr-test ./cmd/gobrrr/ && /tmp/gobrrr-test session --help && /tmp/gobrrr-test timer --help
```

Expected: Help text for both command groups.

- [ ] **Step 4: Commit**

```bash
git add daemon/cmd/gobrrr/main.go
git commit -m "feat: add session and timer CLI commands"
```

---

## Task 8: Skills migration

**Files:**
- Create: `daemon/skills/homelab/SKILL.md`
- Modify: `daemon/skills/timer-management/SKILL.md` (create new or replace existing)

- [ ] **Step 1: Copy homelab skill**

Create `daemon/skills/homelab/SKILL.md` with the same content as `~/github/dotfiles/assistant/skills/homelab/SKILL.md`, but remove the "Required Permissions" section (we're using `--dangerously-skip-permissions`).

- [ ] **Step 2: Rewrite timer-management skill**

Create `daemon/skills/timer-management/SKILL.md`:

```markdown
# Timer Management Skill

## When to Activate

When the user asks to:
- Schedule a recurring task ("remind me every...", "check X every hour")
- List scheduled tasks ("what's scheduled?", "show my timers")
- Remove a scheduled task ("stop the morning briefing", "cancel X")

## Instructions

### Creating a Timer

```bash
gobrrr timer create \
  --name "descriptive-name" \
  --cron "CRON_EXPRESSION" \
  --prompt "What Claude should do when this fires" \
  --reply-to telegram
```

**Cron format** (standard 5-field):
- Daily at 8am: `0 8 * * *`
- Every 30 minutes: `*/30 * * * *`
- Every 2 hours: `0 */2 * * *`
- Weekdays at 9am: `0 9 * * 1-5`
- Every Sunday at 4am: `0 4 * * 0`

**Options:**
- `--reply-to`: Where to send results (telegram, channel, or comma-separated)
- `--allow-writes`: Enable write operations for this task

**Prompt guidelines:**
- Be specific about what to check and how to format output
- Include output format expectations
- Keep prompts under 500 chars for reliability

### Listing Timers

```bash
gobrrr timer list
```

### Removing a Timer

```bash
gobrrr timer remove --name "timer-name"
```

Confirm removal with the user before executing.
```

- [ ] **Step 3: Commit**

```bash
git add daemon/skills/homelab/ daemon/skills/timer-management/
git commit -m "feat: migrate homelab skill, rewrite timer-management skill for gobrrr CLI"
```

---

## Task 9: Update systemd service unit

**Files:**
- Modify: `daemon/systemd/gobrrr.service`

- [ ] **Step 1: Update to root-level service with dedicated user**

Replace `daemon/systemd/gobrrr.service`:

```ini
[Unit]
Description=gobrrr task dispatch daemon
After=network-online.target
Wants=network-online.target
StartLimitIntervalSec=0

[Service]
Type=notify
User=claude-agent
WorkingDirectory=/home/claude-agent/workspace
ExecStart=/home/claude-agent/.local/bin/gobrrr daemon start
Restart=on-failure
RestartSec=5
WatchdogSec=60
MemoryMax=4G
MemoryHigh=3072M
KillMode=control-group
TimeoutStopSec=90
StandardOutput=journal
StandardError=journal
SyslogIdentifier=gobrrr

[Install]
WantedBy=multi-user.target
```

Key changes from current:
- `User=claude-agent` (dedicated system user)
- `MemoryMax=4G` (up from 512M, covers daemon + Claude session)
- `MemoryHigh=3072M` (pressure warnings)
- `TimeoutStopSec=90` (60s for Claude SIGTERM + buffer)
- `WantedBy=multi-user.target` (root-level, not default.target)
- `StartLimitIntervalSec=0` (allow unlimited restarts)

- [ ] **Step 2: Commit**

```bash
git add daemon/systemd/gobrrr.service
git commit -m "feat: update systemd unit for root-level deployment with session support"
```

---

## Task 10: Update setup wizard

**Files:**
- Modify: `daemon/internal/setup/wizard.go`

- [ ] **Step 1: Add telegram session config section**

After the existing Telegram configuration section in `RunWizard()`, add:

```go
// SECTION: Telegram session configuration
fmt.Println("\n--- Telegram Session ---")
fmt.Print("Enable Telegram channel session? [y/N]: ")
if input := readLine(reader); strings.ToLower(input) == "y" {
    cfg.TelegramSession.Enabled = true

    fmt.Printf("Memory ceiling (MB) [%d]: ", cfg.TelegramSession.MemoryCeilingMB)
    if input := readLine(reader); input != "" {
        if v, err := strconv.Atoi(input); err == nil {
            cfg.TelegramSession.MemoryCeilingMB = v
        }
    }

    fmt.Printf("Max uptime (hours) [%d]: ", cfg.TelegramSession.MaxUptimeHours)
    if input := readLine(reader); input != "" {
        if v, err := strconv.Atoi(input); err == nil {
            cfg.TelegramSession.MaxUptimeHours = v
        }
    }

    fmt.Printf("Idle threshold (minutes) [%d]: ", cfg.TelegramSession.IdleThresholdMin)
    if input := readLine(reader); input != "" {
        if v, err := strconv.Atoi(input); err == nil {
            cfg.TelegramSession.IdleThresholdMin = v
        }
    }

    fmt.Print("Channels [plugin:telegram@claude-plugins-official]: ")
    if input := readLine(reader); input != "" {
        cfg.TelegramSession.Channels = []string{input}
    } else {
        cfg.TelegramSession.Channels = []string{"plugin:telegram@claude-plugins-official"}
    }
}
```

- [ ] **Step 2: Update systemd installation section**

Change the systemd section to install to `/etc/systemd/system/` instead of `~/.config/systemd/user/`:

```go
fmt.Print("Install systemd service (requires sudo)? [y/N]: ")
if strings.ToLower(readLine(reader)) == "y" {
    unitPath := "/etc/systemd/system/gobrrr.service"
    // Write embedded unit file
    if err := os.WriteFile(unitPath, embeddedUnit, 0644); err != nil {
        fmt.Printf("Failed to write %s (try with sudo): %v\n", unitPath, err)
    } else {
        fmt.Printf("Installed to %s\n", unitPath)
        fmt.Println("Run: sudo systemctl daemon-reload && sudo systemctl enable gobrrr")
    }
}
```

- [ ] **Step 3: Verify build**

```bash
cd /home/racterub/github/gobrrr/daemon && CGO_ENABLED=0 go build -o /dev/null ./cmd/gobrrr/
```

Expected: Build succeeds.

- [ ] **Step 4: Commit**

```bash
git add daemon/internal/setup/wizard.go
git commit -m "feat: add telegram session config and root-level systemd install to setup wizard"
```

---

## Task 11: Update CLAUDE.md and TODO.md

**Files:**
- Modify: `CLAUDE.md`
- Modify: `TODO.md`

- [ ] **Step 1: Update project structure in CLAUDE.md**

Add new packages to the project structure:

```
    session/                   Telegram channel session supervisor
    scheduler/                 In-process cron scheduler
```

Add to runtime data section:

```
schedules.json       Recurring task schedules (atomic writes)
```

- [ ] **Step 2: Update TODO.md**

Mark "Migrate Assistant into gobrrr" as done. Add a collapsible details block with the original content (same pattern as the async dispatch item).

- [ ] **Step 3: Commit**

```bash
git add CLAUDE.md TODO.md
git commit -m "docs: update project docs for assistant migration"
```

---

## Task 12: Full integration test

- [ ] **Step 1: Run all tests**

```bash
cd /home/racterub/github/gobrrr/daemon && go test ./... -v
```

Expected: All PASS.

- [ ] **Step 2: Build final binary**

```bash
cd /home/racterub/github/gobrrr/daemon && CGO_ENABLED=0 go build -o gobrrr ./cmd/gobrrr/
```

Expected: Build succeeds.

- [ ] **Step 3: Verify CLI help**

```bash
./daemon/gobrrr --help
./daemon/gobrrr session --help
./daemon/gobrrr timer --help
```

Expected: All command groups visible with correct descriptions.

- [ ] **Step 4: Verify no regressions**

```bash
cd /home/racterub/github/gobrrr/daemon && go vet ./...
```

Expected: No issues.
