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
	"github.com/germanamz/tusk/inmem"
	"github.com/germanamz/tusk/migrations"
	"github.com/germanamz/tusk/service"
	"github.com/germanamz/tusk/sqlite"
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
	if !strings.Contains(got, "pending") || !strings.Contains(got, "completed") {
		t.Fatalf("expected transition details in error, got %q", got)
	}
	if !strings.Contains(got, "not allowed") {
		t.Fatalf("expected 'not allowed' in error, got %q", got)
	}
}

func TestFormatError_CyclicParent(t *testing.T) {
	err := fmt.Errorf("setting parent= %w", domain.ErrCyclicParent)
	got := formatError(err, "abc12345")
	want := "parent would create a cycle in task hierarchy"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
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
	projectRepo := inmem.NewProjectRepository(map[string]config.ProjectConfig{"default": {Workflow: "kanban"}})
	workflowRepo := inmem.NewWorkflowRepository(map[string]config.WorkflowConfig{
		"kanban": {
			Statuses: map[string]config.StatusConfig{
				"pending":   {Roles: []string{config.RoleInitial}},
				"active":    {Roles: []string{config.RoleStart, config.RoleHighlight}},
				"completed": {Roles: []string{config.RoleTerminal, config.RoleDone, config.RoleDim}},
				"deleted":   {Roles: []string{config.RoleTerminal, config.RoleDelete, config.RoleDim}},
			},
			Transitions: []config.WorkflowTransitionConfig{
				{From: "pending", To: "active"},
				{From: "pending", To: "deleted"},
				{From: "active", To: "completed"},
				{From: "active", To: "pending"},
				{From: "active", To: "deleted"},
				{From: "completed", To: "pending"},
			},
		},
	})

	tagRepo := sqlite.NewTagRepo(db)
	relationRepo := sqlite.NewRelationRepo(db)

	workflowSvc := service.NewWorkflowService(workflowRepo, projectRepo)
	taskSvc := service.NewTaskService(taskRepo, annotationRepo, relationRepo, tagRepo, projectRepo, workflowSvc, store, nil, nil)
	tagSvc := service.NewTagService(tagRepo)
	relationSvc := service.NewRelationService(relationRepo, taskRepo, store)

	projectSvc := service.NewProjectService(projectRepo)
	app := New(taskSvc, tagSvc, relationSvc, projectSvc, workflowSvc, nil, VersionInfo{}, config.TUIConfig{}, config.MCPConfig{}, nil)
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
	taskSvc.Start(ctx, task.ShortID, 1, "")
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
	app.root.SetArgs([]string{"list", "status=completed"})
	if err := app.root.Execute(); err != nil {
		t.Fatalf("list status=completed: %v", err)
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
	app.root.SetArgs([]string{"add", "Priority", "task", "priority=high"})
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
	app.root.SetArgs([]string{"add", "Due", "task", "due=2026-04-10"})
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
		t.Fatalf("Create parent= %v", err)
	}

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"add", "Child", "task", "parent=" + parent.ShortID})
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

func TestRunAdd_Tags(t *testing.T) {
	app, _ := testApp(t)

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"add", "Tagged", "task", "+api"})
	if err := app.root.Execute(); err != nil {
		t.Fatalf("add with tag: %v", err)
	}
}

func TestRunAdd_NoTitle(t *testing.T) {
	app, _ := testApp(t)

	// Only key=value args, no title words
	app.root.SetArgs([]string{"add", "priority=3"})
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
	app.root.SetArgs([]string{"modify", task.ShortID, "Updated"})
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
	app.root.SetArgs([]string{"modify", task.ShortID, "priority=urgent"})
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

	app.root.SetArgs([]string{"modify", "nonexist", "Nope"})
	err := app.root.Execute()
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected 'not found', got %q", err.Error())
	}
}

func TestRunModify_Tags(t *testing.T) {
	app, taskSvc := testApp(t)
	ctx := context.Background()

	task := &domain.Task{Title: "Tag test"}
	taskSvc.Create(ctx, task)

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"modify", task.ShortID, "+api"})
	if err := app.root.Execute(); err != nil {
		t.Fatalf("modify with tag: %v", err)
	}
}

