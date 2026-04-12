package service

import (
	"context"
	"errors"
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

func twoProjectTaskSvc(t *testing.T) (*TaskService, *RepoBundle, *RepoBundle) {
	t.Helper()
	defaultBundle := newTestBundle(t)
	backendBundle := newTestBundle(t)

	resolver, projects := multiBundleResolver(t, map[string]*RepoBundle{
		"default": defaultBundle,
		"backend": backendBundle,
	})
	projectRepo := multiProjectKanban("default", "backend")
	workflowSvc := multiProjectWorkflowSvc(projectRepo)
	svc := NewTaskService(resolver, projects, projectRepo, workflowSvc, nil)
	return svc, defaultBundle, backendBundle
}

func TestTaskService_CreateRoutesToProjectBundle(t *testing.T) {
	ctx := context.Background()
	svc, defaultBundle, backendBundle := twoProjectTaskSvc(t)

	task := &domain.Task{Title: "backend task", ProjectID: "backend"}
	if err := svc.Create(ctx, task); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := backendBundle.Tasks.GetByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("expected task in backend bundle: %v", err)
	}
	if got.Title != "backend task" {
		t.Fatalf("unexpected title %q", got.Title)
	}

	if _, err := defaultBundle.Tasks.GetByID(ctx, task.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("task should NOT be in default bundle; got err=%v", err)
	}
}

func TestTaskService_ListFansOutAcrossStores(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := twoProjectTaskSvc(t)

	for _, title := range []string{"d1", "d2"} {
		task := &domain.Task{Title: title, ProjectID: "default"}
		if err := svc.Create(ctx, task); err != nil {
			t.Fatalf("Create default %q: %v", title, err)
		}
	}
	backend := &domain.Task{Title: "b1", ProjectID: "backend"}
	if err := svc.Create(ctx, backend); err != nil {
		t.Fatalf("Create backend: %v", err)
	}

	all, err := svc.List(ctx, nil)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("expected 3 merged tasks, got %d", len(all))
	}
}

func TestTaskService_ListProjectFilterNarrowsStores(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := twoProjectTaskSvc(t)

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
	if len(got) != 1 {
		t.Fatalf("expected 1 task, got %d: %+v", len(got), got)
	}
	if got[0].Title != "b1" {
		t.Fatalf("expected 'b1', got %q", got[0].Title)
	}
}

func TestTaskService_AvailableFansOutAcrossStores(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := twoProjectTaskSvc(t)

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
		t.Fatalf("expected 2 available across stores, got %d", len(avail))
	}
}

func TestTaskService_UpdateRejectsCrossStoreProjectChange(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := twoProjectTaskSvc(t)

	task := &domain.Task{Title: "t", ProjectID: "default"}
	if err := svc.Create(ctx, task); err != nil {
		t.Fatalf("Create: %v", err)
	}
	newProj := "backend"
	_, err := svc.Update(ctx, domain.TaskUpdate{
		ShortID:   task.ShortID,
		Version:   task.Version,
		ProjectID: &newProj,
	})
	if !errors.Is(err, domain.ErrCrossStoreRelation) {
		t.Fatalf("expected ErrCrossStoreRelation, got %v", err)
	}
}
