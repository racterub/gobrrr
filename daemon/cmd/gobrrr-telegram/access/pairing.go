package access

import (
	"crypto/rand"
	"time"
)

// alphabet: a-z minus 'l' — matches permission.Match expectations.
const pairAlphabet = "abcdefghijkmnopqrstuvwxyz"

// NewPairingCode returns a 5-char code drawn from pairAlphabet.
func NewPairingCode() string {
	var buf [5]byte
	if _, err := rand.Read(buf[:]); err != nil {
		// crypto/rand should never fail; fall back to time-based seed
		t := time.Now().UnixNano()
		for i := range buf {
			buf[i] = byte(t >> (i * 8))
		}
	}
	out := make([]byte, 5)
	for i, b := range buf {
		out[i] = pairAlphabet[int(b)%len(pairAlphabet)]
	}
	return string(out)
}

// PruneExpired removes expired pending entries. Returns true if anything
// was removed.
func PruneExpired(a *Access) bool {
	now := time.Now().UnixMilli()
	changed := false
	for code, p := range a.Pending {
		if p.ExpiresAt < now {
			delete(a.Pending, code)
			changed = true
		}
	}
	return changed
}
