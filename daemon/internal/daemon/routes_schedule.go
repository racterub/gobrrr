package daemon

import (
	"encoding/json"
	"fmt"
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
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid JSON body"}`, http.StatusBadRequest)
		return
	}
	sched, err := d.scheduler.Create(req.Name, req.Cron, req.Prompt, req.ReplyTo, req.AllowWrites)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(sched) //nolint:errcheck
}

func (d *Daemon) handleListSchedules(w http.ResponseWriter, r *http.Request) {
	schedules := d.scheduler.List()
	if schedules == nil {
		schedules = []*scheduler.Schedule{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(schedules) //nolint:errcheck
}

func (d *Daemon) handleRemoveSchedule(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if err := d.scheduler.Remove(name); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "removed"}) //nolint:errcheck
}
