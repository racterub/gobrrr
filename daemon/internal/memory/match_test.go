package memory

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func makeEntry(id, content string, tags []string) *Entry {
	return &Entry{
		ID:        id,
		Content:   content,
		Tags:      tags,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
}

func TestMatchRelevantReturnsMatches(t *testing.T) {
	entries := []*Entry{
		makeEntry("1", "Go is a compiled language", []string{"golang"}),
		makeEntry("2", "Python is interpreted", []string{"python"}),
		makeEntry("3", "Rust has borrow checker", []string{"rust"}),
	}

	results := MatchRelevant(entries, "Go compiled language", 10)
	require.NotEmpty(t, results)
	assert.Equal(t, "1", results[0].ID)
}

func TestMatchRelevantRespectsLimit(t *testing.T) {
	entries := []*Entry{
		makeEntry("1", "memory management in Go", []string{"golang"}),
		makeEntry("2", "memory layout in C", []string{"c"}),
		makeEntry("3", "memory allocator design", []string{"systems"}),
	}

	results := MatchRelevant(entries, "memory", 2)
	assert.Len(t, results, 2)
}

func TestMatchRelevantSkipsZeroScore(t *testing.T) {
	entries := []*Entry{
		makeEntry("1", "completely unrelated content", nil),
		makeEntry("2", "also irrelevant stuff here", nil),
	}

	results := MatchRelevant(entries, "golang memory", 10)
	assert.Empty(t, results)
}

func TestMatchRelevantTagMatch(t *testing.T) {
	entries := []*Entry{
		makeEntry("1", "some content", []string{"golang", "backend"}),
		makeEntry("2", "other content", []string{"python", "frontend"}),
	}

	// "golang" should match the tag of entry 1.
	results := MatchRelevant(entries, "golang", 10)
	require.Len(t, results, 1)
	assert.Equal(t, "1", results[0].ID)
}

func TestMatchRelevantEmptyPrompt(t *testing.T) {
	entries := []*Entry{
		makeEntry("1", "some content", nil),
	}
	results := MatchRelevant(entries, "", 10)
	assert.Empty(t, results)
}

func TestMatchRelevantNilEntries(t *testing.T) {
	results := MatchRelevant(nil, "query", 10)
	assert.Empty(t, results)
}

func TestMatchRelevantSortsByScore(t *testing.T) {
	entries := []*Entry{
		makeEntry("low", "memory", nil),
		makeEntry("high", "memory management and allocation patterns", nil),
	}

	results := MatchRelevant(entries, "memory management allocation", 10)
	require.Len(t, results, 2)
	assert.Equal(t, "high", results[0].ID)
}
