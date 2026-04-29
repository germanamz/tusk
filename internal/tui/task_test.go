package tui

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/germanamz/tusk/config"
	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/repository"
	"github.com/germanamz/tusk/service"
	"github.com/germanamz/tusk/sqlite"
	"github.com/germanamz/tusk/sqlite/sqlitetest"
	"github.com/google/uuid"
)

// testAppWithTaxonomy builds an App wired with a workspace taxonomy plus a
// raw TaskRepository the caller can use to seed tasks that already violate
// the taxonomy. Mirrors testApp but threads a non-nil config through the
// project service.
func testAppWithTaxonomy(test *testing.T, levels [][]string) (*App, repository.TaskRepository, *service.ProjectService) {
	test.Helper()
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

	cfg := &config.Config{Taxonomy: config.TaxonomyConfig{Levels: levels}}
	workflowSvc := service.NewWorkflowService(workflowRepo, projectRepo)
	projectSvc := service.NewProjectService(projectRepo, bundle.Tasks, bundle.Store, service.ProjectDefaults{}, cfg)
	taskSvc := service.NewTaskService(resolver, projects, projectRepo, projectSvc, workflowSvc, nil)
	tagSvc := service.NewTagService(resolver)
	relationSvc := service.NewRelationService(resolver, projects)

	loadOpts := []config.Option{config.WithSearchPath(test.TempDir())}
	app := New(
		taskSvc, tagSvc, relationSvc, projectSvc, workflowSvc, nil, nil,
		nil,
		nil, nil, nil,
		VersionInfo{}, config.TUIConfig{}, config.MCPConfig{}, config.InlineConfig{}, loadOpts,
	)
	return app, bundle.Tasks, projectSvc
}

// seedViolatingTask inserts a task row directly through the repo so tests can
// place tasks that already violate the active taxonomy. Level may be nil.
func seedViolatingTask(test *testing.T, tasks repository.TaskRepository, shortID, title string, level *string) *domain.Task {
	test.Helper()
	now := time.Now().UTC().Truncate(time.Millisecond)
	task := &domain.Task{
		ID:         uuid.New(),
		ShortID:    shortID,
		ProjectID:  domain.DefaultProjectUUID,
		Title:      title,
		Status:     "pending",
		Level:      level,
		Version:    1,
		CreatedAt:  now,
		ModifiedAt: now,
		UDA:        map[string]any{},
	}
	if err := tasks.Create(context.Background(), task); err != nil {
		test.Fatalf("seed task %q: %v", shortID, err)
	}
	return task
}

// TestRunCreate_Level covers the CLI level= parse on `tusk task create`.
// No taxonomy is configured here, so the service accepts any level verbatim.
func TestRunCreate_Level(test *testing.T) {
	app, taskSvc := testApp(test)
	ctx := context.Background()

	app.root.SetArgs([]string{"task", "create", "Top-level goal", "level=story"})
	if err := app.root.Execute(); err != nil {
		test.Fatalf("create: %v", err)
	}

	tasks, listErr := taskSvc.List(ctx, nil)

	if listErr != nil {
		test.Fatalf("List: %v", listErr)
	}

	if len(tasks) != 1 {
		test.Fatalf("expected 1 task, got %d", len(tasks))
	}
	if tasks[0].Level == nil || *tasks[0].Level != "story" {
		test.Fatalf("expected level 'story', got %v", tasks[0].Level)
	}
}

func TestRunCreate_LevelEmptyRejected(test *testing.T) {
	app, _ := testApp(test)

	app.root.SetArgs([]string{"task", "create", "No-op", "level="})
	err := app.root.Execute()
	if err == nil {
		test.Fatal("expected error for empty level= on create")
	}
	if !strings.Contains(err.Error(), "level") {
		test.Fatalf("error %q should reference 'level'", err)
	}
}

