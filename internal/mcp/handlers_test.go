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

func (adapter *storeWriteTxAdapter) Tasks() repository.TaskRepository { return adapter.tx.Tasks() }
func (adapter *storeWriteTxAdapter) Relations() repository.RelationRepository {
	return adapter.tx.Relations()
}
func (adapter *storeWriteTxAdapter) Events() repository.EventRepository {
	return adapter.tx.Events(10000, 1000)
}

func (adapter *storeWriteTxAdapter) Projects() repository.ProjectRepository {
	return adapter.tx.Projects()
}
func (adapter *storeWriteTxAdapter) Workflows() repository.WorkflowRepository {
	return adapter.tx.Workflows()
}
func (adapter *storeWriteTxAdapter) Players() repository.PlayerRepository {
	return adapter.tx.Players()
}
func (adapter *storeWriteTxAdapter) Tags() repository.TagRepository { return adapter.tx.Tags() }
func (adapter *storeWriteTxAdapter) Annotations() repository.AnnotationRepository {
	return adapter.tx.Annotations()
}
func (adapter *storeWriteTxAdapter) Notes() repository.NoteRepository { return adapter.tx.Notes() }

func (adapter *storeWriteTxAdapter) TruncateAll(ctx context.Context) error {
	return adapter.tx.TruncateAll(ctx)
}

func (wp *storeWriteTx) WithTx(ctx context.Context, fn func(tx service.WriteTx) error) error {
	return wp.store.WithTx(ctx, func(stx *sqlite.Tx) error {
		return fn(&storeWriteTxAdapter{tx: stx})
	})
}

