package clawhub

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInstaller_StagesAndWritesRequest(t *testing.T) {
	skillsRoot := t.TempDir()

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
      - id: gh-brew
        kind: brew
        formula: gh
        bins: [gh]
---

body
`)
	bundle := buildZip(t, map[string][]byte{"SKILL.md": skillMD})
	pkg := &SkillPackage{
		Slug: "github", Version: "1.4.2",
		SHA256: "abc123", BundleBytes: bundle,
	}

	inst := NewInstaller(skillsRoot, "https://clawhub.ai", func(bin string) bool {
		return bin != "gh" // gh missing from PATH
	})
	reqID, err := inst.Stage(pkg)
	require.NoError(t, err)
	require.NotEmpty(t, reqID)

	data, err := os.ReadFile(filepath.Join(skillsRoot, "_requests", reqID+".json"))
	require.NoError(t, err)
	var req InstallRequest
	require.NoError(t, json.Unmarshal(data, &req))
	assert.Equal(t, "github", req.Slug)
	assert.Equal(t, "1.4.2", req.Version)
	assert.Equal(t, "abc123", req.SHA256)
	assert.Contains(t, req.SourceURL, "clawhub.ai")
	assert.Contains(t, req.SourceURL, "slug=github")
	assert.Contains(t, req.SourceURL, "version=1.4.2")
	assert.Contains(t, req.MissingBins, "gh")
	require.NotEmpty(t, req.ProposedCommands)
	found := false
	for _, pc := range req.ProposedCommands {
		for _, b := range pc.Bins {
			if b == "gh" {
				found = true
			}
		}
	}
	assert.True(t, found, "a proposed command must supply the missing gh binary")

	assert.FileExists(t, filepath.Join(req.StagingDir, "SKILL.md"))
	assert.False(t, req.ExpiresAt.IsZero())
	assert.True(t, req.ExpiresAt.After(req.CreatedAt))
}

func TestInstaller_NoMissingBinsYieldsNoProposals(t *testing.T) {
	skillsRoot := t.TempDir()
	skillMD := []byte(`---
name: github
description: GitHub ops
metadata:
  gobrrr: { type: clawhub }
  openclaw:
    requires:
      bins: [gh]
      tool_permissions:
        read: ["Bash(gh issue list:*)"]
---
body
`)
	pkg := &SkillPackage{Slug: "github", Version: "1.4.2", BundleBytes: buildZip(t, map[string][]byte{"SKILL.md": skillMD})}
	inst := NewInstaller(skillsRoot, "", func(string) bool { return true })
	reqID, err := inst.Stage(pkg)
	require.NoError(t, err)

	data, _ := os.ReadFile(filepath.Join(skillsRoot, "_requests", reqID+".json"))
	var req InstallRequest
	require.NoError(t, json.Unmarshal(data, &req))
	assert.Empty(t, req.MissingBins)
	assert.Empty(t, req.ProposedCommands)
}

func TestInstaller_RejectsZipSlipEntries(t *testing.T) {
	skillsRoot := t.TempDir()
	bundle := buildZipRaw(t, []zipEntry{
		{Name: "SKILL.md", Data: []byte("---\nname: x\ndescription: x\nmetadata:\n  gobrrr: {type: clawhub}\n  openclaw: {requires: {tool_permissions: {}}}\n---\nbody")},
		{Name: "../evil.txt", Data: []byte("pwned")},
	})
	pkg := &SkillPackage{Slug: "x", Version: "0.0.1", BundleBytes: bundle}
	inst := NewInstaller(skillsRoot, "", func(string) bool { return true })
	_, err := inst.Stage(pkg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "escapes")
	_, statErr := os.Stat(filepath.Join(filepath.Dir(skillsRoot), "evil.txt"))
	assert.True(t, os.IsNotExist(statErr), "zip slip must not write outside destination")
}

func TestInstaller_MissingSKILLMD(t *testing.T) {
	skillsRoot := t.TempDir()
	bundle := buildZip(t, map[string][]byte{"README.md": []byte("no skill here")})
	pkg := &SkillPackage{Slug: "x", Version: "0.0.1", BundleBytes: bundle}
	inst := NewInstaller(skillsRoot, "", func(string) bool { return true })
	_, err := inst.Stage(pkg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "SKILL.md")
}

func TestInstaller_RejectsOversizedZipEntry(t *testing.T) {
	skillsRoot := t.TempDir()

	// Build a zip with a large uncompressed file (11 MiB) to exceed the 10 MiB cap.
	// We use buildZipRaw with explicit zip.Deflate to compress it small but expand large.
	largePayload := strings.Repeat("A", 11<<20) // 11 MiB of repeated bytes

	// Create a zip with explicit Deflate compression to get a small compressed blob.
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	h := &zip.FileHeader{Name: "large.bin", Method: zip.Deflate}
	w, err := zw.CreateHeader(h)
	require.NoError(t, err)
	_, err = w.Write([]byte(largePayload))
	require.NoError(t, err)

	// Add SKILL.md so stage doesn't fail on missing file before extraction cap.
	skillMD := []byte(`---
name: test
description: test
metadata:
  gobrrr: {type: clawhub}
  openclaw: {requires: {tool_permissions: {}}}
---
body
`)
	w, err = zw.Create("SKILL.md")
	require.NoError(t, err)
	_, err = w.Write(skillMD)
	require.NoError(t, err)

	require.NoError(t, zw.Close())

	pkg := &SkillPackage{Slug: "x", Version: "0.0.1", BundleBytes: buf.Bytes()}
	inst := NewInstaller(skillsRoot, "", func(string) bool { return true })
	_, err = inst.Stage(pkg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "size cap")

	// Verify staging dir was cleaned up.
	files, _ := os.ReadDir(filepath.Join(skillsRoot, "_requests"))
	// Only the failed request dir should remain (or nothing if cleaned).
	for _, f := range files {
		if strings.Contains(f.Name(), "_staging") {
			t.Errorf("staging dir not cleaned up: %s", f.Name())
		}
	}
}

func TestInstaller_RejectsAbsolutePathZipEntry(t *testing.T) {
	skillsRoot := t.TempDir()
	bundle := buildZipRaw(t, []zipEntry{
		{Name: "SKILL.md", Data: []byte("---\nname: x\ndescription: x\nmetadata:\n  gobrrr: {type: clawhub}\n  openclaw: {requires: {tool_permissions: {}}}\n---\nbody")},
		{Name: "/etc/passwd", Data: []byte("hacked")},
	})
	pkg := &SkillPackage{Slug: "x", Version: "0.0.1", BundleBytes: bundle}
	inst := NewInstaller(skillsRoot, "", func(string) bool { return true })
	_, err := inst.Stage(pkg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "absolute")
}

func TestInstaller_RejectsEmptyNameZipEntry(t *testing.T) {
	skillsRoot := t.TempDir()
	bundle := buildZipRaw(t, []zipEntry{
		{Name: "SKILL.md", Data: []byte("---\nname: x\ndescription: x\nmetadata:\n  gobrrr: {type: clawhub}\n  openclaw: {requires: {tool_permissions: {}}}\n---\nbody")},
		{Name: "", Data: []byte("empty")},
	})
	pkg := &SkillPackage{Slug: "x", Version: "0.0.1", BundleBytes: bundle}
	inst := NewInstaller(skillsRoot, "", func(string) bool { return true })
	_, err := inst.Stage(pkg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty")
}

// --- test helpers ---

func buildZip(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	entries := make([]zipEntry, 0, len(files))
	for name, data := range files {
		entries = append(entries, zipEntry{Name: name, Data: data})
	}
	return buildZipRaw(t, entries)
}

type zipEntry struct {
	Name string
	Data []byte
}

func buildZipRaw(t *testing.T, entries []zipEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range entries {
		w, err := zw.Create(e.Name)
		require.NoError(t, err)
		_, err = w.Write(e.Data)
		require.NoError(t, err)
	}
	require.NoError(t, zw.Close())
	return buf.Bytes()
}
