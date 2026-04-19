package daemon

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
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

// pendingRequest is the minimal shape of a skill install-request JSON file
// needed for expiry checks.
type pendingRequest struct {
	RequestID  string    `json:"request_id"`
	ExpiresAt  time.Time `json:"expires_at"`
	StagingDir string    `json:"staging_dir"`
}

// PruneExpiredInstallRequests removes expired skill install requests and their
// staging directories. Safe to call concurrently with the installer.
func PruneExpiredInstallRequests(skillsRoot string) error {
	reqDir := filepath.Join(skillsRoot, "_requests")
	entries, err := os.ReadDir(reqDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	now := time.Now().UTC()
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		path := filepath.Join(reqDir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			log.Printf("maintenance: read install request %s: %v", e.Name(), err)
			continue
		}
		var req pendingRequest
		if err := json.Unmarshal(data, &req); err != nil {
			log.Printf("maintenance: skip malformed install request %s: %v", e.Name(), err)
			continue
		}
		if req.ExpiresAt.IsZero() || req.ExpiresAt.After(now) {
			continue
		}
		if err := os.Remove(path); err != nil {
			log.Printf("maintenance: remove expired install request %s: %v", e.Name(), err)
			continue
		}
		if req.StagingDir != "" {
			cleanRoot := filepath.Clean(skillsRoot) + string(os.PathSeparator)
			cleanDir := filepath.Clean(req.StagingDir) + string(os.PathSeparator)
			if strings.HasPrefix(cleanDir, cleanRoot) {
				_ = os.RemoveAll(req.StagingDir)
			}
		}
	}
	return nil
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
			PruneExpiredInstallRequests(d.skillsRoot)
		}
	}
}
