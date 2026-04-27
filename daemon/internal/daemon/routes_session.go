package daemon

import (
	"net/http"
)

func (d *Daemon) handleSessionStatus(w http.ResponseWriter, r *http.Request) {
	if d.session == nil {
		respondJSON(w, http.StatusOK, map[string]any{"enabled": false})
		return
	}
	pid, uptime, memMB, idle := d.session.Status()
	respondJSON(w, http.StatusOK, map[string]any{
		"enabled": true,
		"running": d.session.Running(),
		"pid":     pid,
		"uptime":  uptime.String(),
		"mem_mb":  memMB,
		"idle":    idle.String(),
	})
}

func (d *Daemon) handleSessionStart(w http.ResponseWriter, r *http.Request) {
	if d.session == nil {
		respondError(w, http.StatusBadRequest, "session not configured")
		return
	}
	if d.session.Running() {
		respondError(w, http.StatusConflict, "session already running")
		return
	}
	d.session.Start(d.ctx)
	respondJSON(w, http.StatusOK, map[string]string{"status": "starting"})
}

func (d *Daemon) handleSessionStop(w http.ResponseWriter, r *http.Request) {
	if d.session == nil {
		respondError(w, http.StatusBadRequest, "session not configured")
		return
	}
	d.session.Stop()
	respondJSON(w, http.StatusOK, map[string]string{"status": "stopped"})
}

func (d *Daemon) handleSessionRestart(w http.ResponseWriter, r *http.Request) {
	if d.session == nil {
		respondError(w, http.StatusBadRequest, "session not configured")
		return
	}
	d.session.Stop()
	d.session.Start(d.ctx)
	respondJSON(w, http.StatusOK, map[string]string{"status": "restarting"})
}
