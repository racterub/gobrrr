package daemon_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/racterub/gobrrr/internal/config"
	"github.com/racterub/gobrrr/internal/daemon"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// startGmailTestDaemon starts a Daemon in the background for Gmail tests.
// The caller must invoke the returned cancel func.
func startGmailTestDaemon(t *testing.T) (*http.Client, func()) {
	t.Helper()
	socketPath := filepath.Join(t.TempDir(), "test-gmail.sock")
	cfg := config.Default()
	d := daemon.New(cfg, socketPath)

	ctx, cancel := context.WithCancel(context.Background())
	go d.Run(ctx) //nolint:errcheck
	waitForSocket(t, socketPath, 2*time.Second)

	return unixClient(socketPath), cancel
}

// submitTaskAndMarkRunning submits a task and returns its ID.
// This mirrors the daemon's internal flow for tests.
func submitTask(t *testing.T, client *http.Client, allowWrites bool) string {
	t.Helper()
	body := map[string]interface{}{
		"prompt":       "test task",
		"allow_writes": allowWrites,
	}
	data, err := json.Marshal(body)
	require.NoError(t, err)

	resp, err := client.Post("http://gobrrr/tasks", "application/json", bytes.NewReader(data))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	var task map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&task)
	require.NoError(t, err)
	return task["id"].(string)
}

func TestSendFromReadOnlyTaskReturns403(t *testing.T) {
	client, cancel := startGmailTestDaemon(t)
	defer cancel()

	taskID := submitTask(t, client, false /* allowWrites=false */)

	reqBody := map[string]interface{}{
		"to":      "someone@example.com",
		"subject": "test",
		"body":    "hello",
		"account": "default",
	}
	data, err := json.Marshal(reqBody)
	require.NoError(t, err)

	req, err := http.NewRequest(http.MethodPost, "http://gobrrr/gmail/send", bytes.NewReader(data))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Gobrrr-Task-ID", taskID)

	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestReplyFromReadOnlyTaskReturns403(t *testing.T) {
	client, cancel := startGmailTestDaemon(t)
	defer cancel()

	taskID := submitTask(t, client, false /* allowWrites=false */)

	reqBody := map[string]interface{}{
		"message_id": "msg123",
		"body":       "Thanks",
		"account":    "default",
	}
	data, err := json.Marshal(reqBody)
	require.NoError(t, err)

	req, err := http.NewRequest(http.MethodPost, "http://gobrrr/gmail/reply", bytes.NewReader(data))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Gobrrr-Task-ID", taskID)

	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestSendFromWriteEnabledTaskReturns503NotForbidden(t *testing.T) {
	// When allow_writes=true, the write permission check passes.
	// The request will fail further (no real account manager → 503),
	// but it must NOT be 403.
	client, cancel := startGmailTestDaemon(t)
	defer cancel()

	taskID := submitTask(t, client, true /* allowWrites=true */)

	reqBody := map[string]interface{}{
		"to":      "someone@example.com",
		"subject": "test",
		"body":    "hello",
		"account": "default",
	}
	data, err := json.Marshal(reqBody)
	require.NoError(t, err)

	req, err := http.NewRequest(http.MethodPost, "http://gobrrr/gmail/send", bytes.NewReader(data))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Gobrrr-Task-ID", taskID)

	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Must not be 403 — should be 503 since accounts aren't configured.
	assert.NotEqual(t, http.StatusForbidden, resp.StatusCode)
	assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
}

func TestSendWithNoTaskIDAllowed(t *testing.T) {
	// Direct CLI calls (no X-Gobrrr-Task-ID) should pass write permission check.
	// They will fail with 503 since accounts aren't configured.
	client, cancel := startGmailTestDaemon(t)
	defer cancel()

	reqBody := map[string]interface{}{
		"to":      "someone@example.com",
		"subject": "test",
		"body":    "hello",
		"account": "default",
	}
	data, err := json.Marshal(reqBody)
	require.NoError(t, err)

	resp, err := client.Post("http://gobrrr/gmail/send", "application/json", bytes.NewReader(data))
	require.NoError(t, err)
	defer resp.Body.Close()

	// Not 403 — the permission check passes, fails on missing account manager.
	assert.NotEqual(t, http.StatusForbidden, resp.StatusCode)
}
