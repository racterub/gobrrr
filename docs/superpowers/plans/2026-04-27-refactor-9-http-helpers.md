# Refactor #9 — HTTP Helpers Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extract repeated JSON encode / decode / error-envelope patterns from daemon route handlers into three small helpers (`decodeJSON`, `respondJSON`, `respondError`) and sweep every route handler to use them.

**Architecture:** Add a single new file `daemon/internal/daemon/http_helpers.go` with three thin stdlib wrappers. Then mechanically migrate nine route files (`routes_*.go`, `health.go`, `skill_routes.go`, `approvals_routes.go`). Pure structural change — error envelope shape is preserved per endpoint (JSON-shape endpoints stay JSON, plain-text endpoints stay plain-text).

**Tech Stack:** Go stdlib `net/http` + `encoding/json`. Same package as the rest of `daemon`.

**Sequence position:** This is refactor #9 of the structural batch (`docs/superpowers/specs/2026-04-26-structural-refactor-batch-design.md`). #13 and #7 are done; #9 unblocks because route handlers now live in tidy per-concern files.

**Branch:** `refactor/9-http-helpers` (cut from `master`).

---

## Scope

The current state of route handlers:

- 41 `json.NewDecoder` / `json.NewEncoder` calls across nine route files.
- Repeated boilerplate: `w.Header().Set("Content-Type", "application/json")`, sometimes `w.WriteHeader(...)`, then `json.NewEncoder(w).Encode(...) //nolint:errcheck`.
- Repeated JSON error envelope: `http.Error(w, `{"error":"<msg>"}`, status)` (and one `fmt.Sprintf("{\"error\":%q}", ...)` variant in `routes_schedule.go`).

Two endpoint families intentionally stay outside the JSON-error envelope:

- `skill_routes.go` returns plain-text errors (e.g. `"missing slug"`). The CLI parses these as raw text. **Do not change error shape — only sweep `json.NewEncoder` → `respondJSON`.**
- `approvals_routes.go` decision endpoint returns plain-text errors. **Only sweep `json.NewDecoder` → `decodeJSON`.**

Both endpoints' decoder failures keep their existing plain-text `http.Error` calls.

The SSE handler in `approvals_routes.go` uses `json.Marshal` directly (not `NewEncoder`) and is fundamentally different — it stays as-is.

---

## File Structure

| File | New / Modify | Responsibility |
|------|--------------|----------------|
| `daemon/internal/daemon/http_helpers.go` | Create | Three helper functions: `decodeJSON`, `respondJSON`, `respondError` |
| `daemon/internal/daemon/http_helpers_test.go` | Create | Unit tests for each helper |
| `daemon/internal/daemon/routes_tasks.go` | Modify | Sweep |
| `daemon/internal/daemon/routes_memory.go` | Modify | Sweep |
| `daemon/internal/daemon/routes_gmail.go` | Modify | Sweep |
| `daemon/internal/daemon/routes_gcal.go` | Modify | Sweep |
| `daemon/internal/daemon/routes_session.go` | Modify | Sweep |
| `daemon/internal/daemon/routes_schedule.go` | Modify | Sweep (also drop the `fmt.Sprintf("{\"error\":%q}", ...)` pattern) |
| `daemon/internal/daemon/health.go` | Modify | Sweep `handleHealth` |
| `daemon/internal/daemon/skill_routes.go` | Modify | Sweep encoders only; delete redundant `writeSkillJSON` wrapper |
| `daemon/internal/daemon/approvals_routes.go` | Modify | Sweep `json.NewDecoder` only |

---

## Task 1: Add `http_helpers.go` and tests

**Files:**
- Create: `daemon/internal/daemon/http_helpers.go`
- Create: `daemon/internal/daemon/http_helpers_test.go`

**Commit:** `refactor(daemon): add HTTP helper functions`

- [ ] **Step 1: Write the failing tests**

Create `daemon/internal/daemon/http_helpers_test.go` with the following content:

```go
package daemon

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDecodeJSON_Valid(t *testing.T) {
	body := strings.NewReader(`{"name":"x","count":3}`)
	r := httptest.NewRequest(http.MethodPost, "/", body)

	var dst struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}
	if err := decodeJSON(r, &dst); err != nil {
		t.Fatalf("decodeJSON returned error: %v", err)
	}
	if dst.Name != "x" || dst.Count != 3 {
		t.Fatalf("decoded value mismatch: %+v", dst)
	}
}

func TestDecodeJSON_Malformed(t *testing.T) {
	body := strings.NewReader(`{not json`)
	r := httptest.NewRequest(http.MethodPost, "/", body)

	var dst struct{}
	if err := decodeJSON(r, &dst); err == nil {
		t.Fatal("decodeJSON should have returned an error for malformed JSON")
	}
}

func TestRespondJSON_WritesHeaderAndBody(t *testing.T) {
	rec := httptest.NewRecorder()
	payload := map[string]string{"hello": "world"}

	respondJSON(rec, http.StatusCreated, payload)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusCreated)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}

	var got map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("body is not valid JSON: %v (%q)", err, rec.Body.String())
	}
	if got["hello"] != "world" {
		t.Fatalf("body = %v, want hello=world", got)
	}
}

func TestRespondError_WritesEnvelope(t *testing.T) {
	rec := httptest.NewRecorder()

	respondError(rec, http.StatusBadRequest, "missing prompt")

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}

	var got map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("body is not valid JSON: %v (%q)", err, rec.Body.String())
	}
	if got["error"] != "missing prompt" {
		t.Fatalf("body = %v, want error=missing prompt", got)
	}
}

func TestRespondError_EscapesQuotesInMessage(t *testing.T) {
	// Regression: routes_schedule.go used fmt.Sprintf with %q to handle
	// quotes in scheduler errors. respondError must preserve that safety.
	rec := httptest.NewRecorder()

	respondError(rec, http.StatusBadRequest, `bad "name" value`)

	var got map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("body is not valid JSON: %v (%q)", err, rec.Body.String())
	}
	if got["error"] != `bad "name" value` {
		t.Fatalf("body = %v, want error=%q", got, `bad "name" value`)
	}
}

// Ensure helpers don't break when given a nil body — used by some
// handlers that respond with a sentinel object without a request body.
func TestRespondJSON_NilSafe(t *testing.T) {
	rec := httptest.NewRecorder()
	respondJSON(rec, http.StatusOK, struct{}{})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !bytes.HasPrefix(rec.Body.Bytes(), []byte("{}")) {
		t.Fatalf("body = %q, want {} prefix", rec.Body.String())
	}
}
```

- [ ] **Step 2: Run tests to verify they fail (compile error)**

Run: `cd daemon && go test ./internal/daemon/ -run 'TestDecodeJSON|TestRespondJSON|TestRespondError' -v`

Expected: FAIL with compile errors — `undefined: decodeJSON`, `undefined: respondJSON`, `undefined: respondError`.

- [ ] **Step 3: Write the helpers**

Create `daemon/internal/daemon/http_helpers.go` with the following content:

```go
package daemon

import (
	"encoding/json"
	"net/http"
)

// decodeJSON decodes the request body JSON into dst. The caller decides how
// to respond on error (some endpoints use the JSON envelope, others use
// plain text), so this only forwards the json.Decoder error.
func decodeJSON(r *http.Request, dst any) error {
	return json.NewDecoder(r.Body).Decode(dst)
}

// respondJSON writes a status line, sets Content-Type to application/json,
// and encodes v as the response body. Encode errors are intentionally
// swallowed — the response is already committed once WriteHeader is called.
func respondJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// respondError writes a JSON error envelope of the form {"error":"<msg>"}.
// Used by handlers that already speak the JSON-error shape; plain-text
// handlers (skill, approvals) keep their own http.Error calls.
func respondError(w http.ResponseWriter, status int, msg string) {
	respondJSON(w, status, map[string]string{"error": msg})
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd daemon && go test ./internal/daemon/ -run 'TestDecodeJSON|TestRespondJSON|TestRespondError' -v`

Expected: PASS — all six tests green.

- [ ] **Step 5: Run the full daemon test suite to confirm no regressions**

Run: `cd daemon && go test ./...`

Expected: PASS for every package.

- [ ] **Step 6: Run `go vet` and `gofmt`**

Run: `cd daemon && go vet ./... && gofmt -l internal/daemon/http_helpers.go internal/daemon/http_helpers_test.go`

Expected: vet clean; gofmt prints nothing (both files already formatted).

- [ ] **Step 7: Commit**

```bash
git add daemon/internal/daemon/http_helpers.go daemon/internal/daemon/http_helpers_test.go
git commit -m "$(cat <<'EOF'
refactor(daemon): add HTTP helper functions

Adds decodeJSON, respondJSON, respondError in
daemon/internal/daemon/http_helpers.go to centralize the JSON
encode/decode/error-envelope pattern repeated across every route
handler.

Helpers are zero-behavior on their own; the per-handler sweep
follows in a separate commit.

Structural change.

AI-Ratio: 1.0
Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 2: Sweep all route handlers to use the new helpers

**Files modified (9):**
- `daemon/internal/daemon/routes_tasks.go`
- `daemon/internal/daemon/routes_memory.go`
- `daemon/internal/daemon/routes_gmail.go`
- `daemon/internal/daemon/routes_gcal.go`
- `daemon/internal/daemon/routes_session.go`
- `daemon/internal/daemon/routes_schedule.go`
- `daemon/internal/daemon/health.go`
- `daemon/internal/daemon/skill_routes.go` (encoders only; plain-text errors stay)
- `daemon/internal/daemon/approvals_routes.go` (decoder only; plain-text errors stay)

**Commit:** `refactor(daemon): sweep handlers to use HTTP helpers`

### Sweep rules (apply consistently across every file)

**Replace pattern A — JSON-envelope error:**
```go
http.Error(w, `{"error":"some message"}`, http.StatusXxx)
```
With:
```go
respondError(w, http.StatusXxx, "some message")
```

**Replace pattern B — `fmt.Sprintf` JSON-envelope error (only in `routes_schedule.go`):**
```go
http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusXxx)
```
With:
```go
respondError(w, http.StatusXxx, err.Error())
```

**Replace pattern C — Encode response (no explicit status, defaults to 200):**
```go
w.Header().Set("Content-Type", "application/json")
json.NewEncoder(w).Encode(v) //nolint:errcheck
```
With:
```go
respondJSON(w, http.StatusOK, v)
```

**Replace pattern D — Encode response with explicit status:**
```go
w.Header().Set("Content-Type", "application/json")
w.WriteHeader(http.StatusCreated)
json.NewEncoder(w).Encode(v) //nolint:errcheck
```
With:
```go
respondJSON(w, http.StatusCreated, v)
```

**Replace pattern E — Decode request body:**
```go
if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
    http.Error(w, `{"error":"invalid JSON body"}`, http.StatusBadRequest)
    return
}
```
With:
```go
if err := decodeJSON(r, &req); err != nil {
    respondError(w, http.StatusBadRequest, "invalid JSON body")
    return
}
```

(For `approvals_routes.go` and `skill_routes.go`, the second line keeps its existing plain-text `http.Error` instead of `respondError`.)

**Out of scope — DO NOT change:**
- `json.Marshal` calls in `approvals_routes.go` SSE handler (lines ~66, ~80) — they predate any envelope.
- Plain-text `http.Error` calls in `skill_routes.go` and `approvals_routes.go` whose body is **not** the `{"error":...}` JSON envelope.
- The `writeSkillJSON` helper in `skill_routes.go` is now redundant — replace its callers with `respondJSON(w, http.StatusOK, v)` and delete the helper.
- `daemon.go` — registration table only, no handler bodies, no changes.

### Sweep order (one file at a time inside the same task)

- [ ] **Step 1: Sweep `routes_tasks.go`**

Apply patterns A/C/D/E. Concrete example for `handleSubmitTask`:

Before:
```go
func (d *Daemon) handleSubmitTask(w http.ResponseWriter, r *http.Request) {
	var req submitTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid JSON body"}`, http.StatusBadRequest)
		return
	}
	if req.Prompt == "" {
		http.Error(w, `{"error":"prompt is required"}`, http.StatusBadRequest)
		return
	}
	if req.TimeoutSec == 0 {
		req.TimeoutSec = d.cfg.DefaultTimeoutSec
	}

	task, err := d.queue.Submit(req.Prompt, req.ReplyTo, req.Priority, req.AllowWrites, req.TimeoutSec, req.Warm)
	if err != nil {
		http.Error(w, `{"error":"failed to submit task"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(task) //nolint:errcheck
}
```

After:
```go
func (d *Daemon) handleSubmitTask(w http.ResponseWriter, r *http.Request) {
	var req submitTaskRequest
	if err := decodeJSON(r, &req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Prompt == "" {
		respondError(w, http.StatusBadRequest, "prompt is required")
		return
	}
	if req.TimeoutSec == 0 {
		req.TimeoutSec = d.cfg.DefaultTimeoutSec
	}

	task, err := d.queue.Submit(req.Prompt, req.ReplyTo, req.Priority, req.AllowWrites, req.TimeoutSec, req.Warm)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to submit task")
		return
	}

	respondJSON(w, http.StatusCreated, task)
}
```

Apply the same shape to `handleListTasks`, `handleGetTask`, `handleCancelTask`, and `handleGetTaskLogs`. Note: `handleGetTaskLogs` returns `text/plain` for the success path — leave that branch alone, only migrate the JSON-envelope error branches.

After the file is migrated, verify the import block: `encoding/json` may no longer be needed; remove it if so.

- [ ] **Step 2: Sweep `routes_memory.go`**

Apply A/C/D/E to all four handlers (`handleSaveMemory`, `handleSearchMemory`, `handleGetMemory`, `handleDeleteMemory`). Drop the `encoding/json` import if unused.

- [ ] **Step 3: Sweep `routes_gmail.go`**

Apply A/C/E to all four handlers (`handleGmailList`, `handleGmailRead`, `handleGmailSend`, `handleGmailReply`) plus the `requireGmail` helper's error branches and the `checkWritePermission` helper's error branch. The success path of `handleGmailSend` and `handleGmailReply` is `w.WriteHeader(http.StatusNoContent)` — keep that as-is (no body, no helper needed). Drop the `encoding/json` import if unused.

- [ ] **Step 4: Sweep `routes_gcal.go`**

Apply A/C/D/E to all six handlers and the `requireCalendar` helper. Same rule for `WriteHeader(http.StatusNoContent)` paths — leave them. Drop the `encoding/json` import if unused.

- [ ] **Step 5: Sweep `routes_session.go`**

Apply C only — every handler responds with JSON, no decoders. Replace each:
```go
w.Header().Set("Content-Type", "application/json")
json.NewEncoder(w).Encode(...) //nolint:errcheck
```
With:
```go
respondJSON(w, http.StatusOK, ...)
```

For the JSON-envelope error branches in `handleSessionStart`, `handleSessionStop`, `handleSessionRestart` (e.g. `http.Error(w, `{"error":"session not configured"}`, http.StatusBadRequest)`), use `respondError`. The 409 conflict branch in `handleSessionStart` likewise.

Drop the `encoding/json` import (it should no longer be needed).

- [ ] **Step 6: Sweep `routes_schedule.go`**

Apply A/B/C/D/E. Particular attention to pattern B — the `fmt.Sprintf` form appears twice (`handleCreateSchedule` and `handleRemoveSchedule`); both become `respondError(w, status, err.Error())`. After the sweep, the `fmt` import is unused — remove it.

Drop the `encoding/json` import if unused.

- [ ] **Step 7: Sweep `health.go`**

Only one handler, `handleHealth`. Replace its tail:
```go
w.Header().Set("Content-Type", "application/json")
w.WriteHeader(http.StatusOK)
json.NewEncoder(w).Encode(resp) //nolint:errcheck
```
With:
```go
respondJSON(w, http.StatusOK, resp)
```

Drop the `encoding/json` import if unused. (Other functions in `health.go` use `runtime`, `time`, `fmt`, etc., but none use `json`.)

- [ ] **Step 8: Sweep `skill_routes.go`**

This file's error responses are plain-text (`http.Error(w, "missing slug", ...)`). **Do NOT change those.**

Sweep only the encoders:
- `handleListSkills`: replace the encoder with `respondJSON(w, http.StatusOK, list)`. The empty-list branch (`d.skillReg == nil`) becomes `respondJSON(w, http.StatusOK, []skills.Skill{})`.
- `handleSkillsSearch`, `handleSkillsInstall`: each currently calls `writeSkillJSON(w, v)` — replace those calls with `respondJSON(w, http.StatusOK, v)`.
- Delete the now-unused `writeSkillJSON` function.

After the sweep, the `encoding/json` import is unused — remove it.

- [ ] **Step 9: Sweep `approvals_routes.go`**

Sweep only the decoder in `approvalDecisionHandler`. The error branches in this handler (and in `approvalStreamHandler`) use plain-text `http.Error` — leave those exactly as written.

Before:
```go
if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
    http.Error(w, "invalid JSON body", http.StatusBadRequest)
    return
}
```

After:
```go
if err := decodeJSON(r, &body); err != nil {
    http.Error(w, "invalid JSON body", http.StatusBadRequest)
    return
}
```

The `json.Marshal` calls in the SSE handler stay. Keep the `encoding/json` import (still used by `json.Marshal`).

- [ ] **Step 10: Build and vet**

Run: `cd daemon && go build ./... && go vet ./...`

Expected: clean build, no vet warnings. If a removed import was missed in Steps 1–9, the build will surface it.

- [ ] **Step 11: Run the full daemon test suite**

Run: `cd daemon && go test ./...`

Expected: every package PASS. The existing handler tests in `daemon_test.go`, `gmail_test.go`, `calendar_test.go`, `daemon_health_test.go`, `skill_install_route_test.go`, `approvals_routes_test.go`, etc. cover the response shapes — they validate that the sweep is behavior-preserving.

- [ ] **Step 12: Verify acceptance with grep**

Run:
```bash
cd daemon
grep -nE 'json\.NewDecoder|json\.NewEncoder' internal/daemon/*.go | grep -v '_test\.go' | grep -v 'http_helpers\.go'
```
Expected: empty output. Every `NewDecoder` / `NewEncoder` lives only in `http_helpers.go` or in test files.

```bash
grep -nE 'http\.Error\(w,\s*`?"\{"error"' internal/daemon/*.go
```
Expected: empty output. No inline JSON-envelope error strings remain.

If either grep returns a match, fix the missed site and re-run.

- [ ] **Step 13: Run `gofmt`**

Run: `cd daemon && gofmt -l internal/daemon/`

Expected: empty output. If any file is listed, run `gofmt -w` on it and re-run.

- [ ] **Step 14: Commit**

```bash
git add daemon/internal/daemon/routes_tasks.go \
        daemon/internal/daemon/routes_memory.go \
        daemon/internal/daemon/routes_gmail.go \
        daemon/internal/daemon/routes_gcal.go \
        daemon/internal/daemon/routes_session.go \
        daemon/internal/daemon/routes_schedule.go \
        daemon/internal/daemon/health.go \
        daemon/internal/daemon/skill_routes.go \
        daemon/internal/daemon/approvals_routes.go
git commit -m "$(cat <<'EOF'
refactor(daemon): sweep handlers to use HTTP helpers

Migrates every route handler in daemon/internal/daemon/ to use
decodeJSON / respondJSON / respondError (added in the previous
commit). Removes ~150 lines of repeated boilerplate and the
//nolint:errcheck noise on json.NewEncoder calls.

Handler-by-handler the response shape is preserved:
- JSON-envelope endpoints continue to emit {"error":"<msg>"} on
  failure; respondError centralizes that shape.
- Plain-text endpoints in skill_routes.go and approvals_routes.go
  keep their existing http.Error calls. Only their NewDecoder /
  NewEncoder calls migrate.
- The redundant writeSkillJSON wrapper in skill_routes.go is
  deleted in favor of respondJSON.
- routes_schedule.go's fmt.Sprintf("{\"error\":%q}", ...) variant
  is replaced by respondError, dropping the fmt import.

Verified by grep:
  no json.NewDecoder/NewEncoder in route handlers
  no inline {"error":"..."} strings in route handlers

Structural change.

AI-Ratio: 1.0
Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Acceptance criteria

From the umbrella spec and TODO #9:

- [ ] `daemon/internal/daemon/http_helpers.go` exists with `decodeJSON`, `respondJSON`, `respondError`.
- [ ] `grep -nE 'json\.NewDecoder|json\.NewEncoder' daemon/internal/daemon/*.go` returns nothing outside `http_helpers.go` and `*_test.go`.
- [ ] `grep -nE 'http\.Error\(w,\s*`?"\{"error"' daemon/internal/daemon/*.go` returns nothing.
- [ ] `daemon/internal/daemon/skill_routes.go`'s plain-text errors are preserved (no behavior change for the CLI).
- [ ] `daemon/internal/daemon/approvals_routes.go`'s plain-text errors are preserved.
- [ ] `cd daemon && go build ./... && go vet ./... && go test ./...` all clean.
- [ ] `gofmt -l` returns nothing across `daemon/`.

---

## Out of scope

- Adding HTTP middleware (auth, structured logging, metrics).
- Migrating away from `net/http` ServeMux to a router framework.
- Replacing `map[string]any` responses with typed structs (different refactor — Refactor #12 family).
- Changing the plain-text error responses in `skill_routes.go` and `approvals_routes.go` to JSON envelopes (would change the CLI contract and is not a goal of this refactor).
- The `json.Marshal` calls inside the SSE handler — they're not the encoder pattern.

---

## Self-review notes

- Spec coverage: every line of TODO #9's "Acceptance criteria" maps to a step or the final grep verification.
- Type consistency: helper signatures match what every step's "after" snippet expects (`decodeJSON(r, &v) error`, `respondJSON(w, status, v)`, `respondError(w, status, msg)`). No drift across tasks.
- Plain-text endpoints (skill, approvals) flagged as exceptions in three places — sweep rules, per-file step, acceptance — to prevent the implementer from over-applying the pattern.
- No phase markers: 2 tasks total (≤3) so the plan runs straight through per the global plan-phase-markers rule.
