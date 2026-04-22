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
