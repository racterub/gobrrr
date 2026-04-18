# Default Channel Routing

**Date:** 2026-04-10
**Status:** Approved

## Problem

Tasks require a per-dispatch `--reply-to` decision. There is no consistent
default, which creates friction and inconsistency.

## Decision

Make `channel` the single default reply target for all dispatched and
scheduled tasks. The main Claude session sees every result and decides how
to relay it. `telegram` and `telegram,channel` remain available as explicit
opt-in overrides.

When the session is down, results silently drop from channel delivery but
remain in task logs (`gobrrr list`, `gobrrr status <id>`, `gobrrr logs <id>`).
The user prompts the session to check results once it reconnects.

## Changes

1. **Dispatch CLI (`cmd/gobrrr/main.go` submit)** — change `--reply-to`
   flag default to `channel`.
2. **Scheduler (`internal/scheduler/`)** — when a scheduled task omits
   `reply_to`, default to `channel` instead of the current default.
   Per-schedule `reply_to` overrides are still honored.
3. **Docs / skills** — update any text that references the old default.
4. **No code removal** — `telegram` routing path stays intact for opt-in.

## Out of Scope

- Session-down fallback or queuing
- New CLI commands (existing `list`/`status`/`logs` suffice)
- Removing the `telegram` routing path

## Testing

- Unit test: submit with no `--reply-to` resolves to `channel`.
- Unit test: scheduler task with no `reply_to` runs with `channel`.
