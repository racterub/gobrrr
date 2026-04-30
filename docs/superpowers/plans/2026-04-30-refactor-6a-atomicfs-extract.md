# Refactor #6a — Centralize Atomic File IO into `internal/atomicfs` Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace seven duplicated `tmp + WriteFile + Rename` helpers across the codebase with a single shared `internal/atomicfs` package exposing `WriteFile(path, data, perm)` and `WriteJSON(path, v, perm)`. After this refactor, no package outside `internal/atomicfs` writes a `.tmp` file followed by `os.Rename`.

**Architecture:** Eight commits — one helper-package commit and seven per-package migrations (one commit per package, one package per task). The `atomicfs` package is a leaf (no internal imports); migrations are pure structural moves with byte-identical on-disk output. Parent-directory `fsync` is **not** added in this refactor — that ships separately in #6b.

**Tech Stack:** Pure Go standard library (`os`, `encoding/json`, `path/filepath`). No new dependencies.

**Sequence position:** Refactor #6a of the structural batch (`docs/superpowers/specs/2026-04-26-structural-refactor-batch-design.md`). #13, #7, #9, #8, and #10 are merged. After #6a ships, only #6b (parent-dir fsync, behavioral) remains in the batch.

**Branch:** `refactor/6a-atomicfs-extract` (cut from `master`).

---

## Scope

The seven migration sites and their current shapes:

| # | Package | File / line | Helper today | JSON format |
|---|---------|-------------|--------------|-------------|
| 1 | `internal/daemon` | `queue.go:389-415` (`flush`) | inline | `MarshalIndent("", "  ")` |
| 2 | `internal/daemon` | `approvals_store.go:42-55` (`Save`) | inline | `MarshalIndent("", "  ")` |
| 3 | `internal/scheduler` | `scheduler.go:80-93` (`flush`) | inline | `MarshalIndent("", "  ")` |
| 4 | `internal/memory` | `store.go:242-257` (`persistIndex` + `atomicWrite`) | local `atomicWrite` | `Marshal` (compact) |
| 5 | `internal/skills` | `bundled.go:97-110` | local `writeAtomic` | `MarshalIndent("", "  ")` |
| 6 | `internal/clawhub` | `installer.go:352-358` (helper) + `commit.go:122,211` (callers) | local `writeAtomic` | `MarshalIndent("", "  ")` |
| 7 | `internal/google` | `auth.go:100, 272, 282-288` | local `writeAtomic` (`auth.go` already comments that this should consolidate into `atomicfs` per Refactor #6) | binary (vault) + `MarshalIndent("", "    ")` (4-space) |

After this refactor:

- New package `daemon/internal/atomicfs/` containing `write.go` and `write_test.go`.
- Zero `os.WriteFile(...".tmp"...)` patterns outside `internal/atomicfs` (verified by grep — see Task 8 final check).
- All seven local `atomicWrite` / `writeAtomic` helpers deleted.
- All existing tests across the affected packages still pass with no modifications.
- On-disk JSON byte-identical to master for every migration (no behavior change).

---

## File Structure

| File | New / Modify | Responsibility |
|------|--------------|----------------|
| `daemon/internal/atomicfs/write.go` | Create | `WriteFile(path, data, perm)` + `WriteJSON(path, v, perm)` |
| `daemon/internal/atomicfs/write_test.go` | Create | Round-trip, perm enforcement, rename-fail cleanup, JSON shape |
| `daemon/internal/daemon/queue.go` | Modify | `flush` calls `atomicfs.WriteJSON`; drops inline `.tmp + Rename` |
| `daemon/internal/daemon/approvals_store.go` | Modify | `Save` calls `atomicfs.WriteJSON`; drops inline `.tmp + Rename` |
| `daemon/internal/scheduler/scheduler.go` | Modify | `flush` calls `atomicfs.WriteJSON`; drops inline `.tmp + Rename` |
| `daemon/internal/memory/store.go` | Modify | `persistIndex` calls `atomicfs.WriteFile`; deletes `atomicWrite` |
| `daemon/internal/skills/bundled.go` | Modify | Calls `atomicfs.WriteJSON`; deletes `writeAtomic` |
| `daemon/internal/clawhub/installer.go` | Modify | Deletes `writeAtomic` |
| `daemon/internal/clawhub/commit.go` | Modify | Replaces both `writeAtomic` call sites with `atomicfs.WriteJSON` (committed alongside `installer.go` since they share the package and the deleted helper) |
| `daemon/internal/google/auth.go` | Modify | Replaces three `writeAtomic` call sites with `atomicfs.WriteFile`; deletes local helper |

---

## API Design — `internal/atomicfs`

```go
// Package atomicfs provides atomic file writes via a sibling .tmp file plus
// rename. The parent directory is NOT fsync'd in this version; durability on
// power loss is added in Refactor #6b.
package atomicfs

import (
	"encoding/json"
	"os"
)

// WriteFile writes data to path atomically by creating path+".tmp" with the
// given permissions and renaming it over path.
func WriteFile(path string, data []byte, perm os.FileMode) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, perm); err != nil {
		return err
	}
	return os.Rename(tmp, path)
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

`WriteJSON` is a thin convenience over `WriteFile`; callers whose existing JSON shape differs from the default (memory's compact `Marshal`, google's four-space `MarshalIndent`) keep their own `json.Marshal*` call and use `WriteFile` to preserve byte-identical output.

The package depends only on stdlib — no other `internal/` package — so it is a leaf and never causes import cycles.

---

## Test gate

After every commit:

```
cd daemon && CGO_ENABLED=0 go build ./... && go test ./... && go vet ./...
```

All three must succeed. No `--no-verify`. If a hook fails, fix the underlying issue and create a new commit.

---

## Commit message convention

Every commit body must include `Structural change.` per the global tidy-first rule, plus the standard trailers:

```
AI-Ratio: 1.0
Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
```

---

## Phase 1 — Helper package + daemon migrations

### Task 1: Create the `atomicfs` package

**Files:**
- Create: `daemon/internal/atomicfs/write.go`
- Create: `daemon/internal/atomicfs/write_test.go`

- [ ] **Step 1: Cut the branch and confirm clean baseline**

```bash
git checkout master
git pull --ff-only
git checkout -b refactor/6a-atomicfs-extract
cd daemon && CGO_ENABLED=0 go build ./... && go test ./... && go vet ./...
```

Expected: clean checkout, all green.

- [ ] **Step 2: Write the failing tests**

Create `daemon/internal/atomicfs/write_test.go`:

```go
package atomicfs_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/racterub/gobrrr/internal/atomicfs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteFileRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "data.bin")
	payload := []byte("hello atomic world")

	require.NoError(t, atomicfs.WriteFile(path, payload, 0600))

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, payload, got)
}

