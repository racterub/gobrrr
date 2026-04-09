# gobrrr-telegram Channel Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a Go drop-in replacement for the official `telegram@0.0.4` Claude Code plugin — same state files, same MCP tool surface, same skills compatibility.

**Architecture:** Single Go binary living at `daemon/cmd/gobrrr-telegram/`. Speaks MCP over stdio via `mark3labs/mcp-go`, talks to Telegram via `go-telegram/bot`, shares state dir `~/.claude/channels/telegram/` with the official `/telegram:access` and `/telegram:configure` skills. Packaged as a new plugin `gobrrr-telegram` in the gobrrr marketplace.

**Tech Stack:** Go 1.25, `github.com/mark3labs/mcp-go`, `github.com/go-telegram/bot`, standard library only otherwise. `CGO_ENABLED=0`. Module `github.com/racterub/gobrrr`.

**Reference:** Design spec at `docs/superpowers/specs/2026-04-06-gobrrr-telegram-channel-design.md`. Source of truth for behavior: `~/.claude/plugins/cache/claude-plugins-official/telegram/0.0.4/server.ts`. When a task references "matching the official plugin", read that file.

---

## File Structure

```
daemon/cmd/gobrrr-telegram/
  main.go                            # wiring, signals, .env load
  access/
    access.go                        # types, Load/Save, defaults, corrupt recovery
    access_test.go
    pairing.go                       # code generation, expiry prune
    pairing_test.go
    gate.go                          # inbound gate: Check()
    gate_test.go
  permission/
    permission.go                    # PERMISSION_REPLY_RE matcher
    permission_test.go
  chunker/
    chunker.go                       # Split() length + newline modes
    chunker_test.go
  mcpserver/
    server.go                        # MCP server init, tool registration
    tools_reply.go                   # reply tool
    tools_edit.go                    # edit_message tool
    tools_react.go                   # react tool
    tools_download.go                # download_attachment tool
    sendable.go                      # assertSendable
    sendable_test.go
    notify.go                        # inbound notification emitter
  bot/
    bot.go                           # bot wrapper, long-poll start
    inbound.go                       # update handler
    outbound.go                      # reply/edit/react callable from mcpserver
    download.go                      # file download into inbox/

plugins/gobrrr-telegram/
  plugin.json                        # Claude Code plugin manifest
  README.md

scripts/
  install-telegram-channel.sh        # build + install binary
```

---

## Task 1: Scaffold command + add dependencies

**Files:**
- Create: `daemon/cmd/gobrrr-telegram/main.go`
- Modify: `daemon/go.mod`, `daemon/go.sum`

- [ ] **Step 1: Add dependencies**

Run:
```bash
cd daemon
go get github.com/mark3labs/mcp-go@latest
go get github.com/go-telegram/bot@latest
```

Expected: both modules added to `go.mod`, checksums in `go.sum`.

- [ ] **Step 2: Create stub main**

Create `daemon/cmd/gobrrr-telegram/main.go`:

```go
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "gobrrr-telegram: starting (stub)")
}
```

- [ ] **Step 3: Verify build**

Run: `cd daemon && CGO_ENABLED=0 go build -o /tmp/gobrrr-telegram ./cmd/gobrrr-telegram/`
Expected: exit 0, `/tmp/gobrrr-telegram` binary exists.

- [ ] **Step 4: Commit (structural)**

```bash
git add daemon/go.mod daemon/go.sum daemon/cmd/gobrrr-telegram/main.go
git commit -m "chore: scaffold gobrrr-telegram command with mcp-go + go-telegram deps"
```

---

## Task 2: permission package

**Files:**
- Create: `daemon/cmd/gobrrr-telegram/permission/permission.go`
- Create: `daemon/cmd/gobrrr-telegram/permission/permission_test.go`

- [ ] **Step 1: Write failing tests**

Create `permission/permission_test.go`:

```go
package permission

import "testing"

func TestMatch(t *testing.T) {
	cases := []struct {
		in       string
		wantOK   bool
		wantYes  bool
		wantCode string
	}{
		{"y abcde", true, true, "abcde"},
		{"yes abcde", true, true, "abcde"},
		{"YES ABCDE", true, true, "abcde"},
		{"n abcde", true, false, "abcde"},
		{"no abcde", true, false, "abcde"},
		{"  y  abcde  ", true, true, "abcde"},
		// bare yes/no rejected
		{"yes", false, false, ""},
		{"y", false, false, ""},
		// prefix/suffix chatter rejected
		{"y abcde foo", false, false, ""},
		{"hey y abcde", false, false, ""},
		// 'l' forbidden in code
		{"y ablde", false, false, ""},
		// wrong length
		{"y abcd", false, false, ""},
		{"y abcdef", false, false, ""},
	}
	for _, c := range cases {
		ok, yes, code := Match(c.in)
		if ok != c.wantOK || yes != c.wantYes || code != c.wantCode {
			t.Errorf("Match(%q) = (%v,%v,%q); want (%v,%v,%q)",
				c.in, ok, yes, code, c.wantOK, c.wantYes, c.wantCode)
		}
	}
}
```

- [ ] **Step 2: Run, expect fail**

Run: `cd daemon && go test ./cmd/gobrrr-telegram/permission/...`
Expected: FAIL — package doesn't exist.

- [ ] **Step 3: Implement**

Create `permission/permission.go`:

```go
// Package permission implements the permission-reply regex matcher used
// by pairing and approval gates. Mirrors the official telegram plugin's
// PERMISSION_REPLY_RE: 5 lowercase letters a-z minus 'l', case-insensitive,
// no bare yes/no, no prefix/suffix chatter.
package permission

import (
	"regexp"
	"strings"
)

var re = regexp.MustCompile(`^\s*(y|yes|n|no)\s+([a-km-zA-KM-Z]{5})\s*$`)

// Match returns (ok, isYes, loweredCode). ok is false if the input is not
// a valid permission reply.
func Match(s string) (ok bool, yes bool, code string) {
	m := re.FindStringSubmatch(s)
	if m == nil {
		return false, false, ""
	}
	verb := strings.ToLower(m[1])
	return true, verb == "y" || verb == "yes", strings.ToLower(m[2])
}
```

- [ ] **Step 4: Run, expect pass**

Run: `cd daemon && go test ./cmd/gobrrr-telegram/permission/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add daemon/cmd/gobrrr-telegram/permission/
git commit -m "feat(telegram): permission-reply regex matcher"
```

---

## Task 3: chunker package

**Files:**
- Create: `daemon/cmd/gobrrr-telegram/chunker/chunker.go`
- Create: `daemon/cmd/gobrrr-telegram/chunker/chunker_test.go`

- [ ] **Step 1: Write failing tests**

Create `chunker/chunker_test.go`:

```go
package chunker

import (
	"strings"
	"testing"
)

func TestSplitLength(t *testing.T) {
	// short → single chunk
	got := Split("hello", ModeLength, 10)
	if len(got) != 1 || got[0] != "hello" {
		t.Fatalf("short: %v", got)
	}
	// exact boundary
	got = Split(strings.Repeat("a", 10), ModeLength, 10)
	if len(got) != 1 {
		t.Fatalf("exact: %v", got)
	}
	// over boundary → split
	got = Split(strings.Repeat("a", 25), ModeLength, 10)
	if len(got) != 3 || len(got[0]) != 10 || len(got[1]) != 10 || len(got[2]) != 5 {
		t.Fatalf("over: %v", got)
	}
}

func TestSplitNewline(t *testing.T) {
	in := "para1 line\n\npara2 line\n\npara3"
	got := Split(in, ModeNewline, 15)
	// each paragraph fits in 15 → one chunk each
	if len(got) != 3 {
		t.Fatalf("newline chunks: %v", got)
	}
	// a paragraph longer than the limit still must fit (hard-split fallback)
	long := strings.Repeat("x", 40)
	got = Split(long, ModeNewline, 15)
	if len(got) < 3 {
		t.Fatalf("long newline: %v", got)
	}
}

func TestSplitEmpty(t *testing.T) {
	got := Split("", ModeLength, 10)
	if len(got) != 1 || got[0] != "" {
		t.Fatalf("empty: %v", got)
	}
}

func TestSplitClampLimit(t *testing.T) {
	// limit above hard cap should clamp to 4096
	got := Split(strings.Repeat("a", 5000), ModeLength, 10000)
	if len(got[0]) != 4096 {
		t.Fatalf("clamp: %d", len(got[0]))
	}
}
```

