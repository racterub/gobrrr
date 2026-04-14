package daemon_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/racterub/gobrrr/internal/daemon"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSubmitTaskGetsQueuedStatus verifies that a newly submitted task gets
// "queued" status and a generated ID.
func TestSubmitTaskGetsQueuedStatus(t *testing.T) {
	q := daemon.NewQueue(filepath.Join(t.TempDir(), "queue.json"))

	task, err := q.Submit("do something", "telegram", 5, false, 300, false)
	require.NoError(t, err)

	assert.NotEmpty(t, task.ID)
	assert.True(t, strings.HasPrefix(task.ID, "t_"), "ID should start with t_")
	assert.Equal(t, "queued", task.Status)
	assert.Equal(t, "do something", task.Prompt)
	assert.Equal(t, "telegram", task.ReplyTo)
	assert.Equal(t, 5, task.Priority)
	assert.False(t, task.AllowWrites)
	assert.Equal(t, 300, task.TimeoutSec)
	assert.WithinDuration(t, time.Now(), task.CreatedAt, 5*time.Second)
	assert.Nil(t, task.StartedAt)
	assert.Nil(t, task.CompletedAt)
}

// TestListDefaultReturnsActiveOnly verifies that List returns only
// queued/running tasks by default (all=false).
func TestListDefaultReturnsActiveOnly(t *testing.T) {
	q := daemon.NewQueue(filepath.Join(t.TempDir(), "queue.json"))

	t1, err := q.Submit("task 1", "", 5, false, 300, false)
	require.NoError(t, err)
	t2, err := q.Submit("task 2", "", 5, false, 300, false)
	require.NoError(t, err)

	// Complete t1
	_, err = q.Next()
	require.NoError(t, err)
	err = q.Complete(t1.ID, "done")
	require.NoError(t, err)

	tasks := q.List(false)
	assert.Len(t, tasks, 1)
	assert.Equal(t, t2.ID, tasks[0].ID)
}

// TestListAllIncludesCompleted verifies that List with all=true includes
// completed and failed tasks.
func TestListAllIncludesCompleted(t *testing.T) {
	q := daemon.NewQueue(filepath.Join(t.TempDir(), "queue.json"))

	t1, err := q.Submit("task 1", "", 5, false, 300, false)
	require.NoError(t, err)
	t2, err := q.Submit("task 2", "", 5, false, 300, false)
	require.NoError(t, err)

	// Complete t1 and fail t2
	_, err = q.Next()
	require.NoError(t, err)
	err = q.Complete(t1.ID, "result")
	require.NoError(t, err)

	_, err = q.Next()
	require.NoError(t, err)
	err = q.Fail(t2.ID, "some error")
	require.NoError(t, err)

	tasks := q.List(true)
	assert.Len(t, tasks, 2)
}

// TestNextReturnsPriorityOrder verifies that Next returns the task with the
// lowest priority value (highest priority) first.
func TestNextReturnsPriorityOrder(t *testing.T) {
	q := daemon.NewQueue(filepath.Join(t.TempDir(), "queue.json"))

	// Priority 10 = lower priority (higher number = lower priority)
	_, err := q.Submit("low priority", "", 10, false, 300, false)
	require.NoError(t, err)

	// Priority 1 = highest priority
	high, err := q.Submit("high priority", "", 1, false, 300, false)
	require.NoError(t, err)

	next, err := q.Next()
	require.NoError(t, err)
	require.NotNil(t, next)
	assert.Equal(t, high.ID, next.ID)
	assert.Equal(t, "running", next.Status)
}

// TestNextReturnsFIFOForEqualPriority verifies that among tasks with the same
// priority, the oldest is returned first.
func TestNextReturnsFIFOForEqualPriority(t *testing.T) {
	q := daemon.NewQueue(filepath.Join(t.TempDir(), "queue.json"))

	first, err := q.Submit("first", "", 5, false, 300, false)
	require.NoError(t, err)
	// Ensure different timestamps
	time.Sleep(2 * time.Millisecond)
	_, err = q.Submit("second", "", 5, false, 300, false)
	require.NoError(t, err)

	next, err := q.Next()
	require.NoError(t, err)
	require.NotNil(t, next)
	assert.Equal(t, first.ID, next.ID)
}

// TestNextReturnsNilWhenQueueEmpty verifies that Next returns nil when there
// are no queued tasks.
func TestNextReturnsNilWhenQueueEmpty(t *testing.T) {
	q := daemon.NewQueue(filepath.Join(t.TempDir(), "queue.json"))

	next, err := q.Next()
	require.NoError(t, err)
	assert.Nil(t, next)
}

