package daemon_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/racterub/gobrrr/internal/daemon"
	"github.com/stretchr/testify/assert"
)

func TestPruneLogsRemovesOldFiles(t *testing.T) {
	dir := t.TempDir()
	// Create old log file (8 days ago)
	oldLog := filepath.Join(dir, "t_old.log")
	os.WriteFile(oldLog, []byte("old"), 0644) //nolint:errcheck
	oldTime := time.Now().AddDate(0, 0, -8)
	os.Chtimes(oldLog, oldTime, oldTime) //nolint:errcheck
	// Create recent log file
	newLog := filepath.Join(dir, "t_new.log")
	os.WriteFile(newLog, []byte("new"), 0644) //nolint:errcheck

	pruned := daemon.PruneLogs(dir, 7)
	assert.Equal(t, 1, pruned)
	assert.NoFileExists(t, oldLog)
	assert.FileExists(t, newLog)
}

func TestPruneLogsEmptyDir(t *testing.T) {
	dir := t.TempDir()
	pruned := daemon.PruneLogs(dir, 7)
	assert.Equal(t, 0, pruned)
}

func TestPruneLogsMissingDir(t *testing.T) {
	pruned := daemon.PruneLogs("/nonexistent/dir", 7)
	assert.Equal(t, 0, pruned)
}
