# Deployment Testing Plan — gobrrr on LXC 10.0.10.20

**Date:** 2026-03-31
**Type:** Manual deployment test
**Target:** LXC container 10.0.10.20 (Debian/Ubuntu, 4 CPU / 8GB)

## Scope

Manual step-by-step deployment of gobrrr daemon + channel bridge with Telegram integration, replacing the existing dotfiles/assistant setup. Commands run by user via SSH; assistant verifies results and debugs issues.

## Phases

### Phase 0 — Reconnaissance
Inspect current state: running services, installed tools, existing configuration.

**Commands:**
- `systemctl list-units --type=service | grep -i claude`
- `which go bun node claude`
- `id claude-agent`
- `ls /home/claude-agent/`
- `cat /etc/os-release`

### Phase 1 — Cleanup existing assistant
- Stop and disable `claude-channels.service` (existing assistant)
- Remove assistant systemd unit
- Clean up assistant workspace files (preserve claude-agent user + Claude auth)

### Phase 2 — Prerequisites
Verify or install:
- Go 1.25+
- Node.js 18+
- Bun
- Claude Code CLI (preserve existing auth if present)
- System packages: git, curl, jq

### Phase 3 — Build & Install gobrrr
- Clone/rsync gobrrr repo to `/home/claude-agent/gobrrr`
- Build daemon: `CGO_ENABLED=0 go build -o /usr/local/bin/gobrrr ./cmd/gobrrr/`
- Install channel deps: `cd channel && bun install`

### Phase 4 — Configure
- Run `gobrrr setup` wizard with Telegram bot token + chat ID
- Verify `~/.gobrrr/config.json` created
- Verify `~/.gobrrr/identity.md` created
- Set up MCP config for channel bridge in `~/.mcp.json`

### Phase 5 — Systemd & Start
- Install `/etc/systemd/system/gobrrr.service`
- `systemctl daemon-reload && systemctl enable --now gobrrr`
- Verify service is running and healthy

### Phase 6 — Smoke tests
1. `gobrrr daemon status` — health check
2. `gobrrr submit --prompt "Say hello" --reply-to stdout` — basic task execution
3. `gobrrr submit --prompt "What time is it?" --reply-to telegram` — Telegram delivery
4. Verify logs: `journalctl -u gobrrr --no-pager -n 50`

## Success Criteria
1. Daemon starts and stays running (no crash loops)
2. CLI can submit tasks and receive results via stdout
3. Task results route to Telegram correctly
4. `journalctl -u gobrrr` shows clean logs

## Out of Scope
- Google OAuth integration
- Warm worker pool
- Automated `scripts/install.sh` validation
- Session rotation / long-running stability
- Channel bridge MCP (tested separately after core works)
