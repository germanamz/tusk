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

func TestUpdate_PartialUpdate(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	task := newMinimalTask("Original title")
	task.Priority = 1
	mustCreateTask(t, env.taskSvc, task)

	newTitle := "Updated title"
	updated, err := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID: task.ShortID,
		Version: 1,
		Title:   &newTitle,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Title != "Updated title" {
		t.Fatalf("expected 'Updated title', got %q", updated.Title)
	}
	// Priority should be unchanged
	if updated.Priority != 1 {
		t.Fatalf("expected priority 1 unchanged, got %d", updated.Priority)
	}
	// Version should be bumped
	if updated.Version != 2 {
		t.Fatalf("expected version 2, got %d", updated.Version)
	}
}

func TestUpdate_VersionConflict(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	task := newMinimalTask("Conflict test")
	mustCreateTask(t, env.taskSvc, task)

	newTitle := "First update"
	_, err := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID: task.ShortID,
		Version: 1,
		Title:   &newTitle,
	})
	if err != nil {
		t.Fatalf("first Update: %v", err)
	}

	// Try to update with stale version
	staleTitle := "Stale update"
	_, err = env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID: task.ShortID,
		Version: 1,
		Title:   &staleTitle,
	})
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("expected ErrConflict, got %v", err)
	}
}

func TestUpdate_StatusTransitionAllowed(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	task := newMinimalTask("Transition test")
	mustCreateTask(t, env.taskSvc, task)

	activeStatus := "active"
	updated, err := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID: task.ShortID,
		Version: 1,
		Status:  &activeStatus,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Status != "active" {
		t.Fatalf("expected status 'active', got %q", updated.Status)
	}
}

func TestUpdate_StatusTransitionDisallowed(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	task := newMinimalTask("Bad transition")
	mustCreateTask(t, env.taskSvc, task)

	// pending → completed is not allowed in the default workflow
	completedStatus := "completed"
	_, err := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID: task.ShortID,
		Version: 1,
		Status:  &completedStatus,
	})
	if !errors.Is(err, domain.ErrInvalidTransition) {
		t.Fatalf("expected ErrInvalidTransition, got %v", err)
	}
}

func TestUpdate_EmptyTitleRejected(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	task := newMinimalTask("Will be emptied")
	mustCreateTask(t, env.taskSvc, task)

	emptyTitle := ""
	_, err := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID: task.ShortID,
		Version: 1,
		Title:   &emptyTitle,
	})
	if err == nil {
		t.Fatal("expected error for empty title")
	}
}

func TestUpdate_InvalidPriorityRejected(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	task := newMinimalTask("Bad priority update")
	mustCreateTask(t, env.taskSvc, task)

	badPriority := 5
	_, err := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID:  task.ShortID,
		Version:  1,
		Priority: &badPriority,
	})
	if err == nil {
		t.Fatal("expected error for priority > 4")
	}
}

func TestUpdate_ParentCannotBeSelf(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	task := newMinimalTask("Self parent")
	mustCreateTask(t, env.taskSvc, task)

	selfRef := &task.ID
	_, err := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID:  task.ShortID,
		Version:  1,
		ParentID: &selfRef,
	})
	if err == nil {
		t.Fatal("expected error when setting parent to self")
	}
}

func TestUpdate_ClearNullableField(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Millisecond)
	due := now.Add(24 * time.Hour)
	task := newMinimalTask("Has due date")
	task.DueAt = &due
	mustCreateTask(t, env.taskSvc, task)

	// Clear the due date by setting outer pointer to non-nil, inner to nil
	var nilTime *time.Time
	updated, err := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID: task.ShortID,
		Version: 1,
		DueAt:   &nilTime,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.DueAt != nil {
		t.Fatal("expected DueAt to be cleared")
	}
}

func TestUpdate_NotFound(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	newTitle := "Doesn't matter"
	_, err := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID: "nonexist",
		Version: 1,
		Title:   &newTitle,
	})
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestStart_HappyPath(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	task := newMinimalTask("Start me")
	mustCreateTask(t, env.taskSvc, task)

	updated, err := env.taskSvc.Start(ctx, task.ShortID, 1)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if updated.Status != "active" {
		t.Fatalf("expected status 'active', got %q", updated.Status)
	}
	if updated.Version != 2 {
		t.Fatalf("expected version 2, got %d", updated.Version)
	}
}

func TestStart_AlreadyActive(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	task := newMinimalTask("Already active")
	mustCreateTask(t, env.taskSvc, task)

	_, err := env.taskSvc.Start(ctx, task.ShortID, 1)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	// active → active is a no-op (status unchanged), should succeed
	updated, err := env.taskSvc.Start(ctx, task.ShortID, 2)
	if err != nil {
		t.Fatalf("Start on already-active task: %v", err)
	}
	if updated.Status != "active" {
		t.Fatalf("expected status 'active', got %q", updated.Status)
	}
}

