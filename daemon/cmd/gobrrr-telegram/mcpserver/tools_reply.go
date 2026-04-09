package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"

	mcp "github.com/mark3labs/mcp-go/mcp"

	"os"

	"github.com/racterub/gobrrr/cmd/gobrrr-telegram/bot"
	"github.com/racterub/gobrrr/cmd/gobrrr-telegram/chunker"
)

func replyTool() mcp.Tool {
	return mcp.NewTool("reply",
		mcp.WithDescription("Send a text reply to a Telegram chat. Optionally attach files or thread to a specific message."),
		mcp.WithString("chat_id", mcp.Required(), mcp.Description("Telegram chat_id as string")),
		mcp.WithString("text", mcp.Required()),
		mcp.WithNumber("reply_to", mcp.Description("message_id to reply to (optional)")),
		mcp.WithArray("files", mcp.Description("absolute file paths to attach (optional)")),
	)
}

func (s *Server) handleReply(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	chatIDStr := req.GetString("chat_id", "")
	text := req.GetString("text", "")
	replyToF := req.GetFloat("reply_to", 0)
	filesRaw := req.GetStringSlice("files", nil)

	chatID, err := s.assertAllowedChatID(chatIDStr)
	if err != nil {
		return nil, err
	}
	for _, p := range filesRaw {
		if err := AssertSendable(p, s.stateDir); err != nil {
			return nil, err
		}
	}
	a, _ := s.store.Load()
	mode := chunker.Mode(a.ChunkMode)
	if mode == "" {
		mode = chunker.ModeLength
	}
	limit := a.TextChunkLimit
	if limit == 0 {
		limit = chunker.HardCap
	}
	chunks := chunker.Split(text, mode, limit)

	replyMode := a.ReplyToMode
	if replyMode == "" {
		replyMode = "first"
	}
	var ids []int
	for i, c := range chunks {
		var rt int
		switch replyMode {
		case "all":
			rt = int(replyToF)
		case "first":
			if i == 0 {
				rt = int(replyToF)
			}
		}
		id, err := s.b.SendText(ctx, chatID, rt, c)
		if err != nil {
			return nil, fmt.Errorf("send chunk %d: %w", i, err)
		}
		ids = append(ids, id)
	}
	// Files sent after text. Document-only for now (photos also go as document;
	// Telegram clients render image documents fine).
	for _, p := range filesRaw {
		id, err := s.b.SendDocument(ctx, chatID, p)
		if err != nil {
			return nil, fmt.Errorf("send file %s: %w", p, err)
		}
		ids = append(ids, id)
	}
	// Swap the ack reaction on the most recent inbound message in this chat
	// to the "done" reaction. Only fires for the first reply after an ack;
	// subsequent replies in the same task are no-ops.
	if mid, ok := s.b.ConsumePendingAck(chatID); ok {
		if err := s.b.React(ctx, chatID, mid, bot.DoneReactionEmoji); err != nil {
			fmt.Fprintf(os.Stderr, "gobrrr-telegram: done react: %v\n", err)
		}
	}

	j, _ := json.Marshal(map[string]any{"message_ids": ids})
	return mcp.NewToolResultText(string(j)), nil
}
