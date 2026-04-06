# gobrrr-telegram — Go Telegram channel for Claude Code

**Date:** 2026-04-06
**Status:** Design approved, implementation pending

## Goal

Drop-in replacement for the official `telegram` plugin
(`claude-plugins-official/telegram@0.0.4`, ~1000 lines of Bun/TypeScript).
Same state files, same MCP tool surface, same UX via `/telegram:access` and
`/telegram:configure` skills. Single Go binary, no Bun runtime dependency.

Motivation: the official TypeScript implementation has proven unstable in the
user's deployment. A Go reimplementation aligns with gobrrr's "pure Go, single
binary" constraint and gives full control over process lifetime, error
recovery, and logging.

## Non-Goals

- New features beyond the official plugin's 0.0.4 surface.
- Changes to `/telegram:access` or `/telegram:configure` skills.
- Integration with the gobrrr daemon (`gobrrr.sock`). This channel is
  standalone; it is not a gobrrr task consumer.
- Windows support (official plugin already no-ops chmod on Windows; we match).

## Architecture

```
Claude Code ──stdio(MCP)──▶ gobrrr-telegram (Go binary)
                              │
                              ├─ mcp-go server (tools + notifications)
                              ├─ go-telegram/bot (long-poll)
                              ├─ access store (access.json RW + static snapshot)
                              └─ inbox writer (photos/docs)

State dir: ~/.claude/channels/telegram/   (unchanged, shared with official skills)
  ├─ .env              TELEGRAM_BOT_TOKEN (0600)
  ├─ access.json       policies, allowlists, pending pairings
  ├─ approved/         (reserved for compat)
  └─ inbox/            downloaded attachments
```

### Libraries

- **Telegram Bot API:** `github.com/go-telegram/bot` — actively maintained,
  native context support, clean API.
- **MCP stdio server:** `github.com/mark3labs/mcp-go` — de facto Go MCP SDK,
  supports stdio transport, tool registration, and notifications.

Rationale: both libraries reduce hand-rolled protocol code and track spec
evolution. The alternatives (hand-rolled HTTP + JSON-RPC) were considered and
rejected as ongoing maintenance burden.

## Package Layout

All code lives inside the gobrrr repo at `daemon/cmd/gobrrr-telegram/`,
sharing `daemon/go.mod` but importing no daemon internals.

```
daemon/cmd/gobrrr-telegram/
  main.go              wiring: .env load, init stores, start MCP + bot, signals
  access/
    access.go          access.json read/write (atomic), static snapshot, defaults
    pairing.go         pairing code generation, expiry prune, pending state
    gate.go            inbound gate: DM policy + group policy checks
  mcpserver/
    server.go          MCP stdio server, tool registration
    tools.go           reply, edit_message, react, download_attachment handlers
    sendable.go        assertSendable guard (refuse to exfil STATE_DIR)
    notify.go          inbound → notifications/claude/channel emitter
  chunker/
    chunker.go         message splitting (length + newline modes, 4096 cap)
    chunker_test.go
  permission/
    permission.go      PERMISSION_REPLY_RE regex + matcher
    permission_test.go
  bot/
    bot.go             go-telegram/bot wrapper, long-poll, signal wiring
    inbound.go         update handler: gate, download, emit
    outbound.go        reply/edit/react implementations
    download.go        file download → inbox/
```

### Package responsibilities

- **`access/`** — Pure logic. No I/O except `access.json` read/write. Fully
  unit-testable. Exposes: `Load()`, `Save()`, `Check(chatID, userID, isGroup,
  text) (Decision, error)`, pairing helpers.
- **`mcpserver/`** — Owns the MCP `Server` instance, tool handlers, and the
  notification emitter. Depends on `access`, `chunker`, `bot` (for outbound
  calls).
- **`chunker/`** — Pure function `Split(text string, mode string, limit int)
  []string`. No deps.
- **`permission/`** — Pure regex matcher.
- **`bot/`** — Owns the `go-telegram/bot.Bot` instance. Long-poll loop routes
  updates to inbound handler. Outbound functions called by `mcpserver` tools.
