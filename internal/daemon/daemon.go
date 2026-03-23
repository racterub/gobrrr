// Package daemon implements the gobrrr HTTP daemon that listens on a Unix socket.
package daemon

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/racterub/gobrrr/internal/config"
)

// Daemon is the HTTP daemon that serves the gobrrr API over a Unix socket.
type Daemon struct {
	cfg        *config.Config
	socket     string
	gobrrDir   string
	mux        *http.ServeMux
	queue      *Queue
	workerPool *WorkerPool
	startTime  time.Time
}

// New creates a new Daemon configured to listen on the given socket path.
// gobrrDir is the directory where queue.json and logs are stored.
func New(cfg *config.Config, socket string) *Daemon {
	gobrrDir := config.GobrrDir()
	queuePath := filepath.Join(gobrrDir, "queue.json")

	var q *Queue
	loaded, err := LoadQueue(queuePath)
	if err != nil {
		// If loading fails (e.g. corrupt file), start fresh.
		q = NewQueue(queuePath)
	} else {
		q = loaded
	}

	spawnInterval := time.Duration(cfg.SpawnIntervalSec) * time.Second
	wp := NewWorkerPool(q, cfg.MaxWorkers, spawnInterval, gobrrDir)

	d := &Daemon{
		cfg:        cfg,
		socket:     socket,
		gobrrDir:   gobrrDir,
		mux:        http.NewServeMux(),
		queue:      q,
		workerPool: wp,
	}
	d.mux.HandleFunc("/health", d.handleHealth)
	d.mux.HandleFunc("POST /tasks", d.handleSubmitTask)
	d.mux.HandleFunc("GET /tasks", d.handleListTasks)
	d.mux.HandleFunc("GET /tasks/{id}", d.handleGetTask)
	d.mux.HandleFunc("DELETE /tasks/{id}", d.handleCancelTask)
	d.mux.HandleFunc("GET /tasks/{id}/logs", d.handleGetTaskLogs)
	return d
}

// Run starts the daemon and blocks until ctx is cancelled or a fatal error occurs.
// It returns nil on graceful shutdown.
func (d *Daemon) Run(ctx context.Context) error {
	d.startTime = time.Now()

	// Start the worker pool in the background.
	go d.workerPool.Run(ctx)

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
	activeTasks := d.queue.List(false)
	resp := healthResponse{
		Status:        "ok",
		UptimeSec:     int64(time.Since(d.startTime).Seconds()),
		WorkersActive: d.workerPool.Active(),
		QueueDepth:    len(activeTasks),
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp) //nolint:errcheck
}

// submitTaskRequest is the JSON body for POST /tasks.
type submitTaskRequest struct {
	Prompt      string `json:"prompt"`
	ReplyTo     string `json:"reply_to"`
	Priority    int    `json:"priority"`
	AllowWrites bool   `json:"allow_writes"`
	TimeoutSec  int    `json:"timeout_sec"`
}

func (d *Daemon) handleSubmitTask(w http.ResponseWriter, r *http.Request) {
	var req submitTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid JSON body"}`, http.StatusBadRequest)
		return
	}
	if req.Prompt == "" {
		http.Error(w, `{"error":"prompt is required"}`, http.StatusBadRequest)
		return
	}
	if req.TimeoutSec == 0 {
		req.TimeoutSec = d.cfg.DefaultTimeoutSec
	}

	task, err := d.queue.Submit(req.Prompt, req.ReplyTo, req.Priority, req.AllowWrites, req.TimeoutSec)
	if err != nil {
		http.Error(w, `{"error":"failed to submit task"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(task) //nolint:errcheck
}

func (d *Daemon) handleListTasks(w http.ResponseWriter, r *http.Request) {
	all := r.URL.Query().Get("all") == "true"
	tasks := d.queue.List(all)
	if tasks == nil {
		tasks = []*Task{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tasks) //nolint:errcheck
}

func (d *Daemon) handleGetTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	task, err := d.queue.Get(id)
	if err != nil {
		http.Error(w, `{"error":"task not found"}`, http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(task) //nolint:errcheck
}

func (d *Daemon) handleCancelTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := d.queue.Cancel(id); err != nil {
		http.Error(w, `{"error":"task not found"}`, http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNoContent)
}

func (d *Daemon) handleGetTaskLogs(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	// Sanitize: reject IDs containing path separators.
	if strings.ContainsAny(id, "/\\") {
		http.Error(w, `{"error":"invalid task id"}`, http.StatusBadRequest)
		return
	}

	logPath := filepath.Join(d.gobrrDir, "logs", id+".log")
	data, err := os.ReadFile(logPath)
	if err != nil {
		if os.IsNotExist(err) {
			http.Error(w, `{"error":"log not found"}`, http.StatusNotFound)
			return
		}
		http.Error(w, `{"error":"failed to read log"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write(data) //nolint:errcheck
}
