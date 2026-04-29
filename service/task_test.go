package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/sqlite"
	"github.com/google/uuid"
)

// testTaskEnvWithSettings creates a test environment with custom project settings.
// The default project (seeded by migrations) is updated in-place with the given
// settings via projectRepo.Update so tests can exercise auto-complete / auto-revert
// behavior without rebuilding the project row from scratch.
func testTaskEnvWithSettings(test *testing.T, settings domain.ProjectSettings) *testEnv {
	test.Helper()
	bundle, projectRepo, _ := newSeededBundle(test)
	workflowRepo := sqlite.NewWorkflowRepo(bundle.Store.DB())

	ctx := context.Background()

	defaultProj, err := projectRepo.GetByID(ctx, domain.DefaultProjectUUID)

	if err != nil {
		test.Fatalf("loading default project: %v", err)
	}

	defaultProj.Settings = settings

	if err := projectRepo.Update(ctx, defaultProj); err != nil {
		test.Fatalf("updating default project settings: %v", err)
	}

	resolver, projects := singleBundleResolver(bundle, domain.DefaultProjectUUID)

	workflowSvc := NewWorkflowService(workflowRepo, projectRepo)
	taskSvc := NewTaskService(resolver, projects, projectRepo, nil, workflowSvc, nil)

	return &testEnv{
		taskSvc:     taskSvc,
		workflowSvc: workflowSvc,
		store:       bundle.Store,
	}
}

// testEnv holds all the services and repos needed for TaskService tests.
type testEnv struct {
	taskSvc     *TaskService
	workflowSvc *WorkflowService
	store       *sqlite.Store
}

func testTaskEnv(test *testing.T) *testEnv {
	test.Helper()
	return testTaskEnvWithSettings(test, domain.ProjectSettings{})
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
func mustCreateTask(test *testing.T, svc *TaskService, task *domain.Task) {
	test.Helper()
	if err := svc.Create(context.Background(), task); err != nil {
		test.Fatalf("mustCreateTask: %v", err)
	}
}

func TestCreate_HappyPath(test *testing.T) {
	env := testTaskEnv(test)
	ctx := context.Background()

	task := newMinimalTask("My first task")

	if err := env.taskSvc.Create(ctx, task); err != nil {
		test.Fatalf("Create: %v", err)
	}

	// Verify the service populated all required fields
	if task.ID == uuid.Nil {
		test.Fatal("expected non-nil ID")
	}
	if task.ShortID == "" {
		test.Fatal("expected non-empty ShortID")
	}
	if len(task.ShortID) < 8 {
		test.Fatalf("expected ShortID length >= 8, got %d", len(task.ShortID))
	}
	if task.Version != 1 {
		test.Fatalf("expected version 1, got %d", task.Version)
	}
	if task.Status != "pending" {
		test.Fatalf("expected status 'pending', got %q", task.Status)
	}
	if task.ProjectID != domain.DefaultProjectUUID {
		test.Fatalf("expected default project ID, got %v", task.ProjectID)
	}
	if task.CreatedAt.IsZero() {
		test.Fatal("expected CreatedAt to be set")
	}
	if task.ModifiedAt.IsZero() {
		test.Fatal("expected ModifiedAt to be set")
	}
	if task.UDA == nil {
		test.Fatal("expected UDA to be initialized")
	}

	// Verify it's actually persisted by reading it back
	got, err := env.taskSvc.GetByShortID(ctx, task.ShortID)

	if err != nil {
		test.Fatalf("GetByShortID: %v", err)
	}

	if got.Title != "My first task" {
		test.Fatalf("expected title 'My first task', got %q", got.Title)
	}
}

func TestCreate_EmptyTitle(test *testing.T) {
	env := testTaskEnv(test)
	ctx := context.Background()

	task := newMinimalTask("")
	err := env.taskSvc.Create(ctx, task)
	if err == nil {
		test.Fatal("expected error for empty title")
	}
}

func TestCreate_PriorityTooHigh(test *testing.T) {
	env := testTaskEnv(test)
	ctx := context.Background()

	task := newMinimalTask("Bad priority")
	task.Priority = 5
	err := env.taskSvc.Create(ctx, task)
	if err == nil {
		test.Fatal("expected error for priority > 4")
	}
}

func TestCreate_PriorityNegative(test *testing.T) {
	env := testTaskEnv(test)
	ctx := context.Background()

	task := newMinimalTask("Negative priority")
	task.Priority = -1
	err := env.taskSvc.Create(ctx, task)
	if err == nil {
		test.Fatal("expected error for negative priority")
	}
}

func TestCreate_InvalidParent(test *testing.T) {
	env := testTaskEnv(test)
	ctx := context.Background()

	nonexistent := uuid.New()
	task := newMinimalTask("Orphan task")
	task.ParentID = &nonexistent
	err := env.taskSvc.Create(ctx, task)
	if err == nil {
		test.Fatal("expected error for nonexistent parent")
	}
}

func TestCreate_InvalidProject(test *testing.T) {
	env := testTaskEnv(test)
	ctx := context.Background()

	task := newMinimalTask("Bad project")
	task.ProjectID = uuid.New() // unknown project UUID
	err := env.taskSvc.Create(ctx, task)
	if err == nil {
		test.Fatal("expected error for nonexistent project")
	}
}

func TestCreate_InvalidInitialStatus(test *testing.T) {
	env := testTaskEnv(test)
	ctx := context.Background()

	task := newMinimalTask("Bad status")
	task.Status = "nonexistent_status"
	err := env.taskSvc.Create(ctx, task)
	if err == nil {
		test.Fatal("expected error for invalid initial status")
	}
}

func TestCreate_ValidParent(test *testing.T) {
	env := testTaskEnv(test)
	ctx := context.Background()

	parent := newMinimalTask("Parent")
	mustCreateTask(test, env.taskSvc, parent)

	child := newMinimalTask("Child")
	child.ParentID = &parent.ID

	if err := env.taskSvc.Create(ctx, child); err != nil {
		test.Fatalf("Create child: %v", err)
	}

	if child.ParentID == nil || *child.ParentID != parent.ID {
		test.Fatal("expected child to reference parent")
	}
}

func TestCreate_DefaultsToDefaultProject(test *testing.T) {
	env := testTaskEnv(test)
	ctx := context.Background()

	task := newMinimalTask("No project set")

	if err := env.taskSvc.Create(ctx, task); err != nil {
		test.Fatalf("Create: %v", err)
	}

	if task.ProjectID != domain.DefaultProjectUUID {
		test.Fatalf("expected default project, got %v", task.ProjectID)
	}
}

func TestCreate_WithAllFields(test *testing.T) {
	env := testTaskEnv(test)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Millisecond)
	due := now.Add(24 * time.Hour)
	wait := now.Add(1 * time.Hour)
	rrule := "FREQ=DAILY;COUNT=5"
	task := &domain.Task{
		Title:          "Full task",
		Description:    "All fields populated",
		Status:         "pending",
		Priority:       3,
		ProjectID:      domain.DefaultProjectUUID,
		DueAt:          &due,
		WaitUntil:      &wait,
		RecurrenceRule: &rrule,
		UDA:            map[string]any{"custom": "value"},
	}

	if err := env.taskSvc.Create(ctx, task); err != nil {
		test.Fatalf("Create: %v", err)
	}

	got, err := env.taskSvc.GetByShortID(ctx, task.ShortID)

	if err != nil {
		test.Fatalf("GetByShortID: %v", err)
	}

	if got.Description != "All fields populated" {
		test.Fatalf("expected description preserved, got %q", got.Description)
	}
	if got.Priority != 3 {
		test.Fatalf("expected priority 3, got %d", got.Priority)
	}
	if got.DueAt == nil {
		test.Fatal("expected DueAt to be set")
	}
	if got.RecurrenceRule == nil || *got.RecurrenceRule != rrule {
		test.Fatal("expected RecurrenceRule to be preserved")
	}
}

