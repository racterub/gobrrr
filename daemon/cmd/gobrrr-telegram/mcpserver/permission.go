package mcpserver

import (
	"context"
	"fmt"
	"os"

	mcp "github.com/mark3labs/mcp-go/mcp"
)

// registerPermissionRelay wires the claude/channel/permission protocol:
// inbound permission_request notifications are forwarded to the bot, which
// pushes a Telegram prompt; the operator's reply is sent back via
// SendPermissionDecision.
func (s *Server) registerPermissionRelay() {
	s.s.AddNotificationHandler(
		"notifications/claude/channel/permission_request",
		func(ctx context.Context, n mcp.JSONRPCNotification) {
			f := n.Params.AdditionalFields
			requestID, _ := f["request_id"].(string)
			toolName, _ := f["tool_name"].(string)
			description, _ := f["description"].(string)
			inputPreview, _ := f["input_preview"].(string)
			if requestID == "" {
				fmt.Fprintf(os.Stderr, "gobrrr-telegram: permission_request missing request_id\n")
				return
			}
			s.b.HandlePermissionRequest(ctx, requestID, toolName, description, inputPreview)
		},
	)
}

// SendPermissionDecision relays the operator's verdict back to Claude Code.
func (s *Server) SendPermissionDecision(requestID string, allow bool) {
	behavior := "deny"
	if allow {
		behavior = "allow"
	}
	s.s.SendNotificationToAllClients("notifications/claude/channel/permission", map[string]any{
		"request_id": requestID,
		"behavior":   behavior,
	})
}
