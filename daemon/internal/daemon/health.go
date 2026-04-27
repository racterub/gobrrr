package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"time"

	"github.com/racterub/gobrrr/internal/config"
)

// runHealthMonitor runs a loop that checks system health every 30 seconds and
// updates the heartbeat with the current status and memory usage.
func (d *Daemon) runHealthMonitor(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			d.updateHeartbeat()
		}
	}
}

// updateHeartbeat checks health and memory usage then calls heartbeat.Update.
func (d *Daemon) updateHeartbeat() {
	healthy, reason := d.healthChecker.Check()

	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	memMB := int(m.Sys / 1024 / 1024)

	status := "up"
	msg := ""
	if !healthy {
		status = "down"
		msg = reason
	} else {
		active := d.queue.List(false)
		running := 0
		queued := 0
		for _, t := range active {
			if t.Status == "running" {
				running++
			} else if t.Status == "queued" {
				queued++
			}
		}
		msg = fmt.Sprintf("%d workers active, %d queued", running, queued)
	}

	d.heartbeat.Update(status, memMB, msg)
}

// warmStatus describes the warm worker pool's capacity and current utilization.
type warmStatus struct {
	Total    int `json:"total"`
	Ready    int `json:"ready"`
	Busy     int `json:"busy"`
	Disabled int `json:"disabled"`
}

// healthResponse is the JSON body returned by GET /health.
type healthResponse struct {
	Status        string              `json:"status"`
	UptimeSec     int64               `json:"uptime_sec"`
	WorkersActive int                 `json:"workers_active"`
	QueueDepth    int                 `json:"queue_depth"`
	WarmWorkers   warmStatus          `json:"warm_workers"`
	Models        config.ModelsConfig `json:"models"`
}

func (d *Daemon) handleHealth(w http.ResponseWriter, r *http.Request) {
	activeTasks := d.queue.List(false)
	total, ready, busy, disabled := d.workerPool.WarmStatus()
	resp := healthResponse{
		Status:        "ok",
		UptimeSec:     int64(time.Since(d.startTime).Seconds()),
		WorkersActive: d.workerPool.Active(),
		QueueDepth:    len(activeTasks),
		WarmWorkers:   warmStatus{Total: total, Ready: ready, Busy: busy, Disabled: disabled},
		Models:        d.cfg.Models,
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp) //nolint:errcheck
}