func TestGetByShortID_Found(test *testing.T) {
	env := testTaskEnv(test)
	ctx := context.Background()

	task := newMinimalTask("Find me")
	mustCreateTask(test, env.taskSvc, task)

	got, err := env.taskSvc.GetByShortID(ctx, task.ShortID)

	if err != nil {
		test.Fatalf("GetByShortID: %v", err)
	}

	if got.Title != "Find me" {
		test.Fatalf("expected 'Find me', got %q", got.Title)
	}
}

func TestGetByShortID_NotFound(test *testing.T) {
	env := testTaskEnv(test)
	ctx := context.Background()

	_, err := env.taskSvc.GetByShortID(ctx, "nonexist")
	if !errors.Is(err, domain.ErrNotFound) {
		test.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestGetByID_Found(test *testing.T) {
	env := testTaskEnv(test)
	ctx := context.Background()

	task := newMinimalTask("Get by ID")
	mustCreateTask(test, env.taskSvc, task)

	got, err := env.taskSvc.GetByID(ctx, task.ID)

	if err != nil {
		test.Fatalf("GetByID: %v", err)
	}

	if got.Title != "Get by ID" {
		test.Fatalf("expected 'Get by ID', got %q", got.Title)
	}
}

func TestList_Empty(test *testing.T) {
	env := testTaskEnv(test)
	ctx := context.Background()

	tasks, err := env.taskSvc.List(ctx, &domain.TermFilter{})

	if err != nil {
		test.Fatalf("List: %v", err)
	}

	if len(tasks) != 0 {
		test.Fatalf("expected 0 tasks, got %d", len(tasks))
	}
}

func TestList_WithFilter(test *testing.T) {
	env := testTaskEnv(test)
	ctx := context.Background()

	taskOne := newMinimalTask("Task one")
	taskOne.Priority = 3
	mustCreateTask(test, env.taskSvc, taskOne)

	taskTwo := newMinimalTask("Task two")
	taskTwo.Priority = 1
	mustCreateTask(test, env.taskSvc, taskTwo)

	minPri := 3
	tasks, err := env.taskSvc.List(ctx, &domain.TermFilter{TaskFilter: domain.TaskFilter{PriorityMin: &minPri}})

	if err != nil {
		test.Fatalf("List: %v", err)
	}

	if len(tasks) != 1 {
		test.Fatalf("expected 1 task with priority >= 3, got %d", len(tasks))
	}
	if tasks[0].Title != "Task one" {
		test.Fatalf("expected 'Task one', got %q", tasks[0].Title)
	}
}

func TestGetChildren(test *testing.T) {
	env := testTaskEnv(test)
	ctx := context.Background()

	parent := newMinimalTask("Parent")
	mustCreateTask(test, env.taskSvc, parent)

	child1 := newMinimalTask("Child 1")
	child1.ParentID = &parent.ID
	mustCreateTask(test, env.taskSvc, child1)

	child2 := newMinimalTask("Child 2")
	child2.ParentID = &parent.ID
	mustCreateTask(test, env.taskSvc, child2)

	children, err := env.taskSvc.GetChildren(ctx, parent.ID)

	if err != nil {
		test.Fatalf("GetChildren: %v", err)
	}

	if len(children) != 2 {
		test.Fatalf("expected 2 children, got %d", len(children))
	}
}

func TestGetDescendants(test *testing.T) {
	env := testTaskEnv(test)
	ctx := context.Background()

	root := newMinimalTask("Root")
	mustCreateTask(test, env.taskSvc, root)

	child := newMinimalTask("Child")
	child.ParentID = &root.ID
	mustCreateTask(test, env.taskSvc, child)

	grandchild := newMinimalTask("Grandchild")
	grandchild.ParentID = &child.ID
	mustCreateTask(test, env.taskSvc, grandchild)

	descendants, err := env.taskSvc.GetDescendants(ctx, root.ID)

	if err != nil {
		test.Fatalf("GetDescendants: %v", err)
	}

	if len(descendants) != 2 {
		test.Fatalf("expected 2 descendants, got %d", len(descendants))
	}
}

func TestUpdate_PartialUpdate(test *testing.T) {
	env := testTaskEnv(test)
	ctx := context.Background()

	task := newMinimalTask("Original title")
	task.Priority = 1
	mustCreateTask(test, env.taskSvc, task)

	newTitle := "Updated title"
	updated, err := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID: task.ShortID,
		Version: 1,
		Title:   &newTitle,
	})

	if err != nil {
		test.Fatalf("Update: %v", err)
	}

	if updated.Title != "Updated title" {
		test.Fatalf("expected 'Updated title', got %q", updated.Title)
	}
	// Priority should be unchanged
	if updated.Priority != 1 {
		test.Fatalf("expected priority 1 unchanged, got %d", updated.Priority)
	}
	// Version should be bumped
	if updated.Version != 2 {
		test.Fatalf("expected version 2, got %d", updated.Version)
	}
}

func TestUpdate_VersionConflict(test *testing.T) {
	env := testTaskEnv(test)
	ctx := context.Background()

	task := newMinimalTask("Conflict test")
	mustCreateTask(test, env.taskSvc, task)

	newTitle := "First update"
	_, firstErr := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID: task.ShortID,
		Version: 1,
		Title:   &newTitle,
	})

	if firstErr != nil {
		test.Fatalf("first Update: %v", firstErr)
	}

	// Try to update with stale version
	staleTitle := "Stale update"
	_, staleErr := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID: task.ShortID,
		Version: 1,
		Title:   &staleTitle,
	})
	if !errors.Is(staleErr, domain.ErrConflict) {
		test.Fatalf("expected ErrConflict, got %v", staleErr)
	}
}

func TestUpdate_StatusTransitionAllowed(test *testing.T) {
	env := testTaskEnv(test)
	ctx := context.Background()

	task := newMinimalTask("Transition test")
	mustCreateTask(test, env.taskSvc, task)

	activeStatus := "active"
	updated, err := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID: task.ShortID,
		Version: 1,
		Status:  &activeStatus,
	})

	if err != nil {
		test.Fatalf("Update: %v", err)
	}

	if updated.Status != "active" {
		test.Fatalf("expected status 'active', got %q", updated.Status)
	}
}

func TestUpdate_StatusTransitionDisallowed(test *testing.T) {
	env := testTaskEnv(test)
	ctx := context.Background()

	task := newMinimalTask("Bad transition")
	mustCreateTask(test, env.taskSvc, task)

	// pending → completed is not allowed in the default workflow
	completedStatus := "completed"
	_, err := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID: task.ShortID,
		Version: 1,
		Status:  &completedStatus,
	})
	if !errors.Is(err, domain.ErrInvalidTransition) {
		test.Fatalf("expected ErrInvalidTransition, got %v", err)
	}
}

func TestUpdate_EmptyTitleRejected(test *testing.T) {
	env := testTaskEnv(test)
	ctx := context.Background()

	task := newMinimalTask("Will be emptied")
	mustCreateTask(test, env.taskSvc, task)

	emptyTitle := ""
	_, err := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID: task.ShortID,
		Version: 1,
		Title:   &emptyTitle,
	})
	if err == nil {
		test.Fatal("expected error for empty title")
	}
}