func TestWriteFileEnforcesPerm(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secret")

	require.NoError(t, atomicfs.WriteFile(path, []byte("x"), 0600))

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0600), info.Mode().Perm())
}

func TestWriteFileNoTempLeftOnSuccess(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.json")

	require.NoError(t, atomicfs.WriteFile(path, []byte("{}"), 0600))

	_, err := os.Stat(path + ".tmp")
	assert.True(t, os.IsNotExist(err), "sibling .tmp must be gone after rename")
}

func TestWriteFileOverwritesExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.json")

	require.NoError(t, atomicfs.WriteFile(path, []byte("old"), 0600))
	require.NoError(t, atomicfs.WriteFile(path, []byte("new"), 0600))

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, []byte("new"), got)
}

func TestWriteFileFailsWhenDirMissing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "missing-subdir", "x.json")

	err := atomicfs.WriteFile(path, []byte("x"), 0600)
	assert.Error(t, err, "writing into nonexistent dir must fail; mkdir is the caller's job")
}

func TestWriteJSONShape(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.json")

	value := map[string]any{"b": 2, "a": 1}
	require.NoError(t, atomicfs.WriteJSON(path, value, 0600))

	got, err := os.ReadFile(path)
	require.NoError(t, err)

	var roundTrip map[string]any
	require.NoError(t, json.Unmarshal(got, &roundTrip))
	assert.Equal(t, float64(1), roundTrip["a"])
	assert.Equal(t, float64(2), roundTrip["b"])

	// Two-space indent contract.
	assert.Contains(t, string(got), "\n  \"a\":")
}
```

- [ ] **Step 3: Run tests to verify they fail**

```bash
cd daemon && go test ./internal/atomicfs/... -v
```

Expected: FAIL with "no Go files" or "package atomicfs is not in std".

- [ ] **Step 4: Implement the package**

Create `daemon/internal/atomicfs/write.go`:

```go
// Package atomicfs provides atomic file writes via a sibling .tmp file plus
// rename. The parent directory is NOT fsync'd in this version; durability on
// power loss is added in Refactor #6b.
package atomicfs

