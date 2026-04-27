package daemon

import (
	"net/http"

	"github.com/racterub/gobrrr/internal/scheduler"
)

func (d *Daemon) handleCreateSchedule(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        string `json:"name"`
		Cron        string `json:"cron"`
		Prompt      string `json:"prompt"`
		ReplyTo     string `json:"reply_to"`
		AllowWrites bool   `json:"allow_writes"`
	}
	if err := decodeJSON(r, &req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	sched, err := d.scheduler.Create(req.Name, req.Cron, req.Prompt, req.ReplyTo, req.AllowWrites)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, http.StatusCreated, sched)
}

func (d *Daemon) handleListSchedules(w http.ResponseWriter, r *http.Request) {
	schedules := d.scheduler.List()
	if schedules == nil {
		schedules = []*scheduler.Schedule{}
	}
	respondJSON(w, http.StatusOK, schedules)
}

func (d *Daemon) handleRemoveSchedule(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if err := d.scheduler.Remove(name); err != nil {
		respondError(w, http.StatusNotFound, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"status": "removed"})
}
