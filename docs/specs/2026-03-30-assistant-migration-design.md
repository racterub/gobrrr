# Assistant Migration Design

**Date:** 2026-03-30
**Status:** Draft
**Goal:** Migrate the Telegram assistant from `~/github/dotfiles/assistant/` into gobrrr so a single repo, single binary, and single `gobrrr setup` handles everything.

## Context

The assistant currently lives in `~/github/dotfiles/assistant/` as a collection of shell scripts managing a Claude Code channel-mode session. It depends on gobrrr for task dispatch (`gobrrr submit`) but runs separately with its own systemd unit, config, and monitoring. This migration brings it fully inside the gobrrr daemon.

## Decisions

| Decision | Choice | Rationale |
|---|---|---|
| Session manager | Go component (`internal/session/`) | Single binary, shared state with daemon, cleaner supervision |
| Timer/scheduler | In-process scheduler (`internal/scheduler/`) | Full visibility into job status, unified with task queue and routing |
| Systemd deployment | Root-level unit, dedicated system user | Better cgroup isolation for daemon + Claude session (~4GB combined) |
| Permissions | `--dangerously-skip-permissions`, no allow/deny lists | Revisited later; simplifies initial migration |
| Session instructions | Folded into `identity.md` | No separate `session-claude.md` needed |
| Idle timeout | Gate on uptime rotation, not a standalone trigger | Prevents rotating mid-conversation |
| Activity tracking | Monitor child process stdout/stderr activity | No file-touch coordination needed |
| Google OAuth setup | Kept in wizard | Removal is a separate TODO |

## 1. Session Manager (`internal/session/`)

Supervises a Claude Code channel-mode process as a child of the daemon.

### Config

Added to `config.json` under `telegram_session`. These fields are additive with safe zero-value defaults -- no config version bump needed. When `telegram_session` is missing or `enabled` is false (bool zero value), the daemon simply does not start a session. Code-level defaults are applied for non-zero fields (same pattern as `MaxWorkers`, `DefaultTimeoutSec`).

```json
{
  "telegram_session": {
    "enabled": true,
    "memory_ceiling_mb": 3072,
    "max_uptime_hours": 6,
    "idle_threshold_min": 5,
    "max_restart_attempts": 6,
    "channels": ["plugin:telegram@claude-plugins-official"]
  }
}
```

### Core struct

```go
type Manager struct {
    cmd        *exec.Cmd
    pty        *os.File     // pseudo-TTY (channel mode requires a TTY)
    startedAt  time.Time
    lastOutput time.Time    // for idle detection
    restarts   int          // consecutive failures for backoff
    config     SessionConfig
    notifier   telegram.Notifier
}
```

### PTY requirement

Claude Code channel mode requires a TTY (the current assistant uses `/bin/script -qec` for this). The session manager allocates a pseudo-TTY using a pure-Go PTY library (e.g., `creack/pty`). Note: `creack/pty` uses syscalls, not cgo -- compatible with `CGO_ENABLED=0`.

### Lifecycle

1. Daemon starts -> `session.Manager.Start()` allocates a PTY and spawns Claude with `--channels ... --dangerously-skip-permissions`
2. Monitor goroutine checks every 60s:
   - Memory > ceiling -> kill immediately, respawn
   - Uptime > max AND idle (no PTY output for `idle_threshold_min`) -> kill, respawn
3. On crash: exponential backoff (30s -> 60s -> 120s -> 300s), reset after 5min successful run
4. After `max_restart_attempts` consecutive failures -> stop, alert via Telegram
5. Daemon shutdown -> SIGTERM child, wait up to 60s, SIGKILL

### Idle detection

Monitor PTY output activity. The daemon reads from the PTY fd -- last read timestamp = last activity. Confirmed: Claude Code channel mode produces stdout output on Telegram interactions (banner, tool calls, responses are all visible in journalctl output).

### Memory check

Read cgroup memory via systemd's `MemoryCurrent` property (the daemon runs under a systemd unit with `MemoryMax=4G`). This counts the entire process tree (daemon + Claude + child processes like Node.js MCP plugins), which is more accurate than `VmRSS` of the main process alone.

### Rotation logic

```
if memory > ceiling:
    rotate immediately
elif uptime > max AND idle:
    rotate
```

### Crash recovery

- Consecutive failure counter, reset after 5min successful run
- Backoff progression: 30s -> 60s -> 120s -> 300s
- After `max_restart_attempts` failures: stop session, send Telegram alert
- Daemon stays alive; session can be restarted via `gobrrr session start` or daemon restart

### CLI

```bash
gobrrr session start     # start the Telegram session (if not running)
gobrrr session stop      # gracefully stop the session
gobrrr session status    # show session PID, uptime, memory, idle time
gobrrr session restart   # stop + start
```