import (
	"encoding/json"
	"os"
)

// WriteFile writes data to path atomically by creating path+".tmp" with the
// given permissions and renaming it over path. The parent directory must
// already exist.
func WriteFile(path string, data []byte, perm os.FileMode) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, perm); err != nil {
		return err
	}
	return os.Rename(tmp, path)
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

- [ ] **Step 5: Run the full test gate**

```bash
cd daemon && CGO_ENABLED=0 go build ./... && go test ./... && go vet ./...
```

Expected: PASS, including the new `internal/atomicfs` tests.

- [ ] **Step 6: Commit**

```bash
git add daemon/internal/atomicfs/write.go daemon/internal/atomicfs/write_test.go
git commit -m "$(cat <<'EOF'
feat(atomicfs): add WriteFile/WriteJSON helper

New leaf package internal/atomicfs centralizes the .tmp + rename pattern
duplicated across queue, approvals_store, scheduler, memory, skills,
clawhub, and google. WriteFile is the primitive; WriteJSON is a
two-space-indent convenience for the common case. Parent-directory
fsync is intentionally not added here — that ships in Refactor #6b.

Structural change.

AI-Ratio: 1.0
Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

### Task 2: Migrate `daemon/queue.go`

**Files:**
- Modify: `daemon/internal/daemon/queue.go:389-415`

- [ ] **Step 1: Read current `flush` to confirm format**

```bash
sed -n '385,415p' daemon/internal/daemon/queue.go
```

Expected: `MarshalIndent(envelope, "", "  ")` followed by `MkdirAll`, `os.WriteFile(tmp,...)`, `os.Rename(tmp, q.path)`.

- [ ] **Step 2: Replace the body of `flush`**

Find the block in `daemon/internal/daemon/queue.go` (currently lines 389-415):

```go
// flush writes the current queue to disk atomically.
// Caller must hold q.mu.
func (q *Queue) flush() error {
	envelope := queueData{Tasks: q.tasks}
	if envelope.Tasks == nil {
		envelope.Tasks = []*Task{}
	}

	data, err := json.MarshalIndent(envelope, "", "  ")
	if err != nil {
		return fmt.Errorf("marshalling queue: %w", err)
	}

	dir := filepath.Dir(q.path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("creating queue dir: %w", err)
	}

	tmp := q.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return fmt.Errorf("writing tmp queue: %w", err)
	}

	if err := os.Rename(tmp, q.path); err != nil {
		return fmt.Errorf("renaming tmp queue: %w", err)
	}

	return nil
}
```

Replace with:

```go
// flush writes the current queue to disk atomically.
// Caller must hold q.mu.
func (q *Queue) flush() error {
	envelope := queueData{Tasks: q.tasks}
	if envelope.Tasks == nil {
		envelope.Tasks = []*Task{}
	}

	dir := filepath.Dir(q.path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("creating queue dir: %w", err)
	}

	if err := atomicfs.WriteJSON(q.path, envelope, 0600); err != nil {
		return fmt.Errorf("writing queue: %w", err)
	}
	return nil
}
```

- [ ] **Step 3: Update imports**

In `daemon/internal/daemon/queue.go`, the import block must:
- Add `"github.com/racterub/gobrrr/internal/atomicfs"`
- Drop `"encoding/json"` only if no other reference to it remains in the file (verify with `grep -n 'json\.' daemon/internal/daemon/queue.go` — there are likely other `json.Unmarshal` references, in which case keep the import).

Run after editing:

```bash
cd daemon && goimports -w internal/daemon/queue.go 2>/dev/null || gofmt -w internal/daemon/queue.go
grep -n '"encoding/json"\|"github.com/racterub/gobrrr/internal/atomicfs"' internal/daemon/queue.go
```

Expected: `atomicfs` import present; `encoding/json` either kept (if used elsewhere) or removed.

- [ ] **Step 4: Run the full test gate**

```bash
cd daemon && CGO_ENABLED=0 go build ./... && go test ./... && go vet ./...
```

Expected: PASS. The existing queue tests exercise the persistence path and must continue to pass with no modifications.

- [ ] **Step 5: Commit**

```bash
git add daemon/internal/daemon/queue.go
git commit -m "$(cat <<'EOF'
refactor(daemon): use atomicfs for queue persistence

