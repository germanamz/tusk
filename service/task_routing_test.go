package service

import (
	"context"
	"testing"

	"github.com/germanamz/tusk/config"
	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/inmem"
)

// multiProjectKanban builds a ProjectRepository with the given project
// IDs, all bound to the kanban workflow used by the test suite.
func multiProjectKanban(projectIDs ...string) *inmem.ProjectRepository {
	cfg := map[string]config.ProjectConfig{}
	for _, id := range projectIDs {
		cfg[id] = config.ProjectConfig{Workflow: "kanban"}
	}
	return inmem.NewProjectRepository(cfg)
}

func multiProjectWorkflowSvc(projectRepo *inmem.ProjectRepository) *WorkflowService {
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
	return NewWorkflowService(workflowRepo, projectRepo)
}

// multiProjectTaskSvc wires a TaskService over a single workspace bundle
// that answers for multiple project IDs.
func multiProjectTaskSvc(t *testing.T) (*TaskService, *RepoBundle) {
	t.Helper()
	bundle := newTestBundle(t)
	resolver, projects := singleBundleResolver(bundle, "default", "backend")
	projectRepo := multiProjectKanban("default", "backend")
	workflowSvc := multiProjectWorkflowSvc(projectRepo)
	svc := NewTaskService(resolver, projects, projectRepo, workflowSvc, nil)
	return svc, bundle
}

func TestTaskService_CreateRoutesToWorkspaceBundle(t *testing.T) {
	ctx := context.Background()
	svc, bundle := multiProjectTaskSvc(t)

	task := &domain.Task{Title: "backend task", ProjectID: "backend"}
	if err := svc.Create(ctx, task); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := bundle.Tasks.GetByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("expected task in workspace bundle: %v", err)
	}
	if got.Title != "backend task" || got.ProjectID != "backend" {
		t.Fatalf("unexpected task %+v", got)
	}
}

func TestTaskService_ListReturnsAllProjectsFromWorkspace(t *testing.T) {
	ctx := context.Background()
	svc, _ := multiProjectTaskSvc(t)

	for _, title := range []string{"d1", "d2"} {
		if err := svc.Create(ctx, &domain.Task{Title: title, ProjectID: "default"}); err != nil {
			t.Fatalf("Create default %q: %v", title, err)
		}
	}
	if err := svc.Create(ctx, &domain.Task{Title: "b1", ProjectID: "backend"}); err != nil {
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
	svc, _ := multiProjectTaskSvc(t)

	if err := svc.Create(ctx, &domain.Task{Title: "d1", ProjectID: "default"}); err != nil {
		t.Fatalf("Create default: %v", err)
	}
	if err := svc.Create(ctx, &domain.Task{Title: "b1", ProjectID: "backend"}); err != nil {
		t.Fatalf("Create backend: %v", err)
	}

	backendProject := "backend"
	filter := &domain.TermFilter{TaskFilter: domain.TaskFilter{ProjectID: &backendProject}}
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
	svc, _ := multiProjectTaskSvc(t)

	if err := svc.Create(ctx, &domain.Task{Title: "d1", ProjectID: "default"}); err != nil {
		t.Fatalf("Create default: %v", err)
	}
	if err := svc.Create(ctx, &domain.Task{Title: "b1", ProjectID: "backend"}); err != nil {
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
	svc, bundle := multiProjectTaskSvc(t)

	task := &domain.Task{Title: "t", ProjectID: "default"}
	if err := svc.Create(ctx, task); err != nil {
		t.Fatalf("Create: %v", err)
	}
	newProj := "backend"
	updated, err := svc.Update(ctx, domain.TaskUpdate{
		ShortID:   task.ShortID,
		Version:   task.Version,
		ProjectID: &newProj,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.ProjectID != "backend" {
		t.Fatalf("expected project=backend, got %q", updated.ProjectID)
	}

	got, err := bundle.Tasks.GetByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.ProjectID != "backend" {
		t.Fatalf("stored project mismatch: %q", got.ProjectID)
	}
}
