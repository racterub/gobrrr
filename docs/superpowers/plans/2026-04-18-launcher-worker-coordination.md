# Launcher/Worker Coordination Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add role-based model and permission-mode assignment (launcher=haiku/default, warm=sonnet/auto, cold=opus/auto) driven by `~/.gobrrr/config.json`, plus stderr capture, anti-flap guard on warm respawn, and health-endpoint visibility.

**Architecture:** Config grows a `models` block. Cold/warm workers read it when building CLI args. Launcher shell script reads it via `jq`. Warm workers drop `--dangerously-skip-permissions` in favor of `--permission-mode auto` + a shared `warm-settings.json` allow-list. Launcher drops `--dangerously-skip-permissions` in favor of a pre-generated `launcher-settings.json` allow-list. Anti-flap stops respawn loops on repeated warm-worker aborts.

**Tech Stack:** Go (daemon, CLI), bash (`launcher.sh`, `install.sh`), `jq`, `expect`, Claude Code CLI.

**Spec:** `docs/superpowers/specs/2026-04-18-launcher-worker-coordination-design.md`

---

## Task 1: Config `Models` Struct and Defaults

**Files:**
- Modify: `daemon/internal/config/config.go`
- Test: `daemon/internal/config/config_test.go`

- [ ] **Step 1: Write the failing test**

Append to `daemon/internal/config/config_test.go`:

```go
func TestDefaultModelsConfig(t *testing.T) {
	cfg := config.Default()

	assert.Equal(t, "haiku", cfg.Models.Launcher.Model)
	assert.Equal(t, "default", cfg.Models.Launcher.PermissionMode)

	assert.Equal(t, "sonnet", cfg.Models.WarmWorker.Model)
	assert.Equal(t, "auto", cfg.Models.WarmWorker.PermissionMode)

	assert.Equal(t, "opus", cfg.Models.ColdWorker.Model)
	assert.Equal(t, "auto", cfg.Models.ColdWorker.PermissionMode)
}

func TestLoadModelsFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	data := []byte(`{
		"models": {
			"launcher":    {"model": "haiku",  "permission_mode": "default"},
			"warm_worker": {"model": "opus",   "permission_mode": "auto"},
			"cold_worker": {"model": "sonnet", "permission_mode": "default"}
		}
	}`)
	require.NoError(t, os.WriteFile(path, data, 0600))

	cfg, err := config.Load(path)
	require.NoError(t, err)

	assert.Equal(t, "opus", cfg.Models.WarmWorker.Model)
	assert.Equal(t, "sonnet", cfg.Models.ColdWorker.Model)
	assert.Equal(t, "default", cfg.Models.ColdWorker.PermissionMode)
}

func TestLoadModelsPreservesDefaultsForMissingFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	// Partial models block: only launcher set.
	data := []byte(`{"models": {"launcher": {"model": "haiku", "permission_mode": "default"}}}`)
	require.NoError(t, os.WriteFile(path, data, 0600))

	cfg, err := config.Load(path)
	require.NoError(t, err)

	assert.Equal(t, "sonnet", cfg.Models.WarmWorker.Model)
	assert.Equal(t, "auto", cfg.Models.WarmWorker.PermissionMode)
	assert.Equal(t, "opus", cfg.Models.ColdWorker.Model)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd daemon && go test ./internal/config/ -run TestDefaultModelsConfig -v`
Expected: FAIL, `cfg.Models` undefined.

- [ ] **Step 3: Add structs, field, defaults, and missing-field fill-in**

Edit `daemon/internal/config/config.go`. Above `type Config struct`, add:

```go
// ModelConfig binds one role to a Claude model and permission mode.
type ModelConfig struct {
	Model          string `json:"model"`
	PermissionMode string `json:"permission_mode"`
}

// ModelsConfig assigns a ModelConfig to each pipeline role.
type ModelsConfig struct {
	Launcher   ModelConfig `json:"launcher"`
	WarmWorker ModelConfig `json:"warm_worker"`
	ColdWorker ModelConfig `json:"cold_worker"`
}
```

Add a `Models` field to `Config`:

```go
type Config struct {
	// ... existing fields ...
	TelegramSession TelegramSessionConfig `json:"telegram_session"`
	Models          ModelsConfig          `json:"models"`
}
```

In `Default()`, append to the returned struct literal:

```go
return &Config{
	// ... existing fields ...
	Models: ModelsConfig{
		Launcher:   ModelConfig{Model: "haiku", PermissionMode: "default"},
		WarmWorker: ModelConfig{Model: "sonnet", PermissionMode: "auto"},
		ColdWorker: ModelConfig{Model: "opus", PermissionMode: "auto"},
	},
}
```

Add a defaults-merger modelled on `applyTelegramSessionDefaults`. Place it directly below that function:

```go
// applyModelsDefaults fills zero-value ModelConfig fields with defaults.
// json.Unmarshal replaces nested structs with missing fields zeroed, so
// defaults must be reapplied after unmarshal.
func applyModelsDefaults(cfg *Config) {
	d := Default().Models
	m := &cfg.Models

	if m.Launcher.Model == "" {
		m.Launcher.Model = d.Launcher.Model
	}
	if m.Launcher.PermissionMode == "" {
		m.Launcher.PermissionMode = d.Launcher.PermissionMode
	}
	if m.WarmWorker.Model == "" {
		m.WarmWorker.Model = d.WarmWorker.Model
	}
	if m.WarmWorker.PermissionMode == "" {
		m.WarmWorker.PermissionMode = d.WarmWorker.PermissionMode
	}
	if m.ColdWorker.Model == "" {
		m.ColdWorker.Model = d.ColdWorker.Model
	}
	if m.ColdWorker.PermissionMode == "" {
		m.ColdWorker.PermissionMode = d.ColdWorker.PermissionMode
	}
}
```

