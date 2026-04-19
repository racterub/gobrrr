package skills

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadAll_ReturnsInstalledSkills(t *testing.T) {
	root := t.TempDir()

	// Layout two skills and one pending (must be skipped).
	writeSkill(t, root, "gmail", "system",
		[]string{"Bash(gobrrr gmail list:*)"}, []string{"Bash(gobrrr gmail send:*)"})
	writeSkill(t, root, "github", "clawhub",
		[]string{"Bash(gh issue list:*)"}, []string{"Bash(gh pr create:*)"})
	require.NoError(t, os.MkdirAll(filepath.Join(root, "_pending", "draft"), 0700))

	skills, err := LoadAll(root)
	require.NoError(t, err)
	assert.Len(t, skills, 2)

	bySlug := map[string]Skill{}
	for _, s := range skills {
		bySlug[s.Slug] = s
	}
	gmail := bySlug["gmail"]
	assert.Equal(t, TypeSystem, gmail.Type)
	assert.Equal(t, []string{"Bash(gobrrr gmail list:*)"}, gmail.ReadPermissions)
	assert.Equal(t, []string{"Bash(gobrrr gmail send:*)"}, gmail.WritePermissions)
	assert.Equal(t, "Email read/send/reply via gobrrr CLI", gmail.Description)
	assert.Equal(t, filepath.Join(root, "gmail", "SKILL.md"), gmail.Path)
}

func TestLoadAll_SkipsSkillWithNoMeta(t *testing.T) {
	root := t.TempDir()
	slug := "orphan"
	dir := filepath.Join(root, slug)
	require.NoError(t, os.MkdirAll(dir, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "SKILL.md"),
		[]byte("---\nname: orphan\ndescription: no meta\nmetadata:\n  gobrrr:\n    type: clawhub\n---\n\nbody\n"), 0600))

	skills, err := LoadAll(root)
	require.NoError(t, err)
	assert.Empty(t, skills)
}

func TestLoadAll_MissingRootIsNotAnError(t *testing.T) {
	skills, err := LoadAll(filepath.Join(t.TempDir(), "does-not-exist"))
	require.NoError(t, err)
	assert.Empty(t, skills)
}

// Test helper: write a skill directory with SKILL.md + _meta.json.
func writeSkill(t *testing.T, root, slug, skillType string, read, write []string) {
	t.Helper()
	dir := filepath.Join(root, slug)
	require.NoError(t, os.MkdirAll(dir, 0700))

	// SKILL.md with frontmatter
	desc := map[string]string{
		"gmail":  "Email read/send/reply via gobrrr CLI",
		"github": "GitHub issue/PR ops via gh CLI",
	}[slug]
	skillMD := "---\nname: " + slug +
		"\ndescription: " + desc +
		"\nmetadata:\n  gobrrr:\n    type: " + skillType +
		"\n---\n\nbody\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(skillMD), 0600))

	// _meta.json
	meta := map[string]any{
		"slug":                       slug,
		"version":                    "1.0.0",
		"installed_at":               "2026-04-19T16:45:00Z",
		"fingerprint":                "sha256:fake",
		"approved_read_permissions":  read,
		"approved_write_permissions": write,
	}
	b, _ := json.MarshalIndent(meta, "", "  ")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "_meta.json"), b, 0600))
}
