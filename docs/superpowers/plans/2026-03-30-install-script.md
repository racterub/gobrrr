# Install Script Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Create a single idempotent `install.sh` that fully provisions gobrrr on a fresh Debian/Ubuntu amd64 machine.

**Architecture:** One bash script with 18 sequential steps, each guarded by idempotency checks. Two structural changes to existing code (systemd unit + wizard) ensure path consistency.

**Tech Stack:** Bash, systemd, Go build toolchain, Node.js, Bun, jq

**Spec:** `docs/specs/2026-03-30-install-script-design.md`

---

## File Map

| Action | Path | Responsibility |
|--------|------|----------------|
| Create | `scripts/install.sh` | Main install script (all 18 steps) |
| Modify | `daemon/systemd/gobrrr.service` | Update to canonical unit (paths, limits, env) |
| Modify | `daemon/internal/setup/wizard.go:296-316` | Update embedded `defaultServiceUnit` constant |

---

### Task 1: Update systemd unit file (behavioral — changes paths, memory limits, targets)

**Files:**
- Modify: `daemon/systemd/gobrrr.service`

- [ ] **Step 1: Replace the systemd unit with the canonical version**

Replace the entire contents of `daemon/systemd/gobrrr.service` with:

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

- [ ] **Step 2: Commit**

```bash
git add daemon/systemd/gobrrr.service
git commit -m "fix: update systemd unit to canonical paths, memory limits, and system-level target"
```

---

### Task 2: Update wizard's embedded systemd unit (behavioral — matches Task 1)

**Files:**
- Modify: `daemon/internal/setup/wizard.go:296-316`

- [ ] **Step 1: Replace the `defaultServiceUnit` constant**

In `daemon/internal/setup/wizard.go`, replace the `defaultServiceUnit` constant (lines 296-316) with the same canonical unit from Task 1. The constant should match `daemon/systemd/gobrrr.service` exactly.

- [ ] **Step 2: Verify build**

```bash
cd daemon && CGO_ENABLED=0 go build ./cmd/gobrrr/
```

Expected: Build succeeds with no errors.

- [ ] **Step 3: Run tests**

```bash
cd daemon && go test ./...
```

Expected: All tests pass.

- [ ] **Step 4: Commit**

```bash
git add daemon/internal/setup/wizard.go
git commit -m "fix: update wizard embedded systemd unit to match canonical system-level unit"
```

---

### Task 3: Write the install script — scaffolding and validation (steps 1-3)

**Files:**
- Create: `scripts/install.sh`

- [ ] **Step 1: Create the script with header, self-elevation, and environment validation**

Create the `scripts/` directory at repo root (`mkdir -p scripts/`), then create `scripts/install.sh`:

```bash
#!/usr/bin/env bash
set -euo pipefail

# === gobrrr Installer ===
# Targets: Debian/Ubuntu amd64
# Requires: root (self-elevates)
# Idempotent: safe to re-run for upgrades

TOTAL_STEPS=18
CURRENT_STEP=0

step() {
    CURRENT_STEP=$((CURRENT_STEP + 1))
    echo ""
    echo "[$CURRENT_STEP/$TOTAL_STEPS] $1"
    echo "-------------------------------------------"
}

fail() {
    echo "ERROR: $1" >&2
    exit 1
}

# --- Step 1: Self-elevate ---
if [ "$(id -u)" -ne 0 ]; then
    echo "Not root, elevating with sudo..."
    exec sudo "$0" "$@"
fi

step "Validating environment"

# --- Step 2: Validate environment ---
if [ "$(uname -m)" != "x86_64" ]; then
    fail "This installer only supports x86_64 (amd64). Detected: $(uname -m)"
fi

if [ ! -f /etc/os-release ]; then
    fail "Cannot detect OS — /etc/os-release not found"
fi

. /etc/os-release
case "${ID:-}" in
    debian|ubuntu) ;;
    *)
        case "${ID_LIKE:-}" in
            *debian*|*ubuntu*) ;;
            *) fail "This installer targets Debian/Ubuntu. Detected: ${ID:-unknown} (ID_LIKE=${ID_LIKE:-none})" ;;
        esac
        ;;
esac

echo "OK: $(uname -m), ${PRETTY_NAME:-$ID}"

# --- Step 3: Install system packages ---
step "Installing system packages"

PACKAGES=(git curl jq)
MISSING=()
for pkg in "${PACKAGES[@]}"; do
    if ! dpkg -s "$pkg" &>/dev/null; then
        MISSING+=("$pkg")
    fi
done

if [ ${#MISSING[@]} -gt 0 ]; then
    apt-get update -qq
    apt-get install -y "${MISSING[@]}"
else
    echo "All packages already installed"
fi
```

