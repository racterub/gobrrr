package config_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/racterub/gobrrr/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultConfig(t *testing.T) {
	cfg := config.Default()
	assert.Equal(t, 2, cfg.MaxWorkers)
	assert.Equal(t, 300, cfg.DefaultTimeoutSec)
	assert.Equal(t, 5, cfg.SpawnIntervalSec)
	assert.Equal(t, 7, cfg.LogRetentionDays)
	assert.Equal(t, 60, cfg.UptimeKuma.IntervalSec)
}

func TestLoadFromFile(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")

	data := map[string]any{
		"max_workers":         4,
		"default_timeout_sec": 600,
		"log_retention_days":  14,
	}
	b, err := json.Marshal(data)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(cfgPath, b, 0o644))

	cfg, err := config.Load(cfgPath)
	require.NoError(t, err)

	// Overrides from file
	assert.Equal(t, 4, cfg.MaxWorkers)
	assert.Equal(t, 600, cfg.DefaultTimeoutSec)
	assert.Equal(t, 14, cfg.LogRetentionDays)

	// Defaults preserved for fields not in file
	assert.Equal(t, 5, cfg.SpawnIntervalSec)
	assert.Equal(t, 60, cfg.UptimeKuma.IntervalSec)
}

func TestLoadDefaultsWhenFileDoesNotExist(t *testing.T) {
	cfg, err := config.Load("/nonexistent/path/config.json")
	require.NoError(t, err)

	expected := config.Default()
	assert.Equal(t, expected, cfg)
}

func TestLoadMalformedJSON(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")

	require.NoError(t, os.WriteFile(cfgPath, []byte("{not valid json}"), 0o644))

	_, err := config.Load(cfgPath)
	assert.Error(t, err)
}

func TestGobrrDir(t *testing.T) {
	t.Setenv("GOBRRR_DIR", "/tmp/test-gobrrr")
	assert.Equal(t, "/tmp/test-gobrrr", config.GobrrDir())
}

func TestGobrrDirDefault(t *testing.T) {
	t.Setenv("GOBRRR_DIR", "")
	dir := config.GobrrDir()
	home, err := os.UserHomeDir()
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(home, ".gobrrr"), dir)
}
