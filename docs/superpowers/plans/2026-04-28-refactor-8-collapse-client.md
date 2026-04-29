# Refactor #8 — Collapse `client.go` Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Collapse `daemon/internal/client/client.go` (872 LoC) by deleting the 10 unused bare `Foo` wrappers, renaming `*WithTaskID` to the short name, and extracting `postJSON` / `getJSON` / `deleteResource` transport helpers used by every Gmail/Gcal/memory/task/health call.

**Architecture:** Two commits. (A) Pure rename — delete dead wrappers, rename `*WithTaskID` methods to the short name, update the 10 CLI call sites. (B) Add `daemon/internal/client/transport.go` with three small helpers + sentinel errors (`ErrWriteNotPermitted`, `ErrNotFound`); migrate Gmail/Gcal/memory/task/health/list-schedule methods to use them. Session methods (`Start`/`Stop`/`Restart`/`Status`) and schedule mutation methods (`Create`/`Remove`) keep their existing inline shape — they read the response body on error to surface the daemon's message, a contract the helpers do not replicate.

**Tech Stack:** Go stdlib `net/http` + `encoding/json` + `errors`. Same package as the rest of `daemon/internal/client`.

**Sequence position:** Refactor #8 of the structural batch (`docs/superpowers/specs/2026-04-26-structural-refactor-batch-design.md`). #13, #7, and #9 are already merged.

**Branch:** `refactor/8-collapse-client` (cut from `master`).

---

## Scope

Current state of `daemon/internal/client/client.go`:

- 872 LoC, 38 exported methods.
- Every Gmail/Gcal method exists as a `Foo` + `FooWithTaskID` pair (10 pairs total). The bare `Foo` is a 4-line wrapper that calls `FooWithTaskID(args, "")`. Verified by grep: no caller uses the bare `Foo` form — `cmd/gobrrr/main.go` always passes the task ID via `os.Getenv("GOBRRR_TASK_ID")`.
- Every Gmail/Gcal `*WithTaskID` method follows the same shape: `marshal → NewRequest → Content-Type → optional X-Gobrrr-Task-ID → Do → status check → ReadAll → return`. ~28 lines × 10 methods = ~280 lines of near-duplicate code.
- The 403 → "write not permitted" mapping is repeated in 5 places (Gmail send/reply + Gcal create/update/delete).
- Task / memory / health methods reimplement the same status-check + decode pattern in their own shape.

After this refactor:

- `client.go` ≤ 450 LoC.
- No `*WithTaskID` methods remain — taskID is just an arg of the unified method.
- `postJSON`, `getJSON`, `deleteResource` are used by every Gmail/Gcal/memory/task/health/list-schedule call.
- Session methods and schedule mutation methods stay inline because their error contract differs.

---

## File Structure

| File | New / Modify | Responsibility |
|------|--------------|----------------|
| `daemon/internal/client/transport.go` | Create | `postJSON`, `getJSON`, `deleteResource` + sentinel errors `ErrWriteNotPermitted`, `ErrNotFound` |
| `daemon/internal/client/transport_test.go` | Create | Unit tests for each helper (httptest-driven) |
| `daemon/internal/client/client.go` | Modify | Delete bare wrappers; rename `*WithTaskID`; migrate Gmail/Gcal/memory/task/health/list-schedule methods to helpers |
| `daemon/cmd/gobrrr/main.go` | Modify | Update 10 call sites: drop `WithTaskID` suffix |

The plan does NOT split `client.go` into per-domain files — out of scope per the umbrella spec. After collapse, ~440 LoC in one file is fine.

---

## Phase 1 — Collapse `*WithTaskID` rename (Commit A)

### Task 1: Delete bare `Foo` wrappers in `internal/client/client.go`

**Files:**
- Modify: `daemon/internal/client/client.go`

**Goal:** Drop the 10 bare wrappers (each is a 4-line forwarding call to its `*WithTaskID` twin). No CLI caller invokes them.

- [ ] **Step 1: Verify no caller uses the bare wrappers**

