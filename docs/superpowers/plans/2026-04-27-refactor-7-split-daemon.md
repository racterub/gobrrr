# Refactor #7 — Split `daemon.go` by Route Concern Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Split the 1109-line `daemon/internal/daemon/daemon.go` into per-concern route files so reviewers can navigate handlers without scrolling through unrelated code, while keeping behavior, signatures, and the route table identical.

**Architecture:** Single structural commit on branch `refactor/7-split-daemon`. All route handlers and their request types move into seven new files within the same `package daemon`: `routes_tasks.go`, `routes_memory.go`, `routes_gmail.go`, `routes_gcal.go`, `routes_session.go`, `routes_schedule.go`, and `health.go`. `handleListSkills` joins the existing `skill_routes.go`. Because every file shares the same package, function/method receivers and identifiers do not change — only the file they live in does. `daemon.go` shrinks to ~290 lines containing the `Daemon` struct, `New()` (with its full route registration table), and `Run()`.

**Tech Stack:** Go 1.21+, stdlib only. No new dependencies. Existing imports redistribute across files; `goimports` (or `go fmt` + manual trim) cleans the per-file import lists.

**Spec:** `docs/superpowers/specs/2026-04-26-structural-refactor-batch-design.md`

**TODO source:** `TODO.md` "Refactor #7 — Split `daemon.go` (1139 lines) by handler concern"

**Branch:** `refactor/7-split-daemon` (already cut from `master` at `047c61c`)

**Test gate at the end of every task:**

```
cd daemon && CGO_ENABLED=0 go build ./...
cd daemon && go test ./...
cd daemon && go vet ./...
```

**Commit policy:** One single commit at the end (Task 4). Title: `refactor(daemon): split daemon.go by route concern`. Body includes `Structural change.` per the global tidy-first rule, plus `AI-Ratio: 1.0` and `Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>` trailers.

**Why incremental moves but a single commit:** Each task moves a self-contained group and is followed by `go build ./... && go test ./... && go vet ./...` so we never leave the working tree in a broken state. The final commit captures the entire restructure as one logical unit per the design's "one structural commit" rule.

**Acceptance criteria (from spec + TODO):**

- [ ] `daemon.go` ≤ 300 lines.
- [ ] Each new `routes_*.go` file owns one concern.
- [ ] All existing tests pass.
- [ ] `go vet ./...` clean.
- [ ] No signature changes; CLI/HTTP behavior unchanged.

---

## Phase 1: Split daemon.go by route concern (4 tasks)

### Task 1: Extract task and memory routes

**Files:**
- Create: `daemon/internal/daemon/routes_tasks.go`
- Create: `daemon/internal/daemon/routes_memory.go`
- Modify: `daemon/internal/daemon/daemon.go` (delete moved blocks)

**Background:** Task and memory routes are small, self-contained, share only `*Daemon` receiver and the `memory.Store` / `Queue` types in scope. Their request types (`submitTaskRequest`, `saveMemoryRequest`) move with their handlers.

- [ ] **Step 1: Create `routes_tasks.go` with the task handlers**

Cut lines 393-487 from `daemon.go` (the `submitTaskRequest` type plus `handleSubmitTask`, `handleListTasks`, `handleGetTask`, `handleCancelTask`, `handleGetTaskLogs`) and paste them into a new `routes_tasks.go`. The new file starts with:

```go
package daemon

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)
```

Trim any imports the moved code does not actually use (e.g. drop `errors` if not referenced in the moved block; rely on `go vet` and `go build` to flag the unused ones). Keep struct comments and method comments verbatim.

- [ ] **Step 2: Create `routes_memory.go` with the memory handlers**

Cut lines 488-591 from `daemon.go` (the `saveMemoryRequest` type plus `handleSaveMemory`, `handleSearchMemory`, `handleGetMemory`, `handleDeleteMemory`) and paste them into a new `routes_memory.go`. The new file starts with:

```go
package daemon

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)
```

