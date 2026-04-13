package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/germanamz/tusk/config"
	"github.com/germanamz/tusk/inmem"
	"github.com/germanamz/tusk/service"
	"github.com/mark3labs/mcp-go/mcp"
)

// minimalConfigTOML is a minimal, valid Tusk config used by config handler
// tests. It sets urgency.due_weight = 42.0 so tests can assert overrides flow
// through config.Load.
const minimalConfigTOML = `
[storage]
backend = "sqlite"
path = "./tusk.db"

[urgency]
priority_weight = 6.0
due_weight = 42.0
age_weight = 2.0
active_weight = 4.0
blocking_weight = 8.0
blocked_weight = 5.0
tags_weight = 1.0
project_weight = 1.0
annotations_weight = 1.0
waiting_weight = 3.0

[tui]
date_format = "2006-01-02"
color = true
tree_indent = 2
default_sort = "urgency"

[mcp]
disabled_tools = []
disabled_tool_groups = []
disabled_resources = []
disabled_resource_groups = []

[workflows.kanban]
[workflows.kanban.statuses.pending]
roles = ["initial"]
[workflows.kanban.statuses.active]
roles = ["start"]
[workflows.kanban.statuses.completed]
roles = ["terminal", "done"]
[workflows.kanban.statuses.deleted]
roles = ["terminal", "delete"]
[[workflows.kanban.transitions]]
from = "pending"
to = "active"
[[workflows.kanban.transitions]]
from = "active"
to = "completed"

[projects.default]
workflow = "kanban"
`

// writeMinimalConfig writes minimalConfigTOML to path.
func writeMinimalConfig(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(minimalConfigTOML), 0o644); err != nil {
		t.Fatalf("writing minimal config: %v", err)
	}
}

// newTestServer builds an *mcp.Server wired with in-memory repos and an
// explicit config file path for use in config handler tests.
func newTestServer(t *testing.T, configFile string) *Server {
	t.Helper()
	workflowRepo := inmem.NewWorkflowRepository(map[string]config.WorkflowConfig{})
	projectRepo := inmem.NewProjectRepository(map[string]config.ProjectConfig{})
	urgencyEngine := service.NewUrgencyEngine(service.UrgencyWeights{})
	srv, err := New(
		nil, nil, nil, nil, nil, nil,
		workflowRepo, projectRepo, urgencyEngine,
		"test", config.MCPConfig{},
		[]config.Option{config.WithExplicitFile(configFile)},
	)
	if err != nil {
		t.Fatalf("mcp.New: %v", err)
	}
	return srv
}

func TestHandleConfigShow(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tusk.toml")
	writeMinimalConfig(t, path)

	srv := newTestServer(t, path)

	res, err := srv.HandleConfigShowForTest(context.Background(), mcp.CallToolRequest{})
	if err != nil {
		t.Fatalf("HandleConfigShowForTest: %v", err)
	}
	if res.IsError {
		text, _ := res.Content[0].(mcp.TextContent)
		t.Fatalf("unexpected error result: %s", text.Text)
	}

	text, ok := res.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", res.Content[0])
	}

	var payload struct {
		ActiveFile string `json:"active_file"`
		Effective  struct {
			Urgency struct {
				DueWeight float64 `json:"due_weight"`
			} `json:"urgency"`
		} `json:"effective"`
	}
	if err := json.Unmarshal([]byte(text.Text), &payload); err != nil {
		t.Fatalf("parse JSON: %v", err)
	}
	if payload.ActiveFile != path {
		t.Fatalf("active_file: got %q, want %q", payload.ActiveFile, path)
	}
	if payload.Effective.Urgency.DueWeight != 42.0 {
		t.Fatalf("due_weight: got %v, want 42.0", payload.Effective.Urgency.DueWeight)
	}
}

func TestHandleConfigSet_WritesAndReloads(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tusk.toml")
	writeMinimalConfig(t, path)

	srv := newTestServer(t, path)

	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Arguments: map[string]any{
				"key":   "urgency.due_weight",
				"value": "99.5",
			},
		},
	}
	res, err := srv.HandleConfigSetForTest(context.Background(), req)
	if err != nil {
		t.Fatalf("HandleConfigSetForTest: %v", err)
	}
	if res.IsError {
		text, _ := res.Content[0].(mcp.TextContent)
		t.Fatalf("unexpected error result: %s", text.Text)
	}

	loaded, err := config.LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if loaded.Urgency.DueWeight != 99.5 {
		t.Fatalf("due_weight: got %v, want 99.5", loaded.Urgency.DueWeight)
	}
}

func TestHandleConfigSet_RejectsStorageKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tusk.toml")
	writeMinimalConfig(t, path)

	srv := newTestServer(t, path)

	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Arguments: map[string]any{
				"key":   "storage.path",
				"value": "/tmp/evil.db",
			},
		},
	}
	res, err := srv.HandleConfigSetForTest(context.Background(), req)
	if err != nil {
		t.Fatalf("HandleConfigSetForTest: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected error result for storage.* key, got success")
	}
	text, _ := res.Content[0].(mcp.TextContent)
	if !strings.Contains(text.Text, "storage.*") {
		t.Fatalf("expected storage.* guard message, got: %q", text.Text)
	}

	loaded, err := config.LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if loaded.Storage.Path == "/tmp/evil.db" {
		t.Fatalf("storage.path was mutated despite guard: %q", loaded.Storage.Path)
	}
}

func TestHandleConfigSet_RejectsUnknownKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tusk.toml")
	writeMinimalConfig(t, path)

	srv := newTestServer(t, path)

	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Arguments: map[string]any{
				"key":   "urgency.nonsense",
				"value": "1",
			},
		},
	}
	res, err := srv.HandleConfigSetForTest(context.Background(), req)
	if err != nil {
		t.Fatalf("HandleConfigSetForTest: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected error result for unknown key, got success")
	}
	text, _ := res.Content[0].(mcp.TextContent)
	if !strings.Contains(text.Text, "unknown config key") {
		t.Fatalf("expected unknown config key message, got: %q", text.Text)
	}
}
