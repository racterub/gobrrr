# Async Dispatch with Result Context — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Push completed worker results into the Claude Code session via the channel protocol, so the session stays responsive and retains context of worker output.

**Architecture:** The daemon gains an SSE endpoint that streams completed task results. A thin Bun/TypeScript MCP channel bridge subscribes to the stream and pushes results into the Claude Code session as `<channel source="gobrrr">` events. The folder is restructured to separate daemon (Go) and channel (Bun) components.

**Tech Stack:** Go 1.25, Bun, TypeScript, @modelcontextprotocol/sdk, SSE, Unix sockets

**Spec:** `docs/specs/2026-03-25-async-dispatch-design.md`

---

## Phase 1: Folder Restructure (Structural Change)

This is a purely structural change — no behavior modifications. All Go code moves under `daemon/`, creating space for the `channel/` component.

### Task 1: Move Go code under `daemon/`

**Files:**
- Move: `cmd/` → `daemon/cmd/`
- Move: `internal/` → `daemon/internal/`
- Move: `go.mod`, `go.sum` → `daemon/go.mod`, `daemon/go.sum`
- Move: `skills/` → `daemon/skills/`
- Move: `systemd/` → `daemon/systemd/`
- Move: `scripts/` → `daemon/scripts/`

- [ ] **Step 1: Create the daemon directory and move files**

```bash
mkdir -p daemon
git mv cmd daemon/cmd
git mv internal daemon/internal
git mv go.mod daemon/go.mod
git mv go.sum daemon/go.sum
git mv skills daemon/skills
git mv systemd daemon/systemd
git mv scripts daemon/scripts
```

- [ ] **Step 2: Update Go module path**

In `daemon/go.mod`, the module path stays `github.com/racterub/gobrrr` — no import path changes needed since the Go module root is now `daemon/` and all internal imports are relative to the module.

Actually, Go modules resolve imports relative to the module root. Since `go.mod` moves into `daemon/`, and all code is under `daemon/`, the import paths like `github.com/racterub/gobrrr/internal/daemon` still resolve correctly because `internal/daemon` is relative to where `go.mod` lives.

Verify: `cd daemon && go build ./cmd/gobrrr/`

- [ ] **Step 3: Update CLAUDE.md build instructions**

In `CLAUDE.md`, update the build command:

```markdown
## Build

```bash
cd daemon && CGO_ENABLED=0 go build -o gobrrr ./cmd/gobrrr/
```
```

And update the test command:

```markdown
## Test

```bash
cd daemon && go test ./...
```
```

- [ ] **Step 4: Update Project Structure section in CLAUDE.md**

Update to reflect the new layout:

```
daemon/
  cmd/gobrrr/main.go          CLI entrypoint (cobra)
  internal/
    ...same as before...
  skills/                      SKILL.md files
  systemd/                     gobrrr.service unit
  scripts/                     setup.sh, uninstall.sh
channel/
  index.ts                     MCP channel bridge
  package.json
docs/
Makefile
TODO.md
CLAUDE.md
```

- [ ] **Step 5: Update systemd unit ExecStart path**

Read `daemon/systemd/gobrrr.service` and update the `ExecStart` path if it references the old build location.

- [ ] **Step 6: Update scripts/setup.sh**

Read `daemon/scripts/setup.sh` and update any paths that reference the old directory structure (build commands, binary paths).

- [ ] **Step 7: Verify build and tests pass**

```bash
cd daemon && CGO_ENABLED=0 go build -o gobrrr ./cmd/gobrrr/ && go test ./...
```

- [ ] **Step 8: Commit structural change**

```bash
git add -A
git commit -m "refactor: move Go code under daemon/ directory

Structural change only — no behavior modifications.
Prepares for channel/ component alongside daemon/."
```

### Task 2: Create root Makefile

**Files:**
- Create: `Makefile`

- [ ] **Step 1: Write the Makefile**

