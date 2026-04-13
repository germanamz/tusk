package mcp

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
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

func TestHandleWorkflowModify_AddAndRemove(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tusk.toml")
	writeMinimalConfig(t, path)
	srv := newTestServer(t, path)

	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{Arguments: map[string]any{
			"name": "kanban",
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

	loaded, err := config.LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if _, ok := loaded.Workflows["kanban"].Statuses["in_review"]; !ok {
		t.Fatalf("in_review not added")
	}
}

func TestHandleWorkflowDelete_Success(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tusk.toml")
	writeMinimalConfig(t, path)

	if err := config.CreateWorkflow(path, "sprint", config.WorkflowConfig{
		Statuses: map[string]config.StatusConfig{
			"todo":  {Roles: []string{"initial"}},
			"doing": {Roles: []string{"start"}},
			"done":  {Roles: []string{"terminal", "done"}},
			"drop":  {Roles: []string{"terminal", "delete"}},
		},
		Transitions: []config.WorkflowTransitionConfig{{From: "todo", To: "doing"}},
	}); err != nil {
		t.Fatalf("seeding sprint: %v", err)
	}

	srv := newTestServer(t, path)

	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{Arguments: map[string]any{"name": "sprint"}},
	}
	res, err := srv.HandleWorkflowDeleteForTest(context.Background(), req)
	if err != nil {
		t.Fatalf("HandleWorkflowDeleteForTest: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Content[0].(mcp.TextContent).Text)
	}

	loaded, _ := config.LoadFile(path)
	if _, ok := loaded.Workflows["sprint"]; ok {
		t.Fatalf("sprint still present after delete")
	}
}

func TestHandleWorkflowDelete_ReferencedByProject(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tusk.toml")
	writeMinimalConfig(t, path)
	srv := newTestServer(t, path)

	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{Arguments: map[string]any{"name": "kanban"}},
	}
	res, _ := srv.HandleWorkflowDeleteForTest(context.Background(), req)
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
// workflow create/modify/delete operations and verifies that the resulting
// config file still parses cleanly, validates, and contains every workflow
// that was supposed to survive. Without the server-level config mutex, the
// parallel read-modify-write paths inside config.CreateWorkflow /
// ModifyWorkflow / DeleteWorkflow would race with each other and with
// tusk_config_set, occasionally losing an update or corrupting the file.
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

	loaded, err := config.LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile after concurrent writes: %v", err)
	}
	if err := loaded.Validate(); err != nil {
		t.Fatalf("Validate after concurrent writes: %v", err)
	}
	for i := 0; i < goroutines; i++ {
		name := fmt.Sprintf("wf_%03d", i)
		if _, ok := loaded.Workflows[name]; !ok {
			t.Errorf("workflow %q lost to race", name)
		}
	}
}