func TestRunCreate_LevelModifierRejected(test *testing.T) {
	app, _ := testApp(test)

	// `+level=story` and `-level=story` must be rejected on create.
	app.root.SetArgs([]string{"task", "create", "bad", "+level=story"})
	if err := app.root.Execute(); err == nil {
		test.Fatal("expected error for +level= on create")
	}

	app.root.SetArgs([]string{"task", "create", "bad", "--", "-level=story"})
	if err := app.root.Execute(); err == nil {
		test.Fatal("expected error for -level= on create")
	}
}

func TestRunModify_Level(test *testing.T) {
	app, taskSvc := testApp(test)
	ctx := context.Background()

	task := &domain.Task{Title: "modify level"}
	if err := taskSvc.Create(ctx, task); err != nil {
		test.Fatalf("Create: %v", err)
	}

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"task", "modify", task.ShortID, "level=task"})
	if err := app.root.Execute(); err != nil {
		test.Fatalf("modify: %v", err)
	}

	got, getErr := taskSvc.GetByShortID(ctx, task.ShortID)

	if getErr != nil {
		test.Fatalf("GetByShortID: %v", getErr)
	}

	if got.Level == nil || *got.Level != "task" {
		test.Fatalf("expected level 'task', got %v", got.Level)
	}
}

func TestRunModify_ClearLevel(test *testing.T) {
	app, taskSvc := testApp(test)
	ctx := context.Background()

	level := "legacy"
	task := &domain.Task{Title: "clear level", Level: &level}
	if err := taskSvc.Create(ctx, task); err != nil {
		test.Fatalf("Create: %v", err)
	}

	app.root.SetArgs([]string{"task", "modify", task.ShortID, "level="})
	if err := app.root.Execute(); err != nil {
		test.Fatalf("modify: %v", err)
	}

	got, getErr := taskSvc.GetByShortID(ctx, task.ShortID)

	if getErr != nil {
		test.Fatalf("GetByShortID: %v", getErr)
	}

	if got.Level != nil {
		test.Fatalf("expected cleared level, got %v", got.Level)
	}
}

func TestRunModify_LevelModifierRejected(test *testing.T) {
	app, taskSvc := testApp(test)
	ctx := context.Background()

	task := &domain.Task{Title: "mod-reject"}
	if err := taskSvc.Create(ctx, task); err != nil {
		test.Fatalf("Create: %v", err)
	}

	app.root.SetArgs([]string{"task", "modify", task.ShortID, "+level=story"})
	if err := app.root.Execute(); err == nil {
		test.Fatal("expected error for +level= on modify")
	}

	app.root.SetArgs([]string{"task", "modify", task.ShortID, "--", "-level=story"})
	if err := app.root.Execute(); err == nil {
		test.Fatal("expected error for -level= on modify")
	}
}

func TestRunCreate_Order(test *testing.T) {
	app, taskSvc := testApp(test)
	ctx := context.Background()

	app.root.SetArgs([]string{"task", "create", "ordered", "order=2.5"})
	if err := app.root.Execute(); err != nil {
		test.Fatalf("create: %v", err)
	}

	tasks, listErr := taskSvc.List(ctx, nil)

	if listErr != nil {
		test.Fatalf("List: %v", listErr)
	}

	if len(tasks) != 1 {
		test.Fatalf("expected 1 task, got %d", len(tasks))
	}
	if tasks[0].Order == nil || *tasks[0].Order != 2.5 {
		test.Fatalf("expected order 2.5, got %v", tasks[0].Order)
	}
}

func TestRunCreate_OrderEmptyRejected(test *testing.T) {
	app, _ := testApp(test)

	app.root.SetArgs([]string{"task", "create", "bad", "order="})
	err := app.root.Execute()
	if err == nil {
		test.Fatal("expected error for empty order= on create")
	}
	if !strings.Contains(err.Error(), "order=") {
		test.Fatalf("expected error to mention 'order=', got %q", err)
	}
}

