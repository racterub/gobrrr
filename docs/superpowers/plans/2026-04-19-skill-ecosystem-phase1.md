# Skill Ecosystem Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship Phase 1 of the skill ecosystem design — system skills loaded into workers via prompt injection, ClawHub skills installable via CLI with explicit user approval.

**Architecture:** Two new Go packages (`internal/skills/` loader + registry + prompt builder; `internal/clawhub/` HTTP client + installer + commit). System skills embedded in the binary via `//go:embed` and copied to `~/.gobrrr/skills/` on first daemon start. Every worker prompt gets an `<available_skills>` block listing names/descriptions/paths; Claude Reads the SKILL.md on demand. Per-task `settings.json` merges installed skills' `approved_read_permissions` always, and `approved_write_permissions` only when `--allow-writes` is set.

**Tech Stack:** Go 1.22+, `gopkg.in/yaml.v3`, `net/http`, `crypto/sha256`, `embed`, testify/assert.

**Spec:** `docs/superpowers/specs/2026-04-19-skill-ecosystem-design.md`

---

## Phase 1: Local Skill Loading + Worker Integration

Value at phase end: bundled skills copied to `~/.gobrrr/skills/` on daemon start, workers see them in `<available_skills>` prompt block, permission merge respects read/write split. ClawHub not wired yet.

### Task 1: Move bundled skills under internal/skills/system/ + promote yaml dependency

**Files:**
- Move: `daemon/skills/<slug>/SKILL.md` → `daemon/internal/skills/system/<slug>/SKILL.md` (7 skills: browser, calendar, dispatch, gmail, homelab, memory, timer-management)
- Modify: `daemon/go.mod` (promote `gopkg.in/yaml.v3` from indirect to direct)

**Why:** `//go:embed` patterns cannot reference files outside the package's directory tree. To embed system skills in the binary, the files must live under the package that embeds them.

- [ ] **Step 1: Move the 7 skill directories**

```bash
mkdir -p daemon/internal/skills/system
git mv daemon/skills/browser daemon/internal/skills/system/browser
git mv daemon/skills/calendar daemon/internal/skills/system/calendar
git mv daemon/skills/dispatch daemon/internal/skills/system/dispatch
git mv daemon/skills/gmail daemon/internal/skills/system/gmail
git mv daemon/skills/homelab daemon/internal/skills/system/homelab
git mv daemon/skills/memory daemon/internal/skills/system/memory
git mv daemon/skills/timer-management daemon/internal/skills/system/timer-management
rmdir daemon/skills 2>/dev/null || true
```

- [ ] **Step 2: Promote yaml.v3 to direct dependency**

```bash
cd daemon && go get gopkg.in/yaml.v3@v3.0.1
```

Verify `daemon/go.mod` has `gopkg.in/yaml.v3 v3.0.1` **without** the `// indirect` suffix.

- [ ] **Step 3: Verify build**

```bash
cd daemon && CGO_ENABLED=0 go build -o /tmp/gobrrr-build-check ./cmd/gobrrr/
```

Expected: build succeeds. No code changes yet.

- [ ] **Step 4: Commit (structural)**

```bash
cd /home/racterub/github/gobrrr
git add daemon/go.mod daemon/go.sum daemon/internal/skills/system/
git commit -m "refactor(skills): relocate bundled skills to internal/skills/system/"
```

---

### Task 2: Add frontmatter to system skills

**Files:**
- Modify: `daemon/internal/skills/system/gmail/SKILL.md`
- Modify: `daemon/internal/skills/system/calendar/SKILL.md`
- Modify: `daemon/internal/skills/system/browser/SKILL.md`
- Modify: `daemon/internal/skills/system/dispatch/SKILL.md`
- Modify: `daemon/internal/skills/system/homelab/SKILL.md`
- Modify: `daemon/internal/skills/system/memory/SKILL.md`
- Modify: `daemon/internal/skills/system/timer-management/SKILL.md`

**Why:** Frontmatter is needed so the loader can parse `name`, `description`, `type: system`, and `tool_permissions.read`/`.write`.

- [ ] **Step 1: Add frontmatter to gmail/SKILL.md**

Prepend to `daemon/internal/skills/system/gmail/SKILL.md`:

```yaml
---
name: gmail
description: Email read/send/reply via gobrrr CLI
metadata:
  gobrrr:
    type: system
  openclaw:
    requires:
      bins: [gobrrr]
      tool_permissions:
        read:
          - "Bash(gobrrr gmail list:*)"
          - "Bash(gobrrr gmail read:*)"
        write:
          - "Bash(gobrrr gmail send:*)"
          - "Bash(gobrrr gmail reply:*)"
---

```

(Keep the existing `# Gmail Skill` body unchanged below.)

- [ ] **Step 2: Add frontmatter to calendar/SKILL.md**

```yaml
---
name: calendar
description: Google Calendar read/create/update via gobrrr CLI
metadata:
  gobrrr:
    type: system
  openclaw:
    requires:
      bins: [gobrrr]
      tool_permissions:
        read:
          - "Bash(gobrrr gcal today:*)"
          - "Bash(gobrrr gcal week:*)"
          - "Bash(gobrrr gcal list:*)"
        write:
          - "Bash(gobrrr gcal create:*)"
          - "Bash(gobrrr gcal update:*)"
          - "Bash(gobrrr gcal delete:*)"
---

```

- [ ] **Step 3: Add frontmatter to browser/SKILL.md**

```yaml
---
name: browser
description: Agent-driven headless browser for web content extraction
metadata:
  gobrrr:
    type: system
  openclaw:
    requires:
      bins: [agent-browser]
      tool_permissions:
        read:
          - "Bash(agent-browser *)"
        write: []
---

```

- [ ] **Step 4: Add frontmatter to dispatch/SKILL.md**

```yaml
---
name: dispatch
description: Submit background tasks to the gobrrr daemon
metadata:
  gobrrr:
    type: system
  openclaw:
    requires:
      bins: [gobrrr]
      tool_permissions:
        read:
          - "Bash(gobrrr list:*)"
          - "Bash(gobrrr status:*)"
          - "Bash(gobrrr logs:*)"
        write:
          - "Bash(gobrrr submit:*)"
          - "Bash(gobrrr cancel:*)"
          - "Bash(gobrrr approve:*)"
          - "Bash(gobrrr deny:*)"
---

```

- [ ] **Step 5: Add frontmatter to homelab/SKILL.md**

```yaml
---
name: homelab
description: Homelab service queries (Proxmox VMs, service health)
metadata:
  gobrrr:
    type: system
  openclaw:
    requires:
      bins: [gobrrr]
      tool_permissions:
        read:
          - "Bash(gobrrr homelab:*)"
        write: []
---

```

- [ ] **Step 6: Add frontmatter to memory/SKILL.md**

```yaml
---
name: memory
description: Persistent gobrrr memory store — search, add, list
metadata:
  gobrrr:
    type: system
  openclaw:
    requires:
      bins: [gobrrr]
      tool_permissions:
        read:
          - "Bash(gobrrr memory list:*)"
          - "Bash(gobrrr memory search:*)"
          - "Bash(gobrrr memory get:*)"
        write:
          - "Bash(gobrrr memory add:*)"
          - "Bash(gobrrr memory delete:*)"
---

```

- [ ] **Step 7: Add frontmatter to timer-management/SKILL.md**

```yaml
---
name: timer-management
description: Scheduled task management (cron-like recurring dispatches)
metadata:
  gobrrr:
    type: system
  openclaw:
    requires:
      bins: [gobrrr]
      tool_permissions:
        read:
          - "Bash(gobrrr schedule list:*)"
          - "Bash(gobrrr schedule show:*)"
        write:
          - "Bash(gobrrr schedule add:*)"
          - "Bash(gobrrr schedule delete:*)"
          - "Bash(gobrrr schedule enable:*)"
          - "Bash(gobrrr schedule disable:*)"
---

```

- [ ] **Step 8: Commit (behavioral — changes skill content)**

```bash
cd /home/racterub/github/gobrrr
git add daemon/internal/skills/system/
git commit -m "feat(skills): add OpenClaw frontmatter to system skills"
```

---

### Task 3: Skill types and frontmatter parser

**Files:**
- Create: `daemon/internal/skills/types.go`
- Create: `daemon/internal/skills/frontmatter.go`
- Create: `daemon/internal/skills/frontmatter_test.go`

- [ ] **Step 1: Write the failing test**

Create `daemon/internal/skills/frontmatter_test.go`:

```go
package skills

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseFrontmatter_SystemSkill(t *testing.T) {
	content := []byte(`---
name: gmail
description: Email read/send/reply via gobrrr CLI
metadata:
  gobrrr:
    type: system
  openclaw:
    requires:
      bins: [gobrrr]
      tool_permissions:
        read:
          - "Bash(gobrrr gmail list:*)"
        write:
          - "Bash(gobrrr gmail send:*)"
---

# Gmail Skill

body content here.
`)

	fm, body, err := ParseFrontmatter(content)
	require.NoError(t, err)
	assert.Equal(t, "gmail", fm.Name)
	assert.Equal(t, "Email read/send/reply via gobrrr CLI", fm.Description)
	assert.Equal(t, "system", fm.Metadata.Gobrrr.Type)
	assert.Equal(t, []string{"gobrrr"}, fm.Metadata.OpenClaw.Requires.Bins)
	assert.Equal(t, []string{"Bash(gobrrr gmail list:*)"}, fm.Metadata.OpenClaw.Requires.ToolPermissions.Read)
	assert.Equal(t, []string{"Bash(gobrrr gmail send:*)"}, fm.Metadata.OpenClaw.Requires.ToolPermissions.Write)
	assert.Contains(t, string(body), "# Gmail Skill")
}

func TestParseFrontmatter_MissingFrontmatter(t *testing.T) {
	content := []byte("# Just a markdown file\n\nno frontmatter here.\n")
	_, _, err := ParseFrontmatter(content)
	assert.Error(t, err)
}

func TestParseFrontmatter_MalformedYAML(t *testing.T) {
	content := []byte("---\nname: broken\n  description: wrong indent\n---\n\nbody\n")
	_, _, err := ParseFrontmatter(content)
	assert.Error(t, err)
}

func TestParseFrontmatter_FlatToolPermissions_DefaultsToRead(t *testing.T) {
	// Fallback for ClawHub skills that use a flat `tool_permissions: [...]` form.
	content := []byte(`---
name: legacy
description: legacy skill
metadata:
  gobrrr:
    type: clawhub
  openclaw:
    requires:
      tool_permissions:
        - "Bash(echo:*)"
---

body
`)
	fm, _, err := ParseFrontmatter(content)
	require.NoError(t, err)
	assert.Equal(t, []string{"Bash(echo:*)"}, fm.Metadata.OpenClaw.Requires.ToolPermissions.Read)
	assert.Empty(t, fm.Metadata.OpenClaw.Requires.ToolPermissions.Write)
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd daemon && go test ./internal/skills/... -run TestParseFrontmatter -v
```

Expected: FAIL — package doesn't exist yet.

- [ ] **Step 3: Create types.go**

Create `daemon/internal/skills/types.go`:

```go
// Package skills loads SKILL.md files, maintains an in-memory registry, and
// builds the <available_skills> prompt block injected into worker prompts.
package skills

type SkillType string

const (
	TypeSystem  SkillType = "system"
	TypeClawhub SkillType = "clawhub"
	TypeUser    SkillType = "user"
)

// Skill is the loaded representation of one installed skill.
type Skill struct {
	Slug             string
	Description      string
	Path             string   // absolute path to SKILL.md
	Dir              string   // skill directory
	Type             SkillType
	ReadPermissions  []string // from _meta.json approved_read_permissions
	WritePermissions []string // from _meta.json approved_write_permissions
	Fingerprint      string   // sha256 of skill dir at last approval
}

// Frontmatter is the parsed YAML header of a SKILL.md.
type Frontmatter struct {
	Name        string       `yaml:"name"`
	Description string       `yaml:"description"`
	Metadata    FMMetadata   `yaml:"metadata"`
}

type FMMetadata struct {
	Gobrrr   FMGobrrr   `yaml:"gobrrr"`
	OpenClaw FMOpenClaw `yaml:"openclaw"`
}

type FMGobrrr struct {
	Type string `yaml:"type"` // system | clawhub | user
}

type FMOpenClaw struct {
	Emoji    string    `yaml:"emoji,omitempty"`
	Homepage string    `yaml:"homepage,omitempty"`
	Requires FMRequires `yaml:"requires"`
}

type FMRequires struct {
	Bins            []string              `yaml:"bins,omitempty"`
	Env             []string              `yaml:"env,omitempty"`
	ToolPermissions FMToolPermissions     `yaml:"tool_permissions"`
}

// FMToolPermissions supports both the split form
//   tool_permissions: { read: [...], write: [...] }
// and the legacy flat form
//   tool_permissions: [...]
// which is treated as all-read.
type FMToolPermissions struct {
	Read  []string
	Write []string
}
```

- [ ] **Step 4: Create frontmatter.go with parser and custom unmarshaller**

Create `daemon/internal/skills/frontmatter.go`:

