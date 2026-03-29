package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
)

// Schedule represents a recurring task.
type Schedule struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Cron        string     `json:"cron"`
	Prompt      string     `json:"prompt"`
	ReplyTo     string     `json:"reply_to"`
	AllowWrites bool       `json:"allow_writes"`
	LastFiredAt *time.Time `json:"last_fired_at"`
	CreatedAt   time.Time  `json:"created_at"`
}

// SubmitFunc submits a task to the daemon queue.
type SubmitFunc func(prompt, replyTo string, allowWrites bool) error

// Scheduler manages recurring task schedules.
type Scheduler struct {
	mu        sync.Mutex
	schedules []*Schedule
	filePath  string
	submitFn  SubmitFunc
	parser    cron.Parser
}

// New creates a scheduler. submitFn may be nil (for tests that only test CRUD).
func New(filePath string, submitFn SubmitFunc) *Scheduler {
	return &Scheduler{
		filePath: filePath,
		submitFn: submitFn,
		parser:   cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow),
	}
}

// Load reads schedules from disk. If the file is corrupted, logs a warning and starts empty.
func (s *Scheduler) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var schedules []*Schedule
	if err := json.Unmarshal(data, &schedules); err != nil {
		log.Printf("scheduler: corrupted %s, starting empty: %v", s.filePath, err)
		s.schedules = nil
		return nil
	}

	s.schedules = schedules
	return nil
}

// Save persists schedules to disk atomically.
func (s *Scheduler) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.flush()
}

func (s *Scheduler) flush() error {
	data, err := json.MarshalIndent(s.schedules, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.filePath + ".tmp"
	if err := os.MkdirAll(filepath.Dir(s.filePath), 0700); err != nil {
		return err
	}
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, s.filePath)
}

// Create adds a new schedule. Returns error if name already exists or cron is invalid.
func (s *Scheduler) Create(name, cronExpr, prompt, replyTo string, allowWrites bool) (*Schedule, error) {
	if _, err := s.parser.Parse(cronExpr); err != nil {
		return nil, fmt.Errorf("invalid cron expression: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, sched := range s.schedules {
		if sched.Name == name {
			return nil, fmt.Errorf("schedule %q already exists", name)
		}
	}

	sched := &Schedule{
		ID:          fmt.Sprintf("s_%d", time.Now().UnixNano()),
		Name:        name,
		Cron:        cronExpr,
		Prompt:      prompt,
		ReplyTo:     replyTo,
		AllowWrites: allowWrites,
		CreatedAt:   time.Now(),
	}

	s.schedules = append(s.schedules, sched)
	if err := s.flush(); err != nil {
		return nil, err
	}
	return sched, nil
}

// Remove deletes a schedule by name.
func (s *Scheduler) Remove(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, sched := range s.schedules {
		if sched.Name == name {
			s.schedules = append(s.schedules[:i], s.schedules[i+1:]...)
			return s.flush()
		}
	}
	return fmt.Errorf("schedule %q not found", name)
}

// List returns all schedules.
func (s *Scheduler) List() []*Schedule {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]*Schedule, len(s.schedules))
	copy(out, s.schedules)
	return out
}

// CatchUp fires any schedules that missed their last run (within 2x interval).
func (s *Scheduler) CatchUp() {
	s.mu.Lock()
	var toFire []*Schedule
	now := time.Now()
	for _, sched := range s.schedules {
		if sched.LastFiredAt == nil {
			continue
		}

		parsed, err := s.parser.Parse(sched.Cron)
		if err != nil {
			continue
		}

		nextAfterLast := parsed.Next(*sched.LastFiredAt)
		if nextAfterLast.Before(now) {
			interval := parsed.Next(nextAfterLast).Sub(nextAfterLast)
			missedBy := now.Sub(nextAfterLast)

			if missedBy <= 2*interval {
				log.Printf("scheduler: catch-up firing %q (missed by %s)", sched.Name, missedBy.Truncate(time.Second))
				toFire = append(toFire, sched)
			} else {
				log.Printf("scheduler: skipping catch-up for %q (missed by %s, > 2x interval)", sched.Name, missedBy.Truncate(time.Second))
			}
		}
	}
	s.mu.Unlock()

	for _, sched := range toFire {
		s.fire(sched)
	}
}

// Run starts the scheduler tick loop. Blocks until ctx is cancelled.
func (s *Scheduler) Run(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.tick()
		}
	}
}

func (s *Scheduler) tick() {
	s.mu.Lock()
	var toFire []*Schedule
	now := time.Now()
	for _, sched := range s.schedules {
		parsed, err := s.parser.Parse(sched.Cron)
		if err != nil {
			continue
		}

		var ref time.Time
		if sched.LastFiredAt != nil {
			ref = *sched.LastFiredAt
		} else {
			ref = sched.CreatedAt
		}

		next := parsed.Next(ref)
		if next.Before(now) || next.Equal(now) {
			toFire = append(toFire, sched)
		}
	}
	s.mu.Unlock()

	for _, sched := range toFire {
		s.fire(sched)
	}
}

// fire submits the schedule's task and updates last_fired_at.
// Must be called WITHOUT s.mu held (submitFn may block).
func (s *Scheduler) fire(sched *Schedule) {
	if s.submitFn == nil {
		return
	}
	if err := s.submitFn(sched.Prompt, sched.ReplyTo, sched.AllowWrites); err != nil {
		log.Printf("scheduler: failed to submit %q: %v", sched.Name, err)
		return
	}
	s.mu.Lock()
	now := time.Now()
	sched.LastFiredAt = &now
	_ = s.flush()
	s.mu.Unlock()
}