- **`main.go`** — Composes everything. Loads `.env` with chmod 0600, reads
  `TELEGRAM_BOT_TOKEN`, decides static mode, wires bot ↔ mcpserver.

## Feature Parity Checklist

Every feature of `telegram@0.0.4` server.ts must be ported:

1. MCP tools: `reply`, `edit_message`, `react`, `download_attachment`
2. DM policies: `pairing`, `allowlist`, `disabled`
3. Group support with mention-triggering and per-group `allowFrom`
4. `mentionPatterns` custom regex list
5. Static mode (`TELEGRAM_ACCESS_MODE=static`) — snapshot at boot, no writes,
   `pairing` downgraded to `allowlist` with stderr warning
6. Permission-reply regex: `^\s*(y|yes|n|no)\s+([a-km-z]{5})\s*$` (case
   insensitive, no bare yes/no, no prefix/suffix chatter)
7. Message chunking: `length` and `newline` modes, configurable
   `textChunkLimit` up to 4096
8. `replyToMode`: `off` / `first` / `all` threading behavior
9. `ackReaction` emoji on inbound receipt (empty string disables)
10. Image/document attachments via `inbox/` dir; inbound tag gets
    `image_path=` for photos, `attachment_file_id=` for generic files
11. `assertSendable` guard: refuse to send anything under `STATE_DIR` except
    `inbox/` (via `realpath`-equivalent resolution)
12. `.env` auto-load with 0600 chmod; real env wins
13. Corrupt `access.json` recovery: rename aside
    `access.json.corrupt-<unix-ms>`, start fresh (skipped in static mode)
14. Atomic writes to `access.json` via `.tmp` + rename
15. `MAX_ATTACHMENT_BYTES` = 50 MiB outbound cap
16. Panic recovery equivalent to `unhandledRejection` / `uncaughtException` —
    log to stderr, keep serving

## Data Flow

### Inbound

1. go-telegram/bot long-poll receives update.
2. `bot/inbound.go` extracts `chat_id`, `user_id`, `message_id`, `ts`, text,
   attachments.
3. `access.Check` runs:
   - DM: policy `disabled` → drop; `allowlist` → check `allowFrom`;
     `pairing` → if matches permission regex against a pending code, approve
     and add to `allowFrom`; otherwise issue new pairing code, reply with it,
     stop.
   - Group: look up group policy, check `allowFrom`, and if `requireMention`
     is true check bot username mention and `mentionPatterns`.
4. If approved and message has photo/document, download to
   `inbox/<chat_id>-<message_id>-<name>`.
5. Build `<channel source="telegram" ...>` opening tag with attributes, emit
   MCP notification `notifications/claude/channel` with `content` = tag +
   body + closing tag, `meta` = raw fields.
6. If `ackReaction` set, react with that emoji.

### Outbound (`reply`)

1. MCP tool handler receives `{chat_id, text, reply_to?, files?}`.
2. `assertAllowedChat(chat_id)` — must be in `allowFrom` or `groups`.
3. For each file in `files`: `assertSendable(path)` and size check against
   `MAX_ATTACHMENT_BYTES`.
4. `chunker.Split(text, chunkMode, textChunkLimit)`.
5. Send chunks in order. Apply `replyToMode`:
   - `off` → never set `reply_to_message_id`
   - `first` → set only on first chunk
   - `all` → set on every chunk
6. After text chunks, send each file as document (or photo for image types).
7. Return `{message_ids: [...]}` to Claude.

### `edit_message`, `react`, `download_attachment`

Direct Bot API calls with `assertAllowedChat` gating. `download_attachment`
fetches by `file_id` into `inbox/` and returns the path.

## Error Handling

- **Panic recovery:** `main.go` installs a top-level recover in the bot
  goroutine; logs to stderr and continues. Tool handlers each have their own
  recover → return error to MCP caller.