Run:
```bash
cd /home/racterub/github/gobrrr
grep -rnE '\.(Gmail(List|Read|Send|Reply)|Gcal(Today|Week|GetEvent|CreateEvent|UpdateEvent|DeleteEvent))\(' --include='*.go' | grep -v '_test.go' | grep -v 'internal/client/client.go'
```

Expected: every match is to a `*WithTaskID` form (i.e. the call ends `WithTaskID(...)`). All 10 matches should be in `daemon/cmd/gobrrr/main.go`. If any non-`WithTaskID` call appears, abort the plan and report — there is a caller that the spec assumed didn't exist.

- [ ] **Step 2: Delete the 10 bare wrappers + their docstrings**

In `daemon/internal/client/client.go`, delete each function block of this form:

```go
// GmailList fetches a list of messages matching query for the given account.
// It returns the raw JSON response body as a string.
func (c *Client) GmailList(query string, maxResults int, account string) (string, error) {
    return c.GmailListWithTaskID(query, maxResults, account, "")
}
```

Targets (10 functions, current line ranges):

| Method | Line range |
|--------|------------|
| `GmailList` | 312–316 |
| `GmailRead` | 348–352 |
| `GmailSend` | 384–387 |
| `GmailReply` | 418–421 |
| `GcalToday` | 489–493 |
| `GcalWeek` | 525–529 |
| `GcalGetEvent` | 561–565 |
| `GcalCreateEvent` | 597–600 |
| `GcalUpdateEvent` | 631–634 |
| `GcalDeleteEvent` | 665–668 |

Each occupies ~5 lines including its doc comment + blank line. Total ~50 lines deleted.

- [ ] **Step 3: Build to confirm no other caller broke**

Run:
```bash
cd /home/racterub/github/gobrrr/daemon
CGO_ENABLED=0 go build ./...
```

Expected: clean build. If a build error names a missing `Gmail*`/`Gcal*` method, the grep in Step 1 missed a caller — restore the wrapper, find the caller, and re-plan.

### Task 2: Rename `*WithTaskID` methods to short names + update CLI call sites

**Files:**
- Modify: `daemon/internal/client/client.go`
- Modify: `daemon/cmd/gobrrr/main.go`

**Goal:** Strip `WithTaskID` suffix from the 10 surviving Gmail/Gcal methods and update the 10 call sites in `main.go`. After this task, every method takes `taskID` as a regular trailing parameter — no overloading.

- [ ] **Step 1: Rename method definitions in `client.go`**

For each of the 10 methods, change the function name to drop `WithTaskID` and rewrite the doc comment to describe the unified method (drop the "is like Foo but" phrasing).

For example, `GmailList`:

Before:
```go
// GmailListWithTaskID is like GmailList but attaches an X-Gobrrr-Task-ID header.
func (c *Client) GmailListWithTaskID(query string, maxResults int, account, taskID string) (string, error) {
```

After:
```go
// GmailList fetches a list of messages matching query for the given account.
// If taskID is non-empty it is sent as the X-Gobrrr-Task-ID header so the
// daemon can attribute the call to a specific task (used for write
// authorization). It returns the raw JSON response body as a string.
func (c *Client) GmailList(query string, maxResults int, account, taskID string) (string, error) {
```

Apply the same pattern to: `GmailRead`, `GmailSend`, `GmailReply`, `GcalToday`, `GcalWeek`, `GcalGetEvent`, `GcalCreateEvent`, `GcalUpdateEvent`, `GcalDeleteEvent`. Pick a one- or two-line docstring per method that reflects what it does.

- [ ] **Step 2: Update the 10 call sites in `cmd/gobrrr/main.go`**

Run:
```bash
cd /home/racterub/github/gobrrr
sed -i -E 's/\.(Gmail(List|Read|Send|Reply)|Gcal(Today|Week|GetEvent|CreateEvent|UpdateEvent|DeleteEvent))WithTaskID\(/\.\1(/g' daemon/cmd/gobrrr/main.go
```

Verify:
```bash
grep -n 'WithTaskID' daemon/cmd/gobrrr/main.go
```
Expected: empty.

```bash
grep -nE '\.(Gmail(List|Read|Send|Reply)|Gcal(Today|Week|GetEvent|CreateEvent|UpdateEvent|DeleteEvent))\(' daemon/cmd/gobrrr/main.go
```
Expected: 10 matches.

