package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApprovalStore_SaveLoadDelete(t *testing.T) {
	root := t.TempDir()
	store := NewApprovalStore(root)

	req := &ApprovalRequest{
		ID:        "abcd",
		Kind:      "skill_install",
		Title:     "install skill foo",
		Body:      "body text",
		Actions:   []string{"approve", "deny"},
		Payload:   json.RawMessage(`{"slug":"foo"}`),
		CreatedAt: time.Now().UTC(),
		ExpiresAt: time.Now().UTC().Add(time.Hour),
	}
	require.NoError(t, store.Save(req))

	got, err := store.Load("abcd")
	require.NoError(t, err)
	assert.Equal(t, req.Kind, got.Kind)
	assert.Equal(t, req.Title, got.Title)

	info, err := os.Stat(filepath.Join(root, "_approvals", "abcd.json"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0600), info.Mode().Perm())

	require.NoError(t, store.Delete("abcd"))
	_, err = store.Load("abcd")
	assert.True(t, os.IsNotExist(err))
}

func TestApprovalStore_List(t *testing.T) {
	root := t.TempDir()
	store := NewApprovalStore(root)

	for _, id := range []string{"aaaa", "bbbb", "cccc"} {
		require.NoError(t, store.Save(&ApprovalRequest{
			ID: id, Kind: "skill_install",
			CreatedAt: time.Now().UTC(), ExpiresAt: time.Now().UTC().Add(time.Hour),
		}))
	}

	list, err := store.List()
	require.NoError(t, err)
	assert.Len(t, list, 3)
}
