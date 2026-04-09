package identity_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/racterub/gobrrr/internal/identity"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadIdentity(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "identity.md"), []byte("# Test Identity"), 0644)
	id, err := identity.Load(dir)
	require.NoError(t, err)
	assert.Equal(t, "# Test Identity", id)
}

func TestLoadIdentityMissing(t *testing.T) {
	dir := t.TempDir()
	// No identity.md exists — installer is expected to provide it.
	_, err := identity.Load(dir)
	require.Error(t, err)
}

func TestBuildPrompt(t *testing.T) {
	prompt := identity.BuildPrompt("# Identity", []string{"mem1", "mem2"}, "Do the task")
	assert.Contains(t, prompt, "<identity>")
	assert.Contains(t, prompt, "# Identity")
	assert.Contains(t, prompt, "<memories>")
	assert.Contains(t, prompt, "mem1")
	assert.Contains(t, prompt, "Do the task")
}

func TestBuildPromptNoMemories(t *testing.T) {
	prompt := identity.BuildPrompt("# Identity", nil, "Do the task")
	assert.NotContains(t, prompt, "<memories>")
	assert.Contains(t, prompt, "<identity>")
	assert.Contains(t, prompt, "Do the task")
}
