package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/racterub/gobrrr/internal/atomicfs"
)

// ApprovalRequest is the persistent record of a pending user approval.
// Payload is kind-specific JSON — each registered handler unmarshals it.
type ApprovalRequest struct {
	ID        string          `json:"id"`
	Kind      string          `json:"kind"`
	Title     string          `json:"title"`
	Body      string          `json:"body"`
	Actions   []string        `json:"actions"`
	Payload   json.RawMessage `json:"payload"`
	CreatedAt time.Time       `json:"created_at"`
	ExpiresAt time.Time       `json:"expires_at"`
}

// ApprovalStore persists ApprovalRequest records under <root>/_approvals/<id>.json.
type ApprovalStore struct {
	root string
}

func NewApprovalStore(root string) *ApprovalStore {
	return &ApprovalStore{root: root}
}

func (s *ApprovalStore) dir() string {
	return filepath.Join(s.root, "_approvals")
}

func (s *ApprovalStore) path(id string) string {
	return filepath.Join(s.dir(), id+".json")
}

// Save writes an approval atomically (.tmp + rename) with mode 0600.
func (s *ApprovalStore) Save(req *ApprovalRequest) error {
	if err := os.MkdirAll(s.dir(), 0700); err != nil {
		return err
	}
	return atomicfs.WriteJSON(s.path(req.ID), req, 0600)
}

func (s *ApprovalStore) Load(id string) (*ApprovalRequest, error) {
	data, err := os.ReadFile(s.path(id))
	if err != nil {
		return nil, err
	}
	var req ApprovalRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, err
	}
	return &req, nil
}

func (s *ApprovalStore) Delete(id string) error {
	return os.Remove(s.path(id))
}

// List returns all currently-persisted approvals in unspecified order.
func (s *ApprovalStore) List() ([]*ApprovalRequest, error) {
	entries, err := os.ReadDir(s.dir())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []*ApprovalRequest
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".json")
		req, err := s.Load(id)
		if err != nil {
			continue
		}
		out = append(out, req)
	}
	return out, nil
}
