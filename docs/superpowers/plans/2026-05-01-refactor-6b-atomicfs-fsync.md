# Refactor #6b — atomicfs Parent-Dir Fsync Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `atomicfs.WriteFile` durable on power loss by fsync'ing the parent directory after the rename, with a test asserting the syscall happens.

**Architecture:** Introduce a package-level `fsyncDir` function variable that production sets to `os.Open(dir).Sync()`. Tests swap it via an `export_test.go` shim to assert call count, target path, and error propagation. `WriteFile` calls `fsyncDir(filepath.Dir(path))` after `os.Rename`; on Linux this flushes the directory entry that the rename created.

**Tech Stack:** Go 1.23+, stdlib `os`/`path/filepath`, `testify/assert`+`require` (already in test file).

**Branch:** `refactor/6b-atomicfs-fsync` cut from current `master`.

**Single commit:** `fix(atomicfs): fsync parent directory after rename` — tagged `Behavioral change.`

---

## Phase 1

### Task 1: Cut the feature branch

**Files:** none (git only)

- [ ] **Step 1: Verify clean working tree on master**

Run: `git status`
Expected: `On branch master` and `nothing to commit, working tree clean`. If dirty, stop and surface to user.

- [ ] **Step 2: Sync master**

Run: `git fetch origin && git log --oneline origin/master..master && git log --oneline master..origin/master`
Expected: both ranges empty (local and origin/master at same SHA). If origin is ahead, fast-forward with `git merge --ff-only origin/master`. If local is ahead, that's the unmerged #6a tail — fine, proceed.

- [ ] **Step 3: Create and switch to branch**

Run: `git checkout -b refactor/6b-atomicfs-fsync`
Expected: `Switched to a new branch 'refactor/6b-atomicfs-fsync'`.

---

### Task 2: Write the failing tests + export shim

**Files:**
- Create: `daemon/internal/atomicfs/export_test.go`
- Modify: `daemon/internal/atomicfs/write_test.go` (append two tests)

- [ ] **Step 1: Create the test-only override shim**

Create `daemon/internal/atomicfs/export_test.go` with exactly:

```go
package atomicfs

// SetFsyncDirForTest swaps the package-internal fsyncDir hook for the
// duration of a test and returns a restore func. Production callers must
// not use this — it is exported only because it lives behind the _test.go
// build tag (compiled only during `go test`).
func SetFsyncDirForTest(fn func(dir string) error) (restore func()) {
	orig := fsyncDir
	fsyncDir = fn
	return func() { fsyncDir = orig }
}
```

- [ ] **Step 2: Append the fsync-called test**

Append to `daemon/internal/atomicfs/write_test.go`:

```go
func TestWriteFileFsyncsParentDir(t *testing.T) {
	var (
		calls   int
		gotDir  string
	)
	restore := atomicfs.SetFsyncDirForTest(func(dir string) error {
		calls++
		gotDir = dir
		return nil
	})
	defer restore()

	dir := t.TempDir()
	path := filepath.Join(dir, "x.json")

	require.NoError(t, atomicfs.WriteFile(path, []byte("x"), 0600))

	assert.Equal(t, 1, calls, "fsync must be called exactly once per WriteFile")
	assert.Equal(t, dir, gotDir, "fsync target must be the parent directory of the written file")
}
```

- [ ] **Step 3: Append the fsync-error-propagation test**

Append to `daemon/internal/atomicfs/write_test.go`:

```go
func TestWriteFileFsyncErrorPropagates(t *testing.T) {
	sentinel := errors.New("simulated fsync failure")
	restore := atomicfs.SetFsyncDirForTest(func(string) error {
		return sentinel
	})
	defer restore()

	dir := t.TempDir()
	path := filepath.Join(dir, "x.json")

	err := atomicfs.WriteFile(path, []byte("x"), 0600)
	assert.ErrorIs(t, err, sentinel, "WriteFile must surface fsync errors to the caller")
}
```

- [ ] **Step 4: Add the `errors` import**

Edit the import block at the top of `daemon/internal/atomicfs/write_test.go` to add `"errors"`. Final import block:

```go
import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/racterub/gobrrr/internal/atomicfs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)
```

- [ ] **Step 5: Run tests — they must FAIL with a build error**

Run: `cd /home/racterub/github/gobrrr/daemon && go test ./internal/atomicfs/...`
Expected: build failure mentioning `undefined: fsyncDir` (because `export_test.go` references the symbol before `write.go` defines it). This is the red phase — both new tests cannot even compile yet.

If you instead see passing tests, you forgot to save one of the new files — re-check Steps 1–4 before proceeding.

---

### Task 3: Implement fsync in production code

**Files:**
- Modify: `daemon/internal/atomicfs/write.go`

- [ ] **Step 1: Replace the entire `write.go` contents**

Overwrite `daemon/internal/atomicfs/write.go` with:

