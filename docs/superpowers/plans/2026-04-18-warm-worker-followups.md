# Warm Worker Pool Follow-ups Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Address five code-review follow-ups from the warm worker pool branch: test coverage gap, state-access encapsulation, subprocess leak on init cancellation, missing per-task timeout in `Run()`, and silent malformed-NDJSON tolerance in protocol readers.

**Architecture:** Five independent fixes to the warm worker subsystem (`daemon/internal/daemon/warm_*.go`) plus one config test. Tasks are ordered Tidy-First: structural/test additions before behavioral changes. Each task is its own commit.

**Tech Stack:** Go 1.22+, `os/exec`, `bufio.Scanner`, `context.Context`, `testify/assert`, `testify/require`.

---

## Task 1: Add WarmWorkers default assertion to config test

**Why:** `TestDefaultConfig` doesn't assert the default value for `WarmWorkers`. Any regression silently breaks warm-worker provisioning.

**Files:**
- Modify: `daemon/internal/config/config_test.go:14-21` (`TestDefaultConfig`)

- [ ] **Step 1: Add the assertion**

Open `daemon/internal/config/config_test.go` and add one line inside `TestDefaultConfig`:

```go
func TestDefaultConfig(t *testing.T) {
	cfg := config.Default()
	assert.Equal(t, 2, cfg.MaxWorkers)
	assert.Equal(t, 300, cfg.DefaultTimeoutSec)
	assert.Equal(t, 5, cfg.SpawnIntervalSec)
	assert.Equal(t, 1, cfg.WarmWorkers)
	assert.Equal(t, 7, cfg.LogRetentionDays)
	assert.Equal(t, 60, cfg.UptimeKuma.IntervalSec)
}
```

- [ ] **Step 2: Run the test and verify it passes**

