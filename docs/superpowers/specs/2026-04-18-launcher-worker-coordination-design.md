# Launcher/Worker Coordination and Role-Based Model Selection

## Problem

The launcher (Telegram session) and workers (warm and cold) today spawn Claude with a single default model and no permission mode. There is no coordination between them — no shared notion of *who should use which model* or *which permission regime*. In particular:

- Launcher runs `claude --channels` with `--dangerously-skip-permissions`. No model specified.
- Cold workers run `claude --print` with per-task `--settings` deny rules. No model specified.
- Warm workers run `claude -p --input-format stream-json ... --dangerously-skip-permissions`. No model specified.

Without role-based model assignment, the launcher (which is meant to be a pure dispatcher) runs with the same default capability as the workers — risking *intelligent insubordination* where a capable model answers the user directly instead of delegating to a worker. Research confirms this is a real failure mode: in one benchmark, swapping a capable planner for a weak one raised accuracy from 31.71% to 74.27%, with the failure signature being `role2_never_called` — the solver was never invoked (see references).

## Solution

Assign each role a fixed model and permission mode via `~/.gobrrr/config.json`. The launcher gets a deliberately weak model (haiku) so it cannot do work on its own — it must delegate. Workers get appropriately strong models (sonnet for warm, opus for cold) and run under Claude Code's `--permission-mode auto`, which auto-approves tool calls through a classifier that still respects the existing deny-rule permission model.

## Architecture

| Role | Model | Permission mode | Notes |
|------|-------|-----------------|-------|
| Launcher (`claude --channels`) | haiku | `default` + tight allow-list | Auto mode unsupported on haiku; not needed for a pure router. |
| Warm worker (`claude -p --input-format stream-json ...`) | sonnet | `auto` | Replaces `--dangerously-skip-permissions`. Classifier respects deny rules. |
| Cold worker (`claude --print`) | opus | `auto` | Per-task `security.Generate()` deny list unchanged. |

The launcher's dispatcher-only role is enforced at three independent layers:

1. **Model capability** — haiku cannot sustain complex multi-step reasoning, so it must hand off.
2. **Prompt/instructions** — identity and CLAUDE.md on the launcher host direct it to always call `gobrrr submit`.
3. **Permissions** — the launcher's `--settings` allow-list restricts tools to `Bash(gobrrr submit:*)`, related `gobrrr` read commands, and telegram MCP tools. Anything else halts the session.

## Configuration Schema

New `models` block in `~/.gobrrr/config.json`:

```json
{
  "models": {
    "launcher":    { "model": "haiku",  "permission_mode": "default" },
    "warm_worker": { "model": "sonnet", "permission_mode": "auto" },
    "cold_worker": { "model": "opus",   "permission_mode": "auto" }
  }
}
```

- Model strings are Claude CLI aliases (`haiku`, `sonnet`, `opus`) — upgrades ride the latest-stable-in-family rail without config churn.
- Loader rejects unknown `permission_mode` values. Haiku + `auto` is rejected (Claude rejects it too) with a fallback to `default` and a warning log.
- `config.Default()` populates these three entries so existing installs upgrade silently.
- No env-var overrides. No per-task `--model` flag on `gobrrr submit`. Pure static config.
- The launcher reads the same file via `jq` in `scripts/launcher.sh` — single source of truth.

## Launcher Wiring

### `scripts/launcher.sh`

Reads model and mode from config:

```bash
LAUNCHER_MODEL=$(cfg '.models.launcher.model // "haiku"')
LAUNCHER_MODE=$(cfg '.models.launcher.permission_mode // "default"')
LAUNCHER_SETTINGS="${GOBRRR_DIR}/launcher-settings.json"
```

Invocation inside the `expect` block changes from:

```
spawn -noecho $CLAUDE_BIN $CHANNELS
```

to:

```
spawn -noecho $CLAUDE_BIN --model $LAUNCHER_MODEL --permission-mode $LAUNCHER_MODE --settings $LAUNCHER_SETTINGS $CHANNELS
```

`--dangerously-skip-permissions` is dropped. The allow-list in `$LAUNCHER_SETTINGS` enforces launcher's narrow tool surface.

### `~/.gobrrr/launcher-settings.json`

Generated once by the install script or setup wizard, not per-session:

```json
{
  "permissions": {
    "allow": [
      "Bash(gobrrr submit:*)",
      "Bash(gobrrr status:*)",
      "Bash(gobrrr list:*)",
      "Bash(gobrrr logs:*)",
      "mcp__plugin_telegram_telegram__*"
    ],
    "deny": ["Write", "Edit", "Bash(rm:*)", "Bash(git push:*)"]
  }
}
```

### Launcher instructions (deployed separately)

The launcher's `~/.claude/CLAUDE.md` and `~/.gobrrr/identity.md` on the remote `claude-agent` server get updated to make the dispatcher contract explicit:

> You are a router, not an executor. For every user request: decide warm vs cold, call `gobrrr submit --warm` or `gobrrr submit`, wait for the result via `--reply-to stdout`, relay to Telegram. Never read or write files. Never call tools other than `gobrrr *` and telegram replies.