func TestRunStart_HappyPath(t *testing.T) {
	app, taskSvc := testApp(t)
	ctx := context.Background()

	task := &domain.Task{Title: "Start me"}
	taskSvc.Create(ctx, task)

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"start", task.ShortID})
	if err := app.root.Execute(); err != nil {
		t.Fatalf("start: %v", err)
	}

	out := strings.TrimSpace(buf.String())
	if out != "Started task "+task.ShortID {
		t.Fatalf("expected 'Started task %s', got %q", task.ShortID, out)
	}

	got, _ := taskSvc.GetByShortID(ctx, task.ShortID)
	if got.Status != "active" {
		t.Fatalf("expected active, got %q", got.Status)
	}
}

func TestRunStart_NotFound(t *testing.T) {
	app, _ := testApp(t)

	app.root.SetArgs([]string{"start", "nonexist"})
	err := app.root.Execute()
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected 'not found' error, got %v", err)
	}
}

func TestRunDone_HappyPath(t *testing.T) {
	app, taskSvc := testApp(t)
	ctx := context.Background()

	task := &domain.Task{Title: "Complete me"}
	taskSvc.Create(ctx, task)
	taskSvc.Start(ctx, task.ShortID, 1, "")

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"done", task.ShortID})
	if err := app.root.Execute(); err != nil {
		t.Fatalf("done: %v", err)
	}

	out := strings.TrimSpace(buf.String())
	if out != "Completed task "+task.ShortID {
		t.Fatalf("expected 'Completed task %s', got %q", task.ShortID, out)
	}

	got, _ := taskSvc.GetByShortID(ctx, task.ShortID)
	if got.Status != "completed" {
		t.Fatalf("expected completed, got %q", got.Status)
	}
}

func TestRunDone_FromPending(t *testing.T) {
	app, taskSvc := testApp(t)
	ctx := context.Background()

	task := &domain.Task{Title: "Skip start"}
	taskSvc.Create(ctx, task)

	app.root.SetArgs([]string{"done", task.ShortID})
	err := app.root.Execute()
	if err == nil {
		t.Fatal("expected error for invalid transition")
	}
}

func TestRunDelete_HappyPath(t *testing.T) {
	app, taskSvc := testApp(t)
	ctx := context.Background()

	task := &domain.Task{Title: "Delete me"}
	taskSvc.Create(ctx, task)

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"delete", task.ShortID})
	if err := app.root.Execute(); err != nil {
		t.Fatalf("delete: %v", err)
	}

	out := strings.TrimSpace(buf.String())
	if out != "Deleted task "+task.ShortID {
		t.Fatalf("expected 'Deleted task %s', got %q", task.ShortID, out)
	}

	got, _ := taskSvc.GetByShortID(ctx, task.ShortID)
	if got.Status != "deleted" {
		t.Fatalf("expected deleted, got %q", got.Status)
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
	out := buf.String()
	if !strings.Contains(out, `"short_id"`) {
		t.Fatalf("expected task JSON with short_id, got:\n%s", out)
	}
	if !strings.Contains(out, `"version"`) {
		t.Fatalf("expected task JSON with version, got:\n%s", out)
	}
}

func TestRunModify_DueDate(t *testing.T) {
	app, taskSvc := testApp(t)
	ctx := context.Background()

	task := &domain.Task{Title: "Due test"}
	taskSvc.Create(ctx, task)

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"modify", task.ShortID, "due=2026-04-15"})
	if err := app.root.Execute(); err != nil {
		t.Fatalf("modify due= %v", err)
	}

	got, _ := taskSvc.GetByShortID(ctx, task.ShortID)
	if got.DueAt == nil {
		t.Fatal("expected DueAt to be set")
	}
	if got.DueAt.Format("2006-01-02") != "2026-04-15" {
		t.Fatalf("expected due 2026-04-15, got %s", got.DueAt.Format("2006-01-02"))
	}
}

