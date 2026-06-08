package mcp

import (
	"context"
	"strings"
	"testing"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
)

func resetRequest(confirm bool) mcpgo.CallToolRequest {
	req := mcpgo.CallToolRequest{}
	req.Params.Name = "tusk_reset"
	req.Params.Arguments = map[string]any{"confirm": confirm}

	return req
}

func TestResetTool_RequiresConfirm(test *testing.T) {
	srv := buildTestServer(test) // from reset_helpers_test.go (Phase 5 Task 4)

	result, err := srv.HandleToolCall(context.Background(), resetRequest(false))
	if err != nil {
		test.Fatalf("HandleToolCall returned transport error: %v", err)
	}

	if result == nil || !result.IsError {
		test.Fatalf("expected an error result when confirm is false, got %+v", result)
	}

	// Assert the error is the CONFIRM gate, not the unknown-tool fallback — so the
	// red step is genuinely red before the tool is registered.
	if body := textOf(result); !strings.Contains(body, "confirm") {
		test.Fatalf("expected a confirm-gate error mentioning 'confirm', got: %s", body)
	}
}