Queue.flush now delegates the .tmp + rename dance to atomicfs.WriteJSON.
On-disk format is byte-identical (same two-space MarshalIndent, same
0600 perm, same MkdirAll-before-write).

Structural change.

AI-Ratio: 1.0
Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

### Task 3: Migrate `daemon/approvals_store.go`

**Files:**
- Modify: `daemon/internal/daemon/approvals_store.go:42-55`

- [ ] **Step 1: Read current `Save` to confirm format**

```bash
sed -n '40,55p' daemon/internal/daemon/approvals_store.go
```

Expected: `MkdirAll(dir, 0700)`, `MarshalIndent(req, "", "  ")`, `WriteFile(tmp, ...)`, `Rename(tmp, ...)`.

- [ ] **Step 2: Replace the body of `Save`**

Find the block in `daemon/internal/daemon/approvals_store.go`:

```go
// Save writes an approval atomically (.tmp + rename) with mode 0600.
func (s *ApprovalStore) Save(req *ApprovalRequest) error {
	if err := os.MkdirAll(s.dir(), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(req, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path(req.ID) + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path(req.ID))
}
```

Replace with:

```go
// Save writes an approval atomically (.tmp + rename) with mode 0600.
func (s *ApprovalStore) Save(req *ApprovalRequest) error {
	if err := os.MkdirAll(s.dir(), 0700); err != nil {
		return err
	}
	return atomicfs.WriteJSON(s.path(req.ID), req, 0600)
}
```

- [ ] **Step 3: Update imports**

In `daemon/internal/daemon/approvals_store.go`:
- Add `"github.com/racterub/gobrrr/internal/atomicfs"`
- Drop `"encoding/json"` if no other `json.` reference remains (`grep -n 'json\.' daemon/internal/daemon/approvals_store.go` — `Load` uses `json.Unmarshal`, so the import stays).

Run:

```bash
cd daemon && gofmt -w internal/daemon/approvals_store.go
```

- [ ] **Step 4: Run the full test gate**

```bash
cd daemon && CGO_ENABLED=0 go build ./... && go test ./... && go vet ./...
```

Expected: PASS. Approvals tests live in `internal/daemon` and exercise Save/Load round-trip.

- [ ] **Step 5: Commit**

```bash
git add daemon/internal/daemon/approvals_store.go
git commit -m "$(cat <<'EOF'
refactor(daemon): use atomicfs for approval persistence

ApprovalStore.Save now delegates to atomicfs.WriteJSON. Same MkdirAll
guard, same MarshalIndent shape, same 0600 perm.

Structural change.

AI-Ratio: 1.0
Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Phase 2 — Standalone-package migrations

### Task 4: Migrate `scheduler/scheduler.go`

**Files:**
- Modify: `daemon/internal/scheduler/scheduler.go:80-93`

- [ ] **Step 1: Read current `flush`**

```bash
sed -n '78,95p' daemon/internal/scheduler/scheduler.go
```

Expected: `MarshalIndent(s.schedules, "", "  ")`, `MkdirAll`, `WriteFile`, `Rename`.

- [ ] **Step 2: Replace the body of `flush`**

Find:

```go
func (s *Scheduler) flush() error {
	data, err := json.MarshalIndent(s.schedules, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.filePath + ".tmp"
	if err := os.MkdirAll(filepath.Dir(s.filePath), 0700); err != nil {
		return err
	}
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, s.filePath)
}
```

Replace with:

```go
func (s *Scheduler) flush() error {
	if err := os.MkdirAll(filepath.Dir(s.filePath), 0700); err != nil {
		return err
	}
	return atomicfs.WriteJSON(s.filePath, s.schedules, 0600)
}
```

- [ ] **Step 3: Update imports**

In `daemon/internal/scheduler/scheduler.go`:
- Add `"github.com/racterub/gobrrr/internal/atomicfs"`
- Drop `"encoding/json"` only if no other `json.` reference remains (verify with `grep -n 'json\.' daemon/internal/scheduler/scheduler.go`).

Run:

```bash
cd daemon && gofmt -w internal/scheduler/scheduler.go
```

- [ ] **Step 4: Run the full test gate**

```bash
cd daemon && CGO_ENABLED=0 go build ./... && go test ./... && go vet ./...
```

Expected: PASS. `scheduler_test.go` exercises Create / Remove / persistence round-trip.

- [ ] **Step 5: Commit**

```bash
git add daemon/internal/scheduler/scheduler.go
git commit -m "$(cat <<'EOF'
refactor(scheduler): use atomicfs for schedules.json persistence

