package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/germanamz/tusk/config"
	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/inmem"
	"github.com/germanamz/tusk/migrations"
	"github.com/germanamz/tusk/sqlite"
	"github.com/google/uuid"
)

// testTaskEnvWithSettings creates a test environment with custom project settings.
// This replaces the old pattern of creating a sqlite.ProjectRepo and calling Update.
func testTaskEnvWithSettings(t *testing.T, settings config.ProjectSettingsConfig) *testEnv {
	t.Helper()
	store, err := sqlite.New(":memory:", migrations.FS)
	if err != nil {
		t.Fatalf("opening test store: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	db := store.DB()
	taskRepo := sqlite.NewTaskRepo(db)
	annotationRepo := sqlite.NewAnnotationRepo(db)
	projectRepo := inmem.NewProjectRepository(map[string]config.ProjectConfig{
		"default": {Workflow: "kanban", Settings: settings},
	})
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

	workflowSvc := NewWorkflowService(workflowRepo, projectRepo)
	taskSvc := NewTaskService(taskRepo, annotationRepo, relationRepo, tagRepo, projectRepo, workflowSvc, store, nil, nil)

	return &testEnv{
		taskSvc:     taskSvc,
		workflowSvc: workflowSvc,
		store:       store,
	}
}

// testEnv holds all the services and repos needed for TaskService tests.
type testEnv struct {
	taskSvc     *TaskService
	workflowSvc *WorkflowService
	store       *sqlite.Store
}

func testTaskEnv(t *testing.T) *testEnv {
	t.Helper()
	return testTaskEnvWithSettings(t, config.ProjectSettingsConfig{})
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
	if task.ProjectID == "" {
		t.Fatal("expected ProjectID to be set to default")
	}
	if task.ProjectID != DefaultProjectID {
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

	task := newMinimalTask("Bad project")
	task.ProjectID = "nonexistent-project"
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
	if task.ProjectID == "" || task.ProjectID != DefaultProjectID {
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
	task := &domain.Task{
		Title:          "Full task",
		Description:    "All fields populated",
		Status:         "pending",
		Priority:       3,
		ProjectID:      DefaultProjectID,
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

	tasks, err := env.taskSvc.List(ctx, &domain.TermFilter{})
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
	tasks, err := env.taskSvc.List(ctx, &domain.TermFilter{TaskFilter: domain.TaskFilter{PriorityMin: &minPri}})
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

func TestUpdate_SetDescription(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	task := newMinimalTask("Has no description")
	mustCreateTask(t, env.taskSvc, task)

	// Set description via double-pointer
	desc := "A detailed description"
	dp := &desc
	updated, err := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID:     task.ShortID,
		Version:     1,
		Description: &dp,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Description != "A detailed description" {
		t.Fatalf("expected description %q, got %q", "A detailed description", updated.Description)
	}
}

func TestUpdate_ClearDescription(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	task := newMinimalTask("Has description")
	task.Description = "Will be cleared"
	mustCreateTask(t, env.taskSvc, task)

	// Verify description was set
	created, err := env.taskSvc.GetByShortID(ctx, task.ShortID)
	if err != nil {
		t.Fatalf("GetByShortID: %v", err)
	}
	if created.Description != "Will be cleared" {
		t.Fatalf("expected description %q, got %q", "Will be cleared", created.Description)
	}

	// Clear description via double-pointer (outer non-nil, inner nil)
	var nilStr *string
	updated, err := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID:     task.ShortID,
		Version:     1,
		Description: &nilStr,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Description != "" {
		t.Fatalf("expected empty description, got %q", updated.Description)
	}
}

func TestUpdate_NilDescriptionNoChange(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	task := newMinimalTask("Keep description")
	task.Description = "Should not change"
	mustCreateTask(t, env.taskSvc, task)

	// Update title only, leave description nil (no change)
	newTitle := "New title"
	updated, err := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID: task.ShortID,
		Version: 1,
		Title:   &newTitle,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Description != "Should not change" {
		t.Fatalf("expected description unchanged, got %q", updated.Description)
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

	updated, err := env.taskSvc.Start(ctx, task.ShortID, 1, "")
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

	_, err := env.taskSvc.Start(ctx, task.ShortID, 1, "")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	// active → active is a no-op (status unchanged), should succeed
	updated, err := env.taskSvc.Start(ctx, task.ShortID, 2, "")
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
	started, err := env.taskSvc.Start(ctx, task.ShortID, 1, "")
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

	started, _ := env.taskSvc.Start(ctx, task.ShortID, 1, "")
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

func TestCreate_CyclicParentRejected(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	// Create A
	taskA := newMinimalTask("Task A")
	mustCreateTask(t, env.taskSvc, taskA)

	// Create B with parent A
	taskB := newMinimalTask("Task B")
	taskB.ParentID = &taskA.ID
	mustCreateTask(t, env.taskSvc, taskB)

	// Create C with parent B — valid chain A -> B -> C, should succeed
	taskC := newMinimalTask("Task C")
	taskC.ParentID = &taskB.ID
	if err := env.taskSvc.Create(ctx, taskC); err != nil {
		t.Fatalf("Create with valid parent chain should succeed: %v", err)
	}
}

func TestUpdate_CyclicParentDirectRejected(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	// Create A
	taskA := newMinimalTask("Task A")
	mustCreateTask(t, env.taskSvc, taskA)

	// Create B with parent A
	taskB := newMinimalTask("Task B")
	taskB.ParentID = &taskA.ID
	mustCreateTask(t, env.taskSvc, taskB)

	// Try to set A's parent to B — should fail (cycle: A->B->A)
	parentRef := &taskB.ID
	_, err := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID:  taskA.ShortID,
		Version:  taskA.Version,
		ParentID: &parentRef,
	})
	if !errors.Is(err, domain.ErrCyclicParent) {
		t.Fatalf("expected ErrCyclicParent, got %v", err)
	}
}

func TestUpdate_StatusChange_Transactional(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	task := newMinimalTask("Transactional test")
	mustCreateTask(t, env.taskSvc, task)

	// Start the task (pending -> active) — this triggers the transactional path
	updated, err := env.taskSvc.Start(ctx, task.ShortID, task.Version, "")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if updated.Status != "active" {
		t.Fatalf("expected status 'active', got %q", updated.Status)
	}
	if updated.Version != 2 {
		t.Fatalf("expected version 2, got %d", updated.Version)
	}

	// Complete it (active -> completed)
	completed, err := env.taskSvc.Complete(ctx, updated.ShortID, updated.Version)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if completed.Status != "completed" {
		t.Fatalf("expected status 'completed', got %q", completed.Status)
	}
	if completed.Version != 3 {
		t.Fatalf("expected version 3, got %d", completed.Version)
	}
}

func TestTaskService_WithTxProvider(t *testing.T) {
	store, err := sqlite.New(":memory:", migrations.FS)
	if err != nil {
		t.Fatalf("opening store: %v", err)
	}
	defer store.Close()

	db := store.DB()
	taskRepo := sqlite.NewTaskRepo(db)
	annotationRepo := sqlite.NewAnnotationRepo(db)
	projectRepo := inmem.NewProjectRepository(map[string]config.ProjectConfig{
		"default": {Workflow: "kanban"},
	})
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

	workflowSvc := NewWorkflowService(workflowRepo, projectRepo)
	// Pass store as the TaskTxProvider
	taskSvc := NewTaskService(taskRepo, annotationRepo, nil, nil, projectRepo, workflowSvc, store, nil, nil)

	ctx := context.Background()
	task := newMinimalTask("Test with tx provider")
	if err := taskSvc.Create(ctx, task); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Start and complete — basic lifecycle still works
	_, err = taskSvc.Start(ctx, task.ShortID, task.Version, "")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	started, _ := taskSvc.GetByShortID(ctx, task.ShortID)

	_, err = taskSvc.Complete(ctx, started.ShortID, started.Version)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
}

func TestUpdate_CyclicParentTransitiveRejected(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	// Create chain: A -> B -> C (A is root, B's parent is A, C's parent is B)
	taskA := newMinimalTask("Task A")
	mustCreateTask(t, env.taskSvc, taskA)

	taskB := newMinimalTask("Task B")
	taskB.ParentID = &taskA.ID
	mustCreateTask(t, env.taskSvc, taskB)

	taskC := newMinimalTask("Task C")
	taskC.ParentID = &taskB.ID
	mustCreateTask(t, env.taskSvc, taskC)

	// Try to set A's parent to C — should fail (cycle: A->B->C->A)
	parentRef := &taskC.ID
	_, err := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID:  taskA.ShortID,
		Version:  taskA.Version,
		ParentID: &parentRef,
	})
	if !errors.Is(err, domain.ErrCyclicParent) {
		t.Fatalf("expected ErrCyclicParent, got %v", err)
	}
}

func TestUpdate_ReparentNoCycle(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	// Create three independent tasks
	taskA := newMinimalTask("Task A")
	mustCreateTask(t, env.taskSvc, taskA)

	taskB := newMinimalTask("Task B")
	mustCreateTask(t, env.taskSvc, taskB)

	taskC := newMinimalTask("Task C")
	mustCreateTask(t, env.taskSvc, taskC)

	// Set B's parent to A — should succeed (no cycle)
	parentRef := &taskA.ID
	updated, err := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID:  taskB.ShortID,
		Version:  taskB.Version,
		ParentID: &parentRef,
	})
	if err != nil {
		t.Fatalf("expected reparent to succeed, got %v", err)
	}
	if updated.ParentID == nil || *updated.ParentID != taskA.ID {
		t.Fatalf("expected parent to be task A")
	}
}

func TestAutoComplete_AllChildrenCompleted(t *testing.T) {
	env := testTaskEnvWithSettings(t, config.ProjectSettingsConfig{
		AutoCompleteParent: &config.AutoCompleteParentConfig{
			TriggerStatus: "completed",
			TargetStatus:  "completed",
		},
	})
	ctx := context.Background()

	// Create parent
	parent := newMinimalTask("Parent")
	mustCreateTask(t, env.taskSvc, parent)
	// Start parent (pending -> active) so it can later transition to completed
	parent, err := env.taskSvc.Start(ctx, parent.ShortID, parent.Version, "")
	if err != nil {
		t.Fatalf("Start parent: %v", err)
	}

	// Create two children
	child1 := &domain.Task{Title: "Child 1", ParentID: &parent.ID}
	mustCreateTask(t, env.taskSvc, child1)
	child2 := &domain.Task{Title: "Child 2", ParentID: &parent.ID}
	mustCreateTask(t, env.taskSvc, child2)

	// Start and complete child1
	child1, err = env.taskSvc.Start(ctx, child1.ShortID, child1.Version, "")
	if err != nil {
		t.Fatalf("Start child1: %v", err)
	}
	_, err = env.taskSvc.Complete(ctx, child1.ShortID, child1.Version)
	if err != nil {
		t.Fatalf("Complete child1: %v", err)
	}

	// Parent should NOT be auto-completed yet (child2 still pending)
	parentCheck, _ := env.taskSvc.GetByShortID(ctx, parent.ShortID)
	if parentCheck.Status != "active" {
		t.Fatalf("expected parent still 'active' after first child completed, got %q", parentCheck.Status)
	}

	// Start and complete child2
	child2, err = env.taskSvc.Start(ctx, child2.ShortID, child2.Version, "")
	if err != nil {
		t.Fatalf("Start child2: %v", err)
	}
	_, err = env.taskSvc.Complete(ctx, child2.ShortID, child2.Version)
	if err != nil {
		t.Fatalf("Complete child2: %v", err)
	}

	// Parent SHOULD be auto-completed now
	parentCheck, _ = env.taskSvc.GetByShortID(ctx, parent.ShortID)
	if parentCheck.Status != "completed" {
		t.Fatalf("expected parent 'completed' after all children completed, got %q", parentCheck.Status)
	}
}

func TestAutoComplete_Disabled_ByDefault(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	// Do NOT enable auto-complete — default settings

	parent := newMinimalTask("Parent")
	mustCreateTask(t, env.taskSvc, parent)
	parent, err := env.taskSvc.Start(ctx, parent.ShortID, parent.Version, "")
	if err != nil {
		t.Fatalf("Start parent: %v", err)
	}

	child := &domain.Task{Title: "Child", ParentID: &parent.ID}
	mustCreateTask(t, env.taskSvc, child)
	child, err = env.taskSvc.Start(ctx, child.ShortID, child.Version, "")
	if err != nil {
		t.Fatalf("Start child: %v", err)
	}
	_, err = env.taskSvc.Complete(ctx, child.ShortID, child.Version)
	if err != nil {
		t.Fatalf("Complete child: %v", err)
	}

	// Parent should NOT be auto-completed (feature disabled)
	parentCheck, _ := env.taskSvc.GetByShortID(ctx, parent.ShortID)
	if parentCheck.Status != "active" {
		t.Fatalf("expected parent still 'active' (propagation disabled), got %q", parentCheck.Status)
	}
}

func TestAutoComplete_DeletedChildrenIgnored(t *testing.T) {
	env := testTaskEnvWithSettings(t, config.ProjectSettingsConfig{
		AutoCompleteParent: &config.AutoCompleteParentConfig{
			TriggerStatus: "completed",
			TargetStatus:  "completed",
		},
	})
	ctx := context.Background()

	parent := newMinimalTask("Parent")
	mustCreateTask(t, env.taskSvc, parent)
	parent, _ = env.taskSvc.Start(ctx, parent.ShortID, parent.Version, "")

	child1 := &domain.Task{Title: "Child 1", ParentID: &parent.ID}
	mustCreateTask(t, env.taskSvc, child1)
	child2 := &domain.Task{Title: "Child 2", ParentID: &parent.ID}
	mustCreateTask(t, env.taskSvc, child2)

	// Delete child2
	_, _ = env.taskSvc.Delete(ctx, child2.ShortID, child2.Version)

	// Start and complete child1
	child1, _ = env.taskSvc.Start(ctx, child1.ShortID, child1.Version, "")
	_, _ = env.taskSvc.Complete(ctx, child1.ShortID, child1.Version)

	// Parent should be auto-completed (deleted child ignored)
	parentCheck, _ := env.taskSvc.GetByShortID(ctx, parent.ShortID)
	if parentCheck.Status != "completed" {
		t.Fatalf("expected parent 'completed' (deleted child ignored), got %q", parentCheck.Status)
	}
}

func TestAutoComplete_WorkflowGuard(t *testing.T) {
	env := testTaskEnvWithSettings(t, config.ProjectSettingsConfig{
		AutoCompleteParent: &config.AutoCompleteParentConfig{
			TriggerStatus: "completed",
			TargetStatus:  "completed",
		},
	})
	ctx := context.Background()

	// Create parent but do NOT start it — leave in "pending"
	parent := newMinimalTask("Parent pending")
	mustCreateTask(t, env.taskSvc, parent)

	child := &domain.Task{Title: "Child", ParentID: &parent.ID}
	mustCreateTask(t, env.taskSvc, child)
	child, _ = env.taskSvc.Start(ctx, child.ShortID, child.Version, "")
	_, _ = env.taskSvc.Complete(ctx, child.ShortID, child.Version)

	// Parent should NOT be auto-completed (pending -> completed is not allowed)
	parentCheck, _ := env.taskSvc.GetByShortID(ctx, parent.ShortID)
	if parentCheck.Status != "pending" {
		t.Fatalf("expected parent still 'pending' (workflow blocks transition), got %q", parentCheck.Status)
	}
}

func TestAutoComplete_Recursive(t *testing.T) {
	env := testTaskEnvWithSettings(t, config.ProjectSettingsConfig{
		AutoCompleteParent: &config.AutoCompleteParentConfig{
			TriggerStatus: "completed",
			TargetStatus:  "completed",
		},
	})
	ctx := context.Background()

	// Create grandparent -> parent -> child chain
	grandparent := newMinimalTask("Grandparent")
	mustCreateTask(t, env.taskSvc, grandparent)
	grandparent, _ = env.taskSvc.Start(ctx, grandparent.ShortID, grandparent.Version, "")

	parent := &domain.Task{Title: "Parent", ParentID: &grandparent.ID}
	mustCreateTask(t, env.taskSvc, parent)
	parent, _ = env.taskSvc.Start(ctx, parent.ShortID, parent.Version, "")

	child := &domain.Task{Title: "Child", ParentID: &parent.ID}
	mustCreateTask(t, env.taskSvc, child)
	child, _ = env.taskSvc.Start(ctx, child.ShortID, child.Version, "")

	// Complete child — should cascade: child done -> parent auto-done -> grandparent auto-done
	_, err := env.taskSvc.Complete(ctx, child.ShortID, child.Version)
	if err != nil {
		t.Fatalf("Complete child: %v", err)
	}

	parentCheck, _ := env.taskSvc.GetByShortID(ctx, parent.ShortID)
	if parentCheck.Status != "completed" {
		t.Fatalf("expected parent 'completed', got %q", parentCheck.Status)
	}

	grandparentCheck, _ := env.taskSvc.GetByShortID(ctx, grandparent.ShortID)
	if grandparentCheck.Status != "completed" {
		t.Fatalf("expected grandparent 'completed', got %q", grandparentCheck.Status)
	}
}

func TestAutoRevert_ChildReopened(t *testing.T) {
	// Enable both auto-complete and auto-revert
	// Note: default workflow allows completed -> pending (not completed -> active)
	env := testTaskEnvWithSettings(t, config.ProjectSettingsConfig{
		AutoCompleteParent: &config.AutoCompleteParentConfig{
			TriggerStatus: "completed",
			TargetStatus:  "completed",
		},
		AutoRevertParent: &config.AutoRevertParentConfig{
			TriggerStatus: "completed",
			TargetStatus:  "pending",
		},
	})
	ctx := context.Background()

	// Create parent + child
	parent := newMinimalTask("Parent")
	mustCreateTask(t, env.taskSvc, parent)
	parent, _ = env.taskSvc.Start(ctx, parent.ShortID, parent.Version, "")

	child := &domain.Task{Title: "Child", ParentID: &parent.ID}
	mustCreateTask(t, env.taskSvc, child)
	child, _ = env.taskSvc.Start(ctx, child.ShortID, child.Version, "")

	// Complete child -> parent auto-completes
	child, err := env.taskSvc.Complete(ctx, child.ShortID, child.Version)
	if err != nil {
		t.Fatalf("Complete child: %v", err)
	}
	parentCheck, _ := env.taskSvc.GetByShortID(ctx, parent.ShortID)
	if parentCheck.Status != "completed" {
		t.Fatalf("expected parent 'completed' after child completed, got %q", parentCheck.Status)
	}

	// Re-open child (completed -> pending)
	child, _ = env.taskSvc.GetByShortID(ctx, child.ShortID)
	_, err = env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID: child.ShortID,
		Version: child.Version,
		Status:  ptr("pending"),
	})
	if err != nil {
		t.Fatalf("Reopen child: %v", err)
	}

	// Parent should be reverted to "pending"
	parentCheck, _ = env.taskSvc.GetByShortID(ctx, parent.ShortID)
	if parentCheck.Status != "pending" {
		t.Fatalf("expected parent 'pending' after child reopened, got %q", parentCheck.Status)
	}
}