```makefile
.PHONY: build build-daemon build-channel dev dev-channel test test-daemon test-channel install clean

# Build
build: build-daemon build-channel

build-daemon:
	cd daemon && CGO_ENABLED=0 go build -o gobrrr ./cmd/gobrrr/

build-channel:
	cd channel && bun install

# Dev
dev:
	cd daemon && go run ./cmd/gobrrr/ daemon start

dev-channel:
	cd channel && bun run index.ts

# Test
test: test-daemon test-channel

test-daemon:
	cd daemon && go test ./...

test-channel:
	@echo "No channel tests yet"

# Install
install: build
	cp daemon/gobrrr ~/.local/bin/gobrrr
	cd channel && bun install
	@echo "Binary installed to ~/.local/bin/gobrrr"
	@echo "Run 'gobrrr setup' to configure"

clean:
	rm -f daemon/gobrrr
```

- [ ] **Step 2: Verify `make build-daemon` and `make test-daemon` work**

```bash
make build-daemon && make test-daemon
```

- [ ] **Step 3: Commit**

```bash
git add Makefile
git commit -m "feat: add root Makefile for multi-component build"
```

---

## Phase 2: Daemon SSE Endpoint (Behavioral Change)

### Task 3: Add `Delivered` field to Task struct

**Files:**
- Modify: `daemon/internal/daemon/queue.go` (Task struct, around line 16)
- Test: `daemon/internal/daemon/queue_test.go`

- [ ] **Step 1: Write failing test for Delivered field persistence**

In `daemon/internal/daemon/queue_test.go`, add a test that verifies the Delivered field round-trips through JSON persistence:

Note: `queue_test.go` uses `package daemon_test` (external test package). All types and functions must be prefixed with `daemon.`.

```go
func TestTaskDeliveredFieldPersistence(t *testing.T) {
	dir := t.TempDir()
	q := daemon.NewQueue(filepath.Join(dir, "queue.json"))

	task, err := q.Submit("test prompt", "channel", 0, false, 300)
	require.NoError(t, err)

	// Complete the task
	err = q.Complete(task.ID, "result")
	require.NoError(t, err)

	// Mark delivered
	err = q.MarkDelivered(task.ID)
	require.NoError(t, err)

	// Reload from disk
	q2, err := daemon.LoadQueue(filepath.Join(dir, "queue.json"))
	require.NoError(t, err)

	reloaded, err := q2.Get(task.ID)
	require.NoError(t, err)
	require.NotNil(t, reloaded)
	assert.True(t, reloaded.Delivered)
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd daemon && go test ./internal/daemon/ -run TestTaskDeliveredFieldPersistence -v
```

Expected: FAIL — `Delivered` field and `MarkDelivered` method don't exist yet.

- [ ] **Step 3: Add Delivered field and MarkDelivered method**

In `daemon/internal/daemon/queue.go`, add to the Task struct:

```go
Delivered bool `json:"delivered"`
```

Add after the existing `Fail` method:

```go
// MarkDelivered marks a completed task as delivered via SSE stream.
func (q *Queue) MarkDelivered(id string) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	t, err := q.findLocked(id)
	if err != nil {
		return err
	}
	t.Delivered = true
	return q.flush()
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
cd daemon && go test ./internal/daemon/ -run TestTaskDeliveredFieldPersistence -v
```

- [ ] **Step 5: Run all existing tests to ensure no regressions**

```bash
cd daemon && go test ./...
```

- [ ] **Step 6: Commit**

```bash
cd daemon && git add internal/daemon/queue.go internal/daemon/queue_test.go
git commit -m "feat: add Delivered field to Task struct for SSE tracking"
```

### Task 4: Multi-destination routing

**Files:**
- Modify: `daemon/internal/daemon/routing.go` (routeResult function, around line 21)
- Test: `daemon/internal/daemon/routing_test.go`

- [ ] **Step 1: Write failing test for comma-separated reply-to**

In `daemon/internal/daemon/routing_test.go`, add a test for multi-destination routing:

Note: `routing_test.go` uses `package daemon` (internal test package) and `newTestDaemon(t, notifier)` which takes a `*telegram.Notifier`, not a directory. The test daemon creates its own temp dir internally.

