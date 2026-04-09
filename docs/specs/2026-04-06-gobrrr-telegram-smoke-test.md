# gobrrr-telegram manual smoke test

Run after every non-trivial change. Requires a real Telegram bot token.

## Setup
- [ ] `~/.claude/channels/telegram/.env` has `TELEGRAM_BOT_TOKEN=...`
- [ ] `~/.gobrrr/bin/gobrrr-telegram` exists and is executable
- [ ] `gobrrr-telegram` plugin enabled in Claude Code, official `telegram` disabled

## Pairing
- [ ] DM the bot from an unknown account → receive pairing code
- [ ] Reply `y <code>` → bot says "paired ✓"
- [ ] Subsequent DMs forward to Claude as `<channel>` tags

## Delivery
- [ ] Claude sends a short message via `reply` → arrives
- [ ] Claude sends a 5000-char message → chunked into multiple messages
- [ ] Set `replyToMode=first` → only first chunk threads as a reply
- [ ] Set `chunkMode=newline`, send paragraphs → splits on blank lines

## Attachments
- [ ] Send a photo from Telegram → `inbox/` contains the file; `<channel image_path=...>` in tag
- [ ] Send a document → `inbox/` contains the file; `attachment_file_id=...` in tag
- [ ] `download_attachment` tool fetches by file_id

## Reactions
- [ ] Set `ackReaction="👍"` → inbound messages get a 👍 reaction
- [ ] `react` tool with whitelisted emoji → reaction appears
- [ ] `react` tool with non-whitelisted emoji → clean tool error (no crash)

## Edit
- [ ] `edit_message` updates previously-sent chunk

## Groups
- [ ] Add bot to a group, configure group policy with `requireMention=true` and a test user in `allowFrom`
- [ ] Non-mention message from allowed user → dropped
- [ ] `@botname` message → forwarded
- [ ] Message from user not in `allowFrom` → dropped

## Resilience
- [ ] Corrupt `access.json` → renamed to `access.json.corrupt-<ts>`, fresh default loaded
- [ ] Kill process mid-operation → `access.json` never partially written (atomic rename)
- [ ] Static mode (`TELEGRAM_ACCESS_MODE=static`) with pairing policy → stderr warns, downgrades to allowlist
