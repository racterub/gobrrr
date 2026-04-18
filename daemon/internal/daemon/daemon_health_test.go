package daemon

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/racterub/gobrrr/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHealthEndpointReportsDisabledWarmWorkers(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{WorkspacePath: dir}
	d := New(cfg, filepath.Join(dir, "sock"))

	// Inject a disabled warm worker directly into the pool.
	ww := NewWarmWorker(0, dir, nil, nil)
	ww.ready = true
	require.True(t, ww.RecordRespawnAttempt())
	require.False(t, ww.RecordRespawnAttempt())
	require.True(t, ww.Disabled())

	d.workerPool.mu.Lock()
	d.workerPool.warmWorkers = []*WarmWorker{ww}
	d.workerPool.mu.Unlock()

	req := httptest.NewRequest("GET", "/health", nil)
	rr := httptest.NewRecorder()
	d.handleHealth(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &body))

	warm, ok := body["warm_workers"].(map[string]any)
	require.True(t, ok, "expected warm_workers object in health response")
	assert.Equal(t, float64(1), warm["total"], "total should count disabled workers")
	assert.Equal(t, float64(0), warm["ready"])
	assert.Equal(t, float64(0), warm["busy"])
	assert.Equal(t, float64(1), warm["disabled"], "disabled count must be surfaced in health response")
}

func TestHealthEndpointIncludesModels(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{
		WorkspacePath: dir,
		Models: config.ModelsConfig{
			Launcher:   config.ModelConfig{Model: "haiku", PermissionMode: "default"},
			WarmWorker: config.ModelConfig{Model: "sonnet", PermissionMode: "auto"},
			ColdWorker: config.ModelConfig{Model: "opus", PermissionMode: "auto"},
		},
	}
	d := New(cfg, filepath.Join(dir, "sock"))

	req := httptest.NewRequest("GET", "/health", nil)
	rr := httptest.NewRecorder()
	d.handleHealth(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &body))

	models, ok := body["models"].(map[string]any)
	require.True(t, ok, "expected models object in health response")

	launcher, ok := models["launcher"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "haiku", launcher["model"])
	assert.Equal(t, "default", launcher["permission_mode"])

	warm, ok := models["warm_worker"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "sonnet", warm["model"])

	cold, ok := models["cold_worker"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "opus", cold["model"])
}
