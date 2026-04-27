package daemon

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// submitTaskRequest is the JSON body for POST /tasks.
type submitTaskRequest struct {
	Prompt      string `json:"prompt"`
	ReplyTo     string `json:"reply_to"`
	Priority    int    `json:"priority"`
	AllowWrites bool   `json:"allow_writes"`
	TimeoutSec  int    `json:"timeout_sec"`
	// Warm, when true, requests warm-pool dispatch. This is a hint, not a guarantee —
	// if no warm worker is free, the task falls through to cold spawn.
	Warm bool `json:"warm"`
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

	task, err := d.queue.Submit(req.Prompt, req.ReplyTo, req.Priority, req.AllowWrites, req.TimeoutSec, req.Warm)
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
