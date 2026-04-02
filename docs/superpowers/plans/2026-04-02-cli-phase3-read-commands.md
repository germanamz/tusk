# CLI Phase 3: Read Commands (`list`, `info`)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement the `list` and `info` commands so users can view tasks from the CLI.

**Architecture:** Each command method in `commands.go` parses args with `parseArgs()`, translates to service calls, and renders output with the functions from `render.go`. The `list` command also uses a `buildTaskFilter()` function in `filter.go` to convert parsed args into a `domain.TaskFilter`.

**Tech Stack:** Existing `internal/tui` functions from Phase 1, `internal/service.TaskService`, `internal/repository.ProjectRepository`.

**Depends on:** Phase 1 (arg parsing, rendering) and Phase 2 (App struct, Cobra tree, main.go).

---

### Task 1: Build task filter from parsed args

**Files:**
- Modify: `internal/tui/filter.go`
- Modify: `internal/tui/filter_test.go`

This function converts `ParsedArgs` into a `domain.TaskFilter`, resolving project names to UUIDs.

- [ ] **Step 1: Write the failing tests**

Append to `internal/tui/filter_test.go`:

```go
import (
	"context"

	"github.com/germanamz/tusk/internal/domain"
	"github.com/germanamz/tusk/internal/sqlite"
	"github.com/germanamz/tusk/migrations"
)

// testProjectRepo creates an in-memory SQLite store and returns its ProjectRepo.
// The store includes the seeded _default project.
func testProjectRepo(t *testing.T) *sqlite.ProjectRepo {
	t.Helper()
	store, err := sqlite.New(":memory:", migrations.FS)
	if err != nil {
		t.Fatalf("opening test store: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return sqlite.NewProjectRepo(store.DB())
}

func TestBuildTaskFilter_DefaultStatuses(t *testing.T) {
	repo := testProjectRepo(t)
	p := ParsedArgs{Fields: map[string]string{}}

	filter, err := buildTaskFilter(context.Background(), p, repo)
	if err != nil {
		t.Fatalf("buildTaskFilter: %v", err)
	}
	if len(filter.Statuses) != 2 {
		t.Fatalf("expected 2 default statuses, got %d", len(filter.Statuses))
	}
	if filter.Statuses[0] != "pending" || filter.Statuses[1] != "active" {
		t.Fatalf("expected [pending active], got %v", filter.Statuses)
	}
}

func TestBuildTaskFilter_ExplicitStatus(t *testing.T) {
	repo := testProjectRepo(t)
	p := ParsedArgs{
		Fields: map[string]string{"status": "completed"},
	}

	filter, err := buildTaskFilter(context.Background(), p, repo)
	if err != nil {
		t.Fatalf("buildTaskFilter: %v", err)
	}
	if len(filter.Statuses) != 1 || filter.Statuses[0] != "completed" {
		t.Fatalf("expected [completed], got %v", filter.Statuses)
	}
}

func TestBuildTaskFilter_MultipleStatuses(t *testing.T) {
	repo := testProjectRepo(t)
	p := ParsedArgs{
		Fields: map[string]string{"status": "pending,active,completed"},
	}

	filter, err := buildTaskFilter(context.Background(), p, repo)
	if err != nil {
		t.Fatalf("buildTaskFilter: %v", err)
	}
	if len(filter.Statuses) != 3 {
		t.Fatalf("expected 3 statuses, got %d", len(filter.Statuses))
	}
}

func TestBuildTaskFilter_ProjectByName(t *testing.T) {
	repo := testProjectRepo(t)
	p := ParsedArgs{
		Fields: map[string]string{"project": "_default"},
	}

	filter, err := buildTaskFilter(context.Background(), p, repo)
	if err != nil {
		t.Fatalf("buildTaskFilter: %v", err)
	}
	if filter.ProjectID == nil {
		t.Fatal("expected ProjectID to be set")
	}
}

func TestBuildTaskFilter_ProjectNotFound(t *testing.T) {
	repo := testProjectRepo(t)
	p := ParsedArgs{
		Fields: map[string]string{"project": "nonexistent"},
	}

	_, err := buildTaskFilter(context.Background(), p, repo)
	if err == nil {
		t.Fatal("expected error for nonexistent project")
	}
}

func TestBuildTaskFilter_PriorityRange(t *testing.T) {
	repo := testProjectRepo(t)
	p := ParsedArgs{
		Fields: map[string]string{"priority": "2..4"},
	}

	filter, err := buildTaskFilter(context.Background(), p, repo)
	if err != nil {
		t.Fatalf("buildTaskFilter: %v", err)
	}
	if filter.PriorityMin == nil || *filter.PriorityMin != 2 {
		t.Fatalf("expected PriorityMin=2, got %v", filter.PriorityMin)
	}
	if filter.PriorityMax == nil || *filter.PriorityMax != 4 {
		t.Fatalf("expected PriorityMax=4, got %v", filter.PriorityMax)
	}
}

func TestBuildTaskFilter_PrioritySingle(t *testing.T) {
	repo := testProjectRepo(t)
	p := ParsedArgs{
		Fields: map[string]string{"priority": "3"},
	}

	filter, err := buildTaskFilter(context.Background(), p, repo)
	if err != nil {
		t.Fatalf("buildTaskFilter: %v", err)
	}
	if filter.PriorityMin == nil || *filter.PriorityMin != 3 {
		t.Fatalf("expected PriorityMin=3, got %v", filter.PriorityMin)
	}
	if filter.PriorityMax == nil || *filter.PriorityMax != 3 {
		t.Fatalf("expected PriorityMax=3, got %v", filter.PriorityMax)
	}
}

func TestBuildTaskFilter_Tags(t *testing.T) {
	repo := testProjectRepo(t)
	p := ParsedArgs{
		Fields: map[string]string{},
		Tags:   []string{"api"},
	}

	_, err := buildTaskFilter(context.Background(), p, repo)
	if err == nil || err.Error() != "tag filtering not yet supported" {
		t.Fatalf("expected 'tag filtering not yet supported' error, got %v", err)
	}
}

func TestBuildTaskFilter_ExclTags(t *testing.T) {
	repo := testProjectRepo(t)
	p := ParsedArgs{
		Fields:   map[string]string{},
		ExclTags: []string{"docs"},
	}

	_, err := buildTaskFilter(context.Background(), p, repo)
	if err == nil || err.Error() != "tag filtering not yet supported" {
		t.Fatalf("expected 'tag filtering not yet supported' error, got %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/tui/ -v -run TestBuildTaskFilter`
