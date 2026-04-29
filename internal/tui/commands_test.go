package tui

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/germanamz/tusk/config"
	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/repository"
	"github.com/germanamz/tusk/service"
	"github.com/germanamz/tusk/sqlite"
	"github.com/germanamz/tusk/sqlite/sqlitetest"
	"github.com/google/uuid"
)

// storeWriteTx adapts a *sqlite.Store to service.WriteTxProvider for TUI
// command tests.
type storeWriteTx struct{ store *sqlite.Store }

type storeWriteTxAdapter struct{ tx *sqlite.Tx }

func (w *storeWriteTxAdapter) Tasks() repository.TaskRepository         { return w.tx.Tasks() }
func (w *storeWriteTxAdapter) Relations() repository.RelationRepository { return w.tx.Relations() }
func (w *storeWriteTxAdapter) Events() repository.EventRepository       { return w.tx.Events(10000, 1000) }

func (w *storeWriteTxAdapter) Projects() repository.ProjectRepository   { return w.tx.Projects() }
func (w *storeWriteTxAdapter) Workflows() repository.WorkflowRepository { return w.tx.Workflows() }
func (w *storeWriteTxAdapter) Players() repository.PlayerRepository     { return w.tx.Players() }
func (w *storeWriteTxAdapter) Tags() repository.TagRepository           { return w.tx.Tags() }
func (w *storeWriteTxAdapter) Annotations() repository.AnnotationRepository {
	return w.tx.Annotations()
}
func (w *storeWriteTxAdapter) Notes() repository.NoteRepository { return w.tx.Notes() }

func (w *storeWriteTxAdapter) TruncateAll(ctx context.Context) error { return w.tx.TruncateAll(ctx) }

func (p *storeWriteTx) WithTx(ctx context.Context, fn func(tx service.WriteTx) error) error {
	return p.store.WithTx(ctx, func(stx *sqlite.Tx) error {
		return fn(&storeWriteTxAdapter{tx: stx})
	})
}

func TestFormatError_NotFound(test *testing.T) {
	err := fmt.Errorf("getting task: %w", domain.ErrNotFound)
	got := formatError(err, "abc12345")
	want := "Task not found: abc12345"
	if got != want {
		test.Fatalf("expected %q, got %q", want, got)
	}
}

func TestFormatError_Conflict(test *testing.T) {
	err := domain.ErrConflict
	got := formatError(err, "abc12345")
	want := "Version conflict - task was modified by another process"
	if got != want {
		test.Fatalf("expected %q, got %q", want, got)
	}
}

func TestFormatError_InvalidTransition(test *testing.T) {
	err := fmt.Errorf("transition %q → %q not allowed: %w", "pending", "completed", domain.ErrInvalidTransition)
	got := formatError(err, "abc12345")
	if !strings.Contains(got, "pending") || !strings.Contains(got, "completed") {
		test.Fatalf("expected transition details in error, got %q", got)
	}
	if !strings.Contains(got, "not allowed") {
		test.Fatalf("expected 'not allowed' in error, got %q", got)
	}
}

func TestFormatError_CyclicParent(test *testing.T) {
	err := fmt.Errorf("setting parent= %w", domain.ErrCyclicParent)
	got := formatError(err, "abc12345")
	want := "parent would create a cycle in task hierarchy"
	if got != want {
		test.Fatalf("expected %q, got %q", want, got)
	}
}

func TestFormatError_Generic(test *testing.T) {
	err := fmt.Errorf("something went wrong")
	got := formatError(err, "abc12345")
	if got != "something went wrong" {
		test.Fatalf("expected original message, got %q", got)
	}
}

// testApp creates a fully wired App with an in-memory database.
func testApp(test *testing.T) (*App, *service.TaskService) {
	test.Helper()
	store, projectRepo, workflowRepo := sqlitetest.NewStore(test)

	db := store.DB()
	bundle := &service.RepoBundle{
		Store:       store,
		WriteTx:     &storeWriteTx{store: store},
		Tasks:       sqlite.NewTaskRepo(db),
		Annotations: sqlite.NewAnnotationRepo(db),
		Relations:   sqlite.NewRelationRepo(db),
		Tags:        sqlite.NewTagRepo(db),
		Players:     sqlite.NewPlayerRepo(db),
	}

	resolver := func(context.Context, uuid.UUID) (*service.RepoBundle, error) { return bundle, nil }
	projects := func(context.Context) ([]uuid.UUID, error) { return []uuid.UUID{domain.DefaultProjectUUID}, nil }

	workflowSvc := service.NewWorkflowService(workflowRepo, projectRepo)
	projectSvc := service.NewProjectService(projectRepo, bundle.Tasks, bundle.Store, service.ProjectDefaults{}, nil)
	taskSvc := service.NewTaskService(resolver, projects, projectRepo, projectSvc, workflowSvc, nil)
	tagSvc := service.NewTagService(resolver)
	relationSvc := service.NewRelationService(resolver, projects)
	// Point config.Load at an isolated temp dir so `config show` does not
	// trip over the developer's real ~/.config/tusk/config.toml (which
	// may still carry legacy [workflows.*] / [projects.*] sections until
	// the user cleans it up post-phase-2).
	loadOpts := []config.Option{config.WithSearchPath(test.TempDir())}
	app := New(
		taskSvc, tagSvc, relationSvc, projectSvc, workflowSvc, nil, nil,
		nil,
		nil, nil, nil,
		VersionInfo{}, config.TUIConfig{}, config.MCPConfig{}, config.InlineConfig{}, loadOpts,
	)
	return app, taskSvc
}

func TestRunList_Empty(test *testing.T) {
	app, _ := testApp(test)

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"task", "list"})
	if err := app.root.Execute(); err != nil {
		test.Fatalf("list: %v", err)
	}
	if buf.String() != "" {
		test.Fatalf("expected empty output, got %q", buf.String())
	}
}