func TestRunModify_ClearDueDate(t *testing.T) {
	app, taskSvc := testApp(t)
	ctx := context.Background()

	due := mustParseTime(t, "2026-04-15")
	task := &domain.Task{Title: "Clear due", DueAt: &due}
	taskSvc.Create(ctx, task)

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"modify", task.ShortID, "due="})
	if err := app.root.Execute(); err != nil {
		t.Fatalf("modify due clear: %v", err)
	}

	got, _ := taskSvc.GetByShortID(ctx, task.ShortID)
	if got.DueAt != nil {
		t.Fatal("expected DueAt to be cleared")
	}
}

func TestRunModify_Parent(t *testing.T) {
	app, taskSvc := testApp(t)
	ctx := context.Background()

	parent := &domain.Task{Title: "Parent"}
	taskSvc.Create(ctx, parent)
	child := &domain.Task{Title: "Child"}
	taskSvc.Create(ctx, child)

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"modify", child.ShortID, "parent=" + parent.ShortID})
	if err := app.root.Execute(); err != nil {
		t.Fatalf("modify parent= %v", err)
	}

	got, _ := taskSvc.GetByShortID(ctx, child.ShortID)
	if got.ParentID == nil || *got.ParentID != parent.ID {
		t.Fatal("expected parent to be set")
	}
}

func TestRunModify_ClearParent(t *testing.T) {
	app, taskSvc := testApp(t)
	ctx := context.Background()

	parent := &domain.Task{Title: "Parent"}
	taskSvc.Create(ctx, parent)
	child := &domain.Task{Title: "Child", ParentID: &parent.ID}
	taskSvc.Create(ctx, child)

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"modify", child.ShortID, "parent="})
	if err := app.root.Execute(); err != nil {
		t.Fatalf("modify clear parent= %v", err)
	}

	got, _ := taskSvc.GetByShortID(ctx, child.ShortID)
	if got.ParentID != nil {
		t.Fatal("expected parent to be cleared")
	}
}

func TestRunModify_Project(t *testing.T) {
	app, taskSvc := testApp(t)
	ctx := context.Background()

	task := &domain.Task{Title: "Project test"}
	taskSvc.Create(ctx, task)

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"modify", task.ShortID, "project=default"})
	if err := app.root.Execute(); err != nil {
		t.Fatalf("modify project: %v", err)
	}

	out := strings.TrimSpace(buf.String())
	if !strings.Contains(out, "Modified task") {
		t.Fatalf("expected 'Modified task', got %q", out)
	}
}

func TestRunList_ParentFilter(t *testing.T) {
	app, taskSvc := testApp(t)
	ctx := context.Background()

	parent := &domain.Task{Title: "Parent"}
	taskSvc.Create(ctx, parent)
	child := &domain.Task{Title: "Child of parent", ParentID: &parent.ID}
	taskSvc.Create(ctx, child)
	other := &domain.Task{Title: "Unrelated task"}
	taskSvc.Create(ctx, other)

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"list", "parent=" + parent.ShortID})
	if err := app.root.Execute(); err != nil {
		t.Fatalf("list parent= %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, child.ShortID) {
		t.Fatalf("expected child in output, got:\n%s", out)
	}
	if strings.Contains(out, other.ShortID) {
		t.Fatalf("expected unrelated task to be excluded, got:\n%s", out)
	}
}

func TestRunList_PriorityFilter(t *testing.T) {
	app, taskSvc := testApp(t)
	ctx := context.Background()

	low := &domain.Task{Title: "Low pri", Priority: 1}
	taskSvc.Create(ctx, low)
	high := &domain.Task{Title: "High pri", Priority: 3}
	taskSvc.Create(ctx, high)
	urgent := &domain.Task{Title: "Urgent pri", Priority: 4}
	taskSvc.Create(ctx, urgent)

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"list", "priority=3..4"})
	if err := app.root.Execute(); err != nil {
		t.Fatalf("list priority: %v", err)
	}

	out := buf.String()
	if strings.Contains(out, low.ShortID) {
		t.Fatalf("expected low priority task to be excluded, got:\n%s", out)
	}
	if !strings.Contains(out, high.ShortID) {
		t.Fatalf("expected high priority task in output, got:\n%s", out)
	}
	if !strings.Contains(out, urgent.ShortID) {
		t.Fatalf("expected urgent priority task in output, got:\n%s", out)
	}
}

