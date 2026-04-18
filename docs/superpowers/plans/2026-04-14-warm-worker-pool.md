# Warm Worker Pool Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extend `WorkerPool` with pre-spawned persistent Claude processes that dispatch tasks via the stream-json NDJSON protocol for sub-second latency.

**Architecture:** `WarmWorker` manages a persistent `claude -p --input-format stream-json --output-format stream-json` process. `WorkerPool.Run()` uses a two-pass dispatch loop — first routing `task.Warm=true` tasks to idle warm workers, then dispatching remaining tasks via cold spawn. Warm workers are pre-spawned at daemon start, auto-respawned on crash, and killed on daemon shutdown.

**Tech Stack:** Go, `bufio.Scanner` for NDJSON line reading, `exec.Cmd` with stdin/stdout pipes, `encoding/json` for marshal/unmarshal.

**Design spec:** `docs/superpowers/specs/2026-04-14-warm-worker-pool-design.md`

---

### Task 1: Add WarmWorkers config field

**Files:**
- Modify: `daemon/internal/config/config.go`

- [ ] **Step 1: Add WarmWorkers field to Config struct**

```go
// In Config struct, after SpawnIntervalSec:
WarmWorkers int `json:"warm_workers"`
```

- [ ] **Step 2: Set default in Default()**

```go
// In Default(), add:
WarmWorkers: 1,
```

- [ ] **Step 3: Run tests**

Run: `cd daemon && go test ./internal/config/... -v`
Expected: PASS (existing tests still pass)

- [ ] **Step 4: Commit**

```bash
git add daemon/internal/config/config.go
git commit -m "feat(config): add WarmWorkers field with default 1"
```

---

### Task 2: Add Warm field to Task and Queue.Submit

**Files:**
- Modify: `daemon/internal/daemon/queue.go`
- Modify: `daemon/internal/daemon/queue_test.go`

- [ ] **Step 1: Write the failing test**

Add to `daemon/internal/daemon/queue_test.go`:

```go
func TestSubmitWarmTask(t *testing.T) {
	q := daemon.NewQueue(filepath.Join(t.TempDir(), "queue.json"))

	task, err := q.Submit("quick lookup", "telegram", 5, false, 300, true)
	require.NoError(t, err)

	assert.True(t, task.Warm, "task should be marked warm")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd daemon && go test ./internal/daemon/ -run TestSubmitWarmTask -v`
Expected: FAIL — `Submit` has wrong number of arguments

- [ ] **Step 3: Add Warm field to Task struct**

In `daemon/internal/daemon/queue.go`, add to the `Task` struct after `AllowWrites`:

```go
Warm bool `json:"warm"`
```

- [ ] **Step 4: Add warm parameter to Queue.Submit**

Update the `Submit` method signature and body:

```go
func (q *Queue) Submit(prompt, replyTo string, priority int, allowWrites bool, timeoutSec int, warm bool) (*Task, error) {
```

In the task creation inside `Submit`, add:

```go
Warm: warm,
```

- [ ] **Step 5: Fix all callers of Queue.Submit**

Every existing call to `q.Submit(...)` needs a `false` appended for the warm parameter. There are callers in:

- `daemon/internal/daemon/daemon.go` — `handleSubmitTask`: add `req.Warm` (will be wired in Task 8)
  - For now, use `false` as placeholder
- `daemon/internal/scheduler/scheduler.go` — scheduler callback: add `false`
- All test files calling `q.Submit(...)`: add `false`

Search for all callers:

```bash
cd daemon && grep -rn 'q\.Submit\|queue\.Submit' --include='*.go'
```

Update each call site by appending `, false` to the argument list.

- [ ] **Step 6: Run all tests**

Run: `cd daemon && go test ./... -v`
Expected: PASS — all tests pass including the new one

- [ ] **Step 7: Commit**

```bash
git add daemon/internal/daemon/queue.go daemon/internal/daemon/queue_test.go
git add -u  # catch all updated callers
git commit -m "feat(queue): add Warm field to Task and Submit"
```

---

### Task 3: Add NextWarm method to Queue

**Files:**
- Modify: `daemon/internal/daemon/queue.go`
- Modify: `daemon/internal/daemon/queue_test.go`

- [ ] **Step 1: Write the failing test**