- [ ] **Step 2: Run, expect fail**

Run: `cd daemon && go test ./cmd/gobrrr-telegram/chunker/...`
Expected: FAIL.

- [ ] **Step 3: Implement**

Create `chunker/chunker.go`:

```go
// Package chunker splits outbound Telegram messages per the official
// plugin's chunkMode + textChunkLimit behavior.
package chunker

import "strings"

type Mode string

const (
	ModeLength  Mode = "length"
	ModeNewline Mode = "newline"

	HardCap = 4096 // Telegram's sendMessage text limit
)

// Split returns 1+ chunks. Empty input → one empty chunk (caller decides).
// limit is clamped to HardCap. Mode "newline" splits on blank lines and
// falls back to hard length splitting for any paragraph still too large.
func Split(text string, mode Mode, limit int) []string {
	if limit <= 0 || limit > HardCap {
		limit = HardCap
	}
	if text == "" {
		return []string{""}
	}
	if mode == ModeNewline {
		return splitNewline(text, limit)
	}
	return splitLength(text, limit)
}

func splitLength(text string, limit int) []string {
	var out []string
	for len(text) > limit {
		out = append(out, text[:limit])
		text = text[limit:]
	}
	out = append(out, text)
	return out
}

func splitNewline(text string, limit int) []string {
	paras := strings.Split(text, "\n\n")
	var out []string
	var cur strings.Builder
	flush := func() {
		if cur.Len() > 0 {
			out = append(out, cur.String())
			cur.Reset()
		}
	}
	for _, p := range paras {
		if len(p) > limit {
			flush()
			out = append(out, splitLength(p, limit)...)
			continue
		}
		sep := ""
		if cur.Len() > 0 {
			sep = "\n\n"
		}
		if cur.Len()+len(sep)+len(p) > limit {
			flush()
			cur.WriteString(p)
		} else {
			cur.WriteString(sep)
			cur.WriteString(p)
		}
	}
	flush()
	if len(out) == 0 {
		return []string{""}
	}
	return out
}
```

- [ ] **Step 4: Run, expect pass**

Run: `cd daemon && go test ./cmd/gobrrr-telegram/chunker/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add daemon/cmd/gobrrr-telegram/chunker/
git commit -m "feat(telegram): message chunker with length + newline modes"
```

---

## Task 4: access types + Load/Save

**Files:**
- Create: `daemon/cmd/gobrrr-telegram/access/access.go`
- Create: `daemon/cmd/gobrrr-telegram/access/access_test.go`

- [ ] **Step 1: Write failing tests**

Create `access/access_test.go`:

```go
package access

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDefaultWhenMissing(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir, false)
	a, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if a.DMPolicy != "pairing" {
		t.Errorf("default dm policy = %q", a.DMPolicy)
	}
	if a.AllowFrom == nil || a.Groups == nil || a.Pending == nil {
		t.Error("default slices/maps must be non-nil")
	}
}

func TestSaveAtomicRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir, false)
	a := Default()
	a.DMPolicy = "allowlist"
	a.AllowFrom = []string{"123"}
	if err := s.Save(a); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "access.json"))
	if err != nil {
		t.Fatal(err)
	}
	var back Access
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	if back.DMPolicy != "allowlist" || len(back.AllowFrom) != 1 {
		t.Errorf("roundtrip: %+v", back)
	}
	// file perm 0600
	st, _ := os.Stat(filepath.Join(dir, "access.json"))
	if st.Mode().Perm() != 0600 {
		t.Errorf("perm = %v", st.Mode().Perm())
	}
}

func TestCorruptRecovery(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "access.json")
	os.WriteFile(p, []byte("{not json"), 0600)
	s := NewStore(dir, false)
	a, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if a.DMPolicy != "pairing" {
		t.Errorf("expected fresh default, got %+v", a)
	}
	// corrupt file renamed aside
	matches, _ := filepath.Glob(filepath.Join(dir, "access.json.corrupt-*"))
	if len(matches) != 1 {
		t.Errorf("expected 1 corrupt-aside file, got %d", len(matches))
	}
}

func TestStaticMode(t *testing.T) {
	dir := t.TempDir()
	// write an access file with pairing policy
	a := Default()
	a.DMPolicy = "pairing"
	a.Pending = map[string]Pending{"abcde": {SenderID: "1"}}
	NewStore(dir, false).Save(a)

	s := NewStore(dir, true) // static
	got, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.DMPolicy != "allowlist" {
		t.Errorf("static: pairing should downgrade to allowlist, got %q", got.DMPolicy)
	}
	if len(got.Pending) != 0 {
		t.Errorf("static: pending must be cleared")
	}
	// Save in static mode must be a no-op (no error, no disk write)
	a2 := Default()
	a2.DMPolicy = "disabled"
	if err := s.Save(a2); err != nil {
		t.Fatal(err)
	}
	reload, _ := NewStore(dir, false).Load()
	if reload.DMPolicy != "pairing" {
		t.Errorf("static save should not persist; got %q", reload.DMPolicy)
	}
}
```

- [ ] **Step 2: Run, expect fail**

Run: `cd daemon && go test ./cmd/gobrrr-telegram/access/...`
Expected: FAIL (package missing).

- [ ] **Step 3: Implement**

Create `access/access.go`:

```go
// Package access manages ~/.claude/channels/telegram/access.json — shared
// with the /telegram:access and /telegram:configure skills.
package access

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

type DMPolicy string

const (
	DMPairing   DMPolicy = "pairing"
	DMAllowlist DMPolicy = "allowlist"
	DMDisabled  DMPolicy = "disabled"
)

type GroupPolicy struct {
	RequireMention bool     `json:"requireMention"`
	AllowFrom      []string `json:"allowFrom"`
}

type Pending struct {
	SenderID  string `json:"senderId"`
	ChatID    string `json:"chatId"`
	CreatedAt int64  `json:"createdAt"`
	ExpiresAt int64  `json:"expiresAt"`
	Replies   int    `json:"replies"`
}

type Access struct {
	DMPolicy        DMPolicy               `json:"dmPolicy"`
	AllowFrom       []string               `json:"allowFrom"`
	Groups          map[string]GroupPolicy `json:"groups"`
	Pending         map[string]Pending     `json:"pending"`
	MentionPatterns []string               `json:"mentionPatterns,omitempty"`
	AckReaction     *string                `json:"ackReaction,omitempty"`
	ReplyToMode     string                 `json:"replyToMode,omitempty"` // off|first|all
	TextChunkLimit  int                    `json:"textChunkLimit,omitempty"`
	ChunkMode       string                 `json:"chunkMode,omitempty"` // length|newline
}

func Default() Access {
	return Access{
		DMPolicy:  DMPairing,
		AllowFrom: []string{},
		Groups:    map[string]GroupPolicy{},
		Pending:   map[string]Pending{},
	}
}

// Store reads and writes access.json atomically. Static=true snapshots at
// first Load and turns Save into a no-op.
type Store struct {
	dir      string
	static   bool
	snapshot *Access
}

func NewStore(dir string, static bool) *Store {
	return &Store{dir: dir, static: static}
}

func (s *Store) path() string { return filepath.Join(s.dir, "access.json") }

func (s *Store) Load() (Access, error) {
	if s.static && s.snapshot != nil {
		return *s.snapshot, nil
	}
	raw, err := os.ReadFile(s.path())
	if errors.Is(err, fs.ErrNotExist) {
		a := Default()
		if s.static {
			s.snapshot = &a
		}
		return a, nil
	}
	if err != nil {
		return Access{}, err
	}
	var a Access
	if err := json.Unmarshal(raw, &a); err != nil {
		// corrupt — move aside, return fresh default (non-static only)
		if !s.static {
			aside := fmt.Sprintf("%s.corrupt-%d", s.path(), time.Now().UnixMilli())
			_ = os.Rename(s.path(), aside)
			fmt.Fprintf(os.Stderr, "gobrrr-telegram: access.json corrupt, moved to %s\n", aside)
		}
		return Default(), nil
	}
	if a.AllowFrom == nil {
		a.AllowFrom = []string{}
	}
	if a.Groups == nil {
		a.Groups = map[string]GroupPolicy{}
	}
	if a.Pending == nil {
		a.Pending = map[string]Pending{}
	}
	if a.DMPolicy == "" {
		a.DMPolicy = DMPairing
	}
	if s.static {
		if a.DMPolicy == DMPairing {
			fmt.Fprintln(os.Stderr, `gobrrr-telegram: static mode — dmPolicy "pairing" downgraded to "allowlist"`)
			a.DMPolicy = DMAllowlist
		}
		a.Pending = map[string]Pending{}
		s.snapshot = &a
	}
	return a, nil
}

func (s *Store) Save(a Access) error {
	if s.static {
		return nil
	}
	if err := os.MkdirAll(s.dir, 0700); err != nil {
		return err
	}
	buf, err := json.MarshalIndent(a, "", "  ")
	if err != nil {
		return err
	}
	buf = append(buf, '\n')
	tmp := s.path() + ".tmp"
	if err := os.WriteFile(tmp, buf, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path())
}
```