func TestRunInfo_ShowsProjectName(t *testing.T) {
	app, taskSvc := testApp(t)
	ctx := context.Background()

	task := &domain.Task{Title: "Project display test"}
	taskSvc.Create(ctx, task)

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"info", task.ShortID})
	if err := app.root.Execute(); err != nil {
		t.Fatalf("info: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "default") {
		t.Fatalf("expected project name 'default' in output, got:\n%s", out)
	}
}

func TestRunInfo_JSON_IncludesAnnotations(t *testing.T) {
	app, taskSvc := testApp(t)
	ctx := context.Background()

	task := &domain.Task{Title: "Annotated task"}
	taskSvc.Create(ctx, task)
	taskSvc.Annotate(ctx, task.ShortID, "Important note")

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"info", task.ShortID, "--format", "json"})
	if err := app.root.Execute(); err != nil {
		t.Fatalf("info json: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, `"annotations"`) {
		t.Fatalf("expected annotations in JSON output, got:\n%s", out)
	}
	if !strings.Contains(out, "Important note") {
		t.Fatalf("expected annotation body in JSON output, got:\n%s", out)
	}
}

func TestRunLink_HappyPath(t *testing.T) {
	app, taskSvc := testApp(t)
	ctx := context.Background()

	src := &domain.Task{Title: "Source"}
	taskSvc.Create(ctx, src)
	tgt := &domain.Task{Title: "Target"}
	taskSvc.Create(ctx, tgt)

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"link", src.ShortID, "blocks", tgt.ShortID})
	if err := app.root.Execute(); err != nil {
		t.Fatalf("link: %v", err)
	}

	out := strings.TrimSpace(buf.String())
	if !strings.Contains(out, "Linked") {
		t.Fatalf("expected 'Linked' in output, got %q", out)
	}
	if !strings.Contains(out, src.ShortID) || !strings.Contains(out, tgt.ShortID) {
		t.Fatalf("expected both short IDs in output, got %q", out)
	}
}

func TestRunLink_JSON(t *testing.T) {
	app, taskSvc := testApp(t)
	ctx := context.Background()

	src := &domain.Task{Title: "Source"}
	taskSvc.Create(ctx, src)
	tgt := &domain.Task{Title: "Target"}
	taskSvc.Create(ctx, tgt)

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"link", src.ShortID, "relates_to", tgt.ShortID, "--format", "json"})
	if err := app.root.Execute(); err != nil {
		t.Fatalf("link json: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, `"source_id"`) {
		t.Fatalf("expected source_id in JSON, got:\n%s", out)
	}
	if !strings.Contains(out, `"relation_type"`) {
		t.Fatalf("expected relation_type in JSON, got:\n%s", out)
	}
}

func TestRunLink_DuplicateRelation(t *testing.T) {
	app, taskSvc := testApp(t)
	ctx := context.Background()

	src := &domain.Task{Title: "Source"}
	taskSvc.Create(ctx, src)
	tgt := &domain.Task{Title: "Target"}
	taskSvc.Create(ctx, tgt)

	// First link succeeds
	app.root.SetArgs([]string{"link", src.ShortID, "blocks", tgt.ShortID})
	if err := app.root.Execute(); err != nil {
		t.Fatalf("first link: %v", err)
	}

	// Second link should fail
	app.root.SetArgs([]string{"link", src.ShortID, "blocks", tgt.ShortID})
	err := app.root.Execute()
	if err == nil {
		t.Fatal("expected error for duplicate relation")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected 'already exists', got %q", err.Error())
	}
}

