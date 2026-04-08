package mcpserver

import (
	"context"
	"fmt"
	"time"

	"github.com/go-telegram/bot/models"
)

// EmitInbound sends a notifications/claude/channel to the connected MCP client.
// Claude Code constructs the <channel> tag itself from `meta`; we send the
// raw message text as `content`, matching the official telegram plugin.
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
	body := msg.Text
	if body == "" && msg.Caption != "" {
		body = msg.Caption
	}

	meta := map[string]any{
		"chat_id":    fmt.Sprintf("%d", msg.Chat.ID),
		"message_id": fmt.Sprintf("%d", msg.ID),
		"user":       user,
		"user_id":    fmt.Sprintf("%d", msg.From.ID),
		"ts":         ts,
	}
	if attachPath != "" && msg.Photo != nil {
		meta["image_path"] = attachPath
	} else if attachFileID != "" {
		meta["attachment_file_id"] = attachFileID
	}

	params := map[string]any{
		"content": body,
		"meta":    meta,
	}
	s.s.SendNotificationToAllClients("notifications/claude/channel", params)
}
