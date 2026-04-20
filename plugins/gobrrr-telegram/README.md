# gobrrr-telegram

Drop-in Go reimplementation of the official `telegram` Claude Code plugin.

## Install

```bash
./scripts/install-telegram-channel.sh
```

Then disable the official `telegram` plugin and enable `gobrrr-telegram` in
Claude Code's plugin settings.

## Launching

Channel-capable plugins must be activated explicitly on each `claude` launch
via the (hidden) development-channels flag:

```bash
claude --dangerously-load-development-channels plugin:gobrrr-telegram@<marketplace>
```

Without this flag the MCP server still connects but Claude logs
`Channel notifications skipped: server ... not in --channels list for this session`
and inbound Telegram messages never reach the conversation.

## State

Uses the same `~/.claude/channels/telegram/` directory as the official plugin.

Access management is provided by the bundled `/gobrrr-telegram:access` skill
(see `skills/access/SKILL.md`) — no need to install the official `telegram`
plugin. The skill mirrors `/telegram:access`: status, `pair <code>`,
`deny <code>`, `allow <senderId>`, `remove <senderId>`, `policy <mode>`,
`group add/rm`, `set <key> <value>`.

If the daemon is launched with `TELEGRAM_ACCESS_MODE=static`, the skill
warns that mutations only take effect on the next restart.
