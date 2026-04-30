package skills

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/racterub/gobrrr/internal/atomicfs"
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
	return atomicfs.WriteJSON(filepath.Join(dst, "_meta.json"), meta, 0600)
}
