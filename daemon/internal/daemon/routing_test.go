package daemon

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/racterub/gobrrr/internal/config"
	"github.com/racterub/gobrrr/internal/telegram"
)

// newTestDaemon creates a minimal Daemon suitable for routing tests.
func newTestDaemon(t *testing.T, notifier *telegram.Notifier) *Daemon {
	t.Helper()
	dir := t.TempDir()
	queuePath := filepath.Join(dir, "queue.json")
	q := NewQueue(queuePath)
	wp := NewWorkerPool(q, &config.Config{WorkspacePath: dir}, 1, 0, dir, nil, nil)
	return &Daemon{
		gobrrDir:   dir,
		queue:      q,
		workerPool: wp,
		notifier:   notifier,
		sseHub:     NewSSEHub(),
	}
}

func TestRouteToStdout(t *testing.T) {
	d := newTestDaemon(t, nil)
	task := &Task{ID: "t_1", ReplyTo: "stdout", Status: "completed"}
	err := d.routeResult(task, "some result")
	require.NoError(t, err)
}

func TestRouteEmptyReplyTo(t *testing.T) {
	d := newTestDaemon(t, nil)
	task := &Task{ID: "t_1", ReplyTo: "", Status: "completed"}
	err := d.routeResult(task, "some result")
	require.NoError(t, err)
}

func TestRouteToTelegram(t *testing.T) {
	var received string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received = r.URL.Path
		w.Write([]byte(`{"ok":true}`)) //nolint:errcheck
	}))
	defer server.Close()

	notifier := telegram.NewNotifier("123:TOKEN", "chat123", telegram.WithBaseURL(server.URL))
	d := newTestDaemon(t, notifier)
	task := &Task{ID: "t_1", ReplyTo: "telegram", Status: "completed"}

	err := d.routeResult(task, "Task done!")
	require.NoError(t, err)
	assert.Contains(t, received, "/sendMessage")
}

func TestRouteToTelegramNotConfigured(t *testing.T) {
	d := newTestDaemon(t, nil)
	task := &Task{ID: "t_1", ReplyTo: "telegram", Status: "completed"}

	err := d.routeResult(task, "result")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "telegram not configured")
}

func TestRouteToFile(t *testing.T) {
	dir := t.TempDir()
	// Set GOBRRR_DIR so the allowed prefix matches our temp dir.
	t.Setenv("GOBRRR_DIR", dir)

	d := newTestDaemon(t, nil)
	outPath := filepath.Join(dir, "output", "test.txt")
	task := &Task{ID: "t_1", ReplyTo: "file:" + outPath}

	err := d.routeResult(task, "hello file")
	require.NoError(t, err)

	data, err := os.ReadFile(outPath)
	require.NoError(t, err)
	assert.Equal(t, "hello file", string(data))
}

func TestRouteToFileBlocked(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GOBRRR_DIR", dir)

	d := newTestDaemon(t, nil)
	task := &Task{ID: "t_1", ReplyTo: "file:/etc/passwd"}

	err := d.routeResult(task, "data")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not in allowed directories")
}

func TestRouteToFileSymlinkEscape(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GOBRRR_DIR", dir)

	// Create the allowed output directory.
	outputDir := filepath.Join(dir, "output")
	require.NoError(t, os.MkdirAll(outputDir, 0700))

	// Create a target directory outside the allowed prefix.
	outsideDir := t.TempDir()
	outsideFile := filepath.Join(outsideDir, "secret.txt")

	// Create a symlink inside the allowed dir that points outside.
	symlinkDir := filepath.Join(outputDir, "escape")
	require.NoError(t, os.Symlink(outsideDir, symlinkDir))

	// Attempt to write via the symlink.
	d := newTestDaemon(t, nil)
	task := &Task{ID: "t_1", ReplyTo: "file:" + filepath.Join(symlinkDir, "secret.txt")}
	err := d.routeResult(task, "escaped data")

	// The file must not have been written regardless of error path.
	_, statErr := os.Stat(outsideFile)
	assert.True(t, os.IsNotExist(statErr), "file outside allowed dir must not be created")
	// Should also return an error.
	require.Error(t, err)
}

