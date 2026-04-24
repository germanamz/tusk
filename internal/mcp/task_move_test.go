package mcp

import (
	"context"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

// createTask creates a task through the MCP handler and returns (short_id, version).
func createTask(t *testing.T, ctx context.Context, s *Server, args map[string]any) (string, float64) {
	t.Helper()
	result, err := s.handleTaskCreate(ctx, callToolRequest(args))
	if err != nil {
		t.Fatalf("handleTaskCreate: %v", err)
	}
	parsed := parseToolResult(t, result)
	return parsed["short_id"].(string), parsed["version"].(float64)
}

func TestHandleTaskMove_Before(t *testing.T) {
	s := testServer(t)
	ctx := context.Background()

	aID, _ := createTask(t, ctx, s, map[string]any{"title": "A"})
	bID, _ := createTask(t, ctx, s, map[string]any{"title": "B"})
	cID, cVer := createTask(t, ctx, s, map[string]any{"title": "C"})

	result, err := s.handleTaskMove(ctx, callToolRequest(map[string]any{
		"task_id":   cID,
		"position":  "before",
		"target_id": bID,
		"version":   cVer,
	}))
	if err != nil {
		t.Fatalf("handleTaskMove: %v", err)
	}
	parsed := parseToolResult(t, result)
	if parsed["short_id"] != cID {
		t.Fatalf("expected moved task %s in response, got %v", cID, parsed["short_id"])
	}
	// After move, C should land between A (1.0) and B (2.0).
	order, ok := parsed["order"].(float64)
	if !ok {
		t.Fatalf("expected numeric order in response, got %T", parsed["order"])
	}
	if order <= 1.0 || order >= 2.0 {
		t.Fatalf("expected C.order in (1.0, 2.0), got %v", order)
	}
	_ = aID
}

func TestHandleTaskMove_After(t *testing.T) {
	s := testServer(t)
	ctx := context.Background()

	_, _ = createTask(t, ctx, s, map[string]any{"title": "A"})
	bID, _ := createTask(t, ctx, s, map[string]any{"title": "B"})
	cID, cVer := createTask(t, ctx, s, map[string]any{"title": "C"})

	result, err := s.handleTaskMove(ctx, callToolRequest(map[string]any{
		"task_id":   cID,
		"position":  "after",
		"target_id": bID,
		"version":   cVer,
	}))
	if err != nil {
		t.Fatalf("handleTaskMove: %v", err)
	}
	parsed := parseToolResult(t, result)
	// After B (2.0) with no next → 3.0; our C was created at 3.0 already so
	// midpoint math could produce anything > 2.0. Just assert > B.
	order := parsed["order"].(float64)
	if order <= 2.0 {
		t.Fatalf("expected C.order > 2.0, got %v", order)
	}
}

func TestHandleTaskMove_First_WithParent(t *testing.T) {
	s := testServer(t)
	ctx := context.Background()

	parentID, _ := createTask(t, ctx, s, map[string]any{"title": "Parent"})
	childID, childVer := createTask(t, ctx, s, map[string]any{"title": "Child"})

	result, err := s.handleTaskMove(ctx, callToolRequest(map[string]any{
		"task_id":   childID,
		"position":  "first",
		"parent_id": parentID,
		"version":   childVer,
	}))
	if err != nil {
		t.Fatalf("handleTaskMove: %v", err)
	}
	parsed := parseToolResult(t, result)
	if parsed["parent_id"] == nil {
		t.Fatal("expected parent_id to be set in response")
	}
}

func TestHandleTaskMove_First_NullParent_MovesToRoot(t *testing.T) {
	s := testServer(t)
	ctx := context.Background()

	parentID, _ := createTask(t, ctx, s, map[string]any{"title": "Parent"})
	childID, childVer := createTask(t, ctx, s, map[string]any{"title": "Child", "parent": parentID})

	result, err := s.handleTaskMove(ctx, callToolRequest(map[string]any{
		"task_id":   childID,
		"position":  "first",
		"parent_id": nil,
		"version":   childVer,
	}))
	if err != nil {
		t.Fatalf("handleTaskMove: %v", err)
	}
	parsed := parseToolResult(t, result)
	if _, ok := parsed["parent_id"]; ok && parsed["parent_id"] != nil {
		t.Fatalf("expected parent_id to be absent/null (moved to root), got %v", parsed["parent_id"])
	}
}

func TestHandleTaskMove_AbsentParent_KeepsCurrentParent(t *testing.T) {
	s := testServer(t)
	ctx := context.Background()

	parentID, _ := createTask(t, ctx, s, map[string]any{"title": "Parent"})
	childID, childVer := createTask(t, ctx, s, map[string]any{"title": "Child", "parent": parentID})

	// No parent_id in arguments; position=first should keep the current
	// parent.
	result, err := s.handleTaskMove(ctx, callToolRequest(map[string]any{
		"task_id":  childID,
		"position": "first",
		"version":  childVer,
	}))
	if err != nil {
		t.Fatalf("handleTaskMove: %v", err)
	}
	parsed := parseToolResult(t, result)
	if parsed["parent_id"] == nil {
		t.Fatal("expected parent_id to still be set (parent unchanged)")
	}
}

func TestHandleTaskMove_BeforeWithoutTarget_IsError(t *testing.T) {
	s := testServer(t)
	ctx := context.Background()

	aID, aVer := createTask(t, ctx, s, map[string]any{"title": "A"})

	result, _ := s.handleTaskMove(ctx, callToolRequest(map[string]any{
		"task_id":  aID,
		"position": "before",
		"version":  aVer,
	}))
	msg := getToolErrorText(t, result)
	if !strings.Contains(msg, "target_id") {
		t.Fatalf("expected error to mention target_id, got %q", msg)
	}
}

func TestHandleTaskMove_FirstWithTarget_IsError(t *testing.T) {
	s := testServer(t)
	ctx := context.Background()

	aID, aVer := createTask(t, ctx, s, map[string]any{"title": "A"})
	bID, _ := createTask(t, ctx, s, map[string]any{"title": "B"})

	result, _ := s.handleTaskMove(ctx, callToolRequest(map[string]any{
		"task_id":   aID,
		"position":  "first",
		"target_id": bID,
		"version":   aVer,
	}))
	msg := getToolErrorText(t, result)
	if !strings.Contains(msg, "target_id") {
		t.Fatalf("expected error to mention target_id, got %q", msg)
	}
}

func TestHandleTaskMove_VersionConflict(t *testing.T) {
	s := testServer(t)
	ctx := context.Background()

	aID, _ := createTask(t, ctx, s, map[string]any{"title": "A"})
	bID, _ := createTask(t, ctx, s, map[string]any{"title": "B"})

	result, _ := s.handleTaskMove(ctx, callToolRequest(map[string]any{
		"task_id":   aID,
		"position":  "before",
		"target_id": bID,
		"version":   float64(999),
	}))
	msg := getToolErrorText(t, result)
	if !strings.Contains(msg, "version conflict") {
		t.Fatalf("expected version conflict, got %q", msg)
	}
}

func TestHandleTaskMove_Cycle(t *testing.T) {
	s := testServer(t)
	ctx := context.Background()

	parentID, parentVer := createTask(t, ctx, s, map[string]any{"title": "Parent"})
	_, _ = createTask(t, ctx, s, map[string]any{"title": "Child", "parent": parentID})

	// Attempting to move parent under its own child forms a cycle.
	// We need the child's ID to pass as parent_id.
	listResult, err := s.handleTaskList(ctx, callToolRequest(map[string]any{}))
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	items := parseToolResultArray(t, listResult)
	var childID string
	for _, it := range items {
		if it["short_id"] != parentID {
			childID = it["short_id"].(string)
			break
		}
	}
	if childID == "" {
		t.Fatal("could not locate child in list response")
	}

	result, _ := s.handleTaskMove(ctx, callToolRequest(map[string]any{
		"task_id":   parentID,
		"position":  "first",
		"parent_id": childID,
		"version":   parentVer,
	}))
	msg := getToolErrorText(t, result)
	if !strings.Contains(msg, "cycle") {
		t.Fatalf("expected cycle error, got %q", msg)
	}
}

func TestHandleTaskResequence_Root(t *testing.T) {
	s := testServer(t)
	ctx := context.Background()

	_, _ = createTask(t, ctx, s, map[string]any{"title": "A"})
	_, _ = createTask(t, ctx, s, map[string]any{"title": "B"})

	result, err := s.handleTaskResequence(ctx, callToolRequest(map[string]any{
		"parent_id": nil,
	}))
	if err != nil {
		t.Fatalf("handleTaskResequence: %v", err)
	}
	parsed := parseToolResult(t, result)
	if _, ok := parsed["rewritten"]; !ok {
		t.Fatalf("expected 'rewritten' key in response, got %v", parsed)
	}
	if parsed["parent_id"] != nil {
		t.Fatalf("expected parent_id=null for root, got %v", parsed["parent_id"])
	}
}

func TestHandleTaskResequence_UnderParent(t *testing.T) {
	s := testServer(t)
	ctx := context.Background()

	parentID, _ := createTask(t, ctx, s, map[string]any{"title": "Parent"})
	_, _ = createTask(t, ctx, s, map[string]any{"title": "Child-A", "parent": parentID})

	result, err := s.handleTaskResequence(ctx, callToolRequest(map[string]any{
		"parent_id": parentID,
	}))
	if err != nil {
		t.Fatalf("handleTaskResequence: %v", err)
	}
	parsed := parseToolResult(t, result)
	if parsed["parent_id"] == nil {
		t.Fatalf("expected non-null parent_id in response, got nil")
	}
}

func TestHandleTaskResequence_MissingParentID_IsError(t *testing.T) {
	s := testServer(t)
	ctx := context.Background()

	result, _ := s.handleTaskResequence(ctx, callToolRequest(map[string]any{}))
	msg := getToolErrorText(t, result)
	if !strings.Contains(msg, "parent_id") {
		t.Fatalf("expected error to mention parent_id, got %q", msg)
	}
}

func TestHandleTaskMove_BlockedField(t *testing.T) {
	s := testServer(t)
	ctx := context.Background()

	aID, _ := createTask(t, ctx, s, map[string]any{"title": "A"})
	bID, _ := createTask(t, ctx, s, map[string]any{"title": "B"})

	// Block parent_id via checkBlocked config.
	s.cfg.BlockedFields = map[string][]string{"tusk_task_move": {"parent_id"}}

	result, _ := s.handleTaskMove(ctx, callToolRequest(map[string]any{
		"task_id":   aID,
		"position":  "first",
		"parent_id": bID,
		"version":   float64(1),
	}))
	msg := getToolErrorText(t, result)
	if !strings.Contains(msg, "parent_id") {
		t.Fatalf("expected blocked-field error mentioning parent_id, got %q", msg)
	}
}

// Ensure mcp.CallToolRequest wires the params correctly for the negative
// tests above (helper used by getToolErrorText above assumes an error result).
var _ = mcp.NewToolResultError
