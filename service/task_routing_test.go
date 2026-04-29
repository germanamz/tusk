package service

import (
	"context"
	"testing"

	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/sqlite/sqlitetest"
	"github.com/google/uuid"
)

// multiProjectTaskSvc wires a TaskService over a single workspace bundle
// that answers for multiple project IDs. It returns a name→UUID lookup
// so tests can stamp the right typed ProjectID onto fixtures.
func multiProjectTaskSvc(test *testing.T) (*TaskService, *RepoBundle, map[string]uuid.UUID) {
	test.Helper()
	bundle, projectRepo, workflowRepo := newSeededBundle(test)
	sqlitetest.SeedProject(test, projectRepo, "backend")

	ids := map[string]uuid.UUID{}

	for _, name := range []string{"default", "backend"} {
		project, err := projectRepo.GetByName(context.Background(), name)

		if err != nil {
			test.Fatalf("resolving project %q: %v", name, err)
		}

		ids[name] = project.ID
	}

	resolver, projects := singleBundleResolver(bundle, ids["default"], ids["backend"])
	workflowSvc := NewWorkflowService(workflowRepo, projectRepo)
	svc := NewTaskService(resolver, projects, projectRepo, nil, workflowSvc, nil)
	return svc, bundle, ids
}

func TestTaskService_CreateRoutesToWorkspaceBundle(test *testing.T) {
	ctx := context.Background()
	svc, bundle, ids := multiProjectTaskSvc(test)

	task := &domain.Task{Title: "backend task", ProjectID: ids["backend"]}

	if err := svc.Create(ctx, task); err != nil {
		test.Fatalf("Create: %v", err)
	}

	got, err := bundle.Tasks.GetByID(ctx, task.ID)

	if err != nil {
		test.Fatalf("expected task in workspace bundle: %v", err)
	}

	if got.Title != "backend task" || got.ProjectID != ids["backend"] {
		test.Fatalf("unexpected task %+v", got)
	}
}

func TestTaskService_ListReturnsAllProjectsFromWorkspace(test *testing.T) {
	ctx := context.Background()
	svc, _, ids := multiProjectTaskSvc(test)

	for _, title := range []string{"d1", "d2"} {
		if err := svc.Create(ctx, &domain.Task{Title: title, ProjectID: ids["default"]}); err != nil {
			test.Fatalf("Create default %q: %v", title, err)
		}
	}

	if err := svc.Create(ctx, &domain.Task{Title: "b1", ProjectID: ids["backend"]}); err != nil {
		test.Fatalf("Create backend: %v", err)
	}

	all, err := svc.List(ctx, nil)

	if err != nil {
		test.Fatalf("List: %v", err)
	}

	if len(all) != 3 {
		test.Fatalf("expected 3 tasks, got %d", len(all))
	}
}

func TestTaskService_ListProjectFilterNarrowsResult(test *testing.T) {
	ctx := context.Background()
	svc, _, ids := multiProjectTaskSvc(test)

	if err := svc.Create(ctx, &domain.Task{Title: "d1", ProjectID: ids["default"]}); err != nil {
		test.Fatalf("Create default: %v", err)
	}

	if err := svc.Create(ctx, &domain.Task{Title: "b1", ProjectID: ids["backend"]}); err != nil {
		test.Fatalf("Create backend: %v", err)
	}

	backendID := ids["backend"]
	filter := &domain.TermFilter{TaskFilter: domain.TaskFilter{ProjectID: &backendID}}
	got, err := svc.List(ctx, filter)

	if err != nil {
		test.Fatalf("List: %v", err)
	}

	if len(got) != 1 || got[0].Title != "b1" {
		test.Fatalf("expected only 'b1', got %+v", got)
	}
}

func TestTaskService_AvailableReturnsAllProjects(test *testing.T) {
	ctx := context.Background()
	svc, _, ids := multiProjectTaskSvc(test)

	if err := svc.Create(ctx, &domain.Task{Title: "d1", ProjectID: ids["default"]}); err != nil {
		test.Fatalf("Create default: %v", err)
	}

	if err := svc.Create(ctx, &domain.Task{Title: "b1", ProjectID: ids["backend"]}); err != nil {
		test.Fatalf("Create backend: %v", err)
	}

	avail, err := svc.Available(ctx, nil)

	if err != nil {
		test.Fatalf("Available: %v", err)
	}

	if len(avail) != 2 {
		test.Fatalf("expected 2 available, got %d", len(avail))
	}
}

func TestTaskService_UpdateAllowsProjectMoveWithinWorkspace(test *testing.T) {
	ctx := context.Background()
	svc, bundle, ids := multiProjectTaskSvc(test)

	task := &domain.Task{Title: "t", ProjectID: ids["default"]}

	if createErr := svc.Create(ctx, task); createErr != nil {
		test.Fatalf("Create: %v", createErr)
	}

	newProj := ids["backend"]
	updated, updateErr := svc.Update(ctx, domain.TaskUpdate{
		ShortID:   task.ShortID,
		Version:   task.Version,
		ProjectID: &newProj,
	})

	if updateErr != nil {
		test.Fatalf("Update: %v", updateErr)
	}

	if updated.ProjectID != ids["backend"] {
		test.Fatalf("expected project=backend, got %v", updated.ProjectID)
	}

	got, getErr := bundle.Tasks.GetByID(ctx, task.ID)

	if getErr != nil {
		test.Fatalf("GetByID: %v", getErr)
	}

	if got.ProjectID != ids["backend"] {
		test.Fatalf("stored project mismatch: %v", got.ProjectID)
	}
}
