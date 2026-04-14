package mcp

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/service"
	"github.com/mark3labs/mcp-go/mcp"
)

// seedBackendProject inserts a "backend" project row through the service so
// subsequent handler tests have something to modify/delete.
func seedBackendProject(t *testing.T, srv *Server) *domain.Project {
	t.Helper()
	wf, err := srv.workflowSvc.GetByName(context.Background(), "kanban")
	if err != nil {
		t.Fatalf("resolving kanban workflow: %v", err)
	}
	p, err := srv.projectSvc.Create(context.Background(), service.CreateProjectInput{
		Name:       "backend",
		WorkflowID: wf.ID,
	})
	if err != nil {
		t.Fatalf("seed backend: %v", err)
	}
	return p
}

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

	p, err := srv.projectSvc.GetByName(context.Background(), "backend")
	if err != nil {
		t.Fatalf("GetByName backend: %v", err)
	}
	if p.Settings.Urgency == nil || p.Settings.Urgency.DueWeight == nil || *p.Settings.Urgency.DueWeight != 15.0 {
		t.Fatalf("due_weight override not persisted: %+v", p.Settings.Urgency)
	}
	if p.Settings.AutoCompleteParent == nil || p.Settings.AutoCompleteParent.TriggerStatus != "completed" {
		t.Fatalf("auto_complete not persisted: %+v", p.Settings.AutoCompleteParent)
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
	srv := newTestServer(t, path)
	p := seedBackendProject(t, srv)

	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{Arguments: map[string]any{
			"name":    "backend",
			"version": float64(p.Version),
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

	got, err := srv.projectSvc.GetByName(context.Background(), "backend")
	if err != nil {
		t.Fatalf("GetByName backend: %v", err)
	}
	if got.Settings.Urgency == nil || got.Settings.Urgency.BlockingWeight == nil || *got.Settings.Urgency.BlockingWeight != 25.0 {
		t.Fatalf("blocking_weight set failed: %+v", got.Settings.Urgency)
	}
	if got.Settings.Urgency.DueWeight == nil {
		t.Fatalf("due_weight delta failed: %+v", got.Settings.Urgency)
	}
}

func TestHandleProjectModify_SetDeltaConflict(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tusk.toml")
	writeMinimalConfig(t, path)
	srv := newTestServer(t, path)
	p := seedBackendProject(t, srv)

	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{Arguments: map[string]any{
			"name":          "backend",
			"version":       float64(p.Version),
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
	srv := newTestServer(t, path)
	p := seedBackendProject(t, srv)

	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{Arguments: map[string]any{
			"name":    "backend",
			"version": float64(p.Version),
		}},
	}
	res, err := srv.HandleProjectDeleteForTest(context.Background(), req)
	if err != nil {
		t.Fatalf("HandleProjectDeleteForTest: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Content[0].(mcp.TextContent).Text)
	}
	if _, err := srv.projectSvc.GetByName(context.Background(), "backend"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("backend still present after delete: err=%v", err)
	}
}

func TestHandleProjectDelete_DefaultGuarded(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tusk.toml")
	writeMinimalConfig(t, path)
	srv := newTestServer(t, path)

	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{Arguments: map[string]any{
			"name":    "default",
			"version": float64(1),
		}},
	}
	res, _ := srv.HandleProjectDeleteForTest(context.Background(), req)
	if !res.IsError {
		t.Fatalf("expected guard error for built-in default project")
	}
}
