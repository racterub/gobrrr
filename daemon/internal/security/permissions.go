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
//
// skillRead is merged into allow unconditionally. skillWrite is merged only
// when allowWrites is true. The baseline deny list is preserved in both modes.
func Generate(workersDir string, taskID string, allowWrites bool, skillRead, skillWrite []string) (string, error) {
	taskDir := filepath.Join(workersDir, taskID)
	if err := os.MkdirAll(taskDir, 0700); err != nil {
		return "", err
	}

	baseAllow := []string{
		"Bash(gobrrr *)",
		"Bash(agent-browser *)",
		"Read",
		"Glob",
		"Grep",
	}
	if allowWrites {
		baseAllow = append(baseAllow, "Write", "Edit")
	}
	baseDeny := []string{
		"Bash(curl *)",
		"Bash(wget *)",
		"Bash(claude *)",
	}
	if !allowWrites {
		baseDeny = append(baseDeny, "Write", "Edit")
	}

	allow := append([]string{}, baseAllow...)
	allow = append(allow, skillRead...)
	if allowWrites {
		allow = append(allow, skillWrite...)
	}

	s := settings{
		Permissions: permissions{
			Allow: allow,
			Deny:  baseDeny,
		},
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