flush now delegates to atomicfs.WriteJSON. MkdirAll guard kept; on-disk
format unchanged (two-space MarshalIndent, 0600).

Structural change.

AI-Ratio: 1.0
Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

### Task 5: Migrate `memory/store.go`

**Files:**
- Modify: `daemon/internal/memory/store.go:206-212` (`writeEntry`)
- Modify: `daemon/internal/memory/store.go:241-257` (`persistIndex` + `atomicWrite`)

Memory uses **compact** `json.Marshal` (no indent) for both per-entry files and the index, so both call sites migrate to `atomicfs.WriteFile` (not `WriteJSON`) to preserve byte-identical on-disk output. There are **two** call sites of the local `atomicWrite` helper — `writeEntry` at line 211 and `persistIndex` at line 247 — both must be migrated in the same commit before the helper is deleted.

- [ ] **Step 1: Confirm both call sites and the helper**

```bash
grep -n 'atomicWrite' daemon/internal/memory/store.go
```

Expected three lines: the two call sites and the function definition.

- [ ] **Step 2: Replace `writeEntry`**

Find:

```go
func (s *Store) writeEntry(e *Entry) error {
	data, err := json.Marshal(e)
	if err != nil {
		return err
	}
	return atomicWrite(s.entryPath(e.ID), data)
}
```

Replace with:

```go
func (s *Store) writeEntry(e *Entry) error {
	data, err := json.Marshal(e)
	if err != nil {
		return err
	}
	return atomicfs.WriteFile(s.entryPath(e.ID), data, 0600)
}
```

- [ ] **Step 3: Replace `persistIndex` and delete `atomicWrite`**

Find:

```go
// persistIndex writes the index atomically. Caller must hold s.mu (write).
func (s *Store) persistIndex() error {
	data, err := json.Marshal(s.idx)
	if err != nil {
		return err
	}
	return atomicWrite(s.indexPath(), data)
}

// atomicWrite writes data to path via a temp file + rename.
func atomicWrite(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
```

Replace with:

```go
// persistIndex writes the index atomically. Caller must hold s.mu (write).
func (s *Store) persistIndex() error {
	data, err := json.Marshal(s.idx)
	if err != nil {
		return err
	}
	return atomicfs.WriteFile(s.indexPath(), data, 0600)
}
```

(The local `atomicWrite` function is deleted entirely.)

- [ ] **Step 4: Confirm no remaining `atomicWrite` references**

```bash
grep -n 'atomicWrite' daemon/internal/memory/store.go
```

Expected: no output. If any line remains, it must be migrated before the commit lands.

- [ ] **Step 5: Update imports**

In `daemon/internal/memory/store.go`:
- Add `"github.com/racterub/gobrrr/internal/atomicfs"`
- Keep `"encoding/json"` (used for `Marshal` / `Unmarshal` elsewhere).
- Keep `"os"` (used by `os.ReadFile` etc.).

Run:

```bash
cd daemon && gofmt -w internal/memory/store.go
```

- [ ] **Step 6: Run the full test gate**

```bash
cd daemon && CGO_ENABLED=0 go build ./... && go test ./... && go vet ./...
```

Expected: PASS. `memory/store_test.go` covers persistence round-trip for both entry files and the index.

- [ ] **Step 7: Commit**