func TestRouteUnknownReplyTo(t *testing.T) {
	d := newTestDaemon(t, nil)
	task := &Task{ID: "t_1", ReplyTo: "slack"}

	err := d.routeResult(task, "result")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown reply_to")
}

func TestRouteToTelegramQuarantinesLeak(t *testing.T) {
	var received string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Capture the raw request body text so we can verify the warning message.
		buf := make([]byte, 4096)
		n, _ := r.Body.Read(buf)
		received = string(buf[:n])
		w.Write([]byte(`{"ok":true}`)) //nolint:errcheck
	}))
	defer server.Close()

	notifier := telegram.NewNotifier("123:TOKEN", "chat123", telegram.WithBaseURL(server.URL))
	d := newTestDaemon(t, notifier)
	task := &Task{ID: "t_leak", ReplyTo: "telegram", Status: "completed"}

	// A result containing a Bearer token — should be quarantined.
	leakyResult := "Authorization: Bearer eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.long.token"
	err := d.routeResult(task, leakyResult)
	require.NoError(t, err)

	// The telegram message must be the warning, not the leaked content.
	assert.Contains(t, received, "quarantined")
	assert.NotContains(t, received, "eyJhbGciOi")

	// The quarantined result must have been written to the task log.
	logPath := filepath.Join(d.gobrrDir, "logs", "t_leak.log")
	logData, err := os.ReadFile(logPath)
	require.NoError(t, err)
	assert.Contains(t, string(logData), "QUARANTINED RESULT")
	assert.Contains(t, string(logData), leakyResult)
}

func TestRouteResultMultiDestination(t *testing.T) {
	d := newTestDaemon(t, nil)
	t.Setenv("GOBRRR_DIR", d.gobrrDir)

	outPath := filepath.Join(d.gobrrDir, "output", "result.txt")
	task := &Task{
		ID:      "t_multi",
		ReplyTo: "file:" + outPath + ",stdout",
		Status:  "completed",
	}
	result := "multi-destination result"

	err := d.routeResult(task, result)
	require.NoError(t, err)

	content, err := os.ReadFile(outPath)
	require.NoError(t, err)
	assert.Equal(t, result, string(content))

	assert.NotNil(t, task.Result)
	assert.Equal(t, result, *task.Result)
}

func TestRouteToTelegramCleanOutputNotQuarantined(t *testing.T) {
	var received string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 4096)
		n, _ := r.Body.Read(buf)
		received = string(buf[:n])
		w.Write([]byte(`{"ok":true}`)) //nolint:errcheck
	}))
	defer server.Close()

	notifier := telegram.NewNotifier("123:TOKEN", "chat123", telegram.WithBaseURL(server.URL))
	d := newTestDaemon(t, notifier)
	task := &Task{ID: "t_clean", ReplyTo: "telegram", Status: "completed"}

	cleanResult := "You have 3 meetings today. The standup is at 10am."
	err := d.routeResult(task, cleanResult)
	require.NoError(t, err)

	assert.Contains(t, received, "3 meetings today")
	assert.NotContains(t, received, "quarantined")
}

func TestSSEIntegration(t *testing.T) {
	d := newTestDaemon(t, nil)

	// Subscribe to SSE events
	ch := d.sseHub.Subscribe()
	defer d.sseHub.Unsubscribe(ch)

	task := &Task{
		ID:        "t_sse_test",
		Prompt:    "test prompt for SSE",
		ReplyTo:   "channel",
		Status:    "completed",
		CreatedAt: time.Now().UTC(),
	}

	err := d.routeResult(task, "SSE test result")
	require.NoError(t, err)

	select {
	case event := <-ch:
		assert.Equal(t, "t_sse_test", event.TaskID)
		assert.Equal(t, "completed", event.Status)
		assert.Equal(t, "SSE test result", event.Result)
		assert.Equal(t, "test prompt for SSE", event.PromptSummary)
	case <-time.After(time.Second):
		t.Fatal("did not receive SSE event")
	}
}
