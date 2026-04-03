# Install Script Design Spec

## Overview

A single idempotent bash script (`install.sh`) that performs a complete gobrrr installation on a fresh Debian/Ubuntu amd64 machine. Self-elevates to root. Creates a dedicated `claude-agent` system user. Handles both fresh installs and upgrades.

## Target Environment

- **OS**: Debian or Ubuntu
- **Arch**: amd64 / x86_64
- **Privileges**: Self-elevates via `sudo` if not already root

## Directory Layout

| Path | Purpose |
|------|---------|
| `/usr/local/bin/gobrrr` | Built binary |
| `/home/claude-agent/gobrrr` | Repo clone (source + channel bridge) |
| `/home/claude-agent/workspace` | Claude Code working directory |
| `/home/claude-agent/.gobrrr/` | Runtime data (config, keys, memory, logs) |
| `/home/claude-agent/.mcp.json` | Claude Code MCP server config |
| `/etc/systemd/system/gobrrr.service` | Systemd unit |

## Installation Steps

### 1. Self-Elevate

If not root, re-exec with `sudo "$0" "$@"`.

### 2. Validate Environment

- Check `/etc/os-release` for Debian or Ubuntu `ID` / `ID_LIKE`
- Check `uname -m` is `x86_64`
- Abort with clear message if either fails

### 3. Install System Packages

```bash
apt-get update
apt-get install -y git curl jq
```

Skip if all are already present. Note: `build-essential` is not needed — the Go build uses `CGO_ENABLED=0` (no C compiler required), and Node/Bun are precompiled.

### 4. Install Go

- Check if `go version` exists and meets minimum version (1.25+, per `go.mod`)
- If missing or outdated:
  - Fetch latest stable version from `https://go.dev/VERSION?m=text`
  - Download tarball from `https://dl.google.com/go/{version}.linux-amd64.tar.gz`
  - Remove old `/usr/local/go` if exists
  - Extract to `/usr/local/go`
  - Symlink `/usr/local/go/bin/go` to `/usr/local/bin/go`

### 5. Install Node.js

- Check if `node --version` exists and is 18+
- If missing or outdated:
  - Use NodeSource setup script for LTS (currently 22.x)
  - `apt-get install -y nodejs`

### 6. Install Bun

- Check if `bun --version` exists
- If missing:
  - `npm install -g bun` (predictable system-wide install since Node.js is already present, avoids curl-to-bash-as-root installing to `~/.bun`)

### 7. Install Claude Code

- Check if `claude --version` exists
- If missing:
  - Run as `claude-agent` (not root) to install to the correct user context:
    `sudo -u claude-agent -i bash -c 'curl -fsSL https://claude.ai/install.sh | bash'`
  - Symlink the binary to `/usr/local/bin/claude` if not already on system PATH

### 8. Install agent-browser + Chrome for Testing

- Check if `agent-browser` command exists
- If missing:
  - `npm install -g @anthropic-ai/agent-browser`
  - `agent-browser install --with-deps` (installs Chrome for Testing + dependencies)

### 9. Create claude-agent User

- Check if user exists (`id claude-agent`)
- If not:
  - `useradd --create-home --shell /bin/bash claude-agent`
  - Note: do NOT use `--system` — system users may not get a usable home directory on all distros. A regular user with no login password is sufficient.
- Verify `/home/claude-agent` was created
- Ensure directories exist:
  - `/home/claude-agent/workspace` (owned by claude-agent)

### 10. Clone or Update Repo

- **Fresh**: `git clone https://github.com/racterub/gobrrr.git /home/claude-agent/gobrrr`
- **Upgrade**: `git -C /home/claude-agent/gobrrr fetch origin && git -C /home/claude-agent/gobrrr reset --hard origin/main` (safe — this is a build-only clone, not a working tree)
- `chown -R claude-agent:claude-agent /home/claude-agent/gobrrr`

### 11. Build Binary

```bash
cd /home/claude-agent/gobrrr/daemon
CGO_ENABLED=0 go build -o /usr/local/bin/gobrrr ./cmd/gobrrr/
```

### 12. Install Channel Bridge Dependencies

```bash
cd /home/claude-agent/gobrrr/channel
sudo -u claude-agent bun install
```

### 13. Configure Channel MCP

Write or merge the gobrrr channel entry into `/home/claude-agent/.mcp.json`:

