package tui

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/germanamz/tusk/internal/domain"
	"github.com/germanamz/tusk/internal/service"
	"github.com/germanamz/tusk/internal/sqlite"
	"github.com/germanamz/tusk/migrations"
)

func TestFormatError_NotFound(t *testing.T) {
	err := fmt.Errorf("getting task: %w", domain.ErrNotFound)
	got := formatError(err, "abc12345")
	want := "Task not found: abc12345"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestFormatError_Conflict(t *testing.T) {
	err := domain.ErrConflict
	got := formatError(err, "abc12345")
	want := "Version conflict - task was modified by another process"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestFormatError_InvalidTransition(t *testing.T) {
	err := fmt.Errorf("transition %q → %q not allowed: %w", "pending", "completed", domain.ErrInvalidTransition)
	got := formatError(err, "abc12345")
	if got != err.Error() {
		t.Fatalf("expected original error message, got %q", got)
	}
}

func TestFormatError_Generic(t *testing.T) {
	err := fmt.Errorf("something went wrong")
	got := formatError(err, "abc12345")
	if got != "something went wrong" {
		t.Fatalf("expected original message, got %q", got)
	}
}

// testApp creates a fully wired App with an in-memory database.
func testApp(t *testing.T) (*App, *service.TaskService) {
	t.Helper()
	store, err := sqlite.New(":memory:", migrations.FS)
	if err != nil {
		t.Fatalf("opening test store: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	db := store.DB()
	taskRepo := sqlite.NewTaskRepo(db)
	annotationRepo := sqlite.NewAnnotationRepo(db)
	projectRepo := sqlite.NewProjectRepo(db)
	workflowRepo := sqlite.NewWorkflowRepo(db)

	workflowSvc := service.NewWorkflowService(workflowRepo)
	taskSvc := service.NewTaskService(taskRepo, annotationRepo, projectRepo, workflowSvc)

	app := New(taskSvc, projectRepo)
	return app, taskSvc
}

func TestRunList_Empty(t *testing.T) {
	app, _ := testApp(t)

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"list"})
	if err := app.root.Execute(); err != nil {
		t.Fatalf("list: %v", err)
	}
	if buf.String() != "" {
		t.Fatalf("expected empty output, got %q", buf.String())
	}
}

func TestRunList_WithTasks(t *testing.T) {
	app, taskSvc := testApp(t)
	ctx := context.Background()

	task := &domain.Task{Title: "Test task", Priority: 3}
	if err := taskSvc.Create(ctx, task); err != nil {
		t.Fatalf("Create: %v", err)
	}

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"list"})
	if err := app.root.Execute(); err != nil {
		t.Fatalf("list: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, task.ShortID) {
		t.Fatalf("expected short ID in output, got:\n%s", out)
	}
	if !strings.Contains(out, "H") {
		t.Fatalf("expected priority H in output, got:\n%s", out)
	}
}

func TestRunList_StatusFilter(t *testing.T) {
	app, taskSvc := testApp(t)
	ctx := context.Background()

	task := &domain.Task{Title: "Completed task"}
	if err := taskSvc.Create(ctx, task); err != nil {
		t.Fatalf("Create: %v", err)
	}
	// Start then complete
	taskSvc.Start(ctx, task.ShortID, 1)
	taskSvc.Complete(ctx, task.ShortID, 2)

	// Default list should NOT show completed tasks
	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"list"})
	if err := app.root.Execute(); err != nil {
		t.Fatalf("list: %v", err)
	}
	if strings.Contains(buf.String(), task.ShortID) {
		t.Fatalf("expected completed task to be hidden from default list")
	}

	// Explicit status filter should show it
	buf.Reset()
	app.root.SetArgs([]string{"list", "status:completed"})
	if err := app.root.Execute(); err != nil {
		t.Fatalf("list status:completed: %v", err)
	}
	if !strings.Contains(buf.String(), task.ShortID) {
		t.Fatalf("expected completed task in filtered list, got:\n%s", buf.String())
	}
}

func TestRunList_JSON(t *testing.T) {
	app, taskSvc := testApp(t)
	ctx := context.Background()

	task := &domain.Task{Title: "JSON task"}
	if err := taskSvc.Create(ctx, task); err != nil {
		t.Fatalf("Create: %v", err)
	}

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"list", "--format", "json"})
	if err := app.root.Execute(); err != nil {
		t.Fatalf("list --format json: %v", err)
	}
	if !strings.Contains(buf.String(), `"short_id"`) {
		t.Fatalf("expected JSON output, got:\n%s", buf.String())
	}
}