Add to `daemon/internal/daemon/queue_test.go`:

```go
func TestNextWarmSkipsColdTasks(t *testing.T) {
	q := daemon.NewQueue(filepath.Join(t.TempDir(), "queue.json"))

	_, err := q.Submit("cold task", "telegram", 5, false, 300, false)
	require.NoError(t, err)
	warm, err := q.Submit("warm task", "telegram", 5, false, 300, true)
	require.NoError(t, err)

	task, err := q.NextWarm()
	require.NoError(t, err)
	require.NotNil(t, task)
	assert.Equal(t, warm.ID, task.ID)
	assert.True(t, task.Warm)
	assert.Equal(t, "running", task.Status)
}

func TestNextWarmReturnsNilWhenNoWarmTasks(t *testing.T) {
	q := daemon.NewQueue(filepath.Join(t.TempDir(), "queue.json"))

	_, err := q.Submit("cold task", "telegram", 5, false, 300, false)
	require.NoError(t, err)

	task, err := q.NextWarm()
	require.NoError(t, err)
	assert.Nil(t, task)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd daemon && go test ./internal/daemon/ -run TestNextWarm -v`
Expected: FAIL — `NextWarm` method does not exist

- [ ] **Step 3: Implement NextWarm**

Add to `daemon/internal/daemon/queue.go`:

```go
// NextWarm returns the next queued task with Warm=true (highest priority, then
// FIFO) and marks it as running. Returns nil if no warm tasks are queued.
func (q *Queue) NextWarm() (*Task, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	var best *Task
	for _, t := range q.tasks {
		if t.Status != "queued" || !t.Warm {
			continue
		}
		if best == nil {
			best = t
			continue
		}
		if t.Priority < best.Priority {
			best = t
		} else if t.Priority == best.Priority && t.CreatedAt.Before(best.CreatedAt) {
			best = t
		}
	}

	if best == nil {
		return nil, nil
	}

	now := time.Now()
	best.Status = "running"
	best.StartedAt = &now

	if err := q.flush(); err != nil {
		best.Status = "queued"
		best.StartedAt = nil
		return nil, fmt.Errorf("persisting queue: %w", err)
	}

	return best, nil
}
```

- [ ] **Step 4: Run tests**

Run: `cd daemon && go test ./internal/daemon/ -run TestNextWarm -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add daemon/internal/daemon/queue.go daemon/internal/daemon/queue_test.go
git commit -m "feat(queue): add NextWarm method for warm task dispatch"
```

---

### Task 4: NDJSON protocol types and parsing functions

**Files:**
- Create: `daemon/internal/daemon/warm_proto.go`
- Create: `daemon/internal/daemon/warm_proto_test.go`

- [ ] **Step 1: Write the failing tests**

Create `daemon/internal/daemon/warm_proto_test.go`:

```go
package daemon

import (
	"bufio"
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteUserMessage(t *testing.T) {
	var buf bytes.Buffer
	err := writeUserMessage(&buf, "hello world")
	require.NoError(t, err)

	line := strings.TrimSpace(buf.String())
	assert.Contains(t, line, `"type":"user"`)
	assert.Contains(t, line, `"role":"user"`)
	assert.Contains(t, line, `"content":"hello world"`)
}

func TestReadUntilInit(t *testing.T) {
	input := `{"type":"system","subtype":"api_retry","attempt":1}
{"type":"system","subtype":"init","session_id":"test-123"}
`
	scanner := bufio.NewScanner(strings.NewReader(input))
	err := readUntilInit(scanner)
	require.NoError(t, err)
}

func TestReadUntilInitEOF(t *testing.T) {
	input := `{"type":"system","subtype":"api_retry","attempt":1}
`
	scanner := bufio.NewScanner(strings.NewReader(input))
	err := readUntilInit(scanner)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no init message")
}

func TestReadUntilResultSuccess(t *testing.T) {
	input := `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"thinking..."}]}}
{"type":"result","subtype":"success","result":"the answer is 42","is_error":false,"duration_ms":150}
`
	scanner := bufio.NewScanner(strings.NewReader(input))
	result, err := readUntilResult(scanner)
	require.NoError(t, err)
	assert.Equal(t, "the answer is 42", result.Result)
	assert.False(t, result.IsError)
	assert.Equal(t, 150, result.DurationMs)
}

func TestReadUntilResultError(t *testing.T) {
	input := `{"type":"result","subtype":"error_during_execution","result":"","is_error":true,"errors":["something broke"],"duration_ms":50}
