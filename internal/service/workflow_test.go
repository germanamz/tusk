package service

import (
	"context"
	"errors"
	"testing"

	"github.com/germanamz/tusk/config"
	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/internal/inmem"
)

func testWorkflowEnv(t *testing.T) *WorkflowService {
	t.Helper()
	workflowRepo := inmem.NewWorkflowRepository(map[string]config.WorkflowConfig{
		"kanban": {
			Statuses: []string{"pending", "active", "completed", "deleted"},
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
	projectRepo := inmem.NewProjectRepository(map[string]config.ProjectConfig{
		"default": {Workflow: "kanban"},
		"backend": {Workflow: "kanban"},
	})
	return NewWorkflowService(workflowRepo, projectRepo)
}

func TestIsTransitionAllowed_Allowed(t *testing.T) {
	svc := testWorkflowEnv(t)
	allowed, err := svc.IsTransitionAllowed(context.Background(), "kanban", "pending", "active")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !allowed {
		t.Fatal("expected pending->active to be allowed")
	}
}

func TestIsTransitionAllowed_Disallowed(t *testing.T) {
	svc := testWorkflowEnv(t)
	allowed, err := svc.IsTransitionAllowed(context.Background(), "kanban", "pending", "completed")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if allowed {
		t.Fatal("expected pending->completed to be disallowed")
	}
}

func TestIsTransitionAllowed_WorkflowNotFound(t *testing.T) {
	svc := testWorkflowEnv(t)
	_, err := svc.IsTransitionAllowed(context.Background(), "nonexistent", "pending", "active")
	if err == nil {
		t.Fatal("expected error for nonexistent workflow")
	}
}

func TestGetStatuses(t *testing.T) {
	svc := testWorkflowEnv(t)
	statuses, err := svc.GetStatuses(context.Background(), "kanban")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := []string{"pending", "active", "completed", "deleted"}
	if len(statuses) != len(expected) {
		t.Fatalf("expected %d statuses, got %d", len(expected), len(statuses))
	}
	for i, s := range statuses {
		if s != expected[i] {
			t.Fatalf("status[%d]: expected %q, got %q", i, expected[i], s)
		}
	}
}

func TestGetStatuses_WorkflowNotFound(t *testing.T) {
	svc := testWorkflowEnv(t)
	_, err := svc.GetStatuses(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent workflow")
	}
}

func TestGetTransitions(t *testing.T) {
	svc := testWorkflowEnv(t)
	transitions, err := svc.GetTransitions(context.Background(), "kanban")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(transitions) != 6 {
		t.Fatalf("expected 6 transitions, got %d", len(transitions))
	}
}

func TestWorkflowList(t *testing.T) {
	svc := testWorkflowEnv(t)
	workflows, err := svc.List(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(workflows) != 1 || workflows[0].Name != "kanban" {
		t.Fatalf("expected [kanban], got %v", workflows)
	}
}

func TestGetByName(t *testing.T) {
	svc := testWorkflowEnv(t)
	wf, err := svc.GetByName(context.Background(), "kanban")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if wf.Name != "kanban" {
		t.Fatalf("expected 'kanban', got %q", wf.Name)
	}
}

func TestGetByName_NotFound(t *testing.T) {
	svc := testWorkflowEnv(t)
	_, err := svc.GetByName(context.Background(), "nonexistent")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestGetWorkflowWithProjects(t *testing.T) {
	svc := testWorkflowEnv(t)
	wf, projectIDs, err := svc.GetWorkflowWithProjects(context.Background(), "kanban")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if wf.Name != "kanban" {
		t.Fatalf("expected 'kanban', got %q", wf.Name)
	}
	if len(projectIDs) != 2 || projectIDs[0] != "backend" || projectIDs[1] != "default" {
		t.Fatalf("expected [backend, default], got %v", projectIDs)
	}
}

func TestGetWorkflowWithProjects_NotFound(t *testing.T) {
	svc := testWorkflowEnv(t)
	_, _, err := svc.GetWorkflowWithProjects(context.Background(), "nonexistent")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
