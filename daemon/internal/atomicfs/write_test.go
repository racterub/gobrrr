package atomicfs_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/racterub/gobrrr/internal/atomicfs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteFileRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "data.bin")
	payload := []byte("hello atomic world")

	require.NoError(t, atomicfs.WriteFile(path, payload, 0600))

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, payload, got)
}

func TestWriteFileEnforcesPerm(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secret")

	require.NoError(t, atomicfs.WriteFile(path, []byte("x"), 0600))

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0600), info.Mode().Perm())
}

func TestWriteFileNoTempLeftOnSuccess(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.json")

	require.NoError(t, atomicfs.WriteFile(path, []byte("{}"), 0600))

	_, err := os.Stat(path + ".tmp")
	assert.True(t, os.IsNotExist(err), "sibling .tmp must be gone after rename")
}

func TestWriteFileOverwritesExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.json")

	require.NoError(t, atomicfs.WriteFile(path, []byte("old"), 0600))
	require.NoError(t, atomicfs.WriteFile(path, []byte("new"), 0600))

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, []byte("new"), got)
}

func TestWriteFileFailsWhenDirMissing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "missing-subdir", "x.json")

	err := atomicfs.WriteFile(path, []byte("x"), 0600)
	assert.Error(t, err, "writing into nonexistent dir must fail; mkdir is the caller's job")
}

func TestWriteJSONShape(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.json")

	value := map[string]any{"b": 2, "a": 1}
	require.NoError(t, atomicfs.WriteJSON(path, value, 0600))

	got, err := os.ReadFile(path)
	require.NoError(t, err)

	var roundTrip map[string]any
	require.NoError(t, json.Unmarshal(got, &roundTrip))
	assert.Equal(t, float64(1), roundTrip["a"])
	assert.Equal(t, float64(2), roundTrip["b"])

	// Two-space indent contract.
	assert.Contains(t, string(got), "\n  \"a\":")
}

func TestWriteFileFsyncsParentDir(t *testing.T) {
	var (
		calls  int
		gotDir string
	)
	restore := atomicfs.SetFsyncDirForTest(func(dir string) error {
		calls++
		gotDir = dir
		return nil
	})
	defer restore()

	dir := t.TempDir()
	path := filepath.Join(dir, "x.json")

	require.NoError(t, atomicfs.WriteFile(path, []byte("x"), 0600))

	assert.Equal(t, 1, calls, "fsync must be called exactly once per WriteFile")
	assert.Equal(t, dir, gotDir, "fsync target must be the parent directory of the written file")
}

func TestWriteFileFsyncErrorPropagates(t *testing.T) {
	sentinel := errors.New("simulated fsync failure")
	restore := atomicfs.SetFsyncDirForTest(func(string) error {
		return sentinel
	})
	defer restore()

	dir := t.TempDir()
	path := filepath.Join(dir, "x.json")

	err := atomicfs.WriteFile(path, []byte("x"), 0600)
	assert.ErrorIs(t, err, sentinel, "WriteFile must surface fsync errors to the caller")
}
