#!/bin/bash
set -euo pipefail

echo "=== gobrrr Uninstaller ==="

# Stop and disable service
echo "Stopping gobrrr daemon..."
systemctl --user stop gobrrr.service 2>/dev/null || true
systemctl --user disable gobrrr.service 2>/dev/null || true
rm -f "$HOME/.config/systemd/user/gobrrr.service"
systemctl --user daemon-reload 2>/dev/null || true

# Remove binary
echo "Removing binary..."
rm -f "$HOME/.local/bin/gobrrr"

# Ask about data
echo
echo "Remove data directory ~/.gobrrr? This includes:"
echo "  - Encrypted credentials"
echo "  - Memory entries"
echo "  - Task logs"
echo "  - Configuration"
echo
read -rp "Remove? [y/N]: " confirm
if [[ "${confirm,,}" == "y" ]]; then
    rm -rf "$HOME/.gobrrr"
    echo "Data removed."
else
    echo "Data preserved at ~/.gobrrr"
fi

echo "Done."