func TestRunCreate_OrderInvalidValueRejected(test *testing.T) {
	app, _ := testApp(test)

	app.root.SetArgs([]string{"task", "create", "bad", "order=notanumber"})
	err := app.root.Execute()
	if err == nil {
		test.Fatal("expected error for non-numeric order=")
	}
	if !strings.Contains(err.Error(), "notanumber") {
		test.Fatalf("expected error to name offending token, got %q", err)
	}
}

func TestRunCreate_OrderModifierRejected(test *testing.T) {
	app, _ := testApp(test)

	app.root.SetArgs([]string{"task", "create", "bad", "+order=5"})
	err := app.root.Execute()
	if err == nil {
		test.Fatal("expected error for +order= on create")
	}
	if !strings.Contains(err.Error(), "move") {
		test.Fatalf("expected error to reference 'move', got %q", err)
	}
}

func TestRunModify_OrderAbsolute(test *testing.T) {
	app, taskSvc := testApp(test)
	ctx := context.Background()

	task := &domain.Task{Title: "reorder me"}
	if err := taskSvc.Create(ctx, task); err != nil {
		test.Fatalf("Create: %v", err)
	}

	app.root.SetArgs([]string{"task", "modify", task.ShortID, "order=4.0"})
	if err := app.root.Execute(); err != nil {
		test.Fatalf("modify: %v", err)
	}

	got, getErr := taskSvc.GetByShortID(ctx, task.ShortID)

	if getErr != nil {
		test.Fatalf("GetByShortID: %v", getErr)
	}

	if got.Order == nil || *got.Order != 4.0 {
		test.Fatalf("expected order 4.0, got %v", got.Order)
	}
}

func TestRunModify_OrderClear(test *testing.T) {
	app, taskSvc := testApp(test)
	ctx := context.Background()

	task := &domain.Task{Title: "clear order"}
	if err := taskSvc.Create(ctx, task); err != nil {
		test.Fatalf("Create: %v", err)
	}

	app.root.SetArgs([]string{"task", "modify", task.ShortID, "order="})
	if err := app.root.Execute(); err != nil {
		test.Fatalf("modify: %v", err)
	}

	got, getErr := taskSvc.GetByShortID(ctx, task.ShortID)

	if getErr != nil {
		test.Fatalf("GetByShortID: %v", getErr)
	}

	if got.Order != nil {
		test.Fatalf("expected cleared order, got %v", *got.Order)
	}
}

func TestRunModify_OrderModifierRejected(test *testing.T) {
	app, taskSvc := testApp(test)
	ctx := context.Background()

	task := &domain.Task{Title: "mod-reject"}
	if err := taskSvc.Create(ctx, task); err != nil {
		test.Fatalf("Create: %v", err)
	}

	app.root.SetArgs([]string{"task", "modify", task.ShortID, "+order=5"})
	err := app.root.Execute()
	if err == nil {
		test.Fatal("expected error for +order= on modify")
	}
	if !strings.Contains(err.Error(), "move") {
		test.Fatalf("expected error to reference 'move', got %q", err)
	}
}

func TestRunModify_OrderInvalidValueRejected(test *testing.T) {
	app, taskSvc := testApp(test)
	ctx := context.Background()

	task := &domain.Task{Title: "bad-val"}
	if err := taskSvc.Create(ctx, task); err != nil {
		test.Fatalf("Create: %v", err)
	}

	app.root.SetArgs([]string{"task", "modify", task.ShortID, "order=foo"})
	err := app.root.Execute()
	if err == nil {
		test.Fatal("expected error for non-numeric order= on modify")
	}
	if !strings.Contains(err.Error(), "foo") {
		test.Fatalf("expected error to name offending token, got %q", err)
	}
}

