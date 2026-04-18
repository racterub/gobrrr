# Warm Worker Pool Design

## Overview

Extend `WorkerPool` to maintain pre-spawned, persistent Claude processes ("warm workers") alongside the existing cold-spawn model. Warm workers use Claude CLI's `--input-format stream-json --output-format stream-json` mode to keep a process alive across multiple tasks, reducing dispatch latency from ~7-12s to sub-second.

The Telegram session acts as a pure dispatcher — it does no work itself, only routes tasks to gobrrr with an explicit warm/cold decision:

- **Warm workers:** simple, fast tasks (lookups, short answers, memory queries)
- **Cold workers:** complex, multi-step tasks (file editing, refactoring, analysis)

This avoids context pollution in the Telegram session and gives the dispatcher control over latency vs capability trade-offs.

## Architecture

```
Task submitted with --warm
  +-- Warm worker idle? --> pipe task via stdin (~0.5s)
  +-- Warm worker busy? --> fall back to cold spawn (~7-12s)
  +-- Warm worker dead? --> fall back to cold, respawn warm in background

Task submitted without --warm
  +-- Cold spawn as today (~7-12s)
```

Warm workers are pre-spawned when the daemon starts and stay alive until daemon shutdown or crash. On crash, they are immediately respawned. There is no idle timeout — warm workers are always-on.

Context accumulates across tasks within a warm worker. Claude's built-in auto-compaction handles context window pressure. No manual recycling.

## Data & Struct Changes

### Task struct

Add one field:

```go
type Task struct {
    // ... existing fields ...
    Warm bool `json:"warm"`
}
```

### WarmWorker struct

New struct in `internal/daemon/warm_worker.go`:

```go
type WarmWorker struct {
    mu       sync.Mutex
    id       int
    cmd      *exec.Cmd
    stdin    io.WriteCloser
    stdout   *bufio.Scanner   // line-oriented NDJSON reader
    busy     bool             // true while processing a task
    ready    bool             // true after system/init received
    taskID   string           // current task ID (for logging)
    gobrrDir string
    cfg      *config.Config
    memStore *memory.Store
}
```

### WorkerPool changes

```go
type WorkerPool struct {
    // ... existing fields ...
    warmWorkers []*WarmWorker
}
```

### Config changes

```go
type Config struct {
    // ... existing fields ...
    WarmWorkers int `json:"warm_workers"` // default 1
}
```

## NDJSON Protocol

Warm workers communicate via the Claude CLI stream-json protocol over stdin/stdout.

### Messages sent to warm worker (stdin)

User message envelope (one JSON object per line):

```json
{"type":"user","message":{"role":"user","content":"<prompt text>"},"parent_tool_use_id":null}
```

### Messages received from warm worker (stdout)

System init (first message after spawn):

```json
{"type":"system","subtype":"init","session_id":"uuid","model":"...","tools":[...]}
```

Assistant response (may contain tool use):

```json
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"..."}]}}
```

Result (signals task completion, ready for next input):

```json
{"type":"result","subtype":"success","result":"final text output","is_error":false,"duration_ms":1234}
```

Error result:

```json
{"type":"result","subtype":"error_during_execution","is_error":true,"errors":["..."]}
```

The `result` message is the delimiter between tasks. After receiving a `result`, the warm worker is ready for the next user message on stdin.

## Warm Worker Lifecycle

### Spawn sequence

1. `WarmWorker.Start()` executes:
   ```
   claude -p --input-format stream-json --output-format stream-json \
     --dangerously-skip-permissions --verbose
   ```
2. Read stdout until `{"type":"system","subtype":"init",...}` — marks worker as `ready`.
3. Send identity as the first user message:
   ```json
   {"type":"user","message":{"role":"user","content":"<identity.md contents>\nAcknowledge and await tasks."}}
   ```
4. Read until `{"type":"result",...}` — discard the ack response. Worker is now warm and idle.

### Task dispatch sequence

1. `WarmWorker.Run(task)` acquires the mutex, sets `busy=true`.
2. Build per-task prompt: relevant memories + task prompt. Identity is not re-injected (already loaded at spawn).
3. Write one NDJSON line to stdin with the task prompt.
4. Read stdout NDJSON lines until `{"type":"result",...}`.
5. Extract `result.result` as the task output string. If `result.is_error` is true, return an error with the error messages.
6. Set `busy=false`, return the result.

### Crash recovery