- [ ] **Step 3: Build, vet, and test**

Run:
```bash
cd /home/racterub/github/gobrrr/daemon
CGO_ENABLED=0 go build ./...
go vet ./...
go test ./...
```

Expected: all green. The Go compiler enforces the rename — any missed call site fails to compile.

### Task 3: Commit Phase 1

**Files modified:**
- `daemon/internal/client/client.go`
- `daemon/cmd/gobrrr/main.go`

- [ ] **Step 1: Confirm `gofmt` is clean**

Run:
```bash
cd /home/racterub/github/gobrrr/daemon
gofmt -l internal/client/ cmd/gobrrr/
```
Expected: empty.

- [ ] **Step 2: Commit**

```bash
cd /home/racterub/github/gobrrr
git add daemon/internal/client/client.go daemon/cmd/gobrrr/main.go
git commit -m "$(cat <<'EOF'
refactor(client): drop bare wrappers, rename WithTaskID variants

Deletes the 10 dead bare Gmail/Gcal wrappers (GmailList, GmailRead,
GmailSend, GmailReply, GcalToday, GcalWeek, GcalGetEvent,
GcalCreateEvent, GcalUpdateEvent, GcalDeleteEvent). Each was a
4-line forwarder calling its *WithTaskID twin with taskID="";
verified via grep that no caller used the bare form.

Renames the 10 *WithTaskID methods to the short name. taskID is
now a regular trailing parameter; cmd/gobrrr/main.go is updated to
drop the WithTaskID suffix at the 10 call sites.

No public-API change beyond the rename. The CLI is the only caller.

Structural change.

AI-Ratio: 1.0
Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Phase 2 — Extract transport helpers and sweep (Commit B)

### Task 1: Add `transport.go` and tests

**Files:**
- Create: `daemon/internal/client/transport.go`
- Create: `daemon/internal/client/transport_test.go`

**Goal:** Add three small helpers (`postJSON`, `getJSON`, `deleteResource`) plus two sentinel errors (`ErrWriteNotPermitted`, `ErrNotFound`). Test-first.

- [ ] **Step 1: Write the failing tests**

Create `daemon/internal/client/transport_test.go`:

```go
package client

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPostJSON_OK(t *testing.T) {
	var gotBody, gotCT, gotTask string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		gotCT = r.Header.Get("Content-Type")
		gotTask = r.Header.Get("X-Gobrrr-Task-ID")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer srv.Close()

	c := newFromHTTPServer(srv)
	raw, err := c.postJSON("/x", map[string]string{"k": "v"}, "task-123", http.StatusOK)
	require.NoError(t, err)
	assert.Equal(t, `{"ok":true}`, string(raw))
	assert.Equal(t, "application/json", gotCT)
	assert.Equal(t, "task-123", gotTask)
	assert.Contains(t, gotBody, `"k":"v"`)
}

func TestPostJSON_NoTaskID_NoHeader(t *testing.T) {
	var gotTask string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotTask = r.Header.Get("X-Gobrrr-Task-ID")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := newFromHTTPServer(srv)
	_, err := c.postJSON("/x", struct{}{}, "", http.StatusNoContent)
	require.NoError(t, err)
	assert.Equal(t, "", gotTask)
}

func TestPostJSON_403_ReturnsErrWriteNotPermitted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	c := newFromHTTPServer(srv)
	_, err := c.postJSON("/x", struct{}{}, "task-1", http.StatusNoContent)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrWriteNotPermitted))
	assert.Equal(t, "write not permitted: task does not have allow_writes", err.Error())
}

func TestPostJSON_UnexpectedStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := newFromHTTPServer(srv)
	_, err := c.postJSON("/x", struct{}{}, "", http.StatusOK)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "500")
	assert.Contains(t, err.Error(), "/x")
}

func TestPostJSON_NilBody(t *testing.T) {
	var gotLen int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotLen = r.ContentLength
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := newFromHTTPServer(srv)
	_, err := c.postJSON("/x", nil, "", http.StatusOK)
	require.NoError(t, err)
	assert.Equal(t, int64(0), gotLen)
}

