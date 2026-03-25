package daemon

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// Heartbeat sends periodic push notifications to an Uptime Kuma push URL.
// When pushURL is empty, Run returns immediately (heartbeat disabled).
type Heartbeat struct {
	pushURL  string
	interval time.Duration
	status   string
	pingMB   int
	msg      string
	mu       sync.Mutex
}

// NewHeartbeat creates a new Heartbeat that will push to pushURL at the given interval.
func NewHeartbeat(pushURL string, interval time.Duration) *Heartbeat {
	return &Heartbeat{
		pushURL:  pushURL,
		interval: interval,
		status:   "up",
	}
}

// Update sets the current heartbeat status, memory usage in MB, and message.
// It is safe to call from any goroutine.
func (h *Heartbeat) Update(status string, pingMB int, msg string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.status = status
	h.pingMB = pingMB
	h.msg = msg
}

// Run starts the heartbeat ticker loop, pushing on each tick.
// If pushURL is empty, Run returns immediately.
// Run blocks until ctx is cancelled.
func (h *Heartbeat) Run(ctx context.Context) {
	if h.pushURL == "" {
		return
	}

	ticker := time.NewTicker(h.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			h.push()
		}
	}
}

// push sends a single GET request to the Uptime Kuma push URL with the current status.
func (h *Heartbeat) push() {
	h.mu.Lock()
	status := h.status
	pingMB := h.pingMB
	msg := h.msg
	h.mu.Unlock()

	params := url.Values{}
	params.Set("status", status)
	params.Set("msg", msg)
	params.Set("ping", fmt.Sprintf("%d", pingMB))

	fullURL := h.pushURL + "?" + params.Encode()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, nil)
	if err != nil {
		return
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return
	}
	resp.Body.Close()
}