- [ ] **Step 2: Make executable**

```bash
chmod +x scripts/install.sh
```

- [ ] **Step 3: Test validation locally (read-only)**

```bash
# Verify syntax
bash -n scripts/install.sh
```

Expected: No output (syntax OK).

- [ ] **Step 4: Commit**

```bash
git add scripts/install.sh
git commit -m "feat: add install script scaffolding with self-elevation and validation"
```

---

### Task 4: Add Go installation (step 4)

**Files:**
- Modify: `scripts/install.sh`

- [ ] **Step 1: Append Go installation logic**

Append to `scripts/install.sh` before the end:

```bash
# --- Step 4: Install Go ---
step "Installing Go"

GO_MIN_MAJOR=1
GO_MIN_MINOR=25

need_go_install() {
    if ! command -v go &>/dev/null; then
        return 0
    fi
    local ver
    ver=$(go version | sed -n 's/.*go\([0-9]*\.[0-9]*\).*/\1/p')
    local major minor
    major=$(echo "$ver" | cut -d. -f1)
    minor=$(echo "$ver" | cut -d. -f2)
    if [ "$major" -lt "$GO_MIN_MAJOR" ] || { [ "$major" -eq "$GO_MIN_MAJOR" ] && [ "$minor" -lt "$GO_MIN_MINOR" ]; }; then
        return 0
    fi
    return 1
}

if need_go_install; then
    echo "Fetching latest Go version..."
    GO_VERSION=$(curl -fsSL "https://go.dev/VERSION?m=text" | head -1)
    echo "Installing $GO_VERSION..."
    curl -fsSL "https://dl.google.com/go/${GO_VERSION}.linux-amd64.tar.gz" -o /tmp/go.tar.gz
    rm -rf /usr/local/go
    tar -C /usr/local -xzf /tmp/go.tar.gz
    rm -f /tmp/go.tar.gz
    ln -sf /usr/local/go/bin/go /usr/local/bin/go
    ln -sf /usr/local/go/bin/gofmt /usr/local/bin/gofmt
    echo "Installed: $(go version)"
else
    echo "Go already installed: $(go version)"
fi
```

- [ ] **Step 2: Verify syntax**

```bash
bash -n scripts/install.sh
```

- [ ] **Step 3: Commit**

```bash
git add scripts/install.sh
git commit -m "feat(install): add Go installation step"
```

---

### Task 5: Add Node.js and Bun installation (steps 5-6)

**Files:**
- Modify: `scripts/install.sh`

- [ ] **Step 1: Append Node.js installation logic**

```bash
# --- Step 5: Install Node.js ---
step "Installing Node.js"

need_node_install() {
    if ! command -v node &>/dev/null; then
        return 0
    fi
    local ver
    ver=$(node --version | sed 's/v\([0-9]*\).*/\1/')
    if [ "$ver" -lt 18 ]; then
        return 0
    fi
    return 1
}

if need_node_install; then
    echo "Installing Node.js LTS via NodeSource..."
    curl -fsSL https://deb.nodesource.com/setup_22.x | bash -
    apt-get install -y nodejs
    echo "Installed: node $(node --version)"
else
    echo "Node.js already installed: $(node --version)"
fi
```

- [ ] **Step 2: Append Bun installation logic**

```bash
# --- Step 6: Install Bun ---
step "Installing Bun"

if ! command -v bun &>/dev/null; then
    npm install -g bun
    echo "Installed: bun $(bun --version)"
else
    echo "Bun already installed: $(bun --version)"
fi
```

- [ ] **Step 3: Verify syntax**

```bash
bash -n scripts/install.sh
```

- [ ] **Step 4: Commit**

```bash
git add scripts/install.sh
git commit -m "feat(install): add Node.js and Bun installation steps"
```

---

### Task 6: Add Claude Code and agent-browser installation (steps 7-8)

**Files:**
- Modify: `scripts/install.sh`

- [ ] **Step 1: Append Claude Code installation logic**

Note: Claude Code must be installed as the `claude-agent` user (step 9 creates the user, but Claude Code install comes before in the spec). We need to **reorder**: create the user first if needed, then install Claude Code as that user. Move user creation (step 9) to happen before step 7.

