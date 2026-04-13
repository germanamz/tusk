package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/germanamz/tusk/config"
	"github.com/mark3labs/mcp-go/mcp"
)

// configShowResponse is the JSON payload returned by tusk_config_show.
type configShowResponse struct {
	ActiveFile string          `json:"active_file"`
	Effective  json.RawMessage `json:"effective"`
}

func (s *Server) handleConfigShow(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	cfg, err := config.Load(s.loadOpts...)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("loading config: %v", err)), nil
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("marshaling config: %v", err)), nil
	}
	return toolResultJSON(configShowResponse{
		ActiveFile: cfg.Sources.File,
		Effective:  raw,
	})
}

func (s *Server) handleConfigSet(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return mcp.NewToolResultError("not implemented"), nil
}

// HandleConfigShowForTest exposes handleConfigShow for internal package tests.
func (s *Server) HandleConfigShowForTest(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return s.handleConfigShow(ctx, req)
}