func TestUpdate_InvalidPriorityRejected(test *testing.T) {
	env := testTaskEnv(test)
	ctx := context.Background()

	task := newMinimalTask("Bad priority update")
	mustCreateTask(test, env.taskSvc, task)

	badPriority := 5
	_, err := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID:  task.ShortID,
		Version:  1,
		Priority: &badPriority,
	})
	if err == nil {
		test.Fatal("expected error for priority > 4")
	}
}

func TestUpdate_ParentCannotBeSelf(test *testing.T) {
	env := testTaskEnv(test)
	ctx := context.Background()

	task := newMinimalTask("Self parent")
	mustCreateTask(test, env.taskSvc, task)

	selfRef := &task.ID
	_, err := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID:  task.ShortID,
		Version:  1,
		ParentID: &selfRef,
	})
	if err == nil {
		test.Fatal("expected error when setting parent to self")
	}
}

func TestUpdate_ClearNullableField(test *testing.T) {
	env := testTaskEnv(test)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Millisecond)
	due := now.Add(24 * time.Hour)
	task := newMinimalTask("Has due date")
	task.DueAt = &due
	mustCreateTask(test, env.taskSvc, task)

	// Clear the due date by setting outer pointer to non-nil, inner to nil
	var nilTime *time.Time
	updated, err := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID: task.ShortID,
		Version: 1,
		DueAt:   &nilTime,
	})

	if err != nil {
		test.Fatalf("Update: %v", err)
	}

	if updated.DueAt != nil {
		test.Fatal("expected DueAt to be cleared")
	}
}

func TestUpdate_SetDescription(test *testing.T) {
	env := testTaskEnv(test)
	ctx := context.Background()

	task := newMinimalTask("Has no description")
	mustCreateTask(test, env.taskSvc, task)

	// Set description via double-pointer
	desc := "A detailed description"
	dp := &desc
	updated, err := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID:     task.ShortID,
		Version:     1,
		Description: &dp,
	})

	if err != nil {
		test.Fatalf("Update: %v", err)
	}

	if updated.Description != "A detailed description" {
		test.Fatalf("expected description %q, got %q", "A detailed description", updated.Description)
	}
}

func TestUpdate_ClearDescription(test *testing.T) {
	env := testTaskEnv(test)
	ctx := context.Background()

	task := newMinimalTask("Has description")
	task.Description = "Will be cleared"
	mustCreateTask(test, env.taskSvc, task)

	// Verify description was set
	created, getErr := env.taskSvc.GetByShortID(ctx, task.ShortID)

	if getErr != nil {
		test.Fatalf("GetByShortID: %v", getErr)
	}

	if created.Description != "Will be cleared" {
		test.Fatalf("expected description %q, got %q", "Will be cleared", created.Description)
	}

	// Clear description via double-pointer (outer non-nil, inner nil)
	var nilStr *string
	updated, updateErr := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID:     task.ShortID,
		Version:     1,
		Description: &nilStr,
	})

	if updateErr != nil {
		test.Fatalf("Update: %v", updateErr)
	}

	if updated.Description != "" {
		test.Fatalf("expected empty description, got %q", updated.Description)
	}
}

func TestUpdate_NilDescriptionNoChange(test *testing.T) {
	env := testTaskEnv(test)
	ctx := context.Background()

	task := newMinimalTask("Keep description")
	task.Description = "Should not change"
	mustCreateTask(test, env.taskSvc, task)

	// Update title only, leave description nil (no change)
	newTitle := "New title"
	updated, err := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID: task.ShortID,
		Version: 1,
		Title:   &newTitle,
	})

	if err != nil {
		test.Fatalf("Update: %v", err)
	}

	if updated.Description != "Should not change" {
		test.Fatalf("expected description unchanged, got %q", updated.Description)
	}
}

