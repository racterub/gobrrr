// Package daemon implements the gobrrr HTTP daemon that listens on a Unix socket.
package daemon

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/racterub/gobrrr/internal/config"
)

// Daemon is the HTTP daemon that serves the gobrrr API over a Unix socket.
type Daemon struct {
	cfg       *config.Config
	socket    string
	mux       *http.ServeMux
	startTime time.Time
}

// New creates a new Daemon configured to listen on the given socket path.
func New(cfg *config.Config, socket string) *Daemon {
	d := &Daemon{
		cfg:    cfg,
		socket: socket,
		mux:    http.NewServeMux(),
	}
	d.mux.HandleFunc("/health", d.handleHealth)
	return d
}

// Run starts the daemon and blocks until ctx is cancelled or a fatal error occurs.
// It returns nil on graceful shutdown.
func (d *Daemon) Run(ctx context.Context) error {
	d.startTime = time.Now()

	// Remove any stale socket file before binding.
	_ = os.Remove(d.socket)

	ln, err := net.Listen("unix", d.socket)
	if err != nil {
		return err
	}

	// Restrict socket to owner read/write only.
	if err := os.Chmod(d.socket, 0600); err != nil {
		ln.Close()
		return err
	}

	srv := &http.Server{Handler: d.mux}

	// Shut down when the context is done.
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		srv.Shutdown(shutdownCtx) //nolint:errcheck
	}()

	if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

// healthResponse is the JSON body returned by GET /health.
type healthResponse struct {
	Status        string `json:"status"`
	UptimeSec     int64  `json:"uptime_sec"`
	WorkersActive int    `json:"workers_active"`
	QueueDepth    int    `json:"queue_depth"`
}

func (d *Daemon) handleHealth(w http.ResponseWriter, r *http.Request) {
	resp := healthResponse{
		Status:        "ok",
		UptimeSec:     int64(time.Since(d.startTime).Seconds()),
		WorkersActive: 0,
		QueueDepth:    0,
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp) //nolint:errcheck
}
