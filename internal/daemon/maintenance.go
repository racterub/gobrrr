package daemon

import (
	"context"
	"os"
	"path/filepath"
	"time"
)

// PruneLogs removes log files older than retentionDays from logsDir.
// Returns the number of files removed.
func PruneLogs(logsDir string, retentionDays int) int {
	entries, err := os.ReadDir(logsDir)
	if err != nil {
		return 0
	}

	cutoff := time.Now().AddDate(0, 0, -retentionDays)
	removed := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			if err := os.Remove(filepath.Join(logsDir, entry.Name())); err == nil {
				removed++
			}
		}
	}
	return removed
}

// runMaintenance runs hourly: prune logs and prune queue.
func (d *Daemon) runMaintenance(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			logsDir := filepath.Join(d.gobrrDir, "logs")
			PruneLogs(logsDir, d.cfg.LogRetentionDays)
			d.queue.Prune(d.cfg.LogRetentionDays)
		}
	}
}