```go
// Package atomicfs provides atomic file writes via a sibling .tmp file plus
// rename, with a parent-directory fsync after the rename so the directory
// entry survives power loss. The parent directory must already exist; mkdir
// is the caller's job.
package atomicfs

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// fsyncDir opens dir and calls Sync on it so the most recent rename within
// dir is durable on the filesystem. It is a package variable so tests can
// substitute a stub via export_test.go.
var fsyncDir = func(dir string) error {
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	syncErr := f.Sync()
	closeErr := f.Close()
	if syncErr != nil {
		return syncErr
	}
	return closeErr
}

// WriteFile writes data to path atomically by creating path+".tmp" with the
// given permissions, renaming it over path, then fsync'ing the parent
// directory. The parent directory must already exist.
func WriteFile(path string, data []byte, perm os.FileMode) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, perm); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	return fsyncDir(filepath.Dir(path))
}

// WriteJSON marshals v with two-space indentation and writes the result
// atomically via WriteFile. Callers needing a non-default JSON format
// (compact, four-space, etc.) should marshal themselves and call WriteFile.
func WriteJSON(path string, v any, perm os.FileMode) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return WriteFile(path, data, perm)
}
```

- [ ] **Step 2: Run the package tests — they must PASS**

Run: `cd /home/racterub/github/gobrrr/daemon && go test ./internal/atomicfs/... -v`
Expected: all eight tests pass — the original six (`TestWriteFileRoundTrip`, `TestWriteFileEnforcesPerm`, `TestWriteFileNoTempLeftOnSuccess`, `TestWriteFileOverwritesExisting`, `TestWriteFileFailsWhenDirMissing`, `TestWriteJSONShape`) plus the two new ones (`TestWriteFileFsyncsParentDir`, `TestWriteFileFsyncErrorPropagates`).

If any pre-existing test now fails, do NOT proceed — the implementation has a regression. Common cause: forgot `path/filepath` import or used `path.Dir` (which is for URL paths, not OS paths).

---

### Task 4: Full gate, then commit

**Files:** none (verification + git only)

- [ ] **Step 1: Run the full daemon test suite**

Run: `cd /home/racterub/github/gobrrr/daemon && CGO_ENABLED=0 go build ./... && go test ./... && go vet ./...`
Expected: build succeeds, all tests pass, vet emits no warnings. Both binaries (`gobrrr` and `gobrrr-telegram`) must build because both depend on `internal/atomicfs` transitively.

If any test outside `internal/atomicfs` fails, that's a real regression — investigate before committing.

- [ ] **Step 2: Review the diff**

Run: `git diff --stat && git diff`
Expected: exactly three files touched —
- `daemon/internal/atomicfs/write.go` (modified, ~15 net lines added)
- `daemon/internal/atomicfs/write_test.go` (modified, ~25 net lines added)
- `daemon/internal/atomicfs/export_test.go` (new, ~10 lines)

No other files. If anything else appears in the diff, stop and surface it to the user.

- [ ] **Step 3: Stage and commit**

Run:

```bash
cd /home/racterub/github/gobrrr && \
  git add daemon/internal/atomicfs/write.go \
          daemon/internal/atomicfs/write_test.go \
          daemon/internal/atomicfs/export_test.go && \
  git commit -m "$(cat <<'EOF'
fix(atomicfs): fsync parent directory after rename

WriteFile previously did write-tmp + rename only. On power loss between
the rename and the next natural fsync, the directory entry could be lost
even though the inode was on disk — leaving the file invisible despite a
successful WriteFile return. Open the parent directory and call Sync()
after the rename so the entry is durable.

Adds two tests via a swappable fsyncDir hook (export_test.go shim): one
asserts the syscall fires with the parent directory path, the other
asserts errors from fsync propagate to the WriteFile caller.

Closes the durability gap flagged in the package doc comment of #6a.

Behavioral change.

AI-Ratio: 1.0
Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

Expected: the commit lands. If the pre-commit hook fails, fix the underlying issue and create a NEW commit (do not `--amend`, do not `--no-verify`).

- [ ] **Step 4: Verify post-commit state**

Run: `git log --oneline -3 && git status`
Expected: top commit is `fix(atomicfs): fsync parent directory after rename` on `refactor/6b-atomicfs-fsync`; working tree clean.

---

## Definition of Done

- Branch `refactor/6b-atomicfs-fsync` carries exactly one commit beyond `master`.
- That commit's title is `fix(atomicfs): fsync parent directory after rename`.
- `daemon/internal/atomicfs/write.go` calls `fsyncDir(filepath.Dir(path))` as the last step of `WriteFile`, returning its error.
- `TestWriteFileFsyncsParentDir` asserts the syscall happens with the parent directory path.
- `TestWriteFileFsyncErrorPropagates` asserts errors propagate.
- `cd daemon && CGO_ENABLED=0 go build ./... && go test ./... && go vet ./...` is green.
- Commit body includes `Behavioral change.` and `AI-Ratio: 1.0` trailers.

This is the final entry in the structural refactor batch — once merged, the umbrella spec at `docs/superpowers/specs/2026-04-26-structural-refactor-batch-design.md` is fully discharged.