Expected: Compilation failure — `buildTaskFilter` not defined.

- [ ] **Step 3: Implement buildTaskFilter**

Add these imports to `internal/tui/filter.go`: `"context"` and `"github.com/germanamz/tusk/internal/domain"` and `"github.com/germanamz/tusk/internal/repository"`. Then append:

```go
// buildTaskFilter converts parsed CLI args into a domain.TaskFilter.
// If no status filter is specified, defaults to ["pending", "active"].
// Project names are resolved to UUIDs via projectRepo.
func buildTaskFilter(ctx context.Context, p ParsedArgs, projectRepo repository.ProjectRepository) (domain.TaskFilter, error) {
	var f domain.TaskFilter

	// Tags not yet supported
	if len(p.Tags) > 0 || len(p.ExclTags) > 0 {
		return f, fmt.Errorf("tag filtering not yet supported")
	}

	// Status filter
	if s, ok := p.Fields["status"]; ok {
		f.Statuses = strings.Split(s, ",")
	} else {
		f.Statuses = []string{"pending", "active"}
	}

	// Project filter
	if name, ok := p.Fields["project"]; ok {
		project, err := projectRepo.GetByName(ctx, name)
		if err != nil {
			return f, fmt.Errorf("project %q not found", name)
		}
		f.ProjectID = &project.ID
	}

	// Priority filter
	if s, ok := p.Fields["priority"]; ok {
		if strings.Contains(s, "..") {
			parts := strings.SplitN(s, "..", 2)
			min, err := parsePriority(parts[0])
			if err != nil {
				return f, err
			}
			max, err := parsePriority(parts[1])
			if err != nil {
				return f, err
			}
			f.PriorityMin = &min
			f.PriorityMax = &max
		} else {
			v, err := parsePriority(s)
			if err != nil {
				return f, err
			}
			f.PriorityMin = &v
			f.PriorityMax = &v
		}
	}

	// Parent filter
	if shortID, ok := p.Fields["parent"]; ok {
		// Store the short ID lookup for the command layer.
		// For now, parent filter requires the caller to resolve it.
		// We'll set ParentID to nil and let the command handle it.
		_ = shortID
		return f, fmt.Errorf("parent filter requires short ID resolution — use the command layer")
	}

	return f, nil
}
```