Dispatch decision rule:

- `--warm` — short Q&A, memory lookups, single-command answers, status checks.
- cold (no flag) — write operations (requires `--allow-writes`), multi-step analysis, anything involving file editing or complex reasoning.
- `--warm --allow-writes` falls through to cold automatically via the existing security gate.

This instruction work absorbs the `Teach Telegram session to dispatch via gobrrr warm/cold workers` TODO item.

## Warm Worker Wiring

### `internal/daemon/warm_worker.go` — `Start()`

Resolve model and mode from config at spawn:

```go
model := wp.cfg.Models.WarmWorker.Model
mode  := wp.cfg.Models.WarmWorker.PermissionMode
```

Invocation changes from:

```go
exec.Command("claude", "-p",
    "--input-format", "stream-json",
    "--output-format", "stream-json",
    "--dangerously-skip-permissions",
    "--verbose",
)
```

to:

```go
exec.Command("claude", "-p",
    "--model", model,
    "--permission-mode", mode,
    "--settings", warmSettingsPath,
    "--input-format", "stream-json",
    "--output-format", "stream-json",
    "--verbose",
)
```

### `~/.gobrrr/workers/warm-settings.json`

Generated once at daemon startup (not per-task — warm workers are persistent and share one file across tasks):

```json
{
  "permissions": {
    "allow": [
      "Read", "Glob", "Grep",
      "Bash(gobrrr memory:*)",
      "Bash(git log:*)", "Bash(git status)", "Bash(git diff:*)"
    ],
    "deny": ["Write", "Edit", "NotebookEdit", "Bash(rm:*)", "Bash(git push:*)"]
  }
}
```

The allow-list is intentionally narrow. Every allow-listed tool skips the auto-mode classifier entirely, reducing the risk of the 3-consecutive-block / 20-total session-abort kill.

### Classifier-abort recovery

Warm worker stderr goes to `~/.gobrrr/logs/warm-<id>.log` (new) so classifier aborts can be distinguished from generic crashes. The existing crash-recovery branch in `dispatchWarm` (worker.go:338) already respawns crashed warm workers — no new code path is needed, but see "Error Handling and Observability" for the anti-flapping guard.

### AllowWrites gate

Warm workers never receive `AllowWrites=true` tasks — the existing gate in `queue.go` excludes them from `NextWarm`. The warm-settings deny on `Write`/`Edit` is a safety net, not the primary enforcement.

## Cold Worker Wiring

### `internal/daemon/worker.go` — `defaultBuildCommand()`

Resolve model and mode from config:

```go
model := wp.cfg.Models.ColdWorker.Model
mode  := wp.cfg.Models.ColdWorker.PermissionMode
```

Args become:

```go
args := []string{
    "--print",
    "--output-format", "text",
    "--model", model,
    "--permission-mode", mode,
    "--settings", settingsPath, // from security.Generate(), unchanged
}
args = append(args, prompt)
```

### Interaction with AllowWrites and auto mode

Auto mode respects deny rules, so existing `security.Generate()` output remains the primary permission layer:

- `AllowWrites=false` — settings deny Write/Edit. Auto mode honors the deny; the classifier never sees the call.
- `AllowWrites=true` — settings allow Write/Edit. Auto mode classifier inspects each write for obvious escalations (data exfiltration, unrecognized infrastructure) and blocks if suspicious. The existing user-confirmation gate in `security/confirm.go` still fires for sensitive actions.

### Broad wildcards

Auto mode drops broad wildcards like `Bash(*)` on startup. `security.Generate()` already produces narrow rules — nothing to change — but a one-line comment in `internal/security/permissions.go` documents this interaction.

### Timeout

Unchanged. Auto mode does not alter the process lifecycle beyond its internal classifier-block counters.

## Error Handling and Observability

1. **Config validation** in `config.Load()`:
   - Reject unknown `permission_mode` values.
   - If `launcher.permission_mode == "auto"` and `launcher.model == "haiku"`, log a warning and fall back to `"default"` (Claude rejects this combo).
2. **Model names not validated** against a hard-coded list — Claude aliases evolve. Let Claude reject; surface stderr in logs.
3. **Warm worker stderr capture** — redirect to `~/.gobrrr/logs/warm-<id>.log` so classifier aborts are diagnosable. Cold workers already log stderr per-task; no change.
4. **Health endpoint** — `daemon.Health()` adds a `models` object (launcher/warm/cold model names) to its JSON response. `gobrrr daemon status` prints them. Makes misconfig obvious.
5. **Anti-flapping guard** — if a warm worker slot respawns twice within 60s, stop respawning that slot and surface the disabled state via the health endpoint. Manual daemon restart required to re-enable. Prevents classifier-abort respawn loops.
6. **Cold workers** — no new failure modes. A classifier abort is just a task failure, routed through the existing error path.

## Testing

### Unit tests