func TestRunLink_NotFound(t *testing.T) {
	app, _ := testApp(t)

	app.root.SetArgs([]string{"link", "nonexist", "blocks", "also_non"})
	err := app.root.Execute()
	if err == nil || !strings.Contains(err.Error(), "Source task not found: nonexist") {
		t.Fatalf("expected 'Source task not found: nonexist' error, got %v", err)
	}
}

func TestRunLink_TargetNotFound(t *testing.T) {
	app, taskSvc := testApp(t)
	ctx := context.Background()

	src := &domain.Task{Title: "Exists"}
	taskSvc.Create(ctx, src)

	app.root.SetArgs([]string{"link", src.ShortID, "blocks", "nonexist"})
	err := app.root.Execute()
	if err == nil || !strings.Contains(err.Error(), "Target task not found: nonexist") {
		t.Fatalf("expected 'Target task not found: nonexist' error, got %v", err)
	}
}

func TestRunUnlink_HappyPath(t *testing.T) {
	app, taskSvc := testApp(t)
	ctx := context.Background()

	src := &domain.Task{Title: "Source"}
	taskSvc.Create(ctx, src)
	tgt := &domain.Task{Title: "Target"}
	taskSvc.Create(ctx, tgt)

	// Link first
	app.root.SetArgs([]string{"link", src.ShortID, "blocks", tgt.ShortID})
	if err := app.root.Execute(); err != nil {
		t.Fatalf("link: %v", err)
	}

	// Unlink
	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"unlink", src.ShortID, "blocks", tgt.ShortID})
	if err := app.root.Execute(); err != nil {
		t.Fatalf("unlink: %v", err)
	}

	out := strings.TrimSpace(buf.String())
	if !strings.Contains(out, "Unlinked") {
		t.Fatalf("expected 'Unlinked' in output, got %q", out)
	}
}

func TestRunUnlink_JSON(t *testing.T) {
	app, taskSvc := testApp(t)
	ctx := context.Background()

	src := &domain.Task{Title: "Source"}
	taskSvc.Create(ctx, src)
	tgt := &domain.Task{Title: "Target"}
	taskSvc.Create(ctx, tgt)

	app.root.SetArgs([]string{"link", src.ShortID, "blocks", tgt.ShortID})
	app.root.Execute()

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"unlink", src.ShortID, "blocks", tgt.ShortID, "--format", "json"})
	if err := app.root.Execute(); err != nil {
		t.Fatalf("unlink json: %v", err)
	}

	out := strings.TrimSpace(buf.String())
	if out != "{}" {
		t.Fatalf("expected '{}', got %q", out)
	}
}

func TestRunUnlink_NotFound(t *testing.T) {
	app, _ := testApp(t)

	app.root.SetArgs([]string{"unlink", "nonexist", "blocks", "also_non"})
	err := app.root.Execute()
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected 'not found' error, got %v", err)
	}
}

func TestRunInfo_ShowsRelations(t *testing.T) {
	app, taskSvc := testApp(t)
	ctx := context.Background()

	src := &domain.Task{Title: "Blocker"}
	taskSvc.Create(ctx, src)
	tgt := &domain.Task{Title: "Blocked"}
	taskSvc.Create(ctx, tgt)

	app.root.SetArgs([]string{"link", src.ShortID, "blocks", tgt.ShortID})
	if err := app.root.Execute(); err != nil {
		t.Fatalf("link: %v", err)
	}

	// Info on source should show "blocks"
	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"info", src.ShortID})
	if err := app.root.Execute(); err != nil {
		t.Fatalf("info source: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "Relations:") {
		t.Fatalf("expected Relations: section, got:\n%s", out)
	}
	if !strings.Contains(out, "blocks") {
		t.Fatalf("expected 'blocks' label, got:\n%s", out)
	}
	// Verify related task short ID and title are shown
	if !strings.Contains(out, tgt.ShortID) {
		t.Fatalf("expected target short ID %q in relations, got:\n%s", tgt.ShortID, out)
	}
	if !strings.Contains(out, "Blocked") {
		t.Fatalf("expected target title 'Blocked' in relations, got:\n%s", out)
	}

	// Info on target should show "blocked_by"
	buf.Reset()
	app.root.SetArgs([]string{"info", tgt.ShortID})
	if err := app.root.Execute(); err != nil {
		t.Fatalf("info target: %v", err)
	}

	out = buf.String()
	if !strings.Contains(out, "blocked_by") {
		t.Fatalf("expected 'blocked_by' label, got:\n%s", out)
	}
	// Verify source task short ID and title shown for inverse relation
	if !strings.Contains(out, src.ShortID) {
		t.Fatalf("expected source short ID %q in relations, got:\n%s", src.ShortID, out)
	}
	if !strings.Contains(out, "Blocker") {
		t.Fatalf("expected source title 'Blocker' in relations, got:\n%s", out)
	}
}

