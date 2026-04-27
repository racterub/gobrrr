package clawhub

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/racterub/gobrrr/internal/skills"
)

// BinChecker reports whether a binary exists on PATH. Production callers
// pass a wrapper around exec.LookPath; tests inject a fake.
type BinChecker func(bin string) bool

// Decompression bomb defense constants.
const (
	maxZipEntryBytes = 10 << 20  // 10 MiB per file
	maxZipTotalBytes = 100 << 20 // 100 MiB aggregate
)

// Installer stages ClawHub bundles to a pending-approval directory. It does
// not commit the skill to the live registry — that is a separate step gated
// on user approval.
type Installer struct {
	skillsRoot      string
	registryBaseURL string
	hasBin          BinChecker
}

// NewInstaller returns an Installer. Empty registryBaseURL falls back to
// DefaultBaseURL (used only for composing the SourceURL recorded in the
// approval request — bytes are already in hand from a prior Fetch).
func NewInstaller(skillsRoot, registryBaseURL string, hasBin BinChecker) *Installer {
	if registryBaseURL == "" {
		registryBaseURL = DefaultBaseURL
	}
	return &Installer{
		skillsRoot:      skillsRoot,
		registryBaseURL: strings.TrimRight(registryBaseURL, "/"),
		hasBin:          hasBin,
	}
}

// Stage unpacks the bundle into <skillsRoot>/_requests/<id>_staging/, parses
// frontmatter, detects missing binaries, and returns the InstallRequest struct
// ready to be placed into an approval payload. Unlike the old shape, Stage no
// longer writes the request JSON to disk — the approval layer owns persistence.
func (in *Installer) Stage(pkg *SkillPackage) (*InstallRequest, error) {
	if pkg == nil {
		return nil, fmt.Errorf("clawhub: nil package")
	}
	reqID, err := in.newRequestID(pkg)
	if err != nil {
		return nil, err
	}

	stagingDir := filepath.Join(in.skillsRoot, "_requests", reqID+"_staging")
	if err := os.MkdirAll(stagingDir, 0700); err != nil {
		return nil, err
	}
	if err := extractZip(pkg.BundleBytes, stagingDir); err != nil {
		_ = os.RemoveAll(stagingDir)
		return nil, err
	}

	skillMD, err := os.ReadFile(filepath.Join(stagingDir, "SKILL.md"))
	if err != nil {
		_ = os.RemoveAll(stagingDir)
		return nil, fmt.Errorf("missing SKILL.md in bundle: %w", err)
	}
	fm, _, err := skills.ParseFrontmatter(skillMD)
	if err != nil {
		_ = os.RemoveAll(stagingDir)
		return nil, fmt.Errorf("parse frontmatter: %w", err)
	}

	missing := []string{}
	for _, bin := range fm.Metadata.OpenClaw.Requires.Bins {
		if !in.hasBin(bin) {
			missing = append(missing, bin)
		}
	}
	proposed := proposeCommands(fm, missing)

	return &InstallRequest{
		Slug:             pkg.Slug,
		Version:          pkg.Version,
		SourceURL:        in.composeSourceURL(pkg.OwnerHandle, pkg.Slug, pkg.Version),
		SHA256:           pkg.SHA256,
		StagingDir:       stagingDir,
		Frontmatter:      *fm,
		MissingBins:      missing,
		ProposedCommands: proposed,
	}, nil
}

// composeSourceURL returns a user-facing URL for the approval card. If the
// owner handle is known we use the human-readable /<handle>/<slug> path that
// resolves to the skill's public page on the ClawHub site; otherwise we fall
// back to the API download URL (the only URL shape we can reconstruct from
// package fields alone).
func (in *Installer) composeSourceURL(ownerHandle, slug, version string) string {
	if ownerHandle != "" {
		return in.registryBaseURL + "/" + url.PathEscape(ownerHandle) + "/" + url.PathEscape(slug)
	}
	return in.registryBaseURL + "/api/v1/download?slug=" + url.QueryEscape(slug) + "&version=" + url.QueryEscape(version)
}

