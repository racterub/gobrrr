package chunker

import (
	"strings"
	"testing"
)

func TestSplitLength(t *testing.T) {
	// short → single chunk
	got := Split("hello", ModeLength, 10)
	if len(got) != 1 || got[0] != "hello" {
		t.Fatalf("short: %v", got)
	}
	// exact boundary
	got = Split(strings.Repeat("a", 10), ModeLength, 10)
	if len(got) != 1 {
		t.Fatalf("exact: %v", got)
	}
	// over boundary → split
	got = Split(strings.Repeat("a", 25), ModeLength, 10)
	if len(got) != 3 || len(got[0]) != 10 || len(got[1]) != 10 || len(got[2]) != 5 {
		t.Fatalf("over: %v", got)
	}
}

func TestSplitNewline(t *testing.T) {
	in := "para1 line\n\npara2 line\n\npara3"
	got := Split(in, ModeNewline, 15)
	// each paragraph fits in 15 → one chunk each
	if len(got) != 3 {
		t.Fatalf("newline chunks: %v", got)
	}
	// a paragraph longer than the limit still must fit (hard-split fallback)
	long := strings.Repeat("x", 40)
	got = Split(long, ModeNewline, 15)
	if len(got) < 3 {
		t.Fatalf("long newline: %v", got)
	}
}

func TestSplitEmpty(t *testing.T) {
	got := Split("", ModeLength, 10)
	if len(got) != 1 || got[0] != "" {
		t.Fatalf("empty: %v", got)
	}
}

func TestSplitClampLimit(t *testing.T) {
	// limit above hard cap should clamp to 4096
	got := Split(strings.Repeat("a", 5000), ModeLength, 10000)
	if len(got[0]) != 4096 {
		t.Fatalf("clamp: %d", len(got[0]))
	}
}
