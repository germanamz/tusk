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

func TestHandleProjectModify_SetAndDelta(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tusk.toml")
	writeMinimalConfig(t, path)
	if err := config.CreateProject(path, "backend", config.ProjectConfig{Workflow: "kanban"}); err != nil {
		t.Fatalf("seed project: %v", err)
	}

	srv := newTestServer(t, path)

	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{Arguments: map[string]any{
			"name": "backend",
			"urgency_set": map[string]any{
				"blocking_weight": 25.0,
			},
			"urgency_delta": map[string]any{
				"due_weight": 3.0,
			},
		}},
	}
	res, err := srv.HandleProjectModifyForTest(context.Background(), req)
	if err != nil {
		t.Fatalf("HandleProjectModifyForTest: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Content[0].(mcp.TextContent).Text)
	}

	loaded, _ := config.LoadFile(path)
	p := loaded.Projects["backend"]
	if p.Settings.Urgency == nil || p.Settings.Urgency.BlockingWeight == nil || *p.Settings.Urgency.BlockingWeight != 25.0 {
		t.Fatalf("blocking_weight set failed: %+v", p.Settings.Urgency)
	}
	if p.Settings.Urgency.DueWeight == nil || *p.Settings.Urgency.DueWeight == 0 {
		t.Fatalf("due_weight delta failed: %+v", p.Settings.Urgency)
	}
}

func TestHandleProjectModify_SetDeltaConflict(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tusk.toml")
	writeMinimalConfig(t, path)
	if err := config.CreateProject(path, "backend", config.ProjectConfig{Workflow: "kanban"}); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	srv := newTestServer(t, path)

	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{Arguments: map[string]any{
			"name":          "backend",
			"urgency_set":   map[string]any{"due_weight": 10.0},
			"urgency_delta": map[string]any{"due_weight": 2.0},
		}},
	}
	res, _ := srv.HandleProjectModifyForTest(context.Background(), req)
	if !res.IsError {
		t.Fatalf("expected conflict error")
	}
}

func TestHandleProjectDelete_Success(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tusk.toml")
	writeMinimalConfig(t, path)
	if err := config.CreateProject(path, "backend", config.ProjectConfig{Workflow: "kanban"}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	srv := newTestServer(t, path)

	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{Arguments: map[string]any{"name": "backend"}},
	}
	res, err := srv.HandleProjectDeleteForTest(context.Background(), req)
	if err != nil {
		t.Fatalf("HandleProjectDeleteForTest: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Content[0].(mcp.TextContent).Text)
	}
	loaded, _ := config.LoadFile(path)
	if _, ok := loaded.Projects["backend"]; ok {
		t.Fatalf("backend still present after delete")
	}
}

func TestHandleProjectDelete_DefaultGuarded(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tusk.toml")
	writeMinimalConfig(t, path)
	srv := newTestServer(t, path)

	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{Arguments: map[string]any{"name": "default"}},
	}
	res, _ := srv.HandleProjectDeleteForTest(context.Background(), req)
	if !res.IsError {
		t.Fatalf("expected guard error for built-in default project")
	}
}