```go
func TestRouteResultMultiDestination(t *testing.T) {
	d := newTestDaemon(t, nil)

	// Use the daemon's gobrrDir for allowed file paths
	outPath := filepath.Join(d.gobrrDir, "output", "result.txt")
	task := &Task{
		ID:      "t_multi",
		ReplyTo: "file:" + outPath + ",stdout",
		Status:  "completed",
	}
	result := "multi-destination result"

	err := d.routeResult(task, result)
	require.NoError(t, err)

	// File should have been written
	content, err := os.ReadFile(outPath)
	require.NoError(t, err)
	assert.Equal(t, result, string(content))

	// Result should also be set on task (stdout path)
	assert.NotNil(t, task.Result)
	assert.Equal(t, result, *task.Result)
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd daemon && go test ./internal/daemon/ -run TestRouteResultMultiDestination -v
```

Expected: FAIL — current routeResult only handles single destination.

- [ ] **Step 3: Refactor routeResult for multi-destination**

In `daemon/internal/daemon/routing.go`, replace the current `routeResult` function:

Note: Sanitization behavior is preserved per-destination (only for `telegram` and `channel`), matching the original code. The `errors` package must be added to imports.

```go
func (d *Daemon) routeResult(task *Task, result string) error {
	destinations := strings.Split(task.ReplyTo, ",")
	var errs []error

	for _, dest := range destinations {
		dest = strings.TrimSpace(dest)
		if err := d.routeToDestination(task, result, dest); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", dest, err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("routing errors: %v", errors.Join(errs...))
	}
	return nil
}

func (d *Daemon) routeToDestination(task *Task, result, dest string) error {
	switch {
	case dest == "telegram":
		if d.notifier == nil {
			return fmt.Errorf("telegram not configured")
		}
		scan := security.Check(result, d.knownSecrets())
		if scan.HasLeak {
			d.quarantineResult(task, result, scan.Matches)
			return d.notifier.Send("\u26a0\ufe0f Task result contained potential credential leak and was quarantined. Check logs.")
		}
		return d.notifier.Send(result)
	case dest == "stdout":
		task.Result = &result
		return nil
	case strings.HasPrefix(dest, "file:"):
		path := strings.TrimPrefix(dest, "file:")
		return d.writeFileResult(path, result)
	case dest == "channel":
		scan := security.Check(result, d.knownSecrets())
		if scan.HasLeak {
			d.quarantineResult(task, result, scan.Matches)
			return fmt.Errorf("credential leak detected, result quarantined")
		}
		return d.emitToSSE(task, result)
	case dest == "":
		return nil
	default:
		return fmt.Errorf("unknown reply_to: %s", dest)
	}
}
```

Add a placeholder for `emitToSSE`:

```go
func (d *Daemon) emitToSSE(task *Task, result string) error {
	// Implemented in Task 5
	return nil
}
```

- [ ] **Step 4: Run tests**

```bash
cd daemon && go test ./internal/daemon/ -run TestRouteResult -v
```

- [ ] **Step 5: Run all tests**

```bash
cd daemon && go test ./...
```

- [ ] **Step 6: Commit**

```bash
cd daemon && git add internal/daemon/routing.go internal/daemon/routing_test.go
git commit -m "feat: support multi-destination reply-to routing

Split routeResult into destination iteration with per-destination
error collection. Adds 'channel' destination (SSE placeholder).
Sanitization behavior preserved per-destination."
```

### Task 5: SSE streaming endpoint with fan-out

**Files:**
- Create: `daemon/internal/daemon/sse.go`
- Create: `daemon/internal/daemon/sse_test.go`
- Modify: `daemon/internal/daemon/daemon.go` (register route, wire fan-out)

- [ ] **Step 1: Write failing test for SSE fan-out**

Create `daemon/internal/daemon/sse_test.go`:

```go
package daemon

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSSEHub_SubscribeAndEmit(t *testing.T) {
	hub := NewSSEHub()

	ch := hub.Subscribe()
	defer hub.Unsubscribe(ch)

	event := TaskResultEvent{
		TaskID:        "t_123",
		Status:        "completed",
		PromptSummary: "check gmail",
		Result:        "You have 3 unread emails",
		Error:         "",
		SubmittedAt:   time.Now().UTC(),
	}

	hub.Emit(event)

	select {
	case received := <-ch:
		assert.Equal(t, event.TaskID, received.TaskID)
		assert.Equal(t, event.Result, received.Result)
	case <-time.After(time.Second):
		t.Fatal("did not receive event")
	}
}

func TestSSEHub_SlowClientDropsEvents(t *testing.T) {
	hub := NewSSEHub()

	ch := hub.Subscribe()
	defer hub.Unsubscribe(ch)

	// Fill the buffer (capacity 64)
	for i := 0; i < 70; i++ {
		hub.Emit(TaskResultEvent{TaskID: fmt.Sprintf("t_%d", i)})
	}

	// Should have received 64, dropped 6
	count := 0
	for {
		select {
		case <-ch:
			count++
		default:
			goto done
		}
	}
done:
	assert.Equal(t, 64, count)
}

func TestSSEHub_UnsubscribeCleansUp(t *testing.T) {
	hub := NewSSEHub()

	ch := hub.Subscribe()
	assert.Equal(t, 1, hub.ClientCount())

	hub.Unsubscribe(ch)
	assert.Equal(t, 0, hub.ClientCount())
}

func TestTaskResultEvent_JSON(t *testing.T) {
	event := TaskResultEvent{
		TaskID:        "t_123",
		Status:        "completed",
		PromptSummary: "check gmail for unread",
		Result:        "3 emails",
		Error:         "",
		SubmittedAt:   time.Date(2026, 3, 25, 1, 30, 0, 0, time.UTC),
	}

	data, err := json.Marshal(event)
	require.NoError(t, err)

	var decoded TaskResultEvent
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, event.TaskID, decoded.TaskID)
	assert.Equal(t, event.PromptSummary, decoded.PromptSummary)
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd daemon && go test ./internal/daemon/ -run TestSSE -v
```

Expected: FAIL — `SSEHub`, `TaskResultEvent` don't exist.

- [ ] **Step 3: Implement SSE hub and event types**

Create `daemon/internal/daemon/sse.go`:

```go
package daemon

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// TaskResultEvent is the SSE payload for a completed task.
type TaskResultEvent struct {
	TaskID        string    `json:"task_id"`
	Status        string    `json:"status"`
	PromptSummary string    `json:"prompt_summary"`
	Result        string    `json:"result"`
	Error         string    `json:"error"`
	SubmittedAt   time.Time `json:"submitted_at"`
}

const sseBufferSize = 64

// SSEHub manages fan-out of task result events to connected SSE clients.
type SSEHub struct {
	mu      sync.Mutex
	clients map[chan TaskResultEvent]struct{}
}

func NewSSEHub() *SSEHub {
	return &SSEHub{
		clients: make(map[chan TaskResultEvent]struct{}),
	}
}

func (h *SSEHub) Subscribe() chan TaskResultEvent {
	h.mu.Lock()
	defer h.mu.Unlock()
	ch := make(chan TaskResultEvent, sseBufferSize)
	h.clients[ch] = struct{}{}
	return ch
}

func (h *SSEHub) Unsubscribe(ch chan TaskResultEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.clients, ch)
	close(ch)
}

func (h *SSEHub) ClientCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.clients)
}

// Emit sends an event to all clients. Non-blocking — drops if a client's buffer is full.
func (h *SSEHub) Emit(event TaskResultEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.clients {
		select {
		case ch <- event:
		default:
			// Client too slow, drop event
		}
	}
}

// TruncateRunes returns the first n runes of s.
func TruncateRunes(s string, n int) string {
	count := 0
	for i := range s {
		if count >= n {
			return s[:i]
		}
		count++
	}
	return s
}

// BuildTaskResultEvent creates a TaskResultEvent from a completed task.
func BuildTaskResultEvent(task *Task, result string) TaskResultEvent {
	errStr := ""
	if task.Error != nil {
		errStr = *task.Error
	}
	return TaskResultEvent{
		TaskID:        task.ID,
		Status:        task.Status,
		PromptSummary: TruncateRunes(task.Prompt, 100),
		Result:        result,
		Error:         errStr,
		SubmittedAt:   task.CreatedAt,
	}
}

// ServeSSE handles the GET /tasks/results/stream endpoint.
func (h *SSEHub) ServeSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch := h.Subscribe()
	defer h.Unsubscribe(ch)

	// Send initial keepalive
	fmt.Fprintf(w, ": connected\n\n")
	flusher.Flush()

	for {
		select {
		case event, ok := <-ch:
			if !ok {
				return
			}
			data, err := json.Marshal(event)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}
```