```go
package skills

import (
	"bytes"
	"errors"
	"fmt"

	"gopkg.in/yaml.v3"
)

var (
	errNoFrontmatter  = errors.New("skills: no YAML frontmatter found")
	errBadFrontmatter = errors.New("skills: malformed YAML frontmatter")
)

// ParseFrontmatter extracts and parses the YAML frontmatter from a SKILL.md
// file's bytes. It returns the parsed Frontmatter, the body after the
// closing "---", and an error if frontmatter is missing or malformed.
func ParseFrontmatter(content []byte) (*Frontmatter, []byte, error) {
	const delim = "---"
	if !bytes.HasPrefix(content, []byte(delim+"\n")) && !bytes.HasPrefix(content, []byte(delim+"\r\n")) {
		return nil, nil, errNoFrontmatter
	}
	after := content[len(delim):]
	if len(after) > 0 && after[0] == '\r' {
		after = after[1:]
	}
	if len(after) > 0 && after[0] == '\n' {
		after = after[1:]
	}
	end := bytes.Index(after, []byte("\n"+delim))
	if end < 0 {
		return nil, nil, errNoFrontmatter
	}
	yamlBytes := after[:end]
	body := after[end+len("\n"+delim):]
	if len(body) > 0 && body[0] == '\r' {
		body = body[1:]
	}
	if len(body) > 0 && body[0] == '\n' {
		body = body[1:]
	}

	var fm Frontmatter
	if err := yaml.Unmarshal(yamlBytes, &fm); err != nil {
		return nil, nil, fmt.Errorf("%w: %v", errBadFrontmatter, err)
	}
	return &fm, body, nil
}

// UnmarshalYAML accepts both
//   tool_permissions: { read: [...], write: [...] }
// and the legacy flat form
//   tool_permissions: [...]
// which is treated as all-read.
func (tp *FMToolPermissions) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.SequenceNode:
		var flat []string
		if err := node.Decode(&flat); err != nil {
			return err
		}
		tp.Read = flat
		tp.Write = nil
		return nil
	case yaml.MappingNode:
		var split struct {
			Read  []string `yaml:"read"`
			Write []string `yaml:"write"`
		}
		if err := node.Decode(&split); err != nil {
			return err
		}
		tp.Read = split.Read
		tp.Write = split.Write
		return nil
	default:
		return fmt.Errorf("tool_permissions: expected sequence or mapping, got %v", node.Kind)
	}
}
```

- [ ] **Step 5: Run tests**

```bash
cd daemon && go test ./internal/skills/... -v
```

Expected: PASS. All 4 frontmatter tests green.

- [ ] **Step 6: Commit**

```bash
cd /home/racterub/github/gobrrr
git add daemon/internal/skills/types.go daemon/internal/skills/frontmatter.go daemon/internal/skills/frontmatter_test.go
git commit -m "feat(skills): add frontmatter parser with read/write permission split"
```

---

### Task 4: Skill loader

**Files:**
- Create: `daemon/internal/skills/loader.go`
- Create: `daemon/internal/skills/loader_test.go`

**Responsibility:** walk `~/.gobrrr/skills/`, skip dot/underscore entries (`_pending/`, `_lock.json`, `_requests/`), parse each skill's SKILL.md + `_meta.json`, return `[]Skill`.

- [ ] **Step 1: Write failing test**

Create `daemon/internal/skills/loader_test.go`:

```go
package skills

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadAll_ReturnsInstalledSkills(t *testing.T) {
	root := t.TempDir()

	// Layout two skills and one pending (must be skipped).
	writeSkill(t, root, "gmail", "system",
		[]string{"Bash(gobrrr gmail list:*)"}, []string{"Bash(gobrrr gmail send:*)"})
	writeSkill(t, root, "github", "clawhub",
		[]string{"Bash(gh issue list:*)"}, []string{"Bash(gh pr create:*)"})
	require.NoError(t, os.MkdirAll(filepath.Join(root, "_pending", "draft"), 0700))

	skills, err := LoadAll(root)
	require.NoError(t, err)
	assert.Len(t, skills, 2)

	bySlug := map[string]Skill{}
	for _, s := range skills {
		bySlug[s.Slug] = s
	}
	gmail := bySlug["gmail"]
	assert.Equal(t, TypeSystem, gmail.Type)
	assert.Equal(t, []string{"Bash(gobrrr gmail list:*)"}, gmail.ReadPermissions)
	assert.Equal(t, []string{"Bash(gobrrr gmail send:*)"}, gmail.WritePermissions)
	assert.Equal(t, "Email read/send/reply via gobrrr CLI", gmail.Description)
	assert.Equal(t, filepath.Join(root, "gmail", "SKILL.md"), gmail.Path)
}

func TestLoadAll_SkipsSkillWithNoMeta(t *testing.T) {
	root := t.TempDir()
	slug := "orphan"
	dir := filepath.Join(root, slug)
	require.NoError(t, os.MkdirAll(dir, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "SKILL.md"),
		[]byte("---\nname: orphan\ndescription: no meta\nmetadata:\n  gobrrr:\n    type: clawhub\n---\n\nbody\n"), 0600))

	skills, err := LoadAll(root)
	require.NoError(t, err)
	assert.Empty(t, skills)
}

func TestLoadAll_MissingRootIsNotAnError(t *testing.T) {
	skills, err := LoadAll(filepath.Join(t.TempDir(), "does-not-exist"))
	require.NoError(t, err)
	assert.Empty(t, skills)
}

// Test helper: write a skill directory with SKILL.md + _meta.json.
func writeSkill(t *testing.T, root, slug, skillType string, read, write []string) {
	t.Helper()
	dir := filepath.Join(root, slug)
	require.NoError(t, os.MkdirAll(dir, 0700))

	// SKILL.md with frontmatter
	desc := map[string]string{
		"gmail":  "Email read/send/reply via gobrrr CLI",
		"github": "GitHub issue/PR ops via gh CLI",
	}[slug]
	skillMD := "---\nname: " + slug +
		"\ndescription: " + desc +
		"\nmetadata:\n  gobrrr:\n    type: " + skillType +
		"\n---\n\nbody\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(skillMD), 0600))

	// _meta.json
	meta := map[string]any{
		"slug":                       slug,
		"version":                    "1.0.0",
		"installed_at":               "2026-04-19T16:45:00Z",
		"fingerprint":                "sha256:fake",
		"approved_read_permissions":  read,
		"approved_write_permissions": write,
	}
	b, _ := json.MarshalIndent(meta, "", "  ")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "_meta.json"), b, 0600))
}
```

- [ ] **Step 2: Run to verify failure**

```bash
cd daemon && go test ./internal/skills/... -run TestLoadAll -v
```

Expected: FAIL — `LoadAll` undefined.

- [ ] **Step 3: Implement loader**

Create `daemon/internal/skills/loader.go`:

```go
package skills

import (
	"encoding/json"
	"errors"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// Meta is the on-disk _meta.json the installer writes at approval time.
type Meta struct {
	Slug                     string   `json:"slug"`
	Version                  string   `json:"version"`
	SourceURL                string   `json:"source_url,omitempty"`
	InstalledAt              string   `json:"installed_at"`
	ApprovedAt               string   `json:"approved_at,omitempty"`
	Fingerprint              string   `json:"fingerprint"`
	ApprovedReadPermissions  []string `json:"approved_read_permissions"`
	ApprovedWritePermissions []string `json:"approved_write_permissions"`
	ApprovedBinaries         []string `json:"approved_binaries,omitempty"`
	BinaryInstallCommands    []BinaryInstallRecord `json:"binary_install_commands,omitempty"`
}

type BinaryInstallRecord struct {
	RecipeID string `json:"recipe_id"`
	Command  string `json:"command"`
	Approved bool   `json:"approved"`
	RanAt    string `json:"ran_at,omitempty"`
	ExitCode int    `json:"exit_code"`
}

// LoadAll walks root (typically ~/.gobrrr/skills/) and returns every valid
// skill (one with both SKILL.md containing frontmatter and a _meta.json).
// Directories starting with "." or "_" are skipped (reserved for pending
// drafts, lockfile, request staging).
//
// Malformed entries are logged and skipped rather than failing the whole load.
func LoadAll(root string) ([]Skill, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}

	var out []Skill
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
			continue
		}
		skill, err := loadOne(filepath.Join(root, name))
		if err != nil {
			log.Printf("skills: skipping %s: %v", name, err)
			continue
		}
		out = append(out, *skill)
	}
	return out, nil
}

func loadOne(dir string) (*Skill, error) {
	skillPath := filepath.Join(dir, "SKILL.md")
	metaPath := filepath.Join(dir, "_meta.json")

	skillBytes, err := os.ReadFile(skillPath)
	if err != nil {
		return nil, err
	}
	metaBytes, err := os.ReadFile(metaPath)
	if err != nil {
		return nil, err
	}

	fm, _, err := ParseFrontmatter(skillBytes)
	if err != nil {
		return nil, err
	}

	var meta Meta
	if err := json.Unmarshal(metaBytes, &meta); err != nil {
		return nil, err
	}

	return &Skill{
		Slug:             fm.Name,
		Description:      fm.Description,
		Path:             skillPath,
		Dir:              dir,
		Type:             SkillType(fm.Metadata.Gobrrr.Type),
		ReadPermissions:  meta.ApprovedReadPermissions,
		WritePermissions: meta.ApprovedWritePermissions,
		Fingerprint:      meta.Fingerprint,
	}, nil
}
```

- [ ] **Step 4: Run tests**

```bash
cd daemon && go test ./internal/skills/... -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /home/racterub/github/gobrrr
git add daemon/internal/skills/loader.go daemon/internal/skills/loader_test.go
git commit -m "feat(skills): implement filesystem loader for installed skills"
```

---

### Task 5: Skill registry (thread-safe cache)

**Files:**
- Create: `daemon/internal/skills/registry.go`
- Create: `daemon/internal/skills/registry_test.go`

**Responsibility:** thread-safe in-memory cache. `Refresh()` rescans filesystem. Public getter returns a stable snapshot.

- [ ] **Step 1: Write failing test**

Create `daemon/internal/skills/registry_test.go`:

```go
package skills

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegistry_RefreshAndList(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "gmail", "system", []string{"Bash(gobrrr gmail list:*)"}, nil)

	reg := NewRegistry(root)
	require.NoError(t, reg.Refresh())
	assert.Len(t, reg.List(), 1)
	assert.Equal(t, "gmail", reg.List()[0].Slug)

	// Add a second skill and refresh.
	writeSkill(t, root, "github", "clawhub", []string{"Bash(gh issue list:*)"}, nil)
	require.NoError(t, reg.Refresh())
	assert.Len(t, reg.List(), 2)

	// Remove one and refresh.
	require.NoError(t, os.RemoveAll(filepath.Join(root, "gmail")))
	require.NoError(t, reg.Refresh())
	assert.Len(t, reg.List(), 1)
	assert.Equal(t, "github", reg.List()[0].Slug)
}

func TestRegistry_EmptyRoot(t *testing.T) {
	reg := NewRegistry(filepath.Join(t.TempDir(), "nonexistent"))
	require.NoError(t, reg.Refresh())
	assert.Empty(t, reg.List())
}
```

- [ ] **Step 2: Run to verify failure**

```bash
cd daemon && go test ./internal/skills/... -run TestRegistry -v
```

Expected: FAIL — `NewRegistry` undefined.

- [ ] **Step 3: Implement registry**

Create `daemon/internal/skills/registry.go`:

```go
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
```

- [ ] **Step 4: Run tests**

```bash
cd daemon && go test ./internal/skills/... -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /home/racterub/github/gobrrr
git add daemon/internal/skills/registry.go daemon/internal/skills/registry_test.go
git commit -m "feat(skills): add thread-safe registry with Refresh/List"
```

---

### Task 6: Prompt block builder

**Files:**
- Create: `daemon/internal/skills/prompt.go`
- Create: `daemon/internal/skills/prompt_test.go`

**Responsibility:** emit `<available_skills>` XML with name + description + tilde-compacted location. Stable skill ordering (alphabetical) so prompt block is deterministic.

- [ ] **Step 1: Write failing test**

Create `daemon/internal/skills/prompt_test.go`:

```go
package skills

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildPromptBlock_TwoSkills(t *testing.T) {
	home := "/home/racterub"
	skills := []Skill{
		{Slug: "github", Description: "GitHub ops", Path: home + "/.gobrrr/skills/github/SKILL.md"},
		{Slug: "gmail", Description: "Email ops", Path: home + "/.gobrrr/skills/gmail/SKILL.md"},
	}
	out := BuildPromptBlock(skills, home)

	assert.Contains(t, out, "<available_skills>")
	assert.Contains(t, out, "</available_skills>")
	// Alphabetical order: github before gmail
	idxGH := strings.Index(out, `name="github"`)
	idxGM := strings.Index(out, `name="gmail"`)
	require.Positive(t, idxGH)
	require.Positive(t, idxGM)
	assert.Less(t, idxGH, idxGM)
	// Tilde compaction
	assert.Contains(t, out, `location="~/.gobrrr/skills/github/SKILL.md"`)
	assert.Contains(t, out, `location="~/.gobrrr/skills/gmail/SKILL.md"`)
	// Descriptions present
	assert.Contains(t, out, "GitHub ops")
	assert.Contains(t, out, "Email ops")
}

func TestBuildPromptBlock_Empty(t *testing.T) {
	out := BuildPromptBlock(nil, "/home/u")
	assert.Equal(t, "", out)
}

func TestBuildPromptBlock_EscapesXML(t *testing.T) {
	skills := []Skill{
		{Slug: "bad", Description: "has <brackets> & ampersands", Path: "/home/u/.gobrrr/skills/bad/SKILL.md"},
	}
	out := BuildPromptBlock(skills, "/home/u")
	assert.Contains(t, out, "&lt;brackets&gt;")
	assert.Contains(t, out, "&amp;")
}
```

- [ ] **Step 2: Run to verify failure**

```bash
cd daemon && go test ./internal/skills/... -run TestBuildPromptBlock -v
```

Expected: FAIL — `BuildPromptBlock` undefined.

- [ ] **Step 3: Implement prompt builder**

Create `daemon/internal/skills/prompt.go`:

```go
package skills

import (
	"encoding/xml"
	"sort"
	"strings"
)

// BuildPromptBlock emits the <available_skills> XML block injected into
// worker prompts. Skills are sorted alphabetically by slug for deterministic
// output. The home prefix (from os.UserHomeDir) is compacted to "~" to save
// ~5 tokens per skill path.
//
// Returns empty string for empty input so callers can unconditionally
// prepend it.
func BuildPromptBlock(skills []Skill, home string) string {
	if len(skills) == 0 {
		return ""
	}
	sorted := make([]Skill, len(skills))
	copy(sorted, skills)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Slug < sorted[j].Slug })

	var b strings.Builder
	b.WriteString("<available_skills>\n")
	for _, s := range sorted {
		loc := compactHome(s.Path, home)
		b.WriteString("  <skill name=\"")
		xmlEscape(&b, s.Slug)
		b.WriteString("\" location=\"")
		xmlEscape(&b, loc)
		b.WriteString("\">\n    ")
		xmlEscape(&b, s.Description)
		b.WriteString("\n  </skill>\n")
	}
	b.WriteString("</available_skills>")
	return b.String()
}

func compactHome(path, home string) string {
	if home == "" {
		return path
	}
	if strings.HasPrefix(path, home+"/") {
		return "~" + path[len(home):]
	}
	return path
}

func xmlEscape(b *strings.Builder, s string) {
	_ = xml.EscapeText(stringWriter{b}, []byte(s))
}

type stringWriter struct{ b *strings.Builder }

func (w stringWriter) Write(p []byte) (int, error) { return w.b.Write(p) }
```

