# Refactor #13 — Drop Pre-Migration Cruft Fields Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Delete six vestigial struct fields that are still serialized to disk but no longer read by anyone, across `Task` (queue.go), `InstallRequest` (clawhub/types.go), and `accountEntry` (google/auth.go).

**Architecture:** Three independent commits, one per struct, on branch `refactor/13-drop-cruft-fields`. Each commit deletes a field's struct definition and every write site, leaves on-disk JSON loaders untouched (Go's `encoding/json` ignores unknown fields on unmarshal, so existing `~/.gobrrr/queue.json`, `~/.gobrrr/_approvals/*.json`, and `~/.gobrrr/google/accounts.json` continue to load).

**Tech Stack:** Go 1.21+, stdlib `encoding/json` (already in use), `testify` (already in use).

**Spec:** `docs/superpowers/specs/2026-04-26-structural-refactor-batch-design.md`

**TODO source:** `TODO.md` "Refactor #13 — Drop pre-migration cruft fields"

**Branch:** `refactor/13-drop-cruft-fields` (cuts from `master`)

**Test gate at every commit:**
```
cd daemon && CGO_ENABLED=0 go build ./...
cd daemon && go test ./...
cd daemon && go vet ./...
```

**Commit message style:** All three commits are `Structural change.` per CLAUDE.md tidy-first rule. All include `AI-Ratio: 1.0` and `Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>` trailers.

---

## Phase 1: Drop all three field sets (3 tasks)

### Task 1: Drop `Task.Retries` and `Task.MaxRetries`

**Files:**
- Modify: `daemon/internal/daemon/queue.go:28-29`

**Background:** The `Task` struct has `Retries` and `MaxRetries` fields that were planned for retry-on-failure but never wired. Grep confirms they have zero readers and zero writers outside the struct definition itself. The on-disk `~/.gobrrr/queue.json` may have these fields in old records — `encoding/json` ignores them on unmarshal, so removing the field is safe at runtime.

- [ ] **Step 1: Create the feature branch**

```bash
git checkout master
git pull --ff-only
git checkout -b refactor/13-drop-cruft-fields
```

Expected: switched to a new branch.

- [ ] **Step 2: Verify zero non-definition references**

Run:
```bash
grep -rn "\.Retries\b\|\.MaxRetries\b" daemon/ --include="*.go" | grep -v "queue.go:28\|queue.go:29"
```
Expected: empty output. (`google/retry.go` has a local `maxRetries := 5` variable — that's a different identifier and won't match `\.MaxRetries\b`.)

- [ ] **Step 3: Delete the two field lines**

In `daemon/internal/daemon/queue.go`, remove lines 28-29:

```go
	Retries     int               `json:"retries"`
	MaxRetries  int               `json:"max_retries"`
```

The struct should now go directly from `CompletedAt` to `TimeoutSec`:

```go
// Task represents a unit of work dispatched to a Claude worker.
type Task struct {
	Version     int               `json:"version"`
	ID          string            `json:"id"`
	Prompt      string            `json:"prompt"`
	Status      string            `json:"status"`
	Priority    int               `json:"priority"`
	ReplyTo     string            `json:"reply_to"`
	AllowWrites bool              `json:"allow_writes"`
	Warm        bool              `json:"warm"`
	CreatedAt   time.Time         `json:"created_at"`
	StartedAt   *time.Time        `json:"started_at"`
	CompletedAt *time.Time        `json:"completed_at"`
	TimeoutSec  int               `json:"timeout_sec"`
	Result      *string           `json:"result"`
	Error       *string           `json:"error"`
	Delivered   bool              `json:"delivered"`
	Metadata    map[string]string `json:"metadata"`
}
```

- [ ] **Step 4: Run the test gate**

Run:
```bash
cd daemon && CGO_ENABLED=0 go build ./... && go test ./... && go vet ./...
```
Expected: build succeeds, all tests pass, vet clean.

- [ ] **Step 5: Commit**

