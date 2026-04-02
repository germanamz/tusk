# Tag Support Phase 2: CLI Wiring Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Wire `TagService` into the CLI app and enable tag support in `add`, `modify`, `list`, and `info` commands.

**Architecture:** The `tui.App` struct gains a `tagSvc` field. The `New()` constructor changes signature to accept it. `cmd/tusk/main.go` instantiates `TagRepo` and `TagService` and passes them through. Each CLI command is updated to call `tagSvc` methods after the existing task operations.

**Tech Stack:** Go, Cobra CLI, `internal/service.TagService`

**Spec:** `docs/superpowers/specs/2026-04-02-tag-support-design.md`

**Prerequisite:** Phase 1 (TagService) must be complete.

---

### Task 1: Wire TagService into App and main.go

**Files:**
- Modify: `internal/tui/app.go`
- Modify: `cmd/tusk/main.go`

- [ ] **Step 1: Update App struct and New() constructor**

In `internal/tui/app.go`, add the `tagSvc` field to `App` and update `New()`:

Change the import block to add:
```go
"github.com/germanamz/tusk/internal/service"
```

Change the `App` struct from:
```go
type App struct {
	taskSvc     *service.TaskService
	projectRepo repository.ProjectRepository
	root        *cobra.Command
	format      string
}
```
to:
```go
type App struct {
	taskSvc     *service.TaskService
	tagSvc      *service.TagService
	projectRepo repository.ProjectRepository
	root        *cobra.Command
	format      string
}
```

Change the `New` function signature from:
```go
func New(taskSvc *service.TaskService, projectRepo repository.ProjectRepository) *App {
	a := &App{
		taskSvc:     taskSvc,
		projectRepo: projectRepo,
	}
```
to:
```go
func New(taskSvc *service.TaskService, tagSvc *service.TagService, projectRepo repository.ProjectRepository) *App {
	a := &App{
		taskSvc:     taskSvc,
		tagSvc:      tagSvc,
		projectRepo: projectRepo,
	}
```

- [ ] **Step 2: Update main.go to create TagRepo, TagService, and pass to App**

In `cmd/tusk/main.go`, add `tagRepo` and `tagSvc` creation. Change the wiring section from:

```go
	workflowSvc := service.NewWorkflowService(workflowRepo)
	taskSvc := service.NewTaskService(taskRepo, annotationRepo, projectRepo, workflowSvc)

	app := tui.New(taskSvc, projectRepo)
```

to:

```go
	tagRepo := sqlite.NewTagRepo(db)

	workflowSvc := service.NewWorkflowService(workflowRepo)
	taskSvc := service.NewTaskService(taskRepo, annotationRepo, projectRepo, workflowSvc)
	tagSvc := service.NewTagService(tagRepo)

	app := tui.New(taskSvc, tagSvc, projectRepo)
```

- [ ] **Step 3: Verify it compiles**

Run: `cd /Users/germanamz/projects/tusk && go build ./...`

Expected: Compiles successfully. (If any other file calls `tui.New()` with the old signature, it will fail here — fix those call sites by passing `nil` for `tagSvc` in test code.)

- [ ] **Step 4: Run full test suite**

Run: `cd /Users/germanamz/projects/tusk && go test ./... -v`

Expected: All tests PASS. If any test creates an `App` via `tui.New()`, update it to pass the new `tagSvc` parameter (use `nil` if not testing tags).

- [ ] **Step 5: Commit**

```bash
git add internal/tui/app.go cmd/tusk/main.go
git commit -m "feat(tui): wire TagService into App and main.go"
```

If you had to fix test files in step 4, include them in the commit too.

---

### Task 2: Enable tags in `add` command

**Files:**
- Modify: `internal/tui/commands.go`

- [ ] **Step 1: Update runAdd to support tags**

In `internal/tui/commands.go`, in the `runAdd` function, remove the "tags not yet supported" block and add tag assignment after task creation.

Remove these lines (around lines 36-39):
```go
	// Tags not yet supported
	if len(parsed.Tags) > 0 || len(parsed.ExclTags) > 0 {
		return fmt.Errorf("tags not yet supported")
	}
```

After the `taskSvc.Create()` call and before `renderMutationResult`, add tag assignment:

```go
	if err := a.taskSvc.Create(ctx, task); err != nil {
		return fmt.Errorf("%s", err)
	}

	// Assign tags if any were specified
	if len(parsed.Tags) > 0 {
		if err := a.tagSvc.AssignToTask(ctx, task.ID, parsed.Tags); err != nil {
			return fmt.Errorf("assigning tags: %w", err)
		}
	}

	return renderMutationResult(cmd.OutOrStdout(), "Created", task, a.format)
```

Note: `-tag` (ExclTags) is not meaningful for `add` — you can't remove tags from a brand new task. Ignore `parsed.ExclTags` in `runAdd`. Only the `+tag` syntax is used here.

