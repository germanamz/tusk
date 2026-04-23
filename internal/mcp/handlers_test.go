package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/germanamz/tusk/config"
	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/repository"
	"github.com/germanamz/tusk/service"
	"github.com/germanamz/tusk/sqlite"
	"github.com/germanamz/tusk/sqlite/sqlitetest"
	"github.com/google/uuid"
	"github.com/mark3labs/mcp-go/mcp"
)

// storeWriteTx adapts a *sqlite.Store to service.WriteTxProvider for MCP
// handler tests.
type storeWriteTx struct{ store *sqlite.Store }

type storeWriteTxAdapter struct{ tx *sqlite.Tx }

func (w *storeWriteTxAdapter) Tasks() repository.TaskRepository         { return w.tx.Tasks() }
func (w *storeWriteTxAdapter) Relations() repository.RelationRepository { return w.tx.Relations() }
func (w *storeWriteTxAdapter) Events() repository.EventRepository       { return w.tx.Events(10000, 1000) }

func (p *storeWriteTx) WithTx(ctx context.Context, fn func(tx service.WriteTx) error) error {
	return p.store.WithTx(ctx, func(stx *sqlite.Tx) error {
		return fn(&storeWriteTxAdapter{tx: stx})
	})
}

// testServer creates a fully wired Server with an in-memory SQLite DB.
func testServer(t *testing.T) *Server {
	t.Helper()
	store, projectRepo, workflowRepo := sqlitetest.NewStore(t)

	db := store.DB()
	bundle := &service.RepoBundle{
		Store:       store,
		WriteTx:     &storeWriteTx{store: store},
		Tasks:       sqlite.NewTaskRepo(db),
		Annotations: sqlite.NewAnnotationRepo(db),
		Notes:       sqlite.NewNoteRepo(db),
		Relations:   sqlite.NewRelationRepo(db),
		Tags:        sqlite.NewTagRepo(db),
		Players:     sqlite.NewPlayerRepo(db),
	}
	resolver := func(context.Context, uuid.UUID) (*service.RepoBundle, error) { return bundle, nil }
	projects := func(context.Context) ([]uuid.UUID, error) { return []uuid.UUID{domain.DefaultProjectUUID}, nil }

	workflowSvc := service.NewWorkflowService(workflowRepo, projectRepo)
	taskSvc := service.NewTaskService(resolver, projects, projectRepo, nil, workflowSvc, nil)
	tagSvc := service.NewTagService(resolver)
	relationSvc := service.NewRelationService(resolver, projects)
	projectSvc := service.NewProjectService(projectRepo, bundle.Tasks, bundle.Store, service.ProjectDefaults{}, nil)
	playerSvc := service.NewPlayerService(bundle.Players)
	noteSvc := service.NewNoteService(bundle.Notes, bundle.Players, projectRepo, bundle.Tasks, 0)

	s, err := New(
		taskSvc, tagSvc, relationSvc, projectSvc, workflowSvc, playerSvc, noteSvc,
		nil, nil, nil,
		"test", config.MCPConfig{}, nil,
	)
	if err != nil {
		t.Fatalf("creating MCP server: %v", err)
	}
	return s
}

// callToolRequest builds a CallToolRequest with the given arguments.
func callToolRequest(args map[string]any) mcp.CallToolRequest {
	return mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Arguments: args,
		},
	}
}

// parseToolResult extracts the JSON content from a tool result into a map.
func parseToolResult(t *testing.T, result *mcp.CallToolResult) map[string]any {
	t.Helper()
	if result.IsError {
		t.Fatalf("unexpected tool error: %v", result.Content)
	}
	text, ok := result.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", result.Content[0])
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(text.Text), &parsed); err != nil {
		t.Fatalf("parsing tool result JSON: %v", err)
	}
	return parsed
}

// parseToolResultArray extracts the JSON array content from a tool result.
func parseToolResultArray(t *testing.T, result *mcp.CallToolResult) []map[string]any {
	t.Helper()
	text, ok := result.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", result.Content[0])
	}
	var parsed []map[string]any
	if err := json.Unmarshal([]byte(text.Text), &parsed); err != nil {
		t.Fatalf("parsing tool result JSON array: %v", err)
	}
	return parsed
}

// getToolErrorText extracts the error message from an isError tool result.
func getToolErrorText(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	if !result.IsError {
		t.Fatal("expected tool error, got success")
	}
	text, ok := result.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", result.Content[0])
	}
	return text.Text
}

