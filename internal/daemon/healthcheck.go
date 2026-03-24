package daemon

import (
	"fmt"
	"time"
)

// HealthChecker inspects the queue for signs of unhealthiness.
type HealthChecker struct {
	queue *Queue
}

// NewHealthChecker creates a new HealthChecker backed by the given queue.
func NewHealthChecker(queue *Queue) *HealthChecker {
	return &HealthChecker{queue: queue}
}

// Check evaluates the health of the daemon based on the queue state.
// Returns (true, "") if healthy, or (false, reason) if unhealthy.
//
// Unhealthy conditions:
//  1. A running task has been running for more than 2x its configured timeout.
//  2. The last 10 completed/failed tasks are all failed.
func (hc *HealthChecker) Check() (healthy bool, reason string) {
	// Check for stuck tasks: running longer than 2x their timeout.
	active := hc.queue.List(false)
	for _, t := range active {
		if t.Status != "running" {
			continue
		}
		if t.StartedAt == nil {
			continue
		}
		maxDuration := time.Duration(t.TimeoutSec*2) * time.Second
		if time.Since(*t.StartedAt) > maxDuration {
			return false, fmt.Sprintf("task %s appears stuck: running for %s (timeout=%ds)", t.ID, time.Since(*t.StartedAt).Round(time.Second), t.TimeoutSec)
		}
	}

	// Check if the last 10 completed tasks are all failures.
	recent := hc.queue.RecentCompleted(10)
	if len(recent) == 0 {
		return true, ""
	}

	allFailed := true
	for _, t := range recent {
		if t.Status != "failed" {
			allFailed = false
			break
		}
	}
	if allFailed && len(recent) >= 10 {
		return false, fmt.Sprintf("last %d tasks all failed", len(recent))
	}

	return true, ""
}
