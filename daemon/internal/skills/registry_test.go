package skills

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegistry_RefreshAndList(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "gmail", "system", []string{"Bash(gobrrr gmail list:*)"}, nil)

	reg := NewRegistry(root)
	require.NoError(t, reg.Refresh())
	assert.Len(t, reg.List(), 1)
	assert.Equal(t, "gmail", reg.List()[0].Slug)

	// Add a second skill and refresh.
	writeSkill(t, root, "github", "clawhub", []string{"Bash(gh issue list:*)"}, nil)
	require.NoError(t, reg.Refresh())
	assert.Len(t, reg.List(), 2)

	// Remove one and refresh.
	require.NoError(t, os.RemoveAll(filepath.Join(root, "gmail")))
	require.NoError(t, reg.Refresh())
	assert.Len(t, reg.List(), 1)
	assert.Equal(t, "github", reg.List()[0].Slug)
}

func TestRegistry_EmptyRoot(t *testing.T) {
	reg := NewRegistry(filepath.Join(t.TempDir(), "nonexistent"))
	require.NoError(t, reg.Refresh())
	assert.Empty(t, reg.List())
}