func TestRunList_WithTasks(test *testing.T) {
	app, taskSvc := testApp(test)
	ctx := context.Background()

	task := &domain.Task{Title: "Test task", Priority: 3}
	if err := taskSvc.Create(ctx, task); err != nil {
		test.Fatalf("Create: %v", err)
	}

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"task", "list"})
	if err := app.root.Execute(); err != nil {
		test.Fatalf("list: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, task.ShortID) {
		test.Fatalf("expected short ID in output, got:\n%s", out)
	}
	if !strings.Contains(out, "H") {
		test.Fatalf("expected priority H in output, got:\n%s", out)
	}
}

func TestRunList_StatusFilter(test *testing.T) {
	app, taskSvc := testApp(test)
	ctx := context.Background()

	task := &domain.Task{Title: "Completed task"}
	if err := taskSvc.Create(ctx, task); err != nil {
		test.Fatalf("Create: %v", err)
	}
	// Start then complete
	taskSvc.Start(ctx, task.ShortID, 1, "")
	taskSvc.Complete(ctx, task.ShortID, 2)

	// Default list should NOT show completed tasks
	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"task", "list"})
	if err := app.root.Execute(); err != nil {
		test.Fatalf("list: %v", err)
	}
	if strings.Contains(buf.String(), task.ShortID) {
		test.Fatalf("expected completed task to be hidden from default list")
	}

	// Explicit status filter should show it
	buf.Reset()
	app.root.SetArgs([]string{"task", "list", "status=completed"})
	if err := app.root.Execute(); err != nil {
		test.Fatalf("list status=completed: %v", err)
	}
	if !strings.Contains(buf.String(), task.ShortID) {
		test.Fatalf("expected completed task in filtered list, got:\n%s", buf.String())
	}
}

func TestRunList_JSON(test *testing.T) {
	app, taskSvc := testApp(test)
	ctx := context.Background()

	task := &domain.Task{Title: "JSON task"}
	if err := taskSvc.Create(ctx, task); err != nil {
		test.Fatalf("Create: %v", err)
	}

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"task", "list", "--format", "json"})
	if err := app.root.Execute(); err != nil {
		test.Fatalf("list --format json: %v", err)
	}
	if !strings.Contains(buf.String(), `"short_id"`) {
		test.Fatalf("expected JSON output, got:\n%s", buf.String())
	}
}

func TestRunInfo_HappyPath(test *testing.T) {
	app, taskSvc := testApp(test)
	ctx := context.Background()

	task := &domain.Task{Title: "Info test", Priority: 2}
	if err := taskSvc.Create(ctx, task); err != nil {
		test.Fatalf("Create: %v", err)
	}
	taskSvc.Annotate(ctx, task.ShortID, "A note")

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"task", "get", task.ShortID})
	if err := app.root.Execute(); err != nil {
		test.Fatalf("info: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, task.ShortID) {
		test.Fatalf("expected short ID in output, got:\n%s", out)
	}
	if !strings.Contains(out, "Info test") {
		test.Fatalf("expected title in output, got:\n%s", out)
	}
	if !strings.Contains(out, "medium") {
		test.Fatalf("expected priority name in output, got:\n%s", out)
	}
	if !strings.Contains(out, "A note") {
		test.Fatalf("expected annotation in output, got:\n%s", out)
	}
}

func TestRunInfo_NotFound(test *testing.T) {
	app, _ := testApp(test)

	app.root.SetArgs([]string{"task", "get", "nonexist"})
	err := app.root.Execute()
	if err == nil {
		test.Fatal("expected error for nonexistent task")
	}
	if !strings.Contains(err.Error(), "not found") {
		test.Fatalf("expected 'not found' in error, got %q", err.Error())
	}
}

func TestRunAdd_HappyPath(test *testing.T) {
	app, _ := testApp(test)

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"task", "create", "My", "new", "task"})
	if err := app.root.Execute(); err != nil {
		test.Fatalf("add: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "Created task") {
		test.Fatalf("expected 'Created task' in output, got %q", out)
	}
}

func TestRunAdd_WithPriority(test *testing.T) {
	app, taskSvc := testApp(test)
	ctx := context.Background()

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"task", "create", "Priority", "task", "priority=high"})
	if err := app.root.Execute(); err != nil {
		test.Fatalf("add: %v", err)
	}

	// Extract short ID from "Created task <id>\n"
	out := strings.TrimSpace(buf.String())
	parts := strings.Fields(out)
	shortID := parts[len(parts)-1]

	task, taskErr := taskSvc.GetByShortID(ctx, shortID)

	if taskErr != nil {
		test.Fatalf("GetByShortID: %v", taskErr)
	}

	if task.Priority != 3 {
		test.Fatalf("expected priority 3, got %d", task.Priority)
	}
}

func TestRunAdd_WithDueDate(test *testing.T) {
	app, taskSvc := testApp(test)
	ctx := context.Background()

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"task", "create", "Due", "task", "due=2026-04-10"})
	if err := app.root.Execute(); err != nil {
		test.Fatalf("add: %v", err)
	}

	out := strings.TrimSpace(buf.String())
	parts := strings.Fields(out)
	shortID := parts[len(parts)-1]

	task, taskErr := taskSvc.GetByShortID(ctx, shortID)

	if taskErr != nil {
		test.Fatalf("GetByShortID: %v", taskErr)
	}

	if task.DueAt == nil {
		test.Fatal("expected DueAt to be set")
	}
	if task.DueAt.Format("2006-01-02") != "2026-04-10" {
		test.Fatalf("expected due 2026-04-10, got %s", task.DueAt.Format("2006-01-02"))
	}
}

func TestRunAdd_WithParent(test *testing.T) {
	app, taskSvc := testApp(test)
	ctx := context.Background()

	parent := &domain.Task{Title: "Parent"}
	if err := taskSvc.Create(ctx, parent); err != nil {
		test.Fatalf("Create parent= %v", err)
	}

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"task", "create", "Child", "task", "parent=" + parent.ShortID})
	if err := app.root.Execute(); err != nil {
		test.Fatalf("add: %v", err)
	}

	out := strings.TrimSpace(buf.String())
	parts := strings.Fields(out)
	shortID := parts[len(parts)-1]

	child, childErr := taskSvc.GetByShortID(ctx, shortID)

	if childErr != nil {
		test.Fatalf("GetByShortID: %v", childErr)
	}

	if child.ParentID == nil || *child.ParentID != parent.ID {
		test.Fatal("expected child to reference parent")
	}
}

