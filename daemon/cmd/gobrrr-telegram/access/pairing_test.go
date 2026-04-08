package access

import (
	"testing"
	"time"
)

func TestNewPairingCode(t *testing.T) {
	code := NewPairingCode()
	if len(code) != 5 {
		t.Fatalf("len=%d", len(code))
	}
	for _, r := range code {
		if r == 'l' || r < 'a' || r > 'z' {
			t.Fatalf("bad char %q in %q", r, code)
		}
	}
}

func TestPruneExpired(t *testing.T) {
	a := Default()
	now := time.Now().UnixMilli()
	a.Pending["aaaaa"] = Pending{ExpiresAt: now - 1000}
	a.Pending["bbbbb"] = Pending{ExpiresAt: now + 60000}
	changed := PruneExpired(&a)
	if !changed {
		t.Error("expected changed=true")
	}
	if _, ok := a.Pending["aaaaa"]; ok {
		t.Error("expired should be gone")
	}
	if _, ok := a.Pending["bbbbb"]; !ok {
		t.Error("live should remain")
	}
}
