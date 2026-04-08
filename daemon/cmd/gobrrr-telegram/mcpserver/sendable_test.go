package mcpserver

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAssertSendable(t *testing.T) {
	state := t.TempDir()
	os.MkdirAll(filepath.Join(state, "inbox"), 0700)

	// state dir file (access.json) → refuse
	accessFile := filepath.Join(state, "access.json")
	os.WriteFile(accessFile, []byte("{}"), 0600)
	if err := AssertSendable(accessFile, state); err == nil {
		t.Error("state-dir file must be refused")
	}
	// inbox file → allow
	inboxFile := filepath.Join(state, "inbox", "x.png")
	os.WriteFile(inboxFile, []byte("png"), 0600)
	if err := AssertSendable(inboxFile, state); err != nil {
		t.Errorf("inbox must be allowed: %v", err)
	}
	// outside state dir → allow
	outsideDir := t.TempDir()
	outsideFile := filepath.Join(outsideDir, "y.png")
	os.WriteFile(outsideFile, []byte("png"), 0600)
	if err := AssertSendable(outsideFile, state); err != nil {
		t.Errorf("outside must be allowed: %v", err)
	}
	// nonexistent file → permissive (assertSendable is best-effort; caller
	// will get a proper error when it opens the file)
	if err := AssertSendable(filepath.Join(state, "nope.txt"), state); err != nil {
		t.Errorf("nonexistent permissive: %v", err)
	}
}