Wait — the parent filter needs a task lookup (short ID → UUID), which requires TaskService. Let's handle that in the command instead. Remove the parent filter block and keep it simpler:

Replace the parent filter block with:

```go
	// Parent filter — requires short ID → UUID resolution.
	// Handled in the command layer, not here.

	return f, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/tui/ -v -run TestBuildTaskFilter`
Expected: All tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/filter.go internal/tui/filter_test.go
git commit -m "feat(tui): implement buildTaskFilter with status, project, priority support"
```

---

### Task 2: Implement `list` command

**Files:**
- Modify: `internal/tui/commands.go`

- [ ] **Step 1: Replace the `runList` stub**

In `internal/tui/commands.go`, replace the `runList` method:

```go
func (a *App) runList(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	parsed := parseArgs(args)

	filter, err := buildTaskFilter(ctx, parsed, a.projectRepo)
	if err != nil {
		return err
	}

	// Handle parent filter if present
	if shortID, ok := parsed.Fields["parent"]; ok {
		parent, err := a.taskSvc.GetByShortID(ctx, shortID)
		if err != nil {
			return fmt.Errorf("%s", formatError(err, shortID))
		}
		filter.ParentID = &parent.ID
	}

	tasks, err := a.taskSvc.List(ctx, filter)
	if err != nil {
		return err
	}

	return renderTaskList(cmd.OutOrStdout(), tasks, a.format)
}
```

Make sure the imports in `commands.go` include `"fmt"`, `"errors"`, `"github.com/germanamz/tusk/internal/domain"`, and `"github.com/spf13/cobra"`.

- [ ] **Step 2: Write an integration test**

Append to `internal/tui/commands_test.go`:

```go
import (
	"bytes"
	"context"
	"strings"

	"github.com/germanamz/tusk/internal/domain"
	"github.com/germanamz/tusk/internal/service"
	"github.com/germanamz/tusk/internal/sqlite"
	"github.com/germanamz/tusk/migrations"
)

// testApp creates a fully wired App with an in-memory database.
func testApp(t *testing.T) (*App, *service.TaskService) {
	t.Helper()
	store, err := sqlite.New(":memory:", migrations.FS)
	if err != nil {
		t.Fatalf("opening test store: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	db := store.DB()
	taskRepo := sqlite.NewTaskRepo(db)
	annotationRepo := sqlite.NewAnnotationRepo(db)
	projectRepo := sqlite.NewProjectRepo(db)
	workflowRepo := sqlite.NewWorkflowRepo(db)

	workflowSvc := service.NewWorkflowService(workflowRepo)
	taskSvc := service.NewTaskService(taskRepo, annotationRepo, projectRepo, workflowSvc)

	app := New(taskSvc, projectRepo)
	return app, taskSvc
}

func TestRunList_Empty(t *testing.T) {
	app, _ := testApp(t)

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"list"})
	if err := app.root.Execute(); err != nil {
		t.Fatalf("list: %v", err)
	}
	if buf.String() != "" {
		t.Fatalf("expected empty output, got %q", buf.String())
	}
}

func TestRunList_WithTasks(t *testing.T) {
	app, taskSvc := testApp(t)
	ctx := context.Background()

	task := &domain.Task{Title: "Test task", Priority: 3}
	if err := taskSvc.Create(ctx, task); err != nil {
		t.Fatalf("Create: %v", err)
	}

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"list"})
	if err := app.root.Execute(); err != nil {
		t.Fatalf("list: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, task.ShortID) {
		t.Fatalf("expected short ID in output, got:\n%s", out)
	}
	if !strings.Contains(out, "H") {
		t.Fatalf("expected priority H in output, got:\n%s", out)
	}
}

func TestRunList_StatusFilter(t *testing.T) {
	app, taskSvc := testApp(t)
	ctx := context.Background()

	task := &domain.Task{Title: "Completed task"}
	if err := taskSvc.Create(ctx, task); err != nil {
		t.Fatalf("Create: %v", err)
	}
	// Start then complete
	taskSvc.Start(ctx, task.ShortID, 1)
	taskSvc.Complete(ctx, task.ShortID, 2)

	// Default list should NOT show completed tasks
	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"list"})
	if err := app.root.Execute(); err != nil {
		t.Fatalf("list: %v", err)
	}
	if strings.Contains(buf.String(), task.ShortID) {
		t.Fatalf("expected completed task to be hidden from default list")
	}

	// Explicit status filter should show it
	buf.Reset()
	app.root.SetArgs([]string{"list", "status:completed"})
	if err := app.root.Execute(); err != nil {
		t.Fatalf("list status:completed: %v", err)
	}
	if !strings.Contains(buf.String(), task.ShortID) {
		t.Fatalf("expected completed task in filtered list, got:\n%s", buf.String())
	}
}

