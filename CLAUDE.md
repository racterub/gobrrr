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
      worker.go                Worker pool, process spawning, identity/memory/skills injection
      routing.go               Result routing (telegram/stdout/file), output sanitization
      heartbeat.go             Uptime Kuma push heartbeats
      healthcheck.go           Health status evaluation (stuck tasks, failure streaks)
      maintenance.go           Hourly log/queue/approval pruning
      skill_routes.go          HTTP handlers for skill search/install/uninstall
      watchdog.go              Systemd sd_notify watchdog
    skills/                    Skill loader, registry, prompt builder, embedded system skills
      system/                  Bundled SKILL.md files embedded via //go:embed
    clawhub/                   ClawHub V1 REST client, stager, committer
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
skills/              Installed skills (system + ClawHub) — <slug>/SKILL.md + <slug>/_meta.json
skills/_requests/    Staged skill bundle dirs (approval metadata lives in _approvals/, TTL 24h)
skills/_lock.json    ClawHub install manifest (slug → version/SHA256)
_approvals/          Pending user-approval requests (kind-tagged, TTL 24h)
logs/                Per-task worker output
workers/             Ephemeral per-task settings.json
workspace/           Worker CWD
output/              Safe directory for file: reply-to
```

## Skills

Workers see every installed skill via an `<available_skills>` block prepended to the prompt (built by `internal/skills.BuildPromptBlock`). Claude reads the referenced SKILL.md on demand; the block itself contains only name/description/path.

Install flow (unified approval system):

1. `gobrrr skill install <slug>` → daemon fetches the ZIP from ClawHub, verifies SHA256, stages under `skills/_requests/<id>_staging/`, and creates a persistent approval at `~/.gobrrr/_approvals/<id>.json` with `kind: skill_install`.
2. Any subscriber to `GET /approvals/stream` receives a `created` event; the gobrrr-telegram bot renders a Telegram inline-keyboard card with Approve / Skip-binary / Deny buttons.
3. `gobrrr skill approve <id>` (or the Telegram button) posts `{"decision":"approve"}` to `POST /approvals/{id}`. The dispatcher claims the approval atomically, fires the `skill_install` handler, which runs approved binary commands and commits the staged skill to `skills/<slug>/`.
4. `gobrrr skill deny <id>` sends `{"decision":"deny"}`; the handler removes the staging artifacts.
5. Expired approvals (TTL 24h) are pruned hourly with a synthesized `deny` decision — same cleanup path as user-initiated deny.
6. System skills (`type: system`, embedded in the binary) are copied to `skills/<slug>/` on daemon start and never overwritten.

Permission merge: worker `settings.json` gains each installed skill's `approved_read_permissions` unconditionally and `approved_write_permissions` only when the task was submitted with `--allow-writes`.

## Approvals

Pending user-approval requests live under `~/.gobrrr/_approvals/<id>.json`. The shape is generic and kind-tagged (`kind: skill_install` today, with `kind: write_action` reserved for the future write-action migration tracked in TODO.md).

- `POST /approvals/{id}` with JSON `{"decision": "<action>"}` — action is one of the kind's advertised actions.
- `GET /approvals/stream` — SSE of `ApprovalEvent { type: "created"|"removed", request|id|decision }`. Rehydrates all pending approvals on connect so a restarted subscriber (e.g. the Telegram bot) catches up.
- Decisions are atomic: the store file is deleted before the per-kind handler runs.

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

- Design spec: `docs/superpowers/specs/2026-03-23-gobrrr-design.md`
- Implementation plan: `docs/superpowers/plans/2026-03-24-gobrrr-implementation.md`
- All specs and plans: `docs/superpowers/{specs,plans}/`
- Future work: `TODO.md` (warm worker pool)
