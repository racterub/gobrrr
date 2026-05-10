# Skill Registry Refresh After Install — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Refresh the in-memory `skills.Registry` after a successful skill install commit so newly-installed skills are visible to the next worker spawn without a daemon restart.

**Architecture:** Add a `refresherLike` dependency to `skillInstallHandler` mirroring the existing `committerLike` pattern. Call `Refresh()` after `Commit()` succeeds and only on approve/skip-binary decisions. Best-effort: log refresh failures but return success (the skill is already on disk).

**Tech Stack:** Go 1.22+, testify, in-memory `skills.Registry` (mutex-guarded slice).

**Spec:** [`docs/superpowers/specs/2026-05-10-skill-registry-refresh-design.md`](../specs/2026-05-10-skill-registry-refresh-design.md)

---

## File Structure

- Modify: `daemon/internal/daemon/skill_install_handler.go` — add `refresherLike` interface, `refresher` field, refresh-after-commit logic, update testing constructor signature.
- Modify: `daemon/internal/daemon/daemon.go:98` — pass `skillReg` into the handler at registration time.
- Modify: `daemon/internal/daemon/skill_e2e_test.go:161` — update the existing caller of the testing constructor to pass a real `skills.Registry`.
- Create: `daemon/internal/daemon/skill_install_handler_test.go` — new internal-package test file with five unit cases.

Two tasks: structural plumbing first, then TDD the behavior. Tidy First — no mixing.

---

### Task 1: Plumb `refresherLike` dependency through the install handler (structural)

**Files:**
- Modify: `daemon/internal/daemon/skill_install_handler.go` (entire file)
- Modify: `daemon/internal/daemon/daemon.go:98`
- Modify: `daemon/internal/daemon/skill_e2e_test.go:158-161`

- [ ] **Step 1.1: Replace `daemon/internal/daemon/skill_install_handler.go` with the structurally-extended version**

```go
package daemon

import (
	"encoding/json"
	"fmt"

	"github.com/racterub/gobrrr/internal/clawhub"
)

// committerLike is the minimal contract skillInstallHandler needs. Exists so
// tests can inject a fake.
type committerLike interface {
	Commit(req clawhub.InstallRequest, decision clawhub.Decision) error
}

// refresherLike is the minimal contract for reloading the skill registry after
// a successful install. *skills.Registry satisfies this directly.
type refresherLike interface {
	Refresh() error
}

// skillInstallHandler implements ApprovalHandler for kind="skill_install".
// It unmarshals the payload (a clawhub.InstallRequest) and delegates to the
// committer with a decision mapped from the generic string. After a successful
// approve commit, it triggers a registry refresh so newly-installed skills are
// visible to subsequent worker spawns without a daemon restart.
type skillInstallHandler struct {
	committer committerLike
	refresher refresherLike
}

// NewSkillInstallHandlerForTesting exposes the internal handler to other
// packages' tests. Production code registers via the dispatcher wiring in
// daemon.New.
func NewSkillInstallHandlerForTesting(c committerLike, r refresherLike) ApprovalHandler {
	return &skillInstallHandler{committer: c, refresher: r}
}

func (h *skillInstallHandler) Handle(req *ApprovalRequest, decision string) error {
	var installReq clawhub.InstallRequest
	if err := json.Unmarshal(req.Payload, &installReq); err != nil {
		return fmt.Errorf("skill_install: bad payload: %w", err)
	}
	var d clawhub.Decision
	switch decision {
	case "approve":
		d = clawhub.Decision{Approve: true}
	case "skip_binary":
		d = clawhub.Decision{Approve: true, SkipBinary: true}
	case "deny":
		d = clawhub.Decision{Approve: false}
	default:
		return fmt.Errorf("skill_install: unknown decision %q", decision)
	}
	return h.committer.Commit(installReq, d)
}
```

This adds the `refresherLike` interface and the `refresher` field, and threads the parameter through `NewSkillInstallHandlerForTesting`. The `Handle` method body is **unchanged behavior** — Refresh isn't called yet. That's Task 2.

- [ ] **Step 1.2: Update `daemon/internal/daemon/daemon.go:98` to pass `skillReg`**

Find this line:

```go
approvals.Register("skill_install", &skillInstallHandler{committer: committer})
```

Replace with:

```go
approvals.Register("skill_install", &skillInstallHandler{
	committer: committer,
	refresher: skillReg,
})
```

`skillReg` is in scope at this point — created at `daemon.go:76` via `skills.NewRegistry(skillsRoot)`.

- [ ] **Step 1.3: Update `daemon/internal/daemon/skill_e2e_test.go` to pass a real registry to the testing constructor**

Find lines 158-161:

```go
cm := clawhub.NewCommitter(skillsRoot, nil)
store := daemon.NewApprovalStore(approvalsRoot)
dispatcher := daemon.NewApprovalDispatcher(store)
dispatcher.Register("skill_install", daemon.NewSkillInstallHandlerForTesting(cm))
```

Replace with:

```go
cm := clawhub.NewCommitter(skillsRoot, nil)
store := daemon.NewApprovalStore(approvalsRoot)
dispatcher := daemon.NewApprovalDispatcher(store)
reg := skills.NewRegistry(skillsRoot)
require.NoError(t, reg.Refresh())
dispatcher.Register("skill_install", daemon.NewSkillInstallHandlerForTesting(cm, reg))
```

The `skills` package is already imported in this test file (line 22).

- [ ] **Step 1.4: Build the daemon to confirm structural change compiles**

Run: `cd daemon && go build ./...`
Expected: clean exit, no errors.

- [ ] **Step 1.5: Run the existing test suite — should still pass with no behavior change**

Run: `cd daemon && go test ./...`
Expected: all tests pass. The `skill_e2e_test.go` E2E test still passes because (a) the registry refresh in the test is a no-op for the assertion (it checks file existence on disk, not registry contents), (b) `Handle` doesn't actually call Refresh yet.

- [ ] **Step 1.6: Commit (structural change only)**

```bash
git add daemon/internal/daemon/skill_install_handler.go daemon/internal/daemon/daemon.go daemon/internal/daemon/skill_e2e_test.go
git commit -m "$(cat <<'EOF'
refactor(skills): add refresher dependency to install handler

Plumb a refresherLike interface and refresher field through
skillInstallHandler. Wire skills.Registry into the production
dispatcher registration in daemon.New, and update the e2e test caller
to pass a real registry.

No behavior change yet: Handle does not call Refresh in this commit.
The registry-refresh logic and its tests land in the next commit.

AI-Ratio: 1.0
Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

### Task 2: TDD the refresh-on-approve behavior (behavioral)

**Files:**
- Create: `daemon/internal/daemon/skill_install_handler_test.go`
- Modify: `daemon/internal/daemon/skill_install_handler.go` (Handle body + log import)

- [ ] **Step 2.1: Write the failing test file**

Create `daemon/internal/daemon/skill_install_handler_test.go` with this exact content:

```go
package daemon

import (
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/racterub/gobrrr/internal/clawhub"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeCommitter struct {
	mu      sync.Mutex
	calls   int
	lastDec clawhub.Decision
	err     error
}

func (f *fakeCommitter) Commit(req clawhub.InstallRequest, d clawhub.Decision) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.lastDec = d
	return f.err
}

type fakeRefresher struct {
	mu    sync.Mutex
	calls int
	err   error
}

func (f *fakeRefresher) Refresh() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return f.err
}

func newTestInstallRequest(t *testing.T) *ApprovalRequest {
	t.Helper()
	payload, err := json.Marshal(clawhub.InstallRequest{Slug: "test", Version: "1.0.0"})
	require.NoError(t, err)
	return &ApprovalRequest{ID: "test-id", Payload: payload}
}

func TestSkillInstallHandler_ApproveCommitsAndRefreshes(t *testing.T) {
	c, r := &fakeCommitter{}, &fakeRefresher{}
	h := &skillInstallHandler{committer: c, refresher: r}

	err := h.Handle(newTestInstallRequest(t), "approve")

	require.NoError(t, err)
	assert.Equal(t, 1, c.calls)
	assert.True(t, c.lastDec.Approve)
	assert.Equal(t, 1, r.calls, "Refresh must be called after a successful approve commit")
}

func TestSkillInstallHandler_SkipBinaryCommitsAndRefreshes(t *testing.T) {
	c, r := &fakeCommitter{}, &fakeRefresher{}
	h := &skillInstallHandler{committer: c, refresher: r}

	err := h.Handle(newTestInstallRequest(t), "skip_binary")

	require.NoError(t, err)
	assert.Equal(t, 1, c.calls)
	assert.True(t, c.lastDec.Approve)
	assert.True(t, c.lastDec.SkipBinary)
	assert.Equal(t, 1, r.calls, "Refresh must be called after a successful skip_binary commit")
}

func TestSkillInstallHandler_DenySkipsRefresh(t *testing.T) {
	c, r := &fakeCommitter{}, &fakeRefresher{}
	h := &skillInstallHandler{committer: c, refresher: r}

	err := h.Handle(newTestInstallRequest(t), "deny")

	require.NoError(t, err)
	assert.Equal(t, 1, c.calls)
	assert.False(t, c.lastDec.Approve)
	assert.Equal(t, 0, r.calls, "Refresh must not run on deny — nothing landed on disk")
}

func TestSkillInstallHandler_CommitErrorSkipsRefresh(t *testing.T) {
	commitErr := errors.New("commit failed")
	c := &fakeCommitter{err: commitErr}
	r := &fakeRefresher{}
	h := &skillInstallHandler{committer: c, refresher: r}

	err := h.Handle(newTestInstallRequest(t), "approve")

	require.Error(t, err)
	assert.ErrorIs(t, err, commitErr)
	assert.Equal(t, 1, c.calls)
	assert.Equal(t, 0, r.calls, "Refresh must not run when Commit fails — registry has nothing new to load")
}