- [ ] **Step 4: Run, expect pass**

Run: `cd daemon && go test ./cmd/gobrrr-telegram/access/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add daemon/cmd/gobrrr-telegram/access/
git commit -m "feat(telegram): access.json store with atomic writes and corrupt recovery"
```

---

## Task 5: access pairing helpers

**Files:**
- Create: `daemon/cmd/gobrrr-telegram/access/pairing.go`
- Create: `daemon/cmd/gobrrr-telegram/access/pairing_test.go`

- [ ] **Step 1: Write failing tests**

Create `access/pairing_test.go`:

```go
package access

import (
	"testing"
	"time"
)

func TestNewPairingCode(t *testing.T) {
	code := NewPairingCode()
	if len(code) != 5 {
		t.Fatalf("len=%d", len(code))
	}
	for _, r := range code {
		if r == 'l' || r < 'a' || r > 'z' {
			t.Fatalf("bad char %q in %q", r, code)
		}
	}
}

func TestPruneExpired(t *testing.T) {
	a := Default()
	now := time.Now().UnixMilli()
	a.Pending["aaaaa"] = Pending{ExpiresAt: now - 1000}
	a.Pending["bbbbb"] = Pending{ExpiresAt: now + 60000}
	changed := PruneExpired(&a)
	if !changed {
		t.Error("expected changed=true")
	}
	if _, ok := a.Pending["aaaaa"]; ok {
		t.Error("expired should be gone")
	}
	if _, ok := a.Pending["bbbbb"]; !ok {
		t.Error("live should remain")
	}
}
```

- [ ] **Step 2: Run, expect fail**

Run: `cd daemon && go test ./cmd/gobrrr-telegram/access/...`
Expected: FAIL.

- [ ] **Step 3: Implement**

Create `access/pairing.go`:

```go
package access

import (
	"crypto/rand"
	"time"
)

// alphabet: a-z minus 'l' — matches permission.Match expectations.
const pairAlphabet = "abcdefghijkmnopqrstuvwxyz"

// NewPairingCode returns a 5-char code drawn from pairAlphabet.
func NewPairingCode() string {
	var buf [5]byte
	if _, err := rand.Read(buf[:]); err != nil {
		// crypto/rand should never fail; fall back to time-based seed
		t := time.Now().UnixNano()
		for i := range buf {
			buf[i] = byte(t >> (i * 8))
		}
	}
	out := make([]byte, 5)
	for i, b := range buf {
		out[i] = pairAlphabet[int(b)%len(pairAlphabet)]
	}
	return string(out)
}

// PruneExpired removes expired pending entries. Returns true if anything
// was removed.
func PruneExpired(a *Access) bool {
	now := time.Now().UnixMilli()
	changed := false
	for code, p := range a.Pending {
		if p.ExpiresAt < now {
			delete(a.Pending, code)
			changed = true
		}
	}
	return changed
}
```

- [ ] **Step 4: Run, expect pass**

Run: `cd daemon && go test ./cmd/gobrrr-telegram/access/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add daemon/cmd/gobrrr-telegram/access/pairing.go daemon/cmd/gobrrr-telegram/access/pairing_test.go
git commit -m "feat(telegram): pairing code generator and expiry prune"
```

---

## Task 6: access inbound gate

**Files:**
- Create: `daemon/cmd/gobrrr-telegram/access/gate.go`
- Create: `daemon/cmd/gobrrr-telegram/access/gate_test.go`

- [ ] **Step 1: Write failing tests**

Create `access/gate_test.go`:

```go
package access

import "testing"

func TestGateDM(t *testing.T) {
	a := Default()
	a.DMPolicy = DMDisabled
	if Check(a, "42", "42", false, "", "bot") != Drop {
		t.Error("disabled dm must drop")
	}

	a = Default()
	a.DMPolicy = DMAllowlist
	a.AllowFrom = []string{"42"}
	if Check(a, "42", "42", false, "hi", "bot") != Allow {
		t.Error("allowlist hit must allow")
	}
	if Check(a, "99", "99", false, "hi", "bot") != Drop {
		t.Error("allowlist miss must drop")
	}

	a = Default() // pairing
	if Check(a, "42", "42", false, "hello", "bot") != NeedPair {
		t.Error("pairing DM from unknown must be NeedPair")
	}
	a.AllowFrom = []string{"42"}
	if Check(a, "42", "42", false, "hello", "bot") != Allow {
		t.Error("pairing DM from approved must allow")
	}
}

func TestGateGroup(t *testing.T) {
	a := Default()
	a.Groups["-100"] = GroupPolicy{
		RequireMention: true,
		AllowFrom:      []string{"42"},
	}
	// unknown group → drop
	if Check(a, "-200", "42", true, "hi", "bot") != Drop {
		t.Error("unknown group must drop")
	}
	// known group, allowed user, no mention → drop
	if Check(a, "-100", "42", true, "hi", "bot") != Drop {
		t.Error("require mention enforced")
	}
	// known group, allowed user, mention → allow
	if Check(a, "-100", "42", true, "@bot hi", "bot") != Allow {
		t.Error("mention should allow")
	}
	// disallowed user → drop
	if Check(a, "-100", "99", true, "@bot hi", "bot") != Drop {
		t.Error("user not in group allowFrom must drop")
	}
	// no mention required
	a.Groups["-100"] = GroupPolicy{RequireMention: false, AllowFrom: []string{"42"}}
	if Check(a, "-100", "42", true, "hi", "bot") != Allow {
		t.Error("mention not required → allow")
	}
}
```

- [ ] **Step 2: Run, expect fail**

Run: `cd daemon && go test ./cmd/gobrrr-telegram/access/...`
Expected: FAIL.

- [ ] **Step 3: Implement**

Create `access/gate.go`:

```go
package access

import (
	"slices"
	"strings"
)

type Decision int

const (
	Drop Decision = iota
	Allow
	NeedPair // caller must run pairing logic (issue code or match reply)
)

// Check decides whether an inbound message should be delivered.
// botUsername is used for mention-triggering in groups.
func Check(a Access, chatID, userID string, isGroup bool, text, botUsername string) Decision {
	if isGroup {
		gp, ok := a.Groups[chatID]
		if !ok {
			return Drop
		}
		if !slices.Contains(gp.AllowFrom, userID) {
			return Drop
		}
		if gp.RequireMention {
			if !mentionsBot(text, botUsername, a.MentionPatterns) {
				return Drop
			}
		}
		return Allow
	}
	// DM
	switch a.DMPolicy {
	case DMDisabled:
		return Drop
	case DMAllowlist:
		if slices.Contains(a.AllowFrom, chatID) {
			return Allow
		}
		return Drop
	case DMPairing:
		if slices.Contains(a.AllowFrom, chatID) {
			return Allow
		}
		return NeedPair
	}
	return Drop
}

func mentionsBot(text, bot string, patterns []string) bool {
	lt := strings.ToLower(text)
	if bot != "" && strings.Contains(lt, "@"+strings.ToLower(bot)) {
		return true
	}
	for _, p := range patterns {
		if p != "" && strings.Contains(lt, strings.ToLower(p)) {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: Run, expect pass**

Run: `cd daemon && go test ./cmd/gobrrr-telegram/access/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add daemon/cmd/gobrrr-telegram/access/gate.go daemon/cmd/gobrrr-telegram/access/gate_test.go
git commit -m "feat(telegram): inbound gate with DM and group policy checks"
```

---

## Task 7: assertSendable guard

**Files:**
- Create: `daemon/cmd/gobrrr-telegram/mcpserver/sendable.go`
- Create: `daemon/cmd/gobrrr-telegram/mcpserver/sendable_test.go`

- [ ] **Step 1: Write failing tests**

Create `mcpserver/sendable_test.go`:

```go
package mcpserver

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAssertSendable(t *testing.T) {
	state := t.TempDir()
	os.MkdirAll(filepath.Join(state, "inbox"), 0700)

	// state dir file (access.json) → refuse
	accessFile := filepath.Join(state, "access.json")
	os.WriteFile(accessFile, []byte("{}"), 0600)
	if err := AssertSendable(accessFile, state); err == nil {
		t.Error("state-dir file must be refused")
	}
	// inbox file → allow
	inboxFile := filepath.Join(state, "inbox", "x.png")
	os.WriteFile(inboxFile, []byte("png"), 0600)
	if err := AssertSendable(inboxFile, state); err != nil {
		t.Errorf("inbox must be allowed: %v", err)
	}
	// outside state dir → allow
	outsideDir := t.TempDir()
	outsideFile := filepath.Join(outsideDir, "y.png")
	os.WriteFile(outsideFile, []byte("png"), 0600)
	if err := AssertSendable(outsideFile, state); err != nil {
		t.Errorf("outside must be allowed: %v", err)
	}
	// nonexistent file → permissive (assertSendable is best-effort; caller
	// will get a proper error when it opens the file)
	if err := AssertSendable(filepath.Join(state, "nope.txt"), state); err != nil {
		t.Errorf("nonexistent permissive: %v", err)
	}
}
```

- [ ] **Step 2: Run, expect fail**

Run: `cd daemon && go test ./cmd/gobrrr-telegram/mcpserver/...`
Expected: FAIL.

- [ ] **Step 3: Implement**

Create `mcpserver/sendable.go`:

```go
// Package mcpserver wires the MCP stdio server for the Telegram channel.
package mcpserver

import (
	"fmt"
	"path/filepath"
	"strings"
)

// AssertSendable refuses to send files that live inside STATE_DIR except
// for STATE_DIR/inbox/. Best-effort: unresolved paths are permitted (the
// actual open/send will surface real errors).
func AssertSendable(path, stateDir string) error {
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return nil
	}
	stateReal, err := filepath.EvalSymlinks(stateDir)
	if err != nil {
		return nil
	}
	inbox := filepath.Join(stateReal, "inbox")
	sep := string(filepath.Separator)
	if strings.HasPrefix(real, stateReal+sep) && !strings.HasPrefix(real, inbox+sep) {
		return fmt.Errorf("refusing to send channel state: %s", path)
	}
	return nil
}
```

- [ ] **Step 4: Run, expect pass**

Run: `cd daemon && go test ./cmd/gobrrr-telegram/mcpserver/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add daemon/cmd/gobrrr-telegram/mcpserver/
git commit -m "feat(telegram): assertSendable guard for state-dir exfil protection"
```

---

## Task 8: bot package — wrapper, long-poll, outbound

**Files:**
- Create: `daemon/cmd/gobrrr-telegram/bot/bot.go`
- Create: `daemon/cmd/gobrrr-telegram/bot/outbound.go`
- Create: `daemon/cmd/gobrrr-telegram/bot/download.go`

**Note:** `go-telegram/bot` API reference: the `Bot` is constructed with `bot.New(token, opts...)`, started with `b.Start(ctx)`. Messages sent via `b.SendMessage(ctx, &bot.SendMessageParams{ChatID: id, Text: ...})`. File downloads via `b.GetFile` + `b.FileDownloadLink`. If any of these names are wrong after you import the package, grep the installed module for the correct ones — the shapes are stable but method names may vary slightly.

- [ ] **Step 1: Create `bot/bot.go` (wrapper + long-poll)**

```go
// Package bot wraps go-telegram/bot to integrate with the Telegram channel's
// access gate and MCP notification emitter.
package bot

import (
	"context"
	"fmt"
	"os"
	"strconv"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/racterub/gobrrr/daemon/cmd/gobrrr-telegram/access"
)

// InboundHandler receives a gated, already-approved message. Implementations
// emit it as an MCP channel notification.
type InboundHandler func(ctx context.Context, u *models.Update, attachPath, attachFileID string)

// Bot is the Telegram client used by both the long-poll loop (inbound) and
// the MCP tool handlers (outbound).
type Bot struct {
	b         *tgbot.Bot
	username  string
	store     *access.Store
	stateDir  string
	onInbound InboundHandler
}

func New(token, stateDir string, store *access.Store, onInbound InboundHandler) (*Bot, error) {
	wrapped := &Bot{store: store, stateDir: stateDir, onInbound: onInbound}
	opts := []tgbot.Option{
		tgbot.WithDefaultHandler(wrapped.handleUpdate),
	}
	inner, err := tgbot.New(token, opts...)
	if err != nil {
		return nil, err
	}
	wrapped.b = inner
	return wrapped, nil
}

func (w *Bot) Username() string { return w.username }

// Start fetches getMe and runs the long-poll loop until ctx is cancelled.
func (w *Bot) Start(ctx context.Context) error {
	me, err := w.b.GetMe(ctx)
	if err != nil {
		return fmt.Errorf("getMe: %w", err)
	}
	w.username = me.Username
	fmt.Fprintf(os.Stderr, "gobrrr-telegram: connected as @%s\n", w.username)
	w.b.Start(ctx) // blocks
	return nil
}

// Inner returns the raw go-telegram/bot instance for outbound tool handlers.
func (w *Bot) Inner() *tgbot.Bot { return w.b }

// ChatIDToString converts any Telegram chat ID (which may arrive as int64)
// to the canonical string form used by access.json.
func ChatIDToString(id int64) string { return strconv.FormatInt(id, 10) }
```

- [ ] **Step 2: Create `bot/inbound.go` (update handler + gate + pairing flow)**

```go
package bot

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/go-telegram/bot/models"

	"github.com/racterub/gobrrr/daemon/cmd/gobrrr-telegram/access"
	"github.com/racterub/gobrrr/daemon/cmd/gobrrr-telegram/permission"
)

const pairingTTL = 10 * time.Minute

func (w *Bot) handleUpdate(ctx context.Context, inner interface{}, upd *models.Update) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "gobrrr-telegram: panic in handleUpdate: %v\n", r)
		}
	}()
	if upd.Message == nil {
		return
	}
	msg := upd.Message
	chatID := ChatIDToString(msg.Chat.ID)
	userID := ChatIDToString(msg.From.ID)
	isGroup := msg.Chat.Type == models.ChatTypeGroup || msg.Chat.Type == models.ChatTypeSupergroup

	a, err := w.store.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "gobrrr-telegram: load access: %v\n", err)
		return
	}

	decision := access.Check(a, chatID, userID, isGroup, msg.Text, w.username)

	switch decision {
	case access.Drop:
		return
	case access.NeedPair:
		w.handlePairing(ctx, &a, chatID, userID, msg)
		return
	case access.Allow:
		// fall through to delivery
	}

	attachPath, attachFileID, _ := w.maybeDownload(ctx, msg)
	if w.onInbound != nil {
		w.onInbound(ctx, upd, attachPath, attachFileID)
	}
	// ackReaction
	if a.AckReaction != nil && *a.AckReaction != "" {
		_, _ = w.Inner().SetMessageReaction(ctx, &reactParams{
			chatID:    msg.Chat.ID,
			messageID: msg.ID,
			emoji:     *a.AckReaction,
		}.to())
	}
}

