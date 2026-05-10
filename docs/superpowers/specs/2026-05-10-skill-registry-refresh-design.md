# Skill Registry Refresh After Install — Design

**Date:** 2026-05-10
**TODO source:** Refactor #17 — Refresh skill registry after install commit
**Status:** Approved

## Problem

After approving a skill install, the staged bundle is committed to `~/.gobrrr/skills/<slug>/`, but the in-memory `skills.Registry` snapshot is never refreshed. Workers spawned after the install do not see the new skill until the daemon restarts.

### Evidence

- `internal/skills/registry.go` — `Registry` is a cached snapshot: `Refresh()` rescans the filesystem and replaces the slice; `List()` returns whatever was last loaded.
- `internal/daemon/worker.go:198` and `worker.go:250` — every task spawn calls `wp.skillReg.List()` to (a) collect skill `readPerms`/`writePerms` for the per-task `settings.json`, (b) build the `<available_skills>` prompt block. So `List()` is consulted per-task, but the underlying data is only updated when something explicitly calls `Refresh()`.
- `internal/daemon/skill_routes.go:95` — uninstall path calls `Refresh()` after removing the skill directory.
- `internal/daemon/skill_install_handler.go` — install path calls `committer.Commit(...)` and returns. **No `Refresh()` call.** This is the bug.

### Impact

After `gobrrr skill approve <id>` succeeds, the next worker spawn:
- Won't see the skill in `<available_skills>` (Claude doesn't know it exists).
- Won't get the skill's `approved_read_permissions` / `approved_write_permissions` merged into its `settings.json` (commands defined by the skill will be denied even with `--allow-writes`).

The only workaround today is `systemctl restart gobrrr.service`, which the user has no reason to expect.

## Approach

Add the missing `Refresh()` call to `skillInstallHandler.Handle`, gated on a successful approve commit.

### Plumbing

`skillInstallHandler` currently has one field, `committer committerLike`. Add a second, mirroring the same minimal-interface pattern:

```go
type refresherLike interface {
    Refresh() error
}

type skillInstallHandler struct {
    committer committerLike
    refresher refresherLike
}
```

`*skills.Registry` satisfies `refresherLike` directly (its `Refresh() error` method). In `daemon.New` (currently `daemon.go:98`), wire the existing `skillReg` into the constructor:

```go
approvals.Register("skill_install", &skillInstallHandler{
    committer: committer,
    refresher: skillReg,
})
```

`NewSkillInstallHandlerForTesting` gets a second parameter so existing callers can inject a fake refresher.

### When to refresh

Refresh runs **only** after `Commit` returns nil **and** the decision was an approval (`approve` or `skip_binary`). Concretely, structure the handler tail like:

```go
if err := h.committer.Commit(installReq, d); err != nil {
    return err
}
if d.Approve {
    if err := h.refresher.Refresh(); err != nil {
        log.Printf("skill_install: registry refresh failed after commit: %v", err)
    }
}
return nil
```

- `deny` → `Commit` is still called (it removes the staging dir per current behavior), but no skill landed on disk, so no refresh.
- `Commit` failed → skill isn't on disk in a usable state; refresh would either find nothing new or partial state. Skip it; return the commit error as-is.

### Refresh failure handling: best-effort

If `Refresh()` returns an error after a successful `Commit`:
- Log the error.
- Return `nil` from the handler (install is reported as successful).

Rationale: the skill is already committed to `~/.gobrrr/skills/<slug>/`. The next daemon restart will pick it up. Hard-failing the handler would surface as an `Error` field on the SSE `removed` event and confuse UX — the user would see "install failed" alongside a skill that's actually on disk.

The trade-off: the user will quietly hit a "skill not active until restart" state. This matches today's behavior pre-fix; the worst case stays the same. We accept it because `Refresh` failures are expected to be rare (the same code path runs at startup; if it fails after an install, the daemon is in a degraded state already).

## Testing

Unit-level only, in a new `daemon/internal/daemon/skill_install_handler_test.go`. Use fakes for both `committerLike` and `refresherLike` (struct with call counters and configurable returns).

### Cases

1. **Approve + Commit succeeds** → `Commit` called once, `Refresh` called once, handler returns nil.
2. **Skip-binary + Commit succeeds** → `Commit` called once with `SkipBinary: true`, `Refresh` called once, handler returns nil.
3. **Approve + Commit fails** → `Commit` called once, `Refresh` **not** called, handler returns the commit error.
4. **Deny** → `Commit` called once with `Approve: false`, `Refresh` **not** called, handler returns nil.
5. **Approve + Commit succeeds + Refresh fails** → handler returns nil (best-effort), error is logged. Test asserts the return is nil; log assertion is out of scope (verifiable by inspection).

The TODO's stated acceptance criterion of "submits a task immediately after install and asserts the new skill is in the worker prompt" is downgraded to the unit assertion that `Refresh` is called — the per-task `List()` consumption at `worker.go:198, 250` is already exercised by existing tests, so the behavioral chain is covered transitively without spawning a real worker.

## Files touched

- `daemon/internal/daemon/skill_install_handler.go` — add `refresherLike` interface, `refresher` field, refresh-after-commit logic, update `NewSkillInstallHandlerForTesting` signature.
- `daemon/internal/daemon/daemon.go:98` — pass `skillReg` to the handler constructor.
- `daemon/internal/daemon/skill_install_handler_test.go` — new file, five unit cases above.

## Out of scope

- Hot-reload of in-flight workers. They keep their snapshot from spawn time; only new tasks see the refreshed registry. (Called out in the TODO.)
- Refreshing on other write paths (e.g., manual filesystem edits to `~/.gobrrr/skills/`). Out of scope for this fix; users editing the skills dir by hand can `systemctl restart gobrrr` or call the uninstall HTTP endpoint as a workaround.
- Eventual sqlite/bolt-backed registry. Orthogonal storage concern.

## Acceptance criteria

- [ ] `skillInstallHandler` has a `refresher refresherLike` field; `Handle` calls `refresher.Refresh()` after a successful approval commit.
- [ ] `daemon.New` wires `skillReg` into the handler.
- [ ] Five unit tests in `skill_install_handler_test.go` pass (the cases enumerated above).
- [ ] Existing test suite still passes (`cd daemon && go test ./...`).
- [ ] After approving a real skill install through the daemon, a freshly-spawned task sees the skill in its `<available_skills>` block without a daemon restart (manual smoke test on the LXC).
