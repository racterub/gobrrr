package daemon_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/racterub/gobrrr/internal/daemon"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestMaintenance_ExpiresOldInstallRequests(t *testing.T) {
	skillsRoot := t.TempDir()
	reqDir := filepath.Join(skillsRoot, "_requests")
	require.NoError(t, os.MkdirAll(reqDir, 0700))

	// Fresh request (expires tomorrow).
	fresh := filepath.Join(reqDir, "fresh.json")
	require.NoError(t, os.WriteFile(fresh, []byte(`{"request_id":"fresh","expires_at":"2030-01-01T00:00:00Z","staging_dir":""}`), 0600))

	// Expired request + staging dir.
	stale := filepath.Join(reqDir, "stale.json")
	staleStaging := filepath.Join(reqDir, "stale_staging")
	require.NoError(t, os.MkdirAll(staleStaging, 0700))
	require.NoError(t, os.WriteFile(stale, []byte(`{"request_id":"stale","expires_at":"2020-01-01T00:00:00Z","staging_dir":"`+staleStaging+`"}`), 0600))

	require.NoError(t, daemon.PruneExpiredInstallRequests(skillsRoot))

	_, err := os.Stat(fresh)
	assert.NoError(t, err, "fresh request should survive")
	_, err = os.Stat(stale)
	assert.True(t, os.IsNotExist(err), "stale request should be removed")
	_, err = os.Stat(staleStaging)
	assert.True(t, os.IsNotExist(err), "stale staging dir should be removed")
}

func TestMaintenance_SkipsMalformedInstallRequest(t *testing.T) {
	skillsRoot := t.TempDir()
	reqDir := filepath.Join(skillsRoot, "_requests")
	require.NoError(t, os.MkdirAll(reqDir, 0700))

	bad := filepath.Join(reqDir, "bad.json")
	require.NoError(t, os.WriteFile(bad, []byte("not json"), 0600))

	require.NoError(t, daemon.PruneExpiredInstallRequests(skillsRoot))

	_, err := os.Stat(bad)
	assert.NoError(t, err, "malformed file should be left alone")
}