func TestHandleTaskCreate(t *testing.T) {
	s := testServer(t)
	ctx := context.Background()

	t.Run("basic create", func(t *testing.T) {
		result, err := s.handleTaskCreate(ctx, callToolRequest(map[string]any{
			"title":    "Test task",
			"priority": float64(3),
		}))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		parsed := parseToolResult(t, result)
		if parsed["title"] != "Test task" {
			t.Fatalf("expected title 'Test task', got %v", parsed["title"])
		}
		if parsed["priority"].(float64) != 3 {
			t.Fatalf("expected priority 3, got %v", parsed["priority"])
		}
		if parsed["status"] != "pending" {
			t.Fatalf("expected status 'pending', got %v", parsed["status"])
		}
		if parsed["version"].(float64) != 1 {
			t.Fatalf("expected version 1, got %v", parsed["version"])
		}
	})

	t.Run("create with tags", func(t *testing.T) {
		result, err := s.handleTaskCreate(ctx, callToolRequest(map[string]any{
			"title": "Tagged task",
			"tags":  []any{"alpha", "beta"},
		}))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		parsed := parseToolResult(t, result)
		tags, _ := parsed["tags"].([]any)
		if len(tags) != 2 {
			t.Fatalf("expected 2 tags, got %d", len(tags))
		}
	})

	t.Run("missing title returns tool error", func(t *testing.T) {
		result, err := s.handleTaskCreate(ctx, callToolRequest(map[string]any{}))
		if err != nil {
			t.Fatalf("unexpected transport error: %v", err)
		}
		msg := getToolErrorText(t, result)
		if msg != "title is required" {
			t.Fatalf("expected 'title is required', got %q", msg)
		}
	})
}

func TestHandleTaskGet(t *testing.T) {
	s := testServer(t)
	ctx := context.Background()

	// Create a task first
	createResult, _ := s.handleTaskCreate(ctx, callToolRequest(map[string]any{
		"title": "Get me",
		"tags":  []any{"fetched"},
	}))
	created := parseToolResult(t, createResult)
	shortID := created["short_id"].(string)

	// Add annotation
	s.handleTaskAnnotate(ctx, callToolRequest(map[string]any{
		"short_id": shortID,
		"body":     "test annotation",
	}))

	t.Run("returns full details", func(t *testing.T) {
		result, err := s.handleTaskGet(ctx, callToolRequest(map[string]any{
			"short_id": shortID,
		}))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		parsed := parseToolResult(t, result)
		if parsed["title"] != "Get me" {
			t.Fatalf("expected title 'Get me', got %v", parsed["title"])
		}
		tags, _ := parsed["tags"].([]any)
		if len(tags) != 1 || tags[0] != "fetched" {
			t.Fatalf("expected tags [fetched], got %v", tags)
		}
		annotations, _ := parsed["annotations"].([]any)
		if len(annotations) != 1 {
			t.Fatalf("expected 1 annotation, got %d", len(annotations))
		}
	})

	t.Run("not found returns tool error", func(t *testing.T) {
		result, _ := s.handleTaskGet(ctx, callToolRequest(map[string]any{
			"short_id": "nonexistent",
		}))
		msg := getToolErrorText(t, result)
		if msg != "not found: task nonexistent" {
			t.Fatalf("expected not found error, got %q", msg)
		}
	})
}

func TestHandleTaskList(t *testing.T) {
	s := testServer(t)
	ctx := context.Background()

	s.handleTaskCreate(ctx, callToolRequest(map[string]any{"title": "Task A"}))
	s.handleTaskCreate(ctx, callToolRequest(map[string]any{"title": "Task B", "priority": float64(4)}))

	t.Run("list all", func(t *testing.T) {
		result, err := s.handleTaskList(ctx, callToolRequest(map[string]any{}))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		items := parseToolResultArray(t, result)
		if len(items) != 2 {
			t.Fatalf("expected 2 tasks, got %d", len(items))
		}
	})

	t.Run("filter by priority", func(t *testing.T) {
		result, err := s.handleTaskList(ctx, callToolRequest(map[string]any{
			"priority_min": float64(4),
		}))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		items := parseToolResultArray(t, result)
		if len(items) != 1 {
			t.Fatalf("expected 1 task with priority >= 4, got %d", len(items))
		}
		if items[0]["title"] != "Task B" {
			t.Fatalf("expected 'Task B', got %v", items[0]["title"])
		}
	})
}

func TestHandleTaskTransition_IncludesTags(t *testing.T) {
	s := testServer(t)
	ctx := context.Background()

	createResult, _ := s.handleTaskCreate(ctx, callToolRequest(map[string]any{
		"title": "Transition tags",
		"tags":  []any{"keep-me"},
	}))
	created := parseToolResult(t, createResult)
	shortID := created["short_id"].(string)
	version := created["version"].(float64)

	// Start
	startResult, err := s.handleTaskStart(ctx, callToolRequest(map[string]any{
		"short_id": shortID,
		"version":  version,
	}))
	if err != nil {
		t.Fatalf("start error: %v", err)
	}
	started := parseToolResult(t, startResult)
	tags, _ := started["tags"].([]any)
	if len(tags) != 1 || tags[0] != "keep-me" {
		t.Fatalf("start: expected tags [keep-me], got %v", tags)
	}

	version = started["version"].(float64)

	// Done
	doneResult, err := s.handleTaskDone(ctx, callToolRequest(map[string]any{
		"short_id": shortID,
		"version":  version,
	}))
	if err != nil {
		t.Fatalf("done error: %v", err)
	}
	done := parseToolResult(t, doneResult)
	tags, _ = done["tags"].([]any)
	if len(tags) != 1 || tags[0] != "keep-me" {
		t.Fatalf("done: expected tags [keep-me], got %v", tags)
	}
}

