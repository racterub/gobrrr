package security

import (
	"fmt"
	"sync"
	"time"
)

// Decision holds the outcome of a confirmation gate request.
type Decision struct {
	Approved bool
	Reason   string
}

// Gate is a synchronisation primitive that lets an external caller
// approve or deny a pending operation before it proceeds.
type Gate struct {
	timeout time.Duration
	pending map[string]chan Decision
	mu      sync.Mutex
}

// NewGate creates a Gate with the given timeout duration.
// When Wait is called and no Approve/Deny arrives within timeout, the gate
// returns Decision{Approved: false, Reason: "timeout"}.
func NewGate(timeout time.Duration) *Gate {
	return &Gate{
		timeout: timeout,
		pending: make(map[string]chan Decision),
	}
}

// Request registers a pending approval for taskID.
// It creates a buffered channel (size 1) so that Approve/Deny do not block.
func (g *Gate) Request(taskID string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.pending[taskID] = make(chan Decision, 1)
}

// Approve sends an approval decision for taskID.
// Returns an error if taskID is not registered.
func (g *Gate) Approve(taskID string) error {
	g.mu.Lock()
	ch, ok := g.pending[taskID]
	g.mu.Unlock()
	if !ok {
		return fmt.Errorf("no pending request for task %q", taskID)
	}
	ch <- Decision{Approved: true}
	return nil
}

// Deny sends a denial decision for taskID.
// Returns an error if taskID is not registered.
func (g *Gate) Deny(taskID string) error {
	g.mu.Lock()
	ch, ok := g.pending[taskID]
	g.mu.Unlock()
	if !ok {
		return fmt.Errorf("no pending request for task %q", taskID)
	}
	ch <- Decision{Approved: false, Reason: "denied"}
	return nil
}

// Wait blocks until taskID is approved, denied, or the gate's timeout elapses.
// On timeout it returns Decision{Approved: false, Reason: "timeout"}.
func (g *Gate) Wait(taskID string) Decision {
	g.mu.Lock()
	ch, ok := g.pending[taskID]
	g.mu.Unlock()
	if !ok {
		return Decision{Approved: false, Reason: "not found"}
	}

	timer := time.NewTimer(g.timeout)
	defer timer.Stop()

	select {
	case d := <-ch:
		return d
	case <-timer.C:
		return Decision{Approved: false, Reason: "timeout"}
	}
}

// Cleanup removes the pending entry for taskID.
// It should be called after Wait returns to release resources.
func (g *Gate) Cleanup(taskID string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.pending, taskID)
}