Actually, looking at the spec ordering more carefully: steps 7-8 install tools globally, step 9 creates the user. Claude Code's native installer is the exception — it installs per-user. So we need to create the user before installing Claude Code.

Append user creation first (moved earlier), then Claude Code:

```bash
# --- Step 7: Create claude-agent user ---
step "Creating claude-agent user"

if id claude-agent &>/dev/null; then
    echo "User claude-agent already exists"
else
    useradd --create-home --shell /bin/bash claude-agent
    echo "Created user claude-agent"
fi

if [ ! -d /home/claude-agent ]; then
    fail "Home directory /home/claude-agent does not exist after user creation"
fi

mkdir -p /home/claude-agent/workspace
chown claude-agent:claude-agent /home/claude-agent/workspace

# --- Step 8: Install Claude Code ---
step "Installing Claude Code"

if sudo -u claude-agent -i claude --version &>/dev/null; then
    echo "Claude Code already installed: $(sudo -u claude-agent -i claude --version 2>/dev/null)"
else
    sudo -u claude-agent -i bash -c 'curl -fsSL https://claude.ai/install.sh | bash'
    # Symlink to system PATH if not already there
    CLAUDE_BIN=$(sudo -u claude-agent -i which claude 2>/dev/null || true)
    if [ -n "$CLAUDE_BIN" ] && [ ! -f /usr/local/bin/claude ]; then
        ln -sf "$CLAUDE_BIN" /usr/local/bin/claude
    fi
    echo "Installed Claude Code"
fi

# --- Step 9: Install agent-browser + Chrome for Testing ---
step "Installing agent-browser"

if command -v agent-browser &>/dev/null; then
    echo "agent-browser already installed"
else
    npm install -g @anthropic-ai/agent-browser
    echo "Installing Chrome for Testing..."
    agent-browser install --with-deps
    echo "Installed agent-browser"
fi
```

Note: This reorders spec steps 9(user)→7(claude)→8(agent-browser) to install steps 7(user)→8(claude)→9(agent-browser), because Claude Code's native installer is per-user and requires the `claude-agent` user to exist first. The step numbers displayed to the user stay sequential. `TOTAL_STEPS` is still 18.

- [ ] **Step 2: Verify syntax**

```bash
bash -n scripts/install.sh
```

- [ ] **Step 3: Commit**

```bash
git add scripts/install.sh
git commit -m "feat(install): add user creation, Claude Code, and agent-browser steps"
```

---

### Task 7: Add repo clone, build, and channel bridge (steps 10-13)

**Files:**
- Modify: `scripts/install.sh`

- [ ] **Step 1: Append repo, build, channel, and MCP config steps**

```bash
# --- Step 10: Clone or update repo ---
step "Setting up gobrrr source"

REPO_DIR="/home/claude-agent/gobrrr"

if [ -d "$REPO_DIR/.git" ]; then
    echo "Updating existing repo..."
    git -C "$REPO_DIR" fetch origin
    git -C "$REPO_DIR" reset --hard origin/main
else
    echo "Cloning gobrrr..."
    git clone https://github.com/racterub/gobrrr.git "$REPO_DIR"
fi
chown -R claude-agent:claude-agent "$REPO_DIR"

# --- Step 11: Build binary ---
step "Building gobrrr"

(cd "$REPO_DIR/daemon" && CGO_ENABLED=0 go build -o /usr/local/bin/gobrrr ./cmd/gobrrr/)
echo "Built: $(gobrrr --version 2>/dev/null || echo 'gobrrr installed')"

# --- Step 12: Install channel bridge dependencies ---
step "Installing channel bridge dependencies"

(cd "$REPO_DIR/channel" && sudo -u claude-agent bun install)
echo "Channel bridge dependencies installed"

# --- Step 13: Configure channel MCP ---
step "Configuring channel MCP"

MCP_FILE="/home/claude-agent/.mcp.json"
GOBRRR_MCP='{"type":"stdio","command":"bun","args":["run","/home/claude-agent/gobrrr/channel/index.ts"]}'

if [ -f "$MCP_FILE" ]; then
    # Merge into existing config, preserving other entries
    UPDATED=$(jq --argjson gobrrr "$GOBRRR_MCP" '.mcpServers.gobrrr = $gobrrr' "$MCP_FILE")
    echo "$UPDATED" > "$MCP_FILE"
    echo "Merged gobrrr into existing $MCP_FILE"
else
    # Create new file
    jq -n --argjson gobrrr "$GOBRRR_MCP" '{"mcpServers":{"gobrrr":$gobrrr}}' > "$MCP_FILE"
    echo "Created $MCP_FILE"
fi
chown claude-agent:claude-agent "$MCP_FILE"
```

