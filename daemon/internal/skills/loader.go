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
	Slug                     string               `json:"slug"`
	Version                  string               `json:"version"`
	SourceURL                string               `json:"source_url,omitempty"`
	InstalledAt              string               `json:"installed_at"`
	ApprovedAt               string               `json:"approved_at,omitempty"`
	Fingerprint              string               `json:"fingerprint"`
	ApprovedReadPermissions  []string             `json:"approved_read_permissions"`
	ApprovedWritePermissions []string             `json:"approved_write_permissions"`
	ApprovedBinaries         []string             `json:"approved_binaries,omitempty"`
	BinaryInstallCommands    []BinaryInstallRecord `json:"binary_install_commands,omitempty"`
}

// BinaryInstallRecord captures a binary install recipe that was run at approval time.
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