func TestGetJSON_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		_, _ = io.WriteString(w, `[1,2,3]`)
	}))
	defer srv.Close()

	c := newFromHTTPServer(srv)
	raw, err := c.getJSON("/y")
	require.NoError(t, err)
	assert.Equal(t, "[1,2,3]", strings.TrimSpace(string(raw)))
}

func TestGetJSON_404_ReturnsErrNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := newFromHTTPServer(srv)
	_, err := c.getJSON("/y")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNotFound))
}

func TestGetJSON_UnexpectedStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := newFromHTTPServer(srv)
	_, err := c.getJSON("/y")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "500")
}

func TestDeleteResource_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodDelete, r.Method)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := newFromHTTPServer(srv)
	err := c.deleteResource("/z", http.StatusNoContent)
	require.NoError(t, err)
}

func TestDeleteResource_404_ReturnsErrNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := newFromHTTPServer(srv)
	err := c.deleteResource("/z", http.StatusNoContent)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNotFound))
}

func TestDeleteResource_UnexpectedStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	c := newFromHTTPServer(srv)
	err := c.deleteResource("/z", http.StatusNoContent)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "400")
}
```

Note: `newFromHTTPServer` is the existing test helper in `internal/client/approvals_test.go` — same package, so it's directly available.

- [ ] **Step 2: Run tests to verify they fail (compile error)**

Run:
```bash
cd /home/racterub/github/gobrrr/daemon
go test ./internal/client/ -run 'TestPostJSON|TestGetJSON|TestDeleteResource' -v
```

Expected: FAIL — `undefined: postJSON`, `undefined: getJSON`, `undefined: deleteResource`, `undefined: ErrWriteNotPermitted`, `undefined: ErrNotFound`.

- [ ] **Step 3: Write the helpers**

Create `daemon/internal/client/transport.go`:

```go
package client

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// ErrWriteNotPermitted is returned when the daemon rejects a write because
// the submitting task lacks allow_writes. The message text is preserved
// verbatim from the pre-helper Gmail/Gcal call sites.
var ErrWriteNotPermitted = errors.New("write not permitted: task does not have allow_writes")

// ErrNotFound is returned when the daemon responds 404 for a single-resource
// lookup. Callers wrap this with their per-resource message via errors.Is.
var ErrNotFound = errors.New("not found")

