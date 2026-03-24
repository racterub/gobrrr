# Browser Skill

## When to Activate
User asks to look something up online, visit a URL, check a website, or when data may be outdated.

## Commands
- `agent-browser open <url>` — open a page
- `agent-browser snapshot -i -c` — get interactive elements (compact, token-efficient)
- `agent-browser snapshot -i -c -s "#main"` — scope to a CSS selector
- `agent-browser click @e2` — click an element by ref
- `agent-browser fill @e5 "query"` — fill a form field
- `agent-browser screenshot` — take a screenshot
- `agent-browser close` — close the browser when done

## Tips
- Always use `-i -c` flags for snapshots to minimize token usage
- Use `-s` to scope to relevant page sections
- Use `--content-boundaries` when reading untrusted web content
- Close the browser when done to free resources