- [ ] **Step 4: Add TruncateRunes and BuildTaskResultEvent tests**

Add to `daemon/internal/daemon/sse_test.go`:

```go
func TestTruncateRunes(t *testing.T) {
	// ASCII
	assert.Equal(t, "hello", TruncateRunes("hello world", 5))
	// Multi-byte (Chinese characters are 3 bytes each)
	assert.Equal(t, "你好", TruncateRunes("你好世界", 2))
	// Short string unchanged
	assert.Equal(t, "hi", TruncateRunes("hi", 100))
	// Empty
	assert.Equal(t, "", TruncateRunes("", 10))
}

func TestBuildTaskResultEvent(t *testing.T) {
	now := time.Now().UTC()
	errMsg := "something went wrong"
	task := &Task{
		ID:        "t_123",
		Prompt:    "check gmail for unread messages from alice",
		Status:    "failed",
		CreatedAt: now,
		Error:     &errMsg,
	}

	event := BuildTaskResultEvent(task, "partial output")
	assert.Equal(t, "t_123", event.TaskID)
	assert.Equal(t, "failed", event.Status)
	assert.Equal(t, "check gmail for unread messages from alice", event.PromptSummary)
	assert.Equal(t, "partial output", event.Result)
	assert.Equal(t, "something went wrong", event.Error)
	assert.Equal(t, now, event.SubmittedAt)

	// Test with nil Error
	task.Error = nil
	event = BuildTaskResultEvent(task, "result")
	assert.Equal(t, "", event.Error)
}
```

- [ ] **Step 5: Run tests**

```bash
cd daemon && go test ./internal/daemon/ -run "TestSSE|TestTruncate|TestTaskResultEvent" -v
```

- [ ] **Step 6: Commit**

```bash
cd daemon && git add internal/daemon/sse.go internal/daemon/sse_test.go
git commit -m "feat: SSE hub for task result streaming

Non-blocking fan-out with bounded buffers (cap 64).
Slow clients drop events rather than blocking workers.
Includes rune-safe prompt truncation."
```

### Task 6: Wire SSE into daemon

**Files:**
- Modify: `daemon/internal/daemon/daemon.go` (add SSEHub field, register route)
- Modify: `daemon/internal/daemon/routing.go` (implement emitToSSE)
- Test: `daemon/internal/daemon/daemon_test.go`

- [ ] **Step 1: Write failing integration test**

In `daemon/internal/daemon/daemon_test.go`, add a test that submits a task with `reply_to: "channel"` and verifies the result arrives on the SSE stream.

Read the existing test file first to understand patterns. The test should:
1. Create a test daemon
2. Connect to `GET /tasks/results/stream` in a goroutine
3. Submit and complete a task with `reply_to: "channel"`
4. Verify the SSE event arrives with correct payload

- [ ] **Step 2: Run test to verify it fails**

```bash
cd daemon && go test ./internal/daemon/ -run TestSSEIntegration -v
```

- [ ] **Step 3: Add SSEHub to Daemon struct**

In `daemon/internal/daemon/daemon.go`:

1. Add `sseHub *SSEHub` field to the `Daemon` struct
2. Initialize in `New()`: `sseHub: NewSSEHub(),`
3. Register route in `registerRoutes()`: `d.mux.HandleFunc("GET /tasks/results/stream", d.sseHub.ServeSSE)`

