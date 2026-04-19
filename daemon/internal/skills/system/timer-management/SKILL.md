---
name: timer-management
description: Scheduled task management (cron-like recurring dispatches)
metadata:
  gobrrr:
    type: system
  openclaw:
    requires:
      bins: [gobrrr]
      tool_permissions:
        read:
          - "Bash(gobrrr schedule list:*)"
          - "Bash(gobrrr schedule show:*)"
        write:
          - "Bash(gobrrr schedule add:*)"
          - "Bash(gobrrr schedule delete:*)"
          - "Bash(gobrrr schedule enable:*)"
          - "Bash(gobrrr schedule disable:*)"
---

# Timer Management Skill

## When to Activate

When the user asks to:
- Schedule a recurring task ("remind me every...", "check X every hour")
- List scheduled tasks ("what's scheduled?", "show my timers")
- Remove a scheduled task ("stop the morning briefing", "cancel X")

## Instructions

### Creating a Timer

```bash
gobrrr timer create \
  --name "descriptive-name" \
  --cron "CRON_EXPRESSION" \
  --prompt "What Claude should do when this fires"
```

**Cron format** (standard 5-field):
- Daily at 8am: `0 8 * * *`
- Every 30 minutes: `*/30 * * * *`
- Every 2 hours: `0 */2 * * *`
- Weekdays at 9am: `0 9 * * 1-5`
- Every Sunday at 4am: `0 4 * * 0`

**Options:**
- `--reply-to`: Where to send results (default: channel; options: telegram, channel, or comma-separated)
- `--allow-writes`: Enable write operations for this task

**Prompt guidelines:**
- Be specific about what to check and how to format output
- Include output format expectations
- Keep prompts under 500 chars for reliability

### Listing Timers

```bash
gobrrr timer list
```

### Removing a Timer

```bash
gobrrr timer remove --name "timer-name"
```

Confirm removal with the user before executing.
