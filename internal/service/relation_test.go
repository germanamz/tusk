package service

import (
	"context"
	"errors"
	"testing"

	"github.com/germanamz/tusk/internal/domain"
	"github.com/germanamz/tusk/internal/sqlite"
	"github.com/germanamz/tusk/migrations"
)

type testRelationEnv struct {
	relationSvc *RelationService
	taskSvc     *TaskService
	store       *sqlite.Store
}

// testRelationEnv creates a fully wired test environment for RelationService tests.
// The DB has all migrations applied, including the _default project and default workflow.
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
	projectRepo := sqlite.NewProjectRepo(db)
	workflowRepo := sqlite.NewWorkflowRepo(db)
	relationRepo := sqlite.NewRelationRepo(db)

	workflowSvc := NewWorkflowService(workflowRepo)
	taskSvc := NewTaskService(taskRepo, annotationRepo, projectRepo, workflowSvc)
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