func (w *Bot) handlePairing(ctx context.Context, a *access.Access, chatID, userID string, msg *models.Message) {
	// Reply matches existing pending code? Approve.
	if ok, yes, code := permission.Match(msg.Text); ok {
		if p, found := a.Pending[code]; found && p.SenderID == userID {
			delete(a.Pending, code)
			if yes {
				a.AllowFrom = append(a.AllowFrom, chatID)
				_ = w.store.Save(*a)
				w.sendText(ctx, msg.Chat.ID, 0, "paired ✓")
			} else {
				_ = w.store.Save(*a)
				w.sendText(ctx, msg.Chat.ID, 0, "pairing declined")
			}
			return
		}
	}
	// Issue new code.
	access.PruneExpired(a)
	code := access.NewPairingCode()
	now := time.Now()
	a.Pending[code] = access.Pending{
		SenderID:  userID,
		ChatID:    chatID,
		CreatedAt: now.UnixMilli(),
		ExpiresAt: now.Add(pairingTTL).UnixMilli(),
	}
	if err := w.store.Save(*a); err != nil {
		fmt.Fprintf(os.Stderr, "gobrrr-telegram: save pairing: %v\n", err)
		return
	}
	w.sendText(ctx, msg.Chat.ID,
		msg.ID,
		fmt.Sprintf("pairing code: %s\nask the operator to reply `y %s` in this chat to approve, or `n %s` to decline. expires in %s.",
			code, code, code, pairingTTL))
}

// reactParams is a tiny shim so the call site reads nicely; we build the
// real go-telegram/bot request in .to().
type reactParams struct {
	chatID    int64
	messageID int
	emoji     string
}

// to is defined in outbound.go (shares the go-telegram/bot types).
var _ = strconv.Itoa // keep import
```

- [ ] **Step 3: Create `bot/outbound.go` (reply, edit, react + reactParams.to)**

```go
package bot

import (
	"context"
	"fmt"
	"os"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

// SendTextResult is one chunk's outcome.
type SendTextResult struct {
	MessageID int
	Err       error
}

// SendText sends a single text message. Caller handles chunking.
// replyTo=0 means no threading.
func (w *Bot) SendText(ctx context.Context, chatID int64, replyTo int, text string) (int, error) {
	params := &tgbot.SendMessageParams{
		ChatID: chatID,
		Text:   text,
	}
	if replyTo != 0 {
		params.ReplyParameters = &models.ReplyParameters{MessageID: replyTo}
	}
	m, err := w.Inner().SendMessage(ctx, params)
	if err != nil {
		return 0, err
	}
	return m.ID, nil
}

// sendText is an internal convenience that logs rather than returning errors.
func (w *Bot) sendText(ctx context.Context, chatID int64, replyTo int, text string) {
	if _, err := w.SendText(ctx, chatID, replyTo, text); err != nil {
		fmt.Fprintf(os.Stderr, "gobrrr-telegram: sendText: %v\n", err)
	}
}

// EditText edits a previously-sent message.
func (w *Bot) EditText(ctx context.Context, chatID int64, messageID int, text string) error {
	_, err := w.Inner().EditMessageText(ctx, &tgbot.EditMessageTextParams{
		ChatID:    chatID,
		MessageID: messageID,
		Text:      text,
	})
	return err
}

// React sets a single emoji reaction (empty string clears).
func (w *Bot) React(ctx context.Context, chatID int64, messageID int, emoji string) error {
	var reactions []models.ReactionType
	if emoji != "" {
		reactions = []models.ReactionType{{
			Type:           models.ReactionTypeTypeEmoji,
			ReactionTypeEmoji: &models.ReactionTypeEmoji{Type: "emoji", Emoji: emoji},
		}}
	}
	_, err := w.Inner().SetMessageReaction(ctx, &tgbot.SetMessageReactionParams{
		ChatID:    chatID,
		MessageID: messageID,
		Reaction:  reactions,
	})
	return err
}

func (r reactParams) to() *tgbot.SetMessageReactionParams {
	return &tgbot.SetMessageReactionParams{
		ChatID:    r.chatID,
		MessageID: r.messageID,
		Reaction: []models.ReactionType{{
			Type:              models.ReactionTypeTypeEmoji,
			ReactionTypeEmoji: &models.ReactionTypeEmoji{Type: "emoji", Emoji: r.emoji},
		}},
	}
}
```

**Note:** The exact struct field names for `ReactionType` in `go-telegram/bot/models` may differ (e.g. `ReactionTypeEmoji` may be embedded differently). If compile fails, `go doc github.com/go-telegram/bot/models.ReactionType` and adjust field names. Do not change behavior — just match the library's shape.

- [ ] **Step 4: Create `bot/download.go` (attachment fetch)**

```go
package bot

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/go-telegram/bot/models"
)

// maybeDownload inspects the message for attachments and, if found, fetches
// the file into <stateDir>/inbox/. Returns (localPath, fileID, err). For
// photos localPath is set and fileID is also returned; for documents only
// the fileID+localPath. If nothing was attached, returns empty strings.
func (w *Bot) maybeDownload(ctx context.Context, msg *models.Message) (string, string, error) {
	var fileID, name string
	switch {
	case len(msg.Photo) > 0:
		// largest size is last
		fileID = msg.Photo[len(msg.Photo)-1].FileID
		name = fmt.Sprintf("photo-%d.jpg", msg.ID)
	case msg.Document != nil:
		fileID = msg.Document.FileID
		name = msg.Document.FileName
		if name == "" {
			name = fmt.Sprintf("doc-%d.bin", msg.ID)
		}
	default:
		return "", "", nil
	}

	inbox := filepath.Join(w.stateDir, "inbox")
	if err := os.MkdirAll(inbox, 0700); err != nil {
		return "", fileID, err
	}
	out := filepath.Join(inbox, fmt.Sprintf("%d-%d-%s", msg.Chat.ID, msg.ID, name))

	f, err := w.Inner().GetFile(ctx, &tgbotGetFile{FileID: fileID})
	if err != nil {
		return "", fileID, err
	}
	url := w.Inner().FileDownloadLink(f)
	resp, err := http.Get(url)
	if err != nil {
		return "", fileID, err
	}
	defer resp.Body.Close()
	fp, err := os.OpenFile(out, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return "", fileID, err
	}
	defer fp.Close()
	if _, err := io.Copy(fp, resp.Body); err != nil {
		return "", fileID, err
	}
	return out, fileID, nil
}

// tgbotGetFile is a type alias placeholder — see note in main task.
type tgbotGetFile = struct{ FileID string }
```

**Note:** The `tgbotGetFile` alias above is a placeholder to keep this file compiling without the real import. When implementing, replace with the actual `tgbot.GetFileParams` (or whatever the library exports) and drop the alias.

- [ ] **Step 5: Build, don't run yet (no tests in this package)**

Run: `cd daemon && CGO_ENABLED=0 go build ./cmd/gobrrr-telegram/...`
Expected: builds cleanly. If the go-telegram/bot model field names differ from the placeholders above, fix them per the library's actual shape (`go doc` it) without changing behavior.

- [ ] **Step 6: Commit**

```bash
git add daemon/cmd/gobrrr-telegram/bot/
git commit -m "feat(telegram): bot wrapper with inbound gate, pairing flow, and outbound ops"
```

---

## Task 9: MCP server + tools

**Files:**
- Create: `daemon/cmd/gobrrr-telegram/mcpserver/server.go`
- Create: `daemon/cmd/gobrrr-telegram/mcpserver/notify.go`
- Create: `daemon/cmd/gobrrr-telegram/mcpserver/tools_reply.go`
- Create: `daemon/cmd/gobrrr-telegram/mcpserver/tools_edit.go`
- Create: `daemon/cmd/gobrrr-telegram/mcpserver/tools_react.go`
- Create: `daemon/cmd/gobrrr-telegram/mcpserver/tools_download.go`

**Note:** `mark3labs/mcp-go` API: `mcp.NewServer(name, version, opts...)`, `server.AddTool(tool, handler)`, `server.ServeStdio(...)`. Notifications sent via `server.SendNotificationToAllClients(method, params)`. If names differ slightly after import, adjust — the shapes are stable.

- [ ] **Step 1: Create `server.go`**

```go
package mcpserver

import (
	"context"
	"strconv"

	mcp "github.com/mark3labs/mcp-go/mcp"
	server "github.com/mark3labs/mcp-go/server"

	"github.com/racterub/gobrrr/daemon/cmd/gobrrr-telegram/access"
	"github.com/racterub/gobrrr/daemon/cmd/gobrrr-telegram/bot"
)

type Server struct {
	s        *server.MCPServer
	b        *bot.Bot
	store    *access.Store
	stateDir string
}

