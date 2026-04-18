package daemon

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Task represents a unit of work dispatched to a Claude worker.
type Task struct {
	Version     int               `json:"version"`
	ID          string            `json:"id"`
	Prompt      string            `json:"prompt"`
	Status      string            `json:"status"`
	Priority    int               `json:"priority"`
	ReplyTo     string            `json:"reply_to"`
	AllowWrites bool              `json:"allow_writes"`
	Warm        bool              `json:"warm"`
	CreatedAt   time.Time         `json:"created_at"`
	StartedAt   *time.Time        `json:"started_at"`
	CompletedAt *time.Time        `json:"completed_at"`
	Retries     int               `json:"retries"`
	MaxRetries  int               `json:"max_retries"`
	TimeoutSec  int               `json:"timeout_sec"`
	Result      *string           `json:"result"`
	Error       *string           `json:"error"`
	Delivered   bool              `json:"delivered"`
	Metadata    map[string]string `json:"metadata"`
}

// Queue is a persistent, priority-ordered task queue backed by a JSON file.
type Queue struct {
	mu    sync.Mutex
	tasks []*Task
	path  string
}

// queueData is the on-disk envelope for the queue.
type queueData struct {
	Tasks []*Task `json:"tasks"`
}

// NewQueue creates an empty Queue that will persist to path.
func NewQueue(path string) *Queue {
	return &Queue{path: path}
}

// LoadQueue reads the queue from path, performing crash recovery: any task
// whose status is "running" is reset to "queued" (the worker may have died).
func LoadQueue(path string) (*Queue, error) {
	q := &Queue{path: path}

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return q, nil
		}
		return nil, fmt.Errorf("reading queue file: %w", err)
	}

	var envelope queueData
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, fmt.Errorf("parsing queue file: %w", err)
	}

	recovered := false
	for _, t := range envelope.Tasks {
		if t.Status == "running" {
			t.Status = "queued"
			t.StartedAt = nil
			recovered = true
		}
	}

	q.tasks = envelope.Tasks

	if recovered {
		if err := q.flush(); err != nil {
			return nil, fmt.Errorf("persisting crash recovery: %w", err)
		}
	}

	return q, nil
}

// Submit adds a new task to the queue and persists the queue to disk.
func (q *Queue) Submit(prompt, replyTo string, priority int, allowWrites bool, timeoutSec int, warm bool) (*Task, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	id := generateTaskID()
	task := &Task{
		Version:     1,
		ID:          id,
		Prompt:      prompt,
		Status:      "queued",
		Priority:    priority,
		ReplyTo:     replyTo,
		AllowWrites: allowWrites,
		Warm:        warm,
		CreatedAt:   time.Now(),
		TimeoutSec:  timeoutSec,
		Metadata:    map[string]string{},
	}

	q.tasks = append(q.tasks, task)

	if err := q.flush(); err != nil {
		// Roll back in-memory state on flush failure.
		q.tasks = q.tasks[:len(q.tasks)-1]
		return nil, fmt.Errorf("persisting queue: %w", err)
	}

	return task, nil
}

// Next returns the next task to run (highest priority → lowest number, then
// FIFO for equal priority) and marks it as running. Returns nil if no queued
// tasks are available.
func (q *Queue) Next() (*Task, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	var best *Task
	for _, t := range q.tasks {
		if t.Status != "queued" {
			continue
		}
		if best == nil {
			best = t
			continue
		}
		if t.Priority < best.Priority {
			best = t
		} else if t.Priority == best.Priority && t.CreatedAt.Before(best.CreatedAt) {
			best = t
		}
	}

	if best == nil {
		return nil, nil
	}

	now := time.Now()
	best.Status = "running"
	best.StartedAt = &now

	if err := q.flush(); err != nil {
		// Roll back in-memory state.
		best.Status = "queued"
		best.StartedAt = nil
		return nil, fmt.Errorf("persisting queue: %w", err)
	}

	return best, nil
}

// NextWarm returns the next queued task with Warm=true (highest priority, then
// FIFO) and marks it as running. Returns nil if no warm tasks are queued.
func (q *Queue) NextWarm() (*Task, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	var best *Task
	for _, t := range q.tasks {
		// Warm workers run without per-task permission sandboxing, so tasks
		// requiring writes must go through cold dispatch which enforces AllowWrites.
		if t.Status != "queued" || !t.Warm || t.AllowWrites {
			continue
		}
		if best == nil {
			best = t
			continue
		}
		if t.Priority < best.Priority {
			best = t
		} else if t.Priority == best.Priority && t.CreatedAt.Before(best.CreatedAt) {
			best = t
		}
	}

	if best == nil {
		return nil, nil
	}

	now := time.Now()
	best.Status = "running"
	best.StartedAt = &now

	if err := q.flush(); err != nil {
		best.Status = "queued"
		best.StartedAt = nil
		return nil, fmt.Errorf("persisting queue: %w", err)
	}

	return best, nil
}

// Complete marks the task with the given ID as completed with the provided
// result string and persists.
func (q *Queue) Complete(id, result string) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	task, err := q.findLocked(id)
	if err != nil {
		return err
	}

	now := time.Now()
	prev := *task
	task.Status = "completed"
	task.Result = &result
	task.CompletedAt = &now

	if err := q.flush(); err != nil {
		*task = prev
		return fmt.Errorf("persisting queue: %w", err)
	}
	return nil
}

