package identity

import (
	_ "embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

//go:embed default.md
var defaultIdentity string

// Load reads identity.md from gobrrDir. If missing, returns embedded default.
func Load(gobrrDir string) (string, error) {
	path := filepath.Join(gobrrDir, "identity.md")
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return defaultIdentity, nil
		}
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