func TestAutoRevert_Disabled(t *testing.T) {
	// Enable auto-complete but NOT auto-revert
	env := testTaskEnvWithSettings(t, config.ProjectSettingsConfig{
		AutoCompleteParent: &config.AutoCompleteParentConfig{
			TriggerStatus: "completed",
			TargetStatus:  "completed",
		},
		// AutoRevertParent intentionally nil
	})
	ctx := context.Background()

	parent := newMinimalTask("Parent")
	mustCreateTask(t, env.taskSvc, parent)
	parent, _ = env.taskSvc.Start(ctx, parent.ShortID, parent.Version, "")

	child := &domain.Task{Title: "Child", ParentID: &parent.ID}
	mustCreateTask(t, env.taskSvc, child)
	child, _ = env.taskSvc.Start(ctx, child.ShortID, child.Version, "")

	// Complete child -> parent auto-completes
	child, _ = env.taskSvc.Complete(ctx, child.ShortID, child.Version)
	parentCheck, _ := env.taskSvc.GetByShortID(ctx, parent.ShortID)
	if parentCheck.Status != "completed" {
		t.Fatalf("expected parent 'completed', got %q", parentCheck.Status)
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
		t.Fatalf("expected parent still 'completed' (revert disabled), got %q", parentCheck.Status)
	}
}

