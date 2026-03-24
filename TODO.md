# TODO

## Warm Worker Pool

Currently each task spawns a cold `claude --print` process (~7-12s startup). A warm worker pool would keep Claude sessions alive and route tasks to them, reducing dispatch latency to sub-second for simple tasks.

### Design

Maintain N idle Claude processes (default 1) running in `--input-format stream-json --output-format stream-json` mode. Tasks are piped in via stdin, results read from stdout. Sessions stay alive between tasks.

### Key decisions needed

- **Session mode**: `claude` supports `--input-format stream-json` for persistent stdin/stdout sessions. Verify this works for sequential task execution without state leaking between tasks.
- **Identity reset**: Each task needs fresh identity + memory injection. Can we send a system prompt per-message in stream mode, or do we need to reset the session?
- **Concurrency**: Warm workers handle one task at a time. For concurrent tasks, keep both warm (fast) and cold (overflow) pools.
- **Idle timeout**: Kill idle warm workers after N minutes to free memory. Respawn on next task.
- **Error recovery**: If a warm worker crashes or hangs, fall back to cold spawn.

### Architecture sketch

```
Task submitted
  ├─ Warm worker available? → pipe task to warm worker (~0.5s)
  └─ No warm worker? → cold spawn claude --print (~7-12s)
```

### Implementation steps

1. Research `claude --input-format stream-json` protocol — message format, session state, error handling
2. Add `WarmWorker` struct managing a persistent `exec.Cmd` with stdin/stdout pipes
3. Add `WarmPool` with configurable pool size, idle timeout, health checks
4. Route `WorkerPool.Run()` to prefer warm workers, fall back to cold
5. Add config: `warm_workers: 1`, `warm_idle_timeout_min: 30`
6. Test with sequential tasks, verify no state leakage between tasks

### Constraints

- Must not leak context between tasks (security)
- Must handle worker crashes gracefully
- Memory budget: each warm Claude session uses ~200-400MB
- On 4CPU/8GB LXC, max 1-2 warm workers realistically

## Migrate Assistant into gobrrr

The assistant currently lives in `~/github/dotfiles/assistant/` as a separate system. It should be migrated into this repo so gobrrr is fully self-contained.

### What to migrate

- `session-wrapper.sh` — Claude Telegram channel session lifecycle, rotation, crash recovery
- `manage-timer.sh` — Systemd timer CRUD for scheduled tasks
- `run-timer-task.sh` — Timer task execution (already calls gobrrr)
- `send-telegram.sh` — Telegram Bot API helper
- `check-permission.sh` — Permission self-check helper
- `healthcheck.sh` — Health monitoring with Uptime Kuma (partially replaced by gobrrr heartbeat)
- `config.env` — Secrets and thresholds
- `settings.json` — Claude Code permissions for the main session
- `systemd/claude-channels.service` — Main session systemd unit
- `skills/` — Timer management, homelab skills
- `CLAUDE.md` — Assistant runtime instructions

### Goal

Single repo, single `gobrrr setup` installs everything: the daemon, the Telegram session wrapper, timers, skills, and systemd units. No cross-repo symlinks.

## Async Dispatch with Result Context

### Problem

The Telegram channel session (main brain) and gobrrr workers have a context gap:

- `gobrrr submit --reply-to stdout` — session gets result in context but **blocks** while waiting. Can't handle other Telegram messages. Makes the queue pointless.
- `gobrrr submit --reply-to telegram` — session stays responsive but **loses context** of what the worker said. If user follows up, the session doesn't know the worker's answer.

### What we want

Non-blocking dispatch where the session stays responsive AND gets worker results back into its conversation context.

### Possible approaches

**A. Telegram channel MCP sees bot's own messages**
- If the Telegram channel plugin receives the bot's own outgoing messages, then `--reply-to telegram` would naturally inject results into the session's context.
- Need to verify: does `plugin:telegram` show the bot's own messages sent via Bot API?
- If yes, this is the simplest solution — zero code changes needed.

**B. File-based result injection**
- Worker writes result to a known file (e.g., `~/.gobrrr/results/<task-id>.txt`)
- Session polls or watches for result files, reads them back into context
- `gobrrr submit --reply-to file:~/.gobrrr/results/<task-id>.txt` + session reads file after

**C. Daemon push via a local channel**
- gobrrr daemon could expose a streaming endpoint (`GET /tasks/stream`)
- Session opens a background connection, receives results as they complete
- Complex but keeps the session fully informed

**D. Non-blocking submit with deferred read**
- `gobrrr submit --prompt "..." --reply-to deferred` returns task ID immediately
- Session continues handling messages
- Periodically or on idle: `gobrrr collect` reads all completed task results
- Session processes results in batch

### Key question to resolve

Does `plugin:telegram` see the bot's own messages? If yes, approach A wins. If no, approach D (deferred collect) is probably the simplest.