// testServer creates a fully wired Server with an in-memory SQLite DB.
func testServer(test *testing.T) *Server {
	test.Helper()
	store, projectRepo, workflowRepo := sqlitetest.NewStore(test)

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

	server, err := New(
		taskSvc, tagSvc, relationSvc, projectSvc, workflowSvc, playerSvc, noteSvc,
		nil, nil, nil,
		"test", config.MCPConfig{}, nil,
	)

	if err != nil {
		test.Fatalf("creating MCP server: %v", err)
	}

	return server
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
func parseToolResult(test *testing.T, result *mcp.CallToolResult) map[string]any {
	test.Helper()
	if result.IsError {
		test.Fatalf("unexpected tool error: %v", result.Content)
	}
	text, ok := result.Content[0].(mcp.TextContent)
	if !ok {
		test.Fatalf("expected TextContent, got %T", result.Content[0])
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(text.Text), &parsed); err != nil {
		test.Fatalf("parsing tool result JSON: %v", err)
	}
	return parsed
}

// parseToolResultArray extracts the JSON array content from a tool result.
func parseToolResultArray(test *testing.T, result *mcp.CallToolResult) []map[string]any {
	test.Helper()
	text, ok := result.Content[0].(mcp.TextContent)
	if !ok {
		test.Fatalf("expected TextContent, got %T", result.Content[0])
	}
	var parsed []map[string]any
	if err := json.Unmarshal([]byte(text.Text), &parsed); err != nil {
		test.Fatalf("parsing tool result JSON array: %v", err)
	}
	return parsed
}

// getToolErrorText extracts the error message from an isError tool result.
func getToolErrorText(test *testing.T, result *mcp.CallToolResult) string {
	test.Helper()
	if !result.IsError {
		test.Fatal("expected tool error, got success")
	}
	text, ok := result.Content[0].(mcp.TextContent)
	if !ok {
		test.Fatalf("expected TextContent, got %T", result.Content[0])
	}
	return text.Text
}

func TestHandleTaskCreate(test *testing.T) {
	server := testServer(test)
	ctx := context.Background()

	test.Run("basic create", func(test *testing.T) {
		result, err := server.handleTaskCreate(ctx, callToolRequest(map[string]any{
			"title":    "Test task",
			"priority": float64(3),
		}))

		if err != nil {
			test.Fatalf("unexpected error: %v", err)
		}

		parsed := parseToolResult(test, result)
		if parsed["title"] != "Test task" {
			test.Fatalf("expected title 'Test task', got %v", parsed["title"])
		}
		if parsed["priority"].(float64) != 3 {
			test.Fatalf("expected priority 3, got %v", parsed["priority"])
		}
		if parsed["status"] != "pending" {
			test.Fatalf("expected status 'pending', got %v", parsed["status"])
		}
		if parsed["version"].(float64) != 1 {
			test.Fatalf("expected version 1, got %v", parsed["version"])
		}
	})

	test.Run("create with tags", func(test *testing.T) {
		result, err := server.handleTaskCreate(ctx, callToolRequest(map[string]any{
			"title": "Tagged task",
			"tags":  []any{"alpha", "beta"},
		}))

		if err != nil {
			test.Fatalf("unexpected error: %v", err)
		}

		parsed := parseToolResult(test, result)
		tags, _ := parsed["tags"].([]any)
		if len(tags) != 2 {
			test.Fatalf("expected 2 tags, got %d", len(tags))
		}
	})

	test.Run("missing title returns tool error", func(test *testing.T) {
		result, err := server.handleTaskCreate(ctx, callToolRequest(map[string]any{}))

		if err != nil {
			test.Fatalf("unexpected transport error: %v", err)
		}

		msg := getToolErrorText(test, result)
		if msg != "title is required" {
			test.Fatalf("expected 'title is required', got %q", msg)
		}
	})
}

func TestHandleTaskGet(test *testing.T) {
	server := testServer(test)
	ctx := context.Background()

	// Create a task first
	createResult, _ := server.handleTaskCreate(ctx, callToolRequest(map[string]any{
		"title": "Get me",
		"tags":  []any{"fetched"},
	}))
	created := parseToolResult(test, createResult)
	shortID := created["short_id"].(string)

	// Add annotation
	server.handleTaskAnnotate(ctx, callToolRequest(map[string]any{
		"short_id": shortID,
		"body":     "test annotation",
	}))

	test.Run("returns full details", func(test *testing.T) {
		result, err := server.handleTaskGet(ctx, callToolRequest(map[string]any{
			"short_id": shortID,
		}))

		if err != nil {
			test.Fatalf("unexpected error: %v", err)
		}

		parsed := parseToolResult(test, result)
		if parsed["title"] != "Get me" {
			test.Fatalf("expected title 'Get me', got %v", parsed["title"])
		}
		tags, _ := parsed["tags"].([]any)
		if len(tags) != 1 || tags[0] != "fetched" {
			test.Fatalf("expected tags [fetched], got %v", tags)
		}
		annotations, _ := parsed["annotations"].([]any)
		if len(annotations) != 1 {
			test.Fatalf("expected 1 annotation, got %d", len(annotations))
		}
	})

	test.Run("not found returns tool error", func(test *testing.T) {
		result, _ := server.handleTaskGet(ctx, callToolRequest(map[string]any{
			"short_id": "nonexistent",
		}))
		msg := getToolErrorText(test, result)
		if msg != "not found: task nonexistent" {
			test.Fatalf("expected not found error, got %q", msg)
		}
	})
}

func TestHandleTaskList(test *testing.T) {
	server := testServer(test)
	ctx := context.Background()

	server.handleTaskCreate(ctx, callToolRequest(map[string]any{"title": "Task A"}))
	server.handleTaskCreate(ctx, callToolRequest(map[string]any{"title": "Task B", "priority": float64(4)}))

	test.Run("list all", func(test *testing.T) {
		result, err := server.handleTaskList(ctx, callToolRequest(map[string]any{}))

		if err != nil {
			test.Fatalf("unexpected error: %v", err)
		}

		items := parseToolResultArray(test, result)
		if len(items) != 2 {
			test.Fatalf("expected 2 tasks, got %d", len(items))
		}
	})

	test.Run("filter by priority", func(test *testing.T) {
		result, err := server.handleTaskList(ctx, callToolRequest(map[string]any{
			"priority_min": float64(4),
		}))

		if err != nil {
			test.Fatalf("unexpected error: %v", err)
		}

		items := parseToolResultArray(test, result)
		if len(items) != 1 {
			test.Fatalf("expected 1 task with priority >= 4, got %d", len(items))
		}
		if items[0]["title"] != "Task B" {
			test.Fatalf("expected 'Task B', got %v", items[0]["title"])
		}
	})
}

func TestHandleTaskTransition_IncludesTags(test *testing.T) {
	server := testServer(test)
	ctx := context.Background()

	createResult, _ := server.handleTaskCreate(ctx, callToolRequest(map[string]any{
		"title": "Transition tags",
		"tags":  []any{"keep-me"},
	}))
	created := parseToolResult(test, createResult)
	shortID := created["short_id"].(string)
	version := created["version"].(float64)

	// Start
	startResult, startErr := server.handleTaskStart(ctx, callToolRequest(map[string]any{
		"short_id": shortID,
		"version":  version,
	}))

	if startErr != nil {
		test.Fatalf("start error: %v", startErr)
	}

	started := parseToolResult(test, startResult)
	tags, _ := started["tags"].([]any)
	if len(tags) != 1 || tags[0] != "keep-me" {
		test.Fatalf("start: expected tags [keep-me], got %v", tags)
	}

	version = started["version"].(float64)

	// Done
	doneResult, doneErr := server.handleTaskDone(ctx, callToolRequest(map[string]any{
		"short_id": shortID,
		"version":  version,
	}))

	if doneErr != nil {
		test.Fatalf("done error: %v", doneErr)
	}

	done := parseToolResult(test, doneResult)
	tags, _ = done["tags"].([]any)
	if len(tags) != 1 || tags[0] != "keep-me" {
		test.Fatalf("done: expected tags [keep-me], got %v", tags)
	}
}

func TestHandleTaskModify(test *testing.T) {
	server := testServer(test)
	ctx := context.Background()

	createResult, _ := server.handleTaskCreate(ctx, callToolRequest(map[string]any{
		"title": "Original",
	}))
	created := parseToolResult(test, createResult)
	shortID := created["short_id"].(string)
	version := created["version"].(float64)

	test.Run("modify title and add tags", func(test *testing.T) {
		result, err := server.handleTaskModify(ctx, callToolRequest(map[string]any{
			"short_id": shortID,
			"version":  version,
			"title":    "Modified",
			"add_tags": []any{"new-tag"},
		}))

		if err != nil {
			test.Fatalf("unexpected error: %v", err)
		}

		parsed := parseToolResult(test, result)
		if parsed["title"] != "Modified" {
			test.Fatalf("expected 'Modified', got %v", parsed["title"])
		}
		tags, _ := parsed["tags"].([]any)
		if len(tags) != 1 || tags[0] != "new-tag" {
			test.Fatalf("expected tags [new-tag], got %v", tags)
		}
	})

	test.Run("version conflict returns tool error", func(test *testing.T) {
		result, _ := server.handleTaskModify(ctx, callToolRequest(map[string]any{
			"short_id": shortID,
			"version":  float64(999),
			"title":    "Conflict",
		}))
		msg := getToolErrorText(test, result)
		if msg != "version conflict: task was modified, re-fetch and retry" {
			test.Fatalf("expected version conflict, got %q", msg)
		}
	})
}

func TestHandleTaskLink(test *testing.T) {
	server := testServer(test)
	ctx := context.Background()

	r1, _ := server.handleTaskCreate(ctx, callToolRequest(map[string]any{"title": "Source"}))
	r2, _ := server.handleTaskCreate(ctx, callToolRequest(map[string]any{"title": "Target"}))
	source := parseToolResult(test, r1)["short_id"].(string)
	target := parseToolResult(test, r2)["short_id"].(string)

	test.Run("add blocks relation", func(test *testing.T) {
		result, err := server.handleTaskLink(ctx, callToolRequest(map[string]any{
			"source": source,
			"target": target,
			"type":   "blocks",
		}))

		if err != nil {
			test.Fatalf("unexpected error: %v", err)
		}

		parsed := parseToolResult(test, result)
		if parsed["relation_type"] != "blocks" {
			test.Fatalf("expected 'blocks', got %v", parsed["relation_type"])
		}
	})

	test.Run("duplicate returns tool error", func(test *testing.T) {
		result, _ := server.handleTaskLink(ctx, callToolRequest(map[string]any{
			"source": source,
			"target": target,
			"type":   "blocks",
		}))
		msg := getToolErrorText(test, result)
		if msg != "relation already exists" {
			test.Fatalf("expected duplicate error, got %q", msg)
		}
	})

	test.Run("cycle returns tool error", func(test *testing.T) {
		result, _ := server.handleTaskLink(ctx, callToolRequest(map[string]any{
			"source": target,
			"target": source,
			"type":   "blocks",
		}))
		msg := getToolErrorText(test, result)
		if msg != "would create a dependency cycle" {
			test.Fatalf("expected cycle error, got %q", msg)
		}
	})
}

func TestHandleProjectList(test *testing.T) {
	server := testServer(test)
	ctx := context.Background()

	result, err := server.handleProjectList(ctx, callToolRequest(map[string]any{}))

	if err != nil {
		test.Fatalf("unexpected error: %v", err)
	}

	items := parseToolResultArray(test, result)
	// Should have at least "default"
	found := false
	for _, proj := range items {
		if proj["id"] == "default" {
			found = true
		}
	}
	if !found {
		test.Fatal("default project not found in list")
	}
}

func TestHandleTaskTree(test *testing.T) {
	server := testServer(test)
	ctx := context.Background()

	parentResult, _ := server.handleTaskCreate(ctx, callToolRequest(map[string]any{"title": "Parent"}))
	parent := parseToolResult(test, parentResult)
	parentSID := parent["short_id"].(string)

	server.handleTaskCreate(ctx, callToolRequest(map[string]any{"title": "Child", "parent": parentSID}))

	result, err := server.handleTaskTree(ctx, callToolRequest(map[string]any{
		"short_id": parentSID,
	}))

	if err != nil {
		test.Fatalf("unexpected error: %v", err)
	}

	text, _ := result.Content[0].(mcp.TextContent)
	var tree []map[string]any
	if err := json.Unmarshal([]byte(text.Text), &tree); err != nil {
		test.Fatalf("parsing tree JSON: %v", err)
	}
	if len(tree) != 1 {
		test.Fatalf("expected 1 root, got %d", len(tree))
	}
	children, _ := tree[0]["children"].([]any)
	if len(children) != 1 {
		test.Fatalf("expected 1 child, got %d", len(children))
	}
}

func TestHandleTaskAnnotate(test *testing.T) {
	server := testServer(test)
	ctx := context.Background()

	createResult, _ := server.handleTaskCreate(ctx, callToolRequest(map[string]any{"title": "Annotate me"}))
	shortID := parseToolResult(test, createResult)["short_id"].(string)

	result, err := server.handleTaskAnnotate(ctx, callToolRequest(map[string]any{
		"short_id": shortID,
		"body":     "test note",
	}))

	if err != nil {
		test.Fatalf("unexpected error: %v", err)
	}

	parsed := parseToolResult(test, result)
	if parsed["body"] != "test note" {
		test.Fatalf("expected body 'test note', got %v", parsed["body"])
	}
}

func TestHandleTaskCreate_Level(test *testing.T) {
	server := testServer(test)
	ctx := context.Background()

	test.Run("level populates response", func(test *testing.T) {
		result, err := server.handleTaskCreate(ctx, callToolRequest(map[string]any{
			"title": "With level",
			"level": "story",
		}))

		if err != nil {
			test.Fatalf("unexpected error: %v", err)
		}

		parsed := parseToolResult(test, result)
		if parsed["level"] != "story" {
			test.Fatalf("expected level 'story', got %v", parsed["level"])
		}
	})

	test.Run("empty level rejected", func(test *testing.T) {
		result, err := server.handleTaskCreate(ctx, callToolRequest(map[string]any{
			"title": "Empty level",
			"level": "",
		}))

		if err != nil {
			test.Fatalf("unexpected transport error: %v", err)
		}

		msg := getToolErrorText(test, result)
		if msg == "" {
			test.Fatal("expected non-empty error message")
		}
	})

	test.Run("omitted level yields no level field", func(test *testing.T) {
		result, err := server.handleTaskCreate(ctx, callToolRequest(map[string]any{
			"title": "No level",
		}))

		if err != nil {
			test.Fatalf("unexpected error: %v", err)
		}

		parsed := parseToolResult(test, result)
		if _, ok := parsed["level"]; ok {
			test.Fatalf("expected no 'level' key in response, got %v", parsed["level"])
		}
	})
}

func TestHandleTaskModify_Level(test *testing.T) {
	server := testServer(test)
	ctx := context.Background()

	createResult, _ := server.handleTaskCreate(ctx, callToolRequest(map[string]any{
		"title": "Original",
		"level": "story",
	}))
	created := parseToolResult(test, createResult)
	shortID := created["short_id"].(string)
	version := created["version"].(float64)

	test.Run("empty level clears the field", func(test *testing.T) {
		result, err := server.handleTaskModify(ctx, callToolRequest(map[string]any{
			"short_id": shortID,
			"version":  version,
			"level":    "",
		}))

		if err != nil {
			test.Fatalf("unexpected error: %v", err)
		}

		parsed := parseToolResult(test, result)
		if _, ok := parsed["level"]; ok {
			test.Fatalf("expected 'level' omitted after clear, got %v", parsed["level"])
		}
	})
}

func TestHandleTaskList_IncludesLevel(test *testing.T) {
	server := testServer(test)
	ctx := context.Background()

	if _, err := server.handleTaskCreate(ctx, callToolRequest(map[string]any{
		"title": "With level",
		"level": "story",
	})); err != nil {
		test.Fatalf("create: %v", err)
	}

	result, err := server.handleTaskList(ctx, callToolRequest(map[string]any{}))

	if err != nil {
		test.Fatalf("list: %v", err)
	}

	items := parseToolResultArray(test, result)
	if len(items) == 0 {
		test.Fatal("expected at least one task")
	}
	if items[0]["level"] != "story" {
		test.Fatalf("expected level 'story', got %v", items[0]["level"])
	}
}
