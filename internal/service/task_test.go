package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/germanamz/tusk/internal/domain"
	"github.com/germanamz/tusk/internal/sqlite"
	"github.com/germanamz/tusk/migrations"
	"github.com/google/uuid"
)

// testEnv holds all the services and repos needed for TaskService tests.
type testEnv struct {
	taskSvc     *TaskService
	workflowSvc *WorkflowService
	store       *sqlite.Store
}

// testTaskEnv creates a fully wired test environment with an in-memory SQLite DB.
// The DB has all migrations applied, including the _default project and default workflow.
func testTaskEnv(t *testing.T) *testEnv {
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

	workflowSvc := NewWorkflowService(workflowRepo)
	taskSvc := NewTaskService(taskRepo, annotationRepo, projectRepo, workflowSvc)

	return &testEnv{
		taskSvc:     taskSvc,
		workflowSvc: workflowSvc,
		store:       store,
	}
}

// newMinimalTask returns a Task with only the required fields set.
// The service's Create method will fill in ID, ShortID, Version, timestamps,
// and default ProjectID.
func newMinimalTask(title string) *domain.Task {
	return &domain.Task{
		Title: title,
	}
}

// mustCreateTask creates a task through the service or fails the test.
func mustCreateTask(t *testing.T, svc *TaskService, task *domain.Task) {
	t.Helper()
	if err := svc.Create(context.Background(), task); err != nil {
		t.Fatalf("mustCreateTask: %v", err)
	}
}

func TestCreate_HappyPath(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	task := newMinimalTask("My first task")
	if err := env.taskSvc.Create(ctx, task); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Verify the service populated all required fields
	if task.ID == uuid.Nil {
		t.Fatal("expected non-nil ID")
	}
	if task.ShortID == "" {
		t.Fatal("expected non-empty ShortID")
	}
	if len(task.ShortID) < 8 {
		t.Fatalf("expected ShortID length >= 8, got %d", len(task.ShortID))
	}
	if task.Version != 1 {
		t.Fatalf("expected version 1, got %d", task.Version)
	}
	if task.Status != "pending" {
		t.Fatalf("expected status 'pending', got %q", task.Status)
	}
	if task.ProjectID == nil {
		t.Fatal("expected ProjectID to be set to default")
	}
	if *task.ProjectID != defaultProjectID {
		t.Fatalf("expected default project ID, got %s", task.ProjectID)
	}
	if task.CreatedAt.IsZero() {
		t.Fatal("expected CreatedAt to be set")
	}
	if task.ModifiedAt.IsZero() {
		t.Fatal("expected ModifiedAt to be set")
	}
	if task.UDA == nil {
		t.Fatal("expected UDA to be initialized")
	}

	// Verify it's actually persisted by reading it back
	got, err := env.taskSvc.GetByShortID(ctx, task.ShortID)
	if err != nil {
		t.Fatalf("GetByShortID: %v", err)
	}
	if got.Title != "My first task" {
		t.Fatalf("expected title 'My first task', got %q", got.Title)
	}
}

func TestCreate_EmptyTitle(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	task := newMinimalTask("")
	err := env.taskSvc.Create(ctx, task)
	if err == nil {
		t.Fatal("expected error for empty title")
	}
}

func TestCreate_PriorityTooHigh(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	task := newMinimalTask("Bad priority")
	task.Priority = 5
	err := env.taskSvc.Create(ctx, task)
	if err == nil {
		t.Fatal("expected error for priority > 4")
	}
}

func TestCreate_PriorityNegative(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	task := newMinimalTask("Negative priority")
	task.Priority = -1
	err := env.taskSvc.Create(ctx, task)
	if err == nil {
		t.Fatal("expected error for negative priority")
	}
}

func TestCreate_InvalidParent(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	nonexistent := uuid.New()
	task := newMinimalTask("Orphan task")
	task.ParentID = &nonexistent
	err := env.taskSvc.Create(ctx, task)
	if err == nil {
		t.Fatal("expected error for nonexistent parent")
	}
}

func TestCreate_InvalidProject(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	nonexistent := uuid.New()
	task := newMinimalTask("Bad project")
	task.ProjectID = &nonexistent
	err := env.taskSvc.Create(ctx, task)
	if err == nil {
		t.Fatal("expected error for nonexistent project")
	}
}

func TestCreate_InvalidInitialStatus(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	task := newMinimalTask("Bad status")
	task.Status = "nonexistent_status"
	err := env.taskSvc.Create(ctx, task)
	if err == nil {
		t.Fatal("expected error for invalid initial status")
	}
}

func TestCreate_ValidParent(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	parent := newMinimalTask("Parent")
	mustCreateTask(t, env.taskSvc, parent)

	child := newMinimalTask("Child")
	child.ParentID = &parent.ID
	if err := env.taskSvc.Create(ctx, child); err != nil {
		t.Fatalf("Create child: %v", err)
	}
	if child.ParentID == nil || *child.ParentID != parent.ID {
		t.Fatal("expected child to reference parent")
	}
}