// Fail marks the task with the given ID as failed with the provided error
// message and persists.
func (q *Queue) Fail(id, errMsg string) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	task, err := q.findLocked(id)
	if err != nil {
		return err
	}

	now := time.Now()
	prev := *task
	task.Status = "failed"
	task.Error = &errMsg
	task.CompletedAt = &now

	if err := q.flush(); err != nil {
		*task = prev
		return fmt.Errorf("persisting queue: %w", err)
	}
	return nil
}

// Cancel marks the task with the given ID as cancelled and persists.
func (q *Queue) Cancel(id string) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	task, err := q.findLocked(id)
	if err != nil {
		return err
	}

	prev := *task
	task.Status = "cancelled"

	if err := q.flush(); err != nil {
		*task = prev
		return fmt.Errorf("persisting queue: %w", err)
	}
	return nil
}

// MarkDelivered marks the task with the given ID as delivered and persists.
func (q *Queue) MarkDelivered(id string) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	t, err := q.findLocked(id)
	if err != nil {
		return err
	}
	t.Delivered = true
	return q.flush()
}

// Get returns the task with the given ID, or an error if not found.
func (q *Queue) Get(id string) (*Task, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.findLocked(id)
}

// List returns all tasks. When all is false, only active tasks (queued,
// running) are returned. When all is true, all tasks are returned.
func (q *Queue) List(all bool) []*Task {
	q.mu.Lock()
	defer q.mu.Unlock()

	var result []*Task
	for _, t := range q.tasks {
		if all || t.Status == "queued" || t.Status == "running" {
			// Return a copy to avoid external mutation.
			copy := *t
			result = append(result, &copy)
		}
	}
	return result
}

// Flush writes the queue to disk atomically (write to .tmp, then rename).
// It is called automatically by all mutating methods. It is exported for
// testing and external use.
func (q *Queue) Flush() error {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.flush()
}

// Prune removes completed/failed/cancelled tasks whose CompletedAt is older
// than retentionDays days. A retentionDays of ≤ 0 removes all terminal tasks.
// Returns the number of tasks removed.
func (q *Queue) Prune(retentionDays int) int {
	q.mu.Lock()
	defer q.mu.Unlock()

	cutoff := time.Now().AddDate(0, 0, -retentionDays)
	terminal := map[string]bool{"completed": true, "failed": true, "cancelled": true}

	var kept []*Task
	removed := 0
	for _, t := range q.tasks {
		if terminal[t.Status] {
			if retentionDays <= 0 || (t.CompletedAt != nil && t.CompletedAt.Before(cutoff)) {
				removed++
				continue
			}
		}
		kept = append(kept, t)
	}

	if removed > 0 {
		q.tasks = kept
		q.flush() //nolint:errcheck
	}
	return removed
}

// RecentCompleted returns the last n completed or failed tasks in reverse
// chronological order (most recently completed first).
func (q *Queue) RecentCompleted(n int) []*Task {
	q.mu.Lock()
	defer q.mu.Unlock()

	var terminal []*Task
	for _, t := range q.tasks {
		if t.Status == "completed" || t.Status == "failed" {
			copy := *t
			terminal = append(terminal, &copy)
		}
	}

	sort.Slice(terminal, func(i, j int) bool {
		if terminal[i].CompletedAt == nil {
			return false
		}
		if terminal[j].CompletedAt == nil {
			return true
		}
		return terminal[i].CompletedAt.After(*terminal[j].CompletedAt)
	})

	if n > len(terminal) {
		n = len(terminal)
	}
	return terminal[:n]
}

// findLocked returns a pointer to the task with the given ID.
// Caller must hold q.mu.
func (q *Queue) findLocked(id string) (*Task, error) {
	for _, t := range q.tasks {
		if t.ID == id {
			return t, nil
		}
	}
	return nil, fmt.Errorf("task %q not found", id)
}

// flush writes the current queue to disk atomically.
// Caller must hold q.mu.
func (q *Queue) flush() error {
	envelope := queueData{Tasks: q.tasks}
	if envelope.Tasks == nil {
		envelope.Tasks = []*Task{}
	}

	data, err := json.MarshalIndent(envelope, "", "  ")
	if err != nil {
		return fmt.Errorf("marshalling queue: %w", err)
	}

	dir := filepath.Dir(q.path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("creating queue dir: %w", err)
	}

	tmp := q.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return fmt.Errorf("writing tmp queue: %w", err)
	}

	if err := os.Rename(tmp, q.path); err != nil {
		return fmt.Errorf("renaming tmp queue: %w", err)
	}

	return nil
}

// generateTaskID generates a task ID in the format t_<unix_timestamp>_<6 random hex chars>.
func generateTaskID() string {
	const hexChars = "0123456789abcdef"
	b := make([]byte, 6)
	for i := range b {
		b[i] = hexChars[rand.Intn(len(hexChars))]
	}
	return fmt.Sprintf("t_%d_%s", time.Now().Unix(), string(b))
}