- If stdout read returns EOF or `cmd.Wait()` returns unexpectedly, the worker is dead.
- Log the crash, set `ready=false`.
- If a task was in-flight, return an error. The task fails and follows normal retry/routing behavior (may be retried via cold spawn).
- Immediately call `Start()` to respawn in the background.

### Shutdown

On daemon context cancellation, send SIGTERM to all warm worker processes, wait up to 10s, then SIGKILL (same pattern as cold workers).

## WorkerPool Routing

The `WorkerPool.Run()` loop is extended:

```
for each tick:
    task = queue.Next()
    if task == nil:
        continue

    if task.Warm:
        warmWorker = findIdleWarmWorker()
        if warmWorker != nil:
            dispatch to warmWorker in goroutine
        else:
            dispatch to cold (existing runWorker) in goroutine
    else:
        dispatch to cold (existing runWorker) in goroutine
```

Warm workers have their own slots, separate from `maxWorkers`. A warm task that falls back to cold does count against `maxWorkers` concurrency and the spawn rate limit.

## Result Routing

Warm worker results route through the same `onResult` callback as cold workers. The `task.ReplyTo` field controls the destination — warm/cold is orthogonal to routing.

- `reply_to: "telegram"` — Telegram notification (with credential leak scanning)
- `reply_to: "channel"` — SSE to channel listeners
- `reply_to: "stdout"` — stored in task result
- `reply_to: "file:<path>"` — written to file

## Permission Model

Warm workers start with `--dangerously-skip-permissions`, granting full tool access including writes. The dispatcher (Telegram session) decides what goes to warm workers — since these are simple tasks, full permissions are acceptable.

This avoids implementing the `control_request`/`control_response` protocol handler in Go. Cold workers continue to use per-task `--settings` for permission sandboxing (with the known merge limitation documented in TODO.md).

## CLI & API Changes

### `gobrrr submit --warm`

```bash
gobrrr submit "what time is it in Tokyo?" --warm
gobrrr submit "refactor the auth module"  # cold by default
```

### HTTP API

`POST /tasks` request body gains `warm` field:

```json
{
  "prompt": "what time is it in Tokyo?",
  "reply_to": "telegram",
  "warm": true
}
```

### Health endpoint

`GET /health` includes warm worker status:

```json
{
  "status": "ok",
  "warm_workers": {
    "total": 1,
    "ready": 1,
    "busy": 0
  }
}
```

## Config

New fields in `config.json` with defaults:

```json
{
  "warm_workers": 1
}
```

On the 4CPU/8GB LXC, each warm Claude session uses ~200-400MB. Default of 1 warm worker is conservative. Configurable up to `max_workers` value.

## Testing Strategy

1. **Unit: NDJSON parsing** — feed mock NDJSON lines (system/init, assistant, result) into a `bufio.Scanner`, verify correct state transitions and result extraction.
2. **Unit: WorkerPool routing** — verify `task.Warm=true` dispatches to warm worker, `task.Warm=false` goes cold, warm-busy falls back to cold.
3. **Integration: real Claude process** — spawn a real `claude -p --input-format stream-json ...`, send a simple prompt, verify result. Skip in CI (requires auth).
4. **Unit: crash recovery** — simulate EOF on stdout, verify respawn and in-flight task error.

## Files Changed

| File | Change |
|---|---|
| `internal/daemon/queue.go` | Add `Warm` field to `Task` struct |
| `internal/daemon/warm_worker.go` | New — `WarmWorker` struct, `Start()`, `Run()`, NDJSON parsing |
| `internal/daemon/worker.go` | Extend `WorkerPool` with `warmWorkers`, modify `Run()` for routing |
| `internal/daemon/daemon.go` | Pass `WarmWorkers` config to pool, add warm status to health endpoint |
| `internal/config/config.go` | Add `WarmWorkers` field with default 1 |
| `cmd/gobrrr/main.go` | Add `--warm` flag to `submit` command |
| `internal/daemon/warm_worker_test.go` | New — NDJSON parsing and lifecycle tests |
| `internal/daemon/worker_test.go` | Routing tests for warm/cold dispatch |

## Constraints

- Pure Go, no cgo (`CGO_ENABLED=0`)
- Claude Max subscription auth (no API keys)
- Each warm worker ~200-400MB — max 1-2 on 4CPU/8GB LXC
- `--input-format stream-json` protocol is undocumented by Anthropic; message formats based on Agent SDK types and community reverse-engineering