func TestCreate_DefaultsToDefaultProject(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	task := newMinimalTask("No project set")
	if err := env.taskSvc.Create(ctx, task); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if task.ProjectID == nil || *task.ProjectID != defaultProjectID {
		t.Fatalf("expected default project, got %v", task.ProjectID)
	}
}

func TestCreate_WithAllFields(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Millisecond)
	due := now.Add(24 * time.Hour)
	wait := now.Add(1 * time.Hour)
	rrule := "FREQ=DAILY;COUNT=5"
	projID := defaultProjectID

	task := &domain.Task{
		Title:          "Full task",
		Description:    "All fields populated",
		Status:         "pending",
		Priority:       3,
		ProjectID:      &projID,
		DueAt:          &due,
		WaitUntil:      &wait,
		RecurrenceRule: &rrule,
		UDA:            map[string]any{"custom": "value"},
	}

	if err := env.taskSvc.Create(ctx, task); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := env.taskSvc.GetByShortID(ctx, task.ShortID)
	if err != nil {
		t.Fatalf("GetByShortID: %v", err)
	}
	if got.Description != "All fields populated" {
		t.Fatalf("expected description preserved, got %q", got.Description)
	}
	if got.Priority != 3 {
		t.Fatalf("expected priority 3, got %d", got.Priority)
	}
	if got.DueAt == nil {
		t.Fatal("expected DueAt to be set")
	}
	if got.RecurrenceRule == nil || *got.RecurrenceRule != rrule {
		t.Fatal("expected RecurrenceRule to be preserved")
	}
}

func TestGetByShortID_Found(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	task := newMinimalTask("Find me")
	mustCreateTask(t, env.taskSvc, task)

	got, err := env.taskSvc.GetByShortID(ctx, task.ShortID)
	if err != nil {
		t.Fatalf("GetByShortID: %v", err)
	}
	if got.Title != "Find me" {
		t.Fatalf("expected 'Find me', got %q", got.Title)
	}
}

func TestGetByShortID_NotFound(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	_, err := env.taskSvc.GetByShortID(ctx, "nonexist")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestGetByID_Found(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	task := newMinimalTask("Get by ID")
	mustCreateTask(t, env.taskSvc, task)

	got, err := env.taskSvc.GetByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Title != "Get by ID" {
		t.Fatalf("expected 'Get by ID', got %q", got.Title)
	}
}

func TestList_Empty(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	tasks, err := env.taskSvc.List(ctx, domain.TaskFilter{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(tasks) != 0 {
		t.Fatalf("expected 0 tasks, got %d", len(tasks))
	}
}

func TestList_WithFilter(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	t1 := newMinimalTask("Task one")
	t1.Priority = 3
	mustCreateTask(t, env.taskSvc, t1)

	t2 := newMinimalTask("Task two")
	t2.Priority = 1
	mustCreateTask(t, env.taskSvc, t2)

	minPri := 3
	tasks, err := env.taskSvc.List(ctx, domain.TaskFilter{PriorityMin: &minPri})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task with priority >= 3, got %d", len(tasks))
	}
	if tasks[0].Title != "Task one" {
		t.Fatalf("expected 'Task one', got %q", tasks[0].Title)
	}
}

func TestGetChildren(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	parent := newMinimalTask("Parent")
	mustCreateTask(t, env.taskSvc, parent)

	child1 := newMinimalTask("Child 1")
	child1.ParentID = &parent.ID
	mustCreateTask(t, env.taskSvc, child1)

	child2 := newMinimalTask("Child 2")
	child2.ParentID = &parent.ID
	mustCreateTask(t, env.taskSvc, child2)

	children, err := env.taskSvc.GetChildren(ctx, parent.ID)
	if err != nil {
		t.Fatalf("GetChildren: %v", err)
	}
	if len(children) != 2 {
		t.Fatalf("expected 2 children, got %d", len(children))
	}
}

func TestGetDescendants(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	root := newMinimalTask("Root")
	mustCreateTask(t, env.taskSvc, root)

	child := newMinimalTask("Child")
	child.ParentID = &root.ID
	mustCreateTask(t, env.taskSvc, child)

	grandchild := newMinimalTask("Grandchild")
	grandchild.ParentID = &child.ID
	mustCreateTask(t, env.taskSvc, grandchild)

	descendants, err := env.taskSvc.GetDescendants(ctx, root.ID)
	if err != nil {
		t.Fatalf("GetDescendants: %v", err)
	}
	if len(descendants) != 2 {
		t.Fatalf("expected 2 descendants, got %d", len(descendants))
	}
}
