package service

import (
	"context"
	"errors"
	"testing"

	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/sqlite/sqlitetest"
	"github.com/google/uuid"
)

func testProjectService(t *testing.T) *ProjectService {
	t.Helper()
	_, projRepo, _ := sqlitetest.NewStore(t)
	sqlitetest.SeedProject(t, projRepo, "backend")
	return NewProjectService(projRepo, nil, nil, ProjectDefaults{})
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