func TestRunInfo_HappyPath(t *testing.T) {
	app, taskSvc := testApp(t)
	ctx := context.Background()

	task := &domain.Task{Title: "Info test", Priority: 2}
	if err := taskSvc.Create(ctx, task); err != nil {
		t.Fatalf("Create: %v", err)
	}
	taskSvc.Annotate(ctx, task.ShortID, "A note")

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"info", task.ShortID})
	if err := app.root.Execute(); err != nil {
		t.Fatalf("info: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, task.ShortID) {
		t.Fatalf("expected short ID in output, got:\n%s", out)
	}
	if !strings.Contains(out, "Info test") {
		t.Fatalf("expected title in output, got:\n%s", out)
	}
	if !strings.Contains(out, "medium") {
		t.Fatalf("expected priority name in output, got:\n%s", out)
	}
	if !strings.Contains(out, "A note") {
		t.Fatalf("expected annotation in output, got:\n%s", out)
	}
}

func TestRunInfo_NotFound(t *testing.T) {
	app, _ := testApp(t)

	app.root.SetArgs([]string{"info", "nonexist"})
	err := app.root.Execute()
	if err == nil {
		t.Fatal("expected error for nonexistent task")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected 'not found' in error, got %q", err.Error())
	}
}

func TestRunAdd_HappyPath(t *testing.T) {
	app, _ := testApp(t)

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"add", "My", "new", "task"})
	if err := app.root.Execute(); err != nil {
		t.Fatalf("add: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "Created task") {
		t.Fatalf("expected 'Created task' in output, got %q", out)
	}
}

func TestRunAdd_WithPriority(t *testing.T) {
	app, taskSvc := testApp(t)
	ctx := context.Background()

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"add", "Priority", "task", "priority:high"})
	if err := app.root.Execute(); err != nil {
		t.Fatalf("add: %v", err)
	}

	// Extract short ID from "Created task <id>\n"
	out := strings.TrimSpace(buf.String())
	parts := strings.Fields(out)
	shortID := parts[len(parts)-1]

	task, err := taskSvc.GetByShortID(ctx, shortID)
	if err != nil {
		t.Fatalf("GetByShortID: %v", err)
	}
	if task.Priority != 3 {
		t.Fatalf("expected priority 3, got %d", task.Priority)
	}
}

func TestRunAdd_WithDueDate(t *testing.T) {
	app, taskSvc := testApp(t)
	ctx := context.Background()

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"add", "Due", "task", "due:2026-04-10"})
	if err := app.root.Execute(); err != nil {
		t.Fatalf("add: %v", err)
	}

	out := strings.TrimSpace(buf.String())
	parts := strings.Fields(out)
	shortID := parts[len(parts)-1]

	task, err := taskSvc.GetByShortID(ctx, shortID)
	if err != nil {
		t.Fatalf("GetByShortID: %v", err)
	}
	if task.DueAt == nil {
		t.Fatal("expected DueAt to be set")
	}
	if task.DueAt.Format("2006-01-02") != "2026-04-10" {
		t.Fatalf("expected due 2026-04-10, got %s", task.DueAt.Format("2006-01-02"))
	}
}

func TestRunAdd_WithParent(t *testing.T) {
	app, taskSvc := testApp(t)
	ctx := context.Background()

	parent := &domain.Task{Title: "Parent"}
	if err := taskSvc.Create(ctx, parent); err != nil {
		t.Fatalf("Create parent: %v", err)
	}

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"add", "Child", "task", "parent:" + parent.ShortID})
	if err := app.root.Execute(); err != nil {
		t.Fatalf("add: %v", err)
	}

	out := strings.TrimSpace(buf.String())
	parts := strings.Fields(out)
	shortID := parts[len(parts)-1]

	child, err := taskSvc.GetByShortID(ctx, shortID)
	if err != nil {
		t.Fatalf("GetByShortID: %v", err)
	}
	if child.ParentID == nil || *child.ParentID != parent.ID {
		t.Fatal("expected child to reference parent")
	}
}

func TestRunAdd_TagsError(t *testing.T) {
	app, _ := testApp(t)

	app.root.SetArgs([]string{"add", "Tagged", "task", "+api"})
	err := app.root.Execute()
	if err == nil {
		t.Fatal("expected error for tags")
	}
	if !strings.Contains(err.Error(), "tags not yet supported") {
		t.Fatalf("expected 'tags not yet supported', got %q", err.Error())
	}
}

func TestRunAdd_NoTitle(t *testing.T) {
	app, _ := testApp(t)

	// Only key:value args, no title words
	app.root.SetArgs([]string{"add", "priority:3"})
	err := app.root.Execute()
	if err == nil {
		t.Fatal("expected error for missing title")
	}
}