- **Telegram errors:** surface cleanly as tool errors with the Bot API
  description. Rate-limits (429) handled by go-telegram/bot's built-in retry
  where possible; otherwise return error, let Claude decide.
- **Corrupt `access.json`:** detected on `Load()`, renamed aside, fresh
  default returned. Skipped in static mode (would lose boot snapshot).
- **Missing `.env` / token:** stderr message matching official plugin's
  format, exit 1.
- **Static-mode pairing:** warn to stderr at boot, downgrade to `allowlist`
  in-memory only.

## Testing

### Unit tests
- `access`: parse valid/missing/corrupt, atomic save round-trip, static
  snapshot semantics, pairing code generation + expiry prune, `Check()`
  across all policy matrix cells (dm × {pairing, allowlist, disabled},
  group × {mention, no mention, allowFrom match/miss}).
- `chunker`: single short, exact boundary, multi-chunk length mode,
  newline mode with long paragraphs, empty string, text with only newlines.
- `permission`: accept `y abcde`, `YES abcde`, reject `yes`, `y`,
  `y abcde foo`, `y l-containing`.
- `mcpserver.assertSendable`: reject `STATE_DIR/access.json`, allow
  `STATE_DIR/inbox/x.png`, allow `/tmp/foo.png`, handle symlinks.

### Integration tests (httptest)
Spin up an `httptest.Server` that mimics Bot API endpoints (`/getMe`,
`/getUpdates`, `/sendMessage`, `/editMessageText`, `/setMessageReaction`,
`/getFile`, file download). Configure go-telegram/bot to use that base URL.

- Drive `reply` tool → assert outbound `/sendMessage` calls match chunking
  and `reply_to_message_id` per `replyToMode`.
- Feed synthetic `/getUpdates` response → assert MCP notification emitted
  with correct `<channel>` tag attributes.
- Pairing flow: unknown DM → assert pairing code sent; follow-up
  `y <code>` → assert `allowFrom` updated and subsequent messages
  forwarded.

### Manual smoke test
A checklist doc `docs/superpowers/specs/2026-04-06-gobrrr-telegram-smoke-test.md`
covering against real Telegram:

- [ ] `.env` loaded, token detected
- [ ] DM from unknown user in `pairing` mode receives code
- [ ] `y <code>` approves and enables forwarding
- [ ] Inbound photo lands in `inbox/`, `image_path` in tag
- [ ] Outbound chunked message splits on newline mode
- [ ] `replyToMode=first` threads only the first chunk
- [ ] `ackReaction` fires on receipt
- [ ] Group mention triggers forwarding; non-mention does not
- [ ] `edit_message` updates previously-sent chunk
- [ ] `react` with non-whitelist emoji returns clean error
- [ ] Corrupt `access.json` renamed aside, fresh start
- [ ] Permission-reply regex approves a pending gate

## Plugin Packaging

- New plugin dir in gobrrr repo: `plugins/gobrrr-telegram/` with a
  `plugin.json` (format per Claude Code plugin marketplace spec) whose
  channel entrypoint execs the built Go binary at an install-time-resolved
  path.
- `scripts/install.sh` (or new `scripts/install-telegram-channel.sh`) builds
  the binary via `CGO_ENABLED=0 go build` and installs to
  `$GOBRRR_HOME/bin/gobrrr-telegram`.
- Install script prints: "disable the official `telegram` plugin, then enable
  `gobrrr-telegram` from the gobrrr marketplace".
- State dir stays `~/.claude/channels/telegram/` — zero migration.

## Open Questions

None at design time. Implementation plan will resolve:

- Exact `plugin.json` schema (look up current plugin spec when implementing)
- Which go-telegram/bot options need tuning for long-poll stability
- Whether mcp-go exposes a notification API compatible with
  `notifications/claude/channel` custom method (expected yes via raw
  send; verify in plan)

## Constraints

- `CGO_ENABLED=0`, single static binary
- No dependency on gobrrr daemon packages
- File permissions: `access.json` 0600, `.env` 0600, `STATE_DIR` 0700,
  `inbox/` 0700
