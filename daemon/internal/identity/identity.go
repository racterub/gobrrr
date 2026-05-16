package identity

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Load reads the worker prompt prefix from gobrrDir. Primary path is
// worker.md; falls back to legacy identity.md so existing installs keep
// working until the next install.sh run migrates the file. The installer
// is responsible for placing a default file; a missing file is an error.
func Load(gobrrDir string) (string, error) {
	for _, name := range []string{"worker.md", "identity.md"} {
		data, err := os.ReadFile(filepath.Join(gobrrDir, name))
		if err == nil {
			return string(data), nil
		}
		if !os.IsNotExist(err) {
			return "", fmt.Errorf("reading %s: %w", name, err)
		}
	}
	return "", fmt.Errorf("no worker.md or identity.md in %s", gobrrDir)
}

// BuildPrompt concatenates identity + memories + task prompt with clear separators.
// If memories is empty, the <memories> section is omitted.
func BuildPrompt(identity string, memories []string, taskPrompt string) string {
	var sb strings.Builder

	sb.WriteString("<identity>\n")
	sb.WriteString(identity)
	sb.WriteString("\n</identity>")

	if len(memories) > 0 {
		sb.WriteString("\n\n<memories>\n")
		for _, m := range memories {
			sb.WriteString(m)
			sb.WriteString("\n")
		}
		sb.WriteString("</memories>")
	}

	sb.WriteString("\n\n<task>\n")
	sb.WriteString(taskPrompt)
	sb.WriteString("\n</task>")

	return sb.String()
}
