// Package permission implements the permission-reply regex matcher used
// by pairing and approval gates. Mirrors the official telegram plugin's
// PERMISSION_REPLY_RE: 5 lowercase letters a-z minus 'l', case-insensitive,
// no bare yes/no, no prefix/suffix chatter.
package permission

import (
	"regexp"
	"strings"
)

var re = regexp.MustCompile(`^\s*(y|yes|n|no)\s+([a-km-z]{5})\s*$`)

// Match returns (ok, isYes, loweredCode). ok is false if the input is not
// a valid permission reply.
func Match(s string) (ok bool, yes bool, code string) {
	m := re.FindStringSubmatch(strings.ToLower(s))
	if m == nil {
		return false, false, ""
	}
	verb := m[1]
	return true, verb == "y" || verb == "yes", m[2]
}
