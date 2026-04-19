# Skill Ecosystem Design

**Date:** 2026-04-19
**Status:** Draft — Phase 1 for implementation, Phase 2 sketched

## Problem

gobrrr currently ships 7 SKILL.md files in `daemon/skills/` that are flat markdown docs, not wired into the worker pipeline. The daemon's `worker.go:buildFullPrompt()` injects identity + memories into every worker prompt but never references skills. Workers only discover skills if Claude Code happens to find them via its own filesystem scans — which doesn't apply to `claude -p` workers running in sandboxed workspaces.

This leaves two gaps:

1. **No ecosystem** — the user can't pull from ClawHub's 5,198-skill curated registry without hand-copying files. Every new capability (github, slack, jq, docker, etc.) requires writing a SKILL.md from scratch.
2. **No self-extension** — workers can't request new skills when they hit a gap; they just fail or muddle through.

## Goal

Enable two capabilities, staged in two phases:

- **Phase 1 (this cycle)**: User-driven install from ClawHub via CLI. Workers automatically see installed skills through prompt injection. All install decisions gated by explicit user approval at the terminal.
- **Phase 2 (follow-up)**: Worker-driven install requests routed to Telegram for approval. `create-skill` meta-skill lets workers author drafts. Requires a new approval-routing primitive between daemon and launcher.

## Non-Goals

- Publishing gobrrr's own skills to ClawHub (interop format is adopted, but publishing workflow is deferred).
- Replacing Claude Code's native skill discovery — gobrrr runs its own loader because `claude -p` workers with plugins disabled don't get the native discovery path.
- Per-task skill selection (`--skill <name>` at submit time). Present design injects union of all installed skills; per-task scoping is a potential Phase 3 optimization.
- Arbitrary code execution in skills. Skills are markdown + optional scripts the user approves at install time.

## Constraints

- Pure Go, no cgo (`CGO_ENABLED=0`) — rules out shelling to Node/bun for the ClawHub client.
- Claude Max subscription only — no API keys.
- Install actions run in the **daemon process** (privileged, owns `~/.gobrrr/`). Workers never run package-manager commands.
- User-settings leak prevention still applies — worker `settings.json` with `enabledPlugins: false` must be preserved.
- All JSON persistence uses atomic writes (`.tmp` + `os.Rename`).
- File permissions: `~/.gobrrr/skills/` is `0700`, `_meta.json` is `0600`.
- Approval decisions are append-only for audit; `_meta.json` records what was approved and when.

## Architecture Overview

```
                          ┌────────────────────┐
                          │   clawhub.com      │
                          │   (REST + tarball) │
                          └─────────┬──────────┘
                                    │ HTTP
                                    ▼
┌──────────────┐       ┌────────────────────────────────┐
│ User at CLI  │◄─────►│  gobrrr daemon                 │
│              │       │    internal/clawhub/  ◄ client │
└──────────────┘       │    internal/skills/   ◄ loader │
                       │    internal/security/ ◄ merge  │
                       └───────────────┬────────────────┘
                                       │ spawns
                                       ▼
                          ┌────────────────────────────┐
                          │ Worker (claude -p)         │
                          │   prompt: identity +       │
                          │   memories + skills block  │
                          └────────────────────────────┘
```

New Go packages:

- `daemon/internal/skills/` — loader, registry, prompt builder
- `daemon/internal/clawhub/` — HTTP client, installer

Modified:

- `daemon/internal/daemon/worker.go` — append `<available_skills>` block to prompt
- `daemon/internal/security/permissions.go` — merge skill-declared tool permissions into task `settings.json`
- `daemon/cmd/gobrrr/main.go` — add `skill` subcommand tree

## Phase 1: User-Driven Install

### Directory Layout

```
~/.gobrrr/skills/
  <slug>/
    SKILL.md                YAML frontmatter + markdown body
    scripts/...             optional bundled scripts
    _meta.json              gobrrr-added: install source, approved perms, fingerprint
  _lock.json                ClawHub-sourced skills: slug → version → sha256
  _requests/
    <req-id>.json           pending install approvals (24h TTL)
```