(`strconv` is for `parseLimit`-style query params; verify by inspecting the moved code. `strings` is for `strings.TrimPrefix` of the URL path. Trim if unused.)

- [ ] **Step 3: Run the test gate**

Run:
```bash
cd daemon && CGO_ENABLED=0 go build ./... && go test ./... && go vet ./...
```
Expected: build succeeds, all tests pass, vet clean. If `go vet` flags an unused import in either new file, remove it. If it flags an unused import remaining in `daemon.go`, remove from there too.

- [ ] **Step 4: Confirm `daemon.go` line count dropped**

Run:
```bash
wc -l daemon/internal/daemon/daemon.go
```
Expected: ~915 lines (down from 1109 by ~194 lines for tasks + memory).

---

### Task 2: Extract gmail and gcal routes

**Files:**
- Create: `daemon/internal/daemon/routes_gmail.go`
- Create: `daemon/internal/daemon/routes_gcal.go`
- Modify: `daemon/internal/daemon/daemon.go`

**Background:** Gmail and Calendar share a permission helper (`checkWritePermission`) used by gmail send/reply and gcal create/update/delete. Place `checkWritePermission` in `routes_gmail.go` (alphabetically first; gcal handlers reach it via same-package access). Both files import `internal/google` for the API surface.

- [ ] **Step 1: Create `routes_gmail.go`**

Cut lines 592-775 from the **current** `daemon.go` (note: line numbers drift after Task 1; re-locate by anchor — the block starts at `type gmailListRequest struct` and ends at the close of `handleGmailReply`). The block contains:
- `gmailListRequest`, `gmailReadRequest`, `gmailSendRequest`, `gmailReplyRequest` types
- `requireGmail` helper
- `checkWritePermission` helper
- `handleGmailList`, `handleGmailRead`, `handleGmailSend`, `handleGmailReply`

Paste into a new `routes_gmail.go`. The new file starts with:

```go
package daemon

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/racterub/gobrrr/internal/google"
)
```

- [ ] **Step 2: Create `routes_gcal.go`**

Cut the next block (anchor: starts at `type gcalAccountRequest struct`, ends at the close of `handleGcalDelete`). Contents:
- `gcalAccountRequest`, `gcalGetRequest`, `gcalCreateRequest`, `gcalUpdateRequest`, `gcalDeleteRequest` types
- `requireCalendar` helper
- `handleGcalToday`, `handleGcalWeek`, `handleGcalGet`, `handleGcalCreate`, `handleGcalUpdate`, `handleGcalDelete`

Paste into a new `routes_gcal.go`. The new file starts with:

```go
package daemon

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/racterub/gobrrr/internal/google"
)
```

(`time` is for the today/week date math inside the gcal handlers — verify by inspection.)

- [ ] **Step 3: Run the test gate**

Run:
```bash
cd daemon && CGO_ENABLED=0 go build ./... && go test ./... && go vet ./...
```
Expected: build green, tests green, vet clean. Particular files to watch: `gmail_test.go` and `calendar_test.go` reference handlers as `d.handleGmail*` / `d.handleGcal*` — same package, still resolves.

- [ ] **Step 4: Confirm `daemon.go` line count dropped**

Run:
```bash
wc -l daemon/internal/daemon/daemon.go
```
Expected: ~500 lines (down by ~415 from start: 194 tasks/memory + 184 gmail + 225 gcal).

---

### Task 3: Extract session, schedule, skill, and health routes

**Files:**
- Create: `daemon/internal/daemon/routes_session.go`
- Create: `daemon/internal/daemon/routes_schedule.go`
- Create: `daemon/internal/daemon/health.go`
- Modify: `daemon/internal/daemon/skill_routes.go` (add `handleListSkills`)
- Modify: `daemon/internal/daemon/daemon.go`