// TestCompleteUpdatesStatusAndTimestamp verifies that Complete sets status to
// "completed", stores the result, and records CompletedAt.
func TestCompleteUpdatesStatusAndTimestamp(t *testing.T) {
	q := daemon.NewQueue(filepath.Join(t.TempDir(), "queue.json"))

	task, err := q.Submit("work", "", 5, false, 300, false)
	require.NoError(t, err)

	_, err = q.Next()
	require.NoError(t, err)

	err = q.Complete(task.ID, "great result")
	require.NoError(t, err)

	got, err := q.Get(task.ID)
	require.NoError(t, err)
	assert.Equal(t, "completed", got.Status)
	assert.Equal(t, "great result", *got.Result)
	assert.NotNil(t, got.CompletedAt)
	assert.NotNil(t, got.StartedAt)
}

// TestFailUpdatesStatusAndTimestamp verifies that Fail sets status to "failed",
// stores the error message, and records CompletedAt.
func TestFailUpdatesStatusAndTimestamp(t *testing.T) {
	q := daemon.NewQueue(filepath.Join(t.TempDir(), "queue.json"))

	task, err := q.Submit("work", "", 5, false, 300, false)
	require.NoError(t, err)

	_, err = q.Next()
	require.NoError(t, err)

	err = q.Fail(task.ID, "exploded")
	require.NoError(t, err)

	got, err := q.Get(task.ID)
	require.NoError(t, err)
	assert.Equal(t, "failed", got.Status)
	assert.Equal(t, "exploded", *got.Error)
	assert.NotNil(t, got.CompletedAt)
}

// TestPersistAndReload verifies that tasks survive a queue reload from disk.
func TestPersistAndReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "queue.json")
	q := daemon.NewQueue(path)

	task, err := q.Submit("persisted task", "telegram", 3, true, 120, false)
	require.NoError(t, err)

	q2, err := daemon.LoadQueue(path)
	require.NoError(t, err)

	got, err := q2.Get(task.ID)
	require.NoError(t, err)
	assert.Equal(t, task.ID, got.ID)
	assert.Equal(t, "persisted task", got.Prompt)
	assert.Equal(t, "telegram", got.ReplyTo)
	assert.Equal(t, 3, got.Priority)
	assert.True(t, got.AllowWrites)
	assert.Equal(t, 120, got.TimeoutSec)
}

// TestAtomicWrite verifies that Flush writes to a tmp file then renames.
// We verify indirectly: the queue.json file should exist after Submit, and
// no queue.json.tmp should remain.
func TestAtomicWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "queue.json")
	q := daemon.NewQueue(path)

	_, err := q.Submit("task", "", 5, false, 300, false)
	require.NoError(t, err)

	_, statErr := os.Stat(path)
	assert.NoError(t, statErr, "queue.json should exist after submit")

	_, tmpErr := os.Stat(path + ".tmp")
	assert.True(t, os.IsNotExist(tmpErr), "queue.json.tmp should not remain after flush")
}

// TestCrashRecoveryResetsRunningToQueued verifies that tasks stuck in "running"
// are reset to "queued" when the queue is loaded from disk.
func TestCrashRecoveryResetsRunningToQueued(t *testing.T) {
	path := filepath.Join(t.TempDir(), "queue.json")
	q := daemon.NewQueue(path)

	task, err := q.Submit("crash me", "", 5, false, 300, false)
	require.NoError(t, err)

	// Mark as running (simulating worker picked it up before crash)
	_, err = q.Next()
	require.NoError(t, err)

	// Reload without completing → crash recovery
	q2, err := daemon.LoadQueue(path)
	require.NoError(t, err)

	got, err := q2.Get(task.ID)
	require.NoError(t, err)
	assert.Equal(t, "queued", got.Status, "running tasks should be reset to queued on load")
	assert.Nil(t, got.StartedAt, "StartedAt should be cleared on crash recovery")
}

// TestPruneRemovesOldCompleted verifies that Prune removes completed/failed
// tasks older than the retention period and returns the count removed.
func TestPruneRemovesOldCompleted(t *testing.T) {
	q := daemon.NewQueue(filepath.Join(t.TempDir(), "queue.json"))

	t1, err := q.Submit("old task", "", 5, false, 300, false)
	require.NoError(t, err)
	_, err = q.Next()
	require.NoError(t, err)
	err = q.Complete(t1.ID, "done")
	require.NoError(t, err)

	// Manually backdating the CompletedAt is not possible from outside, so we
	// submit a second task (recent) and check that Prune with 0 days removes the
	// first task but we'll use a helper approach: prune with -1 days removes all.
	removed := q.Prune(-1)
	assert.Equal(t, 1, removed)

	tasks := q.List(true)
	assert.Empty(t, tasks)
}

