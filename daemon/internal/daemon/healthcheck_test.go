package daemon_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/racterub/gobrrr/internal/daemon"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func timePtr(t time.Time) *time.Time {
	return &t
}

func TestHealthCheckerStuckTask(t *testing.T) {
	q := daemon.NewQueue(filepath.Join(t.TempDir(), "q.json"))
	task, err := q.Submit("test", "telegram", 1, false, 1, false) // 1 second timeout
	require.NoError(t, err)

	_, err = q.Next() // mark running
	require.NoError(t, err)

	// Fake the start time to 10 seconds ago (> 2x timeout of 1s)
	task.StartedAt = timePtr(time.Now().Add(-10 * time.Second))
	err = q.Flush()
	require.NoError(t, err)

	hc := daemon.NewHealthChecker(q)
	healthy, reason := hc.Check()
	assert.False(t, healthy)
	assert.Contains(t, reason, "stuck")
}

func TestHealthCheckerAllFailed(t *testing.T) {
	q := daemon.NewQueue(filepath.Join(t.TempDir(), "q.json"))
	for i := 0; i < 10; i++ {
		_, err := q.Submit("test", "telegram", 1, false, 300, false)
		require.NoError(t, err)
		task, err := q.Next()
		require.NoError(t, err)
		err = q.Fail(task.ID, "error")
		require.NoError(t, err)
	}
	hc := daemon.NewHealthChecker(q)
	healthy, _ := hc.Check()
	assert.False(t, healthy)
}

func TestHealthCheckerHealthy(t *testing.T) {
	q := daemon.NewQueue(filepath.Join(t.TempDir(), "q.json"))
	hc := daemon.NewHealthChecker(q)
	healthy, _ := hc.Check()
	assert.True(t, healthy)
}

func TestHealthCheckerPartialFailures(t *testing.T) {
	q := daemon.NewQueue(filepath.Join(t.TempDir(), "q.json"))
	// Submit 9 failed + 1 completed — should be healthy
	for i := 0; i < 9; i++ {
		_, err := q.Submit("test", "telegram", 1, false, 300, false)
		require.NoError(t, err)
		task, err := q.Next()
		require.NoError(t, err)
		err = q.Fail(task.ID, "error")
		require.NoError(t, err)
	}
	// One success
	_, err := q.Submit("test", "telegram", 1, false, 300, false)
	require.NoError(t, err)
	task, err := q.Next()
	require.NoError(t, err)
	err = q.Complete(task.ID, "done")
	require.NoError(t, err)

	hc := daemon.NewHealthChecker(q)
	healthy, _ := hc.Check()
	assert.True(t, healthy)
}