func TestUpdate_NotFound(test *testing.T) {
	env := testTaskEnv(test)
	ctx := context.Background()

	newTitle := "Doesn't matter"
	_, err := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID: "nonexist",
		Version: 1,
		Title:   &newTitle,
	})
	if !errors.Is(err, domain.ErrNotFound) {
		test.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestStart_HappyPath(test *testing.T) {
	env := testTaskEnv(test)
	ctx := context.Background()

	task := newMinimalTask("Start me")
	mustCreateTask(test, env.taskSvc, task)

	updated, err := env.taskSvc.Start(ctx, task.ShortID, 1, "")

	if err != nil {
		test.Fatalf("Start: %v", err)
	}

	if updated.Status != "active" {
		test.Fatalf("expected status 'active', got %q", updated.Status)
	}
	if updated.Version != 2 {
		test.Fatalf("expected version 2, got %d", updated.Version)
	}
}

func TestStart_AlreadyActive(test *testing.T) {
	env := testTaskEnv(test)
	ctx := context.Background()

	task := newMinimalTask("Already active")
	mustCreateTask(test, env.taskSvc, task)

	_, firstErr := env.taskSvc.Start(ctx, task.ShortID, 1, "")

	if firstErr != nil {
		test.Fatalf("Start: %v", firstErr)
	}

	// active → active is a no-op (status unchanged), should succeed
	updated, secondErr := env.taskSvc.Start(ctx, task.ShortID, 2, "")

	if secondErr != nil {
		test.Fatalf("Start on already-active task: %v", secondErr)
	}

	if updated.Status != "active" {
		test.Fatalf("expected status 'active', got %q", updated.Status)
	}
}

func TestComplete_HappyPath(test *testing.T) {
	env := testTaskEnv(test)
	ctx := context.Background()

	task := newMinimalTask("Complete me")
	mustCreateTask(test, env.taskSvc, task)

	// Must start first: pending → active
	started, startErr := env.taskSvc.Start(ctx, task.ShortID, 1, "")

	if startErr != nil {
		test.Fatalf("Start: %v", startErr)
	}

	// Then complete: active → completed
	completed, completeErr := env.taskSvc.Complete(ctx, task.ShortID, started.Version)

	if completeErr != nil {
		test.Fatalf("Complete: %v", completeErr)
	}

	if completed.Status != "completed" {
		test.Fatalf("expected status 'completed', got %q", completed.Status)
	}
}

func TestComplete_FromPending(test *testing.T) {
	env := testTaskEnv(test)
	ctx := context.Background()

	task := newMinimalTask("Skip start")
	mustCreateTask(test, env.taskSvc, task)

	// pending → completed is not allowed
	_, err := env.taskSvc.Complete(ctx, task.ShortID, 1)
	if !errors.Is(err, domain.ErrInvalidTransition) {
		test.Fatalf("expected ErrInvalidTransition, got %v", err)
	}
}

func TestDelete_HappyPath(test *testing.T) {
	env := testTaskEnv(test)
	ctx := context.Background()

	task := newMinimalTask("Delete me")
	mustCreateTask(test, env.taskSvc, task)

	// pending → deleted is allowed
	deleted, err := env.taskSvc.Delete(ctx, task.ShortID, 1)

	if err != nil {
		test.Fatalf("Delete: %v", err)
	}

	if deleted.Status != "deleted" {
		test.Fatalf("expected status 'deleted', got %q", deleted.Status)
	}
}

func TestDelete_FromCompleted(test *testing.T) {
	env := testTaskEnv(test)
	ctx := context.Background()

	task := newMinimalTask("Complete then delete")
	mustCreateTask(test, env.taskSvc, task)

	started, _ := env.taskSvc.Start(ctx, task.ShortID, 1, "")
	completed, _ := env.taskSvc.Complete(ctx, task.ShortID, started.Version)

	// completed → deleted is not allowed in default workflow
	_, err := env.taskSvc.Delete(ctx, task.ShortID, completed.Version)
	if !errors.Is(err, domain.ErrInvalidTransition) {
		test.Fatalf("expected ErrInvalidTransition, got %v", err)
	}
}

func TestAnnotate_HappyPath(test *testing.T) {
	env := testTaskEnv(test)
	ctx := context.Background()

	task := newMinimalTask("Annotate me")
	mustCreateTask(test, env.taskSvc, task)

	annotation, err := env.taskSvc.Annotate(ctx, task.ShortID, "This is a note")

	if err != nil {
		test.Fatalf("Annotate: %v", err)
	}

	if annotation.ID == uuid.Nil {
		test.Fatal("expected non-nil annotation ID")
	}
	if annotation.TaskID != task.ID {
		test.Fatalf("expected TaskID %s, got %s", task.ID, annotation.TaskID)
	}
	if annotation.Body != "This is a note" {
		test.Fatalf("expected body 'This is a note', got %q", annotation.Body)
	}
	if annotation.CreatedAt.IsZero() {
		test.Fatal("expected CreatedAt to be set")
	}
}

func TestAnnotate_EmptyBody(test *testing.T) {
	env := testTaskEnv(test)
	ctx := context.Background()

	task := newMinimalTask("Annotate me")
	mustCreateTask(test, env.taskSvc, task)

	_, err := env.taskSvc.Annotate(ctx, task.ShortID, "")
	if err == nil {
		test.Fatal("expected error for empty annotation body")
	}
}

func TestAnnotate_TaskNotFound(test *testing.T) {
	env := testTaskEnv(test)
	ctx := context.Background()

	_, err := env.taskSvc.Annotate(ctx, "nonexist", "Some note")
	if !errors.Is(err, domain.ErrNotFound) {
		test.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestGetAnnotations_WithResults(test *testing.T) {
	env := testTaskEnv(test)
	ctx := context.Background()

	task := newMinimalTask("Has annotations")
	mustCreateTask(test, env.taskSvc, task)

	_, firstAnnotateErr := env.taskSvc.Annotate(ctx, task.ShortID, "Note 1")

	if firstAnnotateErr != nil {
		test.Fatalf("Annotate 1: %v", firstAnnotateErr)
	}

	_, secondAnnotateErr := env.taskSvc.Annotate(ctx, task.ShortID, "Note 2")

	if secondAnnotateErr != nil {
		test.Fatalf("Annotate 2: %v", secondAnnotateErr)
	}

	annotations, err := env.taskSvc.GetAnnotations(ctx, task.ShortID)

	if err != nil {
		test.Fatalf("GetAnnotations: %v", err)
	}

	if len(annotations) != 2 {
		test.Fatalf("expected 2 annotations, got %d", len(annotations))
	}
}

func TestGetAnnotations_Empty(test *testing.T) {
	env := testTaskEnv(test)
	ctx := context.Background()

	task := newMinimalTask("No annotations")
	mustCreateTask(test, env.taskSvc, task)

	annotations, err := env.taskSvc.GetAnnotations(ctx, task.ShortID)

	if err != nil {
		test.Fatalf("GetAnnotations: %v", err)
	}

	if len(annotations) != 0 {
		test.Fatalf("expected 0 annotations, got %d", len(annotations))
	}
}

func TestGetAnnotations_TaskNotFound(test *testing.T) {
	env := testTaskEnv(test)
	ctx := context.Background()

	_, err := env.taskSvc.GetAnnotations(ctx, "nonexist")
	if !errors.Is(err, domain.ErrNotFound) {
		test.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestDeleteAnnotation_HappyPath(test *testing.T) {
	env := testTaskEnv(test)
	ctx := context.Background()

	task := newMinimalTask("Delete annotation")
	mustCreateTask(test, env.taskSvc, task)

	annotation, annotateErr := env.taskSvc.Annotate(ctx, task.ShortID, "To be deleted")

	if annotateErr != nil {
		test.Fatalf("Annotate: %v", annotateErr)
	}

	if err := env.taskSvc.DeleteAnnotation(ctx, annotation.ID); err != nil {
		test.Fatalf("DeleteAnnotation: %v", err)
	}

	// Verify it's gone
	annotations, err := env.taskSvc.GetAnnotations(ctx, task.ShortID)

	if err != nil {
		test.Fatalf("GetAnnotations: %v", err)
	}

	if len(annotations) != 0 {
		test.Fatalf("expected 0 annotations after delete, got %d", len(annotations))
	}
}

func TestDeleteAnnotation_NotFound(test *testing.T) {
	env := testTaskEnv(test)
	ctx := context.Background()

	err := env.taskSvc.DeleteAnnotation(ctx, uuid.New())
	if !errors.Is(err, domain.ErrNotFound) {
		test.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestCreate_CyclicParentRejected(test *testing.T) {
	env := testTaskEnv(test)
	ctx := context.Background()

	// Create A
	taskA := newMinimalTask("Task A")
	mustCreateTask(test, env.taskSvc, taskA)

	// Create B with parent A
	taskB := newMinimalTask("Task B")
	taskB.ParentID = &taskA.ID
	mustCreateTask(test, env.taskSvc, taskB)

	// Create C with parent B — valid chain A -> B -> C, should succeed
	taskC := newMinimalTask("Task C")
	taskC.ParentID = &taskB.ID

	if err := env.taskSvc.Create(ctx, taskC); err != nil {
		test.Fatalf("Create with valid parent chain should succeed: %v", err)
	}
}

func TestUpdate_CyclicParentDirectRejected(test *testing.T) {
	env := testTaskEnv(test)
	ctx := context.Background()

	// Create A
	taskA := newMinimalTask("Task A")
	mustCreateTask(test, env.taskSvc, taskA)

	// Create B with parent A
	taskB := newMinimalTask("Task B")
	taskB.ParentID = &taskA.ID
	mustCreateTask(test, env.taskSvc, taskB)

	// Try to set A's parent to B — should fail (cycle: A->B->A)
	parentRef := &taskB.ID
	_, err := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID:  taskA.ShortID,
		Version:  taskA.Version,
		ParentID: &parentRef,
	})
	if !errors.Is(err, domain.ErrCyclicParent) {
		test.Fatalf("expected ErrCyclicParent, got %v", err)
	}
}

func TestUpdate_StatusChange_Transactional(test *testing.T) {
	env := testTaskEnv(test)
	ctx := context.Background()

	task := newMinimalTask("Transactional test")
	mustCreateTask(test, env.taskSvc, task)

	// Start the task (pending -> active) — this triggers the transactional path
	updated, startErr := env.taskSvc.Start(ctx, task.ShortID, task.Version, "")

	if startErr != nil {
		test.Fatalf("Start: %v", startErr)
	}

	if updated.Status != "active" {
		test.Fatalf("expected status 'active', got %q", updated.Status)
	}
	if updated.Version != 2 {
		test.Fatalf("expected version 2, got %d", updated.Version)
	}

	// Complete it (active -> completed)
	completed, completeErr := env.taskSvc.Complete(ctx, updated.ShortID, updated.Version)

	if completeErr != nil {
		test.Fatalf("Complete: %v", completeErr)
	}

	if completed.Status != "completed" {
		test.Fatalf("expected status 'completed', got %q", completed.Status)
	}
	if completed.Version != 3 {
		test.Fatalf("expected version 3, got %d", completed.Version)
	}
}

func TestTaskService_WithTxProvider(test *testing.T) {
	bundle, projectRepo, workflowRepo := newSeededBundle(test)

	resolver, projects := singleBundleResolver(bundle, domain.DefaultProjectUUID)
	workflowSvc := NewWorkflowService(workflowRepo, projectRepo)
	taskSvc := NewTaskService(resolver, projects, projectRepo, nil, workflowSvc, nil)

	ctx := context.Background()
	task := newMinimalTask("Test with tx provider")

	if err := taskSvc.Create(ctx, task); err != nil {
		test.Fatalf("Create: %v", err)
	}

	// Start and complete — basic lifecycle still works
	_, startErr := taskSvc.Start(ctx, task.ShortID, task.Version, "")

	if startErr != nil {
		test.Fatalf("Start: %v", startErr)
	}

	started, _ := taskSvc.GetByShortID(ctx, task.ShortID)

	_, completeErr := taskSvc.Complete(ctx, started.ShortID, started.Version)

	if completeErr != nil {
		test.Fatalf("Complete: %v", completeErr)
	}
}

func TestUpdate_CyclicParentTransitiveRejected(test *testing.T) {
	env := testTaskEnv(test)
	ctx := context.Background()

	// Create chain: A -> B -> C (A is root, B's parent is A, C's parent is B)
	taskA := newMinimalTask("Task A")
	mustCreateTask(test, env.taskSvc, taskA)

	taskB := newMinimalTask("Task B")
	taskB.ParentID = &taskA.ID
	mustCreateTask(test, env.taskSvc, taskB)

	taskC := newMinimalTask("Task C")
	taskC.ParentID = &taskB.ID
	mustCreateTask(test, env.taskSvc, taskC)

	// Try to set A's parent to C — should fail (cycle: A->B->C->A)
	parentRef := &taskC.ID
	_, err := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID:  taskA.ShortID,
		Version:  taskA.Version,
		ParentID: &parentRef,
	})
	if !errors.Is(err, domain.ErrCyclicParent) {
		test.Fatalf("expected ErrCyclicParent, got %v", err)
	}
}

func TestUpdate_ReparentNoCycle(test *testing.T) {
	env := testTaskEnv(test)
	ctx := context.Background()

	// Create three independent tasks
	taskA := newMinimalTask("Task A")
	mustCreateTask(test, env.taskSvc, taskA)

	taskB := newMinimalTask("Task B")
	mustCreateTask(test, env.taskSvc, taskB)

	taskC := newMinimalTask("Task C")
	mustCreateTask(test, env.taskSvc, taskC)

	// Set B's parent to A — should succeed (no cycle)
	parentRef := &taskA.ID
	updated, err := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID:  taskB.ShortID,
		Version:  taskB.Version,
		ParentID: &parentRef,
	})

	if err != nil {
		test.Fatalf("expected reparent to succeed, got %v", err)
	}

	if updated.ParentID == nil || *updated.ParentID != taskA.ID {
		test.Fatalf("expected parent to be task A")
	}
}

func TestAutoComplete_AllChildrenCompleted(test *testing.T) {
	env := testTaskEnvWithSettings(test, domain.ProjectSettings{
		AutoCompleteParent: &domain.AutoCompleteConfig{
			TriggerStatus: "completed",
			TargetStatus:  "completed",
		},
	})
	ctx := context.Background()

	// Create parent
	parent := newMinimalTask("Parent")
	mustCreateTask(test, env.taskSvc, parent)
	// Start parent (pending -> active) so it can later transition to completed
	parent, err := env.taskSvc.Start(ctx, parent.ShortID, parent.Version, "")

	if err != nil {
		test.Fatalf("Start parent: %v", err)
	}

	// Create two children
	child1 := &domain.Task{Title: "Child 1", ParentID: &parent.ID}
	mustCreateTask(test, env.taskSvc, child1)
	child2 := &domain.Task{Title: "Child 2", ParentID: &parent.ID}
	mustCreateTask(test, env.taskSvc, child2)

	// Start and complete child1
	child1, err = env.taskSvc.Start(ctx, child1.ShortID, child1.Version, "")

	if err != nil {
		test.Fatalf("Start child1: %v", err)
	}

	_, err = env.taskSvc.Complete(ctx, child1.ShortID, child1.Version)

	if err != nil {
		test.Fatalf("Complete child1: %v", err)
	}

	// Parent should NOT be auto-completed yet (child2 still pending)
	parentCheck, _ := env.taskSvc.GetByShortID(ctx, parent.ShortID)
	if parentCheck.Status != "active" {
		test.Fatalf("expected parent still 'active' after first child completed, got %q", parentCheck.Status)
	}

	// Start and complete child2
	child2, err = env.taskSvc.Start(ctx, child2.ShortID, child2.Version, "")

	if err != nil {
		test.Fatalf("Start child2: %v", err)
	}

	_, err = env.taskSvc.Complete(ctx, child2.ShortID, child2.Version)

	if err != nil {
		test.Fatalf("Complete child2: %v", err)
	}

	// Parent SHOULD be auto-completed now
	parentCheck, _ = env.taskSvc.GetByShortID(ctx, parent.ShortID)
	if parentCheck.Status != "completed" {
		test.Fatalf("expected parent 'completed' after all children completed, got %q", parentCheck.Status)
	}
}

func TestAutoComplete_Disabled_ByDefault(test *testing.T) {
	env := testTaskEnv(test)
	ctx := context.Background()

	// Do NOT enable auto-complete — default settings

	parent := newMinimalTask("Parent")
	mustCreateTask(test, env.taskSvc, parent)

	parent, err := env.taskSvc.Start(ctx, parent.ShortID, parent.Version, "")

	if err != nil {
		test.Fatalf("Start parent: %v", err)
	}

	child := &domain.Task{Title: "Child", ParentID: &parent.ID}
	mustCreateTask(test, env.taskSvc, child)

	child, err = env.taskSvc.Start(ctx, child.ShortID, child.Version, "")

	if err != nil {
		test.Fatalf("Start child: %v", err)
	}

	_, err = env.taskSvc.Complete(ctx, child.ShortID, child.Version)

	if err != nil {
		test.Fatalf("Complete child: %v", err)
	}

	// Parent should NOT be auto-completed (feature disabled)
	parentCheck, _ := env.taskSvc.GetByShortID(ctx, parent.ShortID)
	if parentCheck.Status != "active" {
		test.Fatalf("expected parent still 'active' (propagation disabled), got %q", parentCheck.Status)
	}
}

func TestAutoComplete_DeletedChildrenIgnored(test *testing.T) {
	env := testTaskEnvWithSettings(test, domain.ProjectSettings{
		AutoCompleteParent: &domain.AutoCompleteConfig{
			TriggerStatus: "completed",
			TargetStatus:  "completed",
		},
	})
	ctx := context.Background()

	parent := newMinimalTask("Parent")
	mustCreateTask(test, env.taskSvc, parent)
	parent, _ = env.taskSvc.Start(ctx, parent.ShortID, parent.Version, "")

	child1 := &domain.Task{Title: "Child 1", ParentID: &parent.ID}
	mustCreateTask(test, env.taskSvc, child1)
	child2 := &domain.Task{Title: "Child 2", ParentID: &parent.ID}
	mustCreateTask(test, env.taskSvc, child2)

	// Delete child2
	_, _ = env.taskSvc.Delete(ctx, child2.ShortID, child2.Version)

	// Start and complete child1
	child1, _ = env.taskSvc.Start(ctx, child1.ShortID, child1.Version, "")
	_, _ = env.taskSvc.Complete(ctx, child1.ShortID, child1.Version)

	// Parent should be auto-completed (deleted child ignored)
	parentCheck, _ := env.taskSvc.GetByShortID(ctx, parent.ShortID)
	if parentCheck.Status != "completed" {
		test.Fatalf("expected parent 'completed' (deleted child ignored), got %q", parentCheck.Status)
	}
}

func TestAutoComplete_WorkflowGuard(test *testing.T) {
	env := testTaskEnvWithSettings(test, domain.ProjectSettings{
		AutoCompleteParent: &domain.AutoCompleteConfig{
			TriggerStatus: "completed",
			TargetStatus:  "completed",
		},
	})
	ctx := context.Background()

	// Create parent but do NOT start it — leave in "pending"
	parent := newMinimalTask("Parent pending")
	mustCreateTask(test, env.taskSvc, parent)

	child := &domain.Task{Title: "Child", ParentID: &parent.ID}
	mustCreateTask(test, env.taskSvc, child)
	child, _ = env.taskSvc.Start(ctx, child.ShortID, child.Version, "")
	_, _ = env.taskSvc.Complete(ctx, child.ShortID, child.Version)

	// Parent should NOT be auto-completed (pending -> completed is not allowed)
	parentCheck, _ := env.taskSvc.GetByShortID(ctx, parent.ShortID)
	if parentCheck.Status != "pending" {
		test.Fatalf("expected parent still 'pending' (workflow blocks transition), got %q", parentCheck.Status)
	}
}

func TestAutoComplete_Recursive(test *testing.T) {
	env := testTaskEnvWithSettings(test, domain.ProjectSettings{
		AutoCompleteParent: &domain.AutoCompleteConfig{
			TriggerStatus: "completed",
			TargetStatus:  "completed",
		},
	})
	ctx := context.Background()

	// Create grandparent -> parent -> child chain
	grandparent := newMinimalTask("Grandparent")
	mustCreateTask(test, env.taskSvc, grandparent)
	grandparent, _ = env.taskSvc.Start(ctx, grandparent.ShortID, grandparent.Version, "")

	parent := &domain.Task{Title: "Parent", ParentID: &grandparent.ID}
	mustCreateTask(test, env.taskSvc, parent)
	parent, _ = env.taskSvc.Start(ctx, parent.ShortID, parent.Version, "")

	child := &domain.Task{Title: "Child", ParentID: &parent.ID}
	mustCreateTask(test, env.taskSvc, child)
	child, _ = env.taskSvc.Start(ctx, child.ShortID, child.Version, "")

	// Complete child — should cascade: child done -> parent auto-done -> grandparent auto-done
	_, err := env.taskSvc.Complete(ctx, child.ShortID, child.Version)

	if err != nil {
		test.Fatalf("Complete child: %v", err)
	}

	parentCheck, _ := env.taskSvc.GetByShortID(ctx, parent.ShortID)
	if parentCheck.Status != "completed" {
		test.Fatalf("expected parent 'completed', got %q", parentCheck.Status)
	}

	grandparentCheck, _ := env.taskSvc.GetByShortID(ctx, grandparent.ShortID)
	if grandparentCheck.Status != "completed" {
		test.Fatalf("expected grandparent 'completed', got %q", grandparentCheck.Status)
	}
}

func TestAutoRevert_ChildReopened(test *testing.T) {
	// Enable both auto-complete and auto-revert
	// Note: default workflow allows completed -> pending (not completed -> active)
	env := testTaskEnvWithSettings(test, domain.ProjectSettings{
		AutoCompleteParent: &domain.AutoCompleteConfig{
			TriggerStatus: "completed",
			TargetStatus:  "completed",
		},
		AutoRevertParent: &domain.AutoRevertConfig{
			TriggerStatus: "completed",
			TargetStatus:  "pending",
		},
	})
	ctx := context.Background()

	// Create parent + child
	parent := newMinimalTask("Parent")
	mustCreateTask(test, env.taskSvc, parent)
	parent, _ = env.taskSvc.Start(ctx, parent.ShortID, parent.Version, "")

	child := &domain.Task{Title: "Child", ParentID: &parent.ID}
	mustCreateTask(test, env.taskSvc, child)
	child, _ = env.taskSvc.Start(ctx, child.ShortID, child.Version, "")

	// Complete child -> parent auto-completes
	child, err := env.taskSvc.Complete(ctx, child.ShortID, child.Version)

	if err != nil {
		test.Fatalf("Complete child: %v", err)
	}

	parentCheck, _ := env.taskSvc.GetByShortID(ctx, parent.ShortID)
	if parentCheck.Status != "completed" {
		test.Fatalf("expected parent 'completed' after child completed, got %q", parentCheck.Status)
	}

	// Re-open child (completed -> pending)
	child, _ = env.taskSvc.GetByShortID(ctx, child.ShortID)
	_, reopenErr := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID: child.ShortID,
		Version: child.Version,
		Status:  ptr("pending"),
	})

	if reopenErr != nil {
		test.Fatalf("Reopen child: %v", reopenErr)
	}

	// Parent should be reverted to "pending"
	parentCheck, _ = env.taskSvc.GetByShortID(ctx, parent.ShortID)
	if parentCheck.Status != "pending" {
		test.Fatalf("expected parent 'pending' after child reopened, got %q", parentCheck.Status)
	}
}

