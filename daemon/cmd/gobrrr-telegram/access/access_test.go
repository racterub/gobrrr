package access

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDefaultWhenMissing(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir, false)
	a, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if a.DMPolicy != "pairing" {
		t.Errorf("default dm policy = %q", a.DMPolicy)
	}
	if a.AllowFrom == nil || a.Groups == nil || a.Pending == nil {
		t.Error("default slices/maps must be non-nil")
	}
}

func TestSaveAtomicRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir, false)
	a := Default()
	a.DMPolicy = "allowlist"
	a.AllowFrom = []string{"123"}
	if err := s.Save(a); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "access.json"))
	if err != nil {
		t.Fatal(err)
	}
	var back Access
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	if back.DMPolicy != "allowlist" || len(back.AllowFrom) != 1 {
		t.Errorf("roundtrip: %+v", back)
	}
	// file perm 0600
	st, _ := os.Stat(filepath.Join(dir, "access.json"))
	if st.Mode().Perm() != 0600 {
		t.Errorf("perm = %v", st.Mode().Perm())
	}
}

func TestCorruptRecovery(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "access.json")
	os.WriteFile(p, []byte("{not json"), 0600)
	s := NewStore(dir, false)
	a, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if a.DMPolicy != "pairing" {
		t.Errorf("expected fresh default, got %+v", a)
	}
	// corrupt file renamed aside
	matches, _ := filepath.Glob(filepath.Join(dir, "access.json.corrupt-*"))
	if len(matches) != 1 {
		t.Errorf("expected 1 corrupt-aside file, got %d", len(matches))
	}
}

func TestStaticMode(t *testing.T) {
	dir := t.TempDir()
	// write an access file with pairing policy
	a := Default()
	a.DMPolicy = "pairing"
	a.Pending = map[string]Pending{"abcde": {SenderID: "1"}}
	NewStore(dir, false).Save(a)

	s := NewStore(dir, true) // static
	got, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.DMPolicy != "allowlist" {
		t.Errorf("static: pairing should downgrade to allowlist, got %q", got.DMPolicy)
	}
	if len(got.Pending) != 0 {
		t.Errorf("static: pending must be cleared")
	}
	// Save in static mode must be a no-op (no error, no disk write)
	a2 := Default()
	a2.DMPolicy = "disabled"
	if err := s.Save(a2); err != nil {
		t.Fatal(err)
	}
	reload, _ := NewStore(dir, false).Load()
	if reload.DMPolicy != "pairing" {
		t.Errorf("static save should not persist; got %q", reload.DMPolicy)
	}
}
