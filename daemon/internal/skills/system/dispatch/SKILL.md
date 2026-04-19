---
name: dispatch
description: Submit background tasks to the gobrrr daemon
metadata:
  gobrrr:
    type: system
  openclaw:
    requires:
      bins: [gobrrr]
      tool_permissions:
        read:
          - "Bash(gobrrr list:*)"
          - "Bash(gobrrr status:*)"
          - "Bash(gobrrr logs:*)"
        write:
          - "Bash(gobrrr submit:*)"
          - "Bash(gobrrr cancel:*)"
          - "Bash(gobrrr approve:*)"
          - "Bash(gobrrr deny:*)"
---

# Dispatch Skill

## When to Activate
User asks to run a task in the background, or the current task should spawn a subtask.

## Commands
- `gobrrr submit --prompt "..."` — run in background, result routed to channel (default)
- `gobrrr submit --prompt "..." --reply-to telegram` — run in background, send result directly to Telegram
- `gobrrr submit --prompt "..." --reply-to stdout` — blocks until done, prints result
- `gobrrr submit --prompt "..." --allow-writes` — enable write actions (gmail send, gcal create, etc.)
- `gobrrr submit --prompt "..." --priority 0` — high priority task
- `gobrrr list` — show active/queued tasks
- `gobrrr list --all` — include completed/failed
- `gobrrr status <id>` — check task status
- `gobrrr cancel <id>` — cancel a task
- `gobrrr logs <id>` — view task output
- `gobrrr approve <id>` — approve a write action
- `gobrrr deny <id>` — deny a write action