```json
{
  "mcpServers": {
    "gobrrr": {
      "type": "stdio",
      "command": "bun",
      "args": ["run", "/home/claude-agent/gobrrr/channel/index.ts"]
    }
  }
}
```

If the file exists, merge `mcpServers.gobrrr` into the existing config using `jq`. If it doesn't exist, create it. Preserve existing MCP entries.

### 14. Install Systemd Unit

Write `gobrrr.service` to `/etc/systemd/system/gobrrr.service`:

```ini
[Unit]
Description=gobrrr task dispatch daemon
After=network-online.target
Wants=network-online.target
StartLimitIntervalSec=0

[Service]
Type=notify
User=claude-agent
WorkingDirectory=/home/claude-agent/workspace
Environment=HOME=/home/claude-agent
ExecStart=/usr/local/bin/gobrrr daemon start
Restart=on-failure
RestartSec=5
WatchdogSec=60
MemoryMax=4G
MemoryHigh=3072M
KillMode=control-group
TimeoutStopSec=90
StandardOutput=journal
StandardError=journal
SyslogIdentifier=gobrrr

[Install]
WantedBy=multi-user.target
```

Then:
```bash
systemctl daemon-reload
systemctl enable gobrrr
```

### 15. Authenticate Claude Code

```bash
sudo -u claude-agent -i claude setup-token
```

Uses `claude setup-token` for headless server authentication. This prompts the user to paste a long-lived auth token (generated from their Claude account on a machine with a browser). This avoids the browser-based OAuth flow that `claude login` requires.

### 16. Run gobrrr Setup

```bash
sudo -u claude-agent -i gobrrr setup
```

Uses `-i` to ensure a proper login shell with TTY. This is the existing interactive wizard — configures master key, Telegram bot, Uptime Kuma, session management, identity, and Google accounts.

### 17. Start Service

```bash
systemctl start gobrrr
```

If service was already running (upgrade), restart instead:
```bash
systemctl restart gobrrr
```

### 18. Verify

```bash
systemctl is-active gobrrr
gobrrr --version  # sanity check binary
sudo -u claude-agent -i gobrrr daemon status
```

Print success message with status output.

## Idempotency

Every step checks current state before acting:

| Step | Check | Action if present |
|------|-------|-------------------|
| System packages | `dpkg -s <pkg>` | Skip |
| Go | `go version` >= 1.25 | Skip |
| Node.js | `node --version` >= 18 | Skip |
| Bun | `bun --version` | Skip |
| Claude Code | `claude --version` | Skip |
| agent-browser | `command -v agent-browser` | Skip |
| claude-agent user | `id claude-agent` | Skip creation |
| Repo | Directory exists | `fetch + reset --hard origin/main` |
| Binary | Always rebuilt | Overwrite |
| Channel deps | Always run | `bun install` is idempotent |
| MCP config | File exists | Merge, don't overwrite |
| Systemd unit | Always written | Overwrite + daemon-reload |
| gobrrr setup | Always run | Wizard handles existing config |
| Service | `is-active` check | Restart if running, start if not |

## Error Handling

- `set -euo pipefail` at the top
- Trap on ERR that prints which step failed
- Each major step prints `[N/18] Description...` status line
- On failure, print what went wrong and suggest manual fix

## Script Location

`scripts/install.sh` in the repo root (not inside `daemon/`), replacing the existing `daemon/scripts/setup.sh`.

The old `daemon/scripts/setup.sh` and `daemon/scripts/uninstall.sh` will be kept but are considered legacy.

## Codebase Changes Required

The install script also requires updating existing code to be consistent:

1. **`daemon/systemd/gobrrr.service`** — replace entirely with the canonical unit from this spec (ExecStart path, User, WorkingDirectory, HOME env, MemoryMax/MemoryHigh, TimeoutStopSec, StartLimitIntervalSec, WantedBy)
2. **`daemon/internal/setup/wizard.go`** — replace the entire embedded `defaultServiceUnit` constant with the canonical unit from this spec, or skip systemd installation in the wizard when a system-level unit already exists at `/etc/systemd/system/gobrrr.service`

## Future Considerations

- ARM64 support (when needed)
- Non-interactive mode (`--yes` flag to skip wizard prompts)
- Uninstall counterpart at repo root level
