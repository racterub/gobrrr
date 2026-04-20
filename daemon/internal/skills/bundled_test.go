package skills

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInstallSystemSkills_CopiesEmbeddedSkills(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, InstallSystemSkills(root))

	// gmail is one of the embedded system skills.
	gmailDir := filepath.Join(root, "gmail")
	info, err := os.Stat(filepath.Join(gmailDir, "SKILL.md"))
	require.NoError(t, err)
	assert.False(t, info.IsDir())

	// _meta.json auto-generated.
	metaBytes, err := os.ReadFile(filepath.Join(gmailDir, "_meta.json"))
	require.NoError(t, err)
	var meta Meta
	require.NoError(t, json.Unmarshal(metaBytes, &meta))
	assert.Equal(t, "gmail", meta.Slug)
	assert.Contains(t, meta.ApprovedReadPermissions, "Bash(gobrrr gmail list:*)")
	assert.Contains(t, meta.ApprovedWritePermissions, "Bash(gobrrr gmail send:*)")
}

func TestInstallSystemSkills_Idempotent(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, InstallSystemSkills(root))

	// User-edit the gmail SKILL.md.
	gmailMD := filepath.Join(root, "gmail", "SKILL.md")
	require.NoError(t, os.WriteFile(gmailMD, []byte("# USER EDITED\n"), 0600))

	// Second install should not overwrite.
	require.NoError(t, InstallSystemSkills(root))
	got, err := os.ReadFile(gmailMD)
	require.NoError(t, err)
	assert.Equal(t, "# USER EDITED\n", string(got))
}
