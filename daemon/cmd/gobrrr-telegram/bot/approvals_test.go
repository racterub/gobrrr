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
		"sha256":  "deadbeef",
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
	assert.Equal(t, 3, len(kb.InlineKeyboard[0]))
	// button callback data shape: ap:{id}:{action}
	assert.Equal(t, "ap:abcd:approve", kb.InlineKeyboard[0][0].CallbackData)
	assert.Equal(t, "ap:abcd:skip_binary", kb.InlineKeyboard[0][1].CallbackData)
	assert.Equal(t, "ap:abcd:deny", kb.InlineKeyboard[0][2].CallbackData)
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
