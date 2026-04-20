package skills

import (
	"encoding/xml"
	"sort"
	"strings"
)

// BuildPromptBlock emits the <available_skills> XML block injected into
// worker prompts. Skills are sorted alphabetically by slug for deterministic
// output. The home prefix (from os.UserHomeDir) is compacted to "~" to save
// ~5 tokens per skill path.
//
// Returns empty string for empty input so callers can unconditionally
// prepend it.
func BuildPromptBlock(skills []Skill, home string) string {
	if len(skills) == 0 {
		return ""
	}
	sorted := make([]Skill, len(skills))
	copy(sorted, skills)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Slug < sorted[j].Slug })

	var b strings.Builder
	b.WriteString("<available_skills>\n")
	for _, s := range sorted {
		loc := compactHome(s.Path, home)
		b.WriteString("  <skill name=\"")
		xmlEscape(&b, s.Slug)
		b.WriteString("\" location=\"")
		xmlEscape(&b, loc)
		b.WriteString("\">\n    ")
		xmlEscape(&b, s.Description)
		b.WriteString("\n  </skill>\n")
	}
	b.WriteString("</available_skills>")
	return b.String()
}

func compactHome(path, home string) string {
	if home == "" {
		return path
	}
	if strings.HasPrefix(path, home+"/") {
		return "~" + path[len(home):]
	}
	return path
}

func xmlEscape(b *strings.Builder, s string) {
	_ = xml.EscapeText(stringWriter{b}, []byte(s))
}

type stringWriter struct{ b *strings.Builder }

func (w stringWriter) Write(p []byte) (int, error) { return w.b.Write(p) }
