package service

import (
	"context"
	"errors"
	"testing"

	"github.com/germanamz/tusk/config"
	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/sqlite/sqlitetest"
	"github.com/google/uuid"
)

func testProjectService(t *testing.T) *ProjectService {
	t.Helper()
	_, projRepo, _ := sqlitetest.NewStore(t)
	sqlitetest.SeedProject(t, projRepo, "backend")
	return NewProjectService(projRepo, nil, nil, ProjectDefaults{}, nil)
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

func TestProjectService_EffectiveTaxonomy_None(t *testing.T) {
	svc := NewProjectService(nil, nil, nil, ProjectDefaults{}, nil)
	p := &domain.Project{}
	tax, src := svc.EffectiveTaxonomy(p)
	if src != TaxonomySourceNone {
		t.Fatalf("source = %d, want None (%d)", src, TaxonomySourceNone)
	}
	if !tax.IsEmpty() {
		t.Fatalf("expected empty taxonomy, got %v", tax)
	}
}

func TestProjectService_EffectiveTaxonomy_Workspace(t *testing.T) {
	cfg := &config.Config{
		Taxonomy: config.TaxonomyConfig{
			Levels: [][]string{{"milestone"}, {"story"}, {"task"}},
		},
	}
	svc := NewProjectService(nil, nil, nil, ProjectDefaults{}, cfg)
	p := &domain.Project{}
	tax, src := svc.EffectiveTaxonomy(p)
	if src != TaxonomySourceWorkspace {
		t.Fatalf("source = %d, want Workspace (%d)", src, TaxonomySourceWorkspace)
	}
	if len(tax) != 3 || tax[0][0] != "milestone" || tax[2][0] != "task" {
		t.Fatalf("unexpected taxonomy %v", tax)
	}

	// Mutating the returned taxonomy must not corrupt the workspace config —
	// it is a clone, not a shared reference.
	tax[0][0] = "ZZZ"
	if cfg.Taxonomy.Levels[0][0] != "milestone" {
		t.Fatalf("cfg taxonomy mutated through shared slice: %v", cfg.Taxonomy.Levels)
	}
}

func TestProjectService_EffectiveTaxonomy_ProjectOverride(t *testing.T) {
	cfg := &config.Config{
		Taxonomy: config.TaxonomyConfig{
			Levels: [][]string{{"workspace-only"}},
		},
	}
	svc := NewProjectService(nil, nil, nil, ProjectDefaults{}, cfg)
	override := domain.Taxonomy{{"epic"}, {"story"}}
	p := &domain.Project{Settings: domain.ProjectSettings{Taxonomy: &override}}
	tax, src := svc.EffectiveTaxonomy(p)
	if src != TaxonomySourceProjectOverride {
		t.Fatalf("source = %d, want ProjectOverride (%d)", src, TaxonomySourceProjectOverride)
	}
	if len(tax) != 2 || tax[0][0] != "epic" || tax[1][0] != "story" {
		t.Fatalf("unexpected taxonomy %v", tax)
	}
}

func TestProjectService_EffectiveTaxonomy_ProjectOptOut(t *testing.T) {
	cfg := &config.Config{
		Taxonomy: config.TaxonomyConfig{
			Levels: [][]string{{"workspace-only"}},
		},
	}
	svc := NewProjectService(nil, nil, nil, ProjectDefaults{}, cfg)
	empty := domain.Taxonomy{}
	p := &domain.Project{Settings: domain.ProjectSettings{Taxonomy: &empty}}
	tax, src := svc.EffectiveTaxonomy(p)
	if src != TaxonomySourceProjectOverride {
		t.Fatalf("source = %d, want ProjectOverride (%d)", src, TaxonomySourceProjectOverride)
	}
	if !tax.IsEmpty() {
		t.Fatalf("expected opt-out to yield empty taxonomy, got %v", tax)
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