func TestSkillInstallHandler_RefreshErrorIsSwallowed(t *testing.T) {
	c := &fakeCommitter{}
	r := &fakeRefresher{err: errors.New("refresh failed")}
	h := &skillInstallHandler{committer: c, refresher: r}

	err := h.Handle(newTestInstallRequest(t), "approve")

	require.NoError(t, err, "Refresh failure must be best-effort: skill is already on disk")
	assert.Equal(t, 1, c.calls)
	assert.Equal(t, 1, r.calls)
}
```

The fakes are mutex-guarded to match the existing `fakeHandler` pattern in `approvals_dispatcher_test.go`.

- [ ] **Step 2.2: Run the new tests to confirm they fail in the expected ways**

Run: `cd daemon && go test ./internal/daemon/ -run TestSkillInstallHandler -v`

Expected:
- `TestSkillInstallHandler_ApproveCommitsAndRefreshes` — **FAIL** (asserts `r.calls == 1` but Refresh isn't called yet)
- `TestSkillInstallHandler_SkipBinaryCommitsAndRefreshes` — **FAIL** (same reason)
- `TestSkillInstallHandler_DenySkipsRefresh` — PASS (Refresh isn't called either way)
- `TestSkillInstallHandler_CommitErrorSkipsRefresh` — PASS (Commit fails, Refresh wouldn't be reached)
- `TestSkillInstallHandler_RefreshErrorIsSwallowed` — PASS vacuously (Refresh isn't called, so no error to swallow)

Two failures means the tests are gating on the actual behavior we want to add.

- [ ] **Step 2.3: Implement the refresh-on-approve logic in `Handle`**

In `daemon/internal/daemon/skill_install_handler.go`, add `"log"` to the import block and replace the final return statement.

Imports become:

```go
import (
	"encoding/json"
	"fmt"
	"log"

	"github.com/racterub/gobrrr/internal/clawhub"
)
```

Replace this:

```go
	return h.committer.Commit(installReq, d)
}
```

With this:

```go
	if err := h.committer.Commit(installReq, d); err != nil {
		return err
	}
	if d.Approve && h.refresher != nil {
		if err := h.refresher.Refresh(); err != nil {
			log.Printf("skill_install: registry refresh failed after commit: %v", err)
		}
	}
	return nil
}
```

The `h.refresher != nil` check is defensive — production wires a real registry in Task 1, but a nil refresher must not panic if some future caller forgets.

- [ ] **Step 2.4: Re-run the new tests — all five must pass**

Run: `cd daemon && go test ./internal/daemon/ -run TestSkillInstallHandler -v`
Expected: all five tests PASS.

- [ ] **Step 2.5: Run the full daemon test suite to confirm no regressions**

Run: `cd daemon && go test ./...`
Expected: all tests pass, including the existing `skill_e2e_test.go` E2E test (which now exercises the real Refresh path through a real `skills.Registry`).

- [ ] **Step 2.6: Commit (behavioral change with tests)**

```bash
git add daemon/internal/daemon/skill_install_handler.go daemon/internal/daemon/skill_install_handler_test.go
git commit -m "$(cat <<'EOF'
fix(skills): refresh registry after install commit

skillInstallHandler now calls refresher.Refresh() after a successful
approve/skip_binary commit, so workers spawned after an install see the
new skill in <available_skills> and get its permissions merged into
their per-task settings.json without a daemon restart.

Refresh failures are best-effort: the skill is already on disk and the
next daemon start picks it up. Failing the handler would surface as an
"install failed" SSE event for a skill that is in fact installed.

Five unit tests cover: approve+commit-ok, skip_binary+commit-ok, deny,
commit-error, and refresh-error-swallowed.

Closes Refactor #17.

AI-Ratio: 1.0
Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

- [ ] **Step 2.7: Remove Refactor #17 from `TODO.md`**

Acceptance criteria are met after Step 2.6 lands. Per the project's todo-tracking rule, demonstrably-complete items are deleted outright. Open `TODO.md` and remove the entire `## Refactor #17 — Refresh skill registry after install commit — 2026-04-26` section.

Run: `git diff TODO.md` — only the Refactor #17 section should be removed.

- [ ] **Step 2.8: Commit the TODO removal**

```bash
git add TODO.md
git commit -m "$(cat <<'EOF'
docs(todo): remove completed Refactor #17

Skill registry refresh after install commit is implemented and tested.

AI-Ratio: 1.0
Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Acceptance criteria recap

After Task 2 completes:
- [ ] `skillInstallHandler` has a `refresher refresherLike` field; `Handle` calls `refresher.Refresh()` after a successful approve/skip_binary commit and skips it on deny or commit error.
- [ ] `daemon.New` wires `skillReg` into the handler.
- [ ] Five unit tests in `skill_install_handler_test.go` pass.
- [ ] `cd daemon && go test ./...` passes.
- [ ] Refactor #17 entry is removed from `TODO.md`.
- [ ] Manual smoke test on the LXC: approve a real skill install through the daemon, then submit a task and verify the new skill shows up in the worker prompt without restarting `gobrrr.service`. (Runs once after merge; not gated by this plan.)
