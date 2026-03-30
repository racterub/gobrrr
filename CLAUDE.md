# CLAUDE.md

## What This Project Is

**gobrrr** is a parallel task dispatch daemon for Claude Code. It solves the problem that Claude Code sessions are single-threaded — when running a Telegram conversation, you can only handle one task at a time. gobrrr enables parallel task dispatch, spawning multiple `claude -p` workers simultaneously while your main session stays responsive.

> **Note:** gobrrr originally existed to give `claude -p` workers access to account-level MCPs (Gmail, Calendar). Claude Code has since fixed this — `claude -p` and `claude --print` now have native MCP access. The project's focus has shifted to parallel execution.

### The User's Requirements

- Parallel task execution from a single-threaded Telegram session
- Single Go binary, no cgo, runs on a 4 CPU / 8GB LXC
- Skills/CLI over MCP (MCP wastes context tokens)
- Prompt injection defense (UNTRUSTED boundaries, read-only defaults, confirmation gates)
- Persistent memory across tasks and sessions
- Identity system for consistent assistant personality
- Compatible with existing Telegram assistant (in ~/github/dotfiles/assistant/)
- Uptime Kuma push heartbeat monitoring
- Browser access via agent-browser (Vercel, Rust CLI)
- Setup wizard for one-command installation

### Architecture

```
Telegram Session ──▶ gobrrr daemon ──▶ Claude -p Workers (with skills)
Systemd Timers   ──▶   (Unix socket)
CLI / Skills     ──▶   (Task queue)
```

- **Daemon** listens on `~/.gobrrr/gobrrr.sock` (HTTP/1.1 over Unix socket)
- **Workers** are `claude --print` processes with per-task settings.json permissions
- **Memory** auto-injects relevant memories into worker prompts
- **Identity** (`~/.gobrrr/identity.md`) injected into every worker prompt

## Build

```bash
cd daemon && CGO_ENABLED=0 go build -o gobrrr ./cmd/gobrrr/
```

## Test

```bash
cd daemon && go test ./...
```

## Project Structure

```
daemon/                        Go daemon and CLI
  cmd/gobrrr/main.go          CLI entrypoint (cobra)
  internal/
    config/                    Config loading, defaults, GobrrDir()
    crypto/                    AES-256-GCM vault, master key
    daemon/
      daemon.go                Unix socket HTTP API, route registration
      queue.go                 Task queue with persistence, priority, crash recovery
      worker.go                Worker pool, process spawning, identity/memory injection
      routing.go               Result routing (telegram/stdout/file), output sanitization
      heartbeat.go             Uptime Kuma push heartbeats
      healthcheck.go           Health status evaluation (stuck tasks, failure streaks)
      maintenance.go           Hourly log/queue pruning
      watchdog.go              Systemd sd_notify watchdog
    google/
      auth.go                  Multi-account OAuth2, encrypted storage
      gmail.go                 Gmail API (list, read, send, reply)
      calendar.go              Calendar API (today, week, CRUD)
      boundary.go              UNTRUSTED marker wrapping
      retry.go                 Exponential backoff for Google API errors
    identity/                  Load identity.md, build prompts
    memory/                    Persistent memory store, tag search, relevance matching
    security/
      permissions.go           Per-task settings.json generation
      sanitize.go              Credential leak detection in output
      confirm.go               Approval gate for write actions
    session/                   Telegram channel session supervisor
    scheduler/                 In-process cron scheduler
    telegram/                  Bot API notification, message splitting
    setup/                     Interactive setup wizard
    client/                    HTTP-over-Unix-socket client for CLI
  skills/                      SKILL.md files (gmail, calendar, browser, memory, dispatch, homelab, timer-management)
  systemd/                     gobrrr.service unit
  scripts/                     setup.sh, uninstall.sh
  go.mod                       Go module (github.com/racterub/gobrrr)
  go.sum                       Go dependency checksums
```

## Runtime Data (`~/.gobrrr/`)

```
config.json          Daemon config (concurrency, timeouts, telegram, uptime kuma, session)
schedules.json       Recurring task schedules (atomic writes)
master.key           AES-256 encryption key (0600)
gobrrr.sock          Unix socket (0600)
queue.json           Persistent task queue (atomic writes)
identity.md          Assistant identity (user-editable)
google/              Multi-account OAuth credentials (encrypted)
memory/              Persistent memory entries + index
logs/                Per-task worker output
workers/             Ephemeral per-task settings.json
workspace/           Worker CWD
output/              Safe directory for file: reply-to
```

## Key Design Decisions

1. **Skills over MCP** — Workers call `gobrrr memory` and other CLI commands. No MCP tool schemas consuming context.
2. **Read-only by default** — Write actions require explicit `--allow-writes` at submission. Sensitive actions require Telegram user confirmation.
4. **UNTRUSTED boundaries** — All external content (emails, calendar events, web pages) wrapped in markers telling Claude to treat as data, not instructions.
5. **Server-side write enforcement** — The daemon checks `allow_writes` on the task when it receives a write request, not the client. Prevents env var manipulation bypass.
6. **JSON file persistence** — No SQLite (avoids cgo). Atomic writes via .tmp + rename. Good enough for 10-50 tasks/day single-user.
7. **Identity + Memory injection** — Every worker gets identity.md + up to 10 relevant memories prepended to their prompt.

## Constraints

- Pure Go, no cgo (`CGO_ENABLED=0`)
- Claude Code CLI only — no Anthropic API keys (uses Max plan subscription)
- All JSON persistence uses atomic writes (write `.tmp`, then `os.Rename`)
- File permissions: secrets `0600`, directories `0700`
- Max 2 concurrent workers (configurable, bounded by 4CPU/8GB)
- 5-second minimum spawn interval between workers

## Specs and Plans

- Design spec: `docs/specs/2026-03-23-gobrrr-design.md`
- Implementation plan: `docs/plans/2026-03-24-gobrrr-implementation.md`
- Future work: `TODO.md` (warm worker pool)
