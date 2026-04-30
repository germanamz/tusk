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
`

// writeMinimalConfig writes minimalConfigTOML to path.
func writeMinimalConfig(test *testing.T, path string) {
	test.Helper()

	writeErr := os.WriteFile(path, []byte(minimalConfigTOML), 0o644)

	if writeErr != nil {
		test.Fatalf("writing minimal config: %v", writeErr)
	}
}

// newTestServer builds an *mcp.Server wired with sqlite-backed repos seeded
// from the TOML at configFile. Project and workflow writes go through the
// real service layer so project/workflow handler tests can exercise the full
// path without separate fixture scaffolding.
func newTestServer(test *testing.T, configFile string) *Server {
	test.Helper()

	dbPath := filepath.Join(test.TempDir(), "tusk.db")

	store, storeErr := sqlite.New(dbPath, migrations.FS)

	if storeErr != nil {
		test.Fatalf("opening test store: %v", storeErr)
	}

	test.Cleanup(func() { _ = store.Close() })

	db := store.DB()
	projectRepo := sqlite.NewProjectRepo(db)
	workflowRepo := sqlite.NewWorkflowRepo(db)
	taskRepo := sqlite.NewTaskRepo(db)

	loadedCfg, loadErr := config.Load(config.WithExplicitFile(configFile))

	if loadErr != nil {
		test.Fatalf("loading test config: %v", loadErr)
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
	}}, loadedCfg)

	newServer, newErr := New(
		nil, nil, nil, projectSvc, workflowSvc, nil, nil,
		workflowRepo, projectRepo, urgencyEngine,
		"test", config.MCPConfig{},
		[]config.Option{config.WithExplicitFile(configFile)},
	)

	if newErr != nil {
		test.Fatalf("mcp.New: %v", newErr)
	}

	return newServer
}

func TestHandleConfigShow(test *testing.T) {
	dir := test.TempDir()
	path := filepath.Join(dir, "tusk.toml")
	writeMinimalConfig(test, path)

	server := newTestServer(test, path)

	res, showErr := server.HandleConfigShowForTest(context.Background(), mcp.CallToolRequest{})

	if showErr != nil {
		test.Fatalf("HandleConfigShowForTest: %v", showErr)
	}

	if res.IsError {
		text, _ := res.Content[0].(mcp.TextContent)
		test.Fatalf("unexpected error result: %s", text.Text)
	}

	text, ok := res.Content[0].(mcp.TextContent)
	if !ok {
		test.Fatalf("expected TextContent, got %T", res.Content[0])
	}

	var payload struct {
		ActiveFile string `json:"active_file"`
		Effective  struct {
			Urgency struct {
				DueWeight float64 `json:"due_weight"`
			} `json:"urgency"`
		} `json:"effective"`
	}

	unmarshalErr := json.Unmarshal([]byte(text.Text), &payload)

	if unmarshalErr != nil {
		test.Fatalf("parse JSON: %v", unmarshalErr)
	}

	if payload.ActiveFile != path {
		test.Fatalf("active_file: got %q, want %q", payload.ActiveFile, path)
	}
	if payload.Effective.Urgency.DueWeight != 42.0 {
		test.Fatalf("due_weight: got %v, want 42.0", payload.Effective.Urgency.DueWeight)
	}
}

func TestHandleConfigSet_WritesAndReloads(test *testing.T) {
	dir := test.TempDir()
	path := filepath.Join(dir, "tusk.toml")
	writeMinimalConfig(test, path)

	server := newTestServer(test, path)

	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Arguments: map[string]any{
				"key":   "urgency.due_weight",
				"value": "99.5",
			},
		},
	}

	res, setErr := server.HandleConfigSetForTest(context.Background(), req)

	if setErr != nil {
		test.Fatalf("HandleConfigSetForTest: %v", setErr)
	}

	if res.IsError {
		text, _ := res.Content[0].(mcp.TextContent)
		test.Fatalf("unexpected error result: %s", text.Text)
	}

	loaded, loadErr := config.LoadFile(path)

	if loadErr != nil {
		test.Fatalf("LoadFile: %v", loadErr)
	}

	if loaded.Urgency.DueWeight != 99.5 {
		test.Fatalf("due_weight: got %v, want 99.5", loaded.Urgency.DueWeight)
	}
}

func TestHandleConfigSet_RejectsStorageKeys(test *testing.T) {
	dir := test.TempDir()
	path := filepath.Join(dir, "tusk.toml")
	writeMinimalConfig(test, path)

	server := newTestServer(test, path)

	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Arguments: map[string]any{
				"key":   "storage.path",
				"value": "/tmp/evil.db",
			},
		},
	}

	res, setErr := server.HandleConfigSetForTest(context.Background(), req)

	if setErr != nil {
		test.Fatalf("HandleConfigSetForTest: %v", setErr)
	}

	if !res.IsError {
		test.Fatalf("expected error result for storage.* key, got success")
	}

	text, _ := res.Content[0].(mcp.TextContent)
	if !strings.Contains(text.Text, "storage.*") {
		test.Fatalf("expected storage.* guard message, got: %q", text.Text)
	}

	loaded, loadErr := config.LoadFile(path)

	if loadErr != nil {
		test.Fatalf("LoadFile: %v", loadErr)
	}

	if loaded.Storage.Path == "/tmp/evil.db" {
		test.Fatalf("storage.path was mutated despite guard: %q", loaded.Storage.Path)
	}
}

func TestHandleConfigSet_RejectsProjectsKeys(test *testing.T) {
	dir := test.TempDir()
	path := filepath.Join(dir, "tusk.toml")
	writeMinimalConfig(test, path)

	server := newTestServer(test, path)

	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Arguments: map[string]any{
				"key":   "projects.foo.workflow",
				"value": "kanban",
			},
		},
	}

	res, setErr := server.HandleConfigSetForTest(context.Background(), req)

	if setErr != nil {
		test.Fatalf("HandleConfigSetForTest: %v", setErr)
	}

	if !res.IsError {
		test.Fatalf("expected error result for projects.* key, got success")
	}

	text, _ := res.Content[0].(mcp.TextContent)
	const want = "projects.* is managed by the database — use `tusk project modify` instead"
	if text.Text != want {
		test.Fatalf("unexpected error text:\n got: %q\nwant: %q", text.Text, want)
	}
}

func TestHandleConfigSet_RejectsWorkflowsKeys(test *testing.T) {
	dir := test.TempDir()
	path := filepath.Join(dir, "tusk.toml")
	writeMinimalConfig(test, path)

	server := newTestServer(test, path)

	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Arguments: map[string]any{
				"key":   "workflows.kanban.statuses.pending.roles",
				"value": "initial",
			},
		},
	}

	res, setErr := server.HandleConfigSetForTest(context.Background(), req)

	if setErr != nil {
		test.Fatalf("HandleConfigSetForTest: %v", setErr)
	}

	if !res.IsError {
		test.Fatalf("expected error result for workflows.* key, got success")
	}

	text, _ := res.Content[0].(mcp.TextContent)
	const want = "workflows.* is managed by the database — use `tusk workflow modify` instead"
	if text.Text != want {
		test.Fatalf("unexpected error text:\n got: %q\nwant: %q", text.Text, want)
	}
}

// TestHandleConfigSet_ConcurrentWritesAreSerialized launches many concurrent
// tusk_config_set calls across two keys and verifies the resulting file still
// parses cleanly and validates. Without the server-level config mutex, the
// parallel read-modify-write paths would race and occasionally produce a
// corrupt file or lose an update in a way that fails Validate().
func TestHandleConfigSet_ConcurrentWritesAreSerialized(test *testing.T) {
	dir := test.TempDir()
	path := filepath.Join(dir, "tusk.toml")
	writeMinimalConfig(test, path)

	server := newTestServer(test, path)

	const goroutines = 50
	keys := []string{"urgency.due_weight", "urgency.age_weight"}

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for goroutineIdx := 0; goroutineIdx < goroutines; goroutineIdx++ {
		go func(goroutineIdx int) {
			defer wg.Done()
			key := keys[goroutineIdx%len(keys)]
			value := fmt.Sprintf("%d.0", 10+goroutineIdx)
			req := mcp.CallToolRequest{
				Params: mcp.CallToolParams{
					Arguments: map[string]any{
						"key":   key,
						"value": value,
					},
				},
			}

			res, setErr := server.HandleConfigSetForTest(context.Background(), req)

			if setErr != nil {
				test.Errorf("HandleConfigSetForTest(%s=%s): %v", key, value, setErr)
				return
			}

			if res.IsError {
				text, _ := res.Content[0].(mcp.TextContent)
				test.Errorf("unexpected error result for %s=%s: %s", key, value, text.Text)
			}
		}(goroutineIdx)
	}
	wg.Wait()

	loaded, loadErr := config.LoadFile(path)

	if loadErr != nil {
		test.Fatalf("LoadFile after concurrent writes: %v", loadErr)
	}

	validateErr := loaded.Validate()

	if validateErr != nil {
		test.Fatalf("Validate after concurrent writes: %v", validateErr)
	}
}

func TestHandleConfigSet_RejectsUnknownKey(test *testing.T) {
	dir := test.TempDir()
	path := filepath.Join(dir, "tusk.toml")
	writeMinimalConfig(test, path)

	server := newTestServer(test, path)

	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Arguments: map[string]any{
				"key":   "urgency.nonsense",
				"value": "1",
			},
		},
	}

	res, setErr := server.HandleConfigSetForTest(context.Background(), req)

	if setErr != nil {
		test.Fatalf("HandleConfigSetForTest: %v", setErr)
	}

	if !res.IsError {
		test.Fatalf("expected error result for unknown key, got success")
	}

	text, _ := res.Content[0].(mcp.TextContent)
	if !strings.Contains(text.Text, "unknown config key") {
		test.Fatalf("expected unknown config key message, got: %q", text.Text)
	}
}
