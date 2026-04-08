// Package chunker splits outbound Telegram messages per the official
// plugin's chunkMode + textChunkLimit behavior.
package chunker

import "strings"

type Mode string

const (
	ModeLength  Mode = "length"
	ModeNewline Mode = "newline"

	HardCap = 4096 // Telegram's sendMessage text limit
)

// Split returns 1+ chunks. Empty input → one empty chunk (caller decides).
// limit is clamped to HardCap. Mode "newline" splits on blank lines and
// falls back to hard length splitting for any paragraph still too large.
func Split(text string, mode Mode, limit int) []string {
	if limit <= 0 || limit > HardCap {
		limit = HardCap
	}
	if text == "" {
		return []string{""}
	}
	if mode == ModeNewline {
		return splitNewline(text, limit)
	}
	return splitLength(text, limit)
}

func splitLength(text string, limit int) []string {
	var out []string
	for len(text) > limit {
		out = append(out, text[:limit])
		text = text[limit:]
	}
	out = append(out, text)
	return out
}

func splitNewline(text string, limit int) []string {
	paras := strings.Split(text, "\n\n")
	var out []string
	var cur strings.Builder
	flush := func() {
		if cur.Len() > 0 {
			out = append(out, cur.String())
			cur.Reset()
		}
	}
	for _, p := range paras {
		if len(p) > limit {
			flush()
			out = append(out, splitLength(p, limit)...)
			continue
		}
		sep := ""
		if cur.Len() > 0 {
			sep = "\n\n"
		}
		if cur.Len()+len(sep)+len(p) > limit {
			flush()
			cur.WriteString(p)
		} else {
			cur.WriteString(sep)
			cur.WriteString(p)
		}
	}
	flush()
	if len(out) == 0 {
		return []string{""}
	}
	return out
}