func TestRunAdd_Tags(test *testing.T) {
	app, _ := testApp(test)

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"task", "create", "Tagged", "task", "+api"})
	if err := app.root.Execute(); err != nil {
		test.Fatalf("add with tag: %v", err)
	}
}

func TestRunAdd_NoTitle(test *testing.T) {
	app, _ := testApp(test)

	// Only key=value args, no title words
	app.root.SetArgs([]string{"task", "create", "priority=3"})
	err := app.root.Execute()
	if err == nil {
		test.Fatal("expected error for missing title")
	}
}

func TestRunAdd_JSON(test *testing.T) {
	app, _ := testApp(test)

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"task", "create", "JSON", "task", "--format", "json"})
	if err := app.root.Execute(); err != nil {
		test.Fatalf("add --format json: %v", err)
	}
	if !strings.Contains(buf.String(), `"short_id"`) {
		test.Fatalf("expected JSON output, got:\n%s", buf.String())
	}
}

func TestRunCreate_RejectsUrgencyFields(test *testing.T) {
	app, _ := testApp(test)

	app.root.SetArgs([]string{"task", "create", "Bad", "task", "urgency.priority-weight=5"})
	err := app.root.Execute()
	if err == nil {
		test.Fatal("expected error for urgency field on create")
	}
	if !strings.Contains(err.Error(), "urgency.priority-weight") {
		test.Fatalf("expected error to name unknown field, got %q", err.Error())
	}
	if !strings.Contains(err.Error(), "unknown") {
		test.Fatalf("expected 'unknown' in error, got %q", err.Error())
	}
}

func TestRunInfo_JSON(test *testing.T) {
	app, taskSvc := testApp(test)
	ctx := context.Background()

	task := &domain.Task{Title: "JSON info test"}
	if err := taskSvc.Create(ctx, task); err != nil {
		test.Fatalf("Create: %v", err)
	}

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"task", "get", task.ShortID, "--format", "json"})
	if err := app.root.Execute(); err != nil {
		test.Fatalf("info --format json: %v", err)
	}
	if !strings.Contains(buf.String(), `"short_id"`) {
		test.Fatalf("expected JSON output, got:\n%s", buf.String())
	}
}

func TestRunModify_Title(test *testing.T) {
	app, taskSvc := testApp(test)
	ctx := context.Background()

	task := &domain.Task{Title: "Original"}
	if err := taskSvc.Create(ctx, task); err != nil {
		test.Fatalf("Create: %v", err)
	}

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"task", "modify", task.ShortID, "Updated"})
	if err := app.root.Execute(); err != nil {
		test.Fatalf("modify: %v", err)
	}

	got, _ := taskSvc.GetByShortID(ctx, task.ShortID)
	if got.Title != "Updated" {
		test.Fatalf("expected title 'Updated', got %q", got.Title)
	}
	if got.Version != 2 {
		test.Fatalf("expected version 2, got %d", got.Version)
	}
}

func TestRunModify_Priority(test *testing.T) {
	app, taskSvc := testApp(test)
	ctx := context.Background()

	task := &domain.Task{Title: "Modify priority"}
	if err := taskSvc.Create(ctx, task); err != nil {
		test.Fatalf("Create: %v", err)
	}

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"task", "modify", task.ShortID, "priority=urgent"})
	if err := app.root.Execute(); err != nil {
		test.Fatalf("modify: %v", err)
	}

	got, _ := taskSvc.GetByShortID(ctx, task.ShortID)
	if got.Priority != 4 {
		test.Fatalf("expected priority 4, got %d", got.Priority)
	}
}

func TestRunModify_NotFound(test *testing.T) {
	app, _ := testApp(test)

	app.root.SetArgs([]string{"task", "modify", "nonexist", "Nope"})
	err := app.root.Execute()
	if err == nil {
		test.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "not found") {
		test.Fatalf("expected 'not found', got %q", err.Error())
	}
}

func TestRunModify_Tags(test *testing.T) {
	app, taskSvc := testApp(test)
	ctx := context.Background()

	task := &domain.Task{Title: "Tag test"}
	taskSvc.Create(ctx, task)

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"task", "modify", task.ShortID, "+api"})
	if err := app.root.Execute(); err != nil {
		test.Fatalf("modify with tag: %v", err)
	}
}

func TestRunStart_HappyPath(test *testing.T) {
	app, taskSvc := testApp(test)
	ctx := context.Background()

	task := &domain.Task{Title: "Start me"}
	taskSvc.Create(ctx, task)

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"task", "start", task.ShortID})
	if err := app.root.Execute(); err != nil {
		test.Fatalf("start: %v", err)
	}

	out := strings.TrimSpace(buf.String())
	if out != "Started task "+task.ShortID {
		test.Fatalf("expected 'Started task %s', got %q", task.ShortID, out)
	}

	got, _ := taskSvc.GetByShortID(ctx, task.ShortID)
	if got.Status != "active" {
		test.Fatalf("expected active, got %q", got.Status)
	}
}

func TestRunStart_NotFound(test *testing.T) {
	app, _ := testApp(test)

	app.root.SetArgs([]string{"task", "start", "nonexist"})
	err := app.root.Execute()
	if err == nil || !strings.Contains(err.Error(), "not found") {
		test.Fatalf("expected 'not found' error, got %v", err)
	}
}

