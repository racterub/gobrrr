# Async Dispatch with Result Context — Design Spec

## Problem

The Telegram channel session (main brain) and gobrrr workers have a context gap:

- `gobrrr submit --reply-to stdout` — session gets result in context but **blocks** while waiting. Can't handle other Telegram messages. Makes the queue pointless.
- `gobrrr submit --reply-to telegram` — session stays responsive but **loses context** of what the worker said. If user follows up, the session doesn't know the worker's answer.

We need non-blocking dispatch where the session stays responsive AND gets worker results back into its conversation context.

## Solution

Use Claude Code's **channel** mechanism to push worker results directly into the session. Two components:

1. **Daemon** gains an SSE streaming endpoint for completed task results
2. **Channel bridge** — a thin Bun MCP server that subscribes to the daemon's stream and pushes results as `<channel source="gobrrr">` events into the Claude Code session

## Architecture

```
Claude Code session
  ├── gobrrr CLI (submit tasks)          → gobrrr.sock → daemon
  └── gobrrr channel (MCP stdio bridge)  ← gobrrr.sock ← daemon (push results)
```

- Session dispatches tasks via `gobrrr submit` CLI (skills over MCP)
- Daemon queues and runs workers as before
- On worker completion, daemon emits result on SSE stream
- Channel bridge receives the event and pushes it into the session as a channel notification
- Session receives the result in-context and decides what to do (relay to user, act on it, absorb silently)

## Daemon Changes

### New endpoint: `GET /tasks/results/stream`

SSE stream over Unix socket. Emits a JSON event for each task that completes.

Event payload:

```json
{
  "task_id": "abc123",
  "status": "completed",
  "prompt_summary": "check gmail for unread",
  "result": "You have 3 unread emails...",
  "error": "",
  "submitted_at": "2026-03-25T01:30:00Z"
}
```

- `task_id` — unique task identifier
- `status` — `completed`, `failed`, or `timeout`
- `prompt_summary` — first 100 **runes** (not bytes) of the original prompt, to avoid splitting multi-byte characters
- `result` — full worker output
- `error` — error message when `status` is `failed` or `timeout`, empty string otherwise
- `submitted_at` — original submission timestamp (ISO 8601)

### Schema change: `Delivered` field on Task

The `Task` struct in `queue.go` gains a new field:

```go
Delivered bool `json:"delivered"`
```

This tracks whether a result has been emitted on the SSE stream. Prevents duplicate delivery on reconnection.

### SSE fan-out implementation

The SSE endpoint maintains a set of connected clients, each with a bounded channel buffer:

- Each client gets a buffered Go channel (capacity 64 events)
- On task completion, the daemon sends to all client channels non-blocking (`select` with `default` to drop if full)
- A slow or dead client that falls behind loses events rather than blocking the daemon's worker completion path
- Stale connections are detected via write errors and cleaned up

Behavior:
- After emitting an event to at least one client, marks the task as `delivered: true` in queue.json
- Multiple clients can connect; each gets events from the time of connection
- If no channel is connected, existing `reply-to` routing (telegram/stdout/file) still works — channel delivery is additive
- **Reconnection gap**: events emitted while the bridge is disconnected are lost to the session. This is acceptable for v1 — use `--reply-to telegram,channel` for critical tasks so Telegram acts as a fallback delivery path. A future `?since=<task_id>` replay parameter may address this

### New reply-to option: `channel`

`gobrrr submit --reply-to channel` tells the daemon to deliver the result only via the SSE stream.

- A task can combine: `--reply-to telegram,channel` to send to both
- Default behavior for tasks without `--reply-to channel` is unchanged

### Multi-destination routing refactor

The current `routeResult` in `routing.go` switches on exact string match for `ReplyTo`. This must be refactored to support comma-separated destinations:

1. Split `task.ReplyTo` on `,` into a list of destinations
2. Iterate and route to each destination independently
3. Collect errors per-destination (partial success is OK — e.g., Telegram delivery fails but channel delivery succeeds)
4. New `"channel"` case emits to the SSE fan-out

Existing single-destination behavior is unchanged — `"telegram"` still works as before.

### No other daemon changes

Queue, workers, Google APIs, memory injection, identity — all untouched.

## Channel Bridge

A one-way (push-only) MCP channel server. No tools exposed — the session dispatches via CLI.

### Directory structure

```
channel/
  index.ts          # MCP channel server
  package.json      # @modelcontextprotocol/sdk dependency
```

### Behavior

1. Creates MCP server with `claude/channel` capability
2. Connects to Claude Code via stdio transport
3. Opens HTTP connection to `~/.gobrrr/gobrrr.sock` on `GET /tasks/results/stream`
4. For each SSE event, pushes a channel notification
5. On disconnect/error, reconnects to daemon with exponential backoff

### Channel event format

```xml
<channel source="gobrrr" task_id="abc123" status="completed" prompt_summary="check gmail for unread">
Worker result content here...
</channel>
```

### Instructions string