// postJSON marshals body to JSON, POSTs it to path with Content-Type:
// application/json, and returns the response body bytes on expectedStatus.
//
// When taskID is non-empty, X-Gobrrr-Task-ID is set so the daemon can
// authorize the call against the originating task. When body is nil, the
// request is sent with no body (used for parameterless POSTs).
//
// Status mapping:
//   - 403 → ErrWriteNotPermitted (preserves the pre-helper error text).
//   - expectedStatus → returns response body bytes.
//   - other → fmt.Errorf("unexpected status %d from POST %s", code, path).
func (c *Client) postJSON(path string, body any, taskID string, expectedStatus int) ([]byte, error) {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshalling request: %w", err)
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequest(http.MethodPost, c.baseURL+path, reader)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if taskID != "" {
		req.Header.Set("X-Gobrrr-Task-ID", taskID)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("POST %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusForbidden {
		return nil, ErrWriteNotPermitted
	}
	if resp.StatusCode != expectedStatus {
		return nil, fmt.Errorf("unexpected status %d from POST %s", resp.StatusCode, path)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}
	return raw, nil
}

// getJSON GETs path. Returns response body bytes on 200 OK; ErrNotFound on
// 404; "unexpected status" error otherwise.
func (c *Client) getJSON(path string) ([]byte, error) {
	resp, err := c.httpClient.Get(c.baseURL + path)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d from GET %s", resp.StatusCode, path)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}
	return raw, nil
}

// deleteResource issues DELETE path. Returns nil on expectedStatus,
// ErrNotFound on 404, "unexpected status" error otherwise.
func (c *Client) deleteResource(path string, expectedStatus int) error {
	req, err := http.NewRequest(http.MethodDelete, c.baseURL+path, nil)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("DELETE %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return ErrNotFound
	}
	if resp.StatusCode != expectedStatus {
		return fmt.Errorf("unexpected status %d from DELETE %s", resp.StatusCode, path)
	}
	return nil
}
```

- [ ] **Step 4: Run helper tests to verify pass**

Run:
```bash
cd /home/racterub/github/gobrrr/daemon
go test ./internal/client/ -run 'TestPostJSON|TestGetJSON|TestDeleteResource' -v
```

Expected: 11 tests PASS.

- [ ] **Step 5: Run the full test suite**

Run:
```bash
cd /home/racterub/github/gobrrr/daemon
go test ./...
```

Expected: every package PASS. The new helpers don't yet have callers in `client.go` so no regression possible.

### Task 2: Migrate Gmail and Gcal methods to helpers

**Files:**
- Modify: `daemon/internal/client/client.go`

**Goal:** Replace each Gmail/Gcal method's marshal-build-Do-status-readall block with a call to `postJSON`. Each method shrinks from ~28 lines to ~6.

- [ ] **Step 1: Migrate the 5 Gmail/Gcal "read" methods (return `(string, error)`)**

For each of `GmailList`, `GmailRead`, `GcalToday`, `GcalWeek`, `GcalGetEvent`, replace the body with the helper call.

Example for `GmailList`:

Before:
```go
func (c *Client) GmailList(query string, maxResults int, account, taskID string) (string, error) {
	body := gmailListRequest{Query: query, MaxResults: maxResults, Account: account}
	data, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("marshalling request: %w", err)
	}
	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/gmail/list", bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if taskID != "" {
		req.Header.Set("X-Gobrrr-Task-ID", taskID)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("POST /gmail/list: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status %d from POST /gmail/list", resp.StatusCode)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading response: %w", err)
	}
	return string(raw), nil
}
```

After:
```go
func (c *Client) GmailList(query string, maxResults int, account, taskID string) (string, error) {
	body := gmailListRequest{Query: query, MaxResults: maxResults, Account: account}
	raw, err := c.postJSON("/gmail/list", body, taskID, http.StatusOK)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}