func TestComplete_HappyPath(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	task := newMinimalTask("Complete me")
	mustCreateTask(t, env.taskSvc, task)

	// Must start first: pending → active
	started, err := env.taskSvc.Start(ctx, task.ShortID, 1)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Then complete: active → completed
	completed, err := env.taskSvc.Complete(ctx, task.ShortID, started.Version)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if completed.Status != "completed" {
		t.Fatalf("expected status 'completed', got %q", completed.Status)
	}
}

func TestComplete_FromPending(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	task := newMinimalTask("Skip start")
	mustCreateTask(t, env.taskSvc, task)

	// pending → completed is not allowed
	_, err := env.taskSvc.Complete(ctx, task.ShortID, 1)
	if !errors.Is(err, domain.ErrInvalidTransition) {
		t.Fatalf("expected ErrInvalidTransition, got %v", err)
	}
}

func TestDelete_HappyPath(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	task := newMinimalTask("Delete me")
	mustCreateTask(t, env.taskSvc, task)

	// pending → deleted is allowed
	deleted, err := env.taskSvc.Delete(ctx, task.ShortID, 1)
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if deleted.Status != "deleted" {
		t.Fatalf("expected status 'deleted', got %q", deleted.Status)
	}
}

func TestDelete_FromCompleted(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	task := newMinimalTask("Complete then delete")
	mustCreateTask(t, env.taskSvc, task)

	started, _ := env.taskSvc.Start(ctx, task.ShortID, 1)
	completed, _ := env.taskSvc.Complete(ctx, task.ShortID, started.Version)

	// completed → deleted is not allowed in default workflow
	_, err := env.taskSvc.Delete(ctx, task.ShortID, completed.Version)
	if !errors.Is(err, domain.ErrInvalidTransition) {
		t.Fatalf("expected ErrInvalidTransition, got %v", err)
	}
}

func TestAnnotate_HappyPath(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	task := newMinimalTask("Annotate me")
	mustCreateTask(t, env.taskSvc, task)

	ann, err := env.taskSvc.Annotate(ctx, task.ShortID, "This is a note")
	if err != nil {
		t.Fatalf("Annotate: %v", err)
	}
	if ann.ID == uuid.Nil {
		t.Fatal("expected non-nil annotation ID")
	}
	if ann.TaskID != task.ID {
		t.Fatalf("expected TaskID %s, got %s", task.ID, ann.TaskID)
	}
	if ann.Body != "This is a note" {
		t.Fatalf("expected body 'This is a note', got %q", ann.Body)
	}
	if ann.CreatedAt.IsZero() {
		t.Fatal("expected CreatedAt to be set")
	}
}

func TestAnnotate_EmptyBody(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	task := newMinimalTask("Annotate me")
	mustCreateTask(t, env.taskSvc, task)

	_, err := env.taskSvc.Annotate(ctx, task.ShortID, "")
	if err == nil {
		t.Fatal("expected error for empty annotation body")
	}
}

func TestAnnotate_TaskNotFound(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	_, err := env.taskSvc.Annotate(ctx, "nonexist", "Some note")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestGetAnnotations_WithResults(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	task := newMinimalTask("Has annotations")
	mustCreateTask(t, env.taskSvc, task)

	_, err := env.taskSvc.Annotate(ctx, task.ShortID, "Note 1")
	if err != nil {
		t.Fatalf("Annotate 1: %v", err)
	}
	_, err = env.taskSvc.Annotate(ctx, task.ShortID, "Note 2")
	if err != nil {
		t.Fatalf("Annotate 2: %v", err)
	}

	annotations, err := env.taskSvc.GetAnnotations(ctx, task.ShortID)
	if err != nil {
		t.Fatalf("GetAnnotations: %v", err)
	}
	if len(annotations) != 2 {
		t.Fatalf("expected 2 annotations, got %d", len(annotations))
	}
}

func TestGetAnnotations_Empty(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	task := newMinimalTask("No annotations")
	mustCreateTask(t, env.taskSvc, task)

	annotations, err := env.taskSvc.GetAnnotations(ctx, task.ShortID)
	if err != nil {
		t.Fatalf("GetAnnotations: %v", err)
	}
	if len(annotations) != 0 {
		t.Fatalf("expected 0 annotations, got %d", len(annotations))
	}
}

func TestGetAnnotations_TaskNotFound(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	_, err := env.taskSvc.GetAnnotations(ctx, "nonexist")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestDeleteAnnotation_HappyPath(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	task := newMinimalTask("Delete annotation")
	mustCreateTask(t, env.taskSvc, task)

	ann, err := env.taskSvc.Annotate(ctx, task.ShortID, "To be deleted")
	if err != nil {
		t.Fatalf("Annotate: %v", err)
	}

	if err := env.taskSvc.DeleteAnnotation(ctx, ann.ID); err != nil {
		t.Fatalf("DeleteAnnotation: %v", err)
	}

	// Verify it's gone
	annotations, err := env.taskSvc.GetAnnotations(ctx, task.ShortID)
	if err != nil {
		t.Fatalf("GetAnnotations: %v", err)
	}
	if len(annotations) != 0 {
		t.Fatalf("expected 0 annotations after delete, got %d", len(annotations))
	}
}

func TestDeleteAnnotation_NotFound(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	err := env.taskSvc.DeleteAnnotation(ctx, uuid.New())
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
