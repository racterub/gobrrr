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
		RequestID:  "abcd",
		Slug:       "noop",
		Version:    "1.0.0",
		SourceURL:  "https://clawhub.com/noop",
		SHA256:     "sha256:test",
		StagingDir: filepath.Join(skillsRoot, "_requests", "abcd_staging"),
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

func TestCommit_ApproveRunsBinaryAndRecords(t *testing.T) {
	skillsRoot := t.TempDir()
	var ran []string

	// Stage an install request with a proposed command.
	req := &InstallRequest{
		RequestID:  "req1",
		Slug:       "gh-tool",
		Version:    "1.0.0",
		SourceURL:  "https://clawhub.com/gh-tool",
		SHA256:     "sha256:test",
		StagingDir: filepath.Join(skillsRoot, "_requests", "req1_staging"),
		Frontmatter: skills.Frontmatter{
			Name:        "gh-tool",
			Description: "github cli tool",
			Metadata: skills.FMMetadata{
				Gobrrr: skills.FMGobrrr{Type: "clawhub"},
				OpenClaw: skills.FMOpenClaw{
					Requires: skills.FMRequires{
						ToolPermissions: skills.FMToolPermissions{
							Read:  []string{"Bash(gh list:*)"},
							Write: []string{"Bash(gh apply:*)"},
						},
					},
				},
			},
		},
		MissingBins: []string{"gh"},
		ProposedCommands: []ProposedCommand{
			{
				RecipeID: "gh-apt",
				Kind:     "apt",
				Command:  "apt install gh-cli",
				Bins:     []string{"gh"},
			},
		},
	}
	require.NoError(t, os.MkdirAll(req.StagingDir, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(req.StagingDir, "SKILL.md"),
		[]byte("---\nname: gh-tool\ndescription: github cli tool\nmetadata:\n  gobrrr:\n    type: clawhub\n---\n\nbody"), 0600))

	require.NoError(t, os.MkdirAll(filepath.Join(skillsRoot, "_requests"), 0700))
	reqBytes, _ := json.MarshalIndent(req, "", "  ")
	require.NoError(t, os.WriteFile(filepath.Join(skillsRoot, "_requests", "req1.json"), reqBytes, 0600))

	committer := NewCommitter(skillsRoot, fakeCmdRunner(&ran))
	err := committer.Commit("req1", Decision{Approve: true, SkipBinary: false})
	require.NoError(t, err)

	// The runner was invoked with the proposed command.
	assert.Equal(t, []string{"apt install gh-cli"}, ran)

	// Skill now installed.
	skillDir := filepath.Join(skillsRoot, "gh-tool")
	assert.FileExists(t, filepath.Join(skillDir, "SKILL.md"))

	// _meta.json has correct state.
	metaBytes, err := os.ReadFile(filepath.Join(skillDir, "_meta.json"))
	require.NoError(t, err)
	var meta skills.Meta
	require.NoError(t, json.Unmarshal(metaBytes, &meta))
	assert.Equal(t, "gh-tool", meta.Slug)
	assert.Contains(t, meta.ApprovedReadPermissions, "Bash(gh list:*)")
	assert.Contains(t, meta.ApprovedWritePermissions, "Bash(gh apply:*)")
	assert.Equal(t, []string{"gh"}, meta.ApprovedBinaries)
	assert.Len(t, meta.BinaryInstallCommands, 1)
	assert.Equal(t, "gh-apt", meta.BinaryInstallCommands[0].RecipeID)
	assert.Equal(t, "apt install gh-cli", meta.BinaryInstallCommands[0].Command)
	assert.True(t, meta.BinaryInstallCommands[0].Approved)
	assert.Equal(t, 0, meta.BinaryInstallCommands[0].ExitCode)

	// Staging dir removed.
	_, err = os.Stat(req.StagingDir)
	assert.True(t, os.IsNotExist(err))

	// Request file removed.
	_, err = os.Stat(filepath.Join(skillsRoot, "_requests", "req1.json"))
	assert.True(t, os.IsNotExist(err))

	// Lockfile updated.
	lockPath := filepath.Join(skillsRoot, "_lock.json")
	lockBytes, err := os.ReadFile(lockPath)
	require.NoError(t, err)
	var lf lockFile
	require.NoError(t, json.Unmarshal(lockBytes, &lf))
	assert.Contains(t, lf.Skills, "gh-tool")
}

func TestCommit_CommandFailureReturnsError(t *testing.T) {
	skillsRoot := t.TempDir()

	// Stage an install request with a proposed command.
	req := &InstallRequest{
		RequestID:  "req2",
		Slug:       "broken-tool",
		Version:    "1.0.0",
		SourceURL:  "https://clawhub.com/broken-tool",
		SHA256:     "sha256:test",
		StagingDir: filepath.Join(skillsRoot, "_requests", "req2_staging"),
		Frontmatter: skills.Frontmatter{
			Name:        "broken-tool",
			Description: "tool with failing install",
			Metadata: skills.FMMetadata{
				Gobrrr: skills.FMGobrrr{Type: "clawhub"},
				OpenClaw: skills.FMOpenClaw{
					Requires: skills.FMRequires{
						ToolPermissions: skills.FMToolPermissions{
							Read:  []string{"Bash(broken list:*)"},
							Write: []string{"Bash(broken apply:*)"},
						},
					},
				},
			},
		},
		MissingBins: []string{"broken"},
		ProposedCommands: []ProposedCommand{
			{
				RecipeID: "broken-apt",
				Kind:     "apt",
				Command:  "apt install nonexistent-package",
				Bins:     []string{"broken"},
			},
		},
	}
	require.NoError(t, os.MkdirAll(req.StagingDir, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(req.StagingDir, "SKILL.md"),
		[]byte("---\nname: broken-tool\ndescription: tool with failing install\nmetadata:\n  gobrrr:\n    type: clawhub\n---\n\nbody"), 0600))

	require.NoError(t, os.MkdirAll(filepath.Join(skillsRoot, "_requests"), 0700))
	reqBytes, _ := json.MarshalIndent(req, "", "  ")
	require.NoError(t, os.WriteFile(filepath.Join(skillsRoot, "_requests", "req2.json"), reqBytes, 0600))

	// Inject a failing command runner.
	failingCmdRunner := func(cmd string) (*exec.Cmd, error) {
		return exec.Command("false"), nil // exits with code 1
	}

	committer := NewCommitter(skillsRoot, failingCmdRunner)
	err := committer.Commit("req2", Decision{Approve: true, SkipBinary: false})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "command")

	// Skill was NOT installed (command failed).
	skillDir := filepath.Join(skillsRoot, "broken-tool")
	_, err = os.Stat(skillDir)
	assert.True(t, os.IsNotExist(err))

	// Staging dir still exists (preserved on failure).
	_, err = os.Stat(req.StagingDir)
	assert.NoError(t, err)

	// Request file still exists (preserved on failure).
	_, err = os.Stat(filepath.Join(skillsRoot, "_requests", "req2.json"))
	assert.NoError(t, err)

	// Lockfile NOT created on failure.
	lockPath := filepath.Join(skillsRoot, "_lock.json")
	_, err = os.Stat(lockPath)
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