func TestHandleTaskModify(t *testing.T) {
	s := testServer(t)
	ctx := context.Background()

	createResult, _ := s.handleTaskCreate(ctx, callToolRequest(map[string]any{
		"title": "Original",
	}))
	created := parseToolResult(t, createResult)
	shortID := created["short_id"].(string)
	version := created["version"].(float64)

	t.Run("modify title and add tags", func(t *testing.T) {
		result, err := s.handleTaskModify(ctx, callToolRequest(map[string]any{
			"short_id": shortID,
			"version":  version,
			"title":    "Modified",
			"add_tags": []any{"new-tag"},
		}))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		parsed := parseToolResult(t, result)
		if parsed["title"] != "Modified" {
			t.Fatalf("expected 'Modified', got %v", parsed["title"])
		}
		tags, _ := parsed["tags"].([]any)
		if len(tags) != 1 || tags[0] != "new-tag" {
			t.Fatalf("expected tags [new-tag], got %v", tags)
		}
	})

	t.Run("version conflict returns tool error", func(t *testing.T) {
		result, _ := s.handleTaskModify(ctx, callToolRequest(map[string]any{
			"short_id": shortID,
			"version":  float64(999),
			"title":    "Conflict",
		}))
		msg := getToolErrorText(t, result)
		if msg != "version conflict: task was modified, re-fetch and retry" {
			t.Fatalf("expected version conflict, got %q", msg)
		}
	})
}

func TestHandleTaskLink(t *testing.T) {
	s := testServer(t)
	ctx := context.Background()

	r1, _ := s.handleTaskCreate(ctx, callToolRequest(map[string]any{"title": "Source"}))
	r2, _ := s.handleTaskCreate(ctx, callToolRequest(map[string]any{"title": "Target"}))
	source := parseToolResult(t, r1)["short_id"].(string)
	target := parseToolResult(t, r2)["short_id"].(string)

	t.Run("add blocks relation", func(t *testing.T) {
		result, err := s.handleTaskLink(ctx, callToolRequest(map[string]any{
			"source": source,
			"target": target,
			"type":   "blocks",
		}))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		parsed := parseToolResult(t, result)
		if parsed["relation_type"] != "blocks" {
			t.Fatalf("expected 'blocks', got %v", parsed["relation_type"])
		}
	})

	t.Run("duplicate returns tool error", func(t *testing.T) {
		result, _ := s.handleTaskLink(ctx, callToolRequest(map[string]any{
			"source": source,
			"target": target,
			"type":   "blocks",
		}))
		msg := getToolErrorText(t, result)
		if msg != "relation already exists" {
			t.Fatalf("expected duplicate error, got %q", msg)
		}
	})

	t.Run("cycle returns tool error", func(t *testing.T) {
		result, _ := s.handleTaskLink(ctx, callToolRequest(map[string]any{
			"source": target,
			"target": source,
			"type":   "blocks",
		}))
		msg := getToolErrorText(t, result)
		if msg != "would create a dependency cycle" {
			t.Fatalf("expected cycle error, got %q", msg)
		}
	})
}

func TestHandleProjectList(t *testing.T) {
	s := testServer(t)
	ctx := context.Background()

	result, err := s.handleProjectList(ctx, callToolRequest(map[string]any{}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	items := parseToolResultArray(t, result)
	// Should have at least "default"
	found := false
	for _, p := range items {
		if p["id"] == "default" {
			found = true
		}
	}
	if !found {
		t.Fatal("default project not found in list")
	}
}

func TestHandleTaskTree(t *testing.T) {
	s := testServer(t)
	ctx := context.Background()

	parentResult, _ := s.handleTaskCreate(ctx, callToolRequest(map[string]any{"title": "Parent"}))
	parent := parseToolResult(t, parentResult)
	parentSID := parent["short_id"].(string)

	s.handleTaskCreate(ctx, callToolRequest(map[string]any{"title": "Child", "parent": parentSID}))

	result, err := s.handleTaskTree(ctx, callToolRequest(map[string]any{
		"short_id": parentSID,
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text, _ := result.Content[0].(mcp.TextContent)
	var tree []map[string]any
	if err := json.Unmarshal([]byte(text.Text), &tree); err != nil {
		t.Fatalf("parsing tree JSON: %v", err)
	}
	if len(tree) != 1 {
		t.Fatalf("expected 1 root, got %d", len(tree))
	}
	children, _ := tree[0]["children"].([]any)
	if len(children) != 1 {
		t.Fatalf("expected 1 child, got %d", len(children))
	}
}

func TestHandleTaskAnnotate(t *testing.T) {
	s := testServer(t)
	ctx := context.Background()

	createResult, _ := s.handleTaskCreate(ctx, callToolRequest(map[string]any{"title": "Annotate me"}))
	shortID := parseToolResult(t, createResult)["short_id"].(string)

	result, err := s.handleTaskAnnotate(ctx, callToolRequest(map[string]any{
		"short_id": shortID,
		"body":     "test note",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	parsed := parseToolResult(t, result)
	if parsed["body"] != "test note" {
		t.Fatalf("expected body 'test note', got %v", parsed["body"])
	}
}