func New(b *bot.Bot, store *access.Store, stateDir string) *Server {
	s := server.NewMCPServer(
		"telegram",
		"0.0.1",
		server.WithInstructions(
			`The sender reads Telegram, not this session. Anything you want them to see must go through the reply tool. Messages arrive as <channel source="telegram" chat_id="..." message_id="..." user="..." ts="...">.`),
	)
	srv := &Server{s: s, b: b, store: store, stateDir: stateDir}
	srv.registerTools()
	return srv
}

func (s *Server) registerTools() {
	s.s.AddTool(replyTool(), s.handleReply)
	s.s.AddTool(editTool(), s.handleEdit)
	s.s.AddTool(reactTool(), s.handleReact)
	s.s.AddTool(downloadTool(), s.handleDownload)
}

// ServeStdio blocks serving the MCP protocol on stdin/stdout.
func (s *Server) ServeStdio(ctx context.Context) error {
	return server.ServeStdio(s.s)
}

// assertAllowedChatID parses the chat_id string and asserts it is in the
// current access allowlist (either a DM allowFrom entry or a known group).
func (s *Server) assertAllowedChatID(chatID string) (int64, error) {
	a, err := s.store.Load()
	if err != nil {
		return 0, err
	}
	for _, id := range a.AllowFrom {
		if id == chatID {
			n, _ := strconv.ParseInt(chatID, 10, 64)
			return n, nil
		}
	}
	if _, ok := a.Groups[chatID]; ok {
		n, _ := strconv.ParseInt(chatID, 10, 64)
		return n, nil
	}
	return 0, &notAllowedError{chatID: chatID}
}

type notAllowedError struct{ chatID string }

func (e *notAllowedError) Error() string {
	return "chat " + e.chatID + " is not allowlisted — add via /telegram:access"
}

// Ensure import balance
var _ = mcp.NewToolResultText
```

- [ ] **Step 2: Create `notify.go`**

```go
package mcpserver

import (
	"context"
	"fmt"
	"html"
	"time"

	"github.com/go-telegram/bot/models"
)

// EmitInbound builds the <channel> tag and sends a notifications/claude/channel
// to the connected MCP client.
func (s *Server) EmitInbound(ctx context.Context, upd *models.Update, attachPath, attachFileID string) {
	msg := upd.Message
	if msg == nil {
		return
	}
	ts := time.Unix(int64(msg.Date), 0).UTC().Format(time.RFC3339)
	user := msg.From.Username
	if user == "" {
		user = msg.From.FirstName
	}
	attrs := fmt.Sprintf(
		`source="telegram" chat_id="%d" message_id="%d" user=%q ts=%q`,
		msg.Chat.ID, msg.ID, user, ts,
	)
	if attachPath != "" && msg.Photo != nil {
		attrs += fmt.Sprintf(` image_path=%q`, attachPath)
	} else if attachFileID != "" {
		attrs += fmt.Sprintf(` attachment_file_id=%q`, attachFileID)
	}
	body := msg.Text
	if body == "" && msg.Caption != "" {
		body = msg.Caption
	}
	content := fmt.Sprintf("<channel %s>\n%s\n</channel>", attrs, html.EscapeString(body))

	params := map[string]any{
		"content": content,
		"meta": map[string]any{
			"chat_id":    fmt.Sprintf("%d", msg.Chat.ID),
			"message_id": msg.ID,
			"user":       user,
			"ts":         ts,
		},
	}
	if err := s.s.SendNotificationToAllClients("notifications/claude/channel", params); err != nil {
		fmt.Printf("emit inbound: %v\n", err)
	}
}
```

- [ ] **Step 3: Create `tools_reply.go`**

```go
package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"

	mcp "github.com/mark3labs/mcp-go/mcp"

	"github.com/racterub/gobrrr/daemon/cmd/gobrrr-telegram/chunker"
)

func replyTool() mcp.Tool {
	return mcp.NewTool("reply",
		mcp.WithDescription("Send a text reply to a Telegram chat. Optionally attach files or thread to a specific message."),
		mcp.WithString("chat_id", mcp.Required(), mcp.Description("Telegram chat_id as string")),
		mcp.WithString("text", mcp.Required()),
		mcp.WithNumber("reply_to", mcp.Description("message_id to reply to (optional)")),
		mcp.WithArray("files", mcp.Description("absolute file paths to attach (optional)")),
	)
}

func (s *Server) handleReply(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	chatIDStr, _ := req.Params.Arguments["chat_id"].(string)
	text, _ := req.Params.Arguments["text"].(string)
	replyToF, _ := req.Params.Arguments["reply_to"].(float64)
	filesRaw, _ := req.Params.Arguments["files"].([]any)

	chatID, err := s.assertAllowedChatID(chatIDStr)
	if err != nil {
		return nil, err
	}
	for _, f := range filesRaw {
		if p, ok := f.(string); ok {
			if err := AssertSendable(p, s.stateDir); err != nil {
				return nil, err
			}
		}
	}
	a, _ := s.store.Load()
	mode := chunker.Mode(a.ChunkMode)
	if mode == "" {
		mode = chunker.ModeLength
	}
	limit := a.TextChunkLimit
	if limit == 0 {
		limit = chunker.HardCap
	}
	chunks := chunker.Split(text, mode, limit)

	replyMode := a.ReplyToMode
	if replyMode == "" {
		replyMode = "first"
	}
	var ids []int
	for i, c := range chunks {
		var rt int
		switch replyMode {
		case "all":
			rt = int(replyToF)
		case "first":
			if i == 0 {
				rt = int(replyToF)
			}
		}
		id, err := s.b.SendText(ctx, chatID, rt, c)
		if err != nil {
			return nil, fmt.Errorf("send chunk %d: %w", i, err)
		}
		ids = append(ids, id)
	}
	// Files sent after text. Document-only for now (photos also go as document;
	// Telegram clients render image documents fine).
	for _, f := range filesRaw {
		p, ok := f.(string)
		if !ok {
			continue
		}
		id, err := s.b.SendDocument(ctx, chatID, p)
		if err != nil {
			return nil, fmt.Errorf("send file %s: %w", p, err)
		}
		ids = append(ids, id)
	}
	j, _ := json.Marshal(map[string]any{"message_ids": ids})
	return mcp.NewToolResultText(string(j)), nil
}
```

**Note:** `SendDocument` isn't yet in `bot/outbound.go` — add it in this task. Minimal impl:

```go
// In bot/outbound.go:
func (w *Bot) SendDocument(ctx context.Context, chatID int64, path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	m, err := w.Inner().SendDocument(ctx, &tgbot.SendDocumentParams{
		ChatID:   chatID,
		Document: &models.InputFileUpload{Filename: filepath.Base(path), Data: f},
	})
	if err != nil {
		return 0, err
	}
	return m.ID, nil
}
```

Add the `os`, `path/filepath` imports to `outbound.go`.

- [ ] **Step 4: Create `tools_edit.go`**

```go
package mcpserver

import (
	"context"

	mcp "github.com/mark3labs/mcp-go/mcp"
)

func editTool() mcp.Tool {
	return mcp.NewTool("edit_message",
		mcp.WithDescription("Edit a previously sent Telegram message."),
		mcp.WithString("chat_id", mcp.Required()),
		mcp.WithNumber("message_id", mcp.Required()),
		mcp.WithString("text", mcp.Required()),
	)
}

func (s *Server) handleEdit(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	chatIDStr, _ := req.Params.Arguments["chat_id"].(string)
	midF, _ := req.Params.Arguments["message_id"].(float64)
	text, _ := req.Params.Arguments["text"].(string)
	chatID, err := s.assertAllowedChatID(chatIDStr)
	if err != nil {
		return nil, err
	}
	if err := s.b.EditText(ctx, chatID, int(midF), text); err != nil {
		return nil, err
	}
	return mcp.NewToolResultText("ok"), nil
}
```

- [ ] **Step 5: Create `tools_react.go`**

```go
package mcpserver

import (
	"context"

	mcp "github.com/mark3labs/mcp-go/mcp"
)

func reactTool() mcp.Tool {
	return mcp.NewTool("react",
		mcp.WithDescription("Set an emoji reaction on a message (empty string clears)."),
		mcp.WithString("chat_id", mcp.Required()),
		mcp.WithNumber("message_id", mcp.Required()),
		mcp.WithString("emoji", mcp.Required()),
	)
}

