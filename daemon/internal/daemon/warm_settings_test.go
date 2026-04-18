package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnsureWarmSettingsCreatesFile(t *testing.T) {
	dir := t.TempDir()
	gobrrDir := filepath.Join(dir, ".gobrrr")

	path, err := EnsureWarmSettings(gobrrDir)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(gobrrDir, "workers", "warm-settings.json"), path)

	data, err := os.ReadFile(path)
	require.NoError(t, err)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal(data, &parsed))
	perms, ok := parsed["permissions"].(map[string]any)
	require.True(t, ok, "expected permissions object")

	allow, ok := perms["allow"].([]any)
	require.True(t, ok)
	assert.Contains(t, allow, "Read")
	assert.Contains(t, allow, "Glob")

	deny, ok := perms["deny"].([]any)
	require.True(t, ok)
	assert.Contains(t, deny, "Write")
	assert.Contains(t, deny, "Edit")
}

func TestEnsureWarmSettingsIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	gobrrDir := filepath.Join(dir, ".gobrrr")

	path, err := EnsureWarmSettings(gobrrDir)
	require.NoError(t, err)

	// Overwrite with arbitrary content — EnsureWarmSettings should not clobber it.
	sentinel := []byte(`{"marker":"user-edit"}`)
	require.NoError(t, os.WriteFile(path, sentinel, 0600))

	_, err = EnsureWarmSettings(gobrrDir)
	require.NoError(t, err)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, sentinel, data, "EnsureWarmSettings must not overwrite existing file")
}
