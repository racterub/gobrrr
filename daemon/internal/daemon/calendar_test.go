package daemon_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"context"

	"github.com/racterub/gobrrr/internal/config"
	"github.com/racterub/gobrrr/internal/daemon"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// startCalendarTestDaemon starts a Daemon in the background for Calendar tests.
// The caller must invoke the returned cancel func.
func startCalendarTestDaemon(t *testing.T) (*http.Client, func()) {
	t.Helper()
	socketPath := filepath.Join(t.TempDir(), "test-gcal.sock")
	cfg := config.Default()
	cfg.WarmWorkers = 0
	d := daemon.New(cfg, socketPath)

	ctx, cancel := context.WithCancel(context.Background())
	go d.Run(ctx) //nolint:errcheck
	waitForSocket(t, socketPath, 2*time.Second)

	return unixClient(socketPath), cancel
}

func TestGcalCreateFromReadOnlyTaskReturns403(t *testing.T) {
	client, cancel := startCalendarTestDaemon(t)
	defer cancel()

	taskID := submitTask(t, client, false /* allowWrites=false */)

	reqBody := map[string]interface{}{
		"title":   "New meeting",
		"start":   "2026-03-24T10:00:00Z",
		"end":     "2026-03-24T11:00:00Z",
		"account": "default",
	}
	data, err := json.Marshal(reqBody)
	require.NoError(t, err)

	req, err := http.NewRequest(http.MethodPost, "http://gobrrr/gcal/create", bytes.NewReader(data))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Gobrrr-Task-ID", taskID)

	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestGcalUpdateFromReadOnlyTaskReturns403(t *testing.T) {
	client, cancel := startCalendarTestDaemon(t)
	defer cancel()

	taskID := submitTask(t, client, false)

	reqBody := map[string]interface{}{
		"event_id": "evt123",
		"title":    "Updated meeting",
		"account":  "default",
	}
	data, err := json.Marshal(reqBody)
	require.NoError(t, err)

	req, err := http.NewRequest(http.MethodPost, "http://gobrrr/gcal/update", bytes.NewReader(data))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Gobrrr-Task-ID", taskID)

	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestGcalDeleteFromReadOnlyTaskReturns403(t *testing.T) {
	client, cancel := startCalendarTestDaemon(t)
	defer cancel()

	taskID := submitTask(t, client, false)

	reqBody := map[string]interface{}{
		"event_id": "evt123",
		"account":  "default",
	}
	data, err := json.Marshal(reqBody)
	require.NoError(t, err)

	req, err := http.NewRequest(http.MethodPost, "http://gobrrr/gcal/delete", bytes.NewReader(data))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Gobrrr-Task-ID", taskID)

	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestGcalCreateFromWriteEnabledTaskReturns503NotForbidden(t *testing.T) {
	// When allow_writes=true, the write permission check passes.
	// The request will fail further (no real account manager → 503),
	// but it must NOT be 403.
	client, cancel := startCalendarTestDaemon(t)
	defer cancel()

	taskID := submitTask(t, client, true)

	reqBody := map[string]interface{}{
		"title":   "New meeting",
		"start":   "2026-03-24T10:00:00Z",
		"end":     "2026-03-24T11:00:00Z",
		"account": "default",
	}
	data, err := json.Marshal(reqBody)
	require.NoError(t, err)

	req, err := http.NewRequest(http.MethodPost, "http://gobrrr/gcal/create", bytes.NewReader(data))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Gobrrr-Task-ID", taskID)

	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.NotEqual(t, http.StatusForbidden, resp.StatusCode)
	assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
}

func TestGcalTodayWithNoAccountReturns400(t *testing.T) {
	client, cancel := startCalendarTestDaemon(t)
	defer cancel()

	reqBody := map[string]interface{}{} // missing account
	data, err := json.Marshal(reqBody)
	require.NoError(t, err)

	resp, err := client.Post("http://gobrrr/gcal/today", "application/json", bytes.NewReader(data))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestGcalWeekWithNoAccountReturns400(t *testing.T) {
	client, cancel := startCalendarTestDaemon(t)
	defer cancel()

	reqBody := map[string]interface{}{} // missing account
	data, err := json.Marshal(reqBody)
	require.NoError(t, err)

	resp, err := client.Post("http://gobrrr/gcal/week", "application/json", bytes.NewReader(data))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestGcalGetWithNoEventIDReturns400(t *testing.T) {
	client, cancel := startCalendarTestDaemon(t)
	defer cancel()

	reqBody := map[string]interface{}{
		"account": "default",
		// missing event_id
	}
	data, err := json.Marshal(reqBody)
	require.NoError(t, err)

	resp, err := client.Post("http://gobrrr/gcal/get", "application/json", bytes.NewReader(data))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestGcalTodayWithNoAccountManagerReturns503(t *testing.T) {
	client, cancel := startCalendarTestDaemon(t)
	defer cancel()

	reqBody := map[string]interface{}{
		"account": "default",
	}
	data, err := json.Marshal(reqBody)
	require.NoError(t, err)

	resp, err := client.Post("http://gobrrr/gcal/today", "application/json", bytes.NewReader(data))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
}
