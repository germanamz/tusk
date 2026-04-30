package mcp

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"github.com/germanamz/tusk/domain"
	"github.com/mark3labs/mcp-go/mcp"
)

func TestHandleWorkflowCreate_Success(test *testing.T) {
	dir := test.TempDir()
	path := filepath.Join(dir, "tusk.toml")
	writeMinimalConfig(test, path)
	srv := newTestServer(test, path)

	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{Arguments: map[string]any{
			"name": "sprint",
			"statuses": []any{
				map[string]any{"name": "todo", "roles": []any{"initial"}},
				map[string]any{"name": "doing", "roles": []any{"start", "highlight"}},
				map[string]any{"name": "done", "roles": []any{"terminal", "done"}},
				map[string]any{"name": "dropped", "roles": []any{"terminal", "delete", "dim"}},
			},
			"transitions": []any{
				map[string]any{"from": "todo", "to": "doing"},
				map[string]any{"from": "doing", "to": "done"},
				map[string]any{"from": "doing", "to": "dropped"},
			},
		}},
	}

	result, createErr := srv.HandleWorkflowCreateForTest(context.Background(), req)

	if createErr != nil {
		test.Fatalf("HandleWorkflowCreateForTest: %v", createErr)
	}

	if result.IsError {
		text := result.Content[0].(mcp.TextContent).Text
		test.Fatalf("unexpected error: %s", text)
	}

	workflow, getErr := srv.WorkflowRepoForTest().GetByName(context.Background(), "sprint")

	if getErr != nil {
		test.Fatalf("GetByName sprint: %v", getErr)
	}

	if len(workflow.Statuses) != 4 || len(workflow.Transitions) != 3 {
		test.Fatalf("unexpected shape: %+v", workflow)
	}
	if workflow.Version != 1 {
		test.Fatalf("expected version 1, got %d", workflow.Version)
	}
}

func TestHandleWorkflowModify_AddAndRemove(test *testing.T) {
	dir := test.TempDir()
	path := filepath.Join(dir, "tusk.toml")
	writeMinimalConfig(test, path)
	srv := newTestServer(test, path)

	workflow, getErr := srv.WorkflowRepoForTest().GetByName(context.Background(), "kanban")

	if getErr != nil {
		test.Fatalf("GetByName kanban: %v", getErr)
	}

	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{Arguments: map[string]any{
			"name":    "kanban",
			"version": float64(workflow.Version),
			"add_statuses": []any{
				map[string]any{"name": "in_review", "roles": []any{}},
			},
			"add_transitions": []any{
				map[string]any{"from": "active", "to": "in_review"},
				map[string]any{"from": "in_review", "to": "completed"},
			},
		}},
	}

	result, modifyErr := srv.HandleWorkflowModifyForTest(context.Background(), req)

	if modifyErr != nil {
		test.Fatalf("HandleWorkflowModifyForTest: %v", modifyErr)
	}

	if result.IsError {
		test.Fatalf("unexpected error: %s", result.Content[0].(mcp.TextContent).Text)
	}

	updated, updatedErr := srv.WorkflowRepoForTest().GetByName(context.Background(), "kanban")

	if updatedErr != nil {
		test.Fatalf("GetByName kanban after modify: %v", updatedErr)
	}

	if _, ok := updated.Statuses["in_review"]; !ok {
		test.Fatalf("in_review not added: %+v", updated.Statuses)
	}
	if updated.Version != workflow.Version+1 {
		test.Fatalf("expected bumped version, got %d", updated.Version)
	}
}

