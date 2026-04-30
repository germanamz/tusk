package mcp

import (
	"context"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

// createTask creates a task through the MCP handler and returns (short_id, version).
func createTask(test *testing.T, ctx context.Context, server *Server, args map[string]any) (string, float64) {
	test.Helper()

	result, createErr := server.handleTaskCreate(ctx, callToolRequest(args))

	if createErr != nil {
		test.Fatalf("handleTaskCreate: %v", createErr)
	}

	parsed := parseToolResult(test, result)
	return parsed["short_id"].(string), parsed["version"].(float64)
}

func TestHandleTaskMove_Before(test *testing.T) {
	server := testServer(test)
	ctx := context.Background()

	aID, _ := createTask(test, ctx, server, map[string]any{"title": "A"})
	bID, _ := createTask(test, ctx, server, map[string]any{"title": "B"})
	cID, cVer := createTask(test, ctx, server, map[string]any{"title": "C"})

	result, moveErr := server.handleTaskMove(ctx, callToolRequest(map[string]any{
		"task_id":   cID,
		"position":  "before",
		"target_id": bID,
		"version":   cVer,
	}))

	if moveErr != nil {
		test.Fatalf("handleTaskMove: %v", moveErr)
	}

	parsed := parseToolResult(test, result)
	if parsed["short_id"] != cID {
		test.Fatalf("expected moved task %s in response, got %v", cID, parsed["short_id"])
	}
	// After move, C should land between A (1.0) and B (2.0).
	order, ok := parsed["order"].(float64)
	if !ok {
		test.Fatalf("expected numeric order in response, got %T", parsed["order"])
	}
	if order <= 1.0 || order >= 2.0 {
		test.Fatalf("expected C.order in (1.0, 2.0), got %v", order)
	}
	_ = aID
}

func TestHandleTaskMove_After(test *testing.T) {
	server := testServer(test)
	ctx := context.Background()

	_, _ = createTask(test, ctx, server, map[string]any{"title": "A"})
	bID, _ := createTask(test, ctx, server, map[string]any{"title": "B"})
	cID, cVer := createTask(test, ctx, server, map[string]any{"title": "C"})

	result, moveErr := server.handleTaskMove(ctx, callToolRequest(map[string]any{
		"task_id":   cID,
		"position":  "after",
		"target_id": bID,
		"version":   cVer,
	}))

	if moveErr != nil {
		test.Fatalf("handleTaskMove: %v", moveErr)
	}

	parsed := parseToolResult(test, result)
	// After B (2.0) with no next → 3.0; our C was created at 3.0 already so
	// midpoint math could produce anything > 2.0. Just assert > B.
	order := parsed["order"].(float64)
	if order <= 2.0 {
		test.Fatalf("expected C.order > 2.0, got %v", order)
	}
}

func TestHandleTaskMove_First_WithParent(test *testing.T) {
	server := testServer(test)
	ctx := context.Background()

	parentID, _ := createTask(test, ctx, server, map[string]any{"title": "Parent"})
	childID, childVer := createTask(test, ctx, server, map[string]any{"title": "Child"})

	result, moveErr := server.handleTaskMove(ctx, callToolRequest(map[string]any{
		"task_id":   childID,
		"position":  "first",
		"parent_id": parentID,
		"version":   childVer,
	}))

	if moveErr != nil {
		test.Fatalf("handleTaskMove: %v", moveErr)
	}

	parsed := parseToolResult(test, result)
	if parsed["parent_id"] == nil {
		test.Fatal("expected parent_id to be set in response")
	}
}

func TestHandleTaskMove_First_NullParent_MovesToRoot(test *testing.T) {
	server := testServer(test)
	ctx := context.Background()

	parentID, _ := createTask(test, ctx, server, map[string]any{"title": "Parent"})
	childID, childVer := createTask(test, ctx, server, map[string]any{"title": "Child", "parent": parentID})

	result, moveErr := server.handleTaskMove(ctx, callToolRequest(map[string]any{
		"task_id":   childID,
		"position":  "first",
		"parent_id": nil,
		"version":   childVer,
	}))

	if moveErr != nil {
		test.Fatalf("handleTaskMove: %v", moveErr)
	}

	parsed := parseToolResult(test, result)
	if _, ok := parsed["parent_id"]; ok && parsed["parent_id"] != nil {
		test.Fatalf("expected parent_id to be absent/null (moved to root), got %v", parsed["parent_id"])
	}
}

func TestHandleTaskMove_AbsentParent_KeepsCurrentParent(test *testing.T) {
	server := testServer(test)
	ctx := context.Background()

	parentID, _ := createTask(test, ctx, server, map[string]any{"title": "Parent"})
	childID, childVer := createTask(test, ctx, server, map[string]any{"title": "Child", "parent": parentID})

	// No parent_id in arguments; position=first should keep the current
	// parent.
	result, moveErr := server.handleTaskMove(ctx, callToolRequest(map[string]any{
		"task_id":  childID,
		"position": "first",
		"version":  childVer,
	}))

	if moveErr != nil {
		test.Fatalf("handleTaskMove: %v", moveErr)
	}

	parsed := parseToolResult(test, result)
	if parsed["parent_id"] == nil {
		test.Fatal("expected parent_id to still be set (parent unchanged)")
	}
}

func TestHandleTaskMove_BeforeWithoutTarget_IsError(test *testing.T) {
	server := testServer(test)
	ctx := context.Background()

	aID, aVer := createTask(test, ctx, server, map[string]any{"title": "A"})

	result, _ := server.handleTaskMove(ctx, callToolRequest(map[string]any{
		"task_id":  aID,
		"position": "before",
		"version":  aVer,
	}))
	msg := getToolErrorText(test, result)
	if !strings.Contains(msg, "target_id") {
		test.Fatalf("expected error to mention target_id, got %q", msg)
	}
}

func TestHandleTaskMove_FirstWithTarget_IsError(test *testing.T) {
	server := testServer(test)
	ctx := context.Background()

	aID, aVer := createTask(test, ctx, server, map[string]any{"title": "A"})
	bID, _ := createTask(test, ctx, server, map[string]any{"title": "B"})

	result, _ := server.handleTaskMove(ctx, callToolRequest(map[string]any{
		"task_id":   aID,
		"position":  "first",
		"target_id": bID,
		"version":   aVer,
	}))
	msg := getToolErrorText(test, result)
	if !strings.Contains(msg, "target_id") {
		test.Fatalf("expected error to mention target_id, got %q", msg)
	}
}

func TestHandleTaskMove_VersionConflict(test *testing.T) {
	server := testServer(test)
	ctx := context.Background()

	aID, _ := createTask(test, ctx, server, map[string]any{"title": "A"})
	bID, _ := createTask(test, ctx, server, map[string]any{"title": "B"})

	result, _ := server.handleTaskMove(ctx, callToolRequest(map[string]any{
		"task_id":   aID,
		"position":  "before",
		"target_id": bID,
		"version":   float64(999),
	}))
	msg := getToolErrorText(test, result)
	if !strings.Contains(msg, "version conflict") {
		test.Fatalf("expected version conflict, got %q", msg)
	}
}

func TestHandleTaskMove_Cycle(test *testing.T) {
	server := testServer(test)
	ctx := context.Background()

	parentID, parentVer := createTask(test, ctx, server, map[string]any{"title": "Parent"})
	_, _ = createTask(test, ctx, server, map[string]any{"title": "Child", "parent": parentID})

	// Attempting to move parent under its own child forms a cycle.
	// We need the child's ID to pass as parent_id.
	listResult, listErr := server.handleTaskList(ctx, callToolRequest(map[string]any{}))

	if listErr != nil {
		test.Fatalf("list: %v", listErr)
	}

	items := parseToolResultArray(test, listResult)
	var childID string
	for _, item := range items {
		if item["short_id"] != parentID {
			childID = item["short_id"].(string)
			break
		}
	}
	if childID == "" {
		test.Fatal("could not locate child in list response")
	}

	result, _ := server.handleTaskMove(ctx, callToolRequest(map[string]any{
		"task_id":   parentID,
		"position":  "first",
		"parent_id": childID,
		"version":   parentVer,
	}))
	msg := getToolErrorText(test, result)
	if !strings.Contains(msg, "cycle") {
		test.Fatalf("expected cycle error, got %q", msg)
	}
}

func TestHandleTaskResequence_Root(test *testing.T) {
	server := testServer(test)
	ctx := context.Background()

	_, _ = createTask(test, ctx, server, map[string]any{"title": "A"})
	_, _ = createTask(test, ctx, server, map[string]any{"title": "B"})

	result, resequenceErr := server.handleTaskResequence(ctx, callToolRequest(map[string]any{
		"parent_id": nil,
	}))

	if resequenceErr != nil {
		test.Fatalf("handleTaskResequence: %v", resequenceErr)
	}

	parsed := parseToolResult(test, result)
	if _, ok := parsed["rewritten"]; !ok {
		test.Fatalf("expected 'rewritten' key in response, got %v", parsed)
	}
	if parsed["parent_id"] != nil {
		test.Fatalf("expected parent_id=null for root, got %v", parsed["parent_id"])
	}
}

func TestHandleTaskResequence_UnderParent(test *testing.T) {
	server := testServer(test)
	ctx := context.Background()

	parentID, _ := createTask(test, ctx, server, map[string]any{"title": "Parent"})
	_, _ = createTask(test, ctx, server, map[string]any{"title": "Child-A", "parent": parentID})

	result, resequenceErr := server.handleTaskResequence(ctx, callToolRequest(map[string]any{
		"parent_id": parentID,
	}))

	if resequenceErr != nil {
		test.Fatalf("handleTaskResequence: %v", resequenceErr)
	}

	parsed := parseToolResult(test, result)
	if parsed["parent_id"] == nil {
		test.Fatalf("expected non-null parent_id in response, got nil")
	}
}

func TestHandleTaskResequence_MissingParentID_IsError(test *testing.T) {
	server := testServer(test)
	ctx := context.Background()

	result, _ := server.handleTaskResequence(ctx, callToolRequest(map[string]any{}))
	msg := getToolErrorText(test, result)
	if !strings.Contains(msg, "parent_id") {
		test.Fatalf("expected error to mention parent_id, got %q", msg)
	}
}

func TestHandleTaskMove_BlockedField(test *testing.T) {
	server := testServer(test)
	ctx := context.Background()

	aID, _ := createTask(test, ctx, server, map[string]any{"title": "A"})
	bID, _ := createTask(test, ctx, server, map[string]any{"title": "B"})

	// Block parent_id via checkBlocked config.
	server.cfg.BlockedFields = map[string][]string{"tusk_task_move": {"parent_id"}}

	result, _ := server.handleTaskMove(ctx, callToolRequest(map[string]any{
		"task_id":   aID,
		"position":  "first",
		"parent_id": bID,
		"version":   float64(1),
	}))
	msg := getToolErrorText(test, result)
	if !strings.Contains(msg, "parent_id") {
		test.Fatalf("expected blocked-field error mentioning parent_id, got %q", msg)
	}
}

// Ensure mcp.CallToolRequest wires the params correctly for the negative
// tests above (helper used by getToolErrorText above assumes an error result).
var _ = mcp.NewToolResultError