func TestRunAdd_JSON(t *testing.T) {
	app, _ := testApp(t)

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"add", "JSON", "task", "--format", "json"})
	if err := app.root.Execute(); err != nil {
		t.Fatalf("add --format json: %v", err)
	}
	if !strings.Contains(buf.String(), `"short_id"`) {
		t.Fatalf("expected JSON output, got:\n%s", buf.String())
	}
}

func TestRunInfo_JSON(t *testing.T) {
	app, taskSvc := testApp(t)
	ctx := context.Background()

	task := &domain.Task{Title: "JSON info test"}
	if err := taskSvc.Create(ctx, task); err != nil {
		t.Fatalf("Create: %v", err)
	}

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"info", task.ShortID, "--format", "json"})
	if err := app.root.Execute(); err != nil {
		t.Fatalf("info --format json: %v", err)
	}
	if !strings.Contains(buf.String(), `"short_id"`) {
		t.Fatalf("expected JSON output, got:\n%s", buf.String())
	}
}

func TestRunModify_Title(t *testing.T) {
	app, taskSvc := testApp(t)
	ctx := context.Background()

	task := &domain.Task{Title: "Original"}
	if err := taskSvc.Create(ctx, task); err != nil {
		t.Fatalf("Create: %v", err)
	}

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"modify", task.ShortID, "title:Updated"})
	if err := app.root.Execute(); err != nil {
		t.Fatalf("modify: %v", err)
	}

	got, _ := taskSvc.GetByShortID(ctx, task.ShortID)
	if got.Title != "Updated" {
		t.Fatalf("expected title 'Updated', got %q", got.Title)
	}
	if got.Version != 2 {
		t.Fatalf("expected version 2, got %d", got.Version)
	}
}

func TestRunModify_Priority(t *testing.T) {
	app, taskSvc := testApp(t)
	ctx := context.Background()

	task := &domain.Task{Title: "Modify priority"}
	if err := taskSvc.Create(ctx, task); err != nil {
		t.Fatalf("Create: %v", err)
	}

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"modify", task.ShortID, "priority:urgent"})
	if err := app.root.Execute(); err != nil {
		t.Fatalf("modify: %v", err)
	}

	got, _ := taskSvc.GetByShortID(ctx, task.ShortID)
	if got.Priority != 4 {
		t.Fatalf("expected priority 4, got %d", got.Priority)
	}
}

func TestRunModify_NotFound(t *testing.T) {
	app, _ := testApp(t)

	app.root.SetArgs([]string{"modify", "nonexist", "title:Nope"})
	err := app.root.Execute()
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected 'not found', got %q", err.Error())
	}
}

func TestRunModify_TagsError(t *testing.T) {
	app, taskSvc := testApp(t)
	ctx := context.Background()

	task := &domain.Task{Title: "Tag test"}
	taskSvc.Create(ctx, task)

	app.root.SetArgs([]string{"modify", task.ShortID, "+api"})
	err := app.root.Execute()
	if err == nil {
		t.Fatal("expected error for tags")
	}
	if !strings.Contains(err.Error(), "tags not yet supported") {
		t.Fatalf("expected 'tags not yet supported', got %q", err.Error())
	}
}

func TestRunAnnotate_HappyPath(t *testing.T) {
	app, taskSvc := testApp(t)
	ctx := context.Background()

	task := &domain.Task{Title: "Annotate me"}
	taskSvc.Create(ctx, task)

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"annotate", task.ShortID, "This", "is", "a", "note"})
	if err := app.root.Execute(); err != nil {
		t.Fatalf("annotate: %v", err)
	}

	out := strings.TrimSpace(buf.String())
	if out != "Annotated task "+task.ShortID {
		t.Fatalf("expected 'Annotated task %s', got %q", task.ShortID, out)
	}

	annotations, _ := taskSvc.GetAnnotations(ctx, task.ShortID)
	if len(annotations) != 1 {
		t.Fatalf("expected 1 annotation, got %d", len(annotations))
	}
	if annotations[0].Body != "This is a note" {
		t.Fatalf("expected 'This is a note', got %q", annotations[0].Body)
	}
}

func TestRunAnnotate_NotFound(t *testing.T) {
	app, _ := testApp(t)

	app.root.SetArgs([]string{"annotate", "nonexist", "A", "note"})
	err := app.root.Execute()
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected 'not found' error, got %v", err)
	}
}

func TestRunAnnotate_JSON(t *testing.T) {
	app, taskSvc := testApp(t)
	ctx := context.Background()

	task := &domain.Task{Title: "JSON annotate"}
	taskSvc.Create(ctx, task)

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"annotate", task.ShortID, "A", "note", "--format", "json"})
	if err := app.root.Execute(); err != nil {
		t.Fatalf("annotate --format json: %v", err)
	}
	if !strings.Contains(buf.String(), `"body"`) {
		t.Fatalf("expected JSON output, got:\n%s", buf.String())
	}
}
