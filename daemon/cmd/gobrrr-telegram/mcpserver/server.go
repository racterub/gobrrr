package mcpserver

import (
	"context"
	"strconv"

	mcp "github.com/mark3labs/mcp-go/mcp"
	server "github.com/mark3labs/mcp-go/server"

	"github.com/racterub/gobrrr/cmd/gobrrr-telegram/access"
	"github.com/racterub/gobrrr/cmd/gobrrr-telegram/bot"
)

type Server struct {
	s        *server.MCPServer
	b        *bot.Bot
	store    *access.Store
	stateDir string
}

func New(b *bot.Bot, store *access.Store, stateDir string) *Server {
	s := server.NewMCPServer(
		"telegram",
		"0.0.1",
		server.WithInstructions(
			`The sender reads Telegram, not this session. Anything you want them to see must go through the reply tool. Messages arrive as <channel source="telegram" chat_id="..." message_id="..." user="..." ts="...">.`),
		server.WithExperimental(map[string]any{
			"claude/channel": map[string]any{},
		}),
	)
	srv := &Server{s: s, b: b, store: store, stateDir: stateDir}
	srv.registerTools()
	return srv
}

func (s *Server) registerTools() {
	s.s.AddTool(replyTool(), s.handleReply)
	s.s.AddTool(editTool(), s.handleEdit)
	s.s.AddTool(reactTool(), s.handleReact)
	s.s.AddTool(downloadTool(), s.handleDownload)
}

// ServeStdio blocks serving the MCP protocol on stdin/stdout.
func (s *Server) ServeStdio(ctx context.Context) error {
	return server.ServeStdio(s.s)
}

// assertAllowedChatID parses the chat_id string and asserts it is in the
// current access allowlist (either a DM allowFrom entry or a known group).
func (s *Server) assertAllowedChatID(chatID string) (int64, error) {
	a, err := s.store.Load()
	if err != nil {
		return 0, err
	}
	for _, id := range a.AllowFrom {
		if id == chatID {
			n, _ := strconv.ParseInt(chatID, 10, 64)
			return n, nil
		}
	}
	if _, ok := a.Groups[chatID]; ok {
		n, _ := strconv.ParseInt(chatID, 10, 64)
		return n, nil
	}
	return 0, &notAllowedError{chatID: chatID}
}

type notAllowedError struct{ chatID string }

func (e *notAllowedError) Error() string {
	return "chat " + e.chatID + " is not allowlisted — add via /telegram:access"
}

// Ensure import balance
var _ = mcp.NewToolResultText
