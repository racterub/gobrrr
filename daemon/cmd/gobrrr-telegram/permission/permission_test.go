package permission

import "testing"

func TestMatch(t *testing.T) {
	cases := []struct {
		in       string
		wantOK   bool
		wantYes  bool
		wantCode string
	}{
		{"y abcde", true, true, "abcde"},
		{"yes abcde", true, true, "abcde"},
		{"YES ABCDE", true, true, "abcde"},
		{"n abcde", true, false, "abcde"},
		{"no abcde", true, false, "abcde"},
		{"  y  abcde  ", true, true, "abcde"},
		// bare yes/no rejected
		{"yes", false, false, ""},
		{"y", false, false, ""},
		// prefix/suffix chatter rejected
		{"y abcde foo", false, false, ""},
		{"hey y abcde", false, false, ""},
		// 'l' forbidden in code
		{"y ablde", false, false, ""},
		// wrong length
		{"y abcd", false, false, ""},
		{"y abcdef", false, false, ""},
	}
	for _, c := range cases {
		ok, yes, code := Match(c.in)
		if ok != c.wantOK || yes != c.wantYes || code != c.wantCode {
			t.Errorf("Match(%q) = (%v,%v,%q); want (%v,%v,%q)",
				c.in, ok, yes, code, c.wantOK, c.wantYes, c.wantCode)
		}
	}
}
