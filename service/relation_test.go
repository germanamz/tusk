package service

import (
	"context"
	"errors"
	"testing"

	"github.com/germanamz/tusk/config"
	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/inmem"
	"github.com/germanamz/tusk/migrations"
	"github.com/germanamz/tusk/sqlite"
)

type testRelationEnv struct {
	relationSvc *RelationService
	taskSvc     *TaskService
	store       *sqlite.Store
}

// testRelationEnv creates a fully wired test environment for RelationService tests.
// The DB has all migrations applied, including the default project and kanban workflow.
func newTestRelationEnv(t *testing.T) *testRelationEnv {
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
	relationRepo := sqlite.NewRelationRepo(db)

	workflowSvc := NewWorkflowService(workflowRepo, projectRepo)
	taskSvc := NewTaskService(taskRepo, annotationRepo, relationRepo, nil, projectRepo, workflowSvc, store, nil, nil)
	relationSvc := NewRelationService(relationRepo, taskRepo, store)

	return &testRelationEnv{
		relationSvc: relationSvc,
		taskSvc:     taskSvc,
		store:       store,
	}
}

// createTask is a helper that creates a task with the given title and returns it.
func (e *testRelationEnv) createTask(t *testing.T, title string) *domain.Task {
	t.Helper()
	task := &domain.Task{Title: title}
	if err := e.taskSvc.Create(context.Background(), task); err != nil {
		t.Fatalf("creating task %q: %v", title, err)
	}
	return task
}

func TestRelationAdd(t *testing.T) {
	env := newTestRelationEnv(t)
	ctx := context.Background()

	taskA := env.createTask(t, "Task A")
	taskB := env.createTask(t, "Task B")

	rel, err := env.relationSvc.Add(ctx, taskA.ShortID, taskB.ShortID, "relates_to")
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	if rel.SourceID != taskA.ID {
		t.Errorf("SourceID = %v, want %v", rel.SourceID, taskA.ID)
	}
	if rel.TargetID != taskB.ID {
		t.Errorf("TargetID = %v, want %v", rel.TargetID, taskB.ID)
	}
	if rel.RelationType != "relates_to" {
		t.Errorf("RelationType = %q, want %q", rel.RelationType, "relates_to")
	}
	if rel.ID.String() == "" {
		t.Error("expected non-empty ID")
	}
	if rel.CreatedAt.IsZero() {
		t.Error("expected non-zero CreatedAt")
	}
}

func TestRelationAddInvalidType(t *testing.T) {
	env := newTestRelationEnv(t)
	ctx := context.Background()

	taskA := env.createTask(t, "Task A")
	taskB := env.createTask(t, "Task B")

	_, err := env.relationSvc.Add(ctx, taskA.ShortID, taskB.ShortID, "depends_on")
	if err == nil {
		t.Fatal("expected error for invalid relation type")
	}
}

func TestRelationAddTaskNotFound(t *testing.T) {
	env := newTestRelationEnv(t)
	ctx := context.Background()

	taskA := env.createTask(t, "Task A")

	_, err := env.relationSvc.Add(ctx, taskA.ShortID, "nonexist", "relates_to")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got: %v", err)
	}
}

func TestRelationAddDuplicate(t *testing.T) {
	env := newTestRelationEnv(t)
	ctx := context.Background()

	taskA := env.createTask(t, "Task A")
	taskB := env.createTask(t, "Task B")

	_, err := env.relationSvc.Add(ctx, taskA.ShortID, taskB.ShortID, "relates_to")
	if err != nil {
		t.Fatalf("first Add: %v", err)
	}

	_, err = env.relationSvc.Add(ctx, taskA.ShortID, taskB.ShortID, "relates_to")
	if !errors.Is(err, domain.ErrDuplicateRelation) {
		t.Fatalf("expected ErrDuplicateRelation, got: %v", err)
	}
}

func TestRelationAddBlocksSelfReference(t *testing.T) {
	env := newTestRelationEnv(t)
	ctx := context.Background()

	taskA := env.createTask(t, "Task A")

	_, err := env.relationSvc.Add(ctx, taskA.ShortID, taskA.ShortID, "blocks")
	if !errors.Is(err, domain.ErrCyclicBlock) {
		t.Fatalf("expected ErrCyclicBlock for self-reference, got: %v", err)
	}
}

func TestRelationAddBlocksDirectCycle(t *testing.T) {
	env := newTestRelationEnv(t)
	ctx := context.Background()

	taskA := env.createTask(t, "Task A")
	taskB := env.createTask(t, "Task B")

	// A blocks B — should succeed
	_, err := env.relationSvc.Add(ctx, taskA.ShortID, taskB.ShortID, "blocks")
	if err != nil {
		t.Fatalf("A blocks B: %v", err)
	}

	// B blocks A — should fail (cycle: A->B->A)
	_, err = env.relationSvc.Add(ctx, taskB.ShortID, taskA.ShortID, "blocks")
	if !errors.Is(err, domain.ErrCyclicBlock) {
		t.Fatalf("expected ErrCyclicBlock for B blocks A, got: %v", err)
	}
}

