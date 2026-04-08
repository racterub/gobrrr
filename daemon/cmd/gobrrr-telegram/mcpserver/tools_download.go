package mcpserver

import (
	"context"

	mcp "github.com/mark3labs/mcp-go/mcp"
)

func downloadTool() mcp.Tool {
	return mcp.NewTool("download_attachment",
		mcp.WithDescription("Download a Telegram file by file_id into the channel inbox and return the local path."),
		mcp.WithString("file_id", mcp.Required()),
	)
}

func (s *Server) handleDownload(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	fileID := req.GetString("file_id", "")
	path, err := s.b.DownloadByFileID(ctx, fileID)
	if err != nil {
		return nil, err
	}
	return mcp.NewToolResultText(path), nil
}