`
	scanner := bufio.NewScanner(strings.NewReader(input))
	result, err := readUntilResult(scanner)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Equal(t, []string{"something broke"}, result.Errors)
}

func TestReadUntilResultEOF(t *testing.T) {
	input := `{"type":"assistant","message":{"role":"assistant","content":[]}}
`
	scanner := bufio.NewScanner(strings.NewReader(input))
	_, err := readUntilResult(scanner)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no result message")
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd daemon && go test ./internal/daemon/ -run 'TestWriteUserMessage|TestReadUntil' -v`
Expected: FAIL — functions not defined

- [ ] **Step 3: Implement the protocol types and functions**

Create `daemon/internal/daemon/warm_proto.go`:

```go
package daemon

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
)

// streamMsg is used for initial type dispatch when reading NDJSON lines.
type streamMsg struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype,omitempty"`
}

// userMsg is the envelope sent to a warm worker's stdin.
type userMsg struct {
	Type            string     `json:"type"`
	Message         msgContent `json:"message"`
	ParentToolUseID *string    `json:"parent_tool_use_id"`
}

// msgContent holds the role and content of a message.
type msgContent struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// resultMsg is emitted by Claude after completing a request.
type resultMsg struct {
	Type       string   `json:"type"`
	Subtype    string   `json:"subtype"`
	Result     string   `json:"result"`
	IsError    bool     `json:"is_error"`
	Errors     []string `json:"errors,omitempty"`
	DurationMs int      `json:"duration_ms"`
}

// writeUserMessage writes one NDJSON user message line to w.
func writeUserMessage(w io.Writer, content string) error {
	msg := userMsg{
		Type: "user",
		Message: msgContent{
			Role:    "user",
			Content: content,
		},
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshalling user message: %w", err)
	}
	_, err = fmt.Fprintf(w, "%s\n", data)
	return err
}

// readUntilInit reads NDJSON lines from scanner until a system/init message.
// Non-init lines are silently skipped.
func readUntilInit(scanner *bufio.Scanner) error {
	for scanner.Scan() {
		var msg streamMsg
		if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
			continue
		}
		if msg.Type == "system" && msg.Subtype == "init" {
			return nil
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("reading stdout: %w", err)
	}
	return fmt.Errorf("unexpected EOF: no init message received")
}

// readUntilResult reads NDJSON lines from scanner until a result message.
// All non-result lines (assistant, system, stream_event) are skipped.
func readUntilResult(scanner *bufio.Scanner) (*resultMsg, error) {
	for scanner.Scan() {
		var msg streamMsg
		if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
			continue
		}
		if msg.Type == "result" {
			var result resultMsg
			if err := json.Unmarshal(scanner.Bytes(), &result); err != nil {
				return nil, fmt.Errorf("parsing result message: %w", err)
			}
			return &result, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading stdout: %w", err)
	}
	return nil, fmt.Errorf("unexpected EOF: no result message received")
}
```

- [ ] **Step 4: Run tests**

Run: `cd daemon && go test ./internal/daemon/ -run 'TestWriteUserMessage|TestReadUntil' -v`
Expected: PASS (all 5 tests)

- [ ] **Step 5: Commit**

```bash
git add daemon/internal/daemon/warm_proto.go daemon/internal/daemon/warm_proto_test.go
git commit -m "feat(daemon): add NDJSON protocol types and parsing for warm workers"
```

---

### Task 5: WarmWorker struct and Start()

**Files:**
- Create: `daemon/internal/daemon/warm_worker.go`
- Create: `daemon/internal/daemon/warm_worker_test.go`

This task uses a mock shell script that mimics the stream-json protocol to test WarmWorker without a real Claude process.

- [ ] **Step 1: Write the test helper — mock warm Claude script**

Add to `daemon/internal/daemon/warm_worker_test.go`:

```go
package daemon

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/racterub/gobrrr/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeMockScript creates a shell script that mimics the stream-json protocol.
// It emits system/init, then loops: read stdin line, emit result.
func writeMockScript(t *testing.T, dir string) string {
	t.Helper()
	script := filepath.Join(dir, "mock-claude.sh")
	content := `#!/bin/bash
echo '{"type":"system","subtype":"init","session_id":"mock-session"}'
while IFS= read -r line; do
  echo '{"type":"result","subtype":"success","result":"mock response","is_error":false,"duration_ms":10}'
done
`
	require.NoError(t, os.WriteFile(script, []byte(content), 0755))
	return script
}