Run: `cd daemon && go test ./internal/config/ -run TestDefaultConfig -v`
Expected: PASS (default is already `1`, we're just locking it in).

- [ ] **Step 3: Commit**

```bash
git add daemon/internal/config/config_test.go
git commit -m "$(cat <<'EOF'
test(config): assert WarmWorkers default in TestDefaultConfig

Structural test addition — locks in the existing default of 1
warm worker so accidental changes surface in CI.

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 2: Encapsulate WarmWorker state via Status() method

**Why:** `WorkerPool.WarmStatus()` currently reaches directly into `ww.mu` and reads `ready`/`busy`/`disabled` fields. This breaks encapsulation and means every caller must know the locking protocol. Add a `Status()` method on `WarmWorker` that returns a snapshot under its own lock.

**Files:**
- Modify: `daemon/internal/daemon/warm_worker.go` (add `Status` method)
- Modify: `daemon/internal/daemon/worker.go:276-294` (`WarmStatus` method)
- Test: no new test — existing `TestDaemonHealth*` and warm tests exercise this path. We verify by running the whole package.

- [ ] **Step 1: Add failing test for Status() snapshot**

Open `daemon/internal/daemon/warm_worker_test.go` and append:

```go
func TestWarmWorkerStatusSnapshot(t *testing.T) {
	ww := NewWarmWorker(0, "", nil, nil)

	ready, busy, disabled := ww.Status()
	assert.False(t, ready)
	assert.False(t, busy)
	assert.False(t, disabled)

	ww.mu.Lock()
	ww.ready = true
	ww.busy = true
	ww.disabled = true
	ww.mu.Unlock()

	ready, busy, disabled = ww.Status()
	assert.True(t, ready)
	assert.True(t, busy)
	assert.True(t, disabled)
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd daemon && go test ./internal/daemon/ -run TestWarmWorkerStatusSnapshot -v`
Expected: FAIL — `ww.Status undefined`.

- [ ] **Step 3: Add Status method to WarmWorker**

Open `daemon/internal/daemon/warm_worker.go` and add after the `Disabled()` method (after line 316):

```go
// Status returns a snapshot of ready/busy/disabled under ww.mu.
// Prefer this over reaching into the worker's fields from the pool.
func (ww *WarmWorker) Status() (ready, busy, disabled bool) {
	ww.mu.Lock()
	defer ww.mu.Unlock()
	return ww.ready, ww.busy, ww.disabled
}
```

- [ ] **Step 4: Run the new test to verify it passes**

Run: `cd daemon && go test ./internal/daemon/ -run TestWarmWorkerStatusSnapshot -v`
Expected: PASS.

- [ ] **Step 5: Refactor WarmStatus to use Status()**

Open `daemon/internal/daemon/worker.go` and replace the `WarmStatus` method (lines 276-294) with:

```go
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
```

- [ ] **Step 6: Run full daemon tests**

Run: `cd daemon && go test ./internal/daemon/ -v -run 'TestWarm|TestDaemonHealth'`
Expected: PASS across all warm-worker and health tests.

- [ ] **Step 7: Commit**

```bash
git add daemon/internal/daemon/warm_worker.go daemon/internal/daemon/worker.go daemon/internal/daemon/warm_worker_test.go
git commit -m "$(cat <<'EOF'
refactor(daemon): encapsulate WarmWorker state behind Status()

Structural change — WorkerPool.WarmStatus no longer reaches into
WarmWorker.mu or its private fields. The new Status() method
returns a (ready, busy, disabled) snapshot under the worker's own
lock, keeping the locking discipline in one place.

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 3: Switch WarmWorker.Start() to exec.CommandContext

**Why:** `exec.Command` ignores context. The ctx-done goroutine that kills the process only registers AFTER the init handshake completes. If the daemon cancels ctx during init (e.g., shutdown, or init hang), the subprocess is orphaned. `exec.CommandContext` binds ctx to the process from creation — context cancellation triggers SIGKILL automatically.

**Files:**
- Modify: `daemon/internal/daemon/warm_worker.go:83-114` (`Start` method)
- Test: `daemon/internal/daemon/warm_worker_test.go`

- [ ] **Step 1: Write failing test for ctx cancellation during init**

Append to `daemon/internal/daemon/warm_worker_test.go`:

```go
// writeHangScript creates a script that prints nothing and sleeps forever.
// Used to simulate a claude process that never emits system/init.
func writeHangScript(t *testing.T, dir string) string {
	t.Helper()
	script := filepath.Join(dir, "mock-claude-hang.sh")
	content := `#!/bin/bash
# Emit nothing; block on read so the process does not exit.
exec sleep 3600
`
	require.NoError(t, os.WriteFile(script, []byte(content), 0755))
	return script
}

func TestWarmWorkerStartCancelledDuringInit(t *testing.T) {
	dir := t.TempDir()
	script := writeHangScript(t, dir)
	writeMockIdentity(t, dir)

	ww := NewWarmWorker(0, dir, &config.Config{WorkspacePath: dir}, nil)
	ww.command = script

	ctx, cancel := context.WithCancel(t.Context())

	errCh := make(chan error, 1)
	go func() { errCh <- ww.Start(ctx) }()

	// Give Start time to launch the subprocess and block on init read.
	time.Sleep(200 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		require.Error(t, err, "Start must return an error when ctx is cancelled mid-init")
	case <-time.After(5 * time.Second):
		t.Fatal("Start did not return after context cancellation — subprocess likely orphaned")
	}
}
```

Also ensure the import block at the top of `warm_worker_test.go` includes `"context"` — it isn't there yet. Update the imports to:

```go
import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/racterub/gobrrr/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd daemon && go test ./internal/daemon/ -run TestWarmWorkerStartCancelledDuringInit -v -timeout 30s`
Expected: FAIL — Start hangs indefinitely (test will time out at 5s), because the current code uses `exec.Command` and there is no ctx goroutine yet.

- [ ] **Step 3: Switch both exec.Command calls to exec.CommandContext**

Open `daemon/internal/daemon/warm_worker.go` and replace the branch at lines 97-114 with:

```go
	var cmd *exec.Cmd
	if ww.command != "claude" {
		// Test mode: run mock script with the same flags so argv-capture tests work.
		cmd = exec.CommandContext(ctx, "bash", ww.command, //nolint:gosec
			"--model", model,
			"--permission-mode", mode,
			"--settings", settingsPath,
		)
	} else {
		cmd = exec.CommandContext(ctx, "claude", "-p",
			"--model", model,
			"--permission-mode", mode,
			"--settings", settingsPath,
			"--input-format", "stream-json",
			"--output-format", "stream-json",
			"--verbose",
		)
	}
	cmd.Dir = workDir
```

- [ ] **Step 4: Run the cancellation test to verify it passes**

Run: `cd daemon && go test ./internal/daemon/ -run TestWarmWorkerStartCancelledDuringInit -v -timeout 30s`
Expected: PASS — ctx cancellation kills the subprocess, `readUntilInit` sees EOF, Start returns an error.

- [ ] **Step 5: Run the full warm-worker test suite to guard against regressions**

Run: `cd daemon && go test ./internal/daemon/ -run 'TestWarmWorker' -v -timeout 60s`
Expected: PASS on all existing warm-worker tests (Start, Run, crash, error-result, arg-capture, stderr, respawn, disabled, new ctx-cancel test).

- [ ] **Step 6: Commit**

```bash
git add daemon/internal/daemon/warm_worker.go daemon/internal/daemon/warm_worker_test.go
git commit -m "$(cat <<'EOF'
fix(daemon): bind warm worker subprocess to ctx from start

Switch WarmWorker.Start from exec.Command to exec.CommandContext so
the subprocess is killed when ctx is cancelled, even if cancellation
arrives during the init handshake before the ctx-done cleanup
goroutine registers.

Previously a daemon shutdown mid-init could orphan the claude
subprocess; added a regression test that cancels during init and
verifies Start returns instead of hanging.

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 4: Add per-task timeout to WarmWorker.Run()

**Why:** `Run()` calls `readUntilResult`, which blocks on `scanner.Scan()` until EOF or a result message. If the claude subprocess hangs mid-request, the dispatcher waits forever. Cold workers honor `task.TimeoutSec` via `time.NewTimer`. Warm workers must too. On timeout, treat the worker as crashed (kill the subprocess so `dispatchWarm` respawns it).

**Files:**
- Modify: `daemon/internal/daemon/warm_worker.go:198-223` (`Run` method)
- Test: `daemon/internal/daemon/warm_worker_test.go`

- [ ] **Step 1: Add ErrWarmTimeout sentinel**

At the top of `daemon/internal/daemon/warm_worker.go`, after the `respawnFlapWindow` constant (around line 25), add:

```go
// ErrWarmTimeout is returned by Run when the task exceeds TimeoutSec.
// The worker is killed and must be respawned by the caller.
var ErrWarmTimeout = errors.New("warm worker: timeout")
```

Add `"errors"` to the import block.

- [ ] **Step 2: Write failing test for Run timeout**

Append to `daemon/internal/daemon/warm_worker_test.go`:

```go
// writeSlowScript handles init + identity-ack normally, then never
// emits a result for the first real task. Used to verify Run timeout.
func writeSlowScript(t *testing.T, dir string) string {
	t.Helper()
	script := filepath.Join(dir, "mock-claude-slow.sh")
	content := `#!/bin/bash
echo '{"type":"system","subtype":"init","session_id":"slow-session"}'
# identity message + ack
read -r line
echo '{"type":"result","subtype":"success","result":"ready","is_error":false,"duration_ms":1}'
# first real task: read but never respond
read -r line
exec sleep 3600
`
	require.NoError(t, os.WriteFile(script, []byte(content), 0755))
	return script
}

func TestWarmWorkerRunTimeout(t *testing.T) {
	dir := t.TempDir()
	script := writeSlowScript(t, dir)
	writeMockIdentity(t, dir)

	ww := NewWarmWorker(0, dir, &config.Config{WorkspacePath: dir}, nil)
	ww.command = script

	ctx := t.Context()
	require.NoError(t, ww.Start(ctx))

	task := &Task{ID: "t_timeout", Prompt: "hang please", TimeoutSec: 1}
	start := time.Now()
	_, err := ww.Run(task)
	elapsed := time.Since(start)

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrWarmTimeout, "Run should return ErrWarmTimeout")
	assert.Less(t, elapsed, 5*time.Second, "Run must honor TimeoutSec and not block indefinitely")

	// After timeout the worker is killed — a follow-up Run should fail fast
	// because stdin/scanner were cleared by Stop.
	_, err = ww.Run(task)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not ready")
}
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `cd daemon && go test ./internal/daemon/ -run TestWarmWorkerRunTimeout -v -timeout 30s`
Expected: FAIL — Run hangs; test times out or reports no error.

- [ ] **Step 4: Rewrite Run to honor TimeoutSec**

Replace the `Run` method in `daemon/internal/daemon/warm_worker.go` (lines 198-223) with:

```go
// Run sends a task prompt to the warm worker and returns the result.
// The caller must Reserve() before calling Run() and Release() after.
// Run does not manage the busy flag.
//
// On timeout (task.TimeoutSec), the worker process is killed and
// ErrWarmTimeout is returned. Callers (dispatchWarm) treat this the
// same as a crash and respawn the slot subject to the anti-flap guard.
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

	type readResult struct {
		result *resultMsg
		err    error
	}
	readCh := make(chan readResult, 1)
	go func() {
		r, err := readUntilResult(scanner)
		readCh <- readResult{result: r, err: err}
	}()

	timeout := time.Duration(task.TimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = time.Hour // sane upper bound when TimeoutSec is unset
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case <-timer.C:
		// Kill the subprocess; readUntilResult will see EOF and exit.
		ww.Stop()
		<-readCh
		return "", fmt.Errorf("warm worker %d: %w", ww.id, ErrWarmTimeout)

	case rr := <-readCh:
		if rr.err != nil {
			return "", fmt.Errorf("warm worker %d: read: %w", ww.id, rr.err)
		}
		if rr.result.IsError {
			return "", fmt.Errorf("warm worker %d: %s", ww.id, strings.Join(rr.result.Errors, "; "))
		}
		return rr.result.Result, nil
	}
}
```

- [ ] **Step 5: Run the timeout test to verify it passes**

Run: `cd daemon && go test ./internal/daemon/ -run TestWarmWorkerRunTimeout -v -timeout 30s`
Expected: PASS — Run returns `ErrWarmTimeout` within ~1 second.

- [ ] **Step 6: Run all warm-worker tests to check for regressions**

Run: `cd daemon && go test ./internal/daemon/ -run 'TestWarmWorker' -v -timeout 60s`
Expected: PASS — including the existing Run, crash, and error-result tests (which should complete well under their TimeoutSec=10 budget).

- [ ] **Step 7: Commit**

```bash
git add daemon/internal/daemon/warm_worker.go daemon/internal/daemon/warm_worker_test.go
git commit -m "$(cat <<'EOF'
fix(daemon): honor TimeoutSec in warm worker Run dispatch

Previously WarmWorker.Run called readUntilResult directly and blocked
forever on scanner.Scan() if the claude process hung. Cold workers
already guard with time.NewTimer; warm workers now do the same.

On timeout the subprocess is killed and ErrWarmTimeout is returned.
The existing dispatchWarm logic treats this as a crash and respawns
the slot through the anti-flap guard.

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 5: Fail-fast on consecutive NDJSON parse failures

**Why:** `readUntilInit` and `readUntilResult` silently `continue` on any `json.Unmarshal` error. If claude emits persistently malformed output (upgrade gone wrong, wrong binary on PATH), the readers spin consuming CPU until the stream closes. Log and enforce a consecutive-failure budget so the failure surfaces quickly.

**Files:**
- Modify: `daemon/internal/daemon/warm_proto.go` (both readers)
- Test: `daemon/internal/daemon/warm_proto_test.go`

- [ ] **Step 1: Write failing tests for malformed-line behavior**

Append to `daemon/internal/daemon/warm_proto_test.go`:

```go
func TestReadUntilInitFailsAfterMalformedBurst(t *testing.T) {
	var sb strings.Builder
	// 101 malformed lines, then an init — exceeds the budget.
	for i := 0; i < 101; i++ {
		sb.WriteString("not json\n")
	}
	sb.WriteString(`{"type":"system","subtype":"init","session_id":"late"}` + "\n")

	scanner := bufio.NewScanner(strings.NewReader(sb.String()))
	err := readUntilInit(scanner)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "malformed")
}

