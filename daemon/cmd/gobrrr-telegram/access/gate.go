package access

import (
	"slices"
	"strings"
)

type Decision int

const (
	Drop Decision = iota
	Allow
	NeedPair // caller must run pairing logic (issue code or match reply)
)

// Check decides whether an inbound message should be delivered.
// botUsername is used for mention-triggering in groups.
func Check(a Access, chatID, userID string, isGroup bool, text, botUsername string) Decision {
	if isGroup {
		gp, ok := a.Groups[chatID]
		if !ok {
			return Drop
		}
		if !slices.Contains(gp.AllowFrom, userID) {
			return Drop
		}
		if gp.RequireMention {
			if !mentionsBot(text, botUsername, a.MentionPatterns) {
				return Drop
			}
		}
		return Allow
	}
	// DM
	switch a.DMPolicy {
	case DMDisabled:
		return Drop
	case DMAllowlist:
		if slices.Contains(a.AllowFrom, chatID) {
			return Allow
		}
		return Drop
	case DMPairing:
		if slices.Contains(a.AllowFrom, chatID) {
			return Allow
		}
		return NeedPair
	}
	return Drop
}

func mentionsBot(text, bot string, patterns []string) bool {
	lt := strings.ToLower(text)
	if bot != "" && strings.Contains(lt, "@"+strings.ToLower(bot)) {
		return true
	}
	for _, p := range patterns {
		if p != "" && strings.Contains(lt, strings.ToLower(p)) {
			return true
		}
	}
	return false
}
