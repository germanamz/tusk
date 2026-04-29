package tui

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/germanamz/tusk/config"
	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/service"
	"github.com/germanamz/tusk/sqlite"
	"github.com/germanamz/tusk/sqlite/sqlitetest"
	"github.com/google/uuid"
)

// testAppWithConfigFile wires an App whose config.Load() reads the provided
// file contents. Returns the app plus the file path so tests can rewrite it
// between assertions.
func testAppWithConfigFile(test *testing.T, contents string) *App {
	test.Helper()
	dir := test.TempDir()
	path := filepath.Join(dir, "tusk.toml")
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		test.Fatalf("writing config: %v", err)
	}

	store, projectRepo, workflowRepo := sqlitetest.NewStore(test)
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

	workflowSvc := service.NewWorkflowService(workflowRepo, projectRepo)
	projectSvc := service.NewProjectService(projectRepo, bundle.Tasks, bundle.Store, service.ProjectDefaults{}, nil)
	taskSvc := service.NewTaskService(resolver, projects, projectRepo, projectSvc, workflowSvc, nil)
	tagSvc := service.NewTagService(resolver)
	relationSvc := service.NewRelationService(resolver, projects)

	loadOpts := []config.Option{config.WithExplicitFile(path)}
	app := New(
		taskSvc, tagSvc, relationSvc, projectSvc, workflowSvc, nil, nil,
		nil,
		nil, nil, nil,
		VersionInfo{}, config.TUIConfig{}, config.MCPConfig{}, config.InlineConfig{}, loadOpts,
	)
	return app
}

func TestRunConfigShow_TaxonomyBlockRendered(test *testing.T) {
	app := testAppWithConfigFile(test, `
[taxonomy]
levels = [["milestone"], ["story"], ["task", "spike"]]
`)

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetErr(&buf)
	app.root.SetArgs([]string{"config", "show"})
	if err := app.root.Execute(); err != nil {
		test.Fatalf("config show: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "[taxonomy]") {
		test.Fatalf("expected [taxonomy] block, got:\n%s", out)
	}
	if !strings.Contains(out, `levels = "milestone:story:(task,spike)"`) {
		test.Fatalf("expected inline levels, got:\n%s", out)
	}
}

func TestRunConfigShow_TaxonomyBlockOmittedWhenEmpty(test *testing.T) {
	app := testAppWithConfigFile(test, "")

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetErr(&buf)
	app.root.SetArgs([]string{"config", "show"})
	if err := app.root.Execute(); err != nil {
		test.Fatalf("config show: %v", err)
	}
	if strings.Contains(buf.String(), "[taxonomy]") {
		test.Fatalf("did not expect [taxonomy] block when empty, got:\n%s", buf.String())
	}
}