Call it from `Load()` directly after `applyTelegramSessionDefaults(cfg)`:

```go
applyTelegramSessionDefaults(cfg)
applyModelsDefaults(cfg)

return cfg, nil
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd daemon && go test ./internal/config/ -v`
Expected: PASS, including the three new tests.

- [ ] **Step 5: Commit**

```bash
git add daemon/internal/config/config.go daemon/internal/config/config_test.go
git commit -m "feat(config): add Models block with role-based defaults"
```

---

## Task 2: Config Validation

**Files:**
- Modify: `daemon/internal/config/config.go`
- Test: `daemon/internal/config/config_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `daemon/internal/config/config_test.go`:

```go
func TestLoadRejectsUnknownPermissionMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	data := []byte(`{"models": {"warm_worker": {"model": "sonnet", "permission_mode": "nonsense"}}}`)
	require.NoError(t, os.WriteFile(path, data, 0600))

	_, err := config.Load(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nonsense")
}

func TestLoadFallsBackWhenLauncherHaikuAuto(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	// haiku + auto is rejected by Claude. Loader downgrades to default.
	data := []byte(`{"models": {"launcher": {"model": "haiku", "permission_mode": "auto"}}}`)
	require.NoError(t, os.WriteFile(path, data, 0600))

	cfg, err := config.Load(path)
	require.NoError(t, err)
	assert.Equal(t, "default", cfg.Models.Launcher.PermissionMode)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd daemon && go test ./internal/config/ -run 'TestLoadRejectsUnknownPermissionMode|TestLoadFallsBackWhenLauncherHaikuAuto' -v`
Expected: FAIL, first test gets no error, second test's launcher mode still `auto`.

- [ ] **Step 3: Implement validation**

Edit `daemon/internal/config/config.go`. Above `applyModelsDefaults`, add:

```go
// validPermissionModes are the permission modes Claude Code accepts today.
// Kept narrow on purpose — catches config typos fail-fast. Extend if Claude adds more.
var validPermissionModes = map[string]struct{}{
	"default":           {},
	"acceptEdits":       {},
	"plan":              {},
	"auto":              {},
	"bypassPermissions": {},
}

// validateAndFixModels returns an error for unknown permission modes.
// Downgrades invalid role-mode combinations (haiku+auto) to safe defaults
// and logs a warning to stderr.
func validateAndFixModels(cfg *Config) error {
	roles := []struct {
		name string
		cfg  *ModelConfig
	}{
		{"launcher", &cfg.Models.Launcher},
		{"warm_worker", &cfg.Models.WarmWorker},
		{"cold_worker", &cfg.Models.ColdWorker},
	}
	for _, r := range roles {
		if _, ok := validPermissionModes[r.cfg.PermissionMode]; !ok {
			return fmt.Errorf("models.%s.permission_mode: unknown value %q", r.name, r.cfg.PermissionMode)
		}
		if r.cfg.Model == "haiku" && r.cfg.PermissionMode == "auto" {
			fmt.Fprintf(os.Stderr, "warning: models.%s haiku+auto not supported by Claude — falling back to default\n", r.name)
			r.cfg.PermissionMode = "default"
		}
	}
	return nil
}
```

Add `"fmt"` to the imports if not already present (it is — `os` and `encoding/json` import list at top already has most of what's needed; verify `fmt` is present, add if not).

Call it from `Load()` after `applyModelsDefaults`:

```go
applyTelegramSessionDefaults(cfg)
applyModelsDefaults(cfg)
if err := validateAndFixModels(cfg); err != nil {
	return nil, err
}

return cfg, nil
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd daemon && go test ./internal/config/ -v`
Expected: PASS, all config tests green.

- [ ] **Step 5: Commit**

```bash
git add daemon/internal/config/config.go daemon/internal/config/config_test.go
git commit -m "feat(config): validate permission_mode and downgrade haiku+auto"
```

---

## Task 3: Cold Worker Uses Model/Mode From Config

**Files:**
- Modify: `daemon/internal/daemon/worker.go:171-203` (inside `defaultBuildCommand`)
- Test: `daemon/internal/daemon/worker_test.go`

- [ ] **Step 1: Write the failing test**

Append to `daemon/internal/daemon/worker_test.go`:

```go
func TestColdWorkerBuildCommandUsesConfiguredModelAndMode(t *testing.T) {
	dir := t.TempDir()
	q := NewQueue(filepath.Join(dir, "queue.json"))
	cfg := &config.Config{
		WorkspacePath: dir,
		Models: config.ModelsConfig{
			ColdWorker: config.ModelConfig{Model: "opus", PermissionMode: "auto"},
		},
	}
	pool := NewWorkerPool(q, cfg, 1, 0, dir, nil)

	task := &Task{ID: "t_test", Prompt: "hello", TimeoutSec: 10}
	wc := pool.defaultBuildCommand(task)

	// Expect --model opus --permission-mode auto somewhere in args.
	joined := strings.Join(wc.Args, " ")
	assert.Contains(t, joined, "--model opus")
	assert.Contains(t, joined, "--permission-mode auto")
	assert.NotContains(t, joined, "--dangerously-skip-permissions")
}
```

Ensure `"strings"` is in the test file's imports (the existing file may not import it; add if needed).

- [ ] **Step 2: Run test to verify it fails**

Run: `cd daemon && go test ./internal/daemon/ -run TestColdWorkerBuildCommandUsesConfiguredModelAndMode -v`
Expected: FAIL, args do not contain `--model` or `--permission-mode`.

- [ ] **Step 3: Implement**

Edit `daemon/internal/daemon/worker.go`. Replace the `args` initialization block inside `defaultBuildCommand` (currently lines ~178-189) with:

```go
args := []string{
	"--print",
	"--output-format", "text",
}

if wp.cfg != nil {
	if m := wp.cfg.Models.ColdWorker.Model; m != "" {
		args = append(args, "--model", m)
	}
	if pm := wp.cfg.Models.ColdWorker.PermissionMode; pm != "" {
		args = append(args, "--permission-mode", pm)
	}
}

// Generate per-task settings.json for permission sandboxing.
workersDir := filepath.Join(wp.gobrrDir, "workers")
if settingsPath, err := security.Generate(workersDir, task.ID, task.AllowWrites); err == nil {
	args = append(args, "--settings", settingsPath)
}

args = append(args, prompt)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd daemon && go test ./internal/daemon/ -v`
Expected: PASS, including the new test and existing `TestWorkerPoolConcurrencyLimit` (which uses its own `buildCommand` override and isn't affected).

- [ ] **Step 5: Commit**

```bash
git add daemon/internal/daemon/worker.go daemon/internal/daemon/worker_test.go
git commit -m "feat(daemon): cold worker uses configured model and permission mode"
```

---

## Task 4: Warm Settings File Generator

**Files:**
- Create: `daemon/internal/daemon/warm_settings.go`
- Create: `daemon/internal/daemon/warm_settings_test.go`

- [ ] **Step 1: Write the failing test**

Create `daemon/internal/daemon/warm_settings_test.go`:

```go
package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnsureWarmSettingsCreatesFile(t *testing.T) {
	dir := t.TempDir()
	gobrrDir := filepath.Join(dir, ".gobrrr")

	path, err := EnsureWarmSettings(gobrrDir)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(gobrrDir, "workers", "warm-settings.json"), path)

	data, err := os.ReadFile(path)
	require.NoError(t, err)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal(data, &parsed))
	perms, ok := parsed["permissions"].(map[string]any)
	require.True(t, ok, "expected permissions object")

	allow, ok := perms["allow"].([]any)
	require.True(t, ok)
	assert.Contains(t, allow, "Read")
	assert.Contains(t, allow, "Glob")

	deny, ok := perms["deny"].([]any)
	require.True(t, ok)
	assert.Contains(t, deny, "Write")
	assert.Contains(t, deny, "Edit")
}

func TestEnsureWarmSettingsIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	gobrrDir := filepath.Join(dir, ".gobrrr")

	path, err := EnsureWarmSettings(gobrrDir)
	require.NoError(t, err)

	// Overwrite with arbitrary content — EnsureWarmSettings should not clobber it.
	sentinel := []byte(`{"marker":"user-edit"}`)
	require.NoError(t, os.WriteFile(path, sentinel, 0600))

	_, err = EnsureWarmSettings(gobrrDir)
	require.NoError(t, err)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, sentinel, data, "EnsureWarmSettings must not overwrite existing file")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd daemon && go test ./internal/daemon/ -run TestEnsureWarmSettings -v`
Expected: FAIL, `EnsureWarmSettings` undefined.

- [ ] **Step 3: Implement the generator**

Create `daemon/internal/daemon/warm_settings.go`:

```go
package daemon

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// warmSettingsFilename is the name of the shared warm-worker permissions file.
const warmSettingsFilename = "warm-settings.json"

// warmSettings is the default permissions document for warm workers.
// Allow-listed tools skip the auto-mode classifier entirely, minimizing
// classifier invocations (and therefore classifier aborts) for routine tasks.
var warmSettings = map[string]any{
	"permissions": map[string]any{
		"allow": []string{
			"Read", "Glob", "Grep",
			"Bash(gobrrr memory:*)",
			"Bash(git log:*)", "Bash(git status)", "Bash(git diff:*)",
		},
		"deny": []string{
			"Write", "Edit", "NotebookEdit",
			"Bash(rm:*)", "Bash(git push:*)",
		},
	},
}