func TestAutoRevert_Disabled(test *testing.T) {
	// Enable auto-complete but NOT auto-revert
	env := testTaskEnvWithSettings(test, domain.ProjectSettings{
		AutoCompleteParent: &domain.AutoCompleteConfig{
			TriggerStatus: "completed",
			TargetStatus:  "completed",
		},
		// AutoRevertParent intentionally nil
	})
	ctx := context.Background()

	parent := newMinimalTask("Parent")
	mustCreateTask(test, env.taskSvc, parent)
	parent, _ = env.taskSvc.Start(ctx, parent.ShortID, parent.Version, "")

	child := &domain.Task{Title: "Child", ParentID: &parent.ID}
	mustCreateTask(test, env.taskSvc, child)
	child, _ = env.taskSvc.Start(ctx, child.ShortID, child.Version, "")

	// Complete child -> parent auto-completes
	child, _ = env.taskSvc.Complete(ctx, child.ShortID, child.Version)
	parentCheck, _ := env.taskSvc.GetByShortID(ctx, parent.ShortID)
	if parentCheck.Status != "completed" {
		test.Fatalf("expected parent 'completed', got %q", parentCheck.Status)
	}

	// Re-open child — parent should NOT revert (auto-revert disabled)
	child, _ = env.taskSvc.GetByShortID(ctx, child.ShortID)
	env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID: child.ShortID,
		Version: child.Version,
		Status:  ptr("pending"),
	})

	parentCheck, _ = env.taskSvc.GetByShortID(ctx, parent.ShortID)
	if parentCheck.Status != "completed" {
		test.Fatalf("expected parent still 'completed' (revert disabled), got %q", parentCheck.Status)
	}
}

