package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/germanamz/tusk/internal/index"
	"github.com/germanamz/tusk/internal/status"
	mcpgo "github.com/mark3labs/mcp-go/mcp"
)

func registerTools(srv *Server) {
	registerStatusTool(srv)
}

func registerStatusTool(srv *Server) {
	tool := mcpgo.NewTool("tusk_status",
		mcpgo.WithDescription("Quick workspace summary: node counts by type, edge count, embed queue depth, last reindex time."),
	)

	handler := func(ctx context.Context, request mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		snap, snapErr := status.Snapshot(status.Config{
			Nodes:      srv.runtime.Nodes,
			Edges:      srv.runtime.Edges,
			EmbedQueue: srv.runtime.EmbedQueue,
			Meta:       srv.runtime.Meta,
		})

		if snapErr != nil {
			return toolError(snapErr), nil
		}

		return toolJSON(map[string]any{
			"nodes_by_type":     snap.NodesByType,
			"edge_count":        snap.EdgeCount,
			"embed_queue_depth": snap.EmbedQueueDepth,
			"last_reindex_at":   snap.LastReindexAt,
		})
	}

	srv.register(tool, handler)
}

// argString extracts a required string argument from the request. Returns an
// error when the key is absent or its value is not a string.
func argString(request mcpgo.CallToolRequest, key string) (string, error) {
	args := request.GetArguments()
	value, ok := args[key].(string)

	if !ok {
		return "", fmt.Errorf("missing or non-string argument %q", key)
	}

	return value, nil
}

// argStringOptional extracts an optional string argument from the request.
// Returns an empty string when the key is absent or its value is not a string.
func argStringOptional(request mcpgo.CallToolRequest, key string) string {
	args := request.GetArguments()
	value, _ := args[key].(string)

	return value
}

// argInt extracts a required numeric argument from the request as an int.
func argInt(request mcpgo.CallToolRequest, key string) (int, error) {
	args := request.GetArguments()
	raw, ok := args[key]

	if !ok {
		return 0, fmt.Errorf("missing argument %q", key)
	}

	switch typed := raw.(type) {
	case float64:
		return int(typed), nil
	case int:
		return typed, nil
	}

	return 0, fmt.Errorf("argument %q is not a number", key)
}

// argIntOptional extracts an optional numeric argument from the request as an
// int. Returns defaultValue when the key is absent or not numeric.
func argIntOptional(request mcpgo.CallToolRequest, key string, defaultValue int) int {
	if value, parseErr := argInt(request, key); parseErr == nil {
		return value
	}

	return defaultValue
}

// argMap extracts an optional map argument from the request.
func argMap(request mcpgo.CallToolRequest, key string) map[string]any {
	args := request.GetArguments()
	value, _ := args[key].(map[string]any)

	return value
}

// argStringSlice extracts an optional string-slice argument from the request.
func argStringSlice(request mcpgo.CallToolRequest, key string) []string {
	args := request.GetArguments()
	raw, ok := args[key].([]any)

	if !ok {
		return nil
	}

	out := make([]string, 0, len(raw))

	for _, item := range raw {
		if str, isString := item.(string); isString {
			out = append(out, str)
		}
	}

	return out
}

// toolError wraps an error as a CallToolResult error.
func toolError(err error) *mcpgo.CallToolResult {
	return mcpgo.NewToolResultError(err.Error())
}

// toolJSON marshals payload to JSON and returns it as a text CallToolResult.
func toolJSON(payload any) (*mcpgo.CallToolResult, error) {
	body, marshalErr := json.Marshal(payload)

	if marshalErr != nil {
		return nil, marshalErr
	}

	return mcpgo.NewToolResultText(string(body)), nil
}

// Keep helper imports and functions alive until later tasks consume them.
var _ = index.NewNodeRepo
var _ = argString
var _ = argStringOptional
var _ = argInt
var _ = argIntOptional
var _ = argMap
var _ = argStringSlice
