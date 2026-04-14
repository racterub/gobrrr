package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/racterub/gobrrr/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeMockScript creates a shell script that mimics the stream-json protocol.
// It emits system/init, then loops: read stdin line, emit result.
func writeMockScript(t *testing.T, dir string) string {
	t.Helper()
	script := filepath.Join(dir, "mock-claude.sh")
	content := `#!/bin/bash
echo '{"type":"system","subtype":"init","session_id":"mock-session"}'
while IFS= read -r line; do
  echo '{"type":"result","subtype":"success","result":"mock response","is_error":false,"duration_ms":10}'
done
`
	require.NoError(t, os.WriteFile(script, []byte(content), 0755))
	return script
}

// writeMockIdentity creates a minimal identity.md for testing.
func writeMockIdentity(t *testing.T, dir string) {
	t.Helper()
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "identity.md"),
		[]byte("You are a test assistant."),
		0644,
	))
}

func TestWarmWorkerRun(t *testing.T) {
	dir := t.TempDir()
	script := writeMockScript(t, dir)
	writeMockIdentity(t, dir)

	ww := NewWarmWorker(0, dir, &config.Config{WorkspacePath: dir}, nil)
	ww.command = script

	ctx := t.Context()

	require.NoError(t, ww.Start(ctx))

	task := &Task{ID: "t_test_1", Prompt: "what is 2+2?", TimeoutSec: 10}
	result, err := ww.Run(task)
	require.NoError(t, err)
	assert.Equal(t, "mock response", result)

	// Worker should be available for another task after Run completes.
	// (Run does not manage busy flag — caller does via Reserve/Release.)
	ww.Stop()
}

func TestWarmWorkerRunMultipleTasks(t *testing.T) {
	dir := t.TempDir()
	script := writeMockScript(t, dir)
	writeMockIdentity(t, dir)

	ww := NewWarmWorker(0, dir, &config.Config{WorkspacePath: dir}, nil)
	ww.command = script

	ctx := t.Context()

	require.NoError(t, ww.Start(ctx))

	for i := 0; i < 3; i++ {
		task := &Task{ID: fmt.Sprintf("t_test_%d", i), Prompt: fmt.Sprintf("task %d", i), TimeoutSec: 10}
		result, err := ww.Run(task)
		require.NoError(t, err)
		assert.Equal(t, "mock response", result)
	}

	ww.Stop()
}

func TestWarmWorkerStart(t *testing.T) {
	dir := t.TempDir()
	script := writeMockScript(t, dir)
	writeMockIdentity(t, dir)

	ww := NewWarmWorker(0, dir, &config.Config{WorkspacePath: dir}, nil)
	ww.command = script // override command for testing

	ctx := t.Context()

	err := ww.Start(ctx)
	require.NoError(t, err)
	assert.True(t, ww.Available())

	ww.Stop()
}

// writeCrashScript creates a script that crashes after one task dispatch.
func writeCrashScript(t *testing.T, dir string) string {
	t.Helper()
	script := filepath.Join(dir, "mock-claude-crash.sh")
	content := `#!/bin/bash
echo '{"type":"system","subtype":"init","session_id":"crash-session"}'
# Read identity, send ack
read -r line
echo '{"type":"result","subtype":"success","result":"ready","is_error":false,"duration_ms":1}'
# Read first task, then crash
read -r line
exit 1
`
	require.NoError(t, os.WriteFile(script, []byte(content), 0755))
	return script
}

// writeErrorScript creates a script that returns an error result.
func writeErrorScript(t *testing.T, dir string) string {
	t.Helper()
	script := filepath.Join(dir, "mock-claude-error.sh")
	content := `#!/bin/bash
echo '{"type":"system","subtype":"init","session_id":"error-session"}'
read -r line
echo '{"type":"result","subtype":"success","result":"ready","is_error":false,"duration_ms":1}'
read -r line
echo '{"type":"result","subtype":"error_during_execution","result":"","is_error":true,"errors":["something broke"],"duration_ms":10}'
`
	require.NoError(t, os.WriteFile(script, []byte(content), 0755))
	return script
}

func TestWarmWorkerRunCrash(t *testing.T) {
	dir := t.TempDir()
	script := writeCrashScript(t, dir)
	writeMockIdentity(t, dir)

	ww := NewWarmWorker(0, dir, &config.Config{WorkspacePath: dir}, nil)
	ww.command = script

	ctx := t.Context()

	require.NoError(t, ww.Start(ctx))

	task := &Task{ID: "t_crash", Prompt: "crash me", TimeoutSec: 10}
	_, err := ww.Run(task)
	assert.Error(t, err, "Run should return error on crash")
	assert.Contains(t, err.Error(), "no result message")
}

func TestWarmWorkerRunErrorResult(t *testing.T) {
	dir := t.TempDir()
	script := writeErrorScript(t, dir)
	writeMockIdentity(t, dir)

	ww := NewWarmWorker(0, dir, &config.Config{WorkspacePath: dir}, nil)
	ww.command = script

	ctx := t.Context()

	require.NoError(t, ww.Start(ctx))

	task := &Task{ID: "t_error", Prompt: "fail me", TimeoutSec: 10}
	_, err := ww.Run(task)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "something broke")
}