- [ ] **Step 2: Verify it compiles**

Run: `cd /Users/germanamz/projects/tusk && go build ./...`

Expected: Compiles successfully.

- [ ] **Step 3: Smoke test manually**

Run: `cd /Users/germanamz/projects/tusk && go run ./cmd/tusk --db /tmp/tusk-test-tags.db add "Test tag task" +bug +urgent`

Expected: Output like `Created task a3f8b2c1` (short ID will vary). No errors.

Verify the tag was stored:
Run: `cd /Users/germanamz/projects/tusk && go run ./cmd/tusk --db /tmp/tusk-test-tags.db list`

Expected: The task appears in the list.

- [ ] **Step 4: Commit**

```bash
git add internal/tui/commands.go
git commit -m "feat(tui): enable tag assignment in add command"
```

---

### Task 3: Enable tags in `modify` command

**Files:**
- Modify: `internal/tui/commands.go`

- [ ] **Step 1: Update runModify to support tags**

In `internal/tui/commands.go`, in the `runModify` function, remove the "tags not yet supported" block and add tag assign/remove after the update call.

Remove these lines (around lines 149-153):
```go
	// Tags not yet supported
	parsed := parseArgs(args[1:])
	if len(parsed.Tags) > 0 || len(parsed.ExclTags) > 0 {
		return fmt.Errorf("tags not yet supported")
	}
```

Replace with just:
```go
	parsed := parseArgs(args[1:])
```

After the `taskSvc.Update()` call and before `renderMutationResult`, add tag operations. The `updated` variable already holds the task. Change from:

```go
	updated, err := a.taskSvc.Update(ctx, upd)
	if err != nil {
		return fmt.Errorf("%s", formatError(err, shortID))
	}

	return renderMutationResult(cmd.OutOrStdout(), "Modified", updated, a.format)
```

to:

```go
	updated, err := a.taskSvc.Update(ctx, upd)
	if err != nil {
		return fmt.Errorf("%s", formatError(err, shortID))
	}

	// Add new tags
	if len(parsed.Tags) > 0 {
		if err := a.tagSvc.AssignToTask(ctx, updated.ID, parsed.Tags); err != nil {
			return fmt.Errorf("assigning tags: %w", err)
		}
	}

	// Remove excluded tags
	if len(parsed.ExclTags) > 0 {
		if err := a.tagSvc.RemoveFromTask(ctx, updated.ID, parsed.ExclTags); err != nil {
			return fmt.Errorf("removing tags: %w", err)
		}
	}

	return renderMutationResult(cmd.OutOrStdout(), "Modified", updated, a.format)
```

- [ ] **Step 2: Verify it compiles**

Run: `cd /Users/germanamz/projects/tusk && go build ./...`

Expected: Compiles successfully.

- [ ] **Step 3: Smoke test manually**

Using the test DB from Task 2, find the task short ID from the list output, then:

Run: `cd /Users/germanamz/projects/tusk && go run ./cmd/tusk --db /tmp/tusk-test-tags.db modify <SHORT_ID> +newfeature -bug`

Expected: Output like `Modified task <SHORT_ID>`. No errors.

- [ ] **Step 4: Commit**

```bash
git add internal/tui/commands.go
git commit -m "feat(tui): enable tag add/remove in modify command"
```

---

### Task 4: Enable tag filtering in `list` command

**Files:**
- Modify: `internal/tui/filter.go`

- [ ] **Step 1: Update buildTaskFilter to support tags**

In `internal/tui/filter.go`, in the `buildTaskFilter` function, remove the "tag filtering not yet supported" block and wire tags through to the filter.

Remove these lines (around lines 117-120):
```go
	// Tags not yet supported
	if len(p.Tags) > 0 || len(p.ExclTags) > 0 {
		return f, fmt.Errorf("tag filtering not yet supported")
	}
```

Replace with:
```go
	// Tag filters
	if len(p.Tags) > 0 {
		f.Tags = p.Tags
	}
	if len(p.ExclTags) > 0 {
		f.ExcludeTags = p.ExclTags
	}
```

- [ ] **Step 2: Verify it compiles**

Run: `cd /Users/germanamz/projects/tusk && go build ./...`

Expected: Compiles successfully.

- [ ] **Step 3: Smoke test manually**

Using the test DB from previous tasks:

Run: `cd /Users/germanamz/projects/tusk && go run ./cmd/tusk --db /tmp/tusk-test-tags.db list +urgent`

Expected: Only tasks tagged `+urgent` appear.

Run: `cd /Users/germanamz/projects/tusk && go run ./cmd/tusk --db /tmp/tusk-test-tags.db list -urgent`

Expected: Tasks NOT tagged `urgent` appear.

- [ ] **Step 4: Run full test suite**

Run: `cd /Users/germanamz/projects/tusk && go test ./... -v`

Expected: All tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/filter.go
git commit -m "feat(tui): enable tag filtering in list command"
```
