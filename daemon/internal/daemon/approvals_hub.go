package daemon

import "sync"

// ApprovalEventType distinguishes the lifecycle transitions broadcast to SSE
// subscribers. Kept as a small enum so bot-side clients can route cheaply.
type ApprovalEventType string

const (
	ApprovalEventCreated ApprovalEventType = "created"
	ApprovalEventRemoved ApprovalEventType = "removed"
)

// ApprovalEvent is the SSE payload. For created events, Request carries the
// full record (bot needs it to render a card). For removed events only ID and
// Decision are set.
type ApprovalEvent struct {
	Type     ApprovalEventType `json:"type"`
	Request  *ApprovalRequest  `json:"request,omitempty"`
	ID       string            `json:"id,omitempty"`
	Decision string            `json:"decision,omitempty"`
}

const approvalBufferSize = 64

// ApprovalHub fans ApprovalEvents to connected SSE clients. Shape mirrors
// SSEHub so the same review has already vetted the pattern.
type ApprovalHub struct {
	mu      sync.Mutex
	clients map[chan ApprovalEvent]struct{}
}

func NewApprovalHub() *ApprovalHub {
	return &ApprovalHub{clients: map[chan ApprovalEvent]struct{}{}}
}

func (h *ApprovalHub) Subscribe() chan ApprovalEvent {
	h.mu.Lock()
	defer h.mu.Unlock()
	ch := make(chan ApprovalEvent, approvalBufferSize)
	h.clients[ch] = struct{}{}
	return ch
}

func (h *ApprovalHub) Unsubscribe(ch chan ApprovalEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.clients[ch]; ok {
		delete(h.clients, ch)
		close(ch)
	}
}

// Emit sends an event to all clients. Non-blocking — drops if a client's
// buffer is full. Matches SSEHub behavior.
func (h *ApprovalHub) Emit(event ApprovalEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.clients {
		select {
		case ch <- event:
		default:
		}
	}
}
