package security

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadOnlySettings(t *testing.T) {
	dir := t.TempDir()
	path, err := Generate(dir, "t_123", false, nil, nil)
	require.NoError(t, err)
	data, _ := os.ReadFile(path)
	var settings map[string]interface{}
	json.Unmarshal(data, &settings) //nolint:errcheck
	perms := settings["permissions"].(map[string]interface{})
	deny := perms["deny"].([]interface{})
	assert.Contains(t, deny, "Bash(curl *)")
	assert.Contains(t, deny, "Write")
	assert.Contains(t, deny, "Edit")
}

func TestWriteEnabledSettings(t *testing.T) {
	dir := t.TempDir()
	path, err := Generate(dir, "t_123", true, nil, nil)
	require.NoError(t, err)
	data, _ := os.ReadFile(path)
	var settings map[string]interface{}
	json.Unmarshal(data, &settings) //nolint:errcheck
	perms := settings["permissions"].(map[string]interface{})
	allow := perms["allow"].([]interface{})
	assert.Contains(t, allow, "Write")
	assert.Contains(t, allow, "Edit")
}

func TestCleanup(t *testing.T) {
	dir := t.TempDir()
	path, _ := Generate(dir, "t_123", false, nil, nil)
	assert.FileExists(t, path)
	Cleanup(dir, "t_123") //nolint:errcheck
	assert.NoFileExists(t, path)
}

func TestFilePermissions(t *testing.T) {
	dir := t.TempDir()
	path, _ := Generate(dir, "t_123", false, nil, nil)
	info, _ := os.Stat(path)
	assert.Equal(t, os.FileMode(0600), info.Mode().Perm())
}

func TestGenerate_MergesSkillReadPermissions(t *testing.T) {
	workers := t.TempDir()
	path, err := Generate(workers, "task-1", false,
		[]string{"Bash(gh issue list:*)", "Bash(gobrrr gmail list:*)"},
		[]string{"Bash(gh pr create:*)"})
	require.NoError(t, err)

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	var s settings
	require.NoError(t, json.Unmarshal(raw, &s))

	assert.Contains(t, s.Permissions.Allow, "Bash(gh issue list:*)")
	assert.Contains(t, s.Permissions.Allow, "Bash(gobrrr gmail list:*)")
	assert.NotContains(t, s.Permissions.Allow, "Bash(gh pr create:*)", "writes forbidden without allowWrites")
	assert.Contains(t, s.Permissions.Deny, "Bash(curl *)")
}

func TestGenerate_MergesSkillWritePermissionsWhenAllowed(t *testing.T) {
	workers := t.TempDir()
	path, err := Generate(workers, "task-2", true,
		[]string{"Bash(gh issue list:*)"},
		[]string{"Bash(gh pr create:*)"})
	require.NoError(t, err)

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	var s settings
	require.NoError(t, json.Unmarshal(raw, &s))

	assert.Contains(t, s.Permissions.Allow, "Bash(gh issue list:*)")
	assert.Contains(t, s.Permissions.Allow, "Bash(gh pr create:*)")
}

func TestGenerate_EmptySkillListsBehaviorUnchanged(t *testing.T) {
	workers := t.TempDir()
	path, err := Generate(workers, "task-3", false, nil, nil)
	require.NoError(t, err)

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	var s settings
	require.NoError(t, json.Unmarshal(raw, &s))
	// Default deny list still intact.
	assert.Contains(t, s.Permissions.Deny, "Bash(curl *)")
	assert.Contains(t, s.Permissions.Deny, "Write")
}
