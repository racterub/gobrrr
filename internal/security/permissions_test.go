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
	path, err := Generate(dir, "t_123", false)
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
	path, err := Generate(dir, "t_123", true)
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
	path, _ := Generate(dir, "t_123", false)
	assert.FileExists(t, path)
	Cleanup(dir, "t_123") //nolint:errcheck
	assert.NoFileExists(t, path)
}

func TestFilePermissions(t *testing.T) {
	dir := t.TempDir()
	path, _ := Generate(dir, "t_123", false)
	info, _ := os.Stat(path)
	assert.Equal(t, os.FileMode(0600), info.Mode().Perm())
}
