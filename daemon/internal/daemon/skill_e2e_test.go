package daemon_test

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/racterub/gobrrr/internal/clawhub"
	"github.com/racterub/gobrrr/internal/security"
	"github.com/racterub/gobrrr/internal/skills"
)

// TestE2E_InstallApproveSkill exercises the full install → approve pipeline
// against a fake ClawHub backend: client.Fetch → installer.Stage →
// committer.Commit → registry.Refresh → BuildPromptBlock → security.Generate.
func TestE2E_InstallApproveSkill(t *testing.T) {
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

	bundle := buildSkillZip(t, map[string][]byte{"SKILL.md": skillMD})
	sum := sha256.Sum256(bundle)
	hexSum := hex.EncodeToString(sum[:])

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/skills/noop", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"skill":{"slug":"noop","displayName":"Noop","tags":{"latest":"1.0.0"}},"latestVersion":{"version":"1.0.0"}}`)
	})
	mux.HandleFunc("/api/v1/skills/noop/versions/1.0.0", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"version":{"version":"1.0.0","security":{"status":"clean","sha256hash":"%s"}}}`, hexSum)
	})
	mux.HandleFunc("/api/v1/download", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/zip")
		_, _ = w.Write(bundle)
	})
	fakeHub := httptest.NewServer(mux)
	defer fakeHub.Close()

	skillsRoot := t.TempDir()

	c := clawhub.NewClient(fakeHub.URL)
	pkg, err := c.Fetch("noop", "1.0.0")
	require.NoError(t, err)

	inst := clawhub.NewInstaller(skillsRoot, fakeHub.URL, func(string) bool { return true })
	installReq, err := inst.Stage(pkg)
	require.NoError(t, err)

	cm := clawhub.NewCommitter(skillsRoot, nil)
	require.NoError(t, cm.Commit(*installReq, clawhub.Decision{Approve: true, SkipBinary: true}))

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

	installedSkill := filepath.Join(skillsRoot, "noop", "SKILL.md")
	assert.FileExists(t, installedSkill)
}

func buildSkillZip(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, data := range files {
		w, err := zw.Create(name)
		require.NoError(t, err)
		_, err = w.Write(data)
		require.NoError(t, err)
	}
	require.NoError(t, zw.Close())
	return buf.Bytes()
}