**Background:** Three smaller route groups land here, plus the orphaned `handleListSkills` (currently in `daemon.go`) joins its sibling handlers in `skill_routes.go`. The health block (`handleHealth`, `warmStatus`, `healthResponse`, `runHealthMonitor`, `updateHeartbeat`) collects all daemon-runtime health concerns into one file — distinct from `healthcheck.go` which holds the *evaluator*. `loadVaultIfAvailable` and `binOnPath` are only used by session handlers and `New()`; they live in `routes_session.go` and remain accessible from `New()` via same-package scope.

- [ ] **Step 1: Create `routes_session.go`**

From the **current** `daemon.go` (line numbers will have drifted), cut by anchor:
- `loadVaultIfAvailable` (function comment + body, currently around old line 999-1010)
- `handleSessionStatus`, `handleSessionStart`, `handleSessionStop`, `handleSessionRestart` (old 1011-1064)
- `binOnPath` (old 1106-1109)

Paste into a new `routes_session.go`. The new file starts with:

```go
package daemon

import (
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	vault "github.com/racterub/gobrrr/internal/crypto"
)
```

(Verify imports by reading the moved code — `os/exec` and `runtime` are used by `binOnPath`; `vault` and `filepath` are used by `loadVaultIfAvailable`.)

- [ ] **Step 2: Create `routes_schedule.go`**

From `daemon.go`, cut by anchor:
- `handleCreateSchedule`, `handleListSchedules`, `handleRemoveSchedule` (old 1065-1105)

Paste into a new `routes_schedule.go`. The new file starts with:

```go
package daemon

import (
	"encoding/json"
	"net/http"
	"strings"
)
```

- [ ] **Step 3: Move `handleListSkills` into the existing `skill_routes.go`**

In `daemon.go`, locate `handleListSkills` (currently around line 238). Cut the entire function (comment header + body) and append it to `daemon/internal/daemon/skill_routes.go`. Confirm `skill_routes.go` already imports `encoding/json` and `net/http` and `internal/skills`; if not, add what is needed.

- [ ] **Step 4: Create `health.go`**

From `daemon.go`, cut by anchor:
- `runHealthMonitor`, `updateHeartbeat` (old 314-358)
- `warmStatus`, `healthResponse` types (old 359-375)
- `handleHealth` (old 376-392)

Paste into a new `health.go`. The new file starts with:

```go
package daemon

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"
)
```

- [ ] **Step 5: Run the test gate**

Run:
```bash
cd daemon && CGO_ENABLED=0 go build ./... && go test ./... && go vet ./...
```
Expected: build, tests, vet all green. Pay particular attention to:
- `daemon_health_test.go` (references `handleHealth`, `runHealthMonitor`) — same package, resolves.
- `daemon_test.go` calls `New(...)` and route helpers — unaffected.
- `skill_install_route_test.go` — unaffected.

- [ ] **Step 6: Confirm `daemon.go` is at or under 300 lines**

Run:
```bash
wc -l daemon/internal/daemon/daemon.go
```
Expected: ≤ 300 lines, containing only:
- Imports
- `Daemon` struct
- `New()` (including the full route registration table)
- `Run()`

If `daemon.go` is over 300, audit what is left (probably one helper or block missed during the cuts) and move it to the file that owns the concern.

---

### Task 4: Final verify, commit, push, open PR

**Files:** No new files; this task only validates and publishes the work.

- [ ] **Step 1: Final test gate**

Run from the repo root:
```bash
cd daemon && CGO_ENABLED=0 go build ./... && go test ./... && go vet ./...
```
Expected: all three commands exit zero. If anything fails, fix in place — do not move on.

- [ ] **Step 2: Spot-check the route table is intact**

Run:
```bash
grep -n "d.mux.HandleFunc" daemon/internal/daemon/daemon.go | wc -l
```
Expected: 24 lines (every original `d.mux.HandleFunc(...)` still in `New()`, since the route registration is what holds wiring together — only the *handler implementations* moved).

