package mcpserver

import (
	"context"
	"fmt"
	"html"
	"time"

	"github.com/go-telegram/bot/models"
)

// EmitInbound builds the <channel> tag and sends a notifications/claude/channel
// to the connected MCP client.
func (s *Server) EmitInbound(ctx context.Context, upd *models.Update, attachPath, attachFileID string) {
	msg := upd.Message
	if msg == nil {
		return
	}
	ts := time.Unix(int64(msg.Date), 0).UTC().Format(time.RFC3339)
	user := msg.From.Username
	if user == "" {
		user = msg.From.FirstName
	}
	attrs := fmt.Sprintf(
		`source="telegram" chat_id="%d" message_id="%d" user=%q ts=%q`,
		msg.Chat.ID, msg.ID, user, ts,
	)
	if attachPath != "" && msg.Photo != nil {
		attrs += fmt.Sprintf(` image_path=%q`, attachPath)
	} else if attachFileID != "" {
		attrs += fmt.Sprintf(` attachment_file_id=%q`, attachFileID)
	}
	body := msg.Text
	if body == "" && msg.Caption != "" {
		body = msg.Caption
	}
	content := fmt.Sprintf("<channel %s>\n%s\n</channel>", attrs, html.EscapeString(body))

	params := map[string]any{
		"content": content,
		"meta": map[string]any{
			"chat_id":    fmt.Sprintf("%d", msg.Chat.ID),
			"message_id": msg.ID,
			"user":       user,
			"ts":         ts,
		},
	}
	s.s.SendNotificationToAllClients("notifications/claude/channel", params)
}
