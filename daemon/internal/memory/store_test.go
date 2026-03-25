package memory

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func tmpDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "memory-test-*")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

func TestSaveAndGet(t *testing.T) {
	s := NewStore(tmpDir(t))

	entry, err := s.Save("hello world", []string{"tag1", "tag2"}, "test")
	require.NoError(t, err)
	assert.NotEmpty(t, entry.ID)
	assert.Equal(t, "hello world", entry.Content)
	assert.Equal(t, []string{"tag1", "tag2"}, entry.Tags)
	assert.Equal(t, "test", entry.Source)
	assert.False(t, entry.CreatedAt.IsZero())

	got, err := s.Get(entry.ID)
	require.NoError(t, err)
	assert.Equal(t, entry.ID, got.ID)
	assert.Equal(t, entry.Content, got.Content)
	assert.Equal(t, entry.Tags, got.Tags)
	assert.Equal(t, entry.Source, got.Source)
}

func TestGetNotFound(t *testing.T) {
	s := NewStore(tmpDir(t))
	_, err := s.Get("m_999_000000")
	assert.Error(t, err)
}

func TestSearchByText(t *testing.T) {
	s := NewStore(tmpDir(t))

	_, err := s.Save("the quick brown fox", nil, "src")
	require.NoError(t, err)
	_, err = s.Save("unrelated content here", nil, "src")
	require.NoError(t, err)

	results, err := s.Search("quick", nil, 0)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "the quick brown fox", results[0].Content)
}

func TestSearchByTag(t *testing.T) {
	s := NewStore(tmpDir(t))

	_, err := s.Save("entry one", []string{"golang", "memory"}, "src")
	require.NoError(t, err)
	_, err = s.Save("entry two", []string{"python"}, "src")
	require.NoError(t, err)

	results, err := s.Search("", []string{"golang"}, 0)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "entry one", results[0].Content)
}

func TestSearchByTextAndTag(t *testing.T) {
	s := NewStore(tmpDir(t))

	_, err := s.Save("go is great", []string{"golang"}, "src")
	require.NoError(t, err)
	_, err = s.Save("go is also used for other things", []string{"general"}, "src")
	require.NoError(t, err)

	results, err := s.Search("great", []string{"golang"}, 0)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "go is great", results[0].Content)
}

func TestListReturnsMostRecentFirst(t *testing.T) {
	s := NewStore(tmpDir(t))

	// Save 3 entries with slight delays so timestamps differ.
	for _, content := range []string{"first", "second", "third"} {
		_, err := s.Save(content, nil, "src")
		require.NoError(t, err)
		time.Sleep(2 * time.Millisecond)
	}

	results, err := s.List(0)
	require.NoError(t, err)
	require.Len(t, results, 3)
	assert.Equal(t, "third", results[0].Content)
	assert.Equal(t, "second", results[1].Content)
	assert.Equal(t, "first", results[2].Content)
}

func TestListLimit(t *testing.T) {
	s := NewStore(tmpDir(t))

	for i := 0; i < 5; i++ {
		_, err := s.Save("entry", nil, "src")
		require.NoError(t, err)
	}

	results, err := s.List(3)
	require.NoError(t, err)
	assert.Len(t, results, 3)
}

func TestDelete(t *testing.T) {
	s := NewStore(tmpDir(t))

	entry, err := s.Save("to be deleted", nil, "src")
	require.NoError(t, err)

	err = s.Delete(entry.ID)
	require.NoError(t, err)

	_, err = s.Get(entry.ID)
	assert.Error(t, err)

	all, err := s.List(0)
	require.NoError(t, err)
	for _, e := range all {
		assert.NotEqual(t, entry.ID, e.ID)
	}
}

func TestDeleteNonExistent(t *testing.T) {
	s := NewStore(tmpDir(t))
	// Should not error when file does not exist.
	err := s.Delete("m_999_000000")
	assert.NoError(t, err)
}

func TestIndexPersistence(t *testing.T) {
	dir := tmpDir(t)
	s1 := NewStore(dir)

	entry, err := s1.Save("persisted content", []string{"persistent"}, "src")
	require.NoError(t, err)

	// Create a new store from the same dir — it should reload the index.
	s2 := NewStore(dir)
	got, err := s2.Get(entry.ID)
	require.NoError(t, err)
	assert.Equal(t, entry.Content, got.Content)
	assert.Equal(t, entry.Tags, got.Tags)
}

func TestSearchCaseInsensitive(t *testing.T) {
	s := NewStore(tmpDir(t))
	_, err := s.Save("The Quick Brown Fox", nil, "src")
	require.NoError(t, err)

	results, err := s.Search("QUICK", nil, 0)
	require.NoError(t, err)
	assert.Len(t, results, 1)
}
