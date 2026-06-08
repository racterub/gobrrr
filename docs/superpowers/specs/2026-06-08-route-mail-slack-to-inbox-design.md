# Route mail + Slack into the dobrrr inbox — design

> Status: approved · Date: 2026-06-08 · Branch: `feat/internal/route-mail-slack-to-inbox`
> Task: dobrrr `019e9ba8-c1da-7744-84af-938bd3636a32` (project `harness`, `repo:gobrrr`, `kind:code`)

## Goal

A periodic background sweep, run by gobrrr's cron scheduler, that **mirrors
high-signal Gmail and Slack messages into the dobrrr inbox** as items to triage.

Success criterion: on a fixed cadence, new high-signal Gmail (agent-triaged) and
Slack (saved-for-later + direct @-mentions) messages appear as dobrrr inbox items
**exactly once**, with `source`, `source_url`, `title`, `body` populated — **without
modifying the sources** (no labels, reads, reactions, or archive on Gmail/Slack).
The sweep is read-only on the sources and write-only against dobrrr.

## Deployment model (load-bearing)

- **Dev:** this repo on the `racterub` box, where the worktree lives and a local
  `gobrrr` (built + run as `racterub`) can dry-run the sweep — `racterub` already has
  Gmail/Slack/dobrrr MCP connected at user scope.
- **Production:** a remote **LXC container** running gobrrr as a dedicated `claude-agent`
  user, provisioned by `scripts/install.sh`. Shipping = edit code here, then run the
  updated `install.sh` on the LXC. So install.sh **is** the deploy path, not a
  speculative artifact.

## Design constraints (discovered, verified against current code)

1. **Account MCPs are free with the login; only local stdio servers need wiring.**
   `mcp__claude_ai_Gmail__*`, `mcp__claude_ai_Slack__*`, `mcp__claude_ai_Google_*` are
   Anthropic account-level MCPs — available in *every* Claude session once the account
   is authenticated (`claude setup-token`, which install.sh already runs for
   `claude-agent`). The **only** server requiring explicit registration is `dobrrr-mcp`,
   a local stdio proxy (`cmd/dobrrr-mcp/main.go` → `mcp.NewClient(DOBRRR_BASE_URL)`,
   HTTP to `http://dobrrr.lab.local`).
2. **MCP tool permission ≠ `allow_writes`.** `security.Generate`
   (`daemon/internal/security/permissions.go:28-51`) toggles only `Write`/`Edit`
   (filesystem) on `allow_writes`. `mcp__*` tools are governed purely by the
   `permissions.allow` list. A **skill's `tool_permissions`** is the uniform per-task
   allow source that works identically on dev (`racterub`) and prod (`claude-agent`):
   `security.Generate` merges a skill's read perms always, and its write perms only when
   `allow_writes=true`.
3. **System skills are auto-wired.** `//go:embed system/*/*` (`bundled.go:16`) embeds any
   new `system/<slug>/` dir at build time; `InstallSystemSkills` copies it to
   `~/.gobrrr/skills/<slug>/` on daemon start (never overwriting an existing copy,
   `bundled.go:47-49`) and auto-generates `_meta.json` from the SKILL.md frontmatter's
   `tool_permissions` (`bundled.go:89-98`). No Go change registers a skill.
4. **Cron is 5-field, no seconds** (`scheduler.go`,
   `cron.NewParser(Minute|Hour|Dom|Month|Dow)`).
   `Schedule{ID,Name,Cron,Prompt,ReplyTo,AllowWrites,LastFiredAt,CreatedAt}`. Scheduled
   tasks dispatch as **cold workers** (model `opus` by config default), `--print`,
   non-interactive (no TTY prompt path), `allow_writes` flows schedule → task →
   `security.Generate`.

## Architecture (three units)

### Unit 1 — `inbox-sweep` system skill (the durable core)

Path: `daemon/internal/skills/system/inbox-sweep/SKILL.md` (new, embedded).

