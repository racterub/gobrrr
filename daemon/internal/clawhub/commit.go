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

// Decision carries the user's response to an InstallRequest. Approve=false
// is a deny (cleanup and exit). Approve=true with SkipBinary=true installs
// the skill without running any proposed binary commands.
type Decision struct {
	Approve    bool
	SkipBinary bool
}

// CmdRunner builds the *exec.Cmd for an approved install command. Tests
// inject a fake runner; production uses defaultCmdRunner which invokes
// `/bin/sh -c`.
type CmdRunner func(cmd string) (*exec.Cmd, error)

// Committer finalizes a staged skill install given a user decision.
type Committer struct {
	skillsRoot string
	run        CmdRunner
}

// NewCommitter returns a Committer rooted at skillsRoot. A nil run falls
// back to defaultCmdRunner.
func NewCommitter(skillsRoot string, run CmdRunner) *Committer {
	if run == nil {
		run = defaultCmdRunner
	}
	return &Committer{skillsRoot: skillsRoot, run: run}
}

// Commit reads the InstallRequest for reqID and either cleans up (deny) or
// runs approved commands, copies staging to <skillsRoot>/<slug>/, writes
// _meta.json, updates _lock.json, and clears the staging/request artifacts.
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
