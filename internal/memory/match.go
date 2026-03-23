package memory

import (
	"sort"
	"strings"
)

// MatchRelevant scores entries against a prompt and returns the top-N most
// relevant entries. Scoring counts how many unique prompt words appear in the
// entry content or tags (case-insensitive). Entries with a score of 0 are
// excluded. When limit <= 0 all non-zero entries are returned.
func MatchRelevant(entries []*Entry, prompt string, limit int) []*Entry {
	words := tokenize(prompt)
	if len(words) == 0 {
		return nil
	}

	type scored struct {
		entry *Entry
		score int
	}

	var candidates []scored
	for _, e := range entries {
		if e == nil {
			continue
		}
		s := scoreEntry(e, words)
		if s > 0 {
			candidates = append(candidates, scored{entry: e, score: s})
		}
	}

	// Sort by score descending, then by CreatedAt descending for ties.
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].score != candidates[j].score {
			return candidates[i].score > candidates[j].score
		}
		return candidates[i].entry.CreatedAt.After(candidates[j].entry.CreatedAt)
	})

	if limit > 0 && len(candidates) > limit {
		candidates = candidates[:limit]
	}

	results := make([]*Entry, len(candidates))
	for i, c := range candidates {
		results[i] = c.entry
	}
	return results
}

// tokenize splits text into unique lowercase words.
func tokenize(text string) []string {
	fields := strings.Fields(strings.ToLower(text))
	seen := make(map[string]struct{}, len(fields))
	var words []string
	for _, f := range fields {
		// Strip non-alphabetic prefix/suffix characters.
		f = strings.Trim(f, `.,;:!?"'()[]{}`)
		if f == "" {
			continue
		}
		if _, ok := seen[f]; !ok {
			seen[f] = struct{}{}
			words = append(words, f)
		}
	}
	return words
}

// scoreEntry counts how many of words appear in e.Content or e.Tags.
func scoreEntry(e *Entry, words []string) int {
	lowerContent := strings.ToLower(e.Content)
	lowerTags := make([]string, len(e.Tags))
	for i, t := range e.Tags {
		lowerTags[i] = strings.ToLower(t)
	}

	score := 0
	for _, w := range words {
		if strings.Contains(lowerContent, w) {
			score++
			continue
		}
		for _, t := range lowerTags {
			if strings.Contains(t, w) {
				score++
				break
			}
		}
	}
	return score
}