```

Apply the same shape to `GmailRead`, `GcalToday`, `GcalWeek`, `GcalGetEvent`. The path string and body type vary; the rest is identical.

- [ ] **Step 2: Migrate the 5 Gmail/Gcal "write" methods (return `error`)**

For each of `GmailSend`, `GmailReply`, `GcalCreateEvent`, `GcalUpdateEvent`, `GcalDeleteEvent`, replace the body with a `postJSON` call expecting `http.StatusNoContent`. The 403 → "write not permitted" mapping is preserved verbatim by `ErrWriteNotPermitted`.

Example for `GmailSend`:

Before:
```go
func (c *Client) GmailSend(to, subject, body, account, taskID string) error {
	reqBody := gmailSendRequest{To: to, Subject: subject, Body: body, Account: account}
	data, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("marshalling request: %w", err)
	}
	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/gmail/send", bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if taskID != "" {
		req.Header.Set("X-Gobrrr-Task-ID", taskID)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("POST /gmail/send: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("write not permitted: task does not have allow_writes")
	}
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("unexpected status %d from POST /gmail/send", resp.StatusCode)
	}
	return nil
}
```

After:
```go
func (c *Client) GmailSend(to, subject, body, account, taskID string) error {
	reqBody := gmailSendRequest{To: to, Subject: subject, Body: body, Account: account}
	_, err := c.postJSON("/gmail/send", reqBody, taskID, http.StatusNoContent)
	return err
}
```

Apply the same shape to `GmailReply`, `GcalCreateEvent`, `GcalUpdateEvent`, `GcalDeleteEvent`.

- [ ] **Step 3: Build and test**

Run:
```bash
cd /home/racterub/github/gobrrr/daemon
CGO_ENABLED=0 go build ./...
go test ./...
```

Expected: clean build + every package PASS. The daemon-level `gmail_test.go` and `calendar_test.go` validate the wire protocol — they confirm the migration is behavior-preserving.

### Task 3: Migrate task, memory, health, and list-schedule methods

**Files:**
- Modify: `daemon/internal/client/client.go`

**Goal:** Sweep the remaining methods that fit the helper shape: 5 task methods, 4 memory methods, `Health`, `ListSchedules`. Session methods (`SessionStart`/`Stop`/`Restart`/`Status`), `CreateSchedule`, and `RemoveSchedule` stay inline — they read the response body on error to surface the daemon's message, which the helpers don't replicate.

- [ ] **Step 1: Migrate task methods (5)**

Targets: `SubmitTask`, `ListTasks`, `GetTask`, `CancelTask`, `GetLogs`.

`SubmitTask` (postJSON expecting 201):
```go
func (c *Client) SubmitTask(prompt, replyTo string, priority int, allowWrites bool, timeoutSec int, warm bool) (*daemon.Task, error) {
	body := submitTaskRequest{
		Prompt:      prompt,
		ReplyTo:     replyTo,
		Priority:    priority,
		AllowWrites: allowWrites,
		TimeoutSec:  timeoutSec,
		Warm:        warm,
	}
	raw, err := c.postJSON("/tasks", body, "", http.StatusCreated)
	if err != nil {
		return nil, err
	}
	var task daemon.Task
	if err := json.Unmarshal(raw, &task); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return &task, nil
}
```

`ListTasks` (getJSON; build path with optional query):
```go
func (c *Client) ListTasks(all bool) ([]*daemon.Task, error) {
	path := "/tasks"
	if all {
		path += "?all=true"
	}
	raw, err := c.getJSON(path)
	if err != nil {
		return nil, err
	}
	var tasks []*daemon.Task
	if err := json.Unmarshal(raw, &tasks); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return tasks, nil
}
```

`GetTask` (getJSON + per-resource not-found message):
```go
func (c *Client) GetTask(id string) (*daemon.Task, error) {
	raw, err := c.getJSON("/tasks/" + id)
	if errors.Is(err, ErrNotFound) {
		return nil, fmt.Errorf("task %q not found", id)
	}
	if err != nil {
		return nil, err
	}
	var task daemon.Task
	if err := json.Unmarshal(raw, &task); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return &task, nil
}
```

`CancelTask` (deleteResource expecting 204 + per-resource not-found message):
```go
func (c *Client) CancelTask(id string) error {
	err := c.deleteResource("/tasks/"+id, http.StatusNoContent)
	if errors.Is(err, ErrNotFound) {
		return fmt.Errorf("task %q not found", id)
	}
	return err
}
```

`GetLogs` (getJSON; convert bytes to string; per-resource not-found message):
```go
func (c *Client) GetLogs(id string) (string, error) {
	raw, err := c.getJSON("/tasks/" + id + "/logs")
	if errors.Is(err, ErrNotFound) {
		return "", fmt.Errorf("logs for task %q not found", id)
	}
	if err != nil {
		return "", err
	}
	return string(raw), nil
}
```

- [ ] **Step 2: Migrate memory methods (4)**

Targets: `SaveMemory`, `SearchMemory`, `GetMemory`, `DeleteMemory`. Same shapes as Tasks.

`SaveMemory` (postJSON expecting 201):
```go
func (c *Client) SaveMemory(content string, tags []string, source string) (*memory.Entry, error) {
	body := saveMemoryRequest{Content: content, Tags: tags, Source: source}
	raw, err := c.postJSON("/memory", body, "", http.StatusCreated)
	if err != nil {
		return nil, err
	}
	var entry memory.Entry
	if err := json.Unmarshal(raw, &entry); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return &entry, nil
}
```

`SearchMemory` (getJSON; URL builder kept; pass path+query relative to baseURL):
```go
func (c *Client) SearchMemory(query string, tags []string, limit int) ([]*memory.Entry, error) {
	u, _ := url.Parse(c.baseURL + "/memory")
	q := u.Query()
	if query != "" {
		q.Set("q", query)
	}
	if len(tags) > 0 {
		q.Set("tags", strings.Join(tags, ","))
	}
	if limit > 0 {
		q.Set("limit", fmt.Sprintf("%d", limit))
	}
	u.RawQuery = q.Encode()

	raw, err := c.getJSON(strings.TrimPrefix(u.String(), c.baseURL))
	if err != nil {
		return nil, err
	}
	var entries []*memory.Entry
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return entries, nil
}
```

`GetMemory` (getJSON + per-resource not-found message):
```go
func (c *Client) GetMemory(id string) (*memory.Entry, error) {
	raw, err := c.getJSON("/memory/" + id)
	if errors.Is(err, ErrNotFound) {
		return nil, fmt.Errorf("memory %q not found", id)
	}
	if err != nil {
		return nil, err
	}
	var entry memory.Entry
	if err := json.Unmarshal(raw, &entry); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return &entry, nil
}
```

`DeleteMemory` (deleteResource expecting 204 + per-resource not-found message):
```go
func (c *Client) DeleteMemory(id string) error {
	err := c.deleteResource("/memory/"+id, http.StatusNoContent)
	if errors.Is(err, ErrNotFound) {
		return fmt.Errorf("memory %q not found", id)
	}
	return err
}
```

- [ ] **Step 3: Migrate `Health` and `ListSchedules`**

`Health`:
```go
func (c *Client) Health() (map[string]interface{}, error) {
	raw, err := c.getJSON("/health")
	if err != nil {
		return nil, err
	}
	var result map[string]interface{}
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return result, nil
}
```

`ListSchedules`:
```go
func (c *Client) ListSchedules() ([]map[string]any, error) {
	raw, err := c.getJSON("/schedules")
	if err != nil {
		return nil, err
	}
	var result []map[string]any
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return result, nil
}
```

Note: `ListSchedules` previously had no status check; `getJSON` adds one. This is a strict improvement — non-200 now produces a clear error instead of a silent decode failure. The daemon route always returns 200 on success, so production behavior is unchanged.

- [ ] **Step 4: Reconcile imports**

Add `"errors"` to the imports of `client.go` (used by `errors.Is` in the migrated task/memory/log methods).

After the migrations, `bytes` and `io` should no longer be referenced from `client.go` — those calls now live in `transport.go`. Run:

```bash
cd /home/racterub/github/gobrrr/daemon
CGO_ENABLED=0 go build ./internal/client/...
```

Go's import-management surfaces unused imports as errors. Remove any flagged. The remaining `client.go` imports are likely:

```go
import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/racterub/gobrrr/internal/daemon"
	"github.com/racterub/gobrrr/internal/memory"
)
```

(`io` may still be needed if any inline session/schedule method continues to use `io.ReadAll`.)

- [ ] **Step 5: Build, vet, test**

Run:
```bash
cd /home/racterub/github/gobrrr/daemon
CGO_ENABLED=0 go build ./...
go vet ./...
go test ./...
```

Expected: clean.

### Task 4: Verify acceptance and commit Phase 2

**Goal:** Confirm `client.go` ≤ 450 LoC, helpers are present and used, then commit.

- [ ] **Step 1: Verify line count**

Run:
```bash
cd /home/racterub/github/gobrrr/daemon
wc -l internal/client/client.go internal/client/transport.go internal/client/transport_test.go
```

Expected: `client.go` ≤ 450. If over, look at remaining inline patterns and tighten — the largest remaining inline block is the three session methods (`SessionStart`/`Stop`/`Restart`); trimming their docstrings or consolidating their body-on-error reads into a tiny local helper is one safe lever. Do not migrate them to `postJSON` if doing so loses the daemon error-message-in-body contract.

- [ ] **Step 2: Verify helpers are used by every Gmail/Gcal/memory/task/health call**

Run:
```bash
cd /home/racterub/github/gobrrr/daemon
grep -nE 'c\.postJSON|c\.getJSON|c\.deleteResource' internal/client/client.go | wc -l
```

Expected: ≥ 20 (10 Gmail/Gcal + 5 task + 4 memory + 1 health + 1 ListSchedules = 21).

```bash
grep -n 'WithTaskID' internal/client/client.go internal/client/transport.go
```

Expected: empty.

```bash
grep -nE 'json\.NewDecoder|json\.NewEncoder|http\.NewRequest' internal/client/client.go
```

Expected: at most a small handful, only in the methods that intentionally stay inline (`SessionStart`, `SessionStop`, `SessionRestart`, `SessionStatus`, `CreateSchedule`, `RemoveSchedule`).

- [ ] **Step 3: Run gofmt**

Run:
```bash
cd /home/racterub/github/gobrrr/daemon
gofmt -l internal/client/
```

Expected: empty.

- [ ] **Step 4: Final full-suite check**

Run:
```bash
cd /home/racterub/github/gobrrr/daemon
CGO_ENABLED=0 go build ./...
go vet ./...
go test ./...
```

Expected: all green.

- [ ] **Step 5: Commit**

```bash
cd /home/racterub/github/gobrrr
git add daemon/internal/client/transport.go \
        daemon/internal/client/transport_test.go \
        daemon/internal/client/client.go