- [ ] **Step 2: Verify syntax**

```bash
bash -n scripts/install.sh
```

- [ ] **Step 3: Commit**

```bash
git add scripts/install.sh
git commit -m "feat(install): add repo clone, build, channel bridge, and MCP config steps"
```

---

### Task 8: Add systemd, auth, setup, start, and verify (steps 14-18)

**Files:**
- Modify: `scripts/install.sh`

- [ ] **Step 1: Append remaining steps**

```bash
# --- Step 14: Install systemd unit ---
step "Installing systemd service"

cat > /etc/systemd/system/gobrrr.service << 'UNIT'
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
UNIT

systemctl daemon-reload
systemctl enable gobrrr
echo "Service installed and enabled"

# --- Step 15: Authenticate Claude Code ---
step "Authenticating Claude Code"

echo "Claude Code requires a long-lived auth token for headless operation."
echo "Generate one at https://claude.ai on a machine with a browser."
echo ""
sudo -u claude-agent -i claude setup-token

# --- Step 16: Run gobrrr setup ---
step "Running gobrrr setup wizard"

sudo -u claude-agent -i gobrrr setup

# --- Step 17: Start service ---
step "Starting gobrrr service"

if systemctl is-active --quiet gobrrr; then
    echo "Service already running, restarting..."
    systemctl restart gobrrr
else
    systemctl start gobrrr
fi

# --- Step 18: Verify ---
step "Verifying installation"

sleep 2  # Give daemon a moment to start

if systemctl is-active --quiet gobrrr; then
    echo "Service is running"
else
    echo "WARNING: Service is not running. Check: journalctl -u gobrrr -n 20"
fi

gobrrr --version
sudo -u claude-agent -i gobrrr daemon status || echo "WARNING: Daemon status check failed (may still be starting)"

echo ""
echo "==========================================="
echo "  gobrrr installation complete!"
echo "==========================================="
echo ""
echo "  Binary:   /usr/local/bin/gobrrr"
echo "  Config:   /home/claude-agent/.gobrrr/config.json"
echo "  Service:  systemctl status gobrrr"
echo "  Logs:     journalctl -u gobrrr -f"
echo ""
```

- [ ] **Step 2: Verify syntax**

```bash
bash -n scripts/install.sh
```

- [ ] **Step 3: Commit**

```bash
git add scripts/install.sh
git commit -m "feat(install): add systemd, auth, setup, start, and verify steps"
```

---

### Task 9: Add ERR trap and final polish

**Files:**
- Modify: `scripts/install.sh`

- [ ] **Step 1: Add ERR trap after `set -euo pipefail`**

Insert after `CURRENT_STEP=0` (so both variables are initialized before the trap references them):

```bash
trap 'echo ""; echo "FAILED at step [${CURRENT_STEP:-0}/${TOTAL_STEPS:-18}]"; echo "Check the output above for details."; exit 1' ERR
```

- [ ] **Step 2: Full syntax check**

```bash
bash -n scripts/install.sh
```

- [ ] **Step 3: Review the complete script end-to-end**

Read the full script and check:
- Step numbering is sequential 1-18
- All steps have the `step "..."` header
- No duplicate step numbers
- `TOTAL_STEPS=18` matches actual count

- [ ] **Step 4: Commit**

```bash
git add scripts/install.sh
git commit -m "feat(install): add error trap and final polish"
```

---

### Task 10: End-to-end verification

- [ ] **Step 1: Verify the build still passes**

```bash
cd /home/racterub/github/gobrrr/daemon && CGO_ENABLED=0 go build ./cmd/gobrrr/
```

- [ ] **Step 2: Verify tests pass**

```bash
cd /home/racterub/github/gobrrr/daemon && go test ./...
```

- [ ] **Step 3: Verify install script syntax is valid**

```bash
bash -n /home/racterub/github/gobrrr/scripts/install.sh
```

- [ ] **Step 4: Read the complete script and verify it matches the spec**

Read `scripts/install.sh` end-to-end and verify all 18 spec steps are present with correct commands and paths.

- [ ] **Step 5: Commit any final fixes if needed**