func TestRunDone_HappyPath(test *testing.T) {
	app, taskSvc := testApp(test)
	ctx := context.Background()

	task := &domain.Task{Title: "Complete me"}
	taskSvc.Create(ctx, task)
	taskSvc.Start(ctx, task.ShortID, 1, "")

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"task", "done", task.ShortID})
	if err := app.root.Execute(); err != nil {
		test.Fatalf("done: %v", err)
	}

	out := strings.TrimSpace(buf.String())
	if out != "Completed task "+task.ShortID {
		test.Fatalf("expected 'Completed task %s', got %q", task.ShortID, out)
	}

	got, _ := taskSvc.GetByShortID(ctx, task.ShortID)
	if got.Status != "completed" {
		test.Fatalf("expected completed, got %q", got.Status)
	}
}

func TestRunDone_FromPending(test *testing.T) {
	app, taskSvc := testApp(test)
	ctx := context.Background()

	task := &domain.Task{Title: "Skip start"}
	taskSvc.Create(ctx, task)

	app.root.SetArgs([]string{"task", "done", task.ShortID})
	err := app.root.Execute()
	if err == nil {
		test.Fatal("expected error for invalid transition")
	}
}

func TestRunDelete_HappyPath(test *testing.T) {
	app, taskSvc := testApp(test)
	ctx := context.Background()

	task := &domain.Task{Title: "Delete me"}
	taskSvc.Create(ctx, task)

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"task", "delete", task.ShortID})
	if err := app.root.Execute(); err != nil {
		test.Fatalf("delete: %v", err)
	}

	out := strings.TrimSpace(buf.String())
	if out != "Deleted task "+task.ShortID {
		test.Fatalf("expected 'Deleted task %s', got %q", task.ShortID, out)
	}

	got, _ := taskSvc.GetByShortID(ctx, task.ShortID)
	if got.Status != "deleted" {
		test.Fatalf("expected deleted, got %q", got.Status)
	}
}

func TestRunAnnotate_HappyPath(test *testing.T) {
	app, taskSvc := testApp(test)
	ctx := context.Background()

	task := &domain.Task{Title: "Annotate me"}
	taskSvc.Create(ctx, task)

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"task", "annotate", task.ShortID, "This", "is", "a", "note"})
	if err := app.root.Execute(); err != nil {
		test.Fatalf("annotate: %v", err)
	}

	out := strings.TrimSpace(buf.String())
	if out != "Annotated task "+task.ShortID {
		test.Fatalf("expected 'Annotated task %s', got %q", task.ShortID, out)
	}

	annotations, _ := taskSvc.GetAnnotations(ctx, task.ShortID)
	if len(annotations) != 1 {
		test.Fatalf("expected 1 annotation, got %d", len(annotations))
	}
	if annotations[0].Body != "This is a note" {
		test.Fatalf("expected 'This is a note', got %q", annotations[0].Body)
	}
}

func TestRunAnnotate_NotFound(test *testing.T) {
	app, _ := testApp(test)

	app.root.SetArgs([]string{"task", "annotate", "nonexist", "A", "note"})
	err := app.root.Execute()
	if err == nil || !strings.Contains(err.Error(), "not found") {
		test.Fatalf("expected 'not found' error, got %v", err)
	}
}

func TestRunAnnotate_JSON(test *testing.T) {
	app, taskSvc := testApp(test)
	ctx := context.Background()

	task := &domain.Task{Title: "JSON annotate"}
	taskSvc.Create(ctx, task)

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"task", "annotate", task.ShortID, "A", "note", "--format", "json"})
	if err := app.root.Execute(); err != nil {
		test.Fatalf("annotate --format json: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, `"short_id"`) {
		test.Fatalf("expected task JSON with short_id, got:\n%s", out)
	}
	if !strings.Contains(out, `"version"`) {
		test.Fatalf("expected task JSON with version, got:\n%s", out)
	}
}

func TestRunModify_DueDate(test *testing.T) {
	app, taskSvc := testApp(test)
	ctx := context.Background()

	task := &domain.Task{Title: "Due test"}
	taskSvc.Create(ctx, task)

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"task", "modify", task.ShortID, "due=2026-04-15"})
	if err := app.root.Execute(); err != nil {
		test.Fatalf("modify due= %v", err)
	}

	got, _ := taskSvc.GetByShortID(ctx, task.ShortID)
	if got.DueAt == nil {
		test.Fatal("expected DueAt to be set")
	}
	if got.DueAt.Format("2006-01-02") != "2026-04-15" {
		test.Fatalf("expected due 2026-04-15, got %s", got.DueAt.Format("2006-01-02"))
	}
}

func TestRunModify_ClearDueDate(test *testing.T) {
	app, taskSvc := testApp(test)
	ctx := context.Background()

	due := mustParseTime(test, "2026-04-15")
	task := &domain.Task{Title: "Clear due", DueAt: &due}
	taskSvc.Create(ctx, task)

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"task", "modify", task.ShortID, "due="})
	if err := app.root.Execute(); err != nil {
		test.Fatalf("modify due clear: %v", err)
	}

	got, _ := taskSvc.GetByShortID(ctx, task.ShortID)
	if got.DueAt != nil {
		test.Fatal("expected DueAt to be cleared")
	}
}

func TestRunModify_Parent(test *testing.T) {
	app, taskSvc := testApp(test)
	ctx := context.Background()

	parent := &domain.Task{Title: "Parent"}
	taskSvc.Create(ctx, parent)
	child := &domain.Task{Title: "Child"}
	taskSvc.Create(ctx, child)

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"task", "modify", child.ShortID, "parent=" + parent.ShortID})
	if err := app.root.Execute(); err != nil {
		test.Fatalf("modify parent= %v", err)
	}

	got, _ := taskSvc.GetByShortID(ctx, child.ShortID)
	if got.ParentID == nil || *got.ParentID != parent.ID {
		test.Fatal("expected parent to be set")
	}
}

func TestRunModify_ClearParent(test *testing.T) {
	app, taskSvc := testApp(test)
	ctx := context.Background()

	parent := &domain.Task{Title: "Parent"}
	taskSvc.Create(ctx, parent)
	child := &domain.Task{Title: "Child", ParentID: &parent.ID}
	taskSvc.Create(ctx, child)

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"task", "modify", child.ShortID, "parent="})
	if err := app.root.Execute(); err != nil {
		test.Fatalf("modify clear parent= %v", err)
	}

	got, _ := taskSvc.GetByShortID(ctx, child.ShortID)
	if got.ParentID != nil {
		test.Fatal("expected parent to be cleared")
	}
}