These map to daemon API endpoints: `POST /session/start`, `POST /session/stop`, `GET /session/status`, `POST /session/restart`.

### Channel bridge coordination

The channel bridge (`channel/index.ts`) connects to the daemon's SSE endpoint, not to the Claude session directly. During session rotation, the daemon stays alive and the SSE connection is unaffected. The bridge auto-reconnects on disconnect with exponential backoff. No coordination needed -- the bridge is decoupled from the session lifecycle.

## 2. In-Process Scheduler (`internal/scheduler/`)

Replaces systemd user timers with a daemon-internal cron scheduler.

### Core structs

```go
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

type Scheduler struct {
    schedules []Schedule
    filePath  string
    submitFn  func(prompt, replyTo string, allowWrites bool) error
}
```

### Behavior

- Tick loop every 30s, evaluate cron expressions against current time
- When a schedule fires -> `submitFn` submits a task to the daemon's queue
- On daemon startup: check `last_fired_at` for each schedule. If missed by less than 2x the schedule interval, fire one catch-up run. Otherwise skip (avoids firing 100 missed hourly runs after extended downtime).
- Persistence via atomic write to `~/.gobrrr/schedules.json`
- If `schedules.json` is corrupted or unparseable on startup: log a warning, start with empty schedules (same resilience pattern as `queue.go` crash recovery)

### Wiring

`Daemon.New()` passes `queue.Submit` as the scheduler's `submitFn`. Scheduled tasks enter the regular queue and are indistinguishable from manual submissions.

### CLI

```bash
gobrrr timer create --name "morning-brief" --cron "0 8 * * *" \
  --prompt "Summarize my email and calendar" --reply-to telegram
gobrrr timer list
gobrrr timer remove --name "morning-brief"
```

### Cron parsing

Use `robfig/cron` (or equivalent) for standard 5-field cron expressions. No custom parser.

### Integration

Scheduled tasks enter the same queue as manual submissions. They get task IDs, logs, routing -- indistinguishable from `gobrrr submit`. The daemon has full visibility: next fire time, last result, failure count.

## 3. Skills Migration

| Source | Destination | Notes |
|---|---|---|
| `assistant/skills/homelab/SKILL.md` | `daemon/skills/homelab/SKILL.md` | Content as-is (curl health checks) |
| `assistant/skills/timer-management/SKILL.md` | `daemon/skills/timer-management/SKILL.md` | Rewritten for `gobrrr timer` CLI |

Existing gobrrr skills (gmail, gcal, memory, dispatch, browser) unchanged.

## 4. Systemd & Deployment

### Service unit changes

Current `gobrrr.service` (user-level, 512M) becomes a root-level system service:

- `User=claude-agent` (dedicated system user)
- `MemoryMax=4G` (daemon + Claude session)
- `MemoryHigh=3072M` (pressure warnings)
- `KillMode=control-group` (child dies with daemon)
- `Type=notify` (kept, watchdog still works)

### Setup wizard additions

- Telegram session config: enabled, memory ceiling, max uptime, idle threshold
- System user creation (or detect existing `claude-agent`)
- Install root-level systemd unit to `/etc/systemd/system/`
- `loginctl enable-linger` no longer needed

## 5. What Gets Replaced

| Assistant component | Replaced by |
|---|---|
| `bin/session-wrapper.sh` | `internal/session/` |
| `bin/manage-timer.sh` | `internal/scheduler/` + `gobrrr timer` CLI |
| `lib/run-timer-task.sh` | Scheduler calls `submitFn` directly |
| `lib/send-telegram.sh` | `internal/telegram/notify.go` |
| `lib/check-permission.sh` | Dropped (revisit later) |
| `lib/healthcheck.sh` | `internal/daemon/heartbeat.go` + `healthcheck.go` |
| `config.env` | `~/.gobrrr/config.json` |
| `settings.json` | `--dangerously-skip-permissions` |
| `systemd/claude-channels.service` | Updated `gobrrr.service` |
| `systemd/claude-healthcheck.cron` | Daemon's internal health monitoring |
| `timers/templates/*.tmpl` | In-process scheduler |
| `CLAUDE.md` | Folded into `identity.md` |
| `skills/timer-management/` | Updated in gobrrr `skills/` |
| `skills/homelab/` | Moved to gobrrr `skills/` |

After migration, `~/github/dotfiles/assistant/` is obsolete and can be archived.

## 6. What Does NOT Change

- `internal/daemon/` core (queue, workers, routing, heartbeat, SSE, maintenance)
- `internal/telegram/notify.go` (already handles Bot API messaging)
- `channel/index.ts` (MCP bridge stays as-is, reconnects after session rotation)
- `internal/google/` (kept, removal is a separate TODO)
- `internal/crypto/` (kept)
- Existing skills (gmail, gcal, memory, dispatch, browser)
- `identity.md` system (gains session instructions, but mechanism unchanged)
