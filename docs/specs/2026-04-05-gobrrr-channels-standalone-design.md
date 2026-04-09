# gobrrr-channels: Standalone Channel Session Service

## Problem

gobrrr's built-in Go session manager (`daemon/internal/session/`) uses `creack/pty` to spawn Claude Code with `--channels`. Claude Code does not complete MCP initialization with channel plugins through Go's PTY — the MCP `initialize` handshake never fires, so the Telegram bot never starts polling. The old assistant system at `~/github/dotfiles/assistant/` uses `/bin/script -qec` via a shell wrapper and works reliably.

## Solution

Extract the channel session into a standalone systemd service (`gobrrr-channels.service`) with a shell wrapper script adapted from the proven old assistant. gobrrr daemon keeps task dispatch, workers, heartbeat, memory, and scheduling — but no longer manages the channel session.

## Architecture

```
gobrrr.service            — Go daemon: task queue, workers, heartbeat, scheduling
gobrrr-channels.service   — Shell: script -qec claude --channels, rotation, backoff
```

Two independent systemd services, same user (`claude-agent`), no runtime dependency between them. They share the filesystem (`~/.gobrrr/`, `~/workspace`) but never communicate directly.

## Files

### New

- `scripts/channel-wrapper.sh` — Session lifecycle script (adapted from `dotfiles/assistant/bin/session-wrapper.sh`)
- `daemon/systemd/gobrrr-channels.service` — Systemd unit

### Modified

- `scripts/install.sh` — Install and enable `gobrrr-channels.service`
- `daemon/internal/config/config.go` — Default `telegram_session.enabled` stays `false` (already the default)

### Not modified

- `daemon/internal/session/manager.go` — Revert the `script` wrapper and debug logging changes from this session, keep the Go session manager as an opt-in fallback

## channel-wrapper.sh

Adapted from `dotfiles/assistant/bin/session-wrapper.sh`. Changes:

| Aspect | Old assistant | gobrrr-channels |
|--------|--------------|-----------------|
| Config source | `config.env` (shell vars) | `~/.gobrrr/config.json` via `jq` |
| Claude command | hardcoded | Channels read from config |
| Working directory | `~/workspace` | `~/workspace` |
| Activity marker | `~/workspace/assistant/.last-activity` | `~/.gobrrr/.last-activity` |
| Telegram notifications | `lib/send-telegram.sh` | Direct `curl` using bot token from `~/.claude/channels/telegram/.env` |
| PTY log | `/dev/null` | `~/.gobrrr/logs/session-pty.log` |
| Restart count file | `~/workspace/assistant/.restart-count` | `~/.gobrrr/.restart-count` |

### Config values read from `~/.gobrrr/config.json`

```json
{
  "telegram_session": {
    "enabled": true,
    "memory_ceiling_mb": 3072,
    "max_uptime_hours": 6,
    "idle_threshold_min": 30,
    "max_restart_attempts": 6,
    "channels": ["plugin:telegram@claude-plugins-official"]
  }
}
```

### Session lifecycle (unchanged from old assistant)

1. Touch activity marker
2. Launch Claude via `/bin/script -qec "claude --dangerously-skip-permissions --channels ..." <pty-log>`
3. Background monitor checks every 60s: idle timeout, memory ceiling, max uptime
4. On rotation/crash: exponential backoff (30s → 60s → 120s → 300s), reset after 5min stable
5. After max restart attempts: stop and send Telegram alert

### Telegram notifications

Read bot token from `~/.claude/channels/telegram/.env` and chat ID from `~/.gobrrr/config.json` (decrypted). For the wrapper script, store a plaintext chat ID at `~/.gobrrr/telegram-chat-id` (created during setup) since the shell script can't decrypt the vault.

## gobrrr-channels.service

```ini
[Unit]
Description=gobrrr channel session (Claude Code + Telegram)
After=network-online.target
Wants=network-online.target
StartLimitIntervalSec=0

[Service]
Type=simple
User=claude-agent
Group=claude-agent
WorkingDirectory=/home/claude-agent/workspace
Environment=HOME=/home/claude-agent
Environment=PATH=/home/claude-agent/.local/bin:/home/claude-agent/.bun/bin:/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin
Environment=TERM=xterm-256color
ExecStart=/bin/bash /home/claude-agent/gobrrr/scripts/channel-wrapper.sh
Restart=on-failure
RestartSec=30
MemoryMax=4G
MemoryHigh=3072M
KillMode=control-group
TimeoutStopSec=60
StandardOutput=journal
StandardError=journal
SyslogIdentifier=gobrrr-channels

[Install]
WantedBy=multi-user.target
```

Key differences from `gobrrr.service`:
- `Type=simple` (not `notify` — no watchdog)
- `TERM=xterm-256color` in environment
- `RestartSec=30` (wrapper handles its own backoff internally)
- Own `SyslogIdentifier` for separate log filtering

## Install script changes

Add a new step after the existing gobrrr service installation:

1. Copy `gobrrr-channels.service` from repo to `/etc/systemd/system/`
2. `systemctl daemon-reload && systemctl enable gobrrr-channels`
3. Extract plaintext chat ID to `~/.gobrrr/telegram-chat-id` (for shell script notifications)
4. Start `gobrrr-channels` after `gobrrr`

## What gobrrr daemon keeps

- Task queue + workers (`claude --print` batch jobs)
- Heartbeat (Uptime Kuma)
- Memory store
- Scheduler
- CLI (`gobrrr submit`, `gobrrr memory`, etc.)
- Session manager code (opt-in fallback, disabled by default)

## Testing

1. Deploy `channel-wrapper.sh` and `gobrrr-channels.service` to remote server (10.0.10.20)
2. Stop gobrrr, disable its built-in session
3. Start `gobrrr-channels` service
4. Verify: process tree shows `script → claude → bun (telegram plugin)`
5. Verify: `getUpdates?timeout=5` returns 409 (bot is polling)
6. Verify: send Telegram message, get response
7. Verify: `gobrrr.service` starts independently (task dispatch still works)