func TestAutoRevert_Recursive(t *testing.T) {
	// Enable both auto-complete and auto-revert
	// Note: default workflow allows completed -> pending (not completed -> active)
	env := testTaskEnvWithSettings(t, config.ProjectSettingsConfig{
		AutoCompleteParent: &config.AutoCompleteParentConfig{
			TriggerStatus: "completed",
			TargetStatus:  "completed",
		},
		AutoRevertParent: &config.AutoRevertParentConfig{
			TriggerStatus: "completed",
			TargetStatus:  "pending",
		},
	})
	ctx := context.Background()

	// grandparent -> parent -> child
	grandparent := newMinimalTask("Grandparent")
	mustCreateTask(t, env.taskSvc, grandparent)
	grandparent, _ = env.taskSvc.Start(ctx, grandparent.ShortID, grandparent.Version, "")

	parent := &domain.Task{Title: "Parent", ParentID: &grandparent.ID}
	mustCreateTask(t, env.taskSvc, parent)
	parent, _ = env.taskSvc.Start(ctx, parent.ShortID, parent.Version, "")

	child := &domain.Task{Title: "Child", ParentID: &parent.ID}
	mustCreateTask(t, env.taskSvc, child)
	child, _ = env.taskSvc.Start(ctx, child.ShortID, child.Version, "")

	// Complete child — cascades up
	child, _ = env.taskSvc.Complete(ctx, child.ShortID, child.Version)
	parentCheck, _ := env.taskSvc.GetByShortID(ctx, parent.ShortID)
	grandparentCheck, _ := env.taskSvc.GetByShortID(ctx, grandparent.ShortID)
	if parentCheck.Status != "completed" || grandparentCheck.Status != "completed" {
		t.Fatalf("expected both completed, got parent=%q grandparent=%q", parentCheck.Status, grandparentCheck.Status)
	}

	// Re-open child — should cascade revert
	child, _ = env.taskSvc.GetByShortID(ctx, child.ShortID)
	_, err := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID: child.ShortID,
		Version: child.Version,
		Status:  ptr("pending"),
	})
	if err != nil {
		t.Fatalf("Reopen child: %v", err)
	}

	parentCheck, _ = env.taskSvc.GetByShortID(ctx, parent.ShortID)
	if parentCheck.Status != "pending" {
		t.Fatalf("expected parent 'pending' after revert, got %q", parentCheck.Status)
	}

	grandparentCheck, _ = env.taskSvc.GetByShortID(ctx, grandparent.ShortID)
	if grandparentCheck.Status != "pending" {
		t.Fatalf("expected grandparent 'pending' after revert, got %q", grandparentCheck.Status)
	}
}