func TestRunInfo_JSON_IncludesRelations(t *testing.T) {
	app, taskSvc := testApp(t)
	ctx := context.Background()

	src := &domain.Task{Title: "Source"}
	taskSvc.Create(ctx, src)
	tgt := &domain.Task{Title: "Target"}
	taskSvc.Create(ctx, tgt)

	app.root.SetArgs([]string{"link", src.ShortID, "relates_to", tgt.ShortID})
	app.root.Execute()

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"info", src.ShortID, "--format", "json"})
	if err := app.root.Execute(); err != nil {
		t.Fatalf("info json: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, `"relations"`) {
		t.Fatalf("expected relations in JSON, got:\n%s", out)
	}
	if !strings.Contains(out, `"relation_type"`) {
		t.Fatalf("expected relation_type in JSON, got:\n%s", out)
	}
	if !strings.Contains(out, `"related_short_id"`) {
		t.Fatalf("expected related_short_id in JSON, got:\n%s", out)
	}
	if !strings.Contains(out, `"related_title"`) {
		t.Fatalf("expected related_title in JSON, got:\n%s", out)
	}
	if !strings.Contains(out, `"direction_label"`) {
		t.Fatalf("expected direction_label in JSON, got:\n%s", out)
	}
}

func TestRunTree_Empty(t *testing.T) {
	app, _ := testApp(t)

	var stdout, stderr bytes.Buffer
	app.root.SetOut(&stdout)
	app.root.SetErr(&stderr)
	app.root.SetArgs([]string{"tree"})
	if err := app.root.Execute(); err != nil {
		t.Fatalf("tree: %v", err)
	}
	if stdout.String() != "" {
		t.Fatalf("expected empty stdout, got %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "No tasks.") {
		t.Fatalf("expected 'No tasks.' on stderr, got %q", stderr.String())
	}
}

func TestRunTree_WithHierarchy(t *testing.T) {
	app, taskSvc := testApp(t)
	ctx := context.Background()

	parent := &domain.Task{Title: "Parent task"}
	if err := taskSvc.Create(ctx, parent); err != nil {
		t.Fatalf("Create parent= %v", err)
	}
	child := &domain.Task{Title: "Child task", ParentID: &parent.ID}
	if err := taskSvc.Create(ctx, child); err != nil {
		t.Fatalf("Create child: %v", err)
	}

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"tree"})
	if err := app.root.Execute(); err != nil {
		t.Fatalf("tree: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, parent.ShortID) {
		t.Fatalf("expected parent short_id in output, got:\n%s", output)
	}
	if !strings.Contains(output, "  "+child.ShortID) {
		t.Fatalf("expected child with indent in output, got:\n%s", output)
	}
}

