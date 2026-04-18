# Default Channel Routing Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `channel` the single default `--reply-to` target for all task submission paths.

**Architecture:** Two flag defaults change (submit CLI and timer create CLI), scheduler passes through whatever `ReplyTo` the schedule was created with (no change needed there). Skills docs updated to match.

**Tech Stack:** Go, cobra, testify

---

### Task 1: Change submit CLI default to `channel`

**Files:**
- Modify: `daemon/cmd/gobrrr/main.go:720`

- [ ] **Step 1: Change the flag default**

In `daemon/cmd/gobrrr/main.go`, line 720, change:

```go
submitCmd.Flags().StringVar(&submitReplyTo, "reply-to", "", "Reply destination (e.g. telegram)")
```

to:

```go
submitCmd.Flags().StringVar(&submitReplyTo, "reply-to", "channel", "Reply destination (e.g. channel, telegram, stdout)")
```

- [ ] **Step 2: Verify build**

Run: `cd daemon && CGO_ENABLED=0 go build ./cmd/gobrrr/`
Expected: clean build, no errors.

- [ ] **Step 3: Run existing tests to confirm no regressions**

Run: `cd daemon && go test ./...`
Expected: all tests pass.

- [ ] **Step 4: Commit**

```bash
git add daemon/cmd/gobrrr/main.go
git commit -m "feat: default submit --reply-to to channel

Behavioral change: submit CLI now defaults to channel routing
instead of empty string, so all dispatched tasks route results
through the main Claude session by default."
```

---

### Task 2: Change timer create CLI default to `channel`

**Files:**
- Modify: `daemon/cmd/gobrrr/main.go:794`

- [ ] **Step 1: Change the flag default**

In `daemon/cmd/gobrrr/main.go`, line 794, change:

```go
timerCreateCmd.Flags().String("reply-to", "telegram", "Result destination")
```

to:

```go
timerCreateCmd.Flags().String("reply-to", "channel", "Result destination")
```

- [ ] **Step 2: Verify build**

Run: `cd daemon && CGO_ENABLED=0 go build ./cmd/gobrrr/`
Expected: clean build, no errors.

- [ ] **Step 3: Run existing tests**

Run: `cd daemon && go test ./...`
Expected: all tests pass.

- [ ] **Step 4: Commit**

```bash
git add daemon/cmd/gobrrr/main.go
git commit -m "feat: default timer create --reply-to to channel

Behavioral change: scheduled tasks now default to channel routing
instead of telegram, consistent with submit command."
```

---

### Task 3: Update dispatch skill doc

**Files:**
- Modify: `daemon/skills/dispatch/SKILL.md`

- [ ] **Step 1: Update examples**

In `daemon/skills/dispatch/SKILL.md`, line 7, change:

```markdown
- `gobrrr submit --prompt "..." --reply-to telegram` — run in background, send result to Telegram
```

to:

```markdown
- `gobrrr submit --prompt "..."` — run in background, result routed to channel (default)
- `gobrrr submit --prompt "..." --reply-to telegram` — run in background, send result directly to Telegram
```

- [ ] **Step 2: Commit**

```bash
git add daemon/skills/dispatch/SKILL.md
git commit -m "docs: update dispatch skill to reflect channel default"
```

---

### Task 4: Update timer management skill doc

**Files:**
- Modify: `daemon/skills/timer-management/SKILL.md`

- [ ] **Step 1: Update example and options**

In `daemon/skills/timer-management/SKILL.md`, lines 14-19, change the example:

```bash
gobrrr timer create \
  --name "descriptive-name" \
  --cron "CRON_EXPRESSION" \
  --prompt "What Claude should do when this fires" \
  --reply-to telegram
```

to:

```bash
gobrrr timer create \
  --name "descriptive-name" \
  --cron "CRON_EXPRESSION" \
  --prompt "What Claude should do when this fires"
```

In lines 29-31, change:

```markdown
- `--reply-to`: Where to send results (telegram, channel, or comma-separated)
```

to:

```markdown
- `--reply-to`: Where to send results (default: channel; options: telegram, channel, or comma-separated)
```

- [ ] **Step 2: Commit**

```bash
git add daemon/skills/timer-management/SKILL.md
git commit -m "docs: update timer skill to reflect channel default"
```

---

### Task 5: Remove TODO item

**Files:**
- Modify: `TODO.md`

- [ ] **Step 1: Remove the Telegram Routing Strategy section**

Delete lines 95-112 from `TODO.md` (the entire "## Telegram Routing Strategy" section).

- [ ] **Step 2: Commit**

```bash
git add TODO.md
git commit -m "docs: remove completed Telegram Routing Strategy TODO"
```