func TestAutoRevert_CustomTargetStatus(t *testing.T) {
	// Auto-revert targets "pending" (the only valid revert transition
	// from "completed" in the default workflow: completed -> pending)
	env := testTaskEnvWithSettings(t, config.ProjectSettingsConfig{
		AutoCompleteParent: &config.AutoCompleteParentConfig{
			TriggerStatus: "completed",
			TargetStatus:  "completed",
		},
		AutoRevertParent: &config.AutoRevertParentConfig{
			TriggerStatus: "completed",
			TargetStatus:  "pending",
		},
	})
	ctx := context.Background()

	parent := newMinimalTask("Parent")
	mustCreateTask(t, env.taskSvc, parent)
	parent, _ = env.taskSvc.Start(ctx, parent.ShortID, parent.Version, "")

	child := &domain.Task{Title: "Child", ParentID: &parent.ID}
	mustCreateTask(t, env.taskSvc, child)
	child, _ = env.taskSvc.Start(ctx, child.ShortID, child.Version, "")

	// Complete child -> parent auto-completes
	child, _ = env.taskSvc.Complete(ctx, child.ShortID, child.Version)
	parentCheck, _ := env.taskSvc.GetByShortID(ctx, parent.ShortID)
	if parentCheck.Status != "completed" {
		t.Fatalf("expected parent 'completed', got %q", parentCheck.Status)
	}

	// Re-open child -> parent should revert to "pending" (custom revert target)
	child, _ = env.taskSvc.GetByShortID(ctx, child.ShortID)
	_, err := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID: child.ShortID,
		Version: child.Version,
		Status:  ptr("pending"),
	})
	if err != nil {
		t.Fatalf("Reopen child: %v", err)
	}

	parentCheck, _ = env.taskSvc.GetByShortID(ctx, parent.ShortID)
	if parentCheck.Status != "pending" {
		t.Fatalf("expected parent 'pending' (custom revert target), got %q", parentCheck.Status)
	}
}