func (s *Server) handleReact(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	chatIDStr, _ := req.Params.Arguments["chat_id"].(string)
	midF, _ := req.Params.Arguments["message_id"].(float64)
	emoji, _ := req.Params.Arguments["emoji"].(string)
	chatID, err := s.assertAllowedChatID(chatIDStr)
	if err != nil {
		return nil, err
	}
	if err := s.b.React(ctx, chatID, int(midF), emoji); err != nil {
		return nil, err
	}
	return mcp.NewToolResultText("ok"), nil
}
```

- [ ] **Step 6: Create `tools_download.go`**

```go
package mcpserver

import (
	"context"

	mcp "github.com/mark3labs/mcp-go/mcp"
)

func downloadTool() mcp.Tool {
	return mcp.NewTool("download_attachment",
		mcp.WithDescription("Download a Telegram file by file_id into the channel inbox and return the local path."),
		mcp.WithString("file_id", mcp.Required()),
	)
}

func (s *Server) handleDownload(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	fileID, _ := req.Params.Arguments["file_id"].(string)
	path, err := s.b.DownloadByFileID(ctx, fileID)
	if err != nil {
		return nil, err
	}
	return mcp.NewToolResultText(path), nil
}
```

**Note:** Add `DownloadByFileID` to `bot/download.go`:

```go
// DownloadByFileID fetches a Telegram file by its file_id into inbox/.
func (w *Bot) DownloadByFileID(ctx context.Context, fileID string) (string, error) {
	f, err := w.Inner().GetFile(ctx, &tgbot.GetFileParams{FileID: fileID})
	if err != nil {
		return "", err
	}
	url := w.Inner().FileDownloadLink(f)
	inbox := filepath.Join(w.stateDir, "inbox")
	if err := os.MkdirAll(inbox, 0700); err != nil {
		return "", err
	}
	out := filepath.Join(inbox, "download-"+fileID)
	resp, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	fp, err := os.OpenFile(out, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return "", err
	}
	defer fp.Close()
	if _, err := io.Copy(fp, resp.Body); err != nil {
		return "", err
	}
	return out, nil
}
```

And replace the `tgbotGetFile` alias in `download.go` with the real `tgbot.GetFileParams`.

- [ ] **Step 7: Build**

Run: `cd daemon && CGO_ENABLED=0 go build ./cmd/gobrrr-telegram/...`
Expected: builds cleanly. Fix any library name mismatches.

- [ ] **Step 8: Commit**

```bash
git add daemon/cmd/gobrrr-telegram/mcpserver/ daemon/cmd/gobrrr-telegram/bot/
git commit -m "feat(telegram): mcp stdio server with reply/edit/react/download tools"
```

---

## Task 10: main.go wiring + .env loader

**Files:**
- Modify: `daemon/cmd/gobrrr-telegram/main.go`

- [ ] **Step 1: Replace main stub**

```go
package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/racterub/gobrrr/daemon/cmd/gobrrr-telegram/access"
	"github.com/racterub/gobrrr/daemon/cmd/gobrrr-telegram/bot"
	"github.com/racterub/gobrrr/daemon/cmd/gobrrr-telegram/mcpserver"
)

func main() {
	stateDir := os.Getenv("TELEGRAM_STATE_DIR")
	if stateDir == "" {
		home, _ := os.UserHomeDir()
		stateDir = filepath.Join(home, ".claude", "channels", "telegram")
	}
	if err := os.MkdirAll(stateDir, 0700); err != nil {
		fail("mkdir state dir: %v", err)
	}
	loadDotEnv(filepath.Join(stateDir, ".env"))

	token := os.Getenv("TELEGRAM_BOT_TOKEN")
	if token == "" {
		fmt.Fprintf(os.Stderr,
			"gobrrr-telegram: TELEGRAM_BOT_TOKEN required\n"+
				"  set in %s/.env\n"+
				"  format: TELEGRAM_BOT_TOKEN=123456789:AAH...\n",
			stateDir)
		os.Exit(1)
	}
	static := os.Getenv("TELEGRAM_ACCESS_MODE") == "static"

	store := access.NewStore(stateDir, static)
	if _, err := store.Load(); err != nil {
		fail("load access: %v", err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	var mcpSrv *mcpserver.Server
	b, err := bot.New(token, stateDir, store, func(ctx context.Context, upd interface{}, attachPath, attachFileID string) {
		// late binding: mcpSrv is set below
		if mcpSrv != nil {
			mcpSrv.EmitInbound(ctx, castUpdate(upd), attachPath, attachFileID)
		}
	})
	if err != nil {
		fail("bot init: %v", err)
	}
	mcpSrv = mcpserver.New(b, store, stateDir)

	// Bot long-poll in a goroutine; MCP stdio server blocks main.
	go func() {
		defer recoverAndLog("bot loop")
		if err := b.Start(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "gobrrr-telegram: bot stopped: %v\n", err)
		}
	}()

	if err := mcpSrv.ServeStdio(ctx); err != nil {
		fail("mcp stdio: %v", err)
	}
}

func loadDotEnv(path string) {
	_ = os.Chmod(path, 0600)
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if i := strings.Index(line, "="); i > 0 {
			key := strings.TrimSpace(line[:i])
			val := strings.TrimSpace(line[i+1:])
			if os.Getenv(key) == "" {
				_ = os.Setenv(key, val)
			}
		}
	}
}

func recoverAndLog(where string) {
	if r := recover(); r != nil {
		fmt.Fprintf(os.Stderr, "gobrrr-telegram: panic in %s: %v\n", where, r)
	}
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "gobrrr-telegram: "+format+"\n", args...)
	os.Exit(1)
}

// castUpdate goes from interface{} to *models.Update without leaking the
// type into main's imports. Defined in main_cast.go below.
```

- [ ] **Step 2: Create `main_cast.go`**

This keeps main.go free of the models import churn. Create `daemon/cmd/gobrrr-telegram/main_cast.go`:

```go
package main

import "github.com/go-telegram/bot/models"

func castUpdate(u interface{}) *models.Update {
	if up, ok := u.(*models.Update); ok {
		return up
	}
	return nil
}
```

- [ ] **Step 3: Update `bot.InboundHandler` signature if needed**

The `InboundHandler` type in `bot/bot.go` should use `*models.Update`. If it already does (from Task 8), leave alone. If main's closure type doesn't match, adjust main to use `*models.Update` directly.

**Simpler:** change main's closure to accept `*models.Update` and drop `main_cast.go`. Use whichever compiles cleanest.

- [ ] **Step 4: Build**

Run: `cd daemon && CGO_ENABLED=0 go build -o /tmp/gobrrr-telegram ./cmd/gobrrr-telegram/`
Expected: builds cleanly.

- [ ] **Step 5: Commit**

```bash
git add daemon/cmd/gobrrr-telegram/main.go daemon/cmd/gobrrr-telegram/main_cast.go
git commit -m "feat(telegram): main wiring with .env loader and signal handling"
```

---

## Task 11: Integration test — httptest Bot API

**Files:**
- Create: `daemon/cmd/gobrrr-telegram/bot/bot_integration_test.go`

- [ ] **Step 1: Write the test**

This test stands up an httptest server that answers the minimum Bot API endpoints needed (`/bot<token>/getMe`, `/bot<token>/sendMessage`), points `go-telegram/bot` at it via the `WithServerURL` option (check the actual option name in the library), and drives `SendText` asserting the outbound request body.

```go
package bot

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	tgbot "github.com/go-telegram/bot"
)