func TestRunModify_Project(test *testing.T) {
	app, taskSvc := testApp(test)
	ctx := context.Background()

	task := &domain.Task{Title: "Project test"}
	taskSvc.Create(ctx, task)

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"task", "modify", task.ShortID, "project=default"})
	if err := app.root.Execute(); err != nil {
		test.Fatalf("modify project: %v", err)
	}

	out := strings.TrimSpace(buf.String())
	if !strings.Contains(out, "Modified task") {
		test.Fatalf("expected 'Modified task', got %q", out)
	}
}

func TestRunList_ParentFilter(test *testing.T) {
	app, taskSvc := testApp(test)
	ctx := context.Background()

	parent := &domain.Task{Title: "Parent"}
	taskSvc.Create(ctx, parent)
	child := &domain.Task{Title: "Child of parent", ParentID: &parent.ID}
	taskSvc.Create(ctx, child)
	other := &domain.Task{Title: "Unrelated task"}
	taskSvc.Create(ctx, other)

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"task", "list", "parent=" + parent.ShortID})
	if err := app.root.Execute(); err != nil {
		test.Fatalf("list parent= %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, child.ShortID) {
		test.Fatalf("expected child in output, got:\n%s", out)
	}
	if strings.Contains(out, other.ShortID) {
		test.Fatalf("expected unrelated task to be excluded, got:\n%s", out)
	}
}

func TestRunList_PriorityFilter(test *testing.T) {
	app, taskSvc := testApp(test)
	ctx := context.Background()

	low := &domain.Task{Title: "Low pri", Priority: 1}
	taskSvc.Create(ctx, low)
	high := &domain.Task{Title: "High pri", Priority: 3}
	taskSvc.Create(ctx, high)
	urgent := &domain.Task{Title: "Urgent pri", Priority: 4}
	taskSvc.Create(ctx, urgent)

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"task", "list", "priority=3..4"})
	if err := app.root.Execute(); err != nil {
		test.Fatalf("list priority: %v", err)
	}

	out := buf.String()
	if strings.Contains(out, low.ShortID) {
		test.Fatalf("expected low priority task to be excluded, got:\n%s", out)
	}
	if !strings.Contains(out, high.ShortID) {
		test.Fatalf("expected high priority task in output, got:\n%s", out)
	}
	if !strings.Contains(out, urgent.ShortID) {
		test.Fatalf("expected urgent priority task in output, got:\n%s", out)
	}
}

func TestRunInfo_ShowsProjectName(test *testing.T) {
	app, taskSvc := testApp(test)
	ctx := context.Background()

	task := &domain.Task{Title: "Project display test"}
	taskSvc.Create(ctx, task)

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"task", "get", task.ShortID})
	if err := app.root.Execute(); err != nil {
		test.Fatalf("info: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "default") {
		test.Fatalf("expected project name 'default' in output, got:\n%s", out)
	}
}

func TestRunInfo_JSON_IncludesAnnotations(test *testing.T) {
	app, taskSvc := testApp(test)
	ctx := context.Background()

	task := &domain.Task{Title: "Annotated task"}
	taskSvc.Create(ctx, task)
	taskSvc.Annotate(ctx, task.ShortID, "Important note")

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"task", "get", task.ShortID, "--format", "json"})
	if err := app.root.Execute(); err != nil {
		test.Fatalf("info json: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, `"annotations"`) {
		test.Fatalf("expected annotations in JSON output, got:\n%s", out)
	}
	if !strings.Contains(out, "Important note") {
		test.Fatalf("expected annotation body in JSON output, got:\n%s", out)
	}
}

func TestRunLink_HappyPath(test *testing.T) {
	app, taskSvc := testApp(test)
	ctx := context.Background()

	src := &domain.Task{Title: "Source"}
	taskSvc.Create(ctx, src)
	tgt := &domain.Task{Title: "Target"}
	taskSvc.Create(ctx, tgt)

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"task", "link", src.ShortID, "blocks", tgt.ShortID})
	if err := app.root.Execute(); err != nil {
		test.Fatalf("link: %v", err)
	}

	out := strings.TrimSpace(buf.String())
	if !strings.Contains(out, "Linked") {
		test.Fatalf("expected 'Linked' in output, got %q", out)
	}
	if !strings.Contains(out, src.ShortID) || !strings.Contains(out, tgt.ShortID) {
		test.Fatalf("expected both short IDs in output, got %q", out)
	}
}

func TestRunLink_JSON(test *testing.T) {
	app, taskSvc := testApp(test)
	ctx := context.Background()

	src := &domain.Task{Title: "Source"}
	taskSvc.Create(ctx, src)
	tgt := &domain.Task{Title: "Target"}
	taskSvc.Create(ctx, tgt)

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"task", "link", src.ShortID, "relates_to", tgt.ShortID, "--format", "json"})
	if err := app.root.Execute(); err != nil {
		test.Fatalf("link json: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, `"source_id"`) {
		test.Fatalf("expected source_id in JSON, got:\n%s", out)
	}
	if !strings.Contains(out, `"relation_type"`) {
		test.Fatalf("expected relation_type in JSON, got:\n%s", out)
	}
}

func TestRunLink_DuplicateRelation(test *testing.T) {
	app, taskSvc := testApp(test)
	ctx := context.Background()

	src := &domain.Task{Title: "Source"}
	taskSvc.Create(ctx, src)
	tgt := &domain.Task{Title: "Target"}
	taskSvc.Create(ctx, tgt)

	// First link succeeds
	app.root.SetArgs([]string{"task", "link", src.ShortID, "blocks", tgt.ShortID})
	if err := app.root.Execute(); err != nil {
		test.Fatalf("first link: %v", err)
	}

	// Second link should fail
	app.root.SetArgs([]string{"task", "link", src.ShortID, "blocks", tgt.ShortID})
	err := app.root.Execute()
	if err == nil {
		test.Fatal("expected error for duplicate relation")
	}
	if !strings.Contains(err.Error(), "already exists") {
		test.Fatalf("expected 'already exists', got %q", err.Error())
	}
}