- [ ] **Step 4: Run tests**

```bash
cd daemon && go test ./internal/skills/... -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /home/racterub/github/gobrrr
git add daemon/internal/skills/prompt.go daemon/internal/skills/prompt_test.go
git commit -m "feat(skills): build <available_skills> XML block with tilde compaction"
```

---

### Task 7: System-skill bootstrap (embed + copy on start)

**Files:**
- Create: `daemon/internal/skills/bundled.go`
- Create: `daemon/internal/skills/bundled_test.go`

**Responsibility:** Embed all files under `daemon/internal/skills/system/*/*` using `//go:embed`. On daemon start, `InstallSystemSkills(root)` copies each embedded skill to `<root>/<slug>/` if missing; writes an auto-generated `_meta.json` mirroring the frontmatter's `tool_permissions.read`/`.write`.

- [ ] **Step 1: Write failing test**

Create `daemon/internal/skills/bundled_test.go`:

```go
package skills

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInstallSystemSkills_CopiesEmbeddedSkills(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, InstallSystemSkills(root))

	// gmail is one of the embedded system skills.
	gmailDir := filepath.Join(root, "gmail")
	info, err := os.Stat(filepath.Join(gmailDir, "SKILL.md"))
	require.NoError(t, err)
	assert.False(t, info.IsDir())

	// _meta.json auto-generated.
	metaBytes, err := os.ReadFile(filepath.Join(gmailDir, "_meta.json"))
	require.NoError(t, err)
	var meta Meta
	require.NoError(t, json.Unmarshal(metaBytes, &meta))
	assert.Equal(t, "gmail", meta.Slug)
	assert.Contains(t, meta.ApprovedReadPermissions, "Bash(gobrrr gmail list:*)")
	assert.Contains(t, meta.ApprovedWritePermissions, "Bash(gobrrr gmail send:*)")
}

func TestInstallSystemSkills_Idempotent(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, InstallSystemSkills(root))

	// User-edit the gmail SKILL.md.
	gmailMD := filepath.Join(root, "gmail", "SKILL.md")
	require.NoError(t, os.WriteFile(gmailMD, []byte("# USER EDITED\n"), 0600))

	// Second install should not overwrite.
	require.NoError(t, InstallSystemSkills(root))
	got, err := os.ReadFile(gmailMD)
	require.NoError(t, err)
	assert.Equal(t, "# USER EDITED\n", string(got))
}
```

- [ ] **Step 2: Run to verify failure**

```bash
cd daemon && go test ./internal/skills/... -run TestInstallSystemSkills -v
```

Expected: FAIL — `InstallSystemSkills` undefined.

- [ ] **Step 3: Implement bundled.go**

Create `daemon/internal/skills/bundled.go`:

```go
package skills

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

//go:embed system/*/*
var systemFS embed.FS

// InstallSystemSkills copies each embedded system skill into root (typically
// ~/.gobrrr/skills/). If a target dir already exists, it is left alone to
// preserve user edits. Writes an auto-generated _meta.json per skill mirroring
// its frontmatter's read/write permission split.
func InstallSystemSkills(root string) error {
	if err := os.MkdirAll(root, 0700); err != nil {
		return err
	}

	entries, err := systemFS.ReadDir("system")
	if err != nil {
		return err
	}

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		slug := e.Name()
		if err := installOneSystemSkill(root, slug); err != nil {
			return fmt.Errorf("installing %s: %w", slug, err)
		}
	}
	return nil
}

func installOneSystemSkill(root, slug string) error {
	dst := filepath.Join(root, slug)
	if _, err := os.Stat(dst); err == nil {
		// Already present — do not overwrite.
		return nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}

	if err := os.MkdirAll(dst, 0700); err != nil {
		return err
	}

	// Copy every embedded file under system/<slug>/.
	srcPrefix := "system/" + slug + "/"
	if err := fs.WalkDir(systemFS, "system/"+slug, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p == "system/"+slug {
				return nil
			}
			return os.MkdirAll(filepath.Join(dst, strings.TrimPrefix(p, srcPrefix)), 0700)
		}
		data, err := systemFS.ReadFile(p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, strings.TrimPrefix(p, srcPrefix))
		return os.WriteFile(target, data, 0600)
	}); err != nil {
		return err
	}

	// Generate _meta.json from the skill's frontmatter.
	skillMD, err := os.ReadFile(filepath.Join(dst, "SKILL.md"))
	if err != nil {
		return err
	}
	fm, _, err := ParseFrontmatter(skillMD)
	if err != nil {
		return err
	}
	meta := Meta{
		Slug:                     fm.Name,
		Version:                  "system",
		InstalledAt:              time.Now().UTC().Format(time.RFC3339),
		ApprovedAt:               time.Now().UTC().Format(time.RFC3339),
		Fingerprint:              "sha256:system",
		ApprovedReadPermissions:  fm.Metadata.OpenClaw.Requires.ToolPermissions.Read,
		ApprovedWritePermissions: fm.Metadata.OpenClaw.Requires.ToolPermissions.Write,
	}
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(filepath.Join(dst, "_meta.json"), data, 0600)
}

func writeAtomic(path string, data []byte, mode os.FileMode) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
```

- [ ] **Step 4: Run tests**

```bash
cd daemon && go test ./internal/skills/... -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /home/racterub/github/gobrrr
git add daemon/internal/skills/bundled.go daemon/internal/skills/bundled_test.go
git commit -m "feat(skills): embed system skills and install to ~/.gobrrr/skills/ on start"
```

---

### Task 8: Wire skills block into worker prompt

**Files:**
- Modify: `daemon/internal/daemon/worker.go` (around lines 160-234)
- Modify: `daemon/internal/daemon/worker_test.go` (add coverage)

**Responsibility:** On daemon startup, `InstallSystemSkills` runs, a `*skills.Registry` is built. `WorkerPool.buildFullPrompt` prepends `<available_skills>` block after identity+memories and before the task prompt.

- [ ] **Step 1: Find the WorkerPool construction site**

```bash
grep -n 'NewWorkerPool\|wp.memStore' daemon/internal/daemon/worker.go daemon/internal/daemon/daemon.go
```

Identify where `WorkerPool` is constructed (daemon.go or similar) and where the gobrrr data dir is known.

- [ ] **Step 2: Add registry field to WorkerPool**

In `daemon/internal/daemon/worker.go`, locate the `WorkerPool` struct definition (near the top) and add:

```go
import (
    // ... existing imports
    "github.com/racterub/gobrrr/internal/skills"
)

type WorkerPool struct {
    // ... existing fields
    skillReg *skills.Registry
}
```

And update the constructor signature:

```go
func NewWorkerPool(cfg *config.Config, gobrrDir string, ms *memory.Store, skillReg *skills.Registry) *WorkerPool {
    // ...
    wp := &WorkerPool{
        // ... existing assignments
        skillReg: skillReg,
    }
    // ...
}
```

- [ ] **Step 3: Update buildFullPrompt to prepend skills block**

In `daemon/internal/daemon/worker.go`, replace the `buildFullPrompt` function:

```go
func (wp *WorkerPool) buildFullPrompt(taskPrompt string) string {
    ident, err := identity.Load(wp.gobrrDir)
    base := taskPrompt
    if err == nil {
        var memContents []string
        if wp.memStore != nil {
            all, err := wp.memStore.List(0)
            if err == nil && len(all) > 0 {
                relevant := memory.MatchRelevant(all, taskPrompt, 10)
                for _, e := range relevant {
                    memContents = append(memContents, e.Content)
                }
            }
        }
        base = identity.BuildPrompt(ident, memContents, taskPrompt)
    }

    if wp.skillReg == nil {
        return base
    }
    home, _ := os.UserHomeDir()
    block := skills.BuildPromptBlock(wp.skillReg.List(), home)
    if block == "" {
        return base
    }
    return block + "\n\n" + base
}
```

(Add `"os"` to imports if not already present.)

- [ ] **Step 4: Run daemon InstallSystemSkills + Refresh at startup**

Find where the daemon starts (likely `daemon/internal/daemon/daemon.go`, an `Init` or `NewDaemon` function) and add:

```go
// After gobrrDir is known, before WorkerPool is created:
skillsRoot := filepath.Join(gobrrDir, "skills")
if err := skills.InstallSystemSkills(skillsRoot); err != nil {
    log.Printf("daemon: install system skills: %v", err)
}
skillReg := skills.NewRegistry(skillsRoot)
if err := skillReg.Refresh(); err != nil {
    log.Printf("daemon: refresh skills: %v", err)
}
// Pass skillReg to NewWorkerPool.
```

(Exact location depends on existing daemon init flow — use the location where `memory.Store` is constructed as a guide; skillReg has the same lifecycle.)

- [ ] **Step 5: Update all NewWorkerPool call sites to pass skillReg (or nil)**

```bash
grep -rn 'NewWorkerPool' daemon/ --include='*.go'
```

For each caller:
- Production: pass the `skillReg` built in Step 4.
- Tests: pass `nil` — existing tests shouldn't need skills.

- [ ] **Step 6: Write a worker_test that verifies skills block is injected**

