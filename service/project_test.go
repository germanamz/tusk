package service

import (
	"context"
	"errors"
	"testing"

	"github.com/germanamz/tusk/config"
	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/inmem"
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

func TestProjectService_GetByID(t *testing.T) {
	svc := testProjectService(t)
	ctx := context.Background()

	p, err := svc.GetByID(ctx, "default")
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if p.ID != "default" {
		t.Fatalf("expected ID 'default', got %q", p.ID)
	}
	if p.Workflow != "kanban" {
		t.Fatalf("expected Workflow 'kanban', got %q", p.Workflow)
	}
}

func TestProjectService_GetByIDNotFound(t *testing.T) {
	svc := testProjectService(t)
	ctx := context.Background()

	_, err := svc.GetByID(ctx, "nonexistent")
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
	// Should be sorted by ID
	if projects[0].ID != "backend" {
		t.Fatalf("expected first project 'backend', got %q", projects[0].ID)
	}
	if projects[1].ID != "default" {
		t.Fatalf("expected second project 'default', got %q", projects[1].ID)
	}
}