func TestRunLink_NotFound(test *testing.T) {
	app, _ := testApp(test)

	app.root.SetArgs([]string{"task", "link", "nonexist", "blocks", "also_non"})
	err := app.root.Execute()
	if err == nil || !strings.Contains(err.Error(), "Source task not found: nonexist") {
		test.Fatalf("expected 'Source task not found: nonexist' error, got %v", err)
	}
}

func TestRunLink_TargetNotFound(test *testing.T) {
	app, taskSvc := testApp(test)
	ctx := context.Background()

	src := &domain.Task{Title: "Exists"}
	taskSvc.Create(ctx, src)

	app.root.SetArgs([]string{"task", "link", src.ShortID, "blocks", "nonexist"})
	err := app.root.Execute()
	if err == nil || !strings.Contains(err.Error(), "Target task not found: nonexist") {
		test.Fatalf("expected 'Target task not found: nonexist' error, got %v", err)
	}
}

func TestRunUnlink_HappyPath(test *testing.T) {
	app, taskSvc := testApp(test)
	ctx := context.Background()

	src := &domain.Task{Title: "Source"}
	taskSvc.Create(ctx, src)
	tgt := &domain.Task{Title: "Target"}
	taskSvc.Create(ctx, tgt)

	// Link first
	app.root.SetArgs([]string{"task", "link", src.ShortID, "blocks", tgt.ShortID})
	if err := app.root.Execute(); err != nil {
		test.Fatalf("link: %v", err)
	}

	// Unlink
	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"task", "unlink", src.ShortID, "blocks", tgt.ShortID})
	if err := app.root.Execute(); err != nil {
		test.Fatalf("unlink: %v", err)
	}

	out := strings.TrimSpace(buf.String())
	if !strings.Contains(out, "Unlinked") {
		test.Fatalf("expected 'Unlinked' in output, got %q", out)
	}
}

func TestRunUnlink_JSON(test *testing.T) {
	app, taskSvc := testApp(test)
	ctx := context.Background()

	src := &domain.Task{Title: "Source"}
	taskSvc.Create(ctx, src)
	tgt := &domain.Task{Title: "Target"}
	taskSvc.Create(ctx, tgt)

	app.root.SetArgs([]string{"task", "link", src.ShortID, "blocks", tgt.ShortID})
	app.root.Execute()

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"task", "unlink", src.ShortID, "blocks", tgt.ShortID, "--format", "json"})
	if err := app.root.Execute(); err != nil {
		test.Fatalf("unlink json: %v", err)
	}

	out := strings.TrimSpace(buf.String())
	if out != "{}" {
		test.Fatalf("expected '{}', got %q", out)
	}
}

func TestRunUnlink_NotFound(test *testing.T) {
	app, _ := testApp(test)

	app.root.SetArgs([]string{"task", "unlink", "nonexist", "blocks", "also_non"})
	err := app.root.Execute()
	if err == nil || !strings.Contains(err.Error(), "not found") {
		test.Fatalf("expected 'not found' error, got %v", err)
	}
}

func TestRunInfo_ShowsRelations(test *testing.T) {
	app, taskSvc := testApp(test)
	ctx := context.Background()

	src := &domain.Task{Title: "Blocker"}
	taskSvc.Create(ctx, src)
	tgt := &domain.Task{Title: "Blocked"}
	taskSvc.Create(ctx, tgt)

	app.root.SetArgs([]string{"task", "link", src.ShortID, "blocks", tgt.ShortID})
	if err := app.root.Execute(); err != nil {
		test.Fatalf("link: %v", err)
	}

	// Info on source should show "blocks"
	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"task", "get", src.ShortID})
	if err := app.root.Execute(); err != nil {
		test.Fatalf("info source: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "Relations:") {
		test.Fatalf("expected Relations: section, got:\n%s", out)
	}
	if !strings.Contains(out, "blocks") {
		test.Fatalf("expected 'blocks' label, got:\n%s", out)
	}
	// Verify related task short ID and title are shown
	if !strings.Contains(out, tgt.ShortID) {
		test.Fatalf("expected target short ID %q in relations, got:\n%s", tgt.ShortID, out)
	}
	if !strings.Contains(out, "Blocked") {
		test.Fatalf("expected target title 'Blocked' in relations, got:\n%s", out)
	}

	// Info on target should show "blocked_by"
	buf.Reset()
	app.root.SetArgs([]string{"task", "get", tgt.ShortID})
	if err := app.root.Execute(); err != nil {
		test.Fatalf("info target: %v", err)
	}

	out = buf.String()
	if !strings.Contains(out, "blocked_by") {
		test.Fatalf("expected 'blocked_by' label, got:\n%s", out)
	}
	// Verify source task short ID and title shown for inverse relation
	if !strings.Contains(out, src.ShortID) {
		test.Fatalf("expected source short ID %q in relations, got:\n%s", src.ShortID, out)
	}
	if !strings.Contains(out, "Blocker") {
		test.Fatalf("expected source title 'Blocker' in relations, got:\n%s", out)
	}
}

func TestRunInfo_JSON_IncludesRelations(test *testing.T) {
	app, taskSvc := testApp(test)
	ctx := context.Background()

	src := &domain.Task{Title: "Source"}
	taskSvc.Create(ctx, src)
	tgt := &domain.Task{Title: "Target"}
	taskSvc.Create(ctx, tgt)

	app.root.SetArgs([]string{"task", "link", src.ShortID, "relates_to", tgt.ShortID})
	app.root.Execute()

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"task", "get", src.ShortID, "--format", "json"})
	if err := app.root.Execute(); err != nil {
		test.Fatalf("info json: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, `"relations"`) {
		test.Fatalf("expected relations in JSON, got:\n%s", out)
	}
	if !strings.Contains(out, `"relation_type"`) {
		test.Fatalf("expected relation_type in JSON, got:\n%s", out)
	}
	if !strings.Contains(out, `"related_short_id"`) {
		test.Fatalf("expected related_short_id in JSON, got:\n%s", out)
	}
	if !strings.Contains(out, `"related_title"`) {
		test.Fatalf("expected related_title in JSON, got:\n%s", out)
	}
	if !strings.Contains(out, `"direction_label"`) {
		test.Fatalf("expected direction_label in JSON, got:\n%s", out)
	}
}