- [ ] **Step 4: Implement emitToSSE in routing.go**

Replace the placeholder in `daemon/internal/daemon/routing.go`:

```go
func (d *Daemon) emitToSSE(task *Task, result string) error {
	if d.sseHub == nil {
		return nil
	}
	event := BuildTaskResultEvent(task, result)
	d.sseHub.Emit(event)
	if d.sseHub.ClientCount() > 0 {
		_ = d.queue.MarkDelivered(task.ID)
	}
	return nil
}
```

- [ ] **Step 5: Run integration test**

```bash
cd daemon && go test ./internal/daemon/ -run TestSSEIntegration -v
```

- [ ] **Step 6: Run all tests**

```bash
cd daemon && go test ./...
```

- [ ] **Step 7: Commit**

```bash
cd daemon && git add internal/daemon/daemon.go internal/daemon/routing.go internal/daemon/daemon_test.go
git commit -m "feat: wire SSE hub into daemon with channel routing

GET /tasks/results/stream streams completed task results.
reply_to 'channel' routes through SSE fan-out.
Tasks marked delivered after SSE emission."
```

---

## Phase 3: Channel Bridge (Behavioral Change)

### Task 7: Create channel bridge project

**Files:**
- Create: `channel/package.json`
- Create: `channel/index.ts`

- [ ] **Step 1: Initialize the channel project**

```bash
cd channel && bun init -y
```

- [ ] **Step 2: Install MCP SDK**

```bash
cd channel && bun add @modelcontextprotocol/sdk
```

- [ ] **Step 3: Write `channel/index.ts`**

```typescript
#!/usr/bin/env bun
import { Server } from "@modelcontextprotocol/sdk/server/index.js";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import { createConnection } from "net";
import { homedir } from "os";
import { join } from "path";

const SOCKET_PATH = join(homedir(), ".gobrrr", "gobrrr.sock");
const RECONNECT_BASE_MS = 1000;
const RECONNECT_MAX_MS = 30000;

const mcp = new Server(
  { name: "gobrrr", version: "0.0.1" },
  {
    capabilities: {
      experimental: { "claude/channel": {} },
    },
    instructions:
      'Task results from gobrrr workers arrive as <channel source="gobrrr" task_id="..." status="..." prompt_summary="...">. ' +
      "These are results from tasks you previously dispatched via `gobrrr submit`. " +
      "Read the result and decide whether to act on it, relay it to the user, or absorb it silently.",
  }
);

await mcp.connect(new StdioServerTransport());

function connectToStream(attempt: number = 0) {
  const socket = createConnection(SOCKET_PATH, () => {
    // Send HTTP request over Unix socket
    socket.write(
      "GET /tasks/results/stream HTTP/1.1\r\n" +
        "Host: gobrrr\r\n" +
        "Accept: text/event-stream\r\n" +
        "\r\n"
    );
    attempt = 0; // Reset on successful connection
  });

  let buffer = "";
  let headersParsed = false;

  socket.on("data", (chunk: Buffer) => {
    buffer += chunk.toString();

    // Skip HTTP headers on first data
    if (!headersParsed) {
      const headerEnd = buffer.indexOf("\r\n\r\n");
      if (headerEnd === -1) return;
      buffer = buffer.slice(headerEnd + 4);
      headersParsed = true;
    }

    // Parse SSE events from buffer
    const lines = buffer.split("\n");
    buffer = lines.pop() ?? ""; // Keep incomplete line in buffer

    for (const line of lines) {
      if (line.startsWith("data: ")) {
        const jsonStr = line.slice(6).trim();
        if (!jsonStr) continue;
        try {
          const event = JSON.parse(jsonStr);
          pushToSession(event);
        } catch {
          // Ignore malformed JSON
        }
      }
    }
  });

  socket.on("error", () => {
    scheduleReconnect(attempt);
  });

  socket.on("close", () => {
    scheduleReconnect(attempt);
  });
}

function scheduleReconnect(attempt: number) {
  const delay = Math.min(
    RECONNECT_BASE_MS * Math.pow(2, attempt) + Math.random() * 1000,
    RECONNECT_MAX_MS
  );
  setTimeout(() => connectToStream(attempt + 1), delay);
}

async function pushToSession(event: {
  task_id: string;
  status: string;
  prompt_summary: string;
  result: string;
  error: string;
  submitted_at: string;
}) {
  const content = event.error
    ? `Error: ${event.error}\n\n${event.result}`
    : event.result;

  await mcp.notification({
    method: "notifications/claude/channel",
    params: {
      content,
      meta: {
        task_id: event.task_id,
        status: event.status,
        prompt_summary: event.prompt_summary,
      },
    },
  });
}

// Start the SSE connection
connectToStream();
```

