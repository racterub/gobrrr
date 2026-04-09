package mcpserver

import (
	"context"

	mcp "github.com/mark3labs/mcp-go/mcp"
)

func reactTool() mcp.Tool {
	return mcp.NewTool("react",
		mcp.WithDescription("Set an emoji reaction on a message (empty string clears)."),
		mcp.WithString("chat_id", mcp.Required()),
		mcp.WithNumber("message_id", mcp.Required()),
		mcp.WithString("emoji", mcp.Required()),
	)
}

func (s *Server) handleReact(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	chatIDStr := req.GetString("chat_id", "")
	midF := req.GetFloat("message_id", 0)
	emoji := req.GetString("emoji", "")
	chatID, err := s.assertAllowedChatID(chatIDStr)
	if err != nil {
		return nil, err
	}
	// Manual react cancels the pending auto-swap: the caller is taking
	// explicit control of reactions on this chat.
	s.b.ClearPendingAck(chatID)
	if err := s.b.React(ctx, chatID, int(midF), emoji); err != nil {
		return nil, err
	}
	return mcp.NewToolResultText("ok"), nil
}
