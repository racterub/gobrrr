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
