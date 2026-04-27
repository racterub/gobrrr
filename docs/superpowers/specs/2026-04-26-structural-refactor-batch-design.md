# Structural Refactor Batch — Design

**Date:** 2026-04-26
**Status:** Approved (brainstorming complete; per-refactor plans to be written JIT)
**Author:** Claude (Opus 4.7) with @racterub

## Summary

Execute seven structural refactors from `TODO.md` as one coordinated batch. Each refactor lands on its own feature branch with one or more commits, gated by `go build` + `go test ./...` + `go vet`. The batch is six numbered TODO items (#13, #7, #9, #8, #10, #6) plus an explicit seventh slot for the parent-directory fsync that #6 implies but doesn't ship as structural code.

The batch deletes roughly 680 lines net (mostly from #8's `Foo`/`FooWithTaskID` collapse, #9's HTTP-helper sweep, and #13's vestigial fields) and moves another ~1700 lines from three monolithic files (`daemon.go`, `main.go`, `client.go`) into focused per-concern files. Externally observable behavior is unchanged — except for one small, isolated commit (#6b) that adds parent-directory fsync to the new `internal/atomicfs` helper.

## Out of scope

- Google integration removal (TODO "Remove Google Integration Code") — confirmed deferred indefinitely; structural work assumes Google code stays.
- `kind: write_action` approval handler — separate TODO item.
- Refactors #11, #12, #14, #15, #16, #17 — security and decision items, not structural.
- Channel Bridge memory leak.
- ClaudeClaw plugin distribution model.
- Type-safety improvements (`map[string]any` → typed structs) inside #8.
- HTTP middleware additions inside #9.
- Subpackage extraction inside #7 (e.g. `daemon/queue` as its own package).
- Exit-code policy reconciliation inside #10.

## Scope & sequence

| Order | # | Refactor | Type |
|-------|---|----------|------|
| 1 | **#13** | Drop pre-migration cruft fields (`Task.Retries`/`MaxRetries`, `InstallRequest.RequestID`/`CreatedAt`/`ExpiresAt`, `accountEntry.Type`) | structural |
| 2 | **#7** | Split `daemon.go` (1109 LoC) into `routes_*.go` files | structural |
| 3 | **#9** | Add `decodeJSON` / `respondJSON` / `respondError` helpers and sweep handlers | structural |
| 4 | **#8** | Collapse `client.go` (872 LoC) `Foo`/`FooWithTaskID` pairs and extract transport helpers | structural |
| 5 | **#10** | Split `cmd/gobrrr/main.go` (1064 LoC) by verb and standardize cobra flag wiring | structural |
| 6 | **#6a** | Extract `internal/atomicfs.WriteFile` / `WriteJSON` (matching today's no-fsync behavior) and migrate seven call sites | structural |
| 7 | **#6b** | Add parent-directory fsync inside `atomicfs.WriteFile` plus test | **behavioral** |

### Sequence rationale

- **Risk-ascending.** Smallest-blast-radius first (#13 is ~30 LoC across three structs) builds momentum and verifies the test gate works before touching larger files.
- **#9 follows #7.** Splitting `daemon.go` first means the helper sweep in #9 happens against tidy per-route files instead of one 1100-line monster.
- **#6a last among structurals.** Atomic writes are touched by seven packages; doing this once everyone else has stabilized minimizes rebase friction.
- **#6b strictly after #6a.** Honors the global "never mix structural and behavioral changes in the same commit" rule by keeping the durability win as a single isolated commit.

## Commit cadence

Per-refactor commit shape, totals to **19 commits across 7 branches**:

| Refactor | Commits | Title pattern |
|----------|---------|---------------|
| #13 | 3 | `refactor(<pkg>): drop unused <field> from <struct>` × 3 |
| #7 | 1 | `refactor(daemon): split daemon.go by route concern` |
| #9 | 2 | `refactor(daemon): add HTTP helper functions` then `refactor(daemon): sweep handlers to use HTTP helpers` |
| #8 | 2 | `refactor(client): drop bare wrappers, rename WithTaskID variants` then `refactor(client): extract postJSON/getJSON/deleteResource helpers` |
| #10 | 2 | `refactor(cli): split main.go into per-verb files` then `refactor(cli): standardize cobra flag wiring` |
| #6a | 8 | `feat(atomicfs): add WriteFile/WriteJSON helper` then 7 × `refactor(<pkg>): use atomicfs for atomic writes` |
| #6b | 1 | `fix(atomicfs): fsync parent directory after rename` |

Every commit message body explicitly tags `Structural change.` or `Behavioral change.` per the global CLAUDE.md tidy-first rule. Every commit also includes the standard `AI-Ratio: 1.0` and `Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>` trailers.

## Test gate

At every commit, the following must pass before the commit lands:

```
cd daemon && CGO_ENABLED=0 go build ./...
cd daemon && go test ./...
cd daemon && go vet ./...
```

Pre-commit hooks run unmodified. **No `--no-verify`** at any point — if a hook fails, fix the underlying issue and create a new commit.

## Branch strategy

One feature branch per refactor → seven branches:

- `refactor/13-drop-cruft-fields`
- `refactor/7-split-daemon`
- `refactor/9-http-helpers` (cuts off `master` after #7 merges)
- `refactor/8-collapse-client`
- `refactor/10-split-main`
- `refactor/6a-atomicfs-extract`
- `refactor/6b-atomicfs-fsync` (cuts off `master` after #6a merges)

Default integration: PR per branch so review history is preserved. User may request plain local fast-forward merge instead.

Worktrees are optional. The `superpowers:using-git-worktrees` skill is available if the user wants to keep `master` checked out while a branch is in flight.

## Per-refactor risk and rollback

| # | Blast radius | Risk | Rollback |
|---|--------------|------|----------|
| **#13** | 3 structs in 3 packages, dead-field deletion only | Very low — fields verified unused via grep before delete | `git revert <sha>` per struct; on-disk JSON unaffected (extra fields tolerated on unmarshal) |
| **#7** | Single package (`internal/daemon`), pure file moves, no signature changes | Low — `go test ./internal/daemon/...` is the gate; if green, semantics preserved | Single-commit revert |
| **#9** | All daemon route handlers, ~96 sweep sites | Medium — easy to miss a non-standard handler that returns plain text instead of JSON | Two commits → revert sweep first (keep helpers), or revert both |
| **#8** | `internal/client` + ~25 CLI call sites in `cmd/gobrrr` | Medium — rename touches CLI; `gobrrr <cmd> --help` smoke check before each commit | Two commits → revert independently; rename revertable via second sed pass |
| **#10** | `cmd/gobrrr/*`, all cobra commands | Medium — easy to break flag parsing; verify with `gobrrr <every-subcmd> --help` | Two commits → flag-style revert is mechanical; per-verb split revert is one commit |
| **#6a** | 7 packages writing JSON: memory, queue, approvals_store, clawhub installer/commit, skills/bundled, scheduler, google/auth | Medium-high — atomic-write contract is load-bearing; per-package commits make bisect surgical | Per-package revert; package-by-package migration means partial rollback possible |
| **#6b** | Single function in `atomicfs` | Low — fsync addition is one syscall; failure mode is "write succeeds but isn't durable on power loss" — same as today | Single-commit revert |

### Cross-cutting safety net

- Build + test + vet at every commit (the test gate above).
- Both binaries (`gobrrr` and the telegram bot) build at every commit, since both depend on `internal/`.
- For #6a, no migration commit lands until the previous one is green.

## Execution flow

### File layout

- This umbrella spec → `docs/superpowers/specs/2026-04-26-structural-refactor-batch-design.md`
- Per-refactor plans → `docs/superpowers/plans/2026-04-26-refactor-<num>-<slug>.md` (written just-in-time, one per refactor)

### Just-in-time plan writing

Plans are written one at a time, not all up front:

1. After this umbrella spec is reviewed and approved, invoke `writing-plans` for **#13** only.
2. Execute via `subagent-driven-development` on the `refactor/13-drop-cruft-fields` branch.
3. PR / merge to `master` once tests are green.
4. Then invoke `writing-plans` for **#7**, repeat.
5. Continue through #9 → #8 → #10 → #6a → #6b.

Why: each plan benefits from learnings from the previous; less doc bloat up front; easier to revise approach mid-batch if something surprises us.

### Session checkpoints

Per the global `plan-phase-markers` rule:

- Each per-refactor plan has phases capped at ≤4 tasks.
- Smallest refactors (#13, #7, #6b) fit in a single phase → no checkpoint needed.
- Refactors that span multiple phases (likely #9 and #6a) → execution stops at each phase boundary; user runs `/compact` and says "continue" before the next phase fires.

Across refactors: after each refactor lands on `master`, user may `/compact` and we move to the next. If the session is fully drained, `/kickoff` produces a handoff prompt that points the next session at this umbrella spec.

## Definition of done

The batch is complete when all of the following hold:

| Check | Source |
|-------|--------|
| All 7 branches merged to `master` | `git log` |
| `daemon.go` ≤ 300 LoC | #7 acceptance criterion |
| `main.go` ≤ 100 LoC | #10 acceptance criterion |
| `client.go` ≤ 450 LoC | #8 acceptance criterion |
| No `*WithTaskID` methods (taskID is just an arg) | grep |
| Zero `json.NewDecoder` / `json.NewEncoder` in route handlers | grep |
| Zero `os.WriteFile(...".tmp"...)` patterns outside `internal/atomicfs` | grep |
| Zero package-level cobra flag-value vars | grep |
| Vestigial fields (`Task.Retries`, `Task.MaxRetries`, `InstallRequest.RequestID`, `InstallRequest.CreatedAt`, `InstallRequest.ExpiresAt`, `accountEntry.Type`) gone | grep |
| `atomicfs.WriteFile` fsyncs the parent directory after rename, with a test asserting the syscall happens | code review |
| `go build` + `go test ./...` + `go vet` all green at `HEAD` of `master` | local |
| `TODO.md` entries for #6, #7, #8, #9, #10, #13 removed | per global todo-tracking rule |