```bash
git add daemon/internal/memory/store.go
git commit -m "$(cat <<'EOF'
refactor(memory): use atomicfs for entry and index persistence

Both call sites of the local atomicWrite helper — writeEntry (per-entry
file) and persistIndex (index file) — now delegate to atomicfs.WriteFile.
The local helper is removed. Compact json.Marshal preserved at both
sites so on-disk output is byte-identical.

Structural change.

AI-Ratio: 1.0
Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

### Task 6: Migrate `skills/bundled.go`

**Files:**
- Modify: `daemon/internal/skills/bundled.go:97-110`

- [ ] **Step 1: Read current code**

```bash
sed -n '95,112p' daemon/internal/skills/bundled.go
```

Expected: `MarshalIndent(meta, "", "  ")` then `writeAtomic(filepath.Join(dst, "_meta.json"), data, 0600)`. Local `writeAtomic` defined directly below.

- [ ] **Step 2: Replace call site and delete local helper**

Find:

```go
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(filepath.Join(dst, "_meta.json"), data, 0600)
}

func writeAtomic(path string, data []byte, mode os.FileMode) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
```

Replace with:

```go
	return atomicfs.WriteJSON(filepath.Join(dst, "_meta.json"), meta, 0600)
}
```

(The marshal call is folded into `WriteJSON`; the local `writeAtomic` is deleted.)

- [ ] **Step 3: Update imports**

In `daemon/internal/skills/bundled.go`:
- Add `"github.com/racterub/gobrrr/internal/atomicfs"`
- Drop `"encoding/json"` only if no other `json.` reference remains in the file (run `grep -n 'json\.' daemon/internal/skills/bundled.go`).
- Drop `"os"` only if no other `os.` reference remains (it likely is still used by other functions; keep it if so).

Run:

```bash
cd daemon && gofmt -w internal/skills/bundled.go
```

- [ ] **Step 4: Run the full test gate**

```bash
cd daemon && CGO_ENABLED=0 go build ./... && go test ./... && go vet ./...
```

Expected: PASS. `skills/bundled_test.go` exercises the install path.

- [ ] **Step 5: Commit**

```bash
git add daemon/internal/skills/bundled.go
git commit -m "$(cat <<'EOF'
refactor(skills): use atomicfs for bundled _meta.json writes

Replaces the local writeAtomic helper with atomicfs.WriteJSON. Same
two-space MarshalIndent shape and 0600 perm — output is byte-identical.

Structural change.

AI-Ratio: 1.0
Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Phase 3 — Cross-file + complex migrations

### Task 7: Migrate `clawhub` (installer.go + commit.go)

**Files:**
- Modify: `daemon/internal/clawhub/installer.go:352-358` (delete local helper)
- Modify: `daemon/internal/clawhub/commit.go:122,211` (replace call sites)

`clawhub/commit.go` calls `writeAtomic` in two places (`_meta.json` write at line 122 and `_lock.json` update at line 211). The helper itself lives in `installer.go`. Both files share the package, so they migrate in a single commit — deleting the helper without updating callers leaves the package un-compilable.

- [ ] **Step 1: Read both call sites and the helper**

```bash
sed -n '118,124p' daemon/internal/clawhub/commit.go
sed -n '205,213p' daemon/internal/clawhub/commit.go
sed -n '350,360p' daemon/internal/clawhub/installer.go
```

Expected:
- `commit.go:122` — `writeAtomic(filepath.Join(dst, "_meta.json"), metaBytes, 0600)`, with `metaBytes` produced by `json.MarshalIndent(meta, "", "  ")` just above.
- `commit.go:211` — `writeAtomic(path, out, 0600)`, with `out` from `json.MarshalIndent(lf, "", "  ")` just above.
- `installer.go:352-358` — local `writeAtomic` definition.

- [ ] **Step 2: Replace `commit.go:122` call site**

Find in `daemon/internal/clawhub/commit.go`:

```go
	metaBytes, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	if err := writeAtomic(filepath.Join(dst, "_meta.json"), metaBytes, 0600); err != nil {
		return err
	}
```

Replace with:

```go
	if err := atomicfs.WriteJSON(filepath.Join(dst, "_meta.json"), meta, 0600); err != nil {
		return err
	}
```

(`metaBytes` and the inline `MarshalIndent` are absorbed into `WriteJSON`. If `metaBytes` is referenced later in the function, keep the marshal call and use `atomicfs.WriteFile(path, metaBytes, 0600)` instead — verify with `grep -n metaBytes daemon/internal/clawhub/commit.go`.)