func TestUpdate_UDAMerge_AddKeys(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	task := &domain.Task{Title: "UDA test", UDA: map[string]any{"existing": "value"}}
	mustCreateTask(t, env.taskSvc, task)

	mergeUDA := map[string]any{"new_key": "new_value"}
	updated, err := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID: task.ShortID,
		Version: task.Version,
		UDA:     &mergeUDA,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.UDA["existing"] != "value" {
		t.Fatalf("expected existing key preserved, got %v", updated.UDA["existing"])
	}
	if updated.UDA["new_key"] != "new_value" {
		t.Fatalf("expected new key added, got %v", updated.UDA["new_key"])
	}
}

func TestUpdate_UDAMerge_OverwriteKey(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	task := &domain.Task{Title: "UDA test", UDA: map[string]any{"env": "dev"}}
	mustCreateTask(t, env.taskSvc, task)

	mergeUDA := map[string]any{"env": "prod"}
	updated, err := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID: task.ShortID,
		Version: task.Version,
		UDA:     &mergeUDA,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.UDA["env"] != "prod" {
		t.Fatalf("expected env=prod, got %v", updated.UDA["env"])
	}
}

func TestUpdate_UDAMerge_DeleteKey(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	task := &domain.Task{Title: "UDA test", UDA: map[string]any{"env": "prod", "team": "backend"}}
	mustCreateTask(t, env.taskSvc, task)

	mergeUDA := map[string]any{"env": ""}
	updated, err := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID: task.ShortID,
		Version: task.Version,
		UDA:     &mergeUDA,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if _, exists := updated.UDA["env"]; exists {
		t.Fatalf("expected env key removed, got %v", updated.UDA["env"])
	}
	if updated.UDA["team"] != "backend" {
		t.Fatalf("expected team preserved, got %v", updated.UDA["team"])
	}
}

