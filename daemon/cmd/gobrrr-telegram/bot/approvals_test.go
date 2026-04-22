package bot

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/racterub/gobrrr/internal/client"
)

func TestRenderSkillInstallCard_MentionsSlug(t *testing.T) {
	payload, _ := json.Marshal(map[string]any{
		"slug":    "foo",
		"version": "1.0.0",
		"sha256":  "deadbeef12345678",
	})
	req := &client.ApprovalRequest{
		ID:      "abcd",
		Kind:    "skill_install",
		Title:   "install skill foo@1.0.0",
		Body:    "proposed install",
		Actions: []string{"approve", "skip_binary", "deny"},
		Payload: payload,
	}
	card, kb := RenderApprovalCard(req)
	assert.Contains(t, card, "install skill foo@1.0.0")
	// sha256 is truncated to the first 8 hex chars — cheap fingerprint, not a
	// verifier (the full value stays in the daemon-side payload).
	assert.Contains(t, card, "sha256: deadbeef")
	assert.NotContains(t, card, "12345678")
	assert.Equal(t, 3, len(kb.InlineKeyboard[0]))
	// button callback data shape: ap:{id}:{action}
	assert.Equal(t, "ap:abcd:approve", kb.InlineKeyboard[0][0].CallbackData)
	assert.Equal(t, "ap:abcd:skip_binary", kb.InlineKeyboard[0][1].CallbackData)
	assert.Equal(t, "ap:abcd:deny", kb.InlineKeyboard[0][2].CallbackData)
}

func TestRenderSkillInstallCard_FullPayload(t *testing.T) {
	payload, _ := json.Marshal(map[string]any{
		"slug":       "tonic-brainstorm",
		"version":    "1.0.0",
		"source_url": "https://clawhub.ai/api/v1/download?slug=tonic-brainstorm&version=1.0.0",
		"sha256":     "d1361e4d1835a330fa73221c5846575e0219943d2f07783a0a19f6c26ba4daf4",
		"frontmatter": map[string]any{
			"name":        "Brainstorm",
			"description": "Generate ideas fast. Adapt depth and structure to what the user actually needs.",
			"metadata": map[string]any{
				"openclaw": map[string]any{
					"requires": map[string]any{
						"tool_permissions": map[string]any{
							"read":  []string{"Read", "Glob", "Grep"},
							"write": []string{"Write"},
						},
					},
				},
			},
		},
		"proposed_commands": []map[string]any{
			{"command": "sudo apt install ripgrep"},
		},
	})
	req := &client.ApprovalRequest{
		ID:      "abcd",
		Kind:    "skill_install",
		Title:   "install skill tonic-brainstorm@1.0.0",
		Actions: []string{"approve", "skip_binary", "deny"},
		Payload: payload,
	}
	card, _ := RenderApprovalCard(req)

	assert.Contains(t, card, "Brainstorm v1.0.0")
	assert.Contains(t, card, "Generate ideas fast.")
	assert.Contains(t, card, "Source: https://clawhub.ai/api/v1/download?slug=tonic-brainstorm&version=1.0.0")
	assert.Contains(t, card, "sha256: d1361e4d")
	assert.NotContains(t, card, "d1361e4d1835a330")
	assert.Contains(t, card, "Binaries to install:")
	assert.Contains(t, card, "sudo apt install ripgrep")
	assert.Contains(t, card, "Permissions:")
	assert.Contains(t, card, "read:  Read, Glob, Grep")
	assert.Contains(t, card, "write: Write")
}

func TestRenderSkillInstallCard_OmitsEmptySections(t *testing.T) {
	// Minimal payload: no binaries, no permissions. Those sections must not
	// appear in the card.
	payload, _ := json.Marshal(map[string]any{
		"slug":    "bare",
		"version": "0.1.0",
		"sha256":  "aabbccdd",
		"frontmatter": map[string]any{
			"name":        "Bare",
			"description": "nothing optional",
		},
	})
	req := &client.ApprovalRequest{
		ID:      "ef01",
		Kind:    "skill_install",
		Title:   "install skill bare@0.1.0",
		Actions: []string{"approve", "deny"},
		Payload: payload,
	}
	card, _ := RenderApprovalCard(req)
	assert.NotContains(t, card, "Binaries to install")
	assert.NotContains(t, card, "Permissions:")
}

