package daemon

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// warmSettingsFilename is the name of the shared warm-worker permissions file.
const warmSettingsFilename = "warm-settings.json"

// warmSettings is the default permissions document for warm workers.
// Allow-listed tools skip the auto-mode classifier entirely, minimizing
// classifier invocations (and therefore classifier aborts) for routine tasks.
//
// enabledPlugins explicitly disables the launcher's channel plugins. Without
// this override, warm workers inherit user-level settings.json from the
// install (which enables gobrrr-telegram/gobrrr-relay globally) and each
// warm worker spawns its own gobrrr-telegram that races the launcher for
// Telegram getUpdates, causing message loss and silent non-replies.
var warmSettings = map[string]any{
	"permissions": map[string]any{
		"allow": []string{
			"Read", "Glob", "Grep",
			"Bash(gobrrr memory:*)",
			"Bash(git log:*)", "Bash(git status)", "Bash(git diff:*)",
		},
		"deny": []string{
			"Write", "Edit", "NotebookEdit",
			"Bash(rm:*)", "Bash(git push:*)",
		},
	},
	"enabledPlugins": map[string]any{
		"gobrrr-telegram@gobrrr-local": false,
		"gobrrr-relay@gobrrr-local":    false,
	},
}

// EnsureWarmSettings writes the warm-worker permissions file at
// <gobrrDir>/workers/warm-settings.json if it does not already exist.
// Returns the file path. Idempotent: existing files are left untouched so
// operators can edit the allow-list without fear of daemon overwrite.
func EnsureWarmSettings(gobrrDir string) (string, error) {
	workersDir := filepath.Join(gobrrDir, "workers")
	if err := os.MkdirAll(workersDir, 0700); err != nil {
		return "", err
	}
	path := filepath.Join(workersDir, warmSettingsFilename)

	if _, err := os.Stat(path); err == nil {
		return path, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}

	data, err := json.MarshalIndent(warmSettings, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		return "", err
	}
	return path, nil
}