- [ ] **Step 4: Verify TypeScript compiles**

```bash
cd channel && bun build --target=bun index.ts --outdir=dist
```

- [ ] **Step 5: Commit**

```bash
git add channel/
git commit -m "feat: channel bridge for pushing worker results to session

Thin Bun MCP server that connects to gobrrr daemon SSE stream
and pushes completed task results as channel events.
Reconnects with exponential backoff on disconnect."
```

### Task 8: Add channel MCP registration to install

**Files:**
- Modify: `Makefile` (update install target)

- [ ] **Step 1: Update Makefile install target**

Add channel registration logic to the `install` target. The channel needs to be registered in the user's `.mcp.json` or the project's MCP config. For now, print instructions:

```makefile
install: build
	cp daemon/gobrrr ~/.local/bin/gobrrr
	cd channel && bun install
	@echo ""
	@echo "Binary installed to ~/.local/bin/gobrrr"
	@echo ""
	@echo "To register the channel, add to your .mcp.json:"
	@echo '  "gobrrr": { "command": "bun", "args": ["$(CURDIR)/channel/index.ts"] }'
	@echo ""
	@echo "Then start Claude Code with:"
	@echo "  claude --dangerously-load-development-channels server:gobrrr"
```

- [ ] **Step 2: Commit**

```bash
git add Makefile
git commit -m "feat: update install target with channel registration instructions"
```

---

## Phase 4: End-to-End Verification

### Task 9: Manual integration test

This task verifies the full flow end-to-end. No code changes — just verification steps.

- [ ] **Step 1: Build everything**

```bash
make build
```

- [ ] **Step 2: Start the daemon**

```bash
make dev
```

- [ ] **Step 3: Test SSE endpoint with curl**

In a separate terminal:

```bash
curl -N --unix-socket ~/.gobrrr/gobrrr.sock http://gobrrr/tasks/results/stream
```

Should see `: connected` and then hang (waiting for events).

- [ ] **Step 4: Submit a task with channel reply-to**

```bash
gobrrr submit --prompt "echo hello" --reply-to channel
```

- [ ] **Step 5: Verify SSE event arrives in curl**

The curl terminal should show a `data: {...}` line with the task result.

- [ ] **Step 6: Test multi-destination routing**

```bash
gobrrr submit --prompt "echo multi" --reply-to "channel,stdout"
```

Verify: SSE event arrives AND `gobrrr status <id>` shows the result.

- [ ] **Step 7: Test channel bridge standalone**

Register in `.mcp.json` and start Claude Code:

```bash
claude --dangerously-load-development-channels server:gobrrr
```

Submit a task and verify the `<channel source="gobrrr">` event appears in the session.

- [ ] **Step 8: Update TODO.md**

Remove or mark the "Async Dispatch with Result Context" section as completed.

- [ ] **Step 9: Final commit**

```bash
git add TODO.md
git commit -m "docs: mark async dispatch TODO as completed"
```

---

## Summary

| Phase | Tasks | Type | Description |
|-------|-------|------|-------------|
| 1 | 1-2 | Structural | Folder restructure + Makefile |
| 2 | 3-6 | Behavioral | Daemon SSE endpoint + routing |
| 3 | 7-8 | Behavioral | Channel bridge + registration |
| 4 | 9 | Verification | End-to-end testing |

Total: 9 tasks, ~30-40 steps. Each task produces a working, testable commit.
