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

func TestCheckBlocked_FieldAbsent(t *testing.T) {
	s := mustNew(t, config.MCPConfig{
		BlockedFields: map[string][]string{"tusk_task_modify": {"priority"}},
	})
	res := s.checkBlocked("tusk_task_modify", blockedReq(map[string]any{"short_id": "abc"}))
	if res != nil {
		t.Fatalf("expected nil for absent field, got %#v", res)
	}
}

func TestCheckBlocked_FieldNil(t *testing.T) {
	s := mustNew(t, config.MCPConfig{
		BlockedFields: map[string][]string{"tusk_task_modify": {"priority"}},
	})
	res := s.checkBlocked("tusk_task_modify", blockedReq(map[string]any{"priority": nil}))
	if res != nil {
		t.Fatalf("expected nil for nil-valued field, got %#v", res)
	}
}

func TestCheckBlocked_FieldSupplied(t *testing.T) {
	s := mustNew(t, config.MCPConfig{
		BlockedFields: map[string][]string{"tusk_task_modify": {"priority"}},
	})
	res := s.checkBlocked("tusk_task_modify", blockedReq(map[string]any{"priority": float64(3)}))
	if res == nil {
		t.Fatal("expected error result, got nil")
	}
	if !res.IsError {
		t.Fatal("expected IsError=true")
	}
	msg := toolErrorMsg(t, res)
	if !strings.Contains(msg, "priority") {
		t.Errorf("message missing field name: %q", msg)
	}
	if !strings.Contains(msg, "tusk_task_modify") {
		t.Errorf("message missing tool name: %q", msg)
	}
}

func TestCheckBlocked_MultipleFieldsSorted(t *testing.T) {
	s := mustNew(t, config.MCPConfig{
		BlockedFields: map[string][]string{
			"tusk_task_modify": {"priority", "due", "title"},
		},
	})
	res := s.checkBlocked("tusk_task_modify", blockedReq(map[string]any{
		"priority": float64(3),
		"due":      "2026-01-01",
		"title":    "new",
	}))
	if res == nil || !res.IsError {
		t.Fatal("expected error result")
	}
	msg := toolErrorMsg(t, res)
	want := "[due, priority, title]"
	if !strings.Contains(msg, want) {
		t.Errorf("message %q missing sorted list %q", msg, want)
	}
}

func TestCheckBlocked_EmptyBlockList(t *testing.T) {
	s := mustNew(t, config.MCPConfig{})
	res := s.checkBlocked("tusk_task_modify", blockedReq(map[string]any{"priority": float64(3)}))
	if res != nil {
		t.Fatalf("expected nil with empty block list, got %#v", res)
	}
}

func TestBlockedFields_BlocksUrgencyOverrides(t *testing.T) {
	s := mustNew(t, config.MCPConfig{
		BlockedFields: map[string][]string{
			"tusk_task_modify": {"urgency_overrides", "urgency_overrides_clear"},
		},
	})
	res := s.checkBlocked("tusk_task_modify", blockedReq(map[string]any{
		"urgency_overrides": map[string]any{"priority_weight": float64(5)},
	}))
	if res == nil || !res.IsError {
		t.Fatal("expected blocked-field error for urgency_overrides")
	}
	msg := toolErrorMsg(t, res)
	if !strings.Contains(msg, "urgency_overrides") {
		t.Errorf("message missing field name: %q", msg)
	}
	if !strings.Contains(msg, "tusk_task_modify") {
		t.Errorf("message missing tool name: %q", msg)
	}

	res = s.checkBlocked("tusk_task_modify", blockedReq(map[string]any{
		"urgency_overrides_clear": true,
	}))
	if res == nil || !res.IsError {
		t.Fatal("expected blocked-field error for urgency_overrides_clear")
	}
	msg = toolErrorMsg(t, res)
	if !strings.Contains(msg, "urgency_overrides_clear") {
		t.Errorf("message missing field name: %q", msg)
	}
}

func toolErrorMsg(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	if len(res.Content) == 0 {
		t.Fatal("result has no content")
	}
	text, ok := res.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", res.Content[0])
	}
	return text.Text
}