func TestUpdate_UDAMerge_WithNilExisting(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	task := newMinimalTask("UDA nil test")
	mustCreateTask(t, env.taskSvc, task)

	mergeUDA := map[string]any{"env": "prod"}
	updated, err := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID: task.ShortID,
		Version: task.Version,
		UDA:     &mergeUDA,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.UDA["env"] != "prod" {
		t.Fatalf("expected env=prod, got %v", updated.UDA["env"])
	}
}

func TestUpdate_UDAMerge_InvalidKey(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	task := newMinimalTask("UDA invalid key test")
	mustCreateTask(t, env.taskSvc, task)

	mergeUDA := map[string]any{"invalid.key": "value"}
	_, err := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID: task.ShortID,
		Version: task.Version,
		UDA:     &mergeUDA,
	})
	if err == nil {
		t.Fatal("expected error for invalid UDA key")
	}
}

func TestUpdate_UDAMerge_NonStringValue(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	task := newMinimalTask("UDA non-string test")
	mustCreateTask(t, env.taskSvc, task)

	mergeUDA := map[string]any{"count": 42}
	_, err := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID: task.ShortID,
		Version: task.Version,
		UDA:     &mergeUDA,
	})
	if err == nil {
		t.Fatal("expected error for non-string UDA value")
	}
}

func TestCreate_UDAValidation_InvalidKey(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	task := &domain.Task{Title: "Bad UDA key", UDA: map[string]any{"invalid.key": "value"}}
	err := env.taskSvc.Create(ctx, task)
	if err == nil {
		t.Fatal("expected error for invalid UDA key on create")
	}
}

func TestCreate_UDAValidation_NonStringValue(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	task := &domain.Task{Title: "Bad UDA value", UDA: map[string]any{"count": 42}}
	err := env.taskSvc.Create(ctx, task)
	if err == nil {
		t.Fatal("expected error for non-string UDA value on create")
	}
}

func TestCreate_UDAValidation_ValidUDA(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	task := &domain.Task{Title: "Good UDA", UDA: map[string]any{"env": "prod", "team": "backend"}}
	if err := env.taskSvc.Create(ctx, task); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := env.taskSvc.GetByShortID(ctx, task.ShortID)
	if err != nil {
		t.Fatalf("GetByShortID: %v", err)
	}
	if got.UDA["env"] != "prod" {
		t.Fatalf("expected env=prod, got %v", got.UDA["env"])
	}
	if got.UDA["team"] != "backend" {
		t.Fatalf("expected team=backend, got %v", got.UDA["team"])
	}
}
