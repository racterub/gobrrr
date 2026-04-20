package skills

import "sync"

// Registry caches the set of installed skills in memory. Refresh rescans the
// filesystem; List returns the current snapshot as a fresh slice safe to pass
// to callers without mutation concerns.
type Registry struct {
	root   string
	mu     sync.RWMutex
	skills []Skill
}

func NewRegistry(root string) *Registry {
	return &Registry{root: root}
}

func (r *Registry) Refresh() error {
	skills, err := LoadAll(r.root)
	if err != nil {
		return err
	}
	r.mu.Lock()
	r.skills = skills
	r.mu.Unlock()
	return nil
}

func (r *Registry) List() []Skill {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Skill, len(r.skills))
	copy(out, r.skills)
	return out
}