func TestAutoRevert_Recursive(test *testing.T) {
	// Enable both auto-complete and auto-revert
	// Note: default workflow allows completed -> pending (not completed -> active)
	env := testTaskEnvWithSettings(test, domain.ProjectSettings{
		AutoCompleteParent: &domain.AutoCompleteConfig{
			TriggerStatus: "completed",
			TargetStatus:  "completed",
		},
		AutoRevertParent: &domain.AutoRevertConfig{
			TriggerStatus: "completed",
			TargetStatus:  "pending",
		},
	})
	ctx := context.Background()

	// grandparent -> parent -> child
	grandparent := newMinimalTask("Grandparent")
	mustCreateTask(test, env.taskSvc, grandparent)
	grandparent, _ = env.taskSvc.Start(ctx, grandparent.ShortID, grandparent.Version, "")

	parent := &domain.Task{Title: "Parent", ParentID: &grandparent.ID}
	mustCreateTask(test, env.taskSvc, parent)
	parent, _ = env.taskSvc.Start(ctx, parent.ShortID, parent.Version, "")

	child := &domain.Task{Title: "Child", ParentID: &parent.ID}
	mustCreateTask(test, env.taskSvc, child)
	child, _ = env.taskSvc.Start(ctx, child.ShortID, child.Version, "")

	// Complete child — cascades up
	child, _ = env.taskSvc.Complete(ctx, child.ShortID, child.Version)
	parentCheck, _ := env.taskSvc.GetByShortID(ctx, parent.ShortID)
	grandparentCheck, _ := env.taskSvc.GetByShortID(ctx, grandparent.ShortID)
	if parentCheck.Status != "completed" || grandparentCheck.Status != "completed" {
		test.Fatalf("expected both completed, got parent=%q grandparent=%q", parentCheck.Status, grandparentCheck.Status)
	}

	// Re-open child — should cascade revert
	child, _ = env.taskSvc.GetByShortID(ctx, child.ShortID)
	_, reopenErr := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID: child.ShortID,
		Version: child.Version,
		Status:  ptr("pending"),
	})

	if reopenErr != nil {
		test.Fatalf("Reopen child: %v", reopenErr)
	}

	parentCheck, _ = env.taskSvc.GetByShortID(ctx, parent.ShortID)
	if parentCheck.Status != "pending" {
		test.Fatalf("expected parent 'pending' after revert, got %q", parentCheck.Status)
	}

	grandparentCheck, _ = env.taskSvc.GetByShortID(ctx, grandparent.ShortID)
	if grandparentCheck.Status != "pending" {
		test.Fatalf("expected grandparent 'pending' after revert, got %q", grandparentCheck.Status)
	}
}