System skills (`type: system` in frontmatter — `gmail`, `calendar`, etc.) are copied from `daemon/skills/` to `~/.gobrrr/skills/` on first daemon start. Idempotent — skips copy if target exists and fingerprint shows user edits.

### YAML Frontmatter Schema

OpenClaw-compatible. Workers read SKILL.md as plain markdown; frontmatter is parsed only by gobrrr's loader and installer.

```yaml
---
name: github
description: GitHub issue/PR operations via the gh CLI
metadata:
  gobrrr:
    type: clawhub          # system | clawhub | user
  openclaw:
    emoji: "🐙"
    homepage: "https://clawhub.com/github"
    requires:
      bins: [gh, git]
      env: []
      tool_permissions:
        read:
          - "Bash(gh issue list:*)"
          - "Bash(gh pr list:*)"
          - "Bash(git log:*)"
          - "Bash(git diff:*)"
        write:
          - "Bash(gh issue create:*)"
          - "Bash(gh pr create:*)"
          - "Bash(gh pr comment:*)"
    install:
      - id: gh-brew
        kind: brew
        formula: gh
        bins: [gh]
      - id: gh-apt
        kind: apt
        package: gh-cli
        bins: [gh]
---

# GitHub Skill

## When to Activate
...
```

### `_meta.json` (Written at Approval Time)

Records what the user approved. Loader reads this, never trusts SKILL.md frontmatter alone. Split matches frontmatter's `tool_permissions.read` / `tool_permissions.write`.

```json
{
  "slug": "github",
  "version": "1.4.2",
  "source_url": "https://clawhub.com/github",
  "installed_at": "2026-04-19T16:45:00Z",
  "fingerprint": "sha256:abc123...",
  "approved_read_permissions": [
    "Bash(gh issue list:*)",
    "Bash(gh pr list:*)",
    "Bash(git log:*)",
    "Bash(git diff:*)"
  ],
  "approved_write_permissions": [
    "Bash(gh issue create:*)",
    "Bash(gh pr create:*)",
    "Bash(gh pr comment:*)"
  ],
  "approved_binaries": ["gh"],
  "binary_install_commands": [
    {"approved": true, "command": "sudo apt install gh-cli"}
  ]
}
```

### ClawHub Client (`daemon/internal/clawhub/`)

- `client.go` — HTTP client, no auth required for public registry:
  - `Search(query string, limit int) ([]SkillSummary, error)` — calls `GET /api/skills/search?q=...`
  - `Fetch(slug, version string) (*SkillPackage, error)` — `GET /api/skills/<slug>` for metadata, then tarball download
  - `VerifyChecksum(pkg, expectedSha256)` — sha256 verify after download
- `installer.go` — install orchestration:
  1. Fetch tarball to tempdir
  2. Verify sha256 matches registry metadata
  3. Parse SKILL.md frontmatter
  4. Compose `InstallRequest{RequestID, Slug, Version, Frontmatter, MissingBins, ProposedCommands}`
  5. Persist to `~/.gobrrr/skills/_requests/<id>.json`
  6. Return request-id to caller (CLI prints approval card)
- `commit.go` — on approval:
  1. Run approved package-manager commands (if any) via `exec.Command`, stream output
  2. Copy staged tree to `~/.gobrrr/skills/<slug>/`
  3. Compute final fingerprint
  4. Write `_meta.json` with approved decisions
  5. Update `_lock.json`
  6. Notify skill registry to refresh

### Loader (`daemon/internal/skills/`)

- `types.go`:
  ```go
  type Skill struct {
      Slug               string
      Description        string
      Path               string     // absolute path to SKILL.md
      Dir                string     // skill directory
      Type               string     // system | clawhub | user
      ReadPermissions    []string   // from _meta.json approved_read_permissions
      WritePermissions   []string   // from _meta.json approved_write_permissions
      Fingerprint        string
  }
  ```