func TestRunTree_Empty(test *testing.T) {
	app, _ := testApp(test)

	var stdout, stderr bytes.Buffer
	app.root.SetOut(&stdout)
	app.root.SetErr(&stderr)
	app.root.SetArgs([]string{"task", "tree"})
	if err := app.root.Execute(); err != nil {
		test.Fatalf("tree: %v", err)
	}
	if stdout.String() != "" {
		test.Fatalf("expected empty stdout, got %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "No tasks.") {
		test.Fatalf("expected 'No tasks.' on stderr, got %q", stderr.String())
	}
}

func TestRunTree_WithHierarchy(test *testing.T) {
	app, taskSvc := testApp(test)
	ctx := context.Background()

	parent := &domain.Task{Title: "Parent task"}
	if err := taskSvc.Create(ctx, parent); err != nil {
		test.Fatalf("Create parent= %v", err)
	}
	child := &domain.Task{Title: "Child task", ParentID: &parent.ID}
	if err := taskSvc.Create(ctx, child); err != nil {
		test.Fatalf("Create child: %v", err)
	}

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"task", "tree"})
	if err := app.root.Execute(); err != nil {
		test.Fatalf("tree: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, parent.ShortID) {
		test.Fatalf("expected parent short_id in output, got:\n%s", output)
	}
	if !strings.Contains(output, "  "+child.ShortID) {
		test.Fatalf("expected child with indent in output, got:\n%s", output)
	}
}

func TestRunTree_Subtree(test *testing.T) {
	app, taskSvc := testApp(test)
	ctx := context.Background()

	rootA := &domain.Task{Title: "Root A"}
	if err := taskSvc.Create(ctx, rootA); err != nil {
		test.Fatalf("Create rootA: %v", err)
	}
	childA := &domain.Task{Title: "Child of A", ParentID: &rootA.ID}
	if err := taskSvc.Create(ctx, childA); err != nil {
		test.Fatalf("Create childA: %v", err)
	}

	rootB := &domain.Task{Title: "Root B"}
	if err := taskSvc.Create(ctx, rootB); err != nil {
		test.Fatalf("Create rootB: %v", err)
	}

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"task", "tree", rootA.ShortID})
	if err := app.root.Execute(); err != nil {
		test.Fatalf("tree subtree: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, rootA.ShortID) {
		test.Fatalf("expected rootA in subtree output, got:\n%s", output)
	}
	if !strings.Contains(output, childA.ShortID) {
		test.Fatalf("expected childA in subtree output, got:\n%s", output)
	}
	if strings.Contains(output, rootB.ShortID) {
		test.Fatalf("rootB should not appear in subtree of rootA, got:\n%s", output)
	}
}

func TestRunTree_JSON(test *testing.T) {
	app, taskSvc := testApp(test)
	app.format = "json"
	ctx := context.Background()

	parent := &domain.Task{Title: "JSON Parent"}
	if err := taskSvc.Create(ctx, parent); err != nil {
		test.Fatalf("Create parent= %v", err)
	}
	child := &domain.Task{Title: "JSON Child", ParentID: &parent.ID}
	if err := taskSvc.Create(ctx, child); err != nil {
		test.Fatalf("Create child: %v", err)
	}

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"task", "tree"})
	if err := app.root.Execute(); err != nil {
		test.Fatalf("tree: %v", err)
	}

	var parsed []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		test.Fatalf("JSON unmarshal: %v\nraw: %s", err, buf.String())
	}
	if len(parsed) != 1 {
		test.Fatalf("expected 1 root, got %d", len(parsed))
	}

	root := parsed[0]
	// Verify all task fields are present (matching taskJSON in render.go)
	for _, field := range []string{"id", "short_id", "title", "description", "status", "priority", "order", "version", "created_at", "modified_at", "children"} {
		if _, ok := root[field]; !ok {
			test.Fatalf("expected field %q in tree JSON, got keys: %v", field, root)
		}
	}
	// parent_id should be present and null for root task
	if _, ok := root["parent_id"]; !ok {
		test.Fatal("expected parent_id field in tree JSON (should be null for root)")
	}

	children, ok := root["children"].([]any)
	if !ok {
		test.Fatalf("expected children array")
	}
	if len(children) != 1 {
		test.Fatalf("expected 1 child, got %d", len(children))
	}
}

func TestRunTree_AllFlag(test *testing.T) {
	app, taskSvc := testApp(test)
	ctx := context.Background()

	alive := &domain.Task{Title: "Alive task"}
	if err := taskSvc.Create(ctx, alive); err != nil {
		test.Fatalf("Create alive: %v", err)
	}

	doomed := &domain.Task{Title: "Doomed task"}
	if err := taskSvc.Create(ctx, doomed); err != nil {
		test.Fatalf("Create doomed: %v", err)
	}
	if _, err := taskSvc.Delete(ctx, doomed.ShortID, doomed.Version); err != nil {
		test.Fatalf("Delete doomed: %v", err)
	}

	// Without --all, deleted task should not appear
	var buf1 bytes.Buffer
	app.root.SetOut(&buf1)
	app.root.SetArgs([]string{"task", "tree"})
	if err := app.root.Execute(); err != nil {
		test.Fatalf("tree: %v", err)
	}
	if strings.Contains(buf1.String(), doomed.ShortID) {
		test.Fatalf("deleted task should not appear without --all:\n%s", buf1.String())
	}

	// With --all, deleted task should appear
	var buf2 bytes.Buffer
	app.root.SetOut(&buf2)
	app.root.SetArgs([]string{"task", "tree", "--all"})
	if err := app.root.Execute(); err != nil {
		test.Fatalf("tree --all: %v", err)
	}
	if !strings.Contains(buf2.String(), doomed.ShortID) {
		test.Fatalf("deleted task should appear with --all:\n%s", buf2.String())
	}
}