func TestRunTree_Subtree(t *testing.T) {
	app, taskSvc := testApp(t)
	ctx := context.Background()

	rootA := &domain.Task{Title: "Root A"}
	if err := taskSvc.Create(ctx, rootA); err != nil {
		t.Fatalf("Create rootA: %v", err)
	}
	childA := &domain.Task{Title: "Child of A", ParentID: &rootA.ID}
	if err := taskSvc.Create(ctx, childA); err != nil {
		t.Fatalf("Create childA: %v", err)
	}

	rootB := &domain.Task{Title: "Root B"}
	if err := taskSvc.Create(ctx, rootB); err != nil {
		t.Fatalf("Create rootB: %v", err)
	}

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"tree", rootA.ShortID})
	if err := app.root.Execute(); err != nil {
		t.Fatalf("tree subtree: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, rootA.ShortID) {
		t.Fatalf("expected rootA in subtree output, got:\n%s", output)
	}
	if !strings.Contains(output, childA.ShortID) {
		t.Fatalf("expected childA in subtree output, got:\n%s", output)
	}
	if strings.Contains(output, rootB.ShortID) {
		t.Fatalf("rootB should not appear in subtree of rootA, got:\n%s", output)
	}
}

func TestRunTree_JSON(t *testing.T) {
	app, taskSvc := testApp(t)
	app.format = "json"
	ctx := context.Background()

	parent := &domain.Task{Title: "JSON Parent"}
	if err := taskSvc.Create(ctx, parent); err != nil {
		t.Fatalf("Create parent= %v", err)
	}
	child := &domain.Task{Title: "JSON Child", ParentID: &parent.ID}
	if err := taskSvc.Create(ctx, child); err != nil {
		t.Fatalf("Create child: %v", err)
	}

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"tree"})
	if err := app.root.Execute(); err != nil {
		t.Fatalf("tree: %v", err)
	}

	var parsed []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("JSON unmarshal: %v\nraw: %s", err, buf.String())
	}
	if len(parsed) != 1 {
		t.Fatalf("expected 1 root, got %d", len(parsed))
	}

	root := parsed[0]
	// Verify all task fields are present (matching taskJSON in render.go)
	for _, field := range []string{"id", "short_id", "title", "description", "status", "priority", "version", "created_at", "modified_at", "children"} {
		if _, ok := root[field]; !ok {
			t.Fatalf("expected field %q in tree JSON, got keys: %v", field, root)
		}
	}
	// parent_id should be present and null for root task
	if _, ok := root["parent_id"]; !ok {
		t.Fatal("expected parent_id field in tree JSON (should be null for root)")
	}

	children, ok := root["children"].([]any)
	if !ok {
		t.Fatalf("expected children array")
	}
	if len(children) != 1 {
		t.Fatalf("expected 1 child, got %d", len(children))
	}
}

func TestRunTree_AllFlag(t *testing.T) {
	app, taskSvc := testApp(t)
	ctx := context.Background()

	alive := &domain.Task{Title: "Alive task"}
	if err := taskSvc.Create(ctx, alive); err != nil {
		t.Fatalf("Create alive: %v", err)
	}

	doomed := &domain.Task{Title: "Doomed task"}
	if err := taskSvc.Create(ctx, doomed); err != nil {
		t.Fatalf("Create doomed: %v", err)
	}
	if _, err := taskSvc.Delete(ctx, doomed.ShortID, doomed.Version); err != nil {
		t.Fatalf("Delete doomed: %v", err)
	}

	// Without --all, deleted task should not appear
	var buf1 bytes.Buffer
	app.root.SetOut(&buf1)
	app.root.SetArgs([]string{"tree"})
	if err := app.root.Execute(); err != nil {
		t.Fatalf("tree: %v", err)
	}
	if strings.Contains(buf1.String(), doomed.ShortID) {
		t.Fatalf("deleted task should not appear without --all:\n%s", buf1.String())
	}

	// With --all, deleted task should appear
	var buf2 bytes.Buffer
	app.root.SetOut(&buf2)
	app.root.SetArgs([]string{"tree", "--all"})
	if err := app.root.Execute(); err != nil {
		t.Fatalf("tree --all: %v", err)
	}
	if !strings.Contains(buf2.String(), doomed.ShortID) {
		t.Fatalf("deleted task should appear with --all:\n%s", buf2.String())
	}
}

func mustParseTime(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.Parse("2006-01-02", s)
	if err != nil {
		t.Fatalf("mustParseTime: %v", err)
	}
	return v
}