- `loader.go` — walks `~/.gobrrr/skills/`, skips `_pending/` / `_lock.json` / `_requests/`, parses frontmatter + `_meta.json`.
- `registry.go` — in-memory cache; `Refresh()` walks filesystem and rebuilds. Called on daemon start, after every install/uninstall, and via a `SIGUSR1` handler for manual refresh.
- `prompt.go`:
  ```go
  func BuildPromptBlock(reg *Registry) string
  ```
  Emits XML:
  ```xml
  <available_skills>
    <skill name="github" location="~/.gobrrr/skills/github/SKILL.md">
      GitHub issue/PR operations via the gh CLI
    </skill>
    <skill name="gmail" location="~/.gobrrr/skills/gmail/SKILL.md">
      Email read/send/reply via gobrrr CLI
    </skill>
  </available_skills>
  ```
  Home-path tilde compaction saves ~5 tokens per skill. If block exceeds 3k tokens, log a warning (soft limit — user owns their library).

### Worker Wiring

`worker.go:buildFullPrompt()`:

```go
prompt := taskPrompt
if ident != nil {
    prompt = identity.BuildPrompt(ident, memContents, prompt)
}
if reg := skills.Get(); reg != nil {
    prompt = skills.BuildPromptBlock(reg) + "\n\n" + prompt
}
return prompt
```

Skills block appears **after** identity + memories, **before** the task prompt. Claude reads SKILL.md on demand using its existing Read tool (already in the permission baseline).

### Permission Merge (`daemon/internal/security/permissions.go`)

`Generate(task *Task, reg *skills.Registry) map[string]any` — existing signature gets a skills registry param:

1. Start from the baseline `settings.json` (enabledPlugins: false, existing deny list).
2. For every installed skill, read `_meta.json`:
   - Add `approved_read_permissions` to `permissions.allow` unconditionally.
   - Add `approved_write_permissions` to `permissions.allow` only if `task.AllowWrites == true`.
3. Deny list stays intact — skills cannot remove existing denies.

**No automatic read/write classification.** The partition lives in the skill's frontmatter (`tool_permissions.read` vs `tool_permissions.write`) and is approved by the user at install time. This avoids brittle heuristics over Bash patterns.

Union-of-all-installed-skills is intentional: skill Read-on-demand is the real gate; permission merge just ensures tools work when Claude invokes them.

### CLI Surface

```
gobrrr skill search <query>              # hit ClawHub, print ranked list
gobrrr skill install <slug>[@version]    # stage; print approval card with request-id
gobrrr skill approve <req-id> [--yes]    # commit; run approved pkg-manager commands
gobrrr skill approve <req-id> --skip-binary   # commit skill without running binary install
gobrrr skill deny <req-id>
gobrrr skill list [--json]
gobrrr skill info <slug>
gobrrr skill uninstall <slug>
gobrrr skill update [--all|<slug>]       # fetch new version, show diff, re-approve
```

### Approval Card (CLI stdout)

```
Install skill: github@1.4.2
  Source: https://clawhub.com/github  sha256: abc123...
  Description: GitHub issue/PR operations via the gh CLI

  Requires binaries: gh  (not on PATH)
    Proposed install:  sudo apt install gh-cli

  Tool permissions (read, always allowed):
    Bash(gh issue list:*)
    Bash(gh pr list:*)
    Bash(git log:*)
    Bash(git diff:*)

  Tool permissions (write, require --allow-writes on task):
    Bash(gh issue create:*)
    Bash(gh pr create:*)
    Bash(gh pr comment:*)

  Request ID: 7f3a

  To proceed:      gobrrr skill approve 7f3a
  Skill only:      gobrrr skill approve 7f3a --skip-binary
  Cancel:          gobrrr skill deny 7f3a
```

### Fingerprint & Updates

- **Fingerprint**: `sha256(concat(sorted(relpath + content) for each file))` of the skill directory, stored in `_meta.json`.
- **`gobrrr skill update <slug>`**: fetch latest tarball → verify checksum → diff frontmatter `requires` and `tool_permissions` against current `_meta.json` → print approval card showing changes → on approval, replace directory preserving user-modified system-skill edits if detected.
- **Local-edit detection**: if current fingerprint differs from last-installed fingerprint, refuse update by default; user passes `--force` to overwrite local edits.

### Error Handling

