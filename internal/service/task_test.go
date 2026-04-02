package service

import (
	"context"
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