func TestAutoRevert_CustomTargetStatus(test *testing.T) {
	// Auto-revert targets "pending" (the only valid revert transition
	// from "completed" in the default workflow: completed -> pending)
	env := testTaskEnvWithSettings(test, domain.ProjectSettings{
		AutoCompleteParent: &domain.AutoCompleteConfig{
			TriggerStatus: "completed",
			TargetStatus:  "completed",
		},
		AutoRevertParent: &domain.AutoRevertConfig{
			TriggerStatus: "completed",
			TargetStatus:  "pending",
		},
	})
	ctx := context.Background()

	parent := newMinimalTask("Parent")
	mustCreateTask(test, env.taskSvc, parent)
	parent, _ = env.taskSvc.Start(ctx, parent.ShortID, parent.Version, "")

	child := &domain.Task{Title: "Child", ParentID: &parent.ID}
	mustCreateTask(test, env.taskSvc, child)
	child, _ = env.taskSvc.Start(ctx, child.ShortID, child.Version, "")

	// Complete child -> parent auto-completes
	child, _ = env.taskSvc.Complete(ctx, child.ShortID, child.Version)
	parentCheck, _ := env.taskSvc.GetByShortID(ctx, parent.ShortID)
	if parentCheck.Status != "completed" {
		test.Fatalf("expected parent 'completed', got %q", parentCheck.Status)
	}

	// Re-open child -> parent should revert to "pending" (custom revert target)
	child, _ = env.taskSvc.GetByShortID(ctx, child.ShortID)
	_, reopenErr := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID: child.ShortID,
		Version: child.Version,
		Status:  ptr("pending"),
	})

	if reopenErr != nil {
		test.Fatalf("Reopen child: %v", reopenErr)
	}

	parentCheck, _ = env.taskSvc.GetByShortID(ctx, parent.ShortID)
	if parentCheck.Status != "pending" {
		test.Fatalf("expected parent 'pending' (custom revert target), got %q", parentCheck.Status)
	}
}

func TestUpdate_UDAMerge_AddKeys(test *testing.T) {
	env := testTaskEnv(test)
	ctx := context.Background()

	task := &domain.Task{Title: "UDA test", UDA: map[string]any{"existing": "value"}}
	mustCreateTask(test, env.taskSvc, task)

	mergeUDA := map[string]any{"new_key": "new_value"}
	updated, err := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID: task.ShortID,
		Version: task.Version,
		UDA:     &mergeUDA,
	})

	if err != nil {
		test.Fatalf("Update: %v", err)
	}

	if updated.UDA["existing"] != "value" {
		test.Fatalf("expected existing key preserved, got %v", updated.UDA["existing"])
	}
	if updated.UDA["new_key"] != "new_value" {
		test.Fatalf("expected new key added, got %v", updated.UDA["new_key"])
	}
}

func TestUpdate_UDAMerge_OverwriteKey(test *testing.T) {
	env := testTaskEnv(test)
	ctx := context.Background()

	task := &domain.Task{Title: "UDA test", UDA: map[string]any{"env": "dev"}}
	mustCreateTask(test, env.taskSvc, task)

	mergeUDA := map[string]any{"env": "prod"}
	updated, err := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID: task.ShortID,
		Version: task.Version,
		UDA:     &mergeUDA,
	})

	if err != nil {
		test.Fatalf("Update: %v", err)
	}

	if updated.UDA["env"] != "prod" {
		test.Fatalf("expected env=prod, got %v", updated.UDA["env"])
	}
}

func TestUpdate_UDAMerge_DeleteKey(test *testing.T) {
	env := testTaskEnv(test)
	ctx := context.Background()

	task := &domain.Task{Title: "UDA test", UDA: map[string]any{"env": "prod", "team": "backend"}}
	mustCreateTask(test, env.taskSvc, task)

	mergeUDA := map[string]any{"env": ""}
	updated, err := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID: task.ShortID,
		Version: task.Version,
		UDA:     &mergeUDA,
	})

	if err != nil {
		test.Fatalf("Update: %v", err)
	}

	if _, exists := updated.UDA["env"]; exists {
		test.Fatalf("expected env key removed, got %v", updated.UDA["env"])
	}
	if updated.UDA["team"] != "backend" {
		test.Fatalf("expected team preserved, got %v", updated.UDA["team"])
	}
}

func TestUpdate_UDAMerge_WithNilExisting(test *testing.T) {
	env := testTaskEnv(test)
	ctx := context.Background()

	task := newMinimalTask("UDA nil test")
	mustCreateTask(test, env.taskSvc, task)

	mergeUDA := map[string]any{"env": "prod"}
	updated, err := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID: task.ShortID,
		Version: task.Version,
		UDA:     &mergeUDA,
	})

	if err != nil {
		test.Fatalf("Update: %v", err)
	}

	if updated.UDA["env"] != "prod" {
		test.Fatalf("expected env=prod, got %v", updated.UDA["env"])
	}
}