func TestRunLevelCheck_NoTaxonomy_ExitsClean(test *testing.T) {
	app, taskRepo, _ := testAppWithTaxonomy(test, nil)

	// Even a task with no level is fine when no taxonomy is configured.
	seedViolatingTask(test, taskRepo, "aaaaaaaa", "no-level", nil)

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"task", "level-check"})
	if err := app.root.Execute(); err != nil {
		test.Fatalf("level-check: %v", err)
	}
	if buf.String() != "" {
		test.Fatalf("expected empty output, got %q", buf.String())
	}
}

func TestRunLevelCheck_ViolationsProduceExitError(test *testing.T) {
	levels := [][]string{{"milestone"}, {"story"}, {"task", "spike"}}
	app, taskRepo, _ := testAppWithTaxonomy(test, levels)

	seedViolatingTask(test, taskRepo, "11111111", "no-level", nil)

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"task", "level-check"})
	err := app.root.Execute()
	if !errors.Is(err, ErrLevelViolations) {
		test.Fatalf("expected ErrLevelViolations, got %v", err)
	}
	if !strings.Contains(buf.String(), "11111111") {
		test.Fatalf("expected short ID in output, got: %q", buf.String())
	}
	if !strings.Contains(buf.String(), "missing") {
		test.Fatalf("expected reason 'missing' in output, got: %q", buf.String())
	}
}

func TestRunLevelCheck_JSON(test *testing.T) {
	levels := [][]string{{"milestone"}, {"story"}}
	app, taskRepo, _ := testAppWithTaxonomy(test, levels)

	bogus := "bogus"
	seedViolatingTask(test, taskRepo, "22222222", "bad-level", &bogus)

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"task", "level-check", "--format", "json"})
	err := app.root.Execute()
	if !errors.Is(err, ErrLevelViolations) {
		test.Fatalf("expected ErrLevelViolations, got %v", err)
	}

	var payload []map[string]any
	if decodeErr := json.Unmarshal(buf.Bytes(), &payload); decodeErr != nil {
		test.Fatalf("decode JSON: %v\nraw: %s", decodeErr, buf.String())
	}
	if len(payload) != 1 {
		test.Fatalf("expected 1 JSON entry, got %d", len(payload))
	}
	if payload[0]["reason"] != "unknown_level" {
		test.Fatalf("reason: got %v, want unknown_level", payload[0]["reason"])
	}
	task := payload[0]["task"].(map[string]any)
	if task["short_id"] != "22222222" {
		test.Fatalf("short_id: got %v, want 22222222", task["short_id"])
	}
	tax := payload[0]["taxonomy"].(map[string]any)
	if _, ok := tax["ranks"]; !ok {
		test.Fatalf("expected taxonomy.ranks in JSON payload: %v", payload[0])
	}
	if payload[0]["source"] != "workspace_default" {
		test.Fatalf("source: got %v, want workspace_default", payload[0]["source"])
	}
}

func TestRunLevelCheck_FilterScopesResults(test *testing.T) {
	levels := [][]string{{"milestone"}, {"story"}}
	app, taskRepo, _ := testAppWithTaxonomy(test, levels)

	// Default project task — should be flagged.
	seedViolatingTask(test, taskRepo, "33333333", "default-bad", nil)
	// Later, we'll create an override project so we can filter to it.
	// For this scope-check, filter by status=pending picks up the task above.

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"task", "level-check", "status=pending"})
	err := app.root.Execute()
	if !errors.Is(err, ErrLevelViolations) {
		test.Fatalf("expected ErrLevelViolations, got %v", err)
	}
	if !strings.Contains(buf.String(), "33333333") {
		test.Fatalf("expected short ID in output, got: %q", buf.String())
	}

	// Filter that excludes the violating task (status=completed) returns clean.
	buf.Reset()
	app.root.SetArgs([]string{"task", "level-check", "status=completed"})
	if err := app.root.Execute(); err != nil {
		test.Fatalf("expected clean exit, got %v", err)
	}
	if buf.String() != "" {
		test.Fatalf("expected empty output when filter has no matches, got %q", buf.String())
	}
}
