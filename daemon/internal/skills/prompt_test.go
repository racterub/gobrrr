package skills

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildPromptBlock_TwoSkills(t *testing.T) {
	home := "/home/racterub"
	skills := []Skill{
		{Slug: "github", Description: "GitHub ops", Path: home + "/.gobrrr/skills/github/SKILL.md"},
		{Slug: "gmail", Description: "Email ops", Path: home + "/.gobrrr/skills/gmail/SKILL.md"},
	}
	out := BuildPromptBlock(skills, home)

	assert.Contains(t, out, "<available_skills>")
	assert.Contains(t, out, "</available_skills>")
	// Alphabetical order: github before gmail
	idxGH := strings.Index(out, `name="github"`)
	idxGM := strings.Index(out, `name="gmail"`)
	require.Positive(t, idxGH)
	require.Positive(t, idxGM)
	assert.Less(t, idxGH, idxGM)
	// Tilde compaction
	assert.Contains(t, out, `location="~/.gobrrr/skills/github/SKILL.md"`)
	assert.Contains(t, out, `location="~/.gobrrr/skills/gmail/SKILL.md"`)
	// Descriptions present
	assert.Contains(t, out, "GitHub ops")
	assert.Contains(t, out, "Email ops")
}

func TestBuildPromptBlock_Empty(t *testing.T) {
	out := BuildPromptBlock(nil, "/home/u")
	assert.Equal(t, "", out)
}

func TestBuildPromptBlock_EscapesXML(t *testing.T) {
	skills := []Skill{
		{Slug: "bad", Description: "has <brackets> & ampersands", Path: "/home/u/.gobrrr/skills/bad/SKILL.md"},
	}
	out := BuildPromptBlock(skills, "/home/u")
	assert.Contains(t, out, "&lt;brackets&gt;")
	assert.Contains(t, out, "&amp;")
}