func TestParseApprovalCallback(t *testing.T) {
	for _, tc := range []struct {
		data       string
		okExpected bool
		id, action string
	}{
		{"ap:abcd:approve", true, "abcd", "approve"},
		{"ap:abcd:skip_binary", true, "abcd", "skip_binary"},
		{"ap:abcd:deny", true, "abcd", "deny"},
		{"pa:xyz", false, "", ""},
		{"ap:onlyid", false, "", ""},
	} {
		t.Run(tc.data, func(t *testing.T) {
			id, action, ok := ParseApprovalCallback(tc.data)
			assert.Equal(t, tc.okExpected, ok)
			if ok {
				assert.Equal(t, tc.id, id)
				assert.Equal(t, tc.action, action)
			}
		})
	}
}

func TestButtonLabel(t *testing.T) {
	assert.Equal(t, "✅ Approve", buttonLabel("approve"))
	assert.Equal(t, "⏭️ Skip binary", buttonLabel("skip_binary"))
	assert.Equal(t, "❌ Deny", buttonLabel("deny"))
	// Fallback: unknown action is rendered as-is.
	assert.True(t, strings.Contains(buttonLabel("something_new"), "something_new"))
}

func TestApprovalSubscriber_TracksPending_OnCreatedEvent(t *testing.T) {
	sub := NewApprovalSubscriber(nil, nil) // bot/client are nil — we only test state
	req := &client.ApprovalRequest{ID: "abcd", Kind: "skill_install", Actions: []string{"approve", "deny"}}

	// Simulate "created" event bookkeeping without actually sending to Telegram.
	sub.trackPending(req, 12345, 67)
	assert.True(t, sub.hasPending("abcd"))
	chatID, messageID, ok := sub.consumePending("abcd")
	assert.True(t, ok)
	assert.Equal(t, int64(12345), chatID)
	assert.Equal(t, 67, messageID)
	assert.False(t, sub.hasPending("abcd"))
}

func TestApprovalSubscriber_ConsumePending_UnknownID(t *testing.T) {
	sub := NewApprovalSubscriber(nil, nil)
	_, _, ok := sub.consumePending("nope")
	assert.False(t, ok)
}

func TestBot_CallbackQuery_Routes_ApprovalPrefix(t *testing.T) {
	called := ""
	b := &Bot{}
	b.SetOnApprovalCallback(func(data string) (bool, string) {
		called = data
		return true, "approve"
	})
	// Simulate a "ap:" prefix; approval callback takes it.
	_, _ = b.dispatchCallback("ap:abcd:approve")
	assert.Equal(t, "ap:abcd:approve", called)
}

func TestBot_CallbackQuery_FallsBackTo_Permission(t *testing.T) {
	apCalled := ""
	b := &Bot{permPending: map[string]*permEntry{}}
	b.SetOnApprovalCallback(func(data string) (bool, string) {
		apCalled = data
		return false, "" // declines to handle
	})
	handled, _ := b.dispatchCallback("pa:XYZAB")
	// Didn't route through approval path (no match), permission path returned
	// false too (no pending code). Handled is false → "expired" in UI.
	assert.False(t, handled)
	assert.Equal(t, "", apCalled) // approval callback refused, so didn't "match"
}

func TestBot_CallbackQuery_ApPrefix_NilCallback_FallsThrough(t *testing.T) {
	b := &Bot{permPending: map[string]*permEntry{}}
	// onApprovalCallback is nil → dispatch should fall through to the
	// permission path; "ap:..." doesn't match pa:/pd:, so unhandled.
	handled, _ := b.dispatchCallback("ap:abcd:approve")
	assert.False(t, handled)
}
