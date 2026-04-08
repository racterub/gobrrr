package mcpserver

import (
	"context"

	mcp "github.com/mark3labs/mcp-go/mcp"
)

func editTool() mcp.Tool {
	return mcp.NewTool("edit_message",
		mcp.WithDescription("Edit a previously sent Telegram message."),
		mcp.WithString("chat_id", mcp.Required()),
		mcp.WithNumber("message_id", mcp.Required()),
		mcp.WithString("text", mcp.Required()),
	)
}

func (s *Server) handleEdit(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	chatIDStr := req.GetString("chat_id", "")
	midF := req.GetFloat("message_id", 0)
	text := req.GetString("text", "")
	chatID, err := s.assertAllowedChatID(chatIDStr)
	if err != nil {
		return nil, err
	}
	if err := s.b.EditText(ctx, chatID, int(midF), text); err != nil {
		return nil, err
	}
	return mcp.NewToolResultText("ok"), nil
}
