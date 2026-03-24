package daemon_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/racterub/gobrrr/internal/daemon"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHeartbeatSendsUp(t *testing.T) {
	var mu sync.Mutex
	var received []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		received = append(received, r.URL.RawQuery)
		mu.Unlock()
		w.WriteHeader(200)
	}))
	defer server.Close()

	hb := daemon.NewHeartbeat(server.URL, 100*time.Millisecond)
	hb.Update("up", 42, "2 workers, 0 queued")
	ctx, cancel := context.WithCancel(context.Background())
	go hb.Run(ctx)
	time.Sleep(250 * time.Millisecond)
	cancel()

	mu.Lock()
	defer mu.Unlock()
	require.NotEmpty(t, received)
	assert.Contains(t, received[0], "status=up")
	assert.Contains(t, received[0], "ping=42")
}

func TestHeartbeatDisabledWhenNoURL(t *testing.T) {
	hb := daemon.NewHeartbeat("", time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Run should return immediately
	done := make(chan struct{})
	go func() { hb.Run(ctx); close(done) }()
	select {
	case <-done:
		// good, returned immediately
	case <-time.After(time.Second):
		t.Fatal("Run should return immediately with empty URL")
	}
}

func TestHeartbeatSendsDown(t *testing.T) {
	var mu sync.Mutex
	var received []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		received = append(received, r.URL.RawQuery)
		mu.Unlock()
		w.WriteHeader(200)
	}))
	defer server.Close()

	hb := daemon.NewHeartbeat(server.URL, 100*time.Millisecond)
	hb.Update("down", 10, "all tasks failed")
	ctx, cancel := context.WithCancel(context.Background())
	go hb.Run(ctx)
	time.Sleep(250 * time.Millisecond)
	cancel()

	mu.Lock()
	defer mu.Unlock()
	require.NotEmpty(t, received)
	assert.Contains(t, received[0], "status=down")
}
