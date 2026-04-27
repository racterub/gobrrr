package daemon

import (
	"encoding/json"
	"net/http"
)

func (d *Daemon) handleSessionStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if d.session == nil {
		json.NewEncoder(w).Encode(map[string]any{"enabled": false}) //nolint:errcheck
		return
	}
	pid, uptime, memMB, idle := d.session.Status()
	json.NewEncoder(w).Encode(map[string]any{
		"enabled": true,
		"running": d.session.Running(),
		"pid":     pid,
		"uptime":  uptime.String(),
		"mem_mb":  memMB,
		"idle":    idle.String(),
	}) //nolint:errcheck
}

func (d *Daemon) handleSessionStart(w http.ResponseWriter, r *http.Request) {
	if d.session == nil {
		http.Error(w, `{"error":"session not configured"}`, http.StatusBadRequest)
		return
	}
	if d.session.Running() {
		http.Error(w, `{"error":"session already running"}`, http.StatusConflict)
		return
	}
	d.session.Start(d.ctx)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "starting"}) //nolint:errcheck
}

func (d *Daemon) handleSessionStop(w http.ResponseWriter, r *http.Request) {
	if d.session == nil {
		http.Error(w, `{"error":"session not configured"}`, http.StatusBadRequest)
		return
	}
	d.session.Stop()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "stopped"}) //nolint:errcheck
}

func (d *Daemon) handleSessionRestart(w http.ResponseWriter, r *http.Request) {
	if d.session == nil {
		http.Error(w, `{"error":"session not configured"}`, http.StatusBadRequest)
		return
	}
	d.session.Stop()
	d.session.Start(d.ctx)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "restarting"}) //nolint:errcheck
}
