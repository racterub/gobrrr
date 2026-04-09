// Package access manages ~/.claude/channels/telegram/access.json — shared
// with the /telegram:access and /telegram:configure skills.
package access

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

type DMPolicy string

const (
	DMPairing   DMPolicy = "pairing"
	DMAllowlist DMPolicy = "allowlist"
	DMDisabled  DMPolicy = "disabled"
)

type GroupPolicy struct {
	RequireMention bool     `json:"requireMention"`
	AllowFrom      []string `json:"allowFrom"`
}

type Pending struct {
	SenderID  string `json:"senderId"`
	ChatID    string `json:"chatId"`
	CreatedAt int64  `json:"createdAt"`
	ExpiresAt int64  `json:"expiresAt"`
	Replies   int    `json:"replies"`
}

type Access struct {
	DMPolicy        DMPolicy               `json:"dmPolicy"`
	AllowFrom       []string               `json:"allowFrom"`
	Groups          map[string]GroupPolicy `json:"groups"`
	Pending         map[string]Pending     `json:"pending"`
	MentionPatterns []string               `json:"mentionPatterns,omitempty"`
	ReplyToMode     string                 `json:"replyToMode,omitempty"` // off|first|all
	TextChunkLimit  int                    `json:"textChunkLimit,omitempty"`
	ChunkMode       string                 `json:"chunkMode,omitempty"` // length|newline
}

func Default() Access {
	return Access{
		DMPolicy:  DMPairing,
		AllowFrom: []string{},
		Groups:    map[string]GroupPolicy{},
		Pending:   map[string]Pending{},
	}
}

// Store reads and writes access.json atomically. Static=true snapshots at
// first Load and turns Save into a no-op.
type Store struct {
	dir      string
	static   bool
	snapshot *Access
}

func NewStore(dir string, static bool) *Store {
	return &Store{dir: dir, static: static}
}

func (s *Store) path() string { return filepath.Join(s.dir, "access.json") }

func (s *Store) Load() (Access, error) {
	if s.static && s.snapshot != nil {
		return *s.snapshot, nil
	}
	raw, err := os.ReadFile(s.path())
	if errors.Is(err, fs.ErrNotExist) {
		a := Default()
		if s.static {
			s.snapshot = &a
		}
		return a, nil
	}
	if err != nil {
		return Access{}, err
	}
	var a Access
	if err := json.Unmarshal(raw, &a); err != nil {
		// corrupt — move aside, return fresh default (non-static only)
		if !s.static {
			aside := fmt.Sprintf("%s.corrupt-%d", s.path(), time.Now().UnixMilli())
			_ = os.Rename(s.path(), aside)
			fmt.Fprintf(os.Stderr, "gobrrr-telegram: access.json corrupt, moved to %s\n", aside)
		}
		return Default(), nil
	}
	if a.AllowFrom == nil {
		a.AllowFrom = []string{}
	}
	if a.Groups == nil {
		a.Groups = map[string]GroupPolicy{}
	}
	if a.Pending == nil {
		a.Pending = map[string]Pending{}
	}
	if a.DMPolicy == "" {
		a.DMPolicy = DMPairing
	}
	if s.static {
		if a.DMPolicy == DMPairing {
			fmt.Fprintln(os.Stderr, `gobrrr-telegram: static mode — dmPolicy "pairing" downgraded to "allowlist"`)
			a.DMPolicy = DMAllowlist
		}
		a.Pending = map[string]Pending{}
		s.snapshot = &a
	}
	return a, nil
}

func (s *Store) Save(a Access) error {
	if s.static {
		return nil
	}
	if err := os.MkdirAll(s.dir, 0700); err != nil {
		return err
	}
	buf, err := json.MarshalIndent(a, "", "  ")
	if err != nil {
		return err
	}
	buf = append(buf, '\n')
	tmp := s.path() + ".tmp"
	if err := os.WriteFile(tmp, buf, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path())
}
