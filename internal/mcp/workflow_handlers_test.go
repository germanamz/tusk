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

func TestHandleWorkflowCreate_Success(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tusk.toml")
	writeMinimalConfig(t, path)
	srv := newTestServer(t, path)

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
	res, err := srv.HandleWorkflowCreateForTest(context.Background(), req)
	if err != nil {
		t.Fatalf("HandleWorkflowCreateForTest: %v", err)
	}
	if res.IsError {
		text := res.Content[0].(mcp.TextContent).Text
		t.Fatalf("unexpected error: %s", text)
	}

	wf, err := srv.WorkflowRepoForTest().GetByName(context.Background(), "sprint")
	if err != nil {
		t.Fatalf("GetByName sprint: %v", err)
	}
	if len(wf.Statuses) != 4 || len(wf.Transitions) != 3 {
		t.Fatalf("unexpected shape: %+v", wf)
	}
	if wf.Version != 1 {
		t.Fatalf("expected version 1, got %d", wf.Version)
	}
}

func TestHandleWorkflowModify_AddAndRemove(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tusk.toml")
	writeMinimalConfig(t, path)
	srv := newTestServer(t, path)

	wf, err := srv.WorkflowRepoForTest().GetByName(context.Background(), "kanban")
	if err != nil {
		t.Fatalf("GetByName kanban: %v", err)
	}

	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{Arguments: map[string]any{
			"name":    "kanban",
			"version": float64(wf.Version),
			"add_statuses": []any{
				map[string]any{"name": "in_review", "roles": []any{}},
			},
			"add_transitions": []any{
				map[string]any{"from": "active", "to": "in_review"},
				map[string]any{"from": "in_review", "to": "completed"},
			},
		}},
	}
	res, err := srv.HandleWorkflowModifyForTest(context.Background(), req)
	if err != nil {
		t.Fatalf("HandleWorkflowModifyForTest: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Content[0].(mcp.TextContent).Text)
	}

	updated, err := srv.WorkflowRepoForTest().GetByName(context.Background(), "kanban")
	if err != nil {
		t.Fatalf("GetByName kanban after modify: %v", err)
	}
	if _, ok := updated.Statuses["in_review"]; !ok {
		t.Fatalf("in_review not added: %+v", updated.Statuses)
	}
	if updated.Version != wf.Version+1 {
		t.Fatalf("expected bumped version, got %d", updated.Version)
	}
}

func TestHandleWorkflowDelete_Success(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tusk.toml")
	writeMinimalConfig(t, path)
	srv := newTestServer(t, path)
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
	if res, err := srv.HandleWorkflowCreateForTest(ctx, createReq); err != nil || res.IsError {
		t.Fatalf("seed create failed: %v / %+v", err, res)
	}

	wf, err := srv.WorkflowRepoForTest().GetByName(ctx, "sprint")
	if err != nil {
		t.Fatalf("GetByName sprint: %v", err)
	}

	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{Arguments: map[string]any{
			"name":    "sprint",
			"version": float64(wf.Version),
		}},
	}
	res, err := srv.HandleWorkflowDeleteForTest(ctx, req)
	if err != nil {
		t.Fatalf("HandleWorkflowDeleteForTest: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Content[0].(mcp.TextContent).Text)
	}

	if _, err := srv.WorkflowRepoForTest().GetByName(ctx, "sprint"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestHandleWorkflowDelete_ReferencedByProject(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tusk.toml")
	writeMinimalConfig(t, path)
	srv := newTestServer(t, path)
	ctx := context.Background()

	wf, err := srv.WorkflowRepoForTest().GetByName(ctx, "kanban")
	if err != nil {
		t.Fatalf("GetByName kanban: %v", err)
	}

	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{Arguments: map[string]any{
			"name":    "kanban",
			"version": float64(wf.Version),
		}},
	}
	res, _ := srv.HandleWorkflowDeleteForTest(ctx, req)
	if !res.IsError {
		t.Fatalf("expected error deleting referenced workflow")
	}
}

func TestHandleWorkflowCreate_ValidationError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tusk.toml")
	writeMinimalConfig(t, path)
	srv := newTestServer(t, path)

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
	res, _ := srv.HandleWorkflowCreateForTest(context.Background(), req)
	if !res.IsError {
		t.Fatalf("expected validation error")
	}
}

// TestHandleWorkflow_ConcurrentMutationsAreSerialized launches many concurrent
// workflow create operations and verifies that every workflow lands in the
// repository. The server-level mutex serializes the read-modify-write paths
// inside the service so creates do not race with each other.
func TestHandleWorkflow_ConcurrentMutationsAreSerialized(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tusk.toml")
	writeMinimalConfig(t, path)

	srv := newTestServer(t, path)

	const goroutines = 40

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(i int) {
			defer wg.Done()
			name := fmt.Sprintf("wf_%03d", i)
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
			res, err := srv.HandleWorkflowCreateForTest(context.Background(), req)
			if err != nil {
				t.Errorf("HandleWorkflowCreateForTest(%s): %v", name, err)
				return
			}
			if res.IsError {
				text, _ := res.Content[0].(mcp.TextContent)
				t.Errorf("unexpected error creating %s: %s", name, text.Text)
			}
		}(i)
	}
	wg.Wait()

	for i := 0; i < goroutines; i++ {
		name := fmt.Sprintf("wf_%03d", i)
		if _, err := srv.WorkflowRepoForTest().GetByName(context.Background(), name); err != nil {
			t.Errorf("workflow %q lost to race: %v", name, err)
		}
	}
}