func TestSendTextHTTPTest(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/getMe"):
			io.WriteString(w, `{"ok":true,"result":{"id":1,"is_bot":true,"first_name":"t","username":"testbot"}}`)
		case strings.HasSuffix(r.URL.Path, "/sendMessage"):
			b, _ := io.ReadAll(r.Body)
			gotBody = string(b)
			io.WriteString(w, `{"ok":true,"result":{"message_id":42,"date":1,"chat":{"id":99,"type":"private"}}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	b, err := tgbot.New("TEST:TOKEN", tgbot.WithServerURL(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	// getMe just to confirm wiring
	if _, err := b.GetMe(nil); err != nil {
		t.Fatal(err)
	}
	m, err := b.SendMessage(nil, &tgbot.SendMessageParams{ChatID: int64(99), Text: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if m.ID != 42 {
		t.Errorf("want ID 42, got %d", m.ID)
	}
	var body map[string]any
	json.Unmarshal([]byte(gotBody), &body)
	if body["text"] != "hello" {
		t.Errorf("want text=hello, got %+v", body)
	}
}
```

**Note:** The option name `WithServerURL` may be different (e.g. `WithAPIURL`). If compile fails, `go doc github.com/go-telegram/bot` and find the correct option. Passing `nil` for ctx is fine for tests but if the library requires non-nil, use `context.Background()`.

- [ ] **Step 2: Run**

Run: `cd daemon && go test ./cmd/gobrrr-telegram/bot/... -run TestSendTextHTTPTest`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add daemon/cmd/gobrrr-telegram/bot/bot_integration_test.go
git commit -m "test(telegram): httptest integration for sendMessage"
```

---

## Task 12: Plugin manifest + install script

**Files:**
- Create: `plugins/gobrrr-telegram/plugin.json`
- Create: `plugins/gobrrr-telegram/README.md`
- Create: `scripts/install-telegram-channel.sh`

- [ ] **Step 1: Inspect the official plugin's manifest for format**

Run: `cat ~/.claude/plugins/cache/claude-plugins-official/telegram/0.0.4/package.json`

Use its shape to model `plugin.json`. The channel entrypoint in the official plugin execs `bun server.ts`. Ours will exec `gobrrr-telegram` from `$PATH` or an absolute path.

- [ ] **Step 2: Create manifest**

Create `plugins/gobrrr-telegram/plugin.json` mirroring the official plugin but pointing at the Go binary:

```json
{
  "name": "gobrrr-telegram",
  "version": "0.0.1",
  "description": "Go drop-in replacement for the official telegram channel plugin",
  "channels": {
    "telegram": {
      "command": "gobrrr-telegram",
      "args": []
    }
  },
  "skills": []
}
```

**Note:** Exact schema depends on Claude Code's plugin format. Check `~/.claude/plugins/cache/claude-plugins-official/telegram/0.0.4/package.json` for the real keys and adapt. Do NOT guess — copy the shape, only change values.

- [ ] **Step 3: Create README**

Create `plugins/gobrrr-telegram/README.md`:

```markdown
# gobrrr-telegram

Drop-in Go reimplementation of the official `telegram` Claude Code plugin.

## Install

```bash
./scripts/install-telegram-channel.sh
```

Then disable the official `telegram` plugin and enable `gobrrr-telegram` in
Claude Code's plugin settings.

## State

Uses the same `~/.claude/channels/telegram/` directory as the official plugin.
The `/telegram:access` and `/telegram:configure` skills continue to work
unchanged.
```

- [ ] **Step 4: Create install script**

Create `scripts/install-telegram-channel.sh`:

```bash
#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BIN_DIR="${GOBRRR_HOME:-$HOME/.gobrrr}/bin"
mkdir -p "$BIN_DIR"

cd "$REPO_ROOT/daemon"
CGO_ENABLED=0 go build -o "$BIN_DIR/gobrrr-telegram" ./cmd/gobrrr-telegram/

echo "installed: $BIN_DIR/gobrrr-telegram"
echo
echo "Next steps:"
echo "  1. Ensure $BIN_DIR is on your PATH, or edit plugins/gobrrr-telegram/plugin.json"
echo "     to use the absolute path $BIN_DIR/gobrrr-telegram."
echo "  2. In Claude Code, disable the official 'telegram' plugin."
echo "  3. Enable 'gobrrr-telegram' from your plugin marketplace."
```

Make executable: `chmod +x scripts/install-telegram-channel.sh`

- [ ] **Step 5: Run install script**

Run: `./scripts/install-telegram-channel.sh`
Expected: binary built to `~/.gobrrr/bin/gobrrr-telegram`.

- [ ] **Step 6: Commit**

```bash
git add plugins/gobrrr-telegram/ scripts/install-telegram-channel.sh
git commit -m "feat(telegram): plugin manifest and install script"
```

---

## Task 13: Smoke test doc

**Files:**
- Create: `docs/superpowers/specs/2026-04-06-gobrrr-telegram-smoke-test.md`

- [ ] **Step 1: Write the checklist**

```markdown
# gobrrr-telegram manual smoke test

Run after every non-trivial change. Requires a real Telegram bot token.

## Setup
- [ ] `~/.claude/channels/telegram/.env` has `TELEGRAM_BOT_TOKEN=...`
- [ ] `~/.gobrrr/bin/gobrrr-telegram` exists and is executable
- [ ] `gobrrr-telegram` plugin enabled in Claude Code, official `telegram` disabled

## Pairing
- [ ] DM the bot from an unknown account → receive pairing code
- [ ] Reply `y <code>` → bot says "paired ✓"
- [ ] Subsequent DMs forward to Claude as `<channel>` tags

## Delivery
- [ ] Claude sends a short message via `reply` → arrives
- [ ] Claude sends a 5000-char message → chunked into multiple messages
- [ ] Set `replyToMode=first` → only first chunk threads as a reply
- [ ] Set `chunkMode=newline`, send paragraphs → splits on blank lines

## Attachments
- [ ] Send a photo from Telegram → `inbox/` contains the file; `<channel image_path=...>` in tag
- [ ] Send a document → `inbox/` contains the file; `attachment_file_id=...` in tag
- [ ] `download_attachment` tool fetches by file_id

## Reactions
- [ ] Set `ackReaction="👍"` → inbound messages get a 👍 reaction
- [ ] `react` tool with whitelisted emoji → reaction appears
- [ ] `react` tool with non-whitelisted emoji → clean tool error (no crash)

## Edit
- [ ] `edit_message` updates previously-sent chunk

## Groups
- [ ] Add bot to a group, configure group policy with `requireMention=true` and a test user in `allowFrom`
- [ ] Non-mention message from allowed user → dropped
- [ ] `@botname` message → forwarded
- [ ] Message from user not in `allowFrom` → dropped

## Resilience
- [ ] Corrupt `access.json` → renamed to `access.json.corrupt-<ts>`, fresh default loaded
- [ ] Kill process mid-operation → `access.json` never partially written (atomic rename)
- [ ] Static mode (`TELEGRAM_ACCESS_MODE=static`) with pairing policy → stderr warns, downgrades to allowlist
```

- [ ] **Step 2: Commit**

```bash
git add docs/superpowers/specs/2026-04-06-gobrrr-telegram-smoke-test.md
git commit -m "docs(telegram): manual smoke test checklist"
```

---

## Task 14: Final verification

- [ ] **Step 1: Build everything**

Run: `cd daemon && CGO_ENABLED=0 go build ./...`
Expected: clean build of the whole daemon module including the new command.

- [ ] **Step 2: Test everything**

Run: `cd daemon && go test ./cmd/gobrrr-telegram/...`
Expected: all packages pass.

- [ ] **Step 3: Vet**

Run: `cd daemon && go vet ./cmd/gobrrr-telegram/...`
Expected: no warnings.

- [ ] **Step 4: Run smoke test**

Follow `docs/superpowers/specs/2026-04-06-gobrrr-telegram-smoke-test.md` against a real Telegram bot. Tick each item. Fix any failures before declaring done.

- [ ] **Step 5: Final commit (if any fixes)**

```bash
git commit -am "fix(telegram): smoke test findings"
```

---

## Notes for the implementing engineer

- **Library shapes may drift.** This plan uses `go-telegram/bot` and
  `mark3labs/mcp-go` method/type names that are current at plan-write time.
  If a name doesn't exist when you `go build`, run `go doc <pkg>.<Type>` and
  match the actual shape. Do not change behavior — just match names.
- **The source of truth for behavior is the official TypeScript plugin** at
  `~/.claude/plugins/cache/claude-plugins-official/telegram/0.0.4/server.ts`.
  When in doubt about an edge case, read it.
- **TDD discipline:** every logic package (permission, chunker, access,
  sendable) has tests first. The bot and mcpserver packages don't — they're
  thin wrappers covered by the integration test in Task 11 and the manual
  smoke test in Task 13.
- **Commit cadence:** one commit per task. Structural (scaffold, deps) and
  behavioral (feature) commits kept separate per the user's CLAUDE.md rule.
