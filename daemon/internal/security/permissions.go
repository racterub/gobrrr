package security

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type permissions struct {
	Allow []string `json:"allow"`
	Deny  []string `json:"deny"`
}

type settings struct {
	Permissions permissions `json:"permissions"`
}

// Generate creates a per-task settings.json for Claude Code workers.
// workersDir is the base directory (e.g. ~/.gobrrr/workers/), taskID is the task ID.
// Returns the path to the generated settings.json.
func Generate(workersDir string, taskID string, allowWrites bool) (string, error) {
	taskDir := filepath.Join(workersDir, taskID)
	if err := os.MkdirAll(taskDir, 0700); err != nil {
		return "", err
	}

	var s settings
	if allowWrites {
		s = settings{
			Permissions: permissions{
				Allow: []string{
					"Bash(gobrrr *)",
					"Bash(agent-browser *)",
					"Read",
					"Glob",
					"Grep",
					"Write",
					"Edit",
				},
				Deny: []string{
					"Bash(curl *)",
					"Bash(wget *)",
					"Bash(claude *)",
				},
			},
		}
	} else {
		s = settings{
			Permissions: permissions{
				Allow: []string{
					"Bash(gobrrr *)",
					"Bash(agent-browser *)",
					"Read",
					"Glob",
					"Grep",
				},
				Deny: []string{
					"Bash(curl *)",
					"Bash(wget *)",
					"Bash(claude *)",
					"Write",
					"Edit",
				},
			},
		}
	}

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return "", err
	}

	settingsPath := filepath.Join(taskDir, "settings.json")
	if err := os.WriteFile(settingsPath, data, 0600); err != nil {
		return "", err
	}

	return settingsPath, nil
}

// Cleanup removes the per-task settings directory.
func Cleanup(workersDir string, taskID string) error {
	taskDir := filepath.Join(workersDir, taskID)
	return os.RemoveAll(taskDir)
}
