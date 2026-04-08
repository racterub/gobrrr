#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
PLUGIN_DIR="$REPO_ROOT/plugins/gobrrr-telegram"
BIN_DIR="${GOBRRR_HOME:-$HOME/.gobrrr}/bin"
mkdir -p "$BIN_DIR"

cd "$REPO_ROOT/daemon"
CGO_ENABLED=0 go build -o "$BIN_DIR/gobrrr-telegram" ./cmd/gobrrr-telegram/

# Also stage binary inside the plugin so the marketplace ships it; .mcp.json
# resolves it via ${CLAUDE_PLUGIN_ROOT}/gobrrr-telegram.
cp "$BIN_DIR/gobrrr-telegram" "$PLUGIN_DIR/gobrrr-telegram"

echo "installed: $BIN_DIR/gobrrr-telegram"
echo "staged:    $PLUGIN_DIR/gobrrr-telegram"
echo
echo "Next steps:"
echo "  1. In Claude Code, disable the official 'telegram' plugin."
echo "  2. Enable 'gobrrr-telegram' from your plugin marketplace."
echo "  3. Launch claude with the channel flag, e.g.:"
echo "       claude --dangerously-load-development-channels plugin:gobrrr-telegram@<marketplace>"
