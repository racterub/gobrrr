package daemon

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// TaskResultEvent is the SSE payload for a completed task.
type TaskResultEvent struct {
	TaskID        string    `json:"task_id"`
	Status        string    `json:"status"`
	PromptSummary string    `json:"prompt_summary"`
	Result        string    `json:"result"`
	Error         string    `json:"error"`
	SubmittedAt   time.Time `json:"submitted_at"`
}

const sseBufferSize = 64

// SSEHub manages fan-out of task result events to connected SSE clients.
type SSEHub struct {
	mu      sync.Mutex
	clients map[chan TaskResultEvent]struct{}
}

func NewSSEHub() *SSEHub {
	return &SSEHub{
		clients: make(map[chan TaskResultEvent]struct{}),
	}
}

func (h *SSEHub) Subscribe() chan TaskResultEvent {
	h.mu.Lock()
	defer h.mu.Unlock()
	ch := make(chan TaskResultEvent, sseBufferSize)
	h.clients[ch] = struct{}{}
	return ch
}

func (h *SSEHub) Unsubscribe(ch chan TaskResultEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.clients, ch)
	close(ch)
}

func (h *SSEHub) ClientCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.clients)
}

// Emit sends an event to all clients. Non-blocking — drops if a client's buffer is full.
func (h *SSEHub) Emit(event TaskResultEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.clients {
		select {
		case ch <- event:
		default:
			// Client too slow, drop event
		}
	}
}

// TruncateRunes returns the first n runes of s.
func TruncateRunes(s string, n int) string {
	count := 0
	for i := range s {
		if count >= n {
			return s[:i]
		}
		count++
	}
	return s
}

// BuildTaskResultEvent creates a TaskResultEvent from a completed task.
func BuildTaskResultEvent(task *Task, result string) TaskResultEvent {
	errStr := ""
	if task.Error != nil {
		errStr = *task.Error
	}
	return TaskResultEvent{
		TaskID:        task.ID,
		Status:        task.Status,
		PromptSummary: TruncateRunes(task.Prompt, 100),
		Result:        result,
		Error:         errStr,
		SubmittedAt:   task.CreatedAt,
	}
}

// ServeSSE handles the GET /tasks/results/stream endpoint.
func (h *SSEHub) ServeSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch := h.Subscribe()
	defer h.Unsubscribe(ch)

	fmt.Fprintf(w, ": connected\n\n")
	flusher.Flush()

	for {
		select {
		case event, ok := <-ch:
			if !ok {
				return
			}
			data, err := json.Marshal(event)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}
