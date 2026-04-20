package skills

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseFrontmatter_SystemSkill(t *testing.T) {
	content := []byte(`---
name: gmail
description: Email read/send/reply via gobrrr CLI
metadata:
  gobrrr:
    type: system
  openclaw:
    requires:
      bins: [gobrrr]
      tool_permissions:
        read:
          - "Bash(gobrrr gmail list:*)"
        write:
          - "Bash(gobrrr gmail send:*)"
---

# Gmail Skill

body content here.
`)

	fm, body, err := ParseFrontmatter(content)
	require.NoError(t, err)
	assert.Equal(t, "gmail", fm.Name)
	assert.Equal(t, "Email read/send/reply via gobrrr CLI", fm.Description)
	assert.Equal(t, "system", fm.Metadata.Gobrrr.Type)
	assert.Equal(t, []string{"gobrrr"}, fm.Metadata.OpenClaw.Requires.Bins)
	assert.Equal(t, []string{"Bash(gobrrr gmail list:*)"}, fm.Metadata.OpenClaw.Requires.ToolPermissions.Read)
	assert.Equal(t, []string{"Bash(gobrrr gmail send:*)"}, fm.Metadata.OpenClaw.Requires.ToolPermissions.Write)
	assert.Contains(t, string(body), "# Gmail Skill")
}

func TestParseFrontmatter_MissingFrontmatter(t *testing.T) {
	content := []byte("# Just a markdown file\n\nno frontmatter here.\n")
	_, _, err := ParseFrontmatter(content)
	assert.Error(t, err)
}

func TestParseFrontmatter_MalformedYAML(t *testing.T) {
	content := []byte("---\nname: broken\n  description: wrong indent\n---\n\nbody\n")
	_, _, err := ParseFrontmatter(content)
	assert.Error(t, err)
}

func TestParseFrontmatter_FlatToolPermissions_DefaultsToRead(t *testing.T) {
	// Fallback for ClawHub skills that use a flat `tool_permissions: [...]` form.
	content := []byte(`---
name: legacy
description: legacy skill
metadata:
  gobrrr:
    type: clawhub
  openclaw:
    requires:
      tool_permissions:
        - "Bash(echo:*)"
---

body
`)
	fm, _, err := ParseFrontmatter(content)
	require.NoError(t, err)
	assert.Equal(t, []string{"Bash(echo:*)"}, fm.Metadata.OpenClaw.Requires.ToolPermissions.Read)
	assert.Empty(t, fm.Metadata.OpenClaw.Requires.ToolPermissions.Write)
}
