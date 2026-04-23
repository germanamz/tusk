package tui

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/germanamz/tusk/config"
	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/service"
	"github.com/germanamz/tusk/sqlite"
	"github.com/germanamz/tusk/sqlite/sqlitetest"
	"github.com/google/uuid"
)

// seedProjectOverride adds a project with the given override taxonomy through
// the repo so tests can exercise the provenance-rendering branches. Passing a
// pointer to an empty slice models the explicit opt-out case.
func seedProjectOverride(t *testing.T, store *sqlite.Store, projectRepo *sqlite.ProjectRepo, name string, override *domain.Taxonomy) *domain.Project {
	t.Helper()
	_ = store
	p := &domain.Project{
		ID:         uuid.New(),
		Name:       name,
		WorkflowID: uuid.Nil,
		Settings:   domain.ProjectSettings{Taxonomy: override},
		Version:    1,
	}
	if err := projectRepo.Create(context.Background(), p); err != nil {
		t.Fatalf("create project %q: %v", name, err)
	}
	return p
}

func testAppForProjectShow(t *testing.T, workspaceLevels [][]string) (*App, *sqlite.Store, *sqlite.ProjectRepo) {
	t.Helper()
	store, projectRepo, workflowRepo := sqlitetest.NewStore(t)

	db := store.DB()
	bundle := &service.RepoBundle{
		Store:       store,
		WriteTx:     &storeWriteTx{store: store},
		Tasks:       sqlite.NewTaskRepo(db),
		Annotations: sqlite.NewAnnotationRepo(db),
		Relations:   sqlite.NewRelationRepo(db),
		Tags:        sqlite.NewTagRepo(db),
		Players:     sqlite.NewPlayerRepo(db),
	}

	resolver := func(context.Context, uuid.UUID) (*service.RepoBundle, error) { return bundle, nil }
	projects := func(context.Context) ([]uuid.UUID, error) { return []uuid.UUID{domain.DefaultProjectUUID}, nil }

	var cfg *config.Config
	if workspaceLevels != nil {
		cfg = &config.Config{Taxonomy: config.TaxonomyConfig{Levels: workspaceLevels}}
	}
	workflowSvc := service.NewWorkflowService(workflowRepo, projectRepo)
	projectSvc := service.NewProjectService(projectRepo, bundle.Tasks, bundle.Store, service.ProjectDefaults{}, cfg)
	taskSvc := service.NewTaskService(resolver, projects, projectRepo, projectSvc, workflowSvc, nil)
	tagSvc := service.NewTagService(resolver)
	relationSvc := service.NewRelationService(resolver, projects)

	loadOpts := []config.Option{config.WithSearchPath(t.TempDir())}
	app := New(
		taskSvc, tagSvc, relationSvc, projectSvc, workflowSvc, nil, nil,
		nil, nil, nil,
		VersionInfo{}, config.TUIConfig{}, config.MCPConfig{}, config.InlineConfig{}, loadOpts,
	)
	return app, store, projectRepo
}

func TestProjectShow_NoTaxonomy(t *testing.T) {
	app, _, _ := testAppForProjectShow(t, nil)

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"project", "show", "default"})
	if err := app.root.Execute(); err != nil {
		t.Fatalf("project show: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "Taxonomy:") {
		t.Fatalf("expected Taxonomy: label, got:\n%s", out)
	}
	if !strings.Contains(out, "(none)") {
		t.Fatalf("expected (none) placeholder, got:\n%s", out)
	}
	if strings.Contains(out, "source:") {
		t.Fatalf("expected no source: line when taxonomy none, got:\n%s", out)
	}
}

func TestProjectShow_WorkspaceDefault(t *testing.T) {
	app, _, _ := testAppForProjectShow(t, [][]string{{"milestone"}, {"story"}, {"task", "spike"}})

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"project", "show", "default"})
	if err := app.root.Execute(); err != nil {
		t.Fatalf("project show: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "milestone:story:(task,spike)") {
		t.Fatalf("expected inline taxonomy, got:\n%s", out)
	}
	if !strings.Contains(out, "workspace default") {
		t.Fatalf("expected 'workspace default' provenance, got:\n%s", out)
	}
}

func TestProjectShow_ProjectOverride(t *testing.T) {
	app, _, projectRepo := testAppForProjectShow(t, [][]string{{"milestone"}, {"story"}})
	override := domain.Taxonomy{{"alpha"}, {"beta"}}
	seedProjectOverride(t, nil, projectRepo, "override", &override)

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"project", "show", "override"})
	if err := app.root.Execute(); err != nil {
		t.Fatalf("project show: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "alpha:beta") {
		t.Fatalf("expected override inline taxonomy, got:\n%s", out)
	}
	if !strings.Contains(out, "project override") {
		t.Fatalf("expected 'project override' provenance, got:\n%s", out)
	}
}

func TestProjectShow_OptOut(t *testing.T) {
	app, _, projectRepo := testAppForProjectShow(t, [][]string{{"milestone"}})
	empty := domain.Taxonomy{}
	seedProjectOverride(t, nil, projectRepo, "optout", &empty)

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"project", "show", "optout"})
	if err := app.root.Execute(); err != nil {
		t.Fatalf("project show: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "disabled; project opted out") {
		t.Fatalf("expected 'disabled; project opted out' placeholder, got:\n%s", out)
	}
	if strings.Contains(out, "source:") {
		t.Fatalf("opt-out branch should omit source: line, got:\n%s", out)
	}
}

func TestProjectShow_JSON(t *testing.T) {
	app, _, _ := testAppForProjectShow(t, [][]string{{"milestone"}, {"story"}})

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"project", "show", "default", "--format", "json"})
	if err := app.root.Execute(); err != nil {
		t.Fatalf("project show json: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(buf.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v\nraw: %s", err, buf.String())
	}
	tax, ok := payload["effective_taxonomy"].(map[string]any)
	if !ok {
		t.Fatalf("expected effective_taxonomy object, got: %v", payload)
	}
	if tax["source"] != "workspace_default" {
		t.Fatalf("source: got %v, want workspace_default", tax["source"])
	}
}