func TestReadUntilInitToleratesSmallBurst(t *testing.T) {
	// A handful of malformed lines followed by init — under budget.
	input := "garbage\nmore garbage\n" +
		`{"type":"system","subtype":"init","session_id":"ok"}` + "\n"

	scanner := bufio.NewScanner(strings.NewReader(input))
	err := readUntilInit(scanner)
	require.NoError(t, err)
}

func TestReadUntilResultFailsAfterMalformedBurst(t *testing.T) {
	var sb strings.Builder
	for i := 0; i < 101; i++ {
		sb.WriteString("not json\n")
	}
	sb.WriteString(`{"type":"result","subtype":"success","result":"late","is_error":false,"duration_ms":1}` + "\n")

	scanner := bufio.NewScanner(strings.NewReader(sb.String()))
	_, err := readUntilResult(scanner)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "malformed")
}

func TestReadUntilResultResetsCounterOnValidLine(t *testing.T) {
	// Interleave malformed and valid-but-not-result lines; counter should
	// reset on each successful parse so a long stream survives.
	var sb strings.Builder
	for i := 0; i < 60; i++ {
		sb.WriteString("garbage\n")
	}
	// A valid (non-result) line resets the counter.
	sb.WriteString(`{"type":"assistant","message":{"role":"assistant","content":[]}}` + "\n")
	for i := 0; i < 60; i++ {
		sb.WriteString("garbage\n")
	}
	sb.WriteString(`{"type":"result","subtype":"success","result":"ok","is_error":false,"duration_ms":1}` + "\n")

	scanner := bufio.NewScanner(strings.NewReader(sb.String()))
	result, err := readUntilResult(scanner)
	require.NoError(t, err)
	assert.Equal(t, "ok", result.Result)
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd daemon && go test ./internal/daemon/ -run 'TestReadUntilInitFailsAfterMalformedBurst|TestReadUntilInitToleratesSmallBurst|TestReadUntilResultFailsAfterMalformedBurst|TestReadUntilResultResetsCounterOnValidLine' -v`
Expected: FAIL — the current readers either hang (EOF eventually, since our inputs end with a valid message they pass the "fail" tests wrongly by succeeding) OR do not return "malformed" error text. Specifically `TestReadUntilInitFailsAfterMalformedBurst` and `TestReadUntilResultFailsAfterMalformedBurst` will fail because the current behavior eventually finds the valid line and returns success.

- [ ] **Step 3: Add the budget + logging to both readers**

Replace the whole body of `daemon/internal/daemon/warm_proto.go` starting from the `readUntilInit` function (line 58) through the end with:

```go
// maxConsecutiveParseFailures caps silent NDJSON parse failures. Past this,
// the reader gives up and returns an error instead of spinning forever on
// a malformed stream (e.g., wrong binary on PATH, upgrade produced garbage).
const maxConsecutiveParseFailures = 100

// readUntilInit reads NDJSON lines from scanner until a system/init message.
// Non-init lines that parse cleanly are skipped. Lines that fail to parse
// are logged and counted — the reader fails fast once maxConsecutiveParseFailures
// is exceeded without an intervening valid line.
func readUntilInit(scanner *bufio.Scanner) error {
	consecutiveFailures := 0
	for scanner.Scan() {
		var msg streamMsg
		if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
			consecutiveFailures++
			if consecutiveFailures == 1 {
				log.Printf("warm proto: malformed NDJSON line (init): %s", truncateLine(scanner.Bytes()))
			}
			if consecutiveFailures > maxConsecutiveParseFailures {
				return fmt.Errorf("reading stdout: exceeded %d consecutive malformed lines before init", maxConsecutiveParseFailures)
			}
			continue
		}
		consecutiveFailures = 0
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
// All non-result lines that parse cleanly are skipped. Malformed lines are
// logged and counted — the reader fails fast once maxConsecutiveParseFailures
// is exceeded without an intervening valid line.
func readUntilResult(scanner *bufio.Scanner) (*resultMsg, error) {
	consecutiveFailures := 0
	for scanner.Scan() {
		var msg streamMsg
		if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
			consecutiveFailures++
			if consecutiveFailures == 1 {
				log.Printf("warm proto: malformed NDJSON line (result): %s", truncateLine(scanner.Bytes()))
			}
			if consecutiveFailures > maxConsecutiveParseFailures {
				return nil, fmt.Errorf("reading stdout: exceeded %d consecutive malformed lines before result", maxConsecutiveParseFailures)
			}
			continue
		}
		consecutiveFailures = 0
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

// truncateLine returns up to 120 bytes of line for logging; callers shouldn't
// dump 10MB of garbage into logs when a stream goes bad.
func truncateLine(b []byte) string {
	const max = 120
	if len(b) <= max {
		return string(b)
	}
	return string(b[:max]) + "..."
}
```

Add `"log"` to the import block at the top of the file.

- [ ] **Step 4: Run the new tests to verify they pass**

Run: `cd daemon && go test ./internal/daemon/ -run 'TestReadUntilInitFailsAfterMalformedBurst|TestReadUntilInitToleratesSmallBurst|TestReadUntilResultFailsAfterMalformedBurst|TestReadUntilResultResetsCounterOnValidLine' -v`
Expected: PASS.

- [ ] **Step 5: Run all warm-proto tests to check for regressions**

Run: `cd daemon && go test ./internal/daemon/ -run 'TestReadUntil|TestWriteUserMessage' -v`
Expected: PASS.

- [ ] **Step 6: Run the whole daemon package to catch any broader breakage**

Run: `cd daemon && go test ./... -timeout 120s`
Expected: PASS across the whole module.

- [ ] **Step 7: Commit**

```bash
git add daemon/internal/daemon/warm_proto.go daemon/internal/daemon/warm_proto_test.go
git commit -m "$(cat <<'EOF'
fix(daemon): fail-fast on malformed NDJSON bursts in warm proto

readUntilInit and readUntilResult previously swallowed all json
parse failures, so a persistently malformed stdout stream would
spin until EOF. Now each reader logs the first malformed line and
gives up after 100 consecutive parse failures without an
intervening valid line.

Surfaces misconfiguration (wrong binary on PATH, upgrade regression)
in seconds rather than minutes, and keeps a generous budget so
transient garbage doesn't break working streams.

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Final Verification

- [ ] **Step A: Run the full test suite**

Run: `cd daemon && go test ./... -timeout 180s`
Expected: PASS on every package.

- [ ] **Step B: Build the binary**

Run: `cd daemon && CGO_ENABLED=0 go build -o /tmp/gobrrr-verify ./cmd/gobrrr/`
Expected: success, no build errors.

- [ ] **Step C: Remove the follow-up section from TODO.md**

Open `TODO.md` and delete the entire `## Warm Worker Pool Follow-ups` section (lines 42-74 at the time of writing) since all five items are now demonstrably done. Leave the other sections untouched.

- [ ] **Step D: Commit the TODO cleanup**

```bash
git add TODO.md
git commit -m "$(cat <<'EOF'
docs(todo): drop warm worker follow-ups — all five items landed

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```