```bash
git add daemon/internal/daemon/queue.go
git commit -m "$(cat <<'EOF'
refactor(daemon): drop unused Retries/MaxRetries from Task

Both fields were defined when retry-on-failure was a planned feature
that never landed. They have no readers and no writers anywhere in
the tree; on-disk queue.json with old records continues to load
because encoding/json tolerates unknown fields on unmarshal.

Part of structural refactor batch (spec:
docs/superpowers/specs/2026-04-26-structural-refactor-batch-design.md).

Structural change.

AI-Ratio: 1.0
Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

Expected: commit created cleanly; pre-commit hook (if any) passes without `--no-verify`.

---

### Task 2: Drop `InstallRequest.RequestID`, `CreatedAt`, `ExpiresAt`

**Files:**
- Modify: `daemon/internal/clawhub/types.go:82, 91-92`
- Modify: `daemon/internal/clawhub/installer.go:25, 99, 108-109`
- Modify: `daemon/internal/clawhub/installer_test.go:57, 77-78`
- Modify: `daemon/internal/clawhub/commit_test.go:21, 76, 93, 172`
- Modify: `daemon/internal/daemon/skill_install_handler_test.go:33, 56, 65`

**Background:** `InstallRequest` is the staging payload that the clawhub installer hands to the approval layer. Its `RequestID`, `CreatedAt`, and `ExpiresAt` fields predate the generic approval store — today the approval layer (`daemon/approvals_store.go`) owns the request ID and expiry timestamps; the comment in `installer.go:58-59` already calls this out. The fields are still set by `Stage()` and inspected by tests, but no production code reads them. The local `reqID` variable inside `Stage()` stays — it's used to name the staging directory (`<reqID>_staging`); only the **field** on the struct goes away. The `requestTTL` constant becomes unused once `ExpiresAt` is removed and is also dropped.

- [ ] **Step 1: Verify zero non-test, non-write references to the three fields**

Run:
```bash
grep -rn "\.RequestID\b\|\.CreatedAt\b\|\.ExpiresAt\b" daemon/internal/clawhub/ daemon/internal/daemon/ --include="*.go" | grep -v "_test.go" | grep -v "installer.go:99\|installer.go:108\|installer.go:109\|types.go:82\|types.go:91\|types.go:92"
```
Expected: empty output. (`ApprovalRequest` in `daemon/approvals_dispatcher.go` has its own `CreatedAt`/`ExpiresAt` on a different struct — that's the canonical location and stays.)

- [ ] **Step 2: Remove the three fields from `InstallRequest`**

In `daemon/internal/clawhub/types.go`, edit the `InstallRequest` struct (lines 78-93) to:

```go
// InstallRequest is the pending-install record written to disk for user
// approval. A human-in-the-loop Telegram confirmation consumes this; the
// downstream commit step reads it back to execute the install.
type InstallRequest struct {
	Slug             string             `json:"slug"`
	Version          string             `json:"version"`
	SourceURL        string             `json:"source_url"`
	SHA256           string             `json:"sha256"`
	StagingDir       string             `json:"staging_dir"`
	Frontmatter      skills.Frontmatter `json:"frontmatter"`
	MissingBins      []string           `json:"missing_bins"`
	ProposedCommands []ProposedCommand  `json:"proposed_commands"`
}
```

- [ ] **Step 3: Remove the writes in `Stage()` and the now-unused constant**

In `daemon/internal/clawhub/installer.go`:

(a) Delete the `requestTTL` constant block (lines 23-25):

```go
// requestTTL is how long an approval proposal stays valid. After this window
// the staging dir and request file are safe to garbage-collect.
const requestTTL = 24 * time.Hour
```

(b) Edit the `Stage()` return statement (currently lines 97-110) to drop the three field writes and the `now` local that only feeds them:

```go
	proposed := proposeCommands(fm, missing)

	return &InstallRequest{
		Slug:             pkg.Slug,
		Version:          pkg.Version,
		SourceURL:        in.composeSourceURL(pkg.OwnerHandle, pkg.Slug, pkg.Version),
		SHA256:           pkg.SHA256,
		StagingDir:       stagingDir,
		Frontmatter:      *fm,
		MissingBins:      missing,
		ProposedCommands: proposed,
	}, nil
}
```

(c) Confirm the `time` import on line 14 stays — `time.Now().UnixNano()` inside `newRequestID` (line 129) still needs it.

- [ ] **Step 4: Update `installer_test.go`**

In `daemon/internal/clawhub/installer_test.go`, remove three lines:
- Line 57: `require.NotEmpty(t, installReq.RequestID)`
- Lines 77-78: `assert.False(t, installReq.ExpiresAt.IsZero())` and `assert.True(t, installReq.ExpiresAt.After(installReq.CreatedAt))`

The first test (around line 50) should still cover the meaningful invariants: slug, version, SHA256, source URL, missing bins, proposed commands, staging dir contents.

- [ ] **Step 5: Update `commit_test.go`**

In `daemon/internal/clawhub/commit_test.go`, remove the `RequestID: "..."` line from each `InstallRequest` literal at lines 21, 76, 93, and 172. Example transformation for line 20-26:

```go
	req := &InstallRequest{
		Slug:       "noop",
		Version:    "1.0.0",
		SourceURL:  "https://clawhub.com/noop",
		SHA256:     "sha256:test",
		StagingDir: filepath.Join(skillsRoot, "_requests", "abcd_staging"),
```

(The `abcd` substring stays in `StagingDir` because the staging directory naming convention is unchanged — only the struct field is being removed.)

- [ ] **Step 6: Update `skill_install_handler_test.go`**

In `daemon/internal/daemon/skill_install_handler_test.go`:
- Line 33: remove `RequestID: "abcd",` from the `installReq` literal
- Line 56: change `clawhub.InstallRequest{RequestID: "abcd"}` → `clawhub.InstallRequest{}`
- Line 65: same change

After the edit, lines 56 and 65 read:
```go
	raw, _ := json.Marshal(clawhub.InstallRequest{})
```

- [ ] **Step 7: Run the test gate**

Run:
```bash
cd daemon && CGO_ENABLED=0 go build ./... && go test ./... && go vet ./...
```
Expected: build succeeds, all tests pass, vet clean. Pay particular attention to `go vet` — it will flag the unused `requestTTL` constant if step 3(a) was missed.

- [ ] **Step 8: Commit**

```bash
git add daemon/internal/clawhub/types.go \
        daemon/internal/clawhub/installer.go \
        daemon/internal/clawhub/installer_test.go \
        daemon/internal/clawhub/commit_test.go \
        daemon/internal/daemon/skill_install_handler_test.go
git commit -m "$(cat <<'EOF'
refactor(clawhub): drop unused RequestID/CreatedAt/ExpiresAt from InstallRequest

These three fields predate the generic approval store. Today the
approval layer (daemon/approvals_store.go) owns the request ID and
expiry timestamps; clawhub's InstallRequest only needs the install
payload. The local reqID variable in Stage() stays because the
staging directory name is derived from it; only the field on the
struct goes away. requestTTL constant is removed alongside (no other
callers).

Part of structural refactor batch (spec:
docs/superpowers/specs/2026-04-26-structural-refactor-batch-design.md).

Structural change.

AI-Ratio: 1.0
Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

Expected: commit created cleanly.

---

### Task 3: Drop `accountEntry.Type`

**Files:**
- Modify: `daemon/internal/google/auth.go:46, 297`

**Background:** `accountEntry` is the per-account record stored in `~/.gobrrr/google/accounts.json`. The `Type` field always gets the literal `"oauth2"` written to it and is never read (no other Google account type exists). Deleting it is safe; existing `accounts.json` files keep loading because Go's `encoding/json` ignores unknown fields on unmarshal.

- [ ] **Step 1: Verify zero readers of `Type` on `accountEntry`**

Run:
```bash
grep -rn "accountEntry\b" daemon/internal/google/ --include="*.go"
grep -rn "\.Type\b" daemon/internal/google/ --include="*.go"
```
Expected: the only write of `Type` is `auth.go:297`. There are no reads.

- [ ] **Step 2: Remove the field from the struct**

In `daemon/internal/google/auth.go` lines 43-47, change:

```go
// accountEntry is the per-account record stored in accounts.json.
type accountEntry struct {
	Email string `json:"email"`
	Type  string `json:"type"`
}
```

to:

```go
// accountEntry is the per-account record stored in accounts.json.
type accountEntry struct {
	Email string `json:"email"`
}
```

- [ ] **Step 3: Update the only writer**

In `daemon/internal/google/auth.go` line 297, change:

```go
	idx.Accounts[name] = accountEntry{Email: email, Type: "oauth2"}
```

to:

```go
	idx.Accounts[name] = accountEntry{Email: email}
```

- [ ] **Step 4: Run the test gate**

Run:
```bash
cd daemon && CGO_ENABLED=0 go build ./... && go test ./... && go vet ./...
```
Expected: build succeeds, all tests pass, vet clean.

- [ ] **Step 5: Commit**

```bash
git add daemon/internal/google/auth.go
git commit -m "$(cat <<'EOF'
refactor(google): drop unused Type field from accountEntry

The Type field always held the literal "oauth2" and was never read
anywhere. No other account type exists in the Google integration.
Existing ~/.gobrrr/google/accounts.json keeps loading because
encoding/json tolerates unknown fields on unmarshal.

Part of structural refactor batch (spec:
docs/superpowers/specs/2026-04-26-structural-refactor-batch-design.md).

Structural change.

AI-Ratio: 1.0
Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

Expected: commit created cleanly.

---

## Wrap-up

After all three commits land:

- [ ] **Step 1: Verify the branch state**

Run:
```bash
git log --oneline master..HEAD
```
Expected: three commits, all `refactor(...)` with the `Structural change.` body line.

- [ ] **Step 2: Push the branch**

```bash
git push -u origin refactor/13-drop-cruft-fields
```
Expected: branch published.

- [ ] **Step 3: Open a PR (or fast-forward merge per user's preference)**

Default per spec is PR per branch. Use `gh pr create` with title `refactor: drop pre-migration cruft fields (TODO #13)` and body that links the spec doc:

```bash
gh pr create --title "refactor: drop pre-migration cruft fields (TODO #13)" --body "$(cat <<'EOF'
## Summary

Three structural commits, each deleting one set of dead struct fields:

- `Task.Retries` / `Task.MaxRetries` (`daemon/queue.go`) — never wired
- `InstallRequest.RequestID` / `CreatedAt` / `ExpiresAt` (`clawhub/types.go`) — superseded by generic approval store
- `accountEntry.Type` (`google/auth.go`) — only ever held the literal `"oauth2"`

Spec: `docs/superpowers/specs/2026-04-26-structural-refactor-batch-design.md`
TODO entry: Refactor #13.

## Test plan

- [ ] `cd daemon && CGO_ENABLED=0 go build ./...` green at HEAD
- [ ] `cd daemon && go test ./...` green at HEAD
- [ ] `cd daemon && go vet ./...` green at HEAD
- [ ] Existing `~/.gobrrr/queue.json`, `~/.gobrrr/_approvals/*.json`, and `~/.gobrrr/google/accounts.json` continue to load (extra fields tolerated by `encoding/json`)

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

If the user prefers local fast-forward merge instead, run `git checkout master && git merge --ff-only refactor/13-drop-cruft-fields`.

- [ ] **Step 4: Once merged, prune the TODO entry**

Per the global todo-tracking rule, once acceptance criteria are demonstrably met (all three field sets gone, tests pass, branch merged), delete the entire "Refactor #13 — Drop pre-migration cruft fields" section from `TODO.md`. Mention the removal in the reply so the user can object.

The remaining structural-batch entries (#6, #7, #8, #9, #10) stay in `TODO.md` until each lands.