- [ ] **Step 3: Replace `commit.go:211` call site**

Find:

```go
	out, err := json.MarshalIndent(lf, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(path, out, 0600)
```

Replace with:

```go
	return atomicfs.WriteJSON(path, lf, 0600)
```

- [ ] **Step 4: Delete the local helper in `installer.go`**

Find in `daemon/internal/clawhub/installer.go`:

```go
func writeAtomic(path string, data []byte, mode os.FileMode) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
```

Delete this function entirely.

- [ ] **Step 5: Confirm no remaining `writeAtomic` callers**

```bash
grep -n 'writeAtomic' daemon/internal/clawhub/
```

Expected: no output. If any remain (e.g. inside `installer.go` itself, which we did not inspect for additional call sites), replace each with `atomicfs.WriteJSON` (for JSON payloads) or `atomicfs.WriteFile` (for raw bytes).

- [ ] **Step 6: Update imports in both files**

In `daemon/internal/clawhub/commit.go`:
- Add `"github.com/racterub/gobrrr/internal/atomicfs"`
- Drop `"encoding/json"` only if no other `json.` reference remains (run `grep -n 'json\.' daemon/internal/clawhub/commit.go`; `updateLock` still uses `json.Unmarshal`, so keep it).

In `daemon/internal/clawhub/installer.go`:
- Drop `"os"` only if no other `os.` reference remains (highly unlikely — installer.go uses `os.Stat` and friends extensively; keep it).

Run:

```bash
cd daemon && gofmt -w internal/clawhub/commit.go internal/clawhub/installer.go
```

- [ ] **Step 7: Run the full test gate**

```bash
cd daemon && CGO_ENABLED=0 go build ./... && go test ./... && go vet ./...
```

Expected: PASS. `clawhub/commit_test.go` and `clawhub/installer_test.go` cover the install + commit paths.

- [ ] **Step 8: Commit**

