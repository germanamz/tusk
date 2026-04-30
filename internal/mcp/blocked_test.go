package mcp

import (
	"strings"
	"testing"

	"github.com/germanamz/tusk/config"
	"github.com/mark3labs/mcp-go/mcp"
)

func blockedReq(args map[string]any) mcp.CallToolRequest {
	return mcp.CallToolRequest{
		Params: mcp.CallToolParams{Arguments: args},
	}
}

func TestCheckBlocked_FieldAbsent(test *testing.T) {
	server := mustNew(test, config.MCPConfig{
		BlockedFields: map[string][]string{"tusk_task_modify": {"priority"}},
	})
	res := server.checkBlocked("tusk_task_modify", blockedReq(map[string]any{"short_id": "abc"}))
	if res != nil {
		test.Fatalf("expected nil for absent field, got %#v", res)
	}
}

func TestCheckBlocked_FieldNil(test *testing.T) {
	server := mustNew(test, config.MCPConfig{
		BlockedFields: map[string][]string{"tusk_task_modify": {"priority"}},
	})
	res := server.checkBlocked("tusk_task_modify", blockedReq(map[string]any{"priority": nil}))
	if res != nil {
		test.Fatalf("expected nil for nil-valued field, got %#v", res)
	}
}

func TestCheckBlocked_FieldSupplied(test *testing.T) {
	server := mustNew(test, config.MCPConfig{
		BlockedFields: map[string][]string{"tusk_task_modify": {"priority"}},
	})
	res := server.checkBlocked("tusk_task_modify", blockedReq(map[string]any{"priority": float64(3)}))
	if res == nil {
		test.Fatal("expected error result, got nil")
	}
	if !res.IsError {
		test.Fatal("expected IsError=true")
	}
	msg := toolErrorMsg(test, res)
	if !strings.Contains(msg, "priority") {
		test.Errorf("message missing field name: %q", msg)
	}
	if !strings.Contains(msg, "tusk_task_modify") {
		test.Errorf("message missing tool name: %q", msg)
	}
}

func TestCheckBlocked_MultipleFieldsSorted(test *testing.T) {
	server := mustNew(test, config.MCPConfig{
		BlockedFields: map[string][]string{
			"tusk_task_modify": {"priority", "due", "title"},
		},
	})
	res := server.checkBlocked("tusk_task_modify", blockedReq(map[string]any{
		"priority": float64(3),
		"due":      "2026-01-01",
		"title":    "new",
	}))
	if res == nil || !res.IsError {
		test.Fatal("expected error result")
	}
	msg := toolErrorMsg(test, res)
	want := "[due, priority, title]"
	if !strings.Contains(msg, want) {
		test.Errorf("message %q missing sorted list %q", msg, want)
	}
}

func TestCheckBlocked_EmptyBlockList(test *testing.T) {
	server := mustNew(test, config.MCPConfig{})
	res := server.checkBlocked("tusk_task_modify", blockedReq(map[string]any{"priority": float64(3)}))
	if res != nil {
		test.Fatalf("expected nil with empty block list, got %#v", res)
	}
}

func TestBlockedFields_BlocksUrgencyOverrides(test *testing.T) {
	server := mustNew(test, config.MCPConfig{
		BlockedFields: map[string][]string{
			"tusk_task_modify": {"urgency_overrides", "urgency_overrides_clear"},
		},
	})
	res := server.checkBlocked("tusk_task_modify", blockedReq(map[string]any{
		"urgency_overrides": map[string]any{"priority_weight": float64(5)},
	}))
	if res == nil || !res.IsError {
		test.Fatal("expected blocked-field error for urgency_overrides")
	}
	msg := toolErrorMsg(test, res)
	if !strings.Contains(msg, "urgency_overrides") {
		test.Errorf("message missing field name: %q", msg)
	}
	if !strings.Contains(msg, "tusk_task_modify") {
		test.Errorf("message missing tool name: %q", msg)
	}

	res = server.checkBlocked("tusk_task_modify", blockedReq(map[string]any{
		"urgency_overrides_clear": true,
	}))
	if res == nil || !res.IsError {
		test.Fatal("expected blocked-field error for urgency_overrides_clear")
	}
	msg = toolErrorMsg(test, res)
	if !strings.Contains(msg, "urgency_overrides_clear") {
		test.Errorf("message missing field name: %q", msg)
	}
}

func toolErrorMsg(test *testing.T, res *mcp.CallToolResult) string {
	test.Helper()
	if len(res.Content) == 0 {
		test.Fatal("result has no content")
	}
	text, ok := res.Content[0].(mcp.TextContent)
	if !ok {
		test.Fatalf("expected TextContent, got %T", res.Content[0])
	}
	return text.Text
}
