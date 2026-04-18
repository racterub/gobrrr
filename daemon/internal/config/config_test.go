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
	assert.Equal(t, 1, cfg.WarmWorkers)
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

func TestDefaultTelegramSessionConfig(t *testing.T) {
	cfg := config.Default()

	assert.False(t, cfg.TelegramSession.Enabled)
	assert.Equal(t, 3072, cfg.TelegramSession.MemoryCeilingMB)
	assert.Equal(t, 6, cfg.TelegramSession.MaxUptimeHours)
	assert.Equal(t, 5, cfg.TelegramSession.IdleThresholdMin)
	assert.Equal(t, 6, cfg.TelegramSession.MaxRestartAttempts)
}

func TestDefaultWorkspacePath(t *testing.T) {
	cfg := config.Default()
	home, err := os.UserHomeDir()
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(home, "workspace"), cfg.WorkspacePath)
}

func TestLoadConfigPreservesTelegramSessionDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	// Partial telegram_session: only enabled and memory_ceiling_mb set.
	// Other fields should get defaults from applyTelegramSessionDefaults().
	data := []byte(`{
		"telegram_session": {
			"enabled": true,
			"memory_ceiling_mb": 2048,
			"channels": ["plugin:telegram@claude-plugins-official"]
		}
	}`)
	require.NoError(t, os.WriteFile(path, data, 0600))

	cfg, err := config.Load(path)
	require.NoError(t, err)

	assert.True(t, cfg.TelegramSession.Enabled)
	assert.Equal(t, 2048, cfg.TelegramSession.MemoryCeilingMB)
	// Defaults applied for unset fields via applyTelegramSessionDefaults()
	assert.Equal(t, 6, cfg.TelegramSession.MaxUptimeHours)
	assert.Equal(t, 5, cfg.TelegramSession.IdleThresholdMin)
	assert.Equal(t, 6, cfg.TelegramSession.MaxRestartAttempts)
}

func TestDefaultModelsConfig(t *testing.T) {
	cfg := config.Default()

	assert.Equal(t, "haiku", cfg.Models.Launcher.Model)
	assert.Equal(t, "default", cfg.Models.Launcher.PermissionMode)

	assert.Equal(t, "sonnet", cfg.Models.WarmWorker.Model)
	assert.Equal(t, "auto", cfg.Models.WarmWorker.PermissionMode)

	assert.Equal(t, "opus", cfg.Models.ColdWorker.Model)
	assert.Equal(t, "auto", cfg.Models.ColdWorker.PermissionMode)
}

func TestLoadModelsFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	data := []byte(`{
		"models": {
			"launcher":    {"model": "haiku",  "permission_mode": "default"},
			"warm_worker": {"model": "opus",   "permission_mode": "auto"},
			"cold_worker": {"model": "sonnet", "permission_mode": "default"}
		}
	}`)
	require.NoError(t, os.WriteFile(path, data, 0600))

	cfg, err := config.Load(path)
	require.NoError(t, err)

	assert.Equal(t, "opus", cfg.Models.WarmWorker.Model)
	assert.Equal(t, "sonnet", cfg.Models.ColdWorker.Model)
	assert.Equal(t, "default", cfg.Models.ColdWorker.PermissionMode)
}

func TestLoadModelsPreservesDefaultsForMissingFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	// Partial models block: only launcher set.
	data := []byte(`{"models": {"launcher": {"model": "haiku", "permission_mode": "default"}}}`)
	require.NoError(t, os.WriteFile(path, data, 0600))

	cfg, err := config.Load(path)
	require.NoError(t, err)

	assert.Equal(t, "sonnet", cfg.Models.WarmWorker.Model)
	assert.Equal(t, "auto", cfg.Models.WarmWorker.PermissionMode)
	assert.Equal(t, "opus", cfg.Models.ColdWorker.Model)
	assert.Equal(t, "auto", cfg.Models.ColdWorker.PermissionMode)
}

func TestLoadRejectsUnknownPermissionMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	data := []byte(`{"models": {"warm_worker": {"model": "sonnet", "permission_mode": "nonsense"}}}`)
	require.NoError(t, os.WriteFile(path, data, 0600))

	_, err := config.Load(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nonsense")
}

func TestLoadFallsBackWhenLauncherHaikuAuto(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	// haiku + auto is rejected by Claude. Loader downgrades to default.
	data := []byte(`{"models": {"launcher": {"model": "haiku", "permission_mode": "auto"}}}`)
	require.NoError(t, os.WriteFile(path, data, 0600))

	cfg, err := config.Load(path)
	require.NoError(t, err)
	assert.Equal(t, "default", cfg.Models.Launcher.PermissionMode)
}