// writeMockIdentity creates a minimal identity.md for testing.
func writeMockIdentity(t *testing.T, dir string) {
	t.Helper()
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "identity.md"),
		[]byte("You are a test assistant."),
		0644,
	))
}
```

- [ ] **Step 2: Write the failing test for Start()**

Add to `daemon/internal/daemon/warm_worker_test.go`:

```go
func TestWarmWorkerStart(t *testing.T) {
	dir := t.TempDir()
	script := writeMockScript(t, dir)
	writeMockIdentity(t, dir)

	ww := NewWarmWorker(0, dir, &config.Config{WorkspacePath: dir}, nil)
	ww.command = script // override command for testing

	ctx, cancel := t.Context()
	defer cancel()

	err := ww.Start(ctx)
	require.NoError(t, err)
	assert.True(t, ww.Available())

	ww.Stop()
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `cd daemon && go test ./internal/daemon/ -run TestWarmWorkerStart -v`
Expected: FAIL — `NewWarmWorker` not defined

- [ ] **Step 4: Implement WarmWorker struct and NewWarmWorker**

Create `daemon/internal/daemon/warm_worker.go`:

```go
package daemon

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"os/exec"
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
```

- [ ] **Step 5: Run test**

Run: `cd daemon && go test ./internal/daemon/ -run TestWarmWorkerStart -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add daemon/internal/daemon/warm_worker.go daemon/internal/daemon/warm_worker_test.go
git commit -m "feat(daemon): add WarmWorker struct with Start() and init handshake"
```

---

### Task 6: WarmWorker Run() — task dispatch

**Files:**
- Modify: `daemon/internal/daemon/warm_worker.go`
- Modify: `daemon/internal/daemon/warm_worker_test.go`

- [ ] **Step 1: Write the failing test**

Add to `daemon/internal/daemon/warm_worker_test.go`:

```go
func TestWarmWorkerRun(t *testing.T) {
	dir := t.TempDir()
	script := writeMockScript(t, dir)
	writeMockIdentity(t, dir)

	ww := NewWarmWorker(0, dir, &config.Config{WorkspacePath: dir}, nil)
	ww.command = script

	ctx, cancel := t.Context()
	defer cancel()

	require.NoError(t, ww.Start(ctx))

	task := &Task{ID: "t_test_1", Prompt: "what is 2+2?", TimeoutSec: 10}
	result, err := ww.Run(task)
	require.NoError(t, err)
	assert.Equal(t, "mock response", result)

	// Worker should be available for another task after Run completes.
	// (Run does not manage busy flag — caller does via Reserve/Release.)
	ww.Stop()
}

func TestWarmWorkerRunMultipleTasks(t *testing.T) {
	dir := t.TempDir()
	script := writeMockScript(t, dir)
	writeMockIdentity(t, dir)

	ww := NewWarmWorker(0, dir, &config.Config{WorkspacePath: dir}, nil)
	ww.command = script

	ctx, cancel := t.Context()
	defer cancel()

	require.NoError(t, ww.Start(ctx))

	for i := 0; i < 3; i++ {
		task := &Task{ID: fmt.Sprintf("t_test_%d", i), Prompt: fmt.Sprintf("task %d", i), TimeoutSec: 10}
		result, err := ww.Run(task)
		require.NoError(t, err)
		assert.Equal(t, "mock response", result)
	}

	ww.Stop()
}
```

Add `"fmt"` to the import block in the test file if not already present.

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd daemon && go test ./internal/daemon/ -run 'TestWarmWorkerRun$|TestWarmWorkerRunMultiple' -v`
Expected: FAIL — `Run` method not defined

- [ ] **Step 3: Implement Run()**

Add to `daemon/internal/daemon/warm_worker.go`:

```go
// Run sends a task prompt to the warm worker and returns the result.
// The caller must Reserve() before calling Run() and Release() after.
// Run does not manage the busy flag.
func (ww *WarmWorker) Run(task *Task) (string, error) {
	prompt := ww.buildTaskPrompt(task.Prompt)

	if err := writeUserMessage(ww.stdin, prompt); err != nil {
		return "", fmt.Errorf("warm worker %d: write: %w", ww.id, err)
	}

	result, err := readUntilResult(ww.scanner)
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
```

- [ ] **Step 4: Run tests**

Run: `cd daemon && go test ./internal/daemon/ -run 'TestWarmWorkerRun' -v`
Expected: PASS (both tests)

- [ ] **Step 5: Commit**

```bash
git add daemon/internal/daemon/warm_worker.go daemon/internal/daemon/warm_worker_test.go
git commit -m "feat(daemon): add WarmWorker.Run() for task dispatch"
```

---

### Task 7: WarmWorker crash detection and error result handling

**Files:**
- Modify: `daemon/internal/daemon/warm_worker_test.go`
- Modify: `daemon/internal/daemon/warm_worker.go` (if needed)

- [ ] **Step 1: Write mock scripts for crash and error cases**

Add to `daemon/internal/daemon/warm_worker_test.go`:

```go
// writeCrashScript creates a script that crashes after one task dispatch.
func writeCrashScript(t *testing.T, dir string) string {
	t.Helper()
	script := filepath.Join(dir, "mock-claude-crash.sh")
	content := `#!/bin/bash
echo '{"type":"system","subtype":"init","session_id":"crash-session"}'
# Read identity, send ack
read -r line
echo '{"type":"result","subtype":"success","result":"ready","is_error":false,"duration_ms":1}'
# Read first task, then crash
read -r line
exit 1
`
	require.NoError(t, os.WriteFile(script, []byte(content), 0755))
	return script
}

// writeErrorScript creates a script that returns an error result.
func writeErrorScript(t *testing.T, dir string) string {
	t.Helper()
	script := filepath.Join(dir, "mock-claude-error.sh")
	content := `#!/bin/bash
echo '{"type":"system","subtype":"init","session_id":"error-session"}'
read -r line
echo '{"type":"result","subtype":"success","result":"ready","is_error":false,"duration_ms":1}'
read -r line
echo '{"type":"result","subtype":"error_during_execution","result":"","is_error":true,"errors":["something broke"],"duration_ms":10}'
`
	require.NoError(t, os.WriteFile(script, []byte(content), 0755))
	return script
}
```

- [ ] **Step 2: Write the crash test**

```go
func TestWarmWorkerRunCrash(t *testing.T) {
	dir := t.TempDir()
	script := writeCrashScript(t, dir)
	writeMockIdentity(t, dir)

	ww := NewWarmWorker(0, dir, &config.Config{WorkspacePath: dir}, nil)
	ww.command = script

	ctx, cancel := t.Context()
	defer cancel()

	require.NoError(t, ww.Start(ctx))

	task := &Task{ID: "t_crash", Prompt: "crash me", TimeoutSec: 10}
	_, err := ww.Run(task)
	assert.Error(t, err, "Run should return error on crash")
	assert.Contains(t, err.Error(), "no result message")
}

func TestWarmWorkerRunErrorResult(t *testing.T) {
	dir := t.TempDir()
	script := writeErrorScript(t, dir)
	writeMockIdentity(t, dir)

	ww := NewWarmWorker(0, dir, &config.Config{WorkspacePath: dir}, nil)
	ww.command = script

	ctx, cancel := t.Context()
	defer cancel()

	require.NoError(t, ww.Start(ctx))

	task := &Task{ID: "t_error", Prompt: "fail me", TimeoutSec: 10}
	_, err := ww.Run(task)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "something broke")
}
```

- [ ] **Step 3: Run tests**

Run: `cd daemon && go test ./internal/daemon/ -run 'TestWarmWorkerRunCrash|TestWarmWorkerRunError' -v`
Expected: PASS — the existing Run() implementation already handles both cases via `readUntilResult` returning EOF error and `result.IsError` check.

- [ ] **Step 4: Commit**

```bash
git add daemon/internal/daemon/warm_worker_test.go
git commit -m "test(daemon): add crash and error result tests for WarmWorker"
```

---

### Task 8: WorkerPool warm routing

**Files:**
- Modify: `daemon/internal/daemon/worker.go`
- Modify: `daemon/internal/daemon/worker_test.go`

- [ ] **Step 1: Write the failing test for warm routing**

Add to `daemon/internal/daemon/worker_test.go`:

```go
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
	require.NoError(t, pool.StartWarm(ctx))

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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd daemon && go test ./internal/daemon/ -run 'TestWorkerPoolRoutesWarm|TestWorkerPoolWarmFallback' -v`
Expected: FAIL — `StartWarm`, `warmCommand` not defined

- [ ] **Step 3: Add warm worker fields and StartWarm to WorkerPool**

In `daemon/internal/daemon/worker.go`, add fields to `WorkerPool`:

```go
type WorkerPool struct {
	// ... existing fields ...
	warmWorkers []*WarmWorker
	warmCommand string // override for tests, empty = "claude"
}
```

Add `StartWarm` method:

```go
// StartWarm pre-spawns warm workers. Call before Run().
func (wp *WorkerPool) StartWarm(ctx context.Context) error {
	warmCount := 0
	if wp.cfg != nil {
		warmCount = wp.cfg.WarmWorkers
	}

	for i := 0; i < warmCount; i++ {
		ww := NewWarmWorker(i, wp.gobrrDir, wp.cfg, wp.memStore)
		if wp.warmCommand != "" {
			ww.command = wp.warmCommand
		}
		if err := ww.Start(ctx); err != nil {
			log.Printf("warm worker %d: failed to start: %v", i, err)
			continue
		}
		wp.warmWorkers = append(wp.warmWorkers, ww)
	}
	return nil
}

// reserveWarmWorker finds an idle warm worker and atomically reserves it.
func (wp *WorkerPool) reserveWarmWorker() *WarmWorker {
	for _, ww := range wp.warmWorkers {
		if ww.Reserve() {
			return ww
		}
	}
	return nil
}

// WarmStatus returns the total, ready, and busy counts for warm workers.
func (wp *WorkerPool) WarmStatus() (total, ready, busy int) {
	for _, ww := range wp.warmWorkers {
		total++
		ww.mu.Lock()
		if ww.ready {
			ready++
		}
		if ww.busy {
			busy++
		}
		ww.mu.Unlock()
	}
	return
}
```

- [ ] **Step 4: Rewrite Run() with two-pass dispatch loop**

Replace the `Run` method in `daemon/internal/daemon/worker.go`:

```go
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
		// Respawn crashed warm worker if daemon is still running.
		if ctx.Err() == nil {
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
```

- [ ] **Step 5: Run all tests**

Run: `cd daemon && go test ./internal/daemon/ -v -timeout 60s`
Expected: PASS — both new tests and all existing tests

- [ ] **Step 6: Commit**

```bash
git add daemon/internal/daemon/worker.go daemon/internal/daemon/worker_test.go
git commit -m "feat(daemon): add warm worker routing to WorkerPool"
```

---

### Task 9: Daemon wiring — init warm pool, update health, shutdown

**Files:**
- Modify: `daemon/internal/daemon/daemon.go`

- [ ] **Step 1: Add warm status to health response**

In `daemon/internal/daemon/daemon.go`, update the `healthResponse` struct:

```go
type warmStatus struct {
	Total int `json:"total"`
	Ready int `json:"ready"`
	Busy  int `json:"busy"`
}

type healthResponse struct {
	Status        string     `json:"status"`
	UptimeSec     int64      `json:"uptime_sec"`
	WorkersActive int        `json:"workers_active"`
	QueueDepth    int        `json:"queue_depth"`
	WarmWorkers   warmStatus `json:"warm_workers"`
}
```

Update `handleHealth` to populate warm worker status:

```go
func (d *Daemon) handleHealth(w http.ResponseWriter, r *http.Request) {
	activeTasks := d.queue.List(false)
	total, ready, busy := d.workerPool.WarmStatus()
	resp := healthResponse{
		Status:        "ok",
		UptimeSec:     int64(time.Since(d.startTime).Seconds()),
		WorkersActive: d.workerPool.Active(),
		QueueDepth:    len(activeTasks),
		WarmWorkers:   warmStatus{Total: total, Ready: ready, Busy: busy},
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp) //nolint:errcheck
}
```

- [ ] **Step 2: Start warm workers in Daemon.Run()**

In `daemon/internal/daemon/daemon.go`, in the `Run` method, add warm worker startup before the worker pool loop. Insert after the watchdog start and before `go d.workerPool.Run(ctx)`:

```go
// Start warm workers (pre-spawn for sub-second dispatch).
if err := d.workerPool.StartWarm(ctx); err != nil {
	log.Printf("warm pool: startup error: %v", err)
}
```

- [ ] **Step 3: Add warm field to submitTaskRequest and handler**

In `daemon/internal/daemon/daemon.go`, update:

```go
type submitTaskRequest struct {
	Prompt      string `json:"prompt"`
	ReplyTo     string `json:"reply_to"`
	Priority    int    `json:"priority"`
	AllowWrites bool   `json:"allow_writes"`
	TimeoutSec  int    `json:"timeout_sec"`
	Warm        bool   `json:"warm"`
}
```

Update `handleSubmitTask` to pass `req.Warm` to `Queue.Submit`:

```go
task, err := d.queue.Submit(req.Prompt, req.ReplyTo, req.Priority, req.AllowWrites, req.TimeoutSec, req.Warm)
```

- [ ] **Step 4: Run all tests**

Run: `cd daemon && go test ./... -v -timeout 120s`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add daemon/internal/daemon/daemon.go
git commit -m "feat(daemon): wire warm pool startup, health status, and API warm field"
```

---

### Task 10: CLI — add --warm flag to submit command

**Files:**
- Modify: `daemon/cmd/gobrrr/main.go`
- Modify: `daemon/internal/client/client.go`

- [ ] **Step 1: Add warm field to client submitTaskRequest**

In `daemon/internal/client/client.go`, update:

```go
type submitTaskRequest struct {
	Prompt      string `json:"prompt"`
	ReplyTo     string `json:"reply_to"`
	Priority    int    `json:"priority"`
	AllowWrites bool   `json:"allow_writes"`
	TimeoutSec  int    `json:"timeout_sec"`
	Warm        bool   `json:"warm"`
}
```

- [ ] **Step 2: Add warm parameter to SubmitTask method**

Update the `SubmitTask` signature and body:

```go
func (c *Client) SubmitTask(prompt, replyTo string, priority int, allowWrites bool, timeoutSec int, warm bool) (*daemon.Task, error) {
	body := submitTaskRequest{
		Prompt:      prompt,
		ReplyTo:     replyTo,
		Priority:    priority,
		AllowWrites: allowWrites,
		TimeoutSec:  timeoutSec,
		Warm:        warm,
	}
```

- [ ] **Step 3: Fix all callers of client.SubmitTask**

Search and update:

```bash
cd daemon && grep -rn 'SubmitTask(' --include='*.go'
```

Each existing caller gets `, false` appended (or the new warm variable for the submit command).

- [ ] **Step 4: Add --warm flag to submit command**

In `daemon/cmd/gobrrr/main.go`, add the variable:

```go
var (
	submitPrompt      string
	submitReplyTo     string
	submitPriority    int
	submitAllowWrites bool
	submitTimeout     int
	submitWarm        bool
)
```

Update the `submitCmd.RunE` to pass `submitWarm`:

```go
task, err := c.SubmitTask(submitPrompt, submitReplyTo, submitPriority, submitAllowWrites, submitTimeout, submitWarm)
```

Register the flag (add near the other submit flags):

```go
submitCmd.Flags().BoolVar(&submitWarm, "warm", false, "Route to warm worker for fast dispatch")
```

- [ ] **Step 5: Build and verify**

Run: `cd daemon && CGO_ENABLED=0 go build -o gobrrr ./cmd/gobrrr/ && ./gobrrr submit --help`
Expected: Build succeeds, `--warm` flag visible in help output

- [ ] **Step 6: Run all tests**

Run: `cd daemon && go test ./... -v -timeout 120s`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add daemon/internal/client/client.go daemon/cmd/gobrrr/main.go
git commit -m "feat(cli): add --warm flag to gobrrr submit"
```
