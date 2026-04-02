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