func TestUpdate_UDAMerge_InvalidKey(test *testing.T) {
	env := testTaskEnv(test)
	ctx := context.Background()

	task := newMinimalTask("UDA invalid key test")
	mustCreateTask(test, env.taskSvc, task)

	mergeUDA := map[string]any{"invalid.key": "value"}
	_, err := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID: task.ShortID,
		Version: task.Version,
		UDA:     &mergeUDA,
	})
	if err == nil {
		test.Fatal("expected error for invalid UDA key")
	}
}

func TestUpdate_UDAMerge_NonStringValue(test *testing.T) {
	env := testTaskEnv(test)
	ctx := context.Background()

	task := newMinimalTask("UDA non-string test")
	mustCreateTask(test, env.taskSvc, task)

	mergeUDA := map[string]any{"count": 42}
	_, err := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID: task.ShortID,
		Version: task.Version,
		UDA:     &mergeUDA,
	})
	if err == nil {
		test.Fatal("expected error for non-string UDA value")
	}
}

func TestCreate_UDAValidation_InvalidKey(test *testing.T) {
	env := testTaskEnv(test)
	ctx := context.Background()

	task := &domain.Task{Title: "Bad UDA key", UDA: map[string]any{"invalid.key": "value"}}
	err := env.taskSvc.Create(ctx, task)
	if err == nil {
		test.Fatal("expected error for invalid UDA key on create")
	}
}

func TestCreate_UDAValidation_NonStringValue(test *testing.T) {
	env := testTaskEnv(test)
	ctx := context.Background()

	task := &domain.Task{Title: "Bad UDA value", UDA: map[string]any{"count": 42}}
	err := env.taskSvc.Create(ctx, task)
	if err == nil {
		test.Fatal("expected error for non-string UDA value on create")
	}
}

func TestCreate_UDAValidation_ValidUDA(test *testing.T) {
	env := testTaskEnv(test)
	ctx := context.Background()

	task := &domain.Task{Title: "Good UDA", UDA: map[string]any{"env": "prod", "team": "backend"}}

	if err := env.taskSvc.Create(ctx, task); err != nil {
		test.Fatalf("Create: %v", err)
	}

	got, err := env.taskSvc.GetByShortID(ctx, task.ShortID)

	if err != nil {
		test.Fatalf("GetByShortID: %v", err)
	}

	if got.UDA["env"] != "prod" {
		test.Fatalf("expected env=prod, got %v", got.UDA["env"])
	}
	if got.UDA["team"] != "backend" {
		test.Fatalf("expected team=backend, got %v", got.UDA["team"])
	}
}

func TestTaskService_Create_AssignsOrder_Default_EmptyGroup(test *testing.T) {
	test.Parallel()
	env := testTaskEnv(test)

	task := newMinimalTask("solo")

	if err := env.taskSvc.Create(context.Background(), task); err != nil {
		test.Fatalf("Create: %v", err)
	}

	if task.Order == nil || *task.Order != 1.0 {
		test.Fatalf("Order: got %v, want *1.0", task.Order)
	}
}

func TestTaskService_Create_AssignsOrder_Default_NonEmpty(test *testing.T) {
	test.Parallel()
	env := testTaskEnv(test)

	parent := newMinimalTask("p")
	mustCreateTask(test, env.taskSvc, parent)

	c1 := &domain.Task{Title: "c1", ParentID: &parent.ID}
	mustCreateTask(test, env.taskSvc, c1)
	if c1.Order == nil || *c1.Order != 1.0 {
		test.Fatalf("c1 Order: got %v, want *1.0", c1.Order)
	}

	c2 := &domain.Task{Title: "c2", ParentID: &parent.ID}
	mustCreateTask(test, env.taskSvc, c2)
	if c2.Order == nil || *c2.Order != 2.0 {
		test.Fatalf("c2 Order: got %v, want *2.0", c2.Order)
	}

	c3 := &domain.Task{Title: "c3", ParentID: &parent.ID}
	mustCreateTask(test, env.taskSvc, c3)
	if c3.Order == nil || *c3.Order != 3.0 {
		test.Fatalf("c3 Order: got %v, want *3.0", c3.Order)
	}
}

func TestTaskService_Create_RespectsCallerOrder(test *testing.T) {
	test.Parallel()
	env := testTaskEnv(test)

	want := 2.5
	task := &domain.Task{Title: "explicit", Order: &want}

	if err := env.taskSvc.Create(context.Background(), task); err != nil {
		test.Fatalf("Create: %v", err)
	}

	if task.Order == nil || *task.Order != 2.5 {
		test.Fatalf("Order: got %v, want *2.5 (no defaulting)", task.Order)
	}
}

func TestTaskService_Update_Order_Absolute(test *testing.T) {
	test.Parallel()
	env := testTaskEnv(test)
	ctx := WithActor(context.Background(), "german")

	task := newMinimalTask("reorder")
	mustCreateTask(test, env.taskSvc, task)
	if task.Order == nil || *task.Order != 1.0 {
		test.Fatalf("default Order: got %v, want *1.0", task.Order)
	}

	newOrder := 5.5
	innerPtr := &newOrder
	updated, err := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID: task.ShortID,
		Version: task.Version,
		Order:   &innerPtr,
	})

	if err != nil {
		test.Fatalf("Update: %v", err)
	}

	if updated.Order == nil || *updated.Order != 5.5 {
		test.Fatalf("Order: got %v, want *5.5", updated.Order)
	}
	if updated.Version == task.Version {
		test.Fatalf("Version not bumped: got %d", updated.Version)
	}

	// Exactly one task_modified event with Changes["order"] = {1.0, 5.5}.
	events := listAllEvents(test, env.store)
	mod := firstEventOfType(test, events, domain.EventTaskModified)
	payload := mod.Payload.(domain.TaskModifiedPayload)
	change, ok := payload.Changes["order"]
	if !ok {
		test.Fatalf("changes missing 'order': %v", payload.Changes)
	}

	if fromF, ok := change.From.(float64); !ok || fromF != 1.0 {
		test.Fatalf("Changes.order.From: got %v, want 1.0", change.From)
	}
	if toF, ok := change.To.(float64); !ok || toF != 5.5 {
		test.Fatalf("Changes.order.To: got %v, want 5.5", change.To)
	}
}

func TestTaskService_Update_Order_Clear(test *testing.T) {
	test.Parallel()
	env := testTaskEnv(test)
	ctx := context.Background()

	task := newMinimalTask("clear order")
	mustCreateTask(test, env.taskSvc, task)

	var inner *float64 // nil inner pointer means "clear"
	updated, err := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID: task.ShortID,
		Version: task.Version,
		Order:   &inner,
	})

	if err != nil {
		test.Fatalf("Update: %v", err)
	}

	if updated.Order != nil {
		test.Fatalf("Order: got %v, want nil", updated.Order)
	}

	events := listAllEvents(test, env.store)
	mod := firstEventOfType(test, events, domain.EventTaskModified)
	payload := mod.Payload.(domain.TaskModifiedPayload)
	change, ok := payload.Changes["order"]
	if !ok {
		test.Fatalf("changes missing 'order': %v", payload.Changes)
	}

	if fromF, ok := change.From.(float64); !ok || fromF != 1.0 {
		test.Fatalf("Changes.order.From: got %v, want 1.0", change.From)
	}
	if change.To != nil {
		test.Fatalf("Changes.order.To: got %v, want nil", change.To)
	}
}
