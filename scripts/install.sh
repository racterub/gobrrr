#!/usr/bin/env bash
set -euo pipefail

# === gobrrr Installer ===
# Targets: Debian/Ubuntu amd64
# Requires: root (self-elevates)
# Idempotent: safe to re-run for upgrades

TOTAL_STEPS=21
CURRENT_STEP=0

trap 'echo ""; echo "FAILED at step [${CURRENT_STEP:-0}/${TOTAL_STEPS:-17}]"; echo "Check the output above for details."; exit 1' ERR

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

PACKAGES=(git curl jq unzip expect)
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

# --- Step 6: Create claude-agent user ---
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

# --- Step 7: Install Bun ---
step "Installing Bun"

if ! command -v bun &>/dev/null; then
    sudo -u claude-agent -i bash -c 'curl -fsSL https://bun.sh/install | bash'
    # Symlink to system PATH
    BUN_BIN="/home/claude-agent/.bun/bin/bun"
    if [ -f "$BUN_BIN" ] && [ ! -f /usr/local/bin/bun ]; then
        ln -sf "$BUN_BIN" /usr/local/bin/bun
    fi
    echo "Installed: bun $(sudo -u claude-agent -i bun --version)"
else
    echo "Bun already installed: $(bun --version)"
fi

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
    npm install -g agent-browser
    echo "Installing Chrome for Testing and system dependencies..."
    agent-browser install --with-deps
    echo "Installed agent-browser"
fi

# --- Step 10: Clone or update repo ---
step "Setting up gobrrr source"

REPO_DIR="/home/claude-agent/gobrrr"

if [ -d "$REPO_DIR/.git" ]; then
    if git -C "$REPO_DIR" remote get-url origin &>/dev/null; then
        echo "Updating existing repo..."
        git -C "$REPO_DIR" fetch origin
        git -C "$REPO_DIR" reset --hard origin/main
    else
        echo "Repo exists (no remote configured, skipping update)"
    fi
else
    echo "Cloning gobrrr..."
    git clone https://github.com/racterub/gobrrr.git "$REPO_DIR"
fi
chown -R claude-agent:claude-agent "$REPO_DIR"

# --- Step 11: Build binary ---
step "Building gobrrr"

(cd "$REPO_DIR/daemon" && CGO_ENABLED=0 go build -o /usr/local/bin/gobrrr ./cmd/gobrrr/)
echo "Built: $(gobrrr --version 2>/dev/null || echo 'gobrrr installed')"

# --- Step 12: Install gobrrr channel plugin dependencies ---
step "Installing gobrrr channel plugin dependencies"

(cd "$REPO_DIR/plugins/gobrrr-relay" && sudo -u claude-agent bun install)
echo "gobrrr-relay channel plugin dependencies installed"

# --- Step 13: Clean up legacy channel MCP entry ---
step "Cleaning up legacy channel MCP entry"

MCP_FILE="/home/claude-agent/.mcp.json"
if [ -f "$MCP_FILE" ] && jq -e '.mcpServers.gobrrr' "$MCP_FILE" &>/dev/null; then
    MCP_TMP="${MCP_FILE}.tmp"
    jq 'del(.mcpServers.gobrrr)' "$MCP_FILE" > "$MCP_TMP"
    mv "$MCP_TMP" "$MCP_FILE"
    chown claude-agent:claude-agent "$MCP_FILE"
    echo "Removed legacy gobrrr entry from $MCP_FILE (now a channel plugin)"
else
    echo "No legacy gobrrr MCP entry found, skipping"
fi

# --- Step 14: Install systemd unit ---
step "Installing systemd service"

cp "$REPO_DIR/daemon/systemd/gobrrr.service" /etc/systemd/system/gobrrr.service
cp "$REPO_DIR/daemon/systemd/gobrrr-launcher.service" /etc/systemd/system/gobrrr-launcher.service

systemctl daemon-reload
systemctl enable gobrrr
systemctl enable gobrrr-launcher
echo "Services installed and enabled"

# --- Step 15: Authenticate Claude Code ---
step "Authenticating Claude Code"

if sudo -u claude-agent -i claude -p "exit" --max-turns 1 &>/dev/null; then
    echo "Claude Code already authenticated"