// TestPruneKeepsRecentCompleted verifies that Prune does not remove tasks
// within the retention period.
func TestPruneKeepsRecentCompleted(t *testing.T) {
	q := daemon.NewQueue(filepath.Join(t.TempDir(), "queue.json"))

	t1, err := q.Submit("recent task", "", 5, false, 300, false)
	require.NoError(t, err)
	_, err = q.Next()
	require.NoError(t, err)
	err = q.Complete(t1.ID, "done")
	require.NoError(t, err)

	// 7 day retention — recently completed task should NOT be pruned
	removed := q.Prune(7)
	assert.Equal(t, 0, removed)

	tasks := q.List(true)
	assert.Len(t, tasks, 1)
}

// TestRecentCompleted verifies that RecentCompleted returns the last N
// completed/failed tasks in reverse chronological order.
func TestRecentCompleted(t *testing.T) {
	q := daemon.NewQueue(filepath.Join(t.TempDir(), "queue.json"))

	for i := 0; i < 5; i++ {
		task, err := q.Submit(fmt.Sprintf("task %d", i), "", 5, false, 300, false)
		require.NoError(t, err)
		_, err = q.Next()
		require.NoError(t, err)
		err = q.Complete(task.ID, "done")
		require.NoError(t, err)
		// small delay to ensure ordering
		time.Sleep(time.Millisecond)
	}

	recent := q.RecentCompleted(3)
	assert.Len(t, recent, 3)

	// Should be in reverse chronological order (most recent first)
	assert.True(t, recent[0].CompletedAt.After(*recent[1].CompletedAt))
	assert.True(t, recent[1].CompletedAt.After(*recent[2].CompletedAt))
}

// TestGetReturnsErrorForUnknownID verifies that Get returns an error for an
// unknown task ID.
func TestGetReturnsErrorForUnknownID(t *testing.T) {
	q := daemon.NewQueue(filepath.Join(t.TempDir(), "queue.json"))

	_, err := q.Get("t_nonexistent_123456")
	assert.Error(t, err)
}

// TestTaskDeliveredFieldPersistence verifies that MarkDelivered sets the
// Delivered field and that it survives a reload from disk.
func TestTaskDeliveredFieldPersistence(t *testing.T) {
	dir := t.TempDir()
	q := daemon.NewQueue(filepath.Join(dir, "queue.json"))

	task, err := q.Submit("test prompt", "channel", 0, false, 300, false)
	require.NoError(t, err)

	err = q.Complete(task.ID, "result")
	require.NoError(t, err)

	err = q.MarkDelivered(task.ID)
	require.NoError(t, err)

	q2, err := daemon.LoadQueue(filepath.Join(dir, "queue.json"))
	require.NoError(t, err)

	reloaded, err := q2.Get(task.ID)
	require.NoError(t, err)
	require.NotNil(t, reloaded)
	assert.True(t, reloaded.Delivered)
}

func TestLoadQueueCrashRecoveryFlushesToDisk(t *testing.T) {
	dir := t.TempDir()
	queuePath := filepath.Join(dir, "queue.json")
	q := daemon.NewQueue(queuePath)

	// Submit and start a task (moves to running)
	task, err := q.Submit("crash test", "stdout", 0, false, 300, false)
	require.NoError(t, err)
	next, err := q.Next()
	require.NoError(t, err)
	assert.Equal(t, task.ID, next.ID)
	assert.Equal(t, "running", next.Status)

	// Simulate crash: reload from disk (running task on disk)
	q2, err := daemon.LoadQueue(queuePath)
	require.NoError(t, err)

	recovered, err := q2.Get(task.ID)
	require.NoError(t, err)
	assert.Equal(t, "queued", recovered.Status, "in-memory state should be queued")

	// Key assertion: read raw disk to verify flush happened
	raw, err := os.ReadFile(queuePath)
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"status": "queued"`)
	assert.NotContains(t, string(raw), `"status": "running"`)
}

// TestCancelTask verifies that cancelling a queued task sets its status to
// "cancelled".
func TestCancelTask(t *testing.T) {
	q := daemon.NewQueue(filepath.Join(t.TempDir(), "queue.json"))

	task, err := q.Submit("cancel me", "", 5, false, 300, false)
	require.NoError(t, err)

	err = q.Cancel(task.ID)
	require.NoError(t, err)

	got, err := q.Get(task.ID)
	require.NoError(t, err)
	assert.Equal(t, "cancelled", got.Status)
}

func TestSubmitWarmTask(t *testing.T) {
	q := daemon.NewQueue(filepath.Join(t.TempDir(), "queue.json"))

	task, err := q.Submit("quick lookup", "telegram", 5, false, 300, true)
	require.NoError(t, err)

	assert.True(t, task.Warm, "task should be marked warm")
}
