package mcp

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/germanamz/tusk/config"
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

	loaded, err := config.LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	wf, ok := loaded.Workflows["sprint"]
	if !ok {
		t.Fatalf("workflow sprint not persisted")
	}
	if len(wf.Statuses) != 4 || len(wf.Transitions) != 3 {
		t.Fatalf("unexpected shape: %+v", wf)
	}

	wfs, err := srv.WorkflowRepoForTest().List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	var found bool
	for _, w := range wfs {
		if w.Name == "sprint" {
			found = true
		}
	}
	if !found {
		t.Fatalf("workflow sprint not reloaded into repo: %+v", wfs)
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
