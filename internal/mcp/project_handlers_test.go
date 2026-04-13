package mcp

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/germanamz/tusk/config"
	"github.com/mark3labs/mcp-go/mcp"
)

func TestHandleProjectCreate_Success(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tusk.toml")
	writeMinimalConfig(t, path)
	srv := newTestServer(t, path)

	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{Arguments: map[string]any{
			"name":     "backend",
			"workflow": "kanban",
			"urgency": map[string]any{
				"due_weight":      15.0,
				"blocking_weight": 20.0,
			},
			"auto_complete": map[string]any{
				"trigger_status": "completed",
				"target_status":  "completed",
			},
		}},
	}
	res, err := srv.HandleProjectCreateForTest(context.Background(), req)
	if err != nil {
		t.Fatalf("HandleProjectCreateForTest: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Content[0].(mcp.TextContent).Text)
	}

	loaded, _ := config.LoadFile(path)
	p, ok := loaded.Projects["backend"]
	if !ok {
		t.Fatalf("backend project not persisted")
	}
	if p.Workflow != "kanban" {
		t.Fatalf("workflow: got %q", p.Workflow)
	}
	if p.Settings.Urgency == nil || p.Settings.Urgency.DueWeight == nil || *p.Settings.Urgency.DueWeight != 15.0 {
		t.Fatalf("due_weight override not persisted: %+v", p.Settings.Urgency)
	}
}

func TestHandleProjectCreate_UnknownWorkflow(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tusk.toml")
	writeMinimalConfig(t, path)
	srv := newTestServer(t, path)

	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{Arguments: map[string]any{
			"name":     "frontend",
			"workflow": "ghost",
		}},
	}
	res, _ := srv.HandleProjectCreateForTest(context.Background(), req)
	if !res.IsError {
		t.Fatalf("expected validation error for unknown workflow")
	}
}