Append to `daemon/internal/daemon/worker_test.go` (create if it doesn't exist — check first):

```go
func TestBuildFullPrompt_IncludesSkillsBlock(t *testing.T) {
    root := t.TempDir()
    // Seed a single skill.
    require.NoError(t, os.MkdirAll(filepath.Join(root, "gmail"), 0700))
    require.NoError(t, os.WriteFile(filepath.Join(root, "gmail", "SKILL.md"),
        []byte("---\nname: gmail\ndescription: email\nmetadata:\n  gobrrr:\n    type: system\n---\n\nbody"), 0600))
    require.NoError(t, os.WriteFile(filepath.Join(root, "gmail", "_meta.json"),
        []byte(`{"slug":"gmail","version":"system","installed_at":"2026-01-01T00:00:00Z","fingerprint":"sha256:x","approved_read_permissions":[],"approved_write_permissions":[]}`), 0600))

    reg := skills.NewRegistry(root)
    require.NoError(t, reg.Refresh())

    wp := &WorkerPool{
        gobrrDir: t.TempDir(), // no identity here
        skillReg: reg,
    }

    got := wp.buildFullPrompt("do the thing")
    assert.Contains(t, got, "<available_skills>")
    assert.Contains(t, got, `name="gmail"`)
    assert.Contains(t, got, "do the thing")
}
```

- [ ] **Step 7: Run tests**

```bash
cd daemon && go test ./internal/daemon/... -v
cd daemon && go test ./internal/skills/... -v
cd daemon && CGO_ENABLED=0 go build -o /tmp/gobrrr-build-check ./cmd/gobrrr/
```

Expected: all tests PASS, build OK.

- [ ] **Step 8: Commit**

```bash
cd /home/racterub/github/gobrrr
git add -A daemon/internal/daemon/ daemon/internal/skills/
git commit -m "feat(daemon): inject <available_skills> block into worker prompt"
```

---

### Task 9: Merge skill permissions into per-task settings.json

**Files:**
- Modify: `daemon/internal/security/permissions.go`
- Modify: `daemon/internal/security/permissions_test.go`
- Modify: `daemon/internal/daemon/worker.go` (update `security.Generate` call site)

**Responsibility:** `security.Generate` accepts read/write permission lists from the skill registry. Read perms are merged into `allow` unconditionally; write perms only when `allowWrites` is true. Existing deny list is preserved verbatim.

- [ ] **Step 1: Write failing test**

Append to `daemon/internal/security/permissions_test.go`:

```go
func TestGenerate_MergesSkillReadPermissions(t *testing.T) {
    workers := t.TempDir()
    path, err := Generate(workers, "task-1", false,
        []string{"Bash(gh issue list:*)", "Bash(gobrrr gmail list:*)"},
        []string{"Bash(gh pr create:*)"})
    require.NoError(t, err)

    raw, err := os.ReadFile(path)
    require.NoError(t, err)
    var s settings
    require.NoError(t, json.Unmarshal(raw, &s))

    assert.Contains(t, s.Permissions.Allow, "Bash(gh issue list:*)")
    assert.Contains(t, s.Permissions.Allow, "Bash(gobrrr gmail list:*)")
    assert.NotContains(t, s.Permissions.Allow, "Bash(gh pr create:*)", "writes forbidden without allowWrites")
    assert.Contains(t, s.Permissions.Deny, "Bash(curl *)")
}

func TestGenerate_MergesSkillWritePermissionsWhenAllowed(t *testing.T) {
    workers := t.TempDir()
    path, err := Generate(workers, "task-2", true,
        []string{"Bash(gh issue list:*)"},
        []string{"Bash(gh pr create:*)"})
    require.NoError(t, err)

    raw, err := os.ReadFile(path)
    require.NoError(t, err)
    var s settings
    require.NoError(t, json.Unmarshal(raw, &s))

    assert.Contains(t, s.Permissions.Allow, "Bash(gh issue list:*)")
    assert.Contains(t, s.Permissions.Allow, "Bash(gh pr create:*)")
}

func TestGenerate_EmptySkillListsBehaviorUnchanged(t *testing.T) {
    workers := t.TempDir()
    path, err := Generate(workers, "task-3", false, nil, nil)
    require.NoError(t, err)

    raw, err := os.ReadFile(path)
    require.NoError(t, err)
    var s settings
    require.NoError(t, json.Unmarshal(raw, &s))
    // Default deny list still intact.
    assert.Contains(t, s.Permissions.Deny, "Bash(curl *)")
    assert.Contains(t, s.Permissions.Deny, "Write")
}
```

- [ ] **Step 2: Run to verify failure**

```bash
cd daemon && go test ./internal/security/... -v
```

Expected: FAIL — `Generate` signature mismatch.

- [ ] **Step 3: Extend Generate signature**

Replace `daemon/internal/security/permissions.go`:

```go
package security

import (
    "encoding/json"
    "os"
    "path/filepath"
)

type permissions struct {
    Allow []string `json:"allow"`
    Deny  []string `json:"deny"`
}

type settings struct {
    Permissions permissions `json:"permissions"`
}

// Generate creates a per-task settings.json for Claude Code workers.
//
// skillRead is merged into allow unconditionally. skillWrite is merged only
// when allowWrites is true. The baseline deny list is preserved in both modes.
func Generate(workersDir string, taskID string, allowWrites bool, skillRead, skillWrite []string) (string, error) {
    taskDir := filepath.Join(workersDir, taskID)
    if err := os.MkdirAll(taskDir, 0700); err != nil {
        return "", err
    }

    baseAllow := []string{
        "Bash(gobrrr *)",
        "Bash(agent-browser *)",
        "Read",
        "Glob",
        "Grep",
    }
    if allowWrites {
        baseAllow = append(baseAllow, "Write", "Edit")
    }
    baseDeny := []string{
        "Bash(curl *)",
        "Bash(wget *)",
        "Bash(claude *)",
    }
    if !allowWrites {
        baseDeny = append(baseDeny, "Write", "Edit")
    }

    allow := append([]string{}, baseAllow...)
    allow = append(allow, skillRead...)
    if allowWrites {
        allow = append(allow, skillWrite...)
    }

    s := settings{
        Permissions: permissions{
            Allow: allow,
            Deny:  baseDeny,
        },
    }

    data, err := json.MarshalIndent(s, "", "  ")
    if err != nil {
        return "", err
    }
    settingsPath := filepath.Join(taskDir, "settings.json")
    if err := os.WriteFile(settingsPath, data, 0600); err != nil {
        return "", err
    }
    return settingsPath, nil
}

// Cleanup removes the per-task settings directory.
func Cleanup(workersDir string, taskID string) error {
    taskDir := filepath.Join(workersDir, taskID)
    return os.RemoveAll(taskDir)
}
```

- [ ] **Step 4: Update worker.go call site**

In `daemon/internal/daemon/worker.go`, update the `security.Generate` call in `defaultBuildCommand`:

```go
// Collect skill permissions from registry.
var readPerms, writePerms []string
if wp.skillReg != nil {
    for _, sk := range wp.skillReg.List() {
        readPerms = append(readPerms, sk.ReadPermissions...)
        writePerms = append(writePerms, sk.WritePermissions...)
    }
}

workersDir := filepath.Join(wp.gobrrDir, "workers")
if settingsPath, err := security.Generate(workersDir, task.ID, task.AllowWrites, readPerms, writePerms); err == nil {
    args = append(args, "--settings", settingsPath)
}
```

- [ ] **Step 5: Update any other Generate callers**

```bash
grep -rn 'security\.Generate' daemon/ --include='*.go'
```

Update every caller to pass two new trailing args (pass `nil, nil` for tests that don't care about skills).

- [ ] **Step 6: Run tests + build**

```bash
cd daemon && go test ./... -v
cd daemon && CGO_ENABLED=0 go build -o /tmp/gobrrr-build-check ./cmd/gobrrr/
```

Expected: all tests PASS, build OK.

- [ ] **Step 7: Commit**

```bash
cd /home/racterub/github/gobrrr
git add daemon/internal/security/ daemon/internal/daemon/worker.go
git commit -m "feat(security): merge skill-declared permissions into per-task settings"
```

---

### Task 10: CLI `gobrrr skill list`

**Files:**
- Create: `daemon/internal/client/skill.go` (HTTP-over-unix-socket client methods)
- Modify: `daemon/internal/daemon/daemon.go` or equivalent route registration
- Modify: `daemon/cmd/gobrrr/main.go` (add `skill list` subcommand)

**Responsibility:** `gobrrr skill list` prints installed skills with type, version, path. Daemon exposes `GET /skills` returning the registry snapshot.

- [ ] **Step 1: Find the daemon's route registration**

```bash
grep -rn 'HandleFunc\|mux\.Handle' daemon/internal/daemon/ --include='*.go'
```

Identify the file where HTTP routes are registered.

- [ ] **Step 2: Add `GET /skills` handler**

In the daemon routes file (likely `daemon.go`), add:

```go
mux.HandleFunc("GET /skills", func(w http.ResponseWriter, r *http.Request) {
    if d.skillReg == nil {
        json.NewEncoder(w).Encode([]skills.Skill{})
        return
    }
    list := d.skillReg.List()
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(list)
})
```

(Ensure `d.skillReg` is a field on the daemon struct; wire it in the constructor alongside `memStore`.)

- [ ] **Step 3: Add client method**

Look at `daemon/internal/client/` for existing HTTP-over-unix-socket helpers. Create `daemon/internal/client/skill.go`:

```go
package client

import (
    "encoding/json"
    "fmt"
    "io"
    "net/http"

    "github.com/racterub/gobrrr/internal/skills"
)

func (c *Client) ListSkills() ([]skills.Skill, error) {
    resp, err := c.http.Get("http://unix/skills")
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()
    if resp.StatusCode != 200 {
        body, _ := io.ReadAll(resp.Body)
        return nil, fmt.Errorf("list skills: %s: %s", resp.Status, string(body))
    }
    var out []skills.Skill
    if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
        return nil, err
    }
    return out, nil
}
```

(Match the existing `Client` struct shape — check `daemon/internal/client/` for an existing `.http` field or equivalent.)

- [ ] **Step 4: Add `skill list` cobra subcommand**

In `daemon/cmd/gobrrr/main.go`, register:

```go
var skillCmd = &cobra.Command{Use: "skill", Short: "Manage worker skills"}

var skillListCmd = &cobra.Command{
    Use:   "list",
    Short: "List installed skills",
    RunE: func(cmd *cobra.Command, args []string) error {
        cli := client.New() // match existing client construction pattern
        list, err := cli.ListSkills()
        if err != nil {
            return err
        }
        if len(list) == 0 {
            fmt.Println("No skills installed.")
            return nil
        }
        // Group by type.
        byType := map[string][]skills.Skill{}
        for _, s := range list {
            byType[string(s.Type)] = append(byType[string(s.Type)], s)
        }
        for _, t := range []string{"system", "clawhub", "user"} {
            if len(byType[t]) == 0 {
                continue
            }
            fmt.Printf("[%s]\n", t)
            for _, s := range byType[t] {
                fmt.Printf("  %-20s  %s\n", s.Slug, s.Description)
            }
        }
        return nil
    },
}

func init() {
    skillCmd.AddCommand(skillListCmd)
    rootCmd.AddCommand(skillCmd)
}
```

- [ ] **Step 5: Manual test**

```bash
cd daemon && CGO_ENABLED=0 go build -o /tmp/gobrrr ./cmd/gobrrr/
# Start daemon (existing pattern)
/tmp/gobrrr daemon &
sleep 1
/tmp/gobrrr skill list
```

Expected output:
```
[system]
  browser              Agent-driven headless browser for web content extraction
  calendar             Google Calendar read/create/update via gobrrr CLI
  dispatch             Submit background tasks to the gobrrr daemon
  gmail                Email read/send/reply via gobrrr CLI
  homelab              Homelab service queries (Proxmox VMs, service health)
  memory               Persistent gobrrr memory store — search, add, list
  timer-management     Scheduled task management (cron-like recurring dispatches)
```

- [ ] **Step 6: Commit**

```bash
cd /home/racterub/github/gobrrr
git add -A daemon/
git commit -m "feat(cli): add gobrrr skill list command"
```

**Phase 1 complete.** System skills load from `~/.gobrrr/skills/`, appear in worker prompts, permissions merge correctly. Stop here and compact/continue before Phase 2.

---

## Phase 2: ClawHub Install + Approval Flow

Value at phase end: `gobrrr skill search/install/approve/deny/uninstall/update` works end-to-end against ClawHub. Workers immediately see newly installed ClawHub skills.

### Task 11: Verify ClawHub REST API surface (spike)

**Files:**
- Create: `docs/superpowers/notes/2026-04-19-clawhub-api.md`

**Responsibility:** Before coding the Go client, confirm the actual ClawHub HTTP endpoints, response schemas, tarball URL format, and whether any auth/cookies are required for public content.

- [ ] **Step 1: Pull the clawhub source**

```bash
cd /tmp && git clone --depth 1 https://github.com/openclaw/clawhub 2>/dev/null || (cd clawhub && git pull)
```

- [ ] **Step 2: Inspect server routes and registry schema**

```bash
grep -rn 'app\.get\|app\.post\|router\.\|export.*route' /tmp/clawhub/src/ /tmp/clawhub/api/ 2>/dev/null | head -40
grep -rn 'schema\|table\|defineTable\|defineSchema' /tmp/clawhub/convex/ /tmp/clawhub/src/ 2>/dev/null | head -20
```

Document in `docs/superpowers/notes/2026-04-19-clawhub-api.md`:
- Search endpoint URL/method/params + response JSON shape
- Skill metadata endpoint (`GET /api/skills/<slug>` or equivalent)
- Skill version endpoint
- Tarball URL pattern and whether it's the registry or a CDN
- Auth requirements (likely none for read, but verify)
- Rate limits (document any headers observed)

- [ ] **Step 3: Make three real requests**

```bash
curl -sv https://clawhub.com/api/skills/search?q=github | head -40
curl -sv https://clawhub.com/api/skills/github | head -40
```

Adjust endpoint URLs based on what the source review revealed. Capture request/response pairs in the note file.

- [ ] **Step 4: Commit the note**

```bash
cd /home/racterub/github/gobrrr
git add docs/superpowers/notes/2026-04-19-clawhub-api.md
git commit -m "docs(skills): document ClawHub REST API surface from source review"
```

If the API is substantially different from what the spec assumes, pause and update the spec before continuing.

---

### Task 12: ClawHub HTTP client

**Files:**
- Create: `daemon/internal/clawhub/types.go`
- Create: `daemon/internal/clawhub/client.go`
- Create: `daemon/internal/clawhub/client_test.go`

**Responsibility:** Pure Go HTTP client for Search, Fetch (metadata + version + zip bundle) with sha256 verification against `security.sha256hash`. Uses the V1 endpoints confirmed in Task 11.

> **API reference:** See `docs/superpowers/notes/2026-04-19-clawhub-api.md` for the full endpoint surface, real response shapes, and the rationale for the two-step Fetch (metadata→version→download). The default registry base is `https://clawhub.ai`. The download returns a ZIP (not a tarball) — we keep the method name `Fetch` but call the bytes a "bundle" and store them as `BundleBytes`.

- [ ] **Step 1: Write failing test with httptest fake server**

Create `daemon/internal/clawhub/client_test.go`:

```go
package clawhub

import (
    "crypto/sha256"
    "encoding/hex"
    "fmt"
    "net/http"
    "net/http/httptest"
    "testing"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

func TestClient_Search(t *testing.T) {
    mux := http.NewServeMux()
    mux.HandleFunc("/api/v1/search", func(w http.ResponseWriter, r *http.Request) {
        assert.Equal(t, "github", r.URL.Query().Get("q"))
        assert.Equal(t, "10", r.URL.Query().Get("limit"))
        w.Header().Set("Content-Type", "application/json")
        fmt.Fprint(w, `{"results":[
            {"score":3.7,"slug":"github","displayName":"Github","summary":"gh CLI ops","version":null,"updatedAt":1774865646622}
        ]}`)
    })
    srv := httptest.NewServer(mux)
    defer srv.Close()

    c := NewClient(srv.URL)
    results, err := c.Search("github", 10)
    require.NoError(t, err)
    require.Len(t, results, 1)
    assert.Equal(t, "github", results[0].Slug)
    assert.Equal(t, "Github", results[0].DisplayName)
    require.NotNil(t, results[0].Summary)
    assert.Equal(t, "gh CLI ops", *results[0].Summary)
    assert.Nil(t, results[0].Version, "search responses always return null version")
}

func TestClient_FetchAndVerifyBundle(t *testing.T) {
    zipBytes := []byte("fake zip bundle content, treat as opaque")
    sum := sha256.Sum256(zipBytes)
    hexSum := hex.EncodeToString(sum[:])

    mux := http.NewServeMux()
    // Metadata endpoint — resolves "latest" tag when caller passes version="".
    mux.HandleFunc("/api/v1/skills/github", func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "application/json")
        fmt.Fprint(w, `{
            "skill":{"slug":"github","displayName":"Github","summary":"gh ops","tags":{"latest":"1.0.0"}},
            "latestVersion":{"version":"1.0.0","createdAt":0,"changelog":"","license":null}
        }`)
    })
    // Version detail — carries the integrity hash under security.sha256hash.
    mux.HandleFunc("/api/v1/skills/github/versions/1.0.0", func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "application/json")
        fmt.Fprintf(w, `{
            "skill":{"slug":"github","displayName":"Github"},
            "version":{"version":"1.0.0","createdAt":0,"changelog":"","license":null,"files":[],
                "security":{"status":"clean","hasWarnings":false,"sha256hash":"%s"}
            }
        }`, hexSum)
    })
    // Download — raw application/zip stream.
    mux.HandleFunc("/api/v1/download", func(w http.ResponseWriter, r *http.Request) {
        assert.Equal(t, "github", r.URL.Query().Get("slug"))
        assert.Equal(t, "1.0.0", r.URL.Query().Get("version"))
        w.Header().Set("Content-Type", "application/zip")
        _, _ = w.Write(zipBytes)
    })
    srv := httptest.NewServer(mux)
    defer srv.Close()

    c := NewClient(srv.URL)
    pkg, err := c.Fetch("github", "1.0.0")
    require.NoError(t, err)
    assert.Equal(t, "github", pkg.Slug)
    assert.Equal(t, "1.0.0", pkg.Version)
    assert.Equal(t, hexSum, pkg.SHA256)
    assert.Equal(t, zipBytes, pkg.BundleBytes)
}

func TestClient_FetchResolvesLatestWhenVersionEmpty(t *testing.T) {
    zipBytes := []byte("latest zip bytes")
    sum := sha256.Sum256(zipBytes)
    hexSum := hex.EncodeToString(sum[:])

    mux := http.NewServeMux()
    mux.HandleFunc("/api/v1/skills/github", func(w http.ResponseWriter, r *http.Request) {
        fmt.Fprint(w, `{"skill":{"slug":"github","tags":{"latest":"2.1.0"}},"latestVersion":{"version":"2.1.0"}}`)
    })
    mux.HandleFunc("/api/v1/skills/github/versions/2.1.0", func(w http.ResponseWriter, r *http.Request) {
        fmt.Fprintf(w, `{"version":{"version":"2.1.0","security":{"status":"clean","sha256hash":"%s"}}}`, hexSum)
    })
    mux.HandleFunc("/api/v1/download", func(w http.ResponseWriter, r *http.Request) {
        assert.Equal(t, "2.1.0", r.URL.Query().Get("version"),
            "client must resolve latest version before calling download")
        _, _ = w.Write(zipBytes)
    })
    srv := httptest.NewServer(mux)
    defer srv.Close()

    c := NewClient(srv.URL)
    pkg, err := c.Fetch("github", "")
    require.NoError(t, err)
    assert.Equal(t, "2.1.0", pkg.Version)
}

func TestClient_BundleChecksumMismatch(t *testing.T) {
    mux := http.NewServeMux()
    mux.HandleFunc("/api/v1/skills/github", func(w http.ResponseWriter, r *http.Request) {
        fmt.Fprint(w, `{"skill":{"slug":"github"},"latestVersion":{"version":"1.0.0"}}`)
    })
    mux.HandleFunc("/api/v1/skills/github/versions/1.0.0", func(w http.ResponseWriter, r *http.Request) {
        fmt.Fprint(w, `{"version":{"version":"1.0.0","security":{"sha256hash":"deadbeef"}}}`)
    })
    mux.HandleFunc("/api/v1/download", func(w http.ResponseWriter, r *http.Request) {
        _, _ = w.Write([]byte("definitely not matching deadbeef"))
    })
    srv := httptest.NewServer(mux)
    defer srv.Close()

    c := NewClient(srv.URL)
    _, err := c.Fetch("github", "1.0.0")
    require.Error(t, err)
    assert.ErrorContains(t, err, "checksum mismatch")
}

func TestClient_SearchNon200(t *testing.T) {
    mux := http.NewServeMux()
    mux.HandleFunc("/api/v1/search", func(w http.ResponseWriter, r *http.Request) {
        http.Error(w, "rate limited", http.StatusTooManyRequests)
    })
    srv := httptest.NewServer(mux)
    defer srv.Close()

    _, err := NewClient(srv.URL).Search("x", 0)
    require.Error(t, err)
    assert.Contains(t, err.Error(), "429")
}
```

- [ ] **Step 2: Run to verify failure**

```bash
cd daemon && go test ./internal/clawhub/... -v
```

Expected: FAIL — package doesn't exist.

- [ ] **Step 3: Create `daemon/internal/clawhub/types.go`**

```go
// Package clawhub is a Go client for the ClawHub skill registry.
//
// API reference: docs/superpowers/notes/2026-04-19-clawhub-api.md
// Default base URL: https://clawhub.ai
package clawhub

// DefaultBaseURL is the canonical ClawHub registry.
// clawhub.com 307-redirects here; the ClawHub CLI hardcodes this value.
const DefaultBaseURL = "https://clawhub.ai"

// SkillSummary is one entry in the search results array.
// Version is always null in search responses; callers that need a concrete
// version must call Fetch (which reads the "latest" tag from metadata).
type SkillSummary struct {
    Score       float64 `json:"score"`
    Slug        string  `json:"slug"`
    DisplayName string  `json:"displayName"`
    Summary     *string `json:"summary"`
    Version     *string `json:"version"`
    UpdatedAt   int64   `json:"updatedAt"`
}

// searchResponse wraps the envelope returned by /api/v1/search.
type searchResponse struct {
    Results []SkillSummary `json:"results"`
}

// skillMetadata is the /api/v1/skills/<slug> response. We only unmarshal the
// fields needed to resolve "latest" and present the skill to users.
type skillMetadata struct {
    Skill struct {
        Slug        string            `json:"slug"`
        DisplayName string            `json:"displayName"`
        Summary     *string           `json:"summary"`
        Tags        map[string]string `json:"tags"`
    } `json:"skill"`
    LatestVersion *struct {
        Version string `json:"version"`
    } `json:"latestVersion"`
}

// versionDetail is the /api/v1/skills/<slug>/versions/<version> response.
// The integrity hash we verify against lives at version.security.sha256hash.
type versionDetail struct {
    Version struct {
        Version  string `json:"version"`
        Security *struct {
            Status     string  `json:"status"`
            SHA256Hash *string `json:"sha256hash"`
        } `json:"security"`
    } `json:"version"`
}

// SkillPackage is the resolved download: raw ZIP bytes plus integrity proof.
// Downstream Task 13 extracts BundleBytes with archive/zip.
type SkillPackage struct {
    Slug        string
    Version     string
    SHA256      string
    BundleBytes []byte
}
```

- [ ] **Step 4: Create `daemon/internal/clawhub/client.go`**

```go
package clawhub

import (
    "crypto/sha256"
    "encoding/hex"
    "encoding/json"
    "fmt"
    "io"
    "net/http"
    "net/url"
    "strconv"
    "strings"
    "time"
)

// Client talks to a ClawHub registry over HTTP. Zero auth required for reads.
type Client struct {
    baseURL string
    http    *http.Client
}

// NewClient returns a Client targeting baseURL (usually DefaultBaseURL).
// Passing "" uses DefaultBaseURL.
func NewClient(baseURL string) *Client {
    if baseURL == "" {
        baseURL = DefaultBaseURL
    }
    return &Client{
        baseURL: strings.TrimRight(baseURL, "/"),
        http:    &http.Client{Timeout: 30 * time.Second},
    }
}

// Search calls GET /api/v1/search?q=<query>&limit=<n> and returns the envelope's
// results array. limit<=0 omits the limit parameter (server default applies).
func (c *Client) Search(query string, limit int) ([]SkillSummary, error) {
    u, err := url.Parse(c.baseURL + "/api/v1/search")
    if err != nil {
        return nil, err
    }
    q := u.Query()
    q.Set("q", query)
    if limit > 0 {
        q.Set("limit", strconv.Itoa(limit))
    }
    u.RawQuery = q.Encode()

    resp, err := c.http.Get(u.String())
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()
    if resp.StatusCode != http.StatusOK {
        body, _ := io.ReadAll(resp.Body)
        return nil, fmt.Errorf("clawhub search %s: %s", resp.Status, string(body))
    }
    var env searchResponse
    if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
        return nil, fmt.Errorf("clawhub search: decode: %w", err)
    }
    return env.Results, nil
}

// Fetch resolves the version (falling back to tags.latest when empty),
// fetches the version detail to learn the bundle's sha256, downloads the
// ZIP bundle, and verifies the integrity hash.
//
// On hash mismatch, returns an error — the bundle bytes are never exposed.
func (c *Client) Fetch(slug, version string) (*SkillPackage, error) {
    if slug == "" {
        return nil, fmt.Errorf("clawhub: empty slug")
    }

    if version == "" {
        meta, err := c.getMetadata(slug)
        if err != nil {
            return nil, err
        }
        switch {
        case meta.LatestVersion != nil && meta.LatestVersion.Version != "":
            version = meta.LatestVersion.Version
        case meta.Skill.Tags["latest"] != "":
            version = meta.Skill.Tags["latest"]
        default:
            return nil, fmt.Errorf("clawhub: %s has no latest version", slug)
        }
    }

    detail, err := c.getVersionDetail(slug, version)
    if err != nil {
        return nil, err
    }
    if detail.Version.Security == nil || detail.Version.Security.SHA256Hash == nil || *detail.Version.Security.SHA256Hash == "" {
        return nil, fmt.Errorf("clawhub: %s@%s has no sha256 hash in version detail", slug, version)
    }
    expectedHash := *detail.Version.Security.SHA256Hash

    zipBytes, err := c.download(slug, version)
    if err != nil {
        return nil, err
    }
    sum := sha256.Sum256(zipBytes)
    got := hex.EncodeToString(sum[:])
    if got != expectedHash {
        return nil, fmt.Errorf("clawhub: checksum mismatch for %s@%s: got %s, expected %s", slug, version, got, expectedHash)
    }

    return &SkillPackage{
        Slug:        slug,
        Version:     version,
        SHA256:      expectedHash,
        BundleBytes: zipBytes,
    }, nil
}

func (c *Client) getMetadata(slug string) (*skillMetadata, error) {
    u := c.baseURL + "/api/v1/skills/" + url.PathEscape(slug)
    resp, err := c.http.Get(u)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()
    if resp.StatusCode != http.StatusOK {
        body, _ := io.ReadAll(resp.Body)
        return nil, fmt.Errorf("clawhub metadata %s: %s: %s", slug, resp.Status, string(body))
    }
    var out skillMetadata
    if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
        return nil, fmt.Errorf("clawhub metadata %s: decode: %w", slug, err)
    }
    return &out, nil
}

func (c *Client) getVersionDetail(slug, version string) (*versionDetail, error) {
    u := c.baseURL + "/api/v1/skills/" + url.PathEscape(slug) + "/versions/" + url.PathEscape(version)
    resp, err := c.http.Get(u)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()
    if resp.StatusCode != http.StatusOK {
        body, _ := io.ReadAll(resp.Body)
        return nil, fmt.Errorf("clawhub version %s@%s: %s: %s", slug, version, resp.Status, string(body))
    }
    var out versionDetail
    if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
        return nil, fmt.Errorf("clawhub version %s@%s: decode: %w", slug, version, err)
    }
    return &out, nil
}

func (c *Client) download(slug, version string) ([]byte, error) {
    u, err := url.Parse(c.baseURL + "/api/v1/download")
    if err != nil {
        return nil, err
    }
    q := u.Query()
    q.Set("slug", slug)
    q.Set("version", version)
    u.RawQuery = q.Encode()

    resp, err := c.http.Get(u.String())
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()
    if resp.StatusCode != http.StatusOK {
        body, _ := io.ReadAll(resp.Body)
        return nil, fmt.Errorf("clawhub download %s@%s: %s: %s", slug, version, resp.Status, string(body))
    }
    return io.ReadAll(resp.Body)
}
```

- [ ] **Step 5: Run tests**

```bash
cd daemon && go test ./internal/clawhub/... -v
```

Expected: PASS (all five tests).

- [ ] **Step 6: Commit**

```bash
cd /home/racterub/github/gobrrr
git add daemon/internal/clawhub/
git commit -m "feat(clawhub): add Go HTTP client for V1 search and bundle fetch"
```

---

### Task 13: Installer — stage + compose approval request

**Files:**
- Create: `daemon/internal/clawhub/installer.go`
- Create: `daemon/internal/clawhub/installer_test.go`

**Responsibility:** Given a fetched `SkillPackage`, unpack the tarball to a staging dir, parse frontmatter, detect missing binaries, compose an `InstallRequest`, and persist it to `<skillsRoot>/_requests/<req-id>.json`. Returns the request-id.

- [ ] **Step 1: Extend types.go**

Append to `daemon/internal/clawhub/types.go`:

```go
import (
    "time"

    "github.com/racterub/gobrrr/internal/skills"
)

type InstallRequest struct {
    RequestID        string             `json:"request_id"`
    Slug             string             `json:"slug"`
    Version          string             `json:"version"`
    SourceURL        string             `json:"source_url"`
    SHA256           string             `json:"sha256"`
    StagingDir       string             `json:"staging_dir"`
    Frontmatter      skills.Frontmatter `json:"frontmatter"`
    MissingBins      []string           `json:"missing_bins"`
    ProposedCommands []ProposedCommand  `json:"proposed_commands"`
    CreatedAt        time.Time          `json:"created_at"`
    ExpiresAt        time.Time          `json:"expires_at"`
}

type ProposedCommand struct {
    RecipeID string `json:"recipe_id"`
    Kind     string `json:"kind"` // brew|apt|npm|go|uv
    Command  string `json:"command"`
    Bins     []string `json:"bins"`
}
```

- [ ] **Step 2: Write failing test**

Create `daemon/internal/clawhub/installer_test.go`:

```go
package clawhub

import (
    "archive/tar"
    "bytes"
    "compress/gzip"
    "encoding/json"
    "os"
    "path/filepath"
    "testing"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

func TestInstaller_StagesAndWritesRequest(t *testing.T) {
    skillsRoot := t.TempDir()

    // Build a fake tarball with SKILL.md.
    skillMD := []byte(`---
name: github
description: GitHub ops
metadata:
  gobrrr:
    type: clawhub
  openclaw:
    requires:
      bins: [gh]
      tool_permissions:
        read:
          - "Bash(gh issue list:*)"
        write:
          - "Bash(gh pr create:*)"
    install:
      - id: gh-apt
        kind: apt
        package: gh-cli
        bins: [gh]
---

body
`)
    tarball := buildTarball(t, map[string][]byte{"SKILL.md": skillMD})
    pkg := &SkillPackage{
        Slug: "github", Version: "1.4.2",
        SHA256: "abc123", TarballBytes: tarball,
    }

    inst := NewInstaller(skillsRoot, func(bin string) bool {
        // Simulate gh not on PATH.
        return bin != "gh"
    })
    reqID, err := inst.Stage(pkg)
    require.NoError(t, err)
    require.NotEmpty(t, reqID)

    // Request persisted.
    data, err := os.ReadFile(filepath.Join(skillsRoot, "_requests", reqID+".json"))
    require.NoError(t, err)
    var req InstallRequest
    require.NoError(t, json.Unmarshal(data, &req))
    assert.Equal(t, "github", req.Slug)
    assert.Contains(t, req.MissingBins, "gh")
    require.Len(t, req.ProposedCommands, 1)
    assert.Equal(t, "apt", req.ProposedCommands[0].Kind)
    assert.Contains(t, req.ProposedCommands[0].Command, "gh-cli")

    // Staging dir contains SKILL.md.
    assert.FileExists(t, filepath.Join(req.StagingDir, "SKILL.md"))
}

func buildTarball(t *testing.T, files map[string][]byte) []byte {
    t.Helper()
    var buf bytes.Buffer
    gz := gzip.NewWriter(&buf)
    tw := tar.NewWriter(gz)
    for name, data := range files {
        require.NoError(t, tw.WriteHeader(&tar.Header{
            Name: name, Mode: 0600, Size: int64(len(data)),
        }))
        _, err := tw.Write(data)
        require.NoError(t, err)
    }
    require.NoError(t, tw.Close())
    require.NoError(t, gz.Close())
    return buf.Bytes()
}
```

- [ ] **Step 3: Run to verify failure**

```bash
cd daemon && go test ./internal/clawhub/... -run TestInstaller -v
```

Expected: FAIL.

- [ ] **Step 4a: Extend `skills.FMOpenClaw` with the install recipe list**

Append to `daemon/internal/skills/types.go`:

```go
type FMInstallRecipe struct {
    ID      string   `yaml:"id"`
    Kind    string   `yaml:"kind"`
    Formula string   `yaml:"formula,omitempty"`
    Package string   `yaml:"package,omitempty"`
    Module  string   `yaml:"module,omitempty"`
    URL     string   `yaml:"url,omitempty"`
    Bins    []string `yaml:"bins,omitempty"`
}
```

And modify the existing `FMOpenClaw` struct in the same file to add an `Install` field:

```go
type FMOpenClaw struct {
    Emoji    string            `yaml:"emoji,omitempty"`
    Homepage string            `yaml:"homepage,omitempty"`
    Requires FMRequires        `yaml:"requires"`
    Install  []FMInstallRecipe `yaml:"install,omitempty"`
}
```

- [ ] **Step 4b: Implement installer.go**

```go
package clawhub

import (
    "archive/tar"
    "bytes"
    "compress/gzip"
    "crypto/sha256"
    "encoding/hex"
    "encoding/json"
    "fmt"
    "io"
    "os"
    "path/filepath"
    "time"

    "github.com/racterub/gobrrr/internal/skills"
)

// BinChecker reports whether a binary exists on PATH. Defaults to exec.LookPath
// in production; tests inject a fake.
type BinChecker func(bin string) bool

type Installer struct {
    skillsRoot string
    hasBin     BinChecker
}

func NewInstaller(skillsRoot string, hasBin BinChecker) *Installer {
    return &Installer{skillsRoot: skillsRoot, hasBin: hasBin}
}

// Stage unpacks the tarball into a staging dir under _requests/<id>/, parses
// frontmatter, detects missing binaries, and writes _requests/<id>.json.
// Returns the request-id (first 4 hex chars of sha256, collision-checked).
func (in *Installer) Stage(pkg *SkillPackage) (string, error) {
    reqID, err := in.newRequestID(pkg)
    if err != nil {
        return "", err
    }

    stagingDir := filepath.Join(in.skillsRoot, "_requests", reqID+"_staging")
    if err := os.MkdirAll(stagingDir, 0700); err != nil {
        return "", err
    }
    if err := extractTarball(pkg.TarballBytes, stagingDir); err != nil {
        return "", err
    }

    skillMD, err := os.ReadFile(filepath.Join(stagingDir, "SKILL.md"))
    if err != nil {
        return "", fmt.Errorf("missing SKILL.md in tarball: %w", err)
    }
    fm, _, err := skills.ParseFrontmatter(skillMD)
    if err != nil {
        return "", fmt.Errorf("parse frontmatter: %w", err)
    }

    // Detect missing binaries.
    missing := []string{}
    for _, bin := range fm.Metadata.OpenClaw.Requires.Bins {
        if !in.hasBin(bin) {
            missing = append(missing, bin)
        }
    }

    // Select one install recipe per missing binary based on host.
    proposed := proposeCommands(fm, missing)

    req := InstallRequest{
        RequestID:        reqID,
        Slug:             pkg.Slug,
        Version:          pkg.Version,
        SourceURL:        pkg.TarballURL,
        SHA256:           pkg.SHA256,
        StagingDir:       stagingDir,
        Frontmatter:      *fm,
        MissingBins:      missing,
        ProposedCommands: proposed,
        CreatedAt:        time.Now().UTC(),
        ExpiresAt:        time.Now().UTC().Add(24 * time.Hour),
    }
    reqPath := filepath.Join(in.skillsRoot, "_requests", reqID+".json")
    if err := os.MkdirAll(filepath.Dir(reqPath), 0700); err != nil {
        return "", err
    }
    data, err := json.MarshalIndent(req, "", "  ")
    if err != nil {
        return "", err
    }
    if err := writeAtomic(reqPath, data, 0600); err != nil {
        return "", err
    }
    return reqID, nil
}

func (in *Installer) newRequestID(pkg *SkillPackage) (string, error) {
    // 4-char hex prefix of sha256 of slug@version+timestamp; retry on collision.
    for i := 0; i < 10; i++ {
        seed := fmt.Sprintf("%s@%s@%d@%d", pkg.Slug, pkg.Version, time.Now().UnixNano(), i)
        h := sha256.Sum256([]byte(seed))
        id := hex.EncodeToString(h[:])[:4]
        if _, err := os.Stat(filepath.Join(in.skillsRoot, "_requests", id+".json")); os.IsNotExist(err) {
            return id, nil
        }
    }
    return "", fmt.Errorf("could not allocate unique request id")
}

func extractTarball(data []byte, dst string) error {
    gz, err := gzip.NewReader(bytes.NewReader(data))
    if err != nil {
        return err
    }
    defer gz.Close()
    tr := tar.NewReader(gz)
    for {
        hdr, err := tr.Next()
        if err == io.EOF {
            return nil
        }
        if err != nil {
            return err
        }
        target := filepath.Join(dst, filepath.Clean("/"+hdr.Name))
        switch hdr.Typeflag {
        case tar.TypeDir:
            if err := os.MkdirAll(target, 0700); err != nil {
                return err
            }
        case tar.TypeReg:
            if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
                return err
            }
            f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
            if err != nil {
                return err
            }
            if _, err := io.Copy(f, tr); err != nil {
                f.Close()
                return err
            }
            f.Close()
        }
    }
}

// proposeCommands picks at most one recipe per missing binary, preferring
// the host's native package manager. Detection is deliberately simple; users
// see the exact command in the approval card and can decline.
func proposeCommands(fm *skills.Frontmatter, missingBins []string) []ProposedCommand {
    if len(missingBins) == 0 {
        return nil
    }
    preferred := detectPackageManager()
    var sudoPrefix string
    if preferred == "apt" || preferred == "apt-get" || preferred == "dnf" {
        sudoPrefix = "sudo "
    }
    missingSet := map[string]bool{}
    for _, b := range missingBins {
        missingSet[b] = true
    }

    var out []ProposedCommand
    for _, recipe := range fm.Metadata.OpenClaw.Install {
        if !matchesRecipe(recipe.Kind, preferred) {
            continue
        }
        if !suppliesMissing(recipe.Bins, missingSet) {
            continue
        }
        var cmd string
        switch recipe.Kind {
        case "brew":
            cmd = "brew install " + recipe.Formula
        case "apt", "apt-get":
            cmd = sudoPrefix + "apt install " + recipe.Package
        case "dnf":
            cmd = sudoPrefix + "dnf install " + recipe.Package
        case "npm":
            cmd = "npm install -g " + recipe.Package
        case "go":
            cmd = "go install " + recipe.Module
        case "uv":
            cmd = "uv tool install " + recipe.Package
        default:
            continue
        }
        out = append(out, ProposedCommand{
            RecipeID: recipe.ID,
            Kind:     recipe.Kind,
            Command:  cmd,
            Bins:     recipe.Bins,
        })
        for _, b := range recipe.Bins {
            delete(missingSet, b)
        }
    }
    return out
}

func matchesRecipe(kind, preferred string) bool {
    if preferred == "" {
        return true
    }
    if kind == preferred {
        return true
    }
    if kind == "apt" && preferred == "apt-get" {
        return true
    }
    if kind == "apt-get" && preferred == "apt" {
        return true
    }
    return false
}

func suppliesMissing(recipeBins []string, missing map[string]bool) bool {
    for _, b := range recipeBins {
        if missing[b] {
            return true
        }
    }
    return false
}

func detectPackageManager() string {
    for _, mgr := range []string{"brew", "apt", "apt-get", "dnf", "pacman"} {
        if _, err := os.Stat("/usr/bin/" + mgr); err == nil {
            return mgr
        }
        if _, err := os.Stat("/opt/homebrew/bin/" + mgr); err == nil {
            return mgr
        }
    }
    return ""
}

func writeAtomic(path string, data []byte, mode os.FileMode) error {
    tmp := path + ".tmp"
    if err := os.WriteFile(tmp, data, mode); err != nil {
        return err
    }
    return os.Rename(tmp, path)
}
```

- [ ] **Step 5: Run tests**

```bash
cd daemon && go test ./internal/clawhub/... -v ./internal/skills/... -v
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
cd /home/racterub/github/gobrrr
git add daemon/internal/clawhub/ daemon/internal/skills/types.go
git commit -m "feat(clawhub): stage tarball and compose install approval request"
```

---

### Task 14: Commit approved install (run binaries + copy + _meta.json)

**Files:**
- Create: `daemon/internal/clawhub/commit.go`
- Create: `daemon/internal/clawhub/commit_test.go`

**Responsibility:** Given a `RequestID` and user's decision (full approve / skip binary / deny), finalize the install: run approved shell commands, copy staging dir to `~/.gobrrr/skills/<slug>/`, write `_meta.json`, update `_lock.json`, trigger registry refresh.

- [ ] **Step 1: Write failing test**

Create `daemon/internal/clawhub/commit_test.go`:

```go
package clawhub

import (
    "encoding/json"
    "os"
    "os/exec"
    "path/filepath"
    "testing"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"

    "github.com/racterub/gobrrr/internal/skills"
)

func TestCommit_SkipBinaryPath(t *testing.T) {
    skillsRoot := t.TempDir()

    // Stage a simple install request by hand.
    req := &InstallRequest{
        RequestID:   "abcd",
        Slug:        "noop",
        Version:     "1.0.0",
        SourceURL:   "https://clawhub.com/noop",
        SHA256:      "sha256:test",
        StagingDir:  filepath.Join(skillsRoot, "_requests", "abcd_staging"),
        Frontmatter: skills.Frontmatter{
            Name:        "noop",
            Description: "does nothing",
            Metadata: skills.FMMetadata{
                Gobrrr: skills.FMGobrrr{Type: "clawhub"},
                OpenClaw: skills.FMOpenClaw{
                    Requires: skills.FMRequires{
                        ToolPermissions: skills.FMToolPermissions{
                            Read:  []string{"Bash(noop list:*)"},
                            Write: []string{"Bash(noop apply:*)"},
                        },
                    },
                },
            },
        },
    }
    require.NoError(t, os.MkdirAll(req.StagingDir, 0700))
    require.NoError(t, os.WriteFile(filepath.Join(req.StagingDir, "SKILL.md"),
        []byte("---\nname: noop\ndescription: does nothing\nmetadata:\n  gobrrr:\n    type: clawhub\n---\n\nbody"), 0600))

    require.NoError(t, os.MkdirAll(filepath.Join(skillsRoot, "_requests"), 0700))
    reqBytes, _ := json.MarshalIndent(req, "", "  ")
    require.NoError(t, os.WriteFile(filepath.Join(skillsRoot, "_requests", "abcd.json"), reqBytes, 0600))

    committer := NewCommitter(skillsRoot, fakeCmdRunner(nil))
    err := committer.Commit("abcd", Decision{Approve: true, SkipBinary: true})
    require.NoError(t, err)

    // Skill now installed.
    skillDir := filepath.Join(skillsRoot, "noop")
    assert.FileExists(t, filepath.Join(skillDir, "SKILL.md"))

    // _meta.json has correct permissions.
    metaBytes, err := os.ReadFile(filepath.Join(skillDir, "_meta.json"))
    require.NoError(t, err)
    var meta skills.Meta
    require.NoError(t, json.Unmarshal(metaBytes, &meta))
    assert.Equal(t, "noop", meta.Slug)
    assert.Contains(t, meta.ApprovedReadPermissions, "Bash(noop list:*)")
    assert.Contains(t, meta.ApprovedWritePermissions, "Bash(noop apply:*)")
    assert.Empty(t, meta.BinaryInstallCommands)

    // Staging dir removed.
    _, err = os.Stat(req.StagingDir)
    assert.True(t, os.IsNotExist(err))

    // Request file removed.
    _, err = os.Stat(filepath.Join(skillsRoot, "_requests", "abcd.json"))
    assert.True(t, os.IsNotExist(err))
}

func TestCommit_Deny_CleansStaging(t *testing.T) {
    skillsRoot := t.TempDir()
    stagingDir := filepath.Join(skillsRoot, "_requests", "abcd_staging")
    require.NoError(t, os.MkdirAll(stagingDir, 0700))
    reqPath := filepath.Join(skillsRoot, "_requests", "abcd.json")
    require.NoError(t, os.MkdirAll(filepath.Dir(reqPath), 0700))
    require.NoError(t, os.WriteFile(reqPath, []byte(`{"request_id":"abcd","staging_dir":"`+stagingDir+`"}`), 0600))

    committer := NewCommitter(skillsRoot, fakeCmdRunner(nil))
    require.NoError(t, committer.Commit("abcd", Decision{Approve: false}))

    _, err := os.Stat(stagingDir)
    assert.True(t, os.IsNotExist(err))
    _, err = os.Stat(reqPath)
    assert.True(t, os.IsNotExist(err))
}

// fakeCmdRunner returns a CmdRunner that records and never executes.
func fakeCmdRunner(ran *[]string) CmdRunner {
    return func(cmd string) (*exec.Cmd, error) {
        if ran != nil {
            *ran = append(*ran, cmd)
        }
        return exec.Command("true"), nil
    }
}
```

- [ ] **Step 2: Run to verify failure**

```bash
cd daemon && go test ./internal/clawhub/... -run TestCommit -v
```

Expected: FAIL.

- [ ] **Step 3: Implement commit.go**

```go
package clawhub

import (
    "crypto/sha256"
    "encoding/hex"
    "encoding/json"
    "errors"
    "fmt"
    "io/fs"
    "os"
    "os/exec"
    "path/filepath"
    "sort"
    "time"

    "github.com/racterub/gobrrr/internal/skills"
)

type Decision struct {
    Approve    bool
    SkipBinary bool
}

type CmdRunner func(cmd string) (*exec.Cmd, error)

type Committer struct {
    skillsRoot string
    run        CmdRunner
}

func NewCommitter(skillsRoot string, run CmdRunner) *Committer {
    if run == nil {
        run = defaultCmdRunner
    }
    return &Committer{skillsRoot: skillsRoot, run: run}
}

func (c *Committer) Commit(reqID string, decision Decision) error {
    reqPath := filepath.Join(c.skillsRoot, "_requests", reqID+".json")
    data, err := os.ReadFile(reqPath)
    if err != nil {
        return fmt.Errorf("load request %s: %w", reqID, err)
    }
    var req InstallRequest
    if err := json.Unmarshal(data, &req); err != nil {
        return err
    }

    // Deny path: remove staging + request, done.
    if !decision.Approve {
        _ = os.RemoveAll(req.StagingDir)
        _ = os.Remove(reqPath)
        return nil
    }

    // Run approved binary install commands.
    var records []skills.BinaryInstallRecord
    approvedBins := []string{}
    if !decision.SkipBinary {
        for _, p := range req.ProposedCommands {
            ran := time.Now().UTC().Format(time.RFC3339)
            cmd, err := c.run(p.Command)
            rec := skills.BinaryInstallRecord{
                RecipeID: p.RecipeID,
                Command:  p.Command,
                Approved: true,
                RanAt:    ran,
            }
            if err != nil {
                rec.ExitCode = -1
                records = append(records, rec)
                return fmt.Errorf("running %q: %w", p.Command, err)
            }
            if err := cmd.Run(); err != nil {
                if ee, ok := err.(*exec.ExitError); ok {
                    rec.ExitCode = ee.ExitCode()
                } else {
                    rec.ExitCode = -1
                }
                records = append(records, rec)
                return fmt.Errorf("command %q failed: %w", p.Command, err)
            }
            rec.ExitCode = 0
            records = append(records, rec)
            approvedBins = append(approvedBins, p.Bins...)
        }
    }

    // Copy staging to final location.
    dst := filepath.Join(c.skillsRoot, req.Slug)
    if err := os.RemoveAll(dst); err != nil && !errors.Is(err, fs.ErrNotExist) {
        return err
    }
    if err := copyDir(req.StagingDir, dst); err != nil {
        return err
    }

    // Compute fingerprint.
    fp, err := fingerprintDir(dst)
    if err != nil {
        return err
    }

    // Write _meta.json.
    meta := skills.Meta{
        Slug:                     req.Slug,
        Version:                  req.Version,
        SourceURL:                req.SourceURL,
        InstalledAt:              time.Now().UTC().Format(time.RFC3339),
        ApprovedAt:               time.Now().UTC().Format(time.RFC3339),
        Fingerprint:              "sha256:" + fp,
        ApprovedReadPermissions:  req.Frontmatter.Metadata.OpenClaw.Requires.ToolPermissions.Read,
        ApprovedWritePermissions: req.Frontmatter.Metadata.OpenClaw.Requires.ToolPermissions.Write,
        ApprovedBinaries:         approvedBins,
        BinaryInstallCommands:    records,
    }
    metaBytes, err := json.MarshalIndent(meta, "", "  ")
    if err != nil {
        return err
    }
    if err := writeAtomic(filepath.Join(dst, "_meta.json"), metaBytes, 0600); err != nil {
        return err
    }

    // Update lockfile.
    if err := updateLock(c.skillsRoot, req.Slug, req.Version, req.SHA256); err != nil {
        return err
    }

    // Cleanup staging + request.
    _ = os.RemoveAll(req.StagingDir)
    _ = os.Remove(reqPath)
    return nil
}

func defaultCmdRunner(cmdline string) (*exec.Cmd, error) {
    // Use /bin/sh -c so single quotes / pipes remain intact; the user already
    // saw and approved the exact string.
    return exec.Command("/bin/sh", "-c", cmdline), nil
}

func copyDir(src, dst string) error {
    return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
        if err != nil {
            return err
        }
        rel, _ := filepath.Rel(src, p)
        target := filepath.Join(dst, rel)
        if d.IsDir() {
            return os.MkdirAll(target, 0700)
        }
        data, err := os.ReadFile(p)
        if err != nil {
            return err
        }
        return os.WriteFile(target, data, 0600)
    })
}

func fingerprintDir(dir string) (string, error) {
    var paths []string
    if err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
        if err != nil || d.IsDir() {
            return err
        }
        rel, _ := filepath.Rel(dir, p)
        paths = append(paths, rel)
        return nil
    }); err != nil {
        return "", err
    }
    sort.Strings(paths)
    h := sha256.New()
    for _, rel := range paths {
        h.Write([]byte(rel))
        h.Write([]byte("\x00"))
        data, err := os.ReadFile(filepath.Join(dir, rel))
        if err != nil {
            return "", err
        }
        h.Write(data)
        h.Write([]byte("\x00"))
    }
    return hex.EncodeToString(h.Sum(nil)), nil
}

type lockFile struct {
    Skills map[string]lockEntry `json:"skills"`
}
type lockEntry struct {
    Version string `json:"version"`
    SHA256  string `json:"sha256"`
    Updated string `json:"updated"`
}

func updateLock(skillsRoot, slug, version, sha string) error {
    path := filepath.Join(skillsRoot, "_lock.json")
    var lf lockFile
    if data, err := os.ReadFile(path); err == nil {
        _ = json.Unmarshal(data, &lf)
    }
    if lf.Skills == nil {
        lf.Skills = map[string]lockEntry{}
    }
    lf.Skills[slug] = lockEntry{Version: version, SHA256: sha, Updated: time.Now().UTC().Format(time.RFC3339)}
    out, err := json.MarshalIndent(lf, "", "  ")
    if err != nil {
        return err
    }
    return writeAtomic(path, out, 0600)
}
```

- [ ] **Step 4: Run tests**

```bash
cd daemon && go test ./internal/clawhub/... -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /home/racterub/github/gobrrr
git add daemon/internal/clawhub/commit.go daemon/internal/clawhub/commit_test.go
git commit -m "feat(clawhub): commit approved install with binary run, copy, lockfile"
```

---

### Task 15: Daemon HTTP endpoints for skill lifecycle + CLI

**Files:**
- Modify: `daemon/internal/daemon/daemon.go` (register routes + keep `d.skillReg`, `d.clawhub` fields)
- Create: `daemon/internal/daemon/skill_routes.go` (handlers)
- Modify: `daemon/internal/client/skill.go` (add lifecycle methods)
- Modify: `daemon/cmd/gobrrr/main.go` (add subcommands: search, install, approve, deny, uninstall, info)

**Responsibility:** Expose `POST /skills/install`, `POST /skills/approve/:id`, `POST /skills/deny/:id`, `DELETE /skills/:slug`, `GET /skills/search?q=`, `GET /skills/:slug`. CLI wraps each.

- [ ] **Step 1: Add daemon fields**

In `daemon.go`:

```go
type Daemon struct {
    // ... existing fields
    skillsRoot string
    skillReg   *skills.Registry
    clawhub    *clawhub.Client
    installer  *clawhub.Installer
    committer  *clawhub.Committer
}
```

Constructor wiring:

```go
d.skillsRoot = filepath.Join(gobrrDir, "skills")
_ = skills.InstallSystemSkills(d.skillsRoot)
d.skillReg = skills.NewRegistry(d.skillsRoot)
_ = d.skillReg.Refresh()

clawhubURL := cfg.ClawHub.RegistryURL
if clawhubURL == "" {
    clawhubURL = "https://clawhub.com"
}
d.clawhub = clawhub.NewClient(clawhubURL)
d.installer = clawhub.NewInstaller(d.skillsRoot, binOnPath)
d.committer = clawhub.NewCommitter(d.skillsRoot, nil)

func binOnPath(bin string) bool {
    _, err := exec.LookPath(bin)
    return err == nil
}
```

Add `ClawHub` config section in `daemon/internal/config/`:

```go
type ClawHubConfig struct {
    RegistryURL string `json:"registry_url"`
}

type Config struct {
    // ... existing fields
    ClawHub ClawHubConfig `json:"clawhub"`
}
```

- [ ] **Step 2: Implement skill_routes.go**

```go
package daemon

import (
    "encoding/json"
    "fmt"
    "net/http"
    "os"
    "path/filepath"

    "github.com/racterub/gobrrr/internal/clawhub"
)

func (d *Daemon) registerSkillRoutes(mux *http.ServeMux) {
    mux.HandleFunc("GET /skills", d.handleSkillsList)
    mux.HandleFunc("GET /skills/search", d.handleSkillsSearch)
    mux.HandleFunc("POST /skills/install", d.handleSkillsInstall)
    mux.HandleFunc("POST /skills/approve/{id}", d.handleSkillsApprove)
    mux.HandleFunc("POST /skills/deny/{id}", d.handleSkillsDeny)
    mux.HandleFunc("DELETE /skills/{slug}", d.handleSkillsUninstall)
}

func (d *Daemon) handleSkillsList(w http.ResponseWriter, r *http.Request) {
    writeJSON(w, d.skillReg.List())
}

func (d *Daemon) handleSkillsSearch(w http.ResponseWriter, r *http.Request) {
    q := r.URL.Query().Get("q")
    limit := 20
    results, err := d.clawhub.Search(q, limit)
    if err != nil {
        http.Error(w, err.Error(), 502)
        return
    }
    writeJSON(w, results)
}

type installReq struct {
    Slug    string `json:"slug"`
    Version string `json:"version,omitempty"`
}

func (d *Daemon) handleSkillsInstall(w http.ResponseWriter, r *http.Request) {
    var body installReq
    if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
        http.Error(w, err.Error(), 400)
        return
    }
    pkg, err := d.clawhub.Fetch(body.Slug, body.Version)
    if err != nil {
        http.Error(w, err.Error(), 502)
        return
    }
    reqID, err := d.installer.Stage(pkg)
    if err != nil {
        http.Error(w, err.Error(), 500)
        return
    }
    // Return the install request so CLI can render the approval card.
    reqPath := filepath.Join(d.skillsRoot, "_requests", reqID+".json")
    data, err := os.ReadFile(reqPath)
    if err != nil {
        http.Error(w, err.Error(), 500)
        return
    }
    var req clawhub.InstallRequest
    _ = json.Unmarshal(data, &req)
    writeJSON(w, map[string]any{
        "request_id": reqID,
        "request":    req,
    })
}

type approveReq struct {
    SkipBinary bool `json:"skip_binary,omitempty"`
}

func (d *Daemon) handleSkillsApprove(w http.ResponseWriter, r *http.Request) {
    id := r.PathValue("id")
    var body approveReq
    _ = json.NewDecoder(r.Body).Decode(&body)
    if err := d.committer.Commit(id, clawhub.Decision{Approve: true, SkipBinary: body.SkipBinary}); err != nil {
        http.Error(w, err.Error(), 500)
        return
    }
    if err := d.skillReg.Refresh(); err != nil {
        http.Error(w, err.Error(), 500)
        return
    }
    fmt.Fprintln(w, "approved")
}

func (d *Daemon) handleSkillsDeny(w http.ResponseWriter, r *http.Request) {
    id := r.PathValue("id")
    if err := d.committer.Commit(id, clawhub.Decision{Approve: false}); err != nil {
        http.Error(w, err.Error(), 500)
        return
    }
    fmt.Fprintln(w, "denied")
}

func (d *Daemon) handleSkillsUninstall(w http.ResponseWriter, r *http.Request) {
    slug := r.PathValue("slug")
    dir := filepath.Join(d.skillsRoot, slug)
    if err := os.RemoveAll(dir); err != nil {
        http.Error(w, err.Error(), 500)
        return
    }
    if err := d.skillReg.Refresh(); err != nil {
        http.Error(w, err.Error(), 500)
        return
    }
    fmt.Fprintf(w, "uninstalled %s\n", slug)
}

func writeJSON(w http.ResponseWriter, v any) {
    w.Header().Set("Content-Type", "application/json")
    _ = json.NewEncoder(w).Encode(v)
}
```

Wire `d.registerSkillRoutes(mux)` in the daemon's existing route registration function.

- [ ] **Step 3: Extend client**

Open `daemon/internal/client/skill.go` and merge these imports into the existing `import (...)` block (do not add a second import block):

```go
"bytes"
"encoding/json"
"fmt"
"io"
"net/http"
"net/url"

"github.com/racterub/gobrrr/internal/clawhub"
```

Then append the following functions to the same file:

```go
func (c *Client) SearchSkills(q string) ([]clawhub.SkillSummary, error) {
    resp, err := c.http.Get("http://unix/skills/search?q=" + url.QueryEscape(q))
    if err != nil { return nil, err }
    defer resp.Body.Close()
    if resp.StatusCode != 200 {
        body, _ := io.ReadAll(resp.Body)
        return nil, fmt.Errorf("search: %s: %s", resp.Status, string(body))
    }
    var out []clawhub.SkillSummary
    return out, json.NewDecoder(resp.Body).Decode(&out)
}

type InstallResult struct {
    RequestID string                  `json:"request_id"`
    Request   clawhub.InstallRequest  `json:"request"`
}

func (c *Client) InstallSkill(slug, version string) (*InstallResult, error) {
    body, _ := json.Marshal(map[string]string{"slug": slug, "version": version})
    resp, err := c.http.Post("http://unix/skills/install", "application/json", bytes.NewReader(body))
    if err != nil { return nil, err }
    defer resp.Body.Close()
    if resp.StatusCode != 200 {
        b, _ := io.ReadAll(resp.Body)
        return nil, fmt.Errorf("install: %s: %s", resp.Status, string(b))
    }
    var out InstallResult
    return &out, json.NewDecoder(resp.Body).Decode(&out)
}

func (c *Client) ApproveSkill(reqID string, skipBinary bool) error {
    body, _ := json.Marshal(map[string]bool{"skip_binary": skipBinary})
    return c.postSimple("http://unix/skills/approve/"+reqID, body)
}

func (c *Client) DenySkill(reqID string) error {
    return c.postSimple("http://unix/skills/deny/"+reqID, nil)
}

func (c *Client) UninstallSkill(slug string) error {
    req, _ := http.NewRequest("DELETE", "http://unix/skills/"+slug, nil)
    resp, err := c.http.Do(req)
    if err != nil { return err }
    defer resp.Body.Close()
    if resp.StatusCode != 200 {
        b, _ := io.ReadAll(resp.Body)
        return fmt.Errorf("uninstall: %s: %s", resp.Status, string(b))
    }
    return nil
}

func (c *Client) postSimple(url string, body []byte) error {
    resp, err := c.http.Post(url, "application/json", bytes.NewReader(body))
    if err != nil { return err }
    defer resp.Body.Close()
    if resp.StatusCode != 200 {
        b, _ := io.ReadAll(resp.Body)
        return fmt.Errorf("%s: %s", resp.Status, string(b))
    }
    return nil
}
```

- [ ] **Step 4: Add CLI subcommands**

In `daemon/cmd/gobrrr/main.go`:

```go
var skillSearchCmd = &cobra.Command{
    Use:   "search <query>",
    Short: "Search ClawHub for skills",
    Args:  cobra.ExactArgs(1),
    RunE: func(cmd *cobra.Command, args []string) error {
        cli := client.New()
        results, err := cli.SearchSkills(args[0])
        if err != nil {
            return err
        }
        if len(results) == 0 {
            fmt.Println("No matches.")
            return nil
        }
        for _, r := range results {
            fmt.Printf("%-24s %-10s  %s\n", r.Slug, r.Version, r.Description)
        }
        return nil
    },
}

var skillInstallCmd = &cobra.Command{
    Use:   "install <slug>[@version]",
    Short: "Stage a ClawHub skill install (prints approval card)",
    Args:  cobra.ExactArgs(1),
    RunE: func(cmd *cobra.Command, args []string) error {
        slug, version := parseSlugVersion(args[0])
        cli := client.New()
        res, err := cli.InstallSkill(slug, version)
        if err != nil {
            return err
        }
        printApprovalCard(res)
        return nil
    },
}

var skillApproveCmd = &cobra.Command{
    Use:   "approve <request-id>",
    Short: "Approve a staged skill install",
    Args:  cobra.ExactArgs(1),
    RunE: func(cmd *cobra.Command, args []string) error {
        skip, _ := cmd.Flags().GetBool("skip-binary")
        return client.New().ApproveSkill(args[0], skip)
    },
}

var skillDenyCmd = &cobra.Command{
    Use:   "deny <request-id>",
    Short: "Deny a staged skill install",
    Args:  cobra.ExactArgs(1),
    RunE: func(cmd *cobra.Command, args []string) error {
        return client.New().DenySkill(args[0])
    },
}

var skillUninstallCmd = &cobra.Command{
    Use:   "uninstall <slug>",
    Short: "Uninstall a skill",
    Args:  cobra.ExactArgs(1),
    RunE: func(cmd *cobra.Command, args []string) error {
        return client.New().UninstallSkill(args[0])
    },
}

func init() {
    skillApproveCmd.Flags().Bool("skip-binary", false, "approve skill only, skip binary install commands")
    skillCmd.AddCommand(skillSearchCmd, skillInstallCmd, skillApproveCmd, skillDenyCmd, skillUninstallCmd)
}

func parseSlugVersion(s string) (string, string) {
    if i := strings.Index(s, "@"); i > 0 {
        return s[:i], s[i+1:]
    }
    return s, ""
}

func printApprovalCard(r *client.InstallResult) {
    req := r.Request
    fmt.Printf("Install skill: %s@%s\n", req.Slug, req.Version)
    fmt.Printf("  Source: %s  sha256: %s\n", req.SourceURL, req.SHA256)
    fmt.Printf("  Description: %s\n\n", req.Frontmatter.Description)

    if len(req.MissingBins) > 0 {
        fmt.Printf("  Requires binaries: %s  (not on PATH)\n", strings.Join(req.MissingBins, ", "))
        for _, p := range req.ProposedCommands {
            fmt.Printf("    Proposed install:  %s\n", p.Command)
        }
        fmt.Println()
    }

    reads := req.Frontmatter.Metadata.OpenClaw.Requires.ToolPermissions.Read
    writes := req.Frontmatter.Metadata.OpenClaw.Requires.ToolPermissions.Write
    if len(reads) > 0 {
        fmt.Println("  Tool permissions (read, always allowed):")
        for _, p := range reads {
            fmt.Printf("    %s\n", p)
        }
    }
    if len(writes) > 0 {
        fmt.Println("\n  Tool permissions (write, require --allow-writes on task):")
        for _, p := range writes {
            fmt.Printf("    %s\n", p)
        }
    }

    fmt.Printf("\n  Request ID: %s\n\n", r.RequestID)
    fmt.Printf("  To proceed:  gobrrr skill approve %s\n", r.RequestID)
    fmt.Printf("  Skill only:  gobrrr skill approve %s --skip-binary\n", r.RequestID)
    fmt.Printf("  Cancel:      gobrrr skill deny %s\n", r.RequestID)
}
```

- [ ] **Step 5: Run tests + build**

```bash
cd daemon && go test ./... -v
cd daemon && CGO_ENABLED=0 go build -o /tmp/gobrrr ./cmd/gobrrr/
```

Expected: all tests PASS, build OK.

- [ ] **Step 6: Commit**

```bash
cd /home/racterub/github/gobrrr
git add -A daemon/
git commit -m "feat(skill): add install/approve/deny/uninstall/search CLI + daemon routes"
```

---

### Task 16: Cleanup expired install requests

**Files:**
- Modify: `daemon/internal/daemon/maintenance.go`
- Modify: `daemon/internal/daemon/maintenance_test.go`

**Responsibility:** Existing hourly maintenance sweep also cleans `_requests/<id>.json` files whose `expires_at` is in the past, plus any orphan `<id>_staging/` directories.

- [ ] **Step 1: Write failing test**

Append to `daemon/internal/daemon/maintenance_test.go`:

```go
func TestMaintenance_ExpiresOldInstallRequests(t *testing.T) {
    skillsRoot := t.TempDir()
    reqDir := filepath.Join(skillsRoot, "_requests")
    require.NoError(t, os.MkdirAll(reqDir, 0700))

    // Fresh request (expires tomorrow).
    fresh := filepath.Join(reqDir, "fresh.json")
    require.NoError(t, os.WriteFile(fresh, []byte(`{"request_id":"fresh","expires_at":"2030-01-01T00:00:00Z","staging_dir":""}`), 0600))

    // Expired request + staging dir.
    stale := filepath.Join(reqDir, "stale.json")
    staleStaging := filepath.Join(reqDir, "stale_staging")
    require.NoError(t, os.MkdirAll(staleStaging, 0700))
    require.NoError(t, os.WriteFile(stale, []byte(`{"request_id":"stale","expires_at":"2020-01-01T00:00:00Z","staging_dir":"`+staleStaging+`"}`), 0600))

    require.NoError(t, PruneExpiredInstallRequests(skillsRoot))

    _, err := os.Stat(fresh)
    assert.NoError(t, err, "fresh request should survive")
    _, err = os.Stat(stale)
    assert.True(t, os.IsNotExist(err), "stale request should be removed")
    _, err = os.Stat(staleStaging)
    assert.True(t, os.IsNotExist(err), "stale staging dir should be removed")
}
```

- [ ] **Step 2: Run to verify failure**

```bash
cd daemon && go test ./internal/daemon/... -run TestMaintenance_Expires -v
```

Expected: FAIL.

- [ ] **Step 3: Implement**

Append to `daemon/internal/daemon/maintenance.go`:

```go
import (
    "encoding/json"
    "os"
    "path/filepath"
    "strings"
    "time"
)

type pendingRequest struct {
    RequestID  string    `json:"request_id"`
    ExpiresAt  time.Time `json:"expires_at"`
    StagingDir string    `json:"staging_dir"`
}

// PruneExpiredInstallRequests removes expired skill install requests and their
// staging directories. Safe to call concurrently with the installer.
func PruneExpiredInstallRequests(skillsRoot string) error {
    reqDir := filepath.Join(skillsRoot, "_requests")
    entries, err := os.ReadDir(reqDir)
    if err != nil {
        if os.IsNotExist(err) {
            return nil
        }
        return err
    }
    now := time.Now().UTC()
    for _, e := range entries {
        if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
            continue
        }
        path := filepath.Join(reqDir, e.Name())
        data, err := os.ReadFile(path)
        if err != nil {
            continue
        }
        var req pendingRequest
        if err := json.Unmarshal(data, &req); err != nil {
            continue
        }
        if req.ExpiresAt.IsZero() || req.ExpiresAt.After(now) {
            continue
        }
        _ = os.Remove(path)
        if req.StagingDir != "" {
            _ = os.RemoveAll(req.StagingDir)
        }
    }
    return nil
}
```

Then in the existing `RunMaintenance` (or equivalent hourly function), add:

```go
_ = PruneExpiredInstallRequests(filepath.Join(gobrrDir, "skills"))
```

- [ ] **Step 4: Run tests**

```bash
cd daemon && go test ./internal/daemon/... -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /home/racterub/github/gobrrr
git add daemon/internal/daemon/maintenance.go daemon/internal/daemon/maintenance_test.go
git commit -m "feat(maintenance): prune expired skill install requests hourly"
```

---

### Task 17: End-to-end integration test

**Files:**
- Create: `daemon/internal/daemon/skill_e2e_test.go`

**Responsibility:** Start a `httptest.NewServer` fake ClawHub, start a real daemon against a tempdir, run `install` → `approve` → verify installed skill appears in registry and worker prompt.

- [ ] **Step 1: Write the test**

Create `daemon/internal/daemon/skill_e2e_test.go`:

```go
package daemon

import (
    "archive/tar"
    "bytes"
    "compress/gzip"
    "crypto/sha256"
    "encoding/hex"
    "fmt"
    "net/http"
    "net/http/httptest"
    "os"
    "testing"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"

    "github.com/racterub/gobrrr/internal/clawhub"
    "github.com/racterub/gobrrr/internal/security"
    "github.com/racterub/gobrrr/internal/skills"
)

func TestE2E_InstallApproveSkill(t *testing.T) {
    // Build a fake skill tarball.
    skillMD := []byte(`---
name: noop
description: does nothing
metadata:
  gobrrr:
    type: clawhub
  openclaw:
    requires:
      tool_permissions:
        read:
          - "Bash(echo:*)"
        write: []
---

body
`)
    tarball := buildTarballE2E(t, map[string][]byte{"SKILL.md": skillMD})
    sum := sha256.Sum256(tarball)
    hexSum := hex.EncodeToString(sum[:])

    mux := http.NewServeMux()
    mux.HandleFunc("/api/skills/noop", func(w http.ResponseWriter, r *http.Request) {
        fmt.Fprintf(w, `{"slug":"noop","version":"1.0.0","sha256":"%s","tarball_url":"/tb/noop-1.0.0.tar.gz"}`, hexSum)
    })
    mux.HandleFunc("/tb/noop-1.0.0.tar.gz", func(w http.ResponseWriter, r *http.Request) {
        w.Write(tarball)
    })
    fakeHub := httptest.NewServer(mux)
    defer fakeHub.Close()

    skillsRoot := t.TempDir()

    c := clawhub.NewClient(fakeHub.URL)
    pkg, err := c.Fetch("noop", "1.0.0")
    require.NoError(t, err)

    inst := clawhub.NewInstaller(skillsRoot, func(string) bool { return true })
    reqID, err := inst.Stage(pkg)
    require.NoError(t, err)

    cm := clawhub.NewCommitter(skillsRoot, nil)
    require.NoError(t, cm.Commit(reqID, clawhub.Decision{Approve: true, SkipBinary: true}))

    reg := skills.NewRegistry(skillsRoot)
    require.NoError(t, reg.Refresh())
    list := reg.List()
    require.Len(t, list, 1)
    assert.Equal(t, "noop", list[0].Slug)
    assert.Equal(t, skills.TypeClawhub, list[0].Type)
    assert.Contains(t, list[0].ReadPermissions, "Bash(echo:*)")

    home, _ := os.UserHomeDir()
    block := skills.BuildPromptBlock(list, home)
    assert.Contains(t, block, `name="noop"`)

    // Verify settings.json merges the read permission.
    workers := t.TempDir()
    var read, write []string
    for _, s := range list {
        read = append(read, s.ReadPermissions...)
        write = append(write, s.WritePermissions...)
    }
    settingsPath, err := security.Generate(workers, "task-1", false, read, write)
    require.NoError(t, err)
    raw, err := os.ReadFile(settingsPath)
    require.NoError(t, err)
    assert.Contains(t, string(raw), "Bash(echo:*)")
}

func buildTarballE2E(t *testing.T, files map[string][]byte) []byte {
    t.Helper()
    var buf bytes.Buffer
    gz := gzip.NewWriter(&buf)
    tw := tar.NewWriter(gz)
    for name, data := range files {
        require.NoError(t, tw.WriteHeader(&tar.Header{
            Name: name, Mode: 0600, Size: int64(len(data)),
        }))
        _, err := tw.Write(data)
        require.NoError(t, err)
    }
    require.NoError(t, tw.Close())
    require.NoError(t, gz.Close())
    return buf.Bytes()
}
```

- [ ] **Step 2: Run**

```bash
cd daemon && go test ./internal/daemon/... -run TestE2E -v
```

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
cd /home/racterub/github/gobrrr
git add daemon/internal/daemon/skill_e2e_test.go
git commit -m "test(skill): end-to-end install → approve → prompt injection"
```

---

## Final Verification

After Phase 2 completes:

- [ ] **Run all tests**

```bash
cd daemon && go test ./... -v
```

- [ ] **Full build**

```bash
cd daemon && CGO_ENABLED=0 go build -o /tmp/gobrrr ./cmd/gobrrr/
/tmp/gobrrr --help
/tmp/gobrrr skill --help
```

- [ ] **Manual smoke test against a real daemon**

```bash
/tmp/gobrrr daemon &
sleep 1
/tmp/gobrrr skill list            # system skills appear
/tmp/gobrrr skill search github   # (may fail if ClawHub API mismatch — see Task 11)
```

- [ ] **Update CLAUDE.md**

Document the new `internal/skills/` and `internal/clawhub/` packages under "Project Structure" and add a "Skills" section explaining the install flow.

- [ ] **Confirm acceptance criteria from spec**

Walk `docs/superpowers/specs/2026-04-19-skill-ecosystem-design.md` § Acceptance Criteria — tick each item that's demonstrably met.