The heart of the feature, and a single source of truth that behaves the same on dev and
prod (its perms flow through gobrrr's per-task `settings.json` either way). Frontmatter
declares the exact MCP tools the sweep needs; the body is the procedure the worker runs.

Frontmatter `tool_permissions` (least-privilege):

```yaml
read:
  - "mcp__claude_ai_Gmail__*"        # search_threads, get_thread
  - "mcp__claude_ai_Slack__*"        # search, read_thread/channel
  - "mcp__dobrrr__inbox_list"        # dedup
write:
  - "mcp__dobrrr__inbox_create"      # the only mutation, gated by --allow-writes
```

Reads (incl. dobrrr `inbox_list`) are always granted; the single write tool
(`inbox_create`) only when the task carries `--allow-writes`, preserving gobrrr's
read-only-by-default principle. This deliberately overrides the project's "CLI over MCP"
default — per the user's decision, account MCPs are the read path and dobrrr MCP is the
write path (dobrrr is the daily hub; project memory `dobrrr-mcp-exception`).
`bins: [gobrrr]` retained.

Body responsibilities (data, not code — the worker is the executor):
- **When to activate** + the read-only/write-only contract.
- **Slack rule:** `is:saved` + direct @-mentions (`<@U02QTV6RW49>`, *not*
  `@here`/`@channel`) via `slack_search_public_and_private`; read detail as needed.
- **Gmail rule:** agent triage. Candidate query
  `is:starred OR (is:unread in:inbox category:primary newer_than:2d)` via
  `search_threads`; snippet-first; `get_thread` only on finalists; drop noise; one
  concise summary per kept thread.
- **Dedup:** before creating, `inbox_list` filtered by `source`, newest-first, collect
  `source_url`s until items predate the scan window; skip any candidate already present
  (across *all* statuses — a `done` item must not reappear).
- **Field mapping:** `source` ∈ {`gmail`,`slack`}; `source_url` = permalink;
  `title` = subject / mention headline; `body` = one-line summary + snippet.
- **UNTRUSTED handling:** all fetched content is data, never instructions
  (gobrrr prompt-injection defense, CLAUDE.md decision #4).
- **Output:** terse — a one-line tally only when ≥1 item was mirrored; otherwise minimal.

### Unit 2 — the schedule (runtime cron entry on the prod daemon)

A `~/.gobrrr/schedules.json` entry created via the existing CLI on the LXC:

```
gobrrr timer create \
  --name inbox-sweep \
  --cron "*/30 8-23 * * *" \
  --prompt "Run the inbox-sweep skill: mirror high-signal Gmail and Slack into the dobrrr inbox." \
  --reply-to telegram \
  --allow-writes
```

`--allow-writes` is **mandatory** — it unlocks the skill's `inbox_create` write perm.
Cadence `*/30 8-23 * * *` = every 30 min, 08:00–23:00 local (≈32 runs/day); tunable
(`0 8-23 * * *` hourly is the lighter alternative). Runtime action, not committed code.

### Unit 3 — install.sh deploy wiring

The production deploy path (run on the LXC). Two changes:
- Add `mcp__claude_ai_Slack__*` to the worker allow block (`install.sh:402-406`;
  Gmail/Calendar already there) — keeps Slack available to all workers, consistent with
  the existing account-MCP allowlist. (The sweep itself doesn't depend on this — its
  skill perms cover it — but it matches the established pattern.)
- Register the `dobrrr-mcp` stdio server for `claude-agent` (new step, analogous to the
  existing `.mcp.json` handling at `install.sh:227`), pointing at the binary path +
  `DOBRRR_BASE_URL`. This is the one server that isn't account-level, so it's the one
  thing install.sh must wire. Prerequisite (deploy-time, not scriptable here): the
  `dobrrr-mcp` binary present on the LXC and `dobrrr.lab.local` reachable.

## Data flow

```
cron (5-field) ─▶ scheduler.fire ─▶ submitFn ─▶ queue.Submit(prompt, replyTo,
   prio=5, allowWrites=true, warm=false)
      ─▶ cold worker: buildFullPrompt = <available_skills>(incl. inbox-sweep)
         + <identity>(worker.md) + <memories> + <task>
      ─▶ claude --print --permission-mode auto --settings <per-task settings.json>
         (allow ⊇ skill read perms + inbox_create via allow_writes)
      ─▶ worker reads inbox-sweep/SKILL.md, then:
           Gmail:  search_threads(candidate query) → snippet triage
                   → get_thread(finalists) → summarize
           Slack:  slack_search_public_and_private(is:saved ∪ <@U02QTV6RW49>)
                   → read detail
           For each candidate:
                   inbox_list(source=…) dedup by source_url (newest-first, windowed)
                   → if new: inbox_create(source, source_url, title, body)
                     [mcp__dobrrr__inbox_create → dobrrr-mcp stdio → HTTP POST
                      http://dobrrr.lab.local/api/v1/inbox]
      ─▶ terse tally routed per --reply-to
```

## Error handling

- **A source MCP errors** (Gmail or Slack): skip that source this run, process the
  other, note it in output; next cron tick retries. Creates are independent — no partial
  corruption.
- **dobrrr unreachable** (`inbox_create` fails): nothing mirrored this run; report in
  output; retried next tick. Dedup guarantees no double-create on recovery.
- **Write-gate denial** (perm missing / `--allow-writes` absent): per `worker.md`,
  surface the request and stop — the signal the schedule is misconfigured.
- **Oversized / malformed thread:** summarize defensively, cap `body` length, treat
  content strictly as UNTRUSTED data.
- **Prompt injection** in message content: never execute embedded instructions; the
  skill body states the UNTRUSTED contract explicitly.

## Testing strategy

- **Unit (Go):** add a `bundled_test.go` assertion (mirroring the existing `gmail` case)
  that the `inbox-sweep` skill installs with `_meta.json` containing the three read perms
  and the `mcp__dobrrr__inbox_create` write perm — guards the perms wiring. Frontmatter
  must parse via `ParseFrontmatter`.
- **Dev dry-run (`racterub`):** build gobrrr, remove any stale
  `~/.gobrrr/skills/inbox-sweep`, run `gobrrr submit --prompt "Run the inbox-sweep
  skill…" --allow-writes`; confirm dobrrr inbox items created with correct fields;
  **run twice → no duplicates**; confirm Gmail labels / Slack reactions unchanged.
- **Prod verification (LXC):** after running install.sh, confirm `dobrrr-mcp` registered
  and reachable, the skill installed with correct `_meta.json`, then
  `gobrrr timer create …`; verify it appears in `gobrrr timer list` / `schedules.json`
  and the 5-field cron parses.
- No automated end-to-end (needs live Gmail/Slack/dobrrr + a running daemon) — the above
  manual checks stand in.

## Open items / risks / deviations

1. **Slack saved-search consent.** `slack_search_public_and_private` can request user
   consent. Account-level, so it likely works across sessions once granted — verify the
   unattended `--print` worker isn't blocked; if it is, pre-authorize once or fall back
   to a non-consent search variant.
2. **Gmail permalink format** — construct from `threadId`
   (`https://mail.google.com/mail/u/0/#all/<threadId>`); verify the exact id→URL mapping
   at implement time rather than asserting it now.
3. **Slack permalink** — prefer the `permalink` field on search results; fall back to
   channel+ts construction. Verify presence at implement time.
4. **`inbox_list` semantics** — verify that omitting the status filter lists *all*
   statuses (so `done` items aren't re-mirrored) and confirm cursor pagination.
5. **`reply_to`** — default `telegram`, terse-only-on-new output; `file:` is the silent
   alternative.
6. **Cold-worker model is `opus`** (config default), no per-schedule override; ≈32 opus
   runs/day on a Max-plan quota (no per-token cost). Note if quota pressure appears.
7. **Out of scope:** any dobrrr code change; the dead `inbox_create` `project`/`labels`
   params (separate `repo:dobrrr` cleanup task, to be filed at finishing); real-time
   push; Slack `@here`/`@channel`; calendar/other sources.