else
    echo "Claude Code requires a long-lived auth token for headless operation."
    echo "Generate one at https://claude.ai on a machine with a browser."
    echo ""
    sudo -u claude-agent -i claude setup-token
fi

# --- Step 16: Install gobrrr-telegram channel plugin ---
step "Installing gobrrr-telegram channel plugin"

# Build the Go binary and stage it inside the plugin dir. The plugin's
# .mcp.json resolves it via ${CLAUDE_PLUGIN_ROOT}/gobrrr-telegram so the
# marketplace install ships a self-contained plugin.
PLUGIN_SRC="$REPO_DIR/plugins/gobrrr-telegram"
sudo -u claude-agent bash -c "cd '$REPO_DIR/daemon' && CGO_ENABLED=0 go build -o '$PLUGIN_SRC/gobrrr-telegram' ./cmd/gobrrr-telegram/"
echo "Built gobrrr-telegram binary"

# Publish a local marketplace pointing at the plugin directory.
MARKETPLACE_DIR="/home/claude-agent/.gobrrr/marketplaces/gobrrr-local"
sudo -u claude-agent mkdir -p "$MARKETPLACE_DIR/.claude-plugin"
sudo -u claude-agent tee "$MARKETPLACE_DIR/.claude-plugin/marketplace.json" > /dev/null << MARKETPLACE
{
  "name": "gobrrr-local",
  "owner": { "name": "racterub" },
  "plugins": [
    {
      "name": "gobrrr-telegram",
      "source": "$PLUGIN_SRC",
      "description": "gobrrr telegram channel plugin"
    },
    {
      "name": "gobrrr-relay",
      "source": "$REPO_DIR/plugins/gobrrr-relay",
      "description": "gobrrr task result channel bridge"
    }
  ]
}
MARKETPLACE

# Claude Code must be launched once after auth to initialize the plugin marketplace.
echo "Initializing Claude Code marketplace..."
sudo -u claude-agent -i claude -p "exit" --max-turns 1 &>/dev/null || true

# Remove the official telegram plugin if previously installed.
if sudo -u claude-agent -i claude plugins installed 2>/dev/null | grep -q "telegram@claude-plugins-official"; then
    sudo -u claude-agent -i claude plugin disable telegram@claude-plugins-official &>/dev/null || true
    sudo -u claude-agent -i claude plugin uninstall telegram@claude-plugins-official &>/dev/null || true
    echo "Removed official telegram plugin"
fi

# Add the local marketplace (idempotent).
if ! sudo -u claude-agent -i claude plugin marketplace list 2>/dev/null | grep -q "gobrrr-local"; then
    sudo -u claude-agent -i claude plugin marketplace add "$MARKETPLACE_DIR"
    echo "Added gobrrr-local marketplace"
fi

if sudo -u claude-agent -i claude plugins installed 2>/dev/null | grep -q "gobrrr-telegram@gobrrr-local"; then
    echo "gobrrr-telegram plugin already installed"
else
    sudo -u claude-agent -i claude plugin install gobrrr-telegram@gobrrr-local
    echo "Installed gobrrr-telegram plugin"
fi

if sudo -u claude-agent -i claude plugins installed 2>/dev/null | grep -q "gobrrr-relay@gobrrr-local"; then
    echo "gobrrr-relay channel plugin already installed"
else
    sudo -u claude-agent -i claude plugin install gobrrr-relay@gobrrr-local
    echo "Installed gobrrr-relay channel plugin"
fi

# --- Step 17: Configure Claude Code settings ---
step "Configuring Claude Code settings"