func mustParseTime(test *testing.T, dateStr string) time.Time {
	test.Helper()
	parsed, parseErr := time.Parse("2006-01-02", dateStr)

	if parseErr != nil {
		test.Fatalf("mustParseTime: %v", parseErr)
	}

	return parsed
}

func TestRunConfigShow_TextIncludesDBSections(test *testing.T) {
	app, _ := testApp(test)

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetErr(&buf)
	app.root.SetArgs([]string{"config", "show"})
	if err := app.root.Execute(); err != nil {
		test.Fatalf("config show: %v", err)
	}

	out := buf.String()
	for _, want := range []string{
		"[storage]",
		"[urgency]",
		"# workflows (from database",
		"[workflows.kanban.statuses.pending]",
		`roles = ["initial"]`,
		"# projects (from database",
		"[projects.default]",
		`workflow = "kanban"`,
	} {
		if !strings.Contains(out, want) {
			test.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestRunConfigShow_JSONIncludesDBSections(test *testing.T) {
	app, _ := testApp(test)
	app.format = "json"

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetErr(&buf)
	app.root.SetArgs([]string{"--format", "json", "config", "show"})
	if err := app.root.Execute(); err != nil {
		test.Fatalf("config show json: %v", err)
	}

	var payload map[string]json.RawMessage
	if err := json.Unmarshal(buf.Bytes(), &payload); err != nil {
		test.Fatalf("invalid json: %v\n%s", err, buf.String())
	}
	for _, key := range []string{"storage", "urgency", "tui", "mcp", "projects", "workflows"} {
		if _, ok := payload[key]; !ok {
			test.Errorf("missing top-level key %q in json:\n%s", key, buf.String())
		}
	}

	var workflows map[string]configWorkflowView
	if err := json.Unmarshal(payload["workflows"], &workflows); err != nil {
		test.Fatalf("workflows decode: %v", err)
	}
	if _, ok := workflows["kanban"]; !ok {
		test.Fatalf("expected kanban workflow in json output: %s", buf.String())
	}

	var projects map[string]configProjectView
	if err := json.Unmarshal(payload["projects"], &projects); err != nil {
		test.Fatalf("projects decode: %v", err)
	}
	defaultProject, ok := projects["default"]
	if !ok {
		test.Fatalf("expected default project in json output: %s", buf.String())
	}
	if defaultProject.Workflow != "kanban" {
		test.Fatalf("default.workflow = %q, want %q", defaultProject.Workflow, "kanban")
	}
}

func TestRunConfigGet_ProjectWorkflow(test *testing.T) {
	app, _ := testApp(test)

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetErr(&buf)
	app.root.SetArgs([]string{"config", "get", "projects.default.workflow"})
	if err := app.root.Execute(); err != nil {
		test.Fatalf("config get: %v", err)
	}
	if strings.TrimSpace(buf.String()) != "kanban" {
		test.Fatalf("expected kanban, got %q", buf.String())
	}
}

func TestRunConfigGet_WorkflowStatusRoles(test *testing.T) {
	app, _ := testApp(test)

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetErr(&buf)
	app.root.SetArgs([]string{"config", "get", "workflows.kanban.statuses.pending.roles"})
	if err := app.root.Execute(); err != nil {
		test.Fatalf("config get: %v", err)
	}
	var roles []string
	if err := json.Unmarshal(buf.Bytes(), &roles); err != nil {
		test.Fatalf("expected JSON array, got %q (%v)", buf.String(), err)
	}
	if len(roles) != 1 || roles[0] != "initial" {
		test.Fatalf("unexpected roles: %v", roles)
	}
}

func TestRunConfigGet_UnsetUrgencyLeaf(test *testing.T) {
	app, _ := testApp(test)

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetErr(&buf)
	app.root.SetArgs([]string{"config", "get", "projects.default.settings.urgency.blocking_weight"})
	if err := app.root.Execute(); err != nil {
		test.Fatalf("config get: %v", err)
	}
	// Unset pointer → nil → blank line in text mode.
	if strings.TrimSpace(buf.String()) != "" {
		test.Fatalf("expected blank output for unset urgency leaf, got %q", buf.String())
	}
}

func TestRunConfigGet_UnknownProject(test *testing.T) {
	app, _ := testApp(test)

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetErr(&buf)
	app.root.SetArgs([]string{"config", "get", "projects.ghost.workflow"})
	err := app.root.Execute()
	if err == nil {
		test.Fatalf("expected error for unknown project, got nil")
	}
	if !strings.Contains(err.Error(), "unknown config key") {
		test.Fatalf("unexpected error: %v", err)
	}
}

func TestRunConfigSet_RejectsProjects(test *testing.T) {
	app, _ := testApp(test)

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetErr(&buf)
	app.root.SetArgs([]string{"config", "set", "projects.foo.workflow", "kanban"})
	err := app.root.Execute()
	if err == nil {
		test.Fatalf("expected error, got nil")
	}
	const want = "projects.* is managed by the database — use `tusk project modify` instead"
	if err.Error() != want {
		test.Fatalf("unexpected error:\n got: %q\nwant: %q", err.Error(), want)
	}
}

func TestRunConfigSet_RejectsWorkflows(test *testing.T) {
	app, _ := testApp(test)

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetErr(&buf)
	app.root.SetArgs([]string{"config", "set", "workflows.foo.statuses.x.roles", "initial"})
	err := app.root.Execute()
	if err == nil {
		test.Fatalf("expected error, got nil")
	}
	const want = "workflows.* is managed by the database — use `tusk workflow modify` instead"
	if err.Error() != want {
		test.Fatalf("unexpected error:\n got: %q\nwant: %q", err.Error(), want)
	}
}