- `config_test.go` — defaults for the `models` block, override path, haiku+auto warning, unknown `permission_mode` rejected. Update `TestDefaultConfig` to assert the new defaults.
- `worker_test.go` — assert `defaultBuildCommand` emits `--model opus --permission-mode auto` given config, and does not emit `--dangerously-skip-permissions`.
- `warm_worker_test.go` — mock-script test that `Start()` spawns `claude` with `--model sonnet --permission-mode auto` and no `--dangerously-skip-permissions`.

### Integration tests

- Cold worker with `AllowWrites=false` + auto mode — deny on Write still fires.
- Warm worker happy-path — task completes through NDJSON protocol with the new flags.
- Warm worker classifier-abort simulation — mock claude exits non-zero after the first task; verify respawn once, then verify a second abort within 60s disables the slot.

### Manual smoke tests (remote LXC)

- Launcher relays a simple Telegram message → dispatched warm → answered. Verify no executor behavior on the launcher host (workspace unchanged).
- Launcher presented with "please edit file X" → dispatches cold with `--allow-writes` rather than editing itself.
- Warm worker given a task that would normally need `Write` → classifier or deny blocks cleanly; warm stays alive.
- `gobrrr daemon status` shows correct model assignments.

### Explicitly not tested

- Classifier decisions themselves — Anthropic's surface, not ours.
- Model-family behavior differences — out of scope; upgrades happen automatically via aliases.

## Migration and Rollout

1. **Config migration** — `Load()` adds a default `models` block when missing. No version bump (existing `version: 1` covers additive fields). Existing installs upgrade silently on next daemon start.
2. **Launcher settings file** — install script generates `~/.gobrrr/launcher-settings.json` if missing. Idempotent.
3. **Warm worker settings file** — daemon generates `~/.gobrrr/workers/warm-settings.json` on startup if missing. Idempotent.
4. **Launcher instruction updates** — separate deliverable on the remote `claude-agent` server (see "Launcher instructions" above). Absorbs the existing TODO.md item.
5. **Rollout order:**
   1. Daemon changes land and tested locally.
   2. Deploy daemon to remote; model defaults applied.
   3. Observe cold workers over a few tasks — classifier oddness visible in per-task logs.
   4. Observe warm worker crash recovery — any classifier aborts?
   5. Switch the launcher last, with CLAUDE.md/identity.md updates shipped at the same time as the `launcher.sh` flag changes.
6. **Rollback** — revert `config.json` `models` block to empty and restart. Since the new defaults *are* the new behavior, true rollback also requires reverting the binary. Keep the previous `gobrrr` binary on the server during deploy for one-command rollback.
7. **No feature flag** — role-based config is the feature flag. Anyone wanting the old behavior can edit config, but the defaults are the recommended setup.

## Files Changed

| File | Change |
|---|---|
| `internal/config/config.go` | New `Models` struct and field, defaults, validation |
| `internal/config/config_test.go` | Tests for defaults and validation |
| `internal/daemon/worker.go` | Cold worker uses `--model` / `--permission-mode` from config |
| `internal/daemon/worker_test.go` | Assertions for cold-worker args |
| `internal/daemon/warm_worker.go` | Warm worker uses `--model` / `--permission-mode`, stderr redirect, anti-flap guard |
| `internal/daemon/warm_worker_test.go` | Assertions for warm-worker args, anti-flap test |
| `internal/daemon/daemon.go` | Health endpoint exposes `models` |
| `internal/security/permissions.go` | One-line comment on auto-mode broad-wildcard interaction |
| `scripts/launcher.sh` | Read model + mode from config; drop `--dangerously-skip-permissions`; pass `--settings` |
| `scripts/install.sh` | Generate `~/.gobrrr/launcher-settings.json` on install |
| New: `~/.gobrrr/launcher-settings.json` | Allow-list for launcher tools |
| New: `~/.gobrrr/workers/warm-settings.json` | Allow-list for warm workers |

## Constraints

- Pure Go, no cgo (`CGO_ENABLED=0`).
- Claude Max subscription auth only (no API keys).
- Auto mode requires Sonnet 4.6+ / Opus 4.6+ / 4.7 — haiku is not supported in auto mode, which is why the launcher uses `default` mode.
- In `-p` mode, auto mode aborts the session after 3 consecutive classifier blocks or 20 total. Warm-worker allow-lists are designed to minimize classifier invocations; the anti-flapping guard catches remaining aborts.
- Per-task model override is explicitly out of scope. Can be layered on later if evidence supports it.

## References

- Chen, *Expensive Model in Wrong Position: Pipeline Optimization*, ai-coding.wiselychen.com. Planner-role failure mode: `Opus(Planner) + Solver = 31.71%` vs `Ministral 8B(Planner) + Opus(Solver) = 74.27%`; `role2_never_called` signature when capable models occupy the planner role.
- Hua et al., *AgentOpt v0.1: Client-Side Optimization for LLM-Based Agents* (arXiv:2604.06296). Cost gap between best and worst model-role combinations reaches 13-32× at matched accuracy.
- Claude Code docs — [permission modes](https://code.claude.com/docs/en/permission-modes.md), [permissions](https://code.claude.com/docs/en/permissions.md), [CLI reference](https://code.claude.com/docs/en/cli.md).
