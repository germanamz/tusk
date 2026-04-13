package mcp

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
)

func (s *Server) handleWorkflowCreate(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return mcp.NewToolResultError("not implemented"), nil
}

func (s *Server) handleWorkflowModify(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return mcp.NewToolResultError("not implemented"), nil
}

func (s *Server) handleWorkflowDelete(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return mcp.NewToolResultError("not implemented"), nil
}
