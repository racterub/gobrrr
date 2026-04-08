package access

import "testing"

func TestGateDM(t *testing.T) {
	a := Default()
	a.DMPolicy = DMDisabled
	if Check(a, "42", "42", false, "", "bot") != Drop {
		t.Error("disabled dm must drop")
	}

	a = Default()
	a.DMPolicy = DMAllowlist
	a.AllowFrom = []string{"42"}
	if Check(a, "42", "42", false, "hi", "bot") != Allow {
		t.Error("allowlist hit must allow")
	}
	if Check(a, "99", "99", false, "hi", "bot") != Drop {
		t.Error("allowlist miss must drop")
	}

	a = Default() // pairing
	if Check(a, "42", "42", false, "hello", "bot") != NeedPair {
		t.Error("pairing DM from unknown must be NeedPair")
	}
	a.AllowFrom = []string{"42"}
	if Check(a, "42", "42", false, "hello", "bot") != Allow {
		t.Error("pairing DM from approved must allow")
	}
}

func TestGateGroup(t *testing.T) {
	a := Default()
	a.Groups["-100"] = GroupPolicy{
		RequireMention: true,
		AllowFrom:      []string{"42"},
	}
	// unknown group → drop
	if Check(a, "-200", "42", true, "hi", "bot") != Drop {
		t.Error("unknown group must drop")
	}
	// known group, allowed user, no mention → drop
	if Check(a, "-100", "42", true, "hi", "bot") != Drop {
		t.Error("require mention enforced")
	}
	// known group, allowed user, mention → allow
	if Check(a, "-100", "42", true, "@bot hi", "bot") != Allow {
		t.Error("mention should allow")
	}
	// disallowed user → drop
	if Check(a, "-100", "99", true, "@bot hi", "bot") != Drop {
		t.Error("user not in group allowFrom must drop")
	}
	// no mention required
	a.Groups["-100"] = GroupPolicy{RequireMention: false, AllowFrom: []string{"42"}}
	if Check(a, "-100", "42", true, "hi", "bot") != Allow {
		t.Error("mention not required → allow")
	}
}
