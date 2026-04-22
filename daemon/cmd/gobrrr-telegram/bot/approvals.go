package bot

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/go-telegram/bot/models"

	"github.com/racterub/gobrrr/internal/client"
)

// RenderApprovalCard builds the Telegram message body + inline keyboard for an
// approval. Callback data is `ap:{id}:{action}` — kept short to fit Telegram's
// 64-byte callback_data cap with plenty of headroom.
func RenderApprovalCard(req *client.ApprovalRequest) (string, models.InlineKeyboardMarkup) {
	body := req.Title
	if req.Body != "" {
		body += "\n\n" + req.Body
	}

	// For skill_install, surface slug/version/sha from the payload without
	// importing the skill package (the bot is intentionally decoupled from
	// daemon-side types).
	if req.Kind == "skill_install" {
		var p struct {
			Slug    string `json:"slug"`
			Version string `json:"version"`
			SHA256  string `json:"sha256"`
		}
		if err := json.Unmarshal(req.Payload, &p); err == nil {
			if p.Slug != "" {
				body += fmt.Sprintf("\n\nSkill: %s@%s", p.Slug, p.Version)
			}
			if p.SHA256 != "" {
				body += fmt.Sprintf("\nsha256: %s", p.SHA256)
			}
		}
	}

	var row []models.InlineKeyboardButton
	for _, action := range req.Actions {
		row = append(row, models.InlineKeyboardButton{
			Text:         buttonLabel(action),
			CallbackData: "ap:" + req.ID + ":" + action,
		})
	}
	kb := models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{row}}
	return body, kb
}

func buttonLabel(action string) string {
	switch action {
	case "approve":
		return "✅ Approve"
	case "deny":
		return "❌ Deny"
	case "skip_binary":
		return "⏭️ Skip binary"
	}
	return action
}

// ParseApprovalCallback parses an inline-keyboard callback payload of the form
// `ap:{id}:{action}`. Returns (id, action, true) on match, (_,_,false) otherwise.
func ParseApprovalCallback(data string) (string, string, bool) {
	if !strings.HasPrefix(data, "ap:") {
		return "", "", false
	}
	rest := strings.TrimPrefix(data, "ap:")
	i := strings.Index(rest, ":")
	if i <= 0 || i == len(rest)-1 {
		return "", "", false
	}
	return rest[:i], rest[i+1:], true
}