CLAUDE_SETTINGS="/home/claude-agent/.claude/settings.json"
mkdir -p /home/claude-agent/.claude
cat > "${CLAUDE_SETTINGS}.tmp" << 'SETTINGS'
{
  "permissions": {
    "allow": [
      "Read",
      "Write",
      "Edit",
      "Glob",
      "Grep",
      "Agent",
      "Skill",
      "WebFetch",
      "WebSearch",
      "Bash(git *)",
      "Bash(ls *)",
      "Bash(cat *)",
      "Bash(head *)",
      "Bash(tail *)",
      "Bash(grep *)",
      "Bash(find *)",
      "Bash(wc *)",
      "Bash(jq *)",
      "Bash(mkdir *)",
      "Bash(cp *)",
      "Bash(mv *)",
      "Bash(touch *)",
      "Bash(echo *)",
      "Bash(date *)",
      "Bash(diff *)",
      "Bash(sort *)",
      "Bash(uniq *)",
      "Bash(sed *)",
      "Bash(awk *)",
      "Bash(tee *)",
      "Bash(curl *)",
      "Bash(wget *)",
      "Bash(python3 *)",
      "Bash(node *)",
      "Bash(bun *)",
      "Bash(npm *)",
      "Bash(npx *)",
      "Bash(claude *)",
      "Bash(gobrrr *)",
      "mcp__claude_ai_Gmail__*",
      "mcp__claude_ai_Google_Calendar__*",
      "mcp__plugin_gobrrr-telegram_telegram__*",
      "mcp__context7__*",
      "mcp__plugin_gobrrr-relay_gobrrr-relay__*"
    ],
    "deny": [
      "Bash(sudo *)",
      "Bash(su *)",
      "Bash(apt *)",
      "Bash(dpkg *)",
      "Bash(rm -rf /*)",
      "Bash(rm -rf ~/*)",
      "Bash(dd *)",
      "Bash(mkfs *)",
      "Bash(reboot *)",
      "Bash(shutdown *)",
      "Bash(passwd *)",
      "Bash(chmod 777 *)"
    ]
  },
  "enabledPlugins": {
    "gobrrr-telegram@gobrrr-local": true,
    "gobrrr-relay@gobrrr-local": true
  },
  "skipDangerousModePermissionPrompt": true
}
SETTINGS
mv "${CLAUDE_SETTINGS}.tmp" "$CLAUDE_SETTINGS"
chown claude-agent:claude-agent "$CLAUDE_SETTINGS"
echo "Settings configured"

# --- Step 18: Install default identity.md and CLAUDE.md ---
step "Installing default identity.md and CLAUDE.md"

install -d -o claude-agent -g claude-agent -m 0700 /home/claude-agent/.gobrrr

if [ ! -f /home/claude-agent/.gobrrr/identity.md ]; then
    install -o claude-agent -g claude-agent -m 0644 \
        "$REPO_DIR/identity.md.default" /home/claude-agent/.gobrrr/identity.md
    echo "Installed default identity.md"
else
    echo "identity.md already exists, leaving it alone"
fi

if [ ! -f /home/claude-agent/.claude/CLAUDE.md ]; then
    install -o claude-agent -g claude-agent -m 0644 \
        "$REPO_DIR/claude-md.default" /home/claude-agent/.claude/CLAUDE.md
    echo "Installed default ~/.claude/CLAUDE.md"
else
    echo "~/.claude/CLAUDE.md already exists, leaving it alone"
fi

# --- Step 19: Pre-trust workspace directory ---
step "Pre-trusting workspace for Claude Code"

TRUST_DIR="/home/claude-agent/.claude/projects/-home-claude-agent-workspace"
mkdir -p "$TRUST_DIR"
chown -R claude-agent:claude-agent /home/claude-agent/.claude/projects
echo "Workspace /home/claude-agent/workspace trusted"

# --- Step 20: Run gobrrr setup ---
step "Running gobrrr setup wizard"

sudo -u claude-agent -i gobrrr setup

# --- Step 21: Start service ---
step "Starting gobrrr service"

if systemctl is-active --quiet gobrrr; then
    echo "gobrrr already running, restarting..."
    systemctl restart gobrrr
else
    systemctl start gobrrr
fi

if systemctl is-active --quiet gobrrr-launcher; then
    echo "gobrrr-launcher already running, restarting..."
    systemctl restart gobrrr-launcher
else
    systemctl start gobrrr-launcher
fi

# --- Verify ---
step "Verifying installation"

sleep 2  # Give daemon a moment to start

if systemctl is-active --quiet gobrrr; then
    echo "gobrrr is running"
else
    echo "WARNING: gobrrr is not running. Check: journalctl -u gobrrr -n 20"
fi

if systemctl is-active --quiet gobrrr-launcher; then
    echo "gobrrr-launcher is running"
else
    echo "WARNING: gobrrr-launcher is not running. Check: journalctl -u gobrrr-launcher -n 20"
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
