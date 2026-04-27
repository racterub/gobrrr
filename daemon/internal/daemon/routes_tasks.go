package daemon

import (
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
	if err := decodeJSON(r, &req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Prompt == "" {
		respondError(w, http.StatusBadRequest, "prompt is required")
		return
	}
	if req.TimeoutSec == 0 {
		req.TimeoutSec = d.cfg.DefaultTimeoutSec
	}

	task, err := d.queue.Submit(req.Prompt, req.ReplyTo, req.Priority, req.AllowWrites, req.TimeoutSec, req.Warm)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to submit task")
		return
	}

	respondJSON(w, http.StatusCreated, task)
}

func (d *Daemon) handleListTasks(w http.ResponseWriter, r *http.Request) {
	all := r.URL.Query().Get("all") == "true"
	tasks := d.queue.List(all)
	if tasks == nil {
		tasks = []*Task{}
	}

	respondJSON(w, http.StatusOK, tasks)
}

func (d *Daemon) handleGetTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	task, err := d.queue.Get(id)
	if err != nil {
		respondError(w, http.StatusNotFound, "task not found")
		return
	}

	respondJSON(w, http.StatusOK, task)
}

func (d *Daemon) handleCancelTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := d.queue.Cancel(id); err != nil {
		respondError(w, http.StatusNotFound, "task not found")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (d *Daemon) handleGetTaskLogs(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	// Sanitize: reject IDs containing path separators.
	if strings.ContainsAny(id, "/\\") {
		respondError(w, http.StatusBadRequest, "invalid task id")
		return
	}

	logPath := filepath.Join(d.gobrrDir, "logs", id+".log")
	data, err := os.ReadFile(logPath)
	if err != nil {
		if os.IsNotExist(err) {
			respondError(w, http.StatusNotFound, "log not found")
			return
		}
		respondError(w, http.StatusInternalServerError, "failed to read log")
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write(data) //nolint:errcheck
}