Injected into session system prompt via MCP server instructions field:

```
Task results from gobrrr workers arrive as <channel source="gobrrr" task_id="..." status="..." prompt_summary="...">.
These are results from tasks you previously dispatched via `gobrrr submit`.
Read the result and decide whether to act on it, relay it to the user, or absorb it silently.
```

### Registration

Added to session's MCP config:

```json
{
  "mcpServers": {
    "gobrrr": { "command": "bun", "args": ["/home/racterub/github/gobrrr/channel/index.ts"] }
  }
}
```

Session starts with `--dangerously-load-development-channels server:gobrrr` during channel research preview. Will migrate to `--channels` once custom channels are on the allowlist.

## Prerequisite: Folder Restructure

The folder restructure is a **separate structural change** that must be completed before implementing async dispatch. It is its own step with its own scope — no behavioral changes mixed in.

Note: since `go.mod` moves alongside the code into `daemon/`, internal import paths (e.g., `github.com/racterub/gobrrr/internal/...`) remain unchanged — they resolve relative to where `go.mod` lives. However, the build command in CLAUDE.md, the systemd unit's `ExecStart`, and `scripts/setup.sh` must all be updated atomically in the restructure commit.

## Folder Restructure

### Current

```
cmd/gobrrr/main.go
internal/
  config/, crypto/, daemon/, google/, identity/, memory/, security/, setup/, telegram/, client/
skills/
systemd/
scripts/
go.mod, go.sum
```

### Proposed

```
daemon/
  cmd/gobrrr/main.go
  internal/
    config/, crypto/, daemon/, google/, identity/, memory/, security/, setup/, telegram/, client/
  go.mod, go.sum
  skills/
  systemd/
  scripts/
channel/
  index.ts
  package.json
docs/
Makefile
TODO.md
CLAUDE.md
```

- Go code moves under `daemon/`
- Channel lives in `channel/` as a separate Bun project
- `docs/`, `TODO.md`, `CLAUDE.md`, `Makefile` stay at root (project-wide)
- `skills/` moves under `daemon/` since workers consume them

## Makefile

Root-level Makefile that ties both components together:

```makefile
# Build
make build           # builds both daemon and channel
make build-daemon    # cd daemon && CGO_ENABLED=0 go build -o gobrrr ./cmd/gobrrr/
make build-channel   # cd channel && bun install

# Dev
make dev             # starts daemon in foreground
make dev-channel     # starts channel bridge in dev mode
make dev-channel     # starts channel in dev mode

# Test
make test            # runs both
make test-daemon     # cd daemon && go test ./...
make test-channel    # cd channel && bun test

# Install
make install         # builds + copies binary + installs systemd unit + registers channel
make clean           # removes build artifacts
```

`make install` performs:
1. Build daemon binary to `~/.local/bin/gobrrr`
2. Run `bun install` in `channel/`
3. Register channel in `.mcp.json` if not already present
4. Copy systemd unit, daemon-reload, enable

## Design Decisions

1. **Channel over polling** — Claude Code's channel mechanism pushes results into the session natively. No polling commands, no file watching, no context wasted on collect cycles.
2. **Push-only channel (no MCP tools)** — Skills/CLI over MCP. The session dispatches via `gobrrr submit` CLI. Channel only delivers results. This matches the project's context-efficiency principle.
3. **Thin TS bridge over Go MCP** — The MCP SDK is TypeScript. Channels are a research preview that may change. A ~80 line Bun adapter is disposable and easy to update. gobrrr stays pure Go.
4. **SSE over WebSocket** — SSE is simpler, works over HTTP/1.1 on Unix socket, one-directional (which is all we need). No upgrade handshake complexity.
5. **Additive delivery** — Channel delivery doesn't replace existing reply-to options. A task can route to both telegram and channel. Backwards compatible.

## Investigated Alternatives

- **Approach A: Telegram plugin sees bot's own messages** — Investigated and confirmed dead. Telegram Bot API does not deliver the bot's own outgoing messages as updates. Verified by sending a curl to the Telegram webhook — the session received no event.
- **Approach B: File-based result injection** — Rejected. Requires polling and the session needs to track task IDs manually.
- **Approach D: Non-blocking submit with deferred `collect`** — Viable but inferior. Requires periodic polling via CLI. Channel push is the native Claude Code solution.

## Security

- The SSE endpoint is only accessible via Unix socket (`~/.gobrrr/gobrrr.sock`, permission `0600`). This is the security boundary.
- Channel event content is sanitized using existing `sanitize.go` before streaming, preventing credential leaks.
- `prompt_summary` exposes what the user asked — acceptable since the socket is user-local.

## Constraints

- Channel requires Claude Code v2.1.80+ with channel support
- Custom channels require `--dangerously-load-development-channels` during research preview
- Channel bridge requires Bun runtime
- SSE reconnection must handle daemon restarts gracefully
- Channel events should not include sensitive data (credentials, tokens) — daemon sanitizes output before streaming, using existing sanitize.go
