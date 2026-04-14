package service

import (
	"context"
	"testing"

	"github.com/germanamz/tusk/config"
	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/sqlite"
	"github.com/google/uuid"
)

// multiProjectKanban seeds the given store with one kanban-backed project
// per id and returns the resulting SQLite project repo.
func multiProjectKanban(t *testing.T, store *sqlite.Store, projectIDs ...string) *sqlite.ProjectRepo {
	t.Helper()
	cfgProjects := map[string]config.ProjectConfig{}
	for _, id := range projectIDs {
		cfgProjects[id] = config.ProjectConfig{Workflow: "kanban"}
	}
	cfg := &config.Config{
		Workflows: map[string]config.WorkflowConfig{"kanban": {}},
		Projects:  cfgProjects,
	}
	projRepo, _ := sqlite.NewTestConfigRepos(t, store, cfg)
	return projRepo
}

func multiProjectWorkflowSvc(t *testing.T, store *sqlite.Store, projectRepo *sqlite.ProjectRepo) *WorkflowService {
	t.Helper()
	workflowRepo := sqlite.NewWorkflowRepo(store.DB())
	return NewWorkflowService(workflowRepo, projectRepo)
}

// multiProjectTaskSvc wires a TaskService over a single workspace bundle
// that answers for multiple project IDs. It returns a name→UUID lookup
// so tests can stamp the right typed ProjectID onto fixtures.
func multiProjectTaskSvc(t *testing.T) (*TaskService, *RepoBundle, map[string]uuid.UUID) {
	t.Helper()
	bundle := newTestBundle(t)
	projectRepo := multiProjectKanban(t, bundle.Store, "default", "backend")
	ids := map[string]uuid.UUID{}
	for _, name := range []string{"default", "backend"} {
		p, err := projectRepo.GetByName(context.Background(), name)
		if err != nil {
			t.Fatalf("resolving project %q: %v", name, err)
		}
		ids[name] = p.ID
	}
	resolver, projects := singleBundleResolver(bundle, ids["default"], ids["backend"])
	workflowSvc := multiProjectWorkflowSvc(t, bundle.Store, projectRepo)
	svc := NewTaskService(resolver, projects, projectRepo, workflowSvc, nil)
	return svc, bundle, ids
}

func TestTaskService_CreateRoutesToWorkspaceBundle(t *testing.T) {
	ctx := context.Background()
	svc, bundle, ids := multiProjectTaskSvc(t)

	task := &domain.Task{Title: "backend task", ProjectID: ids["backend"]}
	if err := svc.Create(ctx, task); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := bundle.Tasks.GetByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("expected task in workspace bundle: %v", err)
	}
	if got.Title != "backend task" || got.ProjectID != ids["backend"] {
		t.Fatalf("unexpected task %+v", got)
	}
}

func TestTaskService_ListReturnsAllProjectsFromWorkspace(t *testing.T) {
	ctx := context.Background()
	svc, _, ids := multiProjectTaskSvc(t)

	for _, title := range []string{"d1", "d2"} {
		if err := svc.Create(ctx, &domain.Task{Title: title, ProjectID: ids["default"]}); err != nil {
			t.Fatalf("Create default %q: %v", title, err)
		}
	}
	if err := svc.Create(ctx, &domain.Task{Title: "b1", ProjectID: ids["backend"]}); err != nil {
		t.Fatalf("Create backend: %v", err)
	}

	all, err := svc.List(ctx, nil)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("expected 3 tasks, got %d", len(all))
	}
}

func TestTaskService_ListProjectFilterNarrowsResult(t *testing.T) {
	ctx := context.Background()
	svc, _, ids := multiProjectTaskSvc(t)

	if err := svc.Create(ctx, &domain.Task{Title: "d1", ProjectID: ids["default"]}); err != nil {
		t.Fatalf("Create default: %v", err)
	}
	if err := svc.Create(ctx, &domain.Task{Title: "b1", ProjectID: ids["backend"]}); err != nil {
		t.Fatalf("Create backend: %v", err)
	}

	backendID := ids["backend"]
	filter := &domain.TermFilter{TaskFilter: domain.TaskFilter{ProjectID: &backendID}}
	got, err := svc.List(ctx, filter)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 || got[0].Title != "b1" {
		t.Fatalf("expected only 'b1', got %+v", got)
	}
}

func TestTaskService_AvailableReturnsAllProjects(t *testing.T) {
	ctx := context.Background()
	svc, _, ids := multiProjectTaskSvc(t)

	if err := svc.Create(ctx, &domain.Task{Title: "d1", ProjectID: ids["default"]}); err != nil {
		t.Fatalf("Create default: %v", err)
	}
	if err := svc.Create(ctx, &domain.Task{Title: "b1", ProjectID: ids["backend"]}); err != nil {
		t.Fatalf("Create backend: %v", err)
	}

	avail, err := svc.Available(ctx, nil)
	if err != nil {
		t.Fatalf("Available: %v", err)
	}
	if len(avail) != 2 {
		t.Fatalf("expected 2 available, got %d", len(avail))
	}
}

func TestTaskService_UpdateAllowsProjectMoveWithinWorkspace(t *testing.T) {
	ctx := context.Background()
	svc, bundle, ids := multiProjectTaskSvc(t)

	task := &domain.Task{Title: "t", ProjectID: ids["default"]}
	if err := svc.Create(ctx, task); err != nil {
		t.Fatalf("Create: %v", err)
	}
	newProj := ids["backend"]
	updated, err := svc.Update(ctx, domain.TaskUpdate{
		ShortID:   task.ShortID,
		Version:   task.Version,
		ProjectID: &newProj,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.ProjectID != ids["backend"] {
		t.Fatalf("expected project=backend, got %v", updated.ProjectID)
	}

	got, err := bundle.Tasks.GetByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.ProjectID != ids["backend"] {
		t.Fatalf("stored project mismatch: %v", got.ProjectID)
	}
}