Run:
```bash
grep -rn "func (d \*Daemon) handle" daemon/internal/daemon/*.go | wc -l
```
Expected: same count of handler definitions as before (verify by `git diff master -- daemon/internal/daemon/daemon.go | grep -c "^-func (d \*Daemon) handle"` showing the same number of removals as new definitions in the new files).

- [ ] **Step 3: Stage and commit**

```bash
git add daemon/internal/daemon/daemon.go \
        daemon/internal/daemon/routes_tasks.go \
        daemon/internal/daemon/routes_memory.go \
        daemon/internal/daemon/routes_gmail.go \
        daemon/internal/daemon/routes_gcal.go \
        daemon/internal/daemon/routes_session.go \
        daemon/internal/daemon/routes_schedule.go \
        daemon/internal/daemon/skill_routes.go \
        daemon/internal/daemon/health.go

git commit -m "$(cat <<'EOF'
refactor(daemon): split daemon.go by route concern

daemon.go grew to 1109 lines mixing lifecycle/ctor, task CRUD,
memory routes, gmail routes, gcal routes, session routes, schedule
routes, skill listing, and the health monitor. Extract handler groups
into per-concern files:

- routes_tasks.go     — submit/list/get/cancel/logs
- routes_memory.go    — save/search/get/delete
- routes_gmail.go     — list/read/send/reply + requireGmail + checkWritePermission
- routes_gcal.go      — today/week/get/create/update/delete + requireCalendar
- routes_session.go   — status/start/stop/restart + loadVaultIfAvailable + binOnPath
- routes_schedule.go  — create/list/remove
- health.go           — handleHealth + runHealthMonitor + updateHeartbeat + types
- skill_routes.go     — handleListSkills (joins existing skill handlers)

daemon.go retains only the Daemon struct, New() with the full route
table, and Run(). All identifiers and method signatures unchanged;
files share package daemon so cross-references resolve as before.

Part of structural refactor batch (spec:
docs/superpowers/specs/2026-04-26-structural-refactor-batch-design.md).

Structural change.

AI-Ratio: 1.0
Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

Expected: commit created cleanly; pre-commit hook (if any) passes without `--no-verify`.

- [ ] **Step 4: Push the branch**

```bash
git push -u origin refactor/7-split-daemon
```
Expected: branch published.

- [ ] **Step 5: Open the PR**

```bash
gh pr create --title "refactor: split daemon.go by route concern (TODO #7)" --body "$(cat <<'EOF'
## Summary

Single structural commit splitting the 1109-line `daemon/internal/daemon/daemon.go` into per-concern files:

- `routes_tasks.go`, `routes_memory.go`, `routes_gmail.go`, `routes_gcal.go`, `routes_session.go`, `routes_schedule.go`
- `health.go` (HTTP handler + runtime monitor + types)
- `skill_routes.go` gains `handleListSkills`

`daemon.go` retains the `Daemon` struct, `New()` (with the unchanged route registration table), and `Run()`. Zero signature changes.

Spec: `docs/superpowers/specs/2026-04-26-structural-refactor-batch-design.md`
TODO entry: Refactor #7.

## Test plan

- [ ] `cd daemon && CGO_ENABLED=0 go build ./...` green at HEAD
- [ ] `cd daemon && go test ./...` green at HEAD
- [ ] `cd daemon && go vet ./...` green at HEAD
- [ ] Route table in `New()` still registers all 24 endpoints
- [ ] `daemon.go` ≤ 300 lines (`wc -l`)

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

If the user prefers local fast-forward merge instead of PR, run:
```bash
git checkout master && git merge --ff-only refactor/7-split-daemon && git push origin master
```

- [ ] **Step 6: Once merged, prune the TODO entry**

Per the global todo-tracking rule, delete the entire "Refactor #7 — Split `daemon.go` (1139 lines) by handler concern" section from `TODO.md`. Mention the removal in the reply so the user can object.

The remaining structural-batch entries (#6a, #6b, #8, #9, #10) stay in `TODO.md` until each lands.