// EnsureWarmSettings writes the warm-worker permissions file at
// <gobrrDir>/workers/warm-settings.json if it does not already exist.
// Returns the file path. Idempotent: existing files are left untouched so
// operators can edit the allow-list without fear of daemon overwrite.
func EnsureWarmSettings(gobrrDir string) (string, error) {
	workersDir := filepath.Join(gobrrDir, "workers")
	if err := os.MkdirAll(workersDir, 0700); err != nil {
		return "", err
	}
	path := filepath.Join(workersDir, warmSettingsFilename)

	if _, err := os.Stat(path); err == nil {
		return path, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}

	data, err := json.MarshalIndent(warmSettings, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		return "", err
	}
	return path, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd daemon && go test ./internal/daemon/ -run TestEnsureWarmSettings -v`
Expected: PASS, both tests.

- [ ] **Step 5: Commit**

```bash
git add daemon/internal/daemon/warm_settings.go daemon/internal/daemon/warm_settings_test.go
git commit -m "feat(daemon): add EnsureWarmSettings generator"
```

---

## Task 5: Warm Worker Uses Model/Mode/Settings and Captures Stderr

**Files:**
- Modify: `daemon/internal/daemon/warm_worker.go:74-152` (`Start` method)
- Test: `daemon/internal/daemon/warm_worker_test.go`

This task combines the flag swap *and* stderr capture. They touch the same code path (`Start`), so splitting them would cause churn.

- [ ] **Step 1: Write the failing test**

Append to `daemon/internal/daemon/warm_worker_test.go`:

```go
func TestWarmWorkerStartArgsIncludeModelAndMode(t *testing.T) {
	dir := t.TempDir()
	script := writeArgCaptureScript(t, dir)
	writeMockIdentity(t, dir)

	cfg := &config.Config{
		WorkspacePath: dir,
		Models: config.ModelsConfig{
			WarmWorker: config.ModelConfig{Model: "sonnet", PermissionMode: "auto"},
		},
	}
	ww := NewWarmWorker(0, dir, cfg, nil)
	ww.command = script

	ctx := t.Context()
	require.NoError(t, ww.Start(ctx))
	defer ww.Stop()

	// The capture script writes its own argv to <workDir>/argv.log
	args, err := os.ReadFile(filepath.Join(dir, "argv.log"))
	require.NoError(t, err)
	joined := string(args)
	assert.Contains(t, joined, "--model sonnet")
	assert.Contains(t, joined, "--permission-mode auto")
	assert.Contains(t, joined, "--settings")
	assert.NotContains(t, joined, "--dangerously-skip-permissions")
}

func TestWarmWorkerStderrRoutedToLogFile(t *testing.T) {
	dir := t.TempDir()
	script := writeStderrScript(t, dir)
	writeMockIdentity(t, dir)

	cfg := &config.Config{
		WorkspacePath: dir,
		Models: config.ModelsConfig{
			WarmWorker: config.ModelConfig{Model: "sonnet", PermissionMode: "auto"},
		},
	}
	ww := NewWarmWorker(7, dir, cfg, nil)
	ww.command = script

	ctx := t.Context()
	require.NoError(t, ww.Start(ctx))
	ww.Stop()

	logPath := filepath.Join(dir, "logs", "warm-7.log")
	data, err := os.ReadFile(logPath)
	require.NoError(t, err, "expected stderr log at %s", logPath)
	assert.Contains(t, string(data), "MARKER-STDERR")
}
```

Add helper scripts below `writeCrashScript` (or near it) in the same test file:

```go
// writeArgCaptureScript writes its own argv to <workDir>/argv.log, then
// behaves like writeMockScript (init, then loop-respond).
func writeArgCaptureScript(t *testing.T, dir string) string {
	t.Helper()
	script := filepath.Join(dir, "mock-claude-argv.sh")
	content := `#!/bin/bash
printf '%s ' "$@" > "` + dir + `/argv.log"
echo '{"type":"system","subtype":"init","session_id":"mock-session"}'
while IFS= read -r line; do
  echo '{"type":"result","subtype":"success","result":"mock response","is_error":false,"duration_ms":10}'
done
`
	require.NoError(t, os.WriteFile(script, []byte(content), 0755))
	return script
}

// writeStderrScript emits a stderr marker line at startup then behaves like
// writeMockScript. Used to verify stderr redirection.
func writeStderrScript(t *testing.T, dir string) string {
	t.Helper()
	script := filepath.Join(dir, "mock-claude-stderr.sh")
	content := `#!/bin/bash
echo "MARKER-STDERR" >&2
echo '{"type":"system","subtype":"init","session_id":"mock-session"}'
while IFS= read -r line; do
  echo '{"type":"result","subtype":"success","result":"mock response","is_error":false,"duration_ms":10}'
done
`
	require.NoError(t, os.WriteFile(script, []byte(content), 0755))
	return script
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd daemon && go test ./internal/daemon/ -run 'TestWarmWorkerStartArgsIncludeModelAndMode|TestWarmWorkerStderrRoutedToLogFile' -v`
Expected: FAIL — args don't contain `--model`; stderr log not found.

- [ ] **Step 3: Implement the `Start` changes**

Edit `daemon/internal/daemon/warm_worker.go`. Add `"path/filepath"` to the imports if not present (it is not; add it). Add `"os"` to the imports (it is not; add it).

Replace the block that constructs `cmd` inside `Start()` (currently lines ~83-94) with:

```go
var cmd *exec.Cmd
if ww.command != "claude" {
	// Test mode: run the mock script directly.
	cmd = exec.Command("bash", ww.command) //nolint:gosec
} else {
	settingsPath, err := EnsureWarmSettings(ww.gobrrDir)
	if err != nil {
		return fmt.Errorf("warm worker %d: ensure warm settings: %w", ww.id, err)
	}
	model := "sonnet"
	mode := "auto"
	if ww.cfg != nil {
		if m := ww.cfg.Models.WarmWorker.Model; m != "" {
			model = m
		}
		if pm := ww.cfg.Models.WarmWorker.PermissionMode; pm != "" {
			mode = pm
		}
	}
	cmd = exec.Command("claude", "-p",
		"--model", model,
		"--permission-mode", mode,
		"--settings", settingsPath,
		"--input-format", "stream-json",
		"--output-format", "stream-json",
		"--verbose",
	)
}
cmd.Dir = workDir
```

Before `stdin, err := cmd.StdinPipe()`, add stderr redirection:

```go
logDir := filepath.Join(ww.gobrrDir, "logs")
if err := os.MkdirAll(logDir, 0700); err == nil {
	logPath := filepath.Join(logDir, fmt.Sprintf("warm-%d.log", ww.id))
	if logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600); err == nil {
		cmd.Stderr = logFile
		// logFile stays open for the lifetime of the process. It is closed
		// when the process exits via Go's process-cleanup reaper.
	}
}
```

**Note for test mode:** the argv-capture test runs the script with `bash script ...`, but the script-construction branch above only passes extra flags when `ww.command == "claude"`. Adjust the test-mode branch to also append the flags so the capture script sees them:

Replace:

```go
if ww.command != "claude" {
	// Test mode: run the mock script directly.
	cmd = exec.Command("bash", ww.command) //nolint:gosec
}
```

with:

```go
if ww.command != "claude" {
	// Test mode: run the mock script and pass through the same flags that
	// the real invocation would use, so tests can assert on argv.
	settingsPath, err := EnsureWarmSettings(ww.gobrrDir)
	if err != nil {
		return fmt.Errorf("warm worker %d: ensure warm settings: %w", ww.id, err)
	}
	model := "sonnet"
	mode := "auto"
	if ww.cfg != nil {
		if m := ww.cfg.Models.WarmWorker.Model; m != "" {
			model = m
		}
		if pm := ww.cfg.Models.WarmWorker.PermissionMode; pm != "" {
			mode = pm
		}
	}
	cmd = exec.Command("bash", ww.command, //nolint:gosec
		"--model", model,
		"--permission-mode", mode,
		"--settings", settingsPath,
	)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd daemon && go test ./internal/daemon/ -v`
Expected: PASS — both new tests and the existing `TestWarmWorker*` suite.

- [ ] **Step 5: Commit**

```bash
git add daemon/internal/daemon/warm_worker.go daemon/internal/daemon/warm_worker_test.go
git commit -m "feat(daemon): warm worker uses --model/--permission-mode/--settings and captures stderr"
```

---

## Task 6: Warm Worker Anti-Flap Guard

**Files:**
- Modify: `daemon/internal/daemon/warm_worker.go` (struct + new method)
- Modify: `daemon/internal/daemon/worker.go:338-363` (`dispatchWarm`)
- Test: `daemon/internal/daemon/warm_worker_test.go`

- [ ] **Step 1: Write the failing test**

Append to `daemon/internal/daemon/warm_worker_test.go`:

```go
func TestWarmWorkerRespawnAllowedAfterTimeWindow(t *testing.T) {
	ww := NewWarmWorker(0, "", nil, nil)

	// First respawn is always allowed.
	assert.True(t, ww.RecordRespawnAttempt())
	// Immediate second respawn is blocked (within window).
	assert.False(t, ww.RecordRespawnAttempt())
	// Manually advance the recorded time beyond the window, then try again.
	ww.mu.Lock()
	ww.lastRespawn = time.Now().Add(-2 * respawnFlapWindow)
	ww.mu.Unlock()
	assert.True(t, ww.RecordRespawnAttempt())
}

func TestWarmWorkerDisabledAfterFlap(t *testing.T) {
	ww := NewWarmWorker(0, "", nil, nil)

	assert.True(t, ww.RecordRespawnAttempt())
	assert.False(t, ww.RecordRespawnAttempt())

	assert.True(t, ww.Disabled())
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd daemon && go test ./internal/daemon/ -run 'TestWarmWorkerRespawn|TestWarmWorkerDisabled' -v`
Expected: FAIL, `RecordRespawnAttempt`/`Disabled` undefined.

- [ ] **Step 3: Implement the anti-flap guard**

Edit `daemon/internal/daemon/warm_worker.go`. Add `"time"` to the imports (already present — confirm). Above `NewWarmWorker`, add:

```go
// respawnFlapWindow is the minimum gap between warm-worker respawns before
// the slot is considered flapping and disabled. Prevents tight loops when
// the classifier consistently aborts a misconfigured worker.
const respawnFlapWindow = 60 * time.Second
```

Add to `WarmWorker` struct (within the existing field block):

```go
type WarmWorker struct {
	// ... existing fields ...
	command     string
	lastRespawn time.Time
	disabled    bool
}
```

Append two methods at the end of the file:

```go
// RecordRespawnAttempt marks a respawn attempt. Returns true if the respawn
// is allowed to proceed; false if the slot has flapped (two respawns within
// respawnFlapWindow) and should be disabled instead. Callers MUST honor
// the returned value.
func (ww *WarmWorker) RecordRespawnAttempt() bool {
	ww.mu.Lock()
	defer ww.mu.Unlock()
	now := time.Now()
	if !ww.lastRespawn.IsZero() && now.Sub(ww.lastRespawn) < respawnFlapWindow {
		ww.disabled = true
		return false
	}
	ww.lastRespawn = now
	return true
}

// Disabled reports whether anti-flap has disabled this slot.
func (ww *WarmWorker) Disabled() bool {
	ww.mu.Lock()
	defer ww.mu.Unlock()
	return ww.disabled
}
```

Wire it into `dispatchWarm` in `daemon/internal/daemon/worker.go`. Replace the respawn block (currently lines ~349-355):

```go
// Respawn crashed warm worker if daemon is still running.
if ctx.Err() == nil {
	log.Printf("warm worker %d: crash detected, respawning", ww.id)
	ww.Stop()
	if startErr := ww.Start(ctx); startErr != nil {
		log.Printf("warm worker %d: respawn failed: %v", ww.id, startErr)
	}
}
```

with:

```go
// Respawn crashed warm worker if daemon is still running, unless the slot
// has flapped (repeated aborts within the anti-flap window).
if ctx.Err() == nil {
	if !ww.RecordRespawnAttempt() {
		log.Printf("warm worker %d: flap detected, slot disabled until manual restart", ww.id)
		ww.Stop()
		return
	}
	log.Printf("warm worker %d: crash detected, respawning", ww.id)
	ww.Stop()
	if startErr := ww.Start(ctx); startErr != nil {
		log.Printf("warm worker %d: respawn failed: %v", ww.id, startErr)
	}
}
```

Update `reserveWarmWorker` in the same file so disabled slots cannot be claimed:

```go
func (wp *WorkerPool) reserveWarmWorker() *WarmWorker {
	wp.mu.Lock()
	workers := append([]*WarmWorker(nil), wp.warmWorkers...)
	wp.mu.Unlock()
	for _, ww := range workers {
		if ww.Disabled() {
			continue
		}
		if ww.Reserve() {
			return ww
		}
	}
	return nil
}
```

Update `WarmStatus` in the same file so disabled slots are still counted in `total` but not in `ready`:

```go
func (wp *WorkerPool) WarmStatus() (total, ready, busy int) {
	wp.mu.Lock()
	workers := append([]*WarmWorker(nil), wp.warmWorkers...)
	wp.mu.Unlock()
	for _, ww := range workers {
		total++
		ww.mu.Lock()
		if ww.ready && !ww.disabled {
			ready++
		}
		if ww.busy {
			busy++
		}
		ww.mu.Unlock()
	}
	return
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd daemon && go test ./internal/daemon/ -v`
Expected: PASS, including the two new anti-flap tests and all existing tests.

- [ ] **Step 5: Commit**

```bash
git add daemon/internal/daemon/warm_worker.go daemon/internal/daemon/worker.go daemon/internal/daemon/warm_worker_test.go
git commit -m "feat(daemon): anti-flap guard for warm worker respawn"
```

---

## Task 7: Health Endpoint Exposes Models

**Files:**
- Modify: `daemon/internal/daemon/daemon.go:308-329` (`healthResponse` + `handleHealth`)
- Test: `daemon/internal/daemon/daemon_test.go`

- [ ] **Step 1: Write the failing test**

Open `daemon/internal/daemon/daemon_test.go` and add:

```go
func TestHealthEndpointIncludesModels(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{
		WorkspacePath: dir,
		Models: config.ModelsConfig{
			Launcher:   config.ModelConfig{Model: "haiku", PermissionMode: "default"},
			WarmWorker: config.ModelConfig{Model: "sonnet", PermissionMode: "auto"},
			ColdWorker: config.ModelConfig{Model: "opus", PermissionMode: "auto"},
		},
	}
	d := New(cfg, filepath.Join(dir, "sock"))

	req := httptest.NewRequest("GET", "/health", nil)
	rr := httptest.NewRecorder()
	d.handleHealth(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &body))

	models, ok := body["models"].(map[string]any)
	require.True(t, ok, "expected models object in health response")

	launcher, ok := models["launcher"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "haiku", launcher["model"])
	assert.Equal(t, "default", launcher["permission_mode"])

	warm, ok := models["warm_worker"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "sonnet", warm["model"])

	cold, ok := models["cold_worker"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "opus", cold["model"])
}
```

Ensure `"net/http"`, `"net/http/httptest"`, and `"encoding/json"` are in the test file imports (add if missing).

- [ ] **Step 2: Run test to verify it fails**

Run: `cd daemon && go test ./internal/daemon/ -run TestHealthEndpointIncludesModels -v`
Expected: FAIL, response does not contain `models` field.

- [ ] **Step 3: Implement**

Edit `daemon/internal/daemon/daemon.go`. Replace the `healthResponse` struct (lines ~308-314):

```go
// healthResponse is the JSON body returned by GET /health.
type healthResponse struct {
	Status        string              `json:"status"`
	UptimeSec     int64               `json:"uptime_sec"`
	WorkersActive int                 `json:"workers_active"`
	QueueDepth    int                 `json:"queue_depth"`
	WarmWorkers   warmStatus          `json:"warm_workers"`
	Models        config.ModelsConfig `json:"models"`
}
```

Replace `handleHealth` (lines ~316-329):

```go
func (d *Daemon) handleHealth(w http.ResponseWriter, r *http.Request) {
	activeTasks := d.queue.List(false)
	total, ready, busy := d.workerPool.WarmStatus()
	resp := healthResponse{
		Status:        "ok",
		UptimeSec:     int64(time.Since(d.startTime).Seconds()),
		WorkersActive: d.workerPool.Active(),
		QueueDepth:    len(activeTasks),
		WarmWorkers:   warmStatus{Total: total, Ready: ready, Busy: busy},
		Models:        d.cfg.Models,
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp) //nolint:errcheck
}
```

Ensure `daemon.go` imports `"github.com/racterub/gobrrr/internal/config"` (it already does).

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd daemon && go test ./internal/daemon/ -v`
Expected: PASS, including the new test and all existing tests.

- [ ] **Step 5: Update `gobrrr daemon status` CLI output**

Edit `daemon/cmd/gobrrr/main.go`. In `daemonStatusCmd.RunE` (lines ~81-93), after the existing `fmt.Printf` calls, append:

```go
if models, ok := info["models"].(map[string]any); ok {
	fmt.Println("Models:")
	for _, role := range []string{"launcher", "warm_worker", "cold_worker"} {
		if m, ok := models[role].(map[string]any); ok {
			fmt.Printf("  %-12s %v (%v)\n", role, m["model"], m["permission_mode"])
		}
	}
}
```

- [ ] **Step 6: Build and smoke-test the CLI**

Run: `cd daemon && CGO_ENABLED=0 go build -o gobrrr ./cmd/gobrrr/`
Expected: Build succeeds.

Manual check (only if a daemon is running locally):
```bash
./gobrrr daemon status
```
Expected: output includes `Models:` block.

- [ ] **Step 7: Commit**

```bash
git add daemon/internal/daemon/daemon.go daemon/internal/daemon/daemon_test.go daemon/cmd/gobrrr/main.go
git commit -m "feat(daemon): expose models in health endpoint and status CLI"
```

---

## Task 8: Security Permissions Documentation

**Files:**
- Modify: `daemon/internal/security/permissions.go:21-27`

No test — documentation-only change.

- [ ] **Step 1: Add the comment**

Edit `daemon/internal/security/permissions.go`. Above the `Generate` function signature, replace the existing docstring with:

```go
// Generate creates a per-task settings.json for Claude Code workers.
// workersDir is the base directory (e.g. ~/.gobrrr/workers/), taskID is the task ID.
// Returns the path to the generated settings.json.
//
// Rules produced here are deliberately narrow (e.g. Bash(gobrrr *) rather
// than Bash(*)). Claude Code's --permission-mode auto drops broad wildcards
// on startup as a defense-in-depth measure, so narrow rules are not just
// good hygiene — they're required for the allow-list to take effect.
```

- [ ] **Step 2: Verify it still builds**

Run: `cd daemon && go build ./...`
Expected: build succeeds.

- [ ] **Step 3: Commit**

```bash
git add daemon/internal/security/permissions.go
git commit -m "docs(security): note auto-mode wildcard-drop behavior on Generate"
```

---

## Task 9: Launcher Shell Script

**Files:**
- Modify: `scripts/launcher.sh:10-31` (config read block) and `:180-192` (`expect` block)

No automated test — shell-level integration. Manual verification steps below.

- [ ] **Step 1: Add model/mode/settings config reads**

Edit `scripts/launcher.sh`. Replace the existing `cfg` and channels lines (around lines 15-21) so they read model/mode/settings alongside the existing fields. Insert after the existing `CHANNELS=...` line:

```bash
LAUNCHER_MODEL=$(cfg '.models.launcher.model // "haiku"')
LAUNCHER_MODE=$(cfg '.models.launcher.permission_mode // "default"')
LAUNCHER_SETTINGS="${GOBRRR_DIR}/launcher-settings.json"
```

- [ ] **Step 2: Update the `expect` spawn line**

Still inside `scripts/launcher.sh`, locate the `expect` heredoc (around line 181) and replace:

```
spawn -noecho $CLAUDE_BIN $CHANNELS
```

with:

```
spawn -noecho $CLAUDE_BIN --model $LAUNCHER_MODEL --permission-mode $LAUNCHER_MODE --settings $LAUNCHER_SETTINGS $CHANNELS
```

**Note:** the launcher previously ran with `--dangerously-skip-permissions` (it did not — checked; the flag is passed via `$CHANNELS` building in the original line `[.telegram_session.channels ... | join(" ")]`). Confirm the current file does not include `--dangerously-skip-permissions` in the literal spawn line; if it does, remove it. If it lives in `$CHANNELS`, leave it — the channels string is only plugin flags.

Grep confirmation:
```bash
grep -n 'dangerously-skip-permissions' scripts/launcher.sh
```
Expected: no output (the launcher currently does not pass this flag explicitly; Claude's default mode is unrestricted when `--channels` is used with `--dangerously-load-development-channels`). If output is present, remove those tokens from the `spawn` line.

- [ ] **Step 3: Shellcheck**

Run: `shellcheck scripts/launcher.sh`
Expected: no new errors introduced by this change (pre-existing warnings are fine if the diff adds none).

- [ ] **Step 4: Manual verification (local or remote)**

Prepare a fixture config:
```bash
mkdir -p /tmp/fake-gobrrr
cat > /tmp/fake-gobrrr/config.json <<'JSON'
{
  "telegram_session": {"channels": ["plugin:foo@bar"], "idle_threshold_min": 30},
  "models": {"launcher": {"model": "haiku", "permission_mode": "default"}}
}
JSON
```

Extract the argv that would be passed to `claude` without actually executing it:
```bash
GOBRRR_DIR=/tmp/fake-gobrrr bash -n scripts/launcher.sh && echo "syntax ok"
GOBRRR_DIR=/tmp/fake-gobrrr CLAUDE_BIN=/usr/bin/echo bash -x scripts/launcher.sh 2>&1 | head -40
```
Expected: trace shows `--model haiku --permission-mode default --settings /tmp/fake-gobrrr/launcher-settings.json` in the `expect` invocation. Kill with `^C` once you see the spawn.

- [ ] **Step 5: Commit**

```bash
git add scripts/launcher.sh
git commit -m "feat(launcher): read model, permission mode, and settings from config"
```

---

## Task 10: Install Script Generates Launcher Settings

**Files:**
- Modify: `scripts/install.sh` (add a new step around line 240, before systemd install)

- [ ] **Step 1: Insert launcher-settings.json generation**

Edit `scripts/install.sh`. Immediately *before* the existing block that begins with the comment `# --- Step 14: Install systemd unit ---`, add:

```bash
# --- Launcher settings ---
step "Ensuring launcher permissions file"

GOBRRR_DIR="/home/claude-agent/.gobrrr"
LAUNCHER_SETTINGS="$GOBRRR_DIR/launcher-settings.json"

if [ ! -f "$LAUNCHER_SETTINGS" ]; then
    mkdir -p "$GOBRRR_DIR"
    cat > "$LAUNCHER_SETTINGS" <<'JSON'
{
  "permissions": {
    "allow": [
      "Bash(gobrrr submit:*)",
      "Bash(gobrrr status:*)",
      "Bash(gobrrr list:*)",
      "Bash(gobrrr logs:*)",
      "mcp__plugin_gobrrr-telegram_telegram__*"
    ],
    "deny": ["Write", "Edit", "Bash(rm:*)", "Bash(git push:*)"]
  }
}
JSON
    chown -R claude-agent:claude-agent "$GOBRRR_DIR"
    chmod 600 "$LAUNCHER_SETTINGS"
    echo "Created $LAUNCHER_SETTINGS"
else
    echo "Launcher settings already exist at $LAUNCHER_SETTINGS"
fi
```

Renumber the subsequent `# --- Step 14 ...` comment to `# --- Step 15 ...` and cascade the rest if the install script uses sequential step numbers. Inspect the file first:

```bash
grep -n 'Step 1[0-9]' scripts/install.sh
```

If the numbering is sequential, increment all steps from 14 onward by 1. If it is purely cosmetic / inconsistent, leave as-is but keep the comment headers in order.

- [ ] **Step 2: Shellcheck**

Run: `shellcheck scripts/install.sh`
Expected: no new errors.

- [ ] **Step 3: Manual dry run** (optional — requires sudo and a test VM)

Skip on dev machines. On the target LXC during actual deploy, verify:
```bash
ls -l /home/claude-agent/.gobrrr/launcher-settings.json
```
Expected: file exists, owner `claude-agent`, mode `0600`.

- [ ] **Step 4: Commit**

```bash
git add scripts/install.sh
git commit -m "feat(install): generate launcher-settings.json on install"
```

---

## Task 11: Remove Stale TODO Items

**Files:**
- Modify: `TODO.md`

The existing TODO item *"Teach Telegram session to dispatch via gobrrr warm/cold workers — 2026-04-13"* is absorbed into this work (the instructions in Task 9's companion `CLAUDE.md` / `identity.md` deploy on the remote server). That item should be deleted here; the deploy work lives outside the repo.

- [ ] **Step 1: Read `TODO.md` and locate the section**

Run: `cat TODO.md` — find the section starting with `## Teach Telegram session to dispatch via gobrrr warm/cold workers`.

- [ ] **Step 2: Remove the section**

Edit `TODO.md` and delete the section *"Teach Telegram session to dispatch via gobrrr warm/cold workers — 2026-04-13"* in its entirety, including the blank line after it.

Do NOT remove any other sections — they are separate deferred work.

- [ ] **Step 3: Commit**

```bash
git add TODO.md
git commit -m "docs(todo): remove dispatch-instructions item (absorbed into coordination spec)"
```

---

## Self-Review

### Spec coverage

Checked each section of the spec against the plan:

- ✅ Role-to-model-to-mode table → Tasks 1, 3, 5, 9
- ✅ Configuration schema → Task 1
- ✅ Config validation (unknown mode, haiku+auto) → Task 2
- ✅ Launcher wiring (flags, settings file) → Tasks 9, 10
- ✅ Warm worker wiring (flags, settings file, stderr capture) → Tasks 4, 5
- ✅ Cold worker wiring → Task 3
- ✅ Error handling (anti-flap, health endpoint) → Tasks 6, 7
- ✅ Security permissions docs → Task 8
- ✅ Manual launcher-instruction deploy absorbs the TODO.md item → Task 11

Spec requirement *"launcher identity.md / CLAUDE.md updates"* is explicitly out-of-repo (on remote `claude-agent`) and deployed separately — not a task in this plan.

### Placeholder scan

Searched this plan for `TBD`, `TODO`, `implement later`, `fill in`, `handle edge cases`, `similar to Task N` (without repeated code), and steps that describe without showing — none found.

### Type consistency

- `ModelsConfig`, `ModelConfig`, `cfg.Models.Launcher.Model`, `cfg.Models.WarmWorker.PermissionMode` — consistent across Tasks 1, 2, 3, 5, 7.
- `EnsureWarmSettings`, `warmSettingsFilename` — Task 4 defines, Task 5 uses.
- `RecordRespawnAttempt`, `Disabled`, `respawnFlapWindow` — Task 6 defines and uses consistently; Task 6 also updates `reserveWarmWorker` and `WarmStatus` to honor the new fields.
- `healthResponse.Models` type `config.ModelsConfig` — matches Task 1's struct.

### Notes for the executing agent

- Run `cd daemon && go build ./... && go test ./...` after each code task. If anything fails that this plan doesn't explain, stop and re-read the spec.
- Commit after every task. Tests must pass before commit.
- Task 9 (launcher.sh) and Task 10 (install.sh) cannot be unit-tested. Rely on shellcheck + manual trace as described.
- The remote instruction deploy (launcher `CLAUDE.md` / `identity.md` edits) is out of scope for this repo; flag it in the PR description so it is not forgotten.