// newRequestID returns a short (4-hex-char) id derived from the package
// identity + wall clock, retrying on directory collisions.
func (in *Installer) newRequestID(pkg *SkillPackage) (string, error) {
	for i := 0; i < 16; i++ {
		seed := fmt.Sprintf("%s@%s@%d@%d", pkg.Slug, pkg.Version, time.Now().UnixNano(), i)
		h := sha256.Sum256([]byte(seed))
		id := hex.EncodeToString(h[:])[:4]
		jsonPath := filepath.Join(in.skillsRoot, "_requests", id+".json")
		stagePath := filepath.Join(in.skillsRoot, "_requests", id+"_staging")
		_, jsonErr := os.Stat(jsonPath)
		_, stageErr := os.Stat(stagePath)
		if os.IsNotExist(jsonErr) && os.IsNotExist(stageErr) {
			return id, nil
		}
	}
	return "", fmt.Errorf("could not allocate unique request id")
}

// extractZip writes every file entry in data under dst. Directory entries
// are created lazily. Symlinks and other non-regular files are skipped.
// Any entry whose resolved path escapes dst returns an error.
// Defends against decompression bombs by enforcing per-entry and total size caps.
func extractZip(data []byte, dst string) error {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return fmt.Errorf("zip reader: %w", err)
	}
	absDst, err := filepath.Abs(dst)
	if err != nil {
		return err
	}
	absDst = filepath.Clean(absDst)

	var totalBytes int64
	for _, f := range zr.File {
		if f.Mode().IsDir() {
			continue
		}
		if !f.Mode().IsRegular() {
			continue // skip symlinks and other special entries
		}

		// Reject empty entry names
		if f.Name == "" {
			return fmt.Errorf("zip: empty entry name")
		}

		// Reject absolute paths
		if strings.HasPrefix(f.Name, "/") || strings.HasPrefix(f.Name, `\`) {
			return fmt.Errorf("zip: absolute entry path %q", f.Name)
		}

		target := filepath.Clean(filepath.Join(absDst, f.Name))
		rel, err := filepath.Rel(absDst, target)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			return fmt.Errorf("zip: entry %q escapes destination", f.Name)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			return err
		}
		n, err := writeZipFile(f, target)
		if err != nil {
			return err
		}
		totalBytes += n
		if totalBytes > maxZipTotalBytes {
			return fmt.Errorf("zip: bundle exceeds total extraction cap (%d bytes)", maxZipTotalBytes)
		}
	}
	return nil
}

func writeZipFile(f *zip.File, target string) (int64, error) {
	rc, err := f.Open()
	if err != nil {
		return 0, err
	}
	defer rc.Close()
	out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return 0, err
	}
	defer out.Close()

	// Enforce per-entry decompression bomb cap.
	n, err := io.CopyN(out, rc, maxZipEntryBytes+1)
	if err != nil && err != io.EOF {
		_ = out.Close()
		return 0, err
	}
	if err == nil {
		// CopyN wrote more than maxZipEntryBytes without hitting EOF.
		_ = out.Close()
		return 0, fmt.Errorf("zip: entry %q exceeds per-file size cap (%d bytes)", f.Name, maxZipEntryBytes)
	}

	// Close and return bytes written.
	if closeErr := out.Close(); closeErr != nil {
		return 0, closeErr
	}
	return n, nil
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
		cmd := formatRecipeCommand(recipe, sudoPrefix)
		if cmd == "" {
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
	// Fallback: if preferred had no matches, try any recipe that supplies a
	// still-missing bin (so the user at least sees a possible install hint).
	if len(missingSet) > 0 {
		for _, recipe := range fm.Metadata.OpenClaw.Install {
			if !suppliesMissing(recipe.Bins, missingSet) {
				continue
			}
			already := false
			for _, pc := range out {
				if pc.RecipeID == recipe.ID {
					already = true
					break
				}
			}
			if already {
				continue
			}
			cmd := formatRecipeCommand(recipe, "")
			if cmd == "" {
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
			if len(missingSet) == 0 {
				break
			}
		}
	}
	return out
}

func formatRecipeCommand(recipe skills.FMInstallRecipe, sudoPrefix string) string {
	switch recipe.Kind {
	case "brew":
		return "brew install " + recipe.Formula
	case "apt", "apt-get":
		return sudoPrefix + "apt install " + recipe.Package
	case "dnf":
		return sudoPrefix + "dnf install " + recipe.Package
	case "npm":
		return "npm install -g " + recipe.Package
	case "go":
		return "go install " + recipe.Module
	case "uv":
		return "uv tool install " + recipe.Package
	}
	return ""
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