- ClawHub unreachable → search/install fail with clear error; local skills keep working.
- Malformed SKILL.md frontmatter → loader logs, skips skill, continues.
- Missing required binary at runtime (worker invokes a skill whose declared binary isn't on PATH) → Claude's own failure message is surfaced to the user; no daemon-level intervention.
- Expired approval requests (>24h) → hourly maintenance sweep (`internal/daemon/maintenance.go`) deletes them.
- Checksum mismatch on tarball → abort install, report hash mismatch.

### Bundled Skill Migration

Current 7 skills get YAML frontmatter added in-place:

```yaml
---
name: gmail
description: Email read/send/reply via gobrrr CLI
metadata:
  gobrrr:
    type: system
  openclaw:
    requires:
      bins: [gobrrr]
      tool_permissions:
        read:
          - "Bash(gobrrr gmail list:*)"
          - "Bash(gobrrr gmail read:*)"
        write:
          - "Bash(gobrrr gmail send:*)"
          - "Bash(gobrrr gmail reply:*)"
---
```

System skills:

- Have `type: system` in frontmatter
- Are copied from `daemon/skills/` to `~/.gobrrr/skills/` on daemon start (idempotent)
- Get a `_meta.json` generated automatically with `approved_read_permissions` / `approved_write_permissions` mirroring frontmatter (no user approval needed — shipped with the binary)
- Appear uniformly in the `<available_skills>` block alongside ClawHub and user skills

## Phase 2: Sketch (Out of Scope for Implementation)

Documented here so Phase 1 doesn't paint us into a corner.

### Missing Primitive: Approval-Routing Channel

**Topology (current):**

- Inbound: `user → telegram → gobrrr-telegram → launcher`
- Outbound (launcher-originated): `launcher → gobrrr-telegram → telegram → user`
- Outbound (worker-originated): `worker → daemon → gobrrr-relay → launcher → gobrrr-telegram → telegram → user`

**Gap:** no way for the daemon to receive an *inbound* approval response from the launcher. Current flow is fire-and-forget notifications.

**Proposed:** a small JSON-RPC-style request/response channel exposed by the daemon (new unix socket endpoint or extend existing `gobrrr-relay` protocol):

```
Daemon → Launcher (push via existing relay):
  ApprovalRequest{
    id:      "req-abc",
    kind:    "skill_install"|"write_action"|...,
    title:   "Install skill: github",
    body:    "<approval card content>",
    actions: ["approve", "approve_skill_only", "deny"],
    expires: "2026-04-20T16:45:00Z"
  }

Launcher → Daemon (inbound via new endpoint POST /approvals/<id>):
  ApprovalResponse{
    id:       "req-abc",
    decision: "approve"|"approve_skill_only"|"deny",
    user:     "<telegram username>",
    ts:       "2026-04-19T16:47:00Z"
  }
```

Persistent queue: `~/.gobrrr/approvals.json` (atomic writes, 24h TTL, hourly sweep).

### Worker-Driven Install

New CLI: `gobrrr skill request <slug> --reason "<why>"`.

Worker invokes this mid-task when it hits a gap. Daemon:

1. Stages an `InstallRequest` via the ClawHub installer.
2. Wraps it in an `ApprovalRequest` and pushes through the relay.
3. Marks the originating task as `failed` with message `"needed skill: <slug> — install request sent"`.
4. On user approval → daemon runs the full install commit → user re-submits original task.

Fail-fast model — worker does not block waiting for approval (workers cost tokens while idle).

### `create-skill` Meta-Skill

Worker authors a SKILL.md draft and writes it to `~/.gobrrr/skills/_pending/<slug>/SKILL.md`.

Worker then calls `gobrrr skill propose <slug> --reason "<what gap this fills>"`.

Daemon reads the pending draft, composes an approval request with the draft body, sends via relay. User sees the proposed skill on Telegram.

On approval: draft moves from `_pending/` to `~/.gobrrr/skills/<slug>/` with `metadata.gobrrr.type: user` in frontmatter, and `_meta.json` records the approved permissions.

Requires tighter security checks than ClawHub skills because the source is an untrusted worker — extra validation keyed off `type: user`: no new `Bash(sudo *)` patterns, no shell-pipe patterns, no binary install blocks (user skills are markdown-only, `kind: download`-free).

### Telegram Approval UX

Approval card rendered as Telegram message with inline keyboard buttons:

```
┌──────────────────────────────────┐
│ Install skill: github@1.4.2      │
│                                  │
│ Description: GitHub issue/PR ops │
│ Binaries: gh (not on PATH)       │
│ Install: sudo apt install gh-cli │
│ Permissions:                     │
│   Bash(gh:*)                     │
│   Bash(git log:*)                │
│                                  │
│ [✓ Approve]  [Skill only] [✗ Deny]│
└──────────────────────────────────┘
```

Buttons trigger launcher → daemon `ApprovalResponse`. Full SKILL.md body shown as attached text file if over message size limit.

### Routing Constraint

All outbound user-facing messages from worker/daemon flow through the launcher so it retains conversation context. `--reply-to telegram` (direct daemon → Telegram bypass) remains discouraged; skill install notifications follow the same rule.

## Testing Strategy

- **Unit tests**:
  - Frontmatter parser (round-trip, malformed inputs, missing fields)
  - Registry refresh (mtime invalidation, pending/requests skip logic)
  - Prompt block builder (token count, XML escaping, tilde compaction)
  - Permission merge (deny-list preservation, write-gate intersection, union correctness)
  - Fingerprint stability (reordering files, whitespace changes)
- **Integration tests**:
  - Fake ClawHub HTTP server (`httptest.NewServer`) serving fixture tarballs
  - Full install → approve → commit cycle with `~/.gobrrr/` in tempdir
  - Verify `_lock.json`, `_meta.json`, `~/.gobrrr/skills/<slug>/` state after install
  - Update cycle (v1 → v2) with and without local edits
- **End-to-end tests**:
  - Spawn daemon against tempdir
  - Install fixture skill
  - Submit task referencing the skill
  - Capture worker stdout/args; verify `<available_skills>` block present
  - Verify merged `settings.json` contains skill-declared permissions

## Out of Scope (Both Phases)

- Publishing gobrrr skills to ClawHub — separate spec
- Per-task skill filtering (`--skill gh,jq` at submit time) — potential later optimization
- Non-ClawHub registries — single registry for now
- Skill signing / provenance verification beyond sha256 checksum
- Auto-update daemon (cron-driven skill updates) — user runs `gobrrr skill update` manually
- Dev/workspace-level skills (project `.gobrrr/skills/`) — home directory only for now

## Acceptance Criteria (Phase 1)

- [ ] `gobrrr skill search github` returns at least one result from ClawHub.
- [ ] `gobrrr skill install github` stages install and prints approval card with request ID.
- [ ] `gobrrr skill approve <id>` runs approved binary install, copies skill to `~/.gobrrr/skills/github/`, writes `_meta.json`, updates `_lock.json`.
- [ ] `gobrrr skill approve <id> --skip-binary` installs skill without running pkg-manager commands.
- [ ] `gobrrr skill deny <id>` removes the staged request.
- [ ] `gobrrr skill list` shows installed skills (system + ClawHub) grouped by `type`.
- [ ] `gobrrr skill uninstall <slug>` removes skill dir and lock entry.
- [ ] Worker prompt contains `<available_skills>` block with all installed skills.
- [ ] Worker's merged `settings.json` includes approved tool permissions from installed skills.
- [ ] System skills (`type: system`) have OpenClaw-compatible frontmatter and appear in `~/.gobrrr/skills/` after first daemon start.
- [ ] Malformed SKILL.md or unreachable ClawHub does not crash daemon.
- [ ] All existing tests pass; new tests cover loader, installer, prompt builder, permission merge.

## Open Questions

- **ClawHub API surface**: need to verify the public REST endpoints and tarball URL format against `openclaw/clawhub` source before implementation. Will the Go client need to replicate any TypeScript-side auth flow, or is all public content accessible unauthenticated?
- **Version resolution**: exact semantics of `gobrrr skill install github` with no version pin — latest stable? latest any? ClawHub's conventions should dictate.
- **System-skill ownership**: if a user modifies `~/.gobrrr/skills/gmail/SKILL.md` (`type: system`), should daemon restart overwrite it? Current design: no, respect user edits, but this means upstream system-skill fixes don't reach already-installed users without explicit `gobrrr skill update gmail --force`.
