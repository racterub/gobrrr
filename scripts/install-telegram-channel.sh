#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BIN_DIR="${GOBRRR_HOME:-$HOME/.gobrrr}/bin"
mkdir -p "$BIN_DIR"

cd "$REPO_ROOT/daemon"
CGO_ENABLED=0 go build -o "$BIN_DIR/gobrrr-telegram" ./cmd/gobrrr-telegram/

echo "installed: $BIN_DIR/gobrrr-telegram"
echo
echo "Next steps:"
echo "  1. Ensure $BIN_DIR is on your PATH, or edit plugins/gobrrr-telegram/plugin.json"
echo "     to use the absolute path $BIN_DIR/gobrrr-telegram."
echo "  2. In Claude Code, disable the official 'telegram' plugin."
echo "  3. Enable 'gobrrr-telegram' from your plugin marketplace."
