package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/germanamz/tusk/config"
	"github.com/germanamz/tusk/migrations"
	"github.com/germanamz/tusk/service"
	"github.com/germanamz/tusk/sqlite"
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

// newTestServer builds an *mcp.Server wired with sqlite-backed repos seeded
// from the TOML at configFile. Project and workflow writes go through the
// real service layer so project/workflow handler tests can exercise the full
// path without separate fixture scaffolding.
func newTestServer(t *testing.T, configFile string) *Server {
	t.Helper()
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "tusk.db")
	store, err := sqlite.New(dbPath, migrations.FS)
	if err != nil {
		t.Fatalf("opening test store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	db := store.DB()
	projectRepo := sqlite.NewProjectRepo(db)
	workflowRepo := sqlite.NewWorkflowRepo(db)
	taskRepo := sqlite.NewTaskRepo(db)

	loadedCfg, err := config.Load(config.WithExplicitFile(configFile))
	if err != nil {
		t.Fatalf("loading test config: %v", err)
	}
	if err := sqlite.SyncConfigToDB(ctx, loadedCfg, workflowRepo, projectRepo); err != nil {
		t.Fatalf("seeding test db: %v", err)
	}

	urgencyEngine := service.NewUrgencyEngine(service.UrgencyWeights{
		Priority:    loadedCfg.Urgency.PriorityWeight,
		Due:         loadedCfg.Urgency.DueWeight,
		Age:         loadedCfg.Urgency.AgeWeight,
		Active:      loadedCfg.Urgency.ActiveWeight,
		Blocking:    loadedCfg.Urgency.BlockingWeight,
		Blocked:     loadedCfg.Urgency.BlockedWeight,
		Tags:        loadedCfg.Urgency.TagsWeight,
		Project:     loadedCfg.Urgency.ProjectWeight,
		Annotations: loadedCfg.Urgency.AnnotationsWeight,
		Waiting:     loadedCfg.Urgency.WaitingWeight,
	})

	reloadHook := func(ctx context.Context, cfg *config.Config) error {
		return sqlite.SyncConfigToDB(ctx, cfg, workflowRepo, projectRepo)
	}

	workflowSvc := service.NewWorkflowService(workflowRepo, projectRepo)
	projectSvc := service.NewProjectService(projectRepo, taskRepo, store, service.ProjectDefaults{Urgency: service.UrgencyWeights{
		Priority:    loadedCfg.Urgency.PriorityWeight,
		Due:         loadedCfg.Urgency.DueWeight,
		Age:         loadedCfg.Urgency.AgeWeight,
		Active:      loadedCfg.Urgency.ActiveWeight,
		Blocking:    loadedCfg.Urgency.BlockingWeight,
		Blocked:     loadedCfg.Urgency.BlockedWeight,
		Tags:        loadedCfg.Urgency.TagsWeight,
		Project:     loadedCfg.Urgency.ProjectWeight,
		Annotations: loadedCfg.Urgency.AnnotationsWeight,
		Waiting:     loadedCfg.Urgency.WaitingWeight,
	}})

	srv, err := New(
		nil, nil, nil, projectSvc, workflowSvc, nil,
		workflowRepo, projectRepo, urgencyEngine, reloadHook,
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

// TestHandleConfigSet_ConcurrentWritesAreSerialized launches many concurrent
// tusk_config_set calls across two keys and verifies the resulting file still
// parses cleanly and validates. Without the server-level config mutex, the
// parallel read-modify-write paths would race and occasionally produce a
// corrupt file or lose an update in a way that fails Validate().
func TestHandleConfigSet_ConcurrentWritesAreSerialized(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tusk.toml")
	writeMinimalConfig(t, path)

	srv := newTestServer(t, path)

	const goroutines = 50
	keys := []string{"urgency.due_weight", "urgency.age_weight"}

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(i int) {
			defer wg.Done()
			key := keys[i%len(keys)]
			value := fmt.Sprintf("%d.0", 10+i)
			req := mcp.CallToolRequest{
				Params: mcp.CallToolParams{
					Arguments: map[string]any{
						"key":   key,
						"value": value,
					},
				},
			}
			res, err := srv.HandleConfigSetForTest(context.Background(), req)
			if err != nil {
				t.Errorf("HandleConfigSetForTest(%s=%s): %v", key, value, err)
				return
			}
			if res.IsError {
				text, _ := res.Content[0].(mcp.TextContent)
				t.Errorf("unexpected error result for %s=%s: %s", key, value, text.Text)
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
