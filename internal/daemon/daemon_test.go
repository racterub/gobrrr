package daemon_test

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/racterub/gobrrr/internal/config"
	"github.com/racterub/gobrrr/internal/daemon"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// unixClient returns an http.Client that dials the given Unix socket path.
func unixClient(socketPath string) *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", socketPath)
			},
		},
	}
}

// waitForSocket polls until a connection to the Unix socket succeeds or the deadline is reached.
func waitForSocket(t *testing.T, path string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.Dial("unix", path)
		if err == nil {
			conn.Close()
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("socket %s not ready within %s", path, timeout)
}

func TestDaemonStartsAndListens(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "test.sock")
	cfg := config.Default()
	d := daemon.New(cfg, socketPath)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- d.Run(ctx) }()

	waitForSocket(t, socketPath, 2*time.Second)

	// Verify we can connect to the socket
	conn, err := net.Dial("unix", socketPath)
	require.NoError(t, err)
	conn.Close()
}

func TestHealthEndpoint(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "test.sock")
	cfg := config.Default()
	d := daemon.New(cfg, socketPath)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go d.Run(ctx)

	waitForSocket(t, socketPath, 2*time.Second)

	client := unixClient(socketPath)
	resp, err := client.Get("http://gobrrr/health")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var body map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&body)
	require.NoError(t, err)

	assert.Equal(t, "ok", body["status"])
	assert.Contains(t, body, "uptime_sec")
	assert.Contains(t, body, "workers_active")
	assert.Contains(t, body, "queue_depth")
}

func TestHealthEndpointContentType(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "test.sock")
	cfg := config.Default()
	d := daemon.New(cfg, socketPath)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go d.Run(ctx)

	waitForSocket(t, socketPath, 2*time.Second)

	client := unixClient(socketPath)
	resp, err := client.Get("http://gobrrr/health")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, "application/json", resp.Header.Get("Content-Type"))
}

func TestUnknownRouteReturns404(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "test.sock")
	cfg := config.Default()
	d := daemon.New(cfg, socketPath)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go d.Run(ctx)

	waitForSocket(t, socketPath, 2*time.Second)

	client := unixClient(socketPath)
	resp, err := client.Get("http://gobrrr/nonexistent")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestGracefulShutdownOnContextCancel(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "test.sock")
	cfg := config.Default()
	d := daemon.New(cfg, socketPath)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() { done <- d.Run(ctx) }()

	waitForSocket(t, socketPath, 2*time.Second)

	// Cancel context to trigger shutdown
	cancel()

	select {
	case err := <-done:
		// Run should return nil on graceful shutdown
		assert.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("daemon did not shut down within 5 seconds")
	}
}

func TestSocketPermissions(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "test.sock")
	cfg := config.Default()
	d := daemon.New(cfg, socketPath)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go d.Run(ctx)

	waitForSocket(t, socketPath, 2*time.Second)

	info, err := os.Stat(socketPath)
	require.NoError(t, err)

	// Socket should be 0600 (owner read/write only)
	assert.Equal(t, os.FileMode(0600), info.Mode().Perm())
}

func TestStaleSocketRemoved(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "test.sock")

	// Create a stale socket file
	f, err := os.Create(socketPath)
	require.NoError(t, err)
	f.Close()

	cfg := config.Default()
	d := daemon.New(cfg, socketPath)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- d.Run(ctx) }()

	// Should start successfully even with the stale file
	waitForSocket(t, socketPath, 2*time.Second)

	client := unixClient(socketPath)
	resp, err := client.Get("http://gobrrr/health")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}
