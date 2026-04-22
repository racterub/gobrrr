package daemon

import (
	"context"
	"log"
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
			if err := PruneExpiredApprovals(d.approvals); err != nil {
				log.Printf("maintenance: prune approvals: %v", err)
			}
		}
	}
}

// PruneExpiredApprovals walks the dispatcher's pending approvals and, for each
// expired entry, synthesizes a "deny" decision. This delegates cleanup to the
// kind's handler (e.g. skill_install's deny handler removes the staged bundle)
// while maintaining a single lifecycle code path.
func PruneExpiredApprovals(d *ApprovalDispatcher) error {
	pending, err := d.List()
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	for _, req := range pending {
		if req.ExpiresAt.IsZero() || req.ExpiresAt.After(now) {
			continue
		}
		_ = d.Decide(req.ID, "deny")
	}
	return nil
}
