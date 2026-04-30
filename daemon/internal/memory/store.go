// Package memory provides a file-backed key-value memory store for gobrrr.
package memory

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/racterub/gobrrr/internal/atomicfs"
)

// Entry represents a single memory record.
type Entry struct {
	ID        string    `json:"id"`
	Content   string    `json:"content"`
	Tags      []string  `json:"tags"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Source    string    `json:"source"`
}

// Index is the top-level index file stored at <dir>/index.json.
type Index struct {
	Version int          `json:"version"`
	Entries []IndexEntry `json:"entries"`
}

// IndexEntry is a lightweight summary of an Entry for fast listing/searching.
type IndexEntry struct {
	ID        string    `json:"id"`
	Summary   string    `json:"summary"` // first 100 chars of content
	Tags      []string  `json:"tags"`
	CreatedAt time.Time `json:"created_at"`
}

// Store is a file-backed memory store.
type Store struct {
	dir string
	mu  sync.RWMutex
	idx *Index
}

// NewStore creates or loads a Store rooted at dir.
// The directory is created if it does not exist.
func NewStore(dir string) *Store {
	s := &Store{dir: dir, idx: &Index{Version: 1}}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return s
	}
	if idx, err := s.loadIndex(); err == nil {
		s.idx = idx
	}
	return s
}

// Save stores a new memory entry and returns it.
func (s *Store) Save(content string, tags []string, source string) (*Entry, error) {
	now := time.Now().UTC()
	entry := &Entry{
		ID:        newID(now),
		Content:   content,
		Tags:      tags,
		CreatedAt: now,
		UpdatedAt: now,
		Source:    source,
	}

	if err := s.writeEntry(entry); err != nil {
		return nil, fmt.Errorf("writing entry: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	summary := content
	if len(summary) > 100 {
		summary = summary[:100]
	}
	s.idx.Entries = append(s.idx.Entries, IndexEntry{
		ID:        entry.ID,
		Summary:   summary,
		Tags:      tags,
		CreatedAt: now,
	})
	if err := s.persistIndex(); err != nil {
		return nil, fmt.Errorf("persisting index: %w", err)
	}
	return entry, nil
}

// Get loads and returns the entry with the given ID.
func (s *Store) Get(id string) (*Entry, error) {
	return s.readEntry(id)
}

// Search returns entries whose content contains query (case-insensitive) and
// whose tags include all of the provided tags. Either query or tags may be
// empty to skip that filter. Results are sorted by CreatedAt descending.
// When limit <= 0 all matching entries are returned.
func (s *Store) Search(query string, tags []string, limit int) ([]*Entry, error) {
	s.mu.RLock()
	entries := make([]IndexEntry, len(s.idx.Entries))
	copy(entries, s.idx.Entries)
	s.mu.RUnlock()

	lowerQuery := strings.ToLower(query)

	// Sort by created_at descending so we load most-recent first.
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].CreatedAt.After(entries[j].CreatedAt)
	})

	var results []*Entry
	for _, ie := range entries {
		if query != "" && !strings.Contains(strings.ToLower(ie.Summary), lowerQuery) {
			// Summary might be truncated; we still need to check full content.
			// Load entry to check full content.
			e, err := s.readEntry(ie.ID)
			if err != nil {
				continue
			}
			if !strings.Contains(strings.ToLower(e.Content), lowerQuery) {
				continue
			}
			if !matchTags(ie.Tags, tags) {
				continue
			}
			results = append(results, e)
		} else {
			if !matchTags(ie.Tags, tags) {
				continue
			}
			if query != "" {
				// Summary matched (or no query), load the full entry.
				e, err := s.readEntry(ie.ID)
				if err != nil {
					continue
				}
				results = append(results, e)
			} else {
				// No text query — just tag filter; load entry.
				e, err := s.readEntry(ie.ID)
				if err != nil {
					continue
				}
				results = append(results, e)
			}
		}

		if limit > 0 && len(results) >= limit {
			break
		}
	}
	return results, nil
}

// List returns up to limit entries sorted by CreatedAt descending.
// When limit <= 0 all entries are returned.
func (s *Store) List(limit int) ([]*Entry, error) {
	return s.Search("", nil, limit)
}

// Delete removes the entry with the given ID from disk and from the index.
func (s *Store) Delete(id string) error {
	path := s.entryPath(id)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing entry file: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	filtered := s.idx.Entries[:0]
	for _, ie := range s.idx.Entries {
		if ie.ID != id {
			filtered = append(filtered, ie)
		}
	}
	s.idx.Entries = filtered
	return s.persistIndex()
}

// --- helpers ---

func newID(t time.Time) string {
	const hexChars = "0123456789abcdef"
	b := make([]byte, 6)
	for i := range b {
		b[i] = hexChars[rand.Intn(16)] //nolint:gosec
	}
	return fmt.Sprintf("m_%d_%s", t.Unix(), string(b))
}

func (s *Store) entryPath(id string) string {
	return filepath.Join(s.dir, id+".json")
}

func (s *Store) indexPath() string {
	return filepath.Join(s.dir, "index.json")
}

func (s *Store) writeEntry(e *Entry) error {
	data, err := json.Marshal(e)
	if err != nil {
		return err
	}
	return atomicfs.WriteFile(s.entryPath(e.ID), data, 0600)
}

func (s *Store) readEntry(id string) (*Entry, error) {
	data, err := os.ReadFile(s.entryPath(id))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("entry %q not found", id)
		}
		return nil, err
	}
	var e Entry
	if err := json.Unmarshal(data, &e); err != nil {
		return nil, fmt.Errorf("decoding entry %q: %w", id, err)
	}
	return &e, nil
}

func (s *Store) loadIndex() (*Index, error) {
	data, err := os.ReadFile(s.indexPath())
	if err != nil {
		return nil, err
	}
	var idx Index
	if err := json.Unmarshal(data, &idx); err != nil {
		return nil, err
	}
	return &idx, nil
}

// persistIndex writes the index atomically. Caller must hold s.mu (write).
func (s *Store) persistIndex() error {
	data, err := json.Marshal(s.idx)
	if err != nil {
		return err
	}
	return atomicfs.WriteFile(s.indexPath(), data, 0600)
}

// matchTags returns true if all required tags appear in entryTags.
// If required is nil or empty, it always returns true.
func matchTags(entryTags, required []string) bool {
	if len(required) == 0 {
		return true
	}
	tagSet := make(map[string]struct{}, len(entryTags))
	for _, t := range entryTags {
		tagSet[strings.ToLower(t)] = struct{}{}
	}
	for _, r := range required {
		if _, ok := tagSet[strings.ToLower(r)]; !ok {
			return false
		}
	}
	return true
}
