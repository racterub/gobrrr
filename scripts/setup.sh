#!/bin/bash
set -euo pipefail

REPO="https://github.com/racterub/gobrrr"
INSTALL_DIR="$HOME/github/gobrrr"

echo "=== gobrrr Installer ==="

# Check prerequisites
command -v go >/dev/null 2>&1 || { echo "Error: Go is required. Install from https://go.dev/dl/"; exit 1; }
command -v git >/dev/null 2>&1 || { echo "Error: Git is required."; exit 1; }

# Ensure ~/.local/bin exists and is in PATH
mkdir -p "$HOME/.local/bin"
if [[ ":$PATH:" != *":$HOME/.local/bin:"* ]]; then
    echo "Note: Add ~/.local/bin to your PATH (e.g. in ~/.bashrc or ~/.zshrc):"
    echo "  export PATH=\"\$HOME/.local/bin:\$PATH\""
fi

# Clone or update
if [ -d "$INSTALL_DIR" ]; then
    echo "Updating existing installation..."
    cd "$INSTALL_DIR" && git pull
else
    echo "Cloning gobrrr..."
    git clone "$REPO" "$INSTALL_DIR"
    cd "$INSTALL_DIR"
fi

# Build
echo "Building..."
CGO_ENABLED=0 go build -o "$HOME/.local/bin/gobrrr" ./cmd/gobrrr/
echo "Installed to ~/.local/bin/gobrrr"

# Run setup
echo
"$HOME/.local/bin/gobrrr" setup