func TestHandleWorkflowDelete_Success(test *testing.T) {
	dir := test.TempDir()
	path := filepath.Join(dir, "tusk.toml")
	writeMinimalConfig(test, path)
	srv := newTestServer(test, path)
	ctx := context.Background()

	createReq := mcp.CallToolRequest{
		Params: mcp.CallToolParams{Arguments: map[string]any{
			"name": "sprint",
			"statuses": []any{
				map[string]any{"name": "todo", "roles": []any{"initial"}},
				map[string]any{"name": "doing", "roles": []any{"start"}},
				map[string]any{"name": "done", "roles": []any{"terminal", "done"}},
				map[string]any{"name": "drop", "roles": []any{"terminal", "delete"}},
			},
			"transitions": []any{
				map[string]any{"from": "todo", "to": "doing"},
				map[string]any{"from": "doing", "to": "done"},
				map[string]any{"from": "doing", "to": "drop"},
			},
		}},
	}

	if result, err := srv.HandleWorkflowCreateForTest(ctx, createReq); err != nil || result.IsError {
		test.Fatalf("seed create failed: %v / %+v", err, result)
	}

	workflow, getErr := srv.WorkflowRepoForTest().GetByName(ctx, "sprint")

	if getErr != nil {
		test.Fatalf("GetByName sprint: %v", getErr)
	}

	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{Arguments: map[string]any{
			"name":    "sprint",
			"version": float64(workflow.Version),
		}},
	}

	result, deleteErr := srv.HandleWorkflowDeleteForTest(ctx, req)

	if deleteErr != nil {
		test.Fatalf("HandleWorkflowDeleteForTest: %v", deleteErr)
	}

	if result.IsError {
		test.Fatalf("unexpected error: %s", result.Content[0].(mcp.TextContent).Text)
	}

	if _, err := srv.WorkflowRepoForTest().GetByName(ctx, "sprint"); !errors.Is(err, domain.ErrNotFound) {
		test.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestHandleWorkflowDelete_ReferencedByProject(test *testing.T) {
	dir := test.TempDir()
	path := filepath.Join(dir, "tusk.toml")
	writeMinimalConfig(test, path)
	srv := newTestServer(test, path)
	ctx := context.Background()

	workflow, getErr := srv.WorkflowRepoForTest().GetByName(ctx, "kanban")

	if getErr != nil {
		test.Fatalf("GetByName kanban: %v", getErr)
	}

	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{Arguments: map[string]any{
			"name":    "kanban",
			"version": float64(workflow.Version),
		}},
	}
	result, _ := srv.HandleWorkflowDeleteForTest(ctx, req)
	if !result.IsError {
		test.Fatalf("expected error deleting referenced workflow")
	}
}

func TestHandleWorkflowCreate_ValidationError(test *testing.T) {
	dir := test.TempDir()
	path := filepath.Join(dir, "tusk.toml")
	writeMinimalConfig(test, path)
	srv := newTestServer(test, path)

	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{Arguments: map[string]any{
			"name": "broken",
			"statuses": []any{
				map[string]any{"name": "doing", "roles": []any{"start"}},
				map[string]any{"name": "done", "roles": []any{"terminal", "done"}},
				map[string]any{"name": "dropped", "roles": []any{"terminal", "delete"}},
			},
			"transitions": []any{
				map[string]any{"from": "doing", "to": "done"},
			},
		}},
	}
	result, _ := srv.HandleWorkflowCreateForTest(context.Background(), req)
	if !result.IsError {
		test.Fatalf("expected validation error")
	}
}

// TestHandleWorkflow_ConcurrentMutationsAreSerialized launches many concurrent
// workflow create operations and verifies that every workflow lands in the
// repository. The server-level mutex serializes the read-modify-write paths
// inside the service so creates do not race with each other.
func TestHandleWorkflow_ConcurrentMutationsAreSerialized(test *testing.T) {
	dir := test.TempDir()
	path := filepath.Join(dir, "tusk.toml")
	writeMinimalConfig(test, path)

	srv := newTestServer(test, path)

	const goroutines = 40

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for idx := 0; idx < goroutines; idx++ {
		go func(idx int) {
			defer wg.Done()
			name := fmt.Sprintf("wf_%03d", idx)
			req := mcp.CallToolRequest{
				Params: mcp.CallToolParams{Arguments: map[string]any{
					"name": name,
					"statuses": []any{
						map[string]any{"name": "todo", "roles": []any{"initial"}},
						map[string]any{"name": "doing", "roles": []any{"start"}},
						map[string]any{"name": "done", "roles": []any{"terminal", "done"}},
						map[string]any{"name": "dropped", "roles": []any{"terminal", "delete"}},
					},
					"transitions": []any{
						map[string]any{"from": "todo", "to": "doing"},
						map[string]any{"from": "doing", "to": "done"},
					},
				}},
			}

			result, err := srv.HandleWorkflowCreateForTest(context.Background(), req)

			if err != nil {
				test.Errorf("HandleWorkflowCreateForTest(%s): %v", name, err)
				return
			}

			if result.IsError {
				text, _ := result.Content[0].(mcp.TextContent)
				test.Errorf("unexpected error creating %s: %s", name, text.Text)
			}
		}(idx)
	}
	wg.Wait()

	for idx := 0; idx < goroutines; idx++ {
		name := fmt.Sprintf("wf_%03d", idx)
		if _, err := srv.WorkflowRepoForTest().GetByName(context.Background(), name); err != nil {
			test.Errorf("workflow %q lost to race: %v", name, err)
		}
	}
}
