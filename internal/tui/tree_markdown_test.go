package tui

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/germanamz/tusk/config"
	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/service"
	"github.com/germanamz/tusk/sqlite"
	"github.com/germanamz/tusk/sqlite/sqlitetest"
	"github.com/google/uuid"
)

// testAppForMarkdown builds an App wired with a NoteService and PlayerService
// so runTree --format markdown can call ListAllForProject during input
// gathering. The base testApp helper passes nil for these, which would panic
// inside gatherMarkdownInputs.
func testAppForMarkdown(t *testing.T) (*App, *service.TaskService, *service.ProjectService) {
	t.Helper()
	store, projectRepo, workflowRepo := sqlitetest.NewStore(t)

	db := store.DB()
	bundle := &service.RepoBundle{
		Store:       store,
		WriteTx:     &storeWriteTx{store: store},
		Tasks:       sqlite.NewTaskRepo(db),
		Annotations: sqlite.NewAnnotationRepo(db),
		Notes:       sqlite.NewNoteRepo(db),
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
	playerSvc := service.NewPlayerService(bundle.Players)
	noteSvc := service.NewNoteService(bundle.Notes, bundle.Players, projectRepo, bundle.Tasks, 0)

	loadOpts := []config.Option{config.WithSearchPath(t.TempDir())}
	app := New(
		taskSvc, tagSvc, relationSvc, projectSvc, workflowSvc, playerSvc, noteSvc,
		nil,
		nil, nil, nil,
		VersionInfo{}, config.TUIConfig{}, config.MCPConfig{}, config.InlineConfig{}, loadOpts,
	)
	return app, taskSvc, projectSvc
}

// setProjectDescription updates the seeded default project so subsequent runs
// see a non-empty description.
func setProjectDescription(t *testing.T, projectSvc *service.ProjectService, name, desc string) {
	t.Helper()
	ctx := context.Background()
	p, err := projectSvc.GetByName(ctx, name)
	if err != nil {
		t.Fatalf("GetByName(%q): %v", name, err)
	}
	descPtr := &desc
	_, err = projectSvc.Modify(ctx, service.ModifyProjectInput{
		Name:            name,
		ExpectedVersion: p.Version,
		Description:     &descPtr,
	})
	if err != nil {
		t.Fatalf("Modify(%q) description: %v", name, err)
	}
}

func TestProjectDisplayName(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"tusk-roadmap", "Tusk Roadmap"},
		{"_default", "Default"},
		{"a", "A"},
		{"multi-word_project", "Multi Word Project"},
	}
	for _, c := range cases {
		got := projectDisplayName(c.in)
		if got != c.want {
			t.Errorf("projectDisplayName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestWriteBlockquote(t *testing.T) {
	t.Run("single line", func(t *testing.T) {
		var buf bytes.Buffer
		if err := writeBlockquote(&buf, "hello", ""); err != nil {
			t.Fatalf("writeBlockquote: %v", err)
		}
		want := "> hello\n\n"
		if buf.String() != want {
			t.Fatalf("got %q, want %q", buf.String(), want)
		}
	})

	t.Run("multi paragraph", func(t *testing.T) {
		var buf bytes.Buffer
		if err := writeBlockquote(&buf, "first\n\nsecond", ""); err != nil {
			t.Fatalf("writeBlockquote: %v", err)
		}
		want := "> first\n>\n> second\n\n"
		if buf.String() != want {
			t.Fatalf("got %q, want %q", buf.String(), want)
		}
	})

	t.Run("with indent", func(t *testing.T) {
		var buf bytes.Buffer
		if err := writeBlockquote(&buf, "line a\nline b", "  "); err != nil {
			t.Fatalf("writeBlockquote: %v", err)
		}
		want := "  > line a\n  > line b\n\n"
		if buf.String() != want {
			t.Fatalf("got %q, want %q", buf.String(), want)
		}
	})
}

func TestRunTree_MarkdownRejectsRollup(t *testing.T) {
	app, _, _ := testAppForMarkdown(t)

	app.root.SetArgs([]string{"task", "tree", "--format", "markdown", "--rollup"})
	err := app.root.Execute()
	if err == nil {
		t.Fatal("expected error for --format markdown --rollup, got nil")
	}
	if !strings.Contains(err.Error(), "--rollup is not supported with --format markdown") {
		t.Fatalf("error %q should mention rollup-not-supported", err.Error())
	}
}

func TestRunTree_MarkdownRejectsMultiProject(t *testing.T) {
	app, taskSvc, projectSvc := testAppForMarkdown(t)
	ctx := context.Background()

	defaultProj, err := projectSvc.GetByName(ctx, "default")
	if err != nil {
		t.Fatalf("GetByName(default): %v", err)
	}

	other, err := projectSvc.Create(ctx, service.CreateProjectInput{
		Name:       "second",
		WorkflowID: defaultProj.WorkflowID,
	})
	if err != nil {
		t.Fatalf("create second project: %v", err)
	}

	if err := taskSvc.Create(ctx, &domain.Task{Title: "in default"}); err != nil {
		t.Fatalf("create default task: %v", err)
	}
	if err := taskSvc.Create(ctx, &domain.Task{Title: "in second", ProjectID: other.ID}); err != nil {
		t.Fatalf("create second task: %v", err)
	}

	app.root.SetArgs([]string{"task", "tree", "--format", "markdown"})
	err = app.root.Execute()
	if err == nil {
		t.Fatal("expected error for multi-project markdown export, got nil")
	}
	if !strings.Contains(err.Error(), "requires a single project") {
		t.Fatalf("error %q should mention single-project requirement", err.Error())
	}
}

func TestRunTree_MarkdownEmptyWorkspace(t *testing.T) {
	app, _, _ := testAppForMarkdown(t)

	var stdout, stderr bytes.Buffer
	app.root.SetOut(&stdout)
	app.root.SetErr(&stderr)
	app.root.SetArgs([]string{"task", "tree", "--format", "markdown"})
	if err := app.root.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if stdout.String() != "" {
		t.Fatalf("expected empty stdout, got %q", stdout.String())
	}
	if stderr.String() != "" {
		t.Fatalf("expected empty stderr, got %q", stderr.String())
	}
}

func TestRunTree_MarkdownSingleProject_StubOutput(t *testing.T) {
	app, taskSvc, projectSvc := testAppForMarkdown(t)
	ctx := context.Background()

	setProjectDescription(t, projectSvc, "default", "The headline goal for this workspace.")

	if err := taskSvc.Create(ctx, &domain.Task{Title: "Root task"}); err != nil {
		t.Fatalf("create task: %v", err)
	}

	var stdout bytes.Buffer
	app.root.SetOut(&stdout)
	app.root.SetArgs([]string{"task", "tree", "--format", "markdown"})
	if err := app.root.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	out := stdout.String()
	if !strings.HasPrefix(out, "# Default\n") {
		t.Fatalf("expected output to begin with H1 for default project, got:\n%s", out)
	}
	if !strings.Contains(out, "> The headline goal for this workspace.") {
		t.Fatalf("expected description blockquote, got:\n%s", out)
	}
	if !strings.Contains(out, "<!-- tusk: markdown body lands in phase 4 -->") {
		t.Fatalf("expected phase-4 placeholder comment, got:\n%s", out)
	}
	// The Phase 3 stub must not render any task body yet — guard against
	// accidental forward-progress before Phase 4.
	if strings.Contains(out, "Root task") {
		t.Fatalf("phase-3 stub should not render task content, got:\n%s", out)
	}
}