```bash
git add daemon/internal/clawhub/installer.go daemon/internal/clawhub/commit.go
git commit -m "$(cat <<'EOF'
refactor(clawhub): use atomicfs for skill commit writes

Replaces both writeAtomic call sites in commit.go (the per-skill
_meta.json write and the global _lock.json update) with
atomicfs.WriteJSON. Deletes the local writeAtomic helper from
installer.go now that no caller references it. Same two-space
MarshalIndent and 0600 perm — output is byte-identical.

Structural change.

AI-Ratio: 1.0
Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

### Task 8: Migrate `google/auth.go`

**Files:**
- Modify: `daemon/internal/google/auth.go:100, 272, 282-288`

Google's three call sites are heterogeneous — the vault write (`auth.go:100`) is **binary ciphertext**, and the accounts index (`auth.go:272`) uses **four-space** `MarshalIndent`. Both must use `atomicfs.WriteFile` (not `WriteJSON`) to preserve byte-identical on-disk output.

- [ ] **Step 1: Read all three sites**

```bash
sed -n '98,103p' daemon/internal/google/auth.go
sed -n '265,278p' daemon/internal/google/auth.go
sed -n '278,290p' daemon/internal/google/auth.go
```

Expected:
- Line 100 — `writeAtomic(encPath, ciphertext, 0600)` (binary).
- Line 272 — `writeAtomic(idxPath, data, 0600)`, with `data` from `json.MarshalIndent(idx, "", "    ")` (four-space) just above.
- Line 282-288 — local `writeAtomic` definition with the documented "tracked separately (Refactor #6)" comment.

- [ ] **Step 2: Replace the vault write at line 100**

Find:

```go
	if err := writeAtomic(encPath, ciphertext, 0600); err != nil {
```

Replace with:

```go
	if err := atomicfs.WriteFile(encPath, ciphertext, 0600); err != nil {
```

(The error-wrapping `fmt.Errorf` immediately following stays unchanged.)

- [ ] **Step 3: Replace the accounts-index write at line 272**

Find:

```go
	if err := writeAtomic(idxPath, data, 0600); err != nil {
		return fmt.Errorf("google: write accounts index: %w", err)
	}
```

Replace with:

```go
	if err := atomicfs.WriteFile(idxPath, data, 0600); err != nil {
		return fmt.Errorf("google: write accounts index: %w", err)
	}
```

The four-space `MarshalIndent` call just above is **kept verbatim** to preserve the on-disk indent shape.

- [ ] **Step 4: Delete the local `writeAtomic` helper**

Find:

```go
// writeAtomic writes data to path via a sibling .tmp file plus rename so
// readers never observe a partial write. Inline duplicate of the helper
// in clawhub/installer.go and daemon/approvals_store.go — consolidation
// into a shared atomicfs package is tracked separately (Refactor #6).
func writeAtomic(path string, data []byte, mode os.FileMode) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
```

Delete this entire block (comment included — Refactor #6 is now done, so the comment is stale).

- [ ] **Step 5: Confirm no remaining `writeAtomic` callers in the file**

```bash
grep -n 'writeAtomic' daemon/internal/google/auth.go
```

Expected: no output.

- [ ] **Step 6: Update imports**

In `daemon/internal/google/auth.go`:
- Add `"github.com/racterub/gobrrr/internal/atomicfs"`
- Keep `"encoding/json"` (still used for `MarshalIndent` and `Unmarshal`).
- Keep `"os"` (used by `os.ReadFile`, `os.MkdirAll`, etc.).

Run:

```bash
cd daemon && gofmt -w internal/google/auth.go
```

- [ ] **Step 7: Final repo-wide invariant grep**

This is the acceptance check from `TODO.md` — no `os.WriteFile(...".tmp"...)` patterns survive outside `internal/atomicfs`:

```bash
cd daemon
grep -rn '\.tmp"' --include='*.go' internal/ cmd/ | grep -v 'internal/atomicfs/'
```

Expected: no output (or only false-positives unrelated to atomic writes — inspect each remaining hit). If any genuine `.tmp + Rename` pattern remains, that file must also be migrated before the commit lands.

Also confirm no orphan local helpers:

```bash
grep -rn 'func writeAtomic\|func atomicWrite' --include='*.go' daemon/
```

Expected: no output.

- [ ] **Step 8: Run the full test gate**

```bash
cd daemon && CGO_ENABLED=0 go build ./... && go test ./... && go vet ./...
```

Expected: PASS. `google/auth_test.go` covers credential save/load round-trip; the heavy `internal/google` test suite (~47s) exercises the full Google integration.

- [ ] **Step 9: Commit**

```bash
git add daemon/internal/google/auth.go
git commit -m "$(cat <<'EOF'
refactor(google): use atomicfs for credential and index writes

Replaces all three writeAtomic call sites in auth.go — the encrypted
vault write (binary ciphertext) and the accounts index write (custom
four-space JSON) — with atomicfs.WriteFile, and deletes the local
helper plus its now-stale "tracked separately (Refactor #6)" comment.
JSON marshalling stays inline to preserve the four-space indent shape,
so on-disk output is byte-identical.

This is the last #6a migration: no .tmp + rename patterns remain
outside internal/atomicfs.

Structural change.

AI-Ratio: 1.0
Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Definition of Done

- [ ] Branch `refactor/6a-atomicfs-extract` exists with eight commits (one helper + seven migrations).
- [ ] `daemon/internal/atomicfs/` exists with `WriteFile`, `WriteJSON`, and tests covering round-trip, perm enforcement, no-leftover-tmp, overwrite, missing-dir-fails, and JSON shape.
- [ ] `grep -rn '\.tmp"' --include='*.go' daemon/internal/ daemon/cmd/ | grep -v 'internal/atomicfs/'` returns nothing (or only confirmed-unrelated hits).
- [ ] `grep -rn 'func writeAtomic\|func atomicWrite' --include='*.go' daemon/` returns nothing.
- [ ] `cd daemon && CGO_ENABLED=0 go build ./... && go test ./... && go vet ./...` passes at branch HEAD.
- [ ] Each commit body contains `Structural change.`, `AI-Ratio: 1.0`, and the `Co-Authored-By` trailer.
- [ ] On-disk JSON byte-identical to master for every migrated file (verified by running before/after diff on a populated `~/.gobrrr/` if available, or by inspecting that the `MarshalIndent` parameters match).

After this branch merges, only Refactor #6b (parent-directory `fsync` inside `atomicfs.WriteFile` plus a test) remains in the structural batch.