func TestRelationAddBlocksTransitiveCycle(t *testing.T) {
	env := newTestRelationEnv(t)
	ctx := context.Background()

	taskA := env.createTask(t, "Task A")
	taskB := env.createTask(t, "Task B")
	taskC := env.createTask(t, "Task C")

	// A blocks B
	_, err := env.relationSvc.Add(ctx, taskA.ShortID, taskB.ShortID, "blocks")
	if err != nil {
		t.Fatalf("A blocks B: %v", err)
	}

	// B blocks C
	_, err = env.relationSvc.Add(ctx, taskB.ShortID, taskC.ShortID, "blocks")
	if err != nil {
		t.Fatalf("B blocks C: %v", err)
	}

	// C blocks A — should fail (cycle: A->B->C->A)
	_, err = env.relationSvc.Add(ctx, taskC.ShortID, taskA.ShortID, "blocks")
	if !errors.Is(err, domain.ErrCyclicBlock) {
		t.Fatalf("expected ErrCyclicBlock for C blocks A, got: %v", err)
	}
}

func TestRelationAddBlocksNoCycle(t *testing.T) {
	env := newTestRelationEnv(t)
	ctx := context.Background()

	taskA := env.createTask(t, "Task A")
	taskB := env.createTask(t, "Task B")
	taskC := env.createTask(t, "Task C")

	// A blocks B
	_, err := env.relationSvc.Add(ctx, taskA.ShortID, taskB.ShortID, "blocks")
	if err != nil {
		t.Fatalf("A blocks B: %v", err)
	}

	// B blocks C — should succeed (A->B->C is a chain, not a cycle)
	_, err = env.relationSvc.Add(ctx, taskB.ShortID, taskC.ShortID, "blocks")
	if err != nil {
		t.Fatalf("B blocks C: %v", err)
	}
}

func TestRelationNonBlocksAllowsBidirectional(t *testing.T) {
	env := newTestRelationEnv(t)
	ctx := context.Background()

	taskA := env.createTask(t, "Task A")
	taskB := env.createTask(t, "Task B")

	// A relates_to B
	_, err := env.relationSvc.Add(ctx, taskA.ShortID, taskB.ShortID, "relates_to")
	if err != nil {
		t.Fatalf("A relates_to B: %v", err)
	}

	// B relates_to A — should succeed (no cycle check for non-blocks)
	_, err = env.relationSvc.Add(ctx, taskB.ShortID, taskA.ShortID, "relates_to")
	if err != nil {
		t.Fatalf("B relates_to A: %v", err)
	}
}

func TestRelationRemove(t *testing.T) {
	env := newTestRelationEnv(t)
	ctx := context.Background()

	taskA := env.createTask(t, "Task A")
	taskB := env.createTask(t, "Task B")

	// Create a relation
	_, err := env.relationSvc.Add(ctx, taskA.ShortID, taskB.ShortID, "blocks")
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	// Remove it
	err = env.relationSvc.Remove(ctx, taskA.ShortID, taskB.ShortID, "blocks")
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}

	// Verify it's gone
	rels, err := env.relationSvc.GetByTask(ctx, taskA.ShortID)
	if err != nil {
		t.Fatalf("GetByTask: %v", err)
	}
	if len(rels) != 0 {
		t.Fatalf("expected 0 relations after remove, got %d", len(rels))
	}
}

func TestRelationRemoveNotFound(t *testing.T) {
	env := newTestRelationEnv(t)
	ctx := context.Background()

	taskA := env.createTask(t, "Task A")
	taskB := env.createTask(t, "Task B")

	// Try to remove a relation that doesn't exist
	err := env.relationSvc.Remove(ctx, taskA.ShortID, taskB.ShortID, "blocks")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got: %v", err)
	}
}

func TestRelationGetByTask(t *testing.T) {
	env := newTestRelationEnv(t)
	ctx := context.Background()

	taskA := env.createTask(t, "Task A")
	taskB := env.createTask(t, "Task B")
	taskC := env.createTask(t, "Task C")

	// A blocks B
	_, err := env.relationSvc.Add(ctx, taskA.ShortID, taskB.ShortID, "blocks")
	if err != nil {
		t.Fatalf("A blocks B: %v", err)
	}

	// C relates_to A
	_, err = env.relationSvc.Add(ctx, taskC.ShortID, taskA.ShortID, "relates_to")
	if err != nil {
		t.Fatalf("C relates_to A: %v", err)
	}

	// GetByTask for A should return both relations
	rels, err := env.relationSvc.GetByTask(ctx, taskA.ShortID)
	if err != nil {
		t.Fatalf("GetByTask: %v", err)
	}
	if len(rels) != 2 {
		t.Fatalf("expected 2 relations, got %d", len(rels))
	}
}

func TestRelationGetByTaskNotFound(t *testing.T) {
	env := newTestRelationEnv(t)
	ctx := context.Background()

	_, err := env.relationSvc.GetByTask(ctx, "nonexist")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got: %v", err)
	}
}

func TestRelationGetByTaskEmpty(t *testing.T) {
	env := newTestRelationEnv(t)
	ctx := context.Background()

	taskA := env.createTask(t, "Task A")

	rels, err := env.relationSvc.GetByTask(ctx, taskA.ShortID)
	if err != nil {
		t.Fatalf("GetByTask: %v", err)
	}
	if len(rels) != 0 {
		t.Fatalf("expected 0 relations, got %d", len(rels))
	}
}
