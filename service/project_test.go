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

func testProjectService(test *testing.T) *ProjectService {
	test.Helper()
	_, projRepo, _ := sqlitetest.NewStore(test)
	sqlitetest.SeedProject(test, projRepo, "backend")
	return NewProjectService(projRepo, nil, nil, ProjectDefaults{}, nil)
}

func TestProjectService_GetByName(test *testing.T) {
	svc := testProjectService(test)
	ctx := context.Background()

	project, getErr := svc.GetByName(ctx, "default")

	if getErr != nil {
		test.Fatalf("GetByID: %v", getErr)
	}

	if project.Name != "default" {
		test.Fatalf("expected name 'default', got %q", project.Name)
	}
	expectedWorkflowID := uuid.Nil
	if project.WorkflowID != expectedWorkflowID {
		test.Fatalf("expected WorkflowID for kanban, got %v", project.WorkflowID)
	}
}

func TestProjectService_GetByNameNotFound(test *testing.T) {
	svc := testProjectService(test)
	ctx := context.Background()

	_, err := svc.GetByName(ctx, "nonexistent")
	if !errors.Is(err, domain.ErrNotFound) {
		test.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestProjectService_EffectiveTaxonomy_None(test *testing.T) {
	svc := NewProjectService(nil, nil, nil, ProjectDefaults{}, nil)
	domainProject := &domain.Project{}
	tax, src := svc.EffectiveTaxonomy(domainProject)
	if src != TaxonomySourceNone {
		test.Fatalf("source = %d, want None (%d)", src, TaxonomySourceNone)
	}
	if !tax.IsEmpty() {
		test.Fatalf("expected empty taxonomy, got %v", tax)
	}
}

func TestProjectService_EffectiveTaxonomy_Workspace(test *testing.T) {
	cfg := &config.Config{
		Taxonomy: config.TaxonomyConfig{
			Levels: [][]string{{"milestone"}, {"story"}, {"task"}},
		},
	}
	svc := NewProjectService(nil, nil, nil, ProjectDefaults{}, cfg)
	domainProject := &domain.Project{}
	tax, src := svc.EffectiveTaxonomy(domainProject)
	if src != TaxonomySourceWorkspace {
		test.Fatalf("source = %d, want Workspace (%d)", src, TaxonomySourceWorkspace)
	}
	if len(tax) != 3 || tax[0][0] != "milestone" || tax[2][0] != "task" {
		test.Fatalf("unexpected taxonomy %v", tax)
	}

	// Mutating the returned taxonomy must not corrupt the workspace config —
	// it is a clone, not a shared reference.
	tax[0][0] = "ZZZ"
	if cfg.Taxonomy.Levels[0][0] != "milestone" {
		test.Fatalf("cfg taxonomy mutated through shared slice: %v", cfg.Taxonomy.Levels)
	}
}

func TestProjectService_EffectiveTaxonomy_ProjectOverride(test *testing.T) {
	cfg := &config.Config{
		Taxonomy: config.TaxonomyConfig{
			Levels: [][]string{{"workspace-only"}},
		},
	}
	svc := NewProjectService(nil, nil, nil, ProjectDefaults{}, cfg)
	override := domain.Taxonomy{{"epic"}, {"story"}}
	domainProject := &domain.Project{Settings: domain.ProjectSettings{Taxonomy: &override}}
	tax, src := svc.EffectiveTaxonomy(domainProject)
	if src != TaxonomySourceProjectOverride {
		test.Fatalf("source = %d, want ProjectOverride (%d)", src, TaxonomySourceProjectOverride)
	}
	if len(tax) != 2 || tax[0][0] != "epic" || tax[1][0] != "story" {
		test.Fatalf("unexpected taxonomy %v", tax)
	}
}

func TestProjectService_EffectiveTaxonomy_ProjectOptOut(test *testing.T) {
	cfg := &config.Config{
		Taxonomy: config.TaxonomyConfig{
			Levels: [][]string{{"workspace-only"}},
		},
	}
	svc := NewProjectService(nil, nil, nil, ProjectDefaults{}, cfg)
	empty := domain.Taxonomy{}
	domainProject := &domain.Project{Settings: domain.ProjectSettings{Taxonomy: &empty}}
	tax, src := svc.EffectiveTaxonomy(domainProject)
	if src != TaxonomySourceProjectOverride {
		test.Fatalf("source = %d, want ProjectOverride (%d)", src, TaxonomySourceProjectOverride)
	}
	if !tax.IsEmpty() {
		test.Fatalf("expected opt-out to yield empty taxonomy, got %v", tax)
	}
}

func TestProjectService_Create_WithDescription(test *testing.T) {
	_, projRepo, _ := sqlitetest.NewStore(test)
	svc := NewProjectService(projRepo, nil, nil, ProjectDefaults{}, nil)
	ctx := context.Background()

	const desc = "hello world\nmulti-line"
	created, createErr := svc.Create(ctx, CreateProjectInput{
		Name:        "p1",
		WorkflowID:  uuid.Nil,
		Description: desc,
	})

	if createErr != nil {
		test.Fatalf("Create: %v", createErr)
	}

	if created.Description != desc {
		test.Fatalf("created Description = %q, want %q", created.Description, desc)
	}
	got, getErr := svc.GetByName(ctx, "p1")

	if getErr != nil {
		test.Fatalf("GetByName: %v", getErr)
	}

	if got.Description != desc {
		test.Fatalf("round-trip Description = %q, want %q", got.Description, desc)
	}
}

func TestProjectService_Modify_SetDescription(test *testing.T) {
	_, projRepo, _ := sqlitetest.NewStore(test)
	svc := NewProjectService(projRepo, nil, nil, ProjectDefaults{}, nil)
	ctx := context.Background()

	created, createErr := svc.Create(ctx, CreateProjectInput{Name: "p1", WorkflowID: uuid.Nil})

	if createErr != nil {
		test.Fatalf("Create: %v", createErr)
	}

	desc := "the new description"
	descPtr := &desc
	updated, modifyErr := svc.Modify(ctx, ModifyProjectInput{
		Name:            "p1",
		ExpectedVersion: created.Version,
		Description:     &descPtr,
	})

	if modifyErr != nil {
		test.Fatalf("Modify: %v", modifyErr)
	}

	if updated.Description != desc {
		test.Fatalf("updated Description = %q, want %q", updated.Description, desc)
	}
	got, getErr := svc.GetByName(ctx, "p1")

	if getErr != nil {
		test.Fatalf("GetByName: %v", getErr)
	}

	if got.Description != desc {
		test.Fatalf("persisted Description = %q, want %q", got.Description, desc)
	}
}

func TestProjectService_Modify_ClearDescription(test *testing.T) {
	_, projRepo, _ := sqlitetest.NewStore(test)
	svc := NewProjectService(projRepo, nil, nil, ProjectDefaults{}, nil)
	ctx := context.Background()

	created, createErr := svc.Create(ctx, CreateProjectInput{
		Name:        "p1",
		WorkflowID:  uuid.Nil,
		Description: "starts populated",
	})

	if createErr != nil {
		test.Fatalf("Create: %v", createErr)
	}

	var inner *string // nil → clear
	updated, modifyErr := svc.Modify(ctx, ModifyProjectInput{
		Name:            "p1",
		ExpectedVersion: created.Version,
		Description:     &inner,
	})

	if modifyErr != nil {
		test.Fatalf("Modify: %v", modifyErr)
	}

	if updated.Description != "" {
		test.Fatalf("updated Description = %q, want empty", updated.Description)
	}
	got, getErr := svc.GetByName(ctx, "p1")

	if getErr != nil {
		test.Fatalf("GetByName: %v", getErr)
	}

	if got.Description != "" {
		test.Fatalf("persisted Description = %q, want empty", got.Description)
	}
}

func TestProjectService_Modify_LeaveDescription(test *testing.T) {
	_, projRepo, _ := sqlitetest.NewStore(test)
	svc := NewProjectService(projRepo, nil, nil, ProjectDefaults{}, nil)
	ctx := context.Background()

	const orig = "stays put"
	created, createErr := svc.Create(ctx, CreateProjectInput{
		Name:        "p1",
		WorkflowID:  uuid.Nil,
		Description: orig,
	})

	if createErr != nil {
		test.Fatalf("Create: %v", createErr)
	}

	// Outer nil → leave unchanged. Use a non-description mutation to ensure
	// the Modify path actually exercises the persistence layer.
	workflowID := uuid.Nil
	updated, modifyErr := svc.Modify(ctx, ModifyProjectInput{
		Name:            "p1",
		ExpectedVersion: created.Version,
		WorkflowID:      &workflowID,
	})

	if modifyErr != nil {
		test.Fatalf("Modify: %v", modifyErr)
	}

	if updated.Description != orig {
		test.Fatalf("updated Description = %q, want %q", updated.Description, orig)
	}
	got, getErr := svc.GetByName(ctx, "p1")

	if getErr != nil {
		test.Fatalf("GetByName: %v", getErr)
	}

	if got.Description != orig {
		test.Fatalf("persisted Description = %q, want %q", got.Description, orig)
	}
}

func TestProjectService_List(test *testing.T) {
	svc := testProjectService(test)
	ctx := context.Background()

	projects, listErr := svc.List(ctx)

	if listErr != nil {
		test.Fatalf("List: %v", listErr)
	}

	if len(projects) != 2 {
		test.Fatalf("expected 2 projects, got %d", len(projects))
	}
	// Should be sorted by name
	if projects[0].Name != "backend" {
		test.Fatalf("expected first project 'backend', got %q", projects[0].Name)
	}
	if projects[1].Name != "default" {
		test.Fatalf("expected second project 'default', got %q", projects[1].Name)
	}
}