git commit -m "$(cat <<'EOF'
refactor(client): extract postJSON/getJSON/deleteResource helpers

Adds internal/client/transport.go with three small helpers and two
sentinel errors:

  postJSON(path, body, taskID, expectedStatus) ([]byte, error)
  getJSON(path) ([]byte, error)
  deleteResource(path, expectedStatus) error
  ErrWriteNotPermitted (403 on Gmail/Gcal write endpoints)
  ErrNotFound (404 for single-resource lookups)

The 403 → "write not permitted: task does not have allow_writes"
mapping previously inlined in 5 Gmail/Gcal write methods is now
expressed once via the sentinel, with the error text preserved
verbatim. 404 → per-resource not-found messages remain at the call
site via errors.Is(err, ErrNotFound).

Migrates every Gmail, Gcal, task, memory, health, and ListSchedules
call to the helpers. Session start/stop/restart/status,
CreateSchedule, and RemoveSchedule keep their inline shape because
they intentionally read the response body into the error message —
a contract the helpers don't replicate.

client.go shrinks from 872 LoC to ~440. Coverage in
internal/client/transport_test.go validates header propagation,
status-code → sentinel mapping, and nil-body POST behavior.

Structural change.

AI-Ratio: 1.0
Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Acceptance criteria

From the umbrella spec and TODO #8:

- [ ] `daemon/internal/client/client.go` ≤ 450 LoC.
- [ ] No `*WithTaskID` methods remain.
- [ ] `daemon/internal/client/transport.go` exists with `postJSON`, `getJSON`, `deleteResource`.
- [ ] Every Gmail / Gcal / memory / task / health / list-schedule call uses the helpers.
- [ ] 403 → "write not permitted: task does not have allow_writes" mapping preserved verbatim via `ErrWriteNotPermitted`.
- [ ] 404 → not-found mapping preserved per-resource (`task %q not found`, `memory %q not found`, `logs for task %q not found`) via `errors.Is(err, ErrNotFound)`.
- [ ] `cd daemon && go build ./... && go vet ./... && go test ./...` clean at every commit.
- [ ] `gofmt -l daemon/internal/client/` empty.

---

## Out of scope

- Replacing `map[string]any` returns with typed structs (Refactor #12 family).
- Splitting `client.go` into per-domain files. After collapse, ~440 LoC is fine.
- Migrating session methods or schedule mutation methods (`CreateSchedule`, `RemoveSchedule`) to helpers. Their "include daemon error body in returned error message" contract is a different shape; keeping them inline preserves observable behavior.
- HTTP middleware (auth, logging, retries).
- Generic typed-decoder helpers — `json.Unmarshal(raw, &v)` at the call site is fine.

---

## Self-review notes

- Spec coverage: every TODO #8 acceptance criterion maps to a step or a verification grep.
- Type consistency: `postJSON` / `getJSON` / `deleteResource` signatures match what every migration step's "after" snippet calls.
- 403 mapping: tested explicitly in `TestPostJSON_403_ReturnsErrWriteNotPermitted` with the verbatim error string assertion.
- 404 per-resource messages: each migration step shows the `errors.Is(err, ErrNotFound)` wrap so the visible CLI error text is identical to today's.
- Phase markers: 2 phases (per the global plan-phase-markers rule). Phase 1 has 3 tasks, Phase 2 has 4 tasks — both ≤4. Stop at the phase boundary so the user can `/compact` between commits A and B.
