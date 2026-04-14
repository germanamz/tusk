package service

import (
	"context"
	"errors"
	"testing"

	"github.com/germanamz/tusk/config"
	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/inmem"
	"github.com/google/uuid"
)

func testProjectService(t *testing.T) *ProjectService {
	t.Helper()
	cfgProjects := map[string]config.ProjectConfig{
		"default": {Workflow: "kanban"},
		"backend": {Workflow: "kanban"},
	}
	repo := inmem.NewProjectRepository(cfgProjects)
	return NewProjectService(repo)
}

func TestProjectService_GetByName(t *testing.T) {
	svc := testProjectService(t)
	ctx := context.Background()

	p, err := svc.GetByName(ctx, "default")
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if p.Name != "default" {
		t.Fatalf("expected name 'default', got %q", p.Name)
	}
	expectedWorkflowID := uuid.Nil
	if p.WorkflowID != expectedWorkflowID {
		t.Fatalf("expected WorkflowID for kanban, got %v", p.WorkflowID)
	}
}

func TestProjectService_GetByNameNotFound(t *testing.T) {
	svc := testProjectService(t)
	ctx := context.Background()

	_, err := svc.GetByName(ctx, "nonexistent")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestProjectService_List(t *testing.T) {
	svc := testProjectService(t)
	ctx := context.Background()

	projects, err := svc.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(projects) != 2 {
		t.Fatalf("expected 2 projects, got %d", len(projects))
	}
	// Should be sorted by name
	if projects[0].Name != "backend" {
		t.Fatalf("expected first project 'backend', got %q", projects[0].Name)
	}
	if projects[1].Name != "default" {
		t.Fatalf("expected second project 'default', got %q", projects[1].Name)
	}
}
