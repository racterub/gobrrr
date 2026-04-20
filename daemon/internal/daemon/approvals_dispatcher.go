package daemon

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ApprovalHandler is the per-kind callback invoked when the user (or the prune
// loop, with a synthesized "deny") decides a pending approval. Implementations
// are responsible for their own side-effects (commit the skill, allow the
// write action, remove staging artifacts on deny, etc.).
type ApprovalHandler interface {
	Handle(req *ApprovalRequest, decision string) error
}

// ApprovalDispatcher coordinates creation, persistence, and resolution of
// approval requests. It is process-local; all persistence goes through the
// injected ApprovalStore.
type ApprovalDispatcher struct {
	store    *ApprovalStore
	mu       sync.RWMutex
	handlers map[string]ApprovalHandler
	onCreate func(*ApprovalRequest)
	onRemove func(id, decision string)
}

func NewApprovalDispatcher(store *ApprovalStore) *ApprovalDispatcher {
	return &ApprovalDispatcher{
		store:    store,
		handlers: map[string]ApprovalHandler{},
	}
}

func (d *ApprovalDispatcher) Register(kind string, h ApprovalHandler) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.handlers[kind] = h
}

// SetCallbacks wires post-Create and post-Decide hooks. Intended for SSE
// fan-out; either may be nil.
func (d *ApprovalDispatcher) SetCallbacks(onCreate func(*ApprovalRequest), onRemove func(id, decision string)) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.onCreate = onCreate
	d.onRemove = onRemove
}

var (
	ErrApprovalNotFound    = errors.New("approval not found")
	ErrUnknownApprovalKind = errors.New("unknown approval kind")
)

// Create persists a new approval and fires the onCreate callback. Returns the
// stored request so the caller can log/echo the id.
// A zero ttl defaults to 24 hours. Negative ttl values are accepted as-is,
// which produces an ExpiresAt in the past (useful in tests to pre-expire).
func (d *ApprovalDispatcher) Create(kind, title, body string, actions []string, payload any, ttl time.Duration) (*ApprovalRequest, error) {
	if ttl == 0 {
		ttl = 24 * time.Hour
	}
	rawPayload, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	id, err := d.newUniqueID()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	req := &ApprovalRequest{
		ID:        id,
		Kind:      kind,
		Title:     title,
		Body:      body,
		Actions:   actions,
		Payload:   rawPayload,
		CreatedAt: now,
		ExpiresAt: now.Add(ttl),
	}
	if err := d.store.Save(req); err != nil {
		return nil, err
	}
	d.mu.RLock()
	cb := d.onCreate
	d.mu.RUnlock()
	if cb != nil {
		cb(req)
	}
	return req, nil
}

// Decide loads the approval, atomically deletes the persistence file (claim
// semantics — any concurrent Decide for the same id loses), then invokes the
// per-kind handler. Deletion-before-handler is intentional: a handler that
// panics can't cause the approval to be re-invoked, and a handler that returns
// an error still leaves the approval cleared (the failure is surfaced to the
// caller but the approval is considered decided).
func (d *ApprovalDispatcher) Decide(id, decision string) error {
	req, err := d.store.Load(id)
	if err != nil {
		if os.IsNotExist(err) {
			return ErrApprovalNotFound
		}
		return err
	}
	if err := d.store.Delete(id); err != nil {
		if os.IsNotExist(err) {
			return ErrApprovalNotFound
		}
		return err
	}
	d.mu.RLock()
	h, ok := d.handlers[req.Kind]
	removeCB := d.onRemove
	d.mu.RUnlock()
	if !ok {
		return fmt.Errorf("%w: %s", ErrUnknownApprovalKind, req.Kind)
	}
	handleErr := h.Handle(req, decision)
	if removeCB != nil {
		removeCB(id, decision)
	}
	return handleErr
}

// List returns all pending approvals (used for rehydration on SSE connect
// and by the prune loop).
func (d *ApprovalDispatcher) List() ([]*ApprovalRequest, error) {
	return d.store.List()
}

// newUniqueID returns a 4-hex id with a collision retry loop against the
// store directory. Matches the existing installer id shape for consistency.
func (d *ApprovalDispatcher) newUniqueID() (string, error) {
	for i := 0; i < 16; i++ {
		b := make([]byte, 2)
		if _, err := rand.Read(b); err != nil {
			return "", err
		}
		id := hex.EncodeToString(b)
		if _, err := os.Stat(filepath.Join(d.store.dir(), id+".json")); os.IsNotExist(err) {
			return id, nil
		}
	}
	return "", errors.New("could not allocate unique approval id")
}
