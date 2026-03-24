# Memory Skill

## When to Save
- User states a preference or makes a decision
- User provides context that should persist across sessions
- You learn something non-obvious about the user's workflow

## When NOT to Save
- Ephemeral task details
- Information derivable from other sources (code, docs, git)
- Temporary state

## Commands
- `gobrrr memory save --content "..." --tags tag1,tag2` — save a memory
- `gobrrr memory search "query"` — search by text
- `gobrrr memory search --tags preference` — search by tag
- `gobrrr memory list --limit 20` — list recent memories
- `gobrrr memory get <id>` — get specific memory
- `gobrrr memory delete <id>` — delete a memory