func TestRunList_JSON(t *testing.T) {
	app, taskSvc := testApp(t)
	ctx := context.Background()

	task := &domain.Task{Title: "JSON task"}
	if err := taskSvc.Create(ctx, task); err != nil {
		t.Fatalf("Create: %v", err)
	}

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"list", "--format", "json"})
	if err := app.root.Execute(); err != nil {
		t.Fatalf("list --format json: %v", err)
	}
	if !strings.Contains(buf.String(), `"short_id"`) {
		t.Fatalf("expected JSON output, got:\n%s", buf.String())
	}
}
```

- [ ] **Step 3: Run tests to verify they pass**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/tui/ -v -run "TestRunList"`
Expected: All 4 tests PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/tui/commands.go internal/tui/commands_test.go
git commit -m "feat(tui): implement list command with status, project, priority filters"
```

---

### Task 3: Implement `info` command

**Files:**
- Modify: `internal/tui/commands.go`
- Modify: `internal/tui/commands_test.go`

- [ ] **Step 1: Replace the `runInfo` stub**

In `internal/tui/commands.go`, replace the `runInfo` method:

```go
func (a *App) runInfo(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	shortID := args[0]

	task, err := a.taskSvc.GetByShortID(ctx, shortID)
	if err != nil {
		return fmt.Errorf("%s", formatError(err, shortID))
	}

	annotations, err := a.taskSvc.GetAnnotations(ctx, shortID)
	if err != nil {
		return fmt.Errorf("loading annotations: %w", err)
	}

	return renderTaskInfo(cmd.OutOrStdout(), task, annotations, a.format)
}
```

- [ ] **Step 2: Write integration tests**

Append to `internal/tui/commands_test.go`:

```go
func TestRunInfo_HappyPath(t *testing.T) {
	app, taskSvc := testApp(t)
	ctx := context.Background()

	task := &domain.Task{Title: "Info test", Priority: 2}
	if err := taskSvc.Create(ctx, task); err != nil {
		t.Fatalf("Create: %v", err)
	}
	taskSvc.Annotate(ctx, task.ShortID, "A note")

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"info", task.ShortID})
	if err := app.root.Execute(); err != nil {
		t.Fatalf("info: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, task.ShortID) {
		t.Fatalf("expected short ID in output, got:\n%s", out)
	}
	if !strings.Contains(out, "Info test") {
		t.Fatalf("expected title in output, got:\n%s", out)
	}
	if !strings.Contains(out, "medium") {
		t.Fatalf("expected priority name in output, got:\n%s", out)
	}
	if !strings.Contains(out, "A note") {
		t.Fatalf("expected annotation in output, got:\n%s", out)
	}
}

func TestRunInfo_NotFound(t *testing.T) {
	app, _ := testApp(t)

	app.root.SetArgs([]string{"info", "nonexist"})
	err := app.root.Execute()
	if err == nil {
		t.Fatal("expected error for nonexistent task")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected 'not found' in error, got %q", err.Error())
	}
}

func TestRunInfo_JSON(t *testing.T) {
	app, taskSvc := testApp(t)
	ctx := context.Background()

	task := &domain.Task{Title: "JSON info test"}
	if err := taskSvc.Create(ctx, task); err != nil {
		t.Fatalf("Create: %v", err)
	}

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"info", task.ShortID, "--format", "json"})
	if err := app.root.Execute(); err != nil {
		t.Fatalf("info --format json: %v", err)
	}
	if !strings.Contains(buf.String(), `"short_id"`) {
		t.Fatalf("expected JSON output, got:\n%s", buf.String())
	}
}
```

- [ ] **Step 3: Run tests to verify they pass**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/tui/ -v -run TestRunInfo`
Expected: All 3 tests PASS.

- [ ] **Step 4: Run all tui tests to confirm nothing is broken**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/tui/ -v`
Expected: All tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/commands.go internal/tui/commands_test.go
git commit -m "feat(tui): implement info command with annotations display"
```
