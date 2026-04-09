package identity

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Load reads identity.md from gobrrDir. The installer is responsible for
// placing a default file; a missing file is an error.
func Load(gobrrDir string) (string, error) {
	path := filepath.Join(gobrrDir, "identity.md")
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("reading identity.md: %w", err)
	}
	return string(data), nil
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
