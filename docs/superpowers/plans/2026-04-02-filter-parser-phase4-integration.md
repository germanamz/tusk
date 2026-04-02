# Filter Parser Phase 4: TUI Integration and Migration

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Wire the new `internal/filter` package into the TUI commands, replacing the old `parseArgs`/`buildTaskFilter` code, and clean up the old implementation.

**Architecture:** The TUI `runList` and `runAdd` handlers switch from calling `parseArgs()` + `buildTaskFilter()` to calling `filter.Parse()` + `resolver.Resolve()`. The `Resolver` is created once during `App` construction and stored as a field. Old parsing code and its tests are removed.

**Tech Stack:** Go standard library + existing tusk packages. Module: `github.com/germanamz/tusk`.

**Spec:** `docs/superpowers/specs/2026-04-02-filter-syntax-parser-design.md`

**Depends on:** Phases 1-3 must be complete. The `internal/filter` package must have `Parse()`, `Resolver`, and all supporting types.

---

### Task 1: Wire Resolver into App

**Files:**
- Modify: `internal/tui/app.go` (add Resolver field, update constructor)
- Modify: `internal/tui/commands.go` (update `runList` to use filter package)

- [ ] **Step 1: Read current state of files**

Read these files to confirm their current contents match what's expected:
- `internal/tui/app.go`
- `internal/tui/commands.go` (lines 101-135, the `runList` handler)

- [ ] **Step 2: Add Resolver to App struct and constructor**

In `internal/tui/app.go`, add the import and modify the `App` struct and `New` function.

Add to imports:

```go
"github.com/germanamz/tusk/internal/filter"
```

Add to the `App` struct (alongside the existing fields):

```go
resolver *filter.Resolver
```

Update the `New` function to create the resolver. Change the function signature to:

```go
func New(taskSvc *service.TaskService, tagSvc *service.TagService, projectRepo repository.ProjectRepository, taskRepo repository.TaskRepository) *App {
```

Add this line inside `New` after the `App` struct literal is created:

```go
a.resolver = filter.NewResolver(projectRepo, taskRepo)
```

Also add `taskRepo` to the `App` struct:

```go
taskRepo repository.TaskRepository
```

And set it in the constructor:

```go
taskRepo: taskRepo,
```

- [ ] **Step 3: Update runList to use filter.Parse + resolver.Resolve**

Replace the body of `runList` in `internal/tui/commands.go` (currently lines 101-135). The new implementation:

```go
func (a *App) runList(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	input := strings.Join(args, " ")
	fs, parseErrs := filter.Parse(input)
	if len(parseErrs) > 0 {
		return fmt.Errorf("%s", filter.FormatErrors(parseErrs))
	}

	tf, resolveErrs := a.resolver.Resolve(ctx, fs)
	if len(resolveErrs) > 0 {
		msgs := make([]string, len(resolveErrs))
		for i, e := range resolveErrs {
			msgs[i] = e.Error()
		}
		return fmt.Errorf("%s", strings.Join(msgs, "\n"))
	}

	tasks, err := a.taskSvc.List(ctx, *tf)
	if err != nil {
		return err
	}

	// Fetch tags for all tasks in one query
	taskIDs := make([]uuid.UUID, len(tasks))
	for i, t := range tasks {
		taskIDs[i] = t.ID
	}
	tagsByTaskID, err := a.tagSvc.GetTaskTagsBatch(ctx, taskIDs)
	if err != nil {
		return fmt.Errorf("loading tags: %w", err)
	}

	// Convert uuid.UUID keys to string keys for the render layer
	taskTags := make(map[string][]*domain.Tag, len(tagsByTaskID))
	for id, tags := range tagsByTaskID {
		taskTags[id.String()] = tags
	}

	return renderTaskList(cmd.OutOrStdout(), tasks, taskTags, a.format)
}
```

Add the import for the filter package to `commands.go`:

```go
"github.com/germanamz/tusk/internal/filter"
```

- [ ] **Step 4: Update cmd/tusk/main.go to pass taskRepo to New**

Read `cmd/tusk/main.go` to find the call to `tui.New(...)`. It currently passes `(taskSvc, tagSvc, projectRepo)`. Add `taskRepo` as the fourth argument:

```go
app := tui.New(taskSvc, tagSvc, projectRepo, taskRepo)
```

Where `taskRepo` is the `sqlite.NewTaskRepo(store.DB())` that should already be created earlier in `main.go` (it's used to create `taskSvc`). If it's created inline, extract it to a variable first.

- [ ] **Step 5: Verify it compiles**

Run: `cd /Users/germanamz/projects/tusk && go build ./...`
Expected: No errors.

- [ ] **Step 6: Run existing tests**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/tui/ -run TestRunList -v`
Expected: PASS (existing tests should still work since behavior is equivalent).

- [ ] **Step 7: Commit**

```bash
cd /Users/germanamz/projects/tusk
git add internal/tui/app.go internal/tui/commands.go cmd/tusk/main.go
git commit -m "refactor(tui): wire filter.Parse + Resolver into runList handler"
```

---

### Task 2: Migrate runAdd to Use filter.Parse

**Files:**
- Modify: `internal/tui/commands.go` (update `runAdd` handler)

- [ ] **Step 1: Read current runAdd**

Read `internal/tui/commands.go` lines 28-91 to confirm current state.

- [ ] **Step 2: Replace runAdd body**

Replace the body of `runAdd` (currently lines 28-91) with the new implementation that uses `filter.Parse()`:

```go
func (a *App) runAdd(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	input := strings.Join(args, " ")
	fs, parseErrs := filter.Parse(input)
	if len(parseErrs) > 0 {
		return fmt.Errorf("%s", filter.FormatErrors(parseErrs))
	}

	title := fs.Title()
	if title == "" {
		return fmt.Errorf("title is required")
	}

	task := &domain.Task{
		Title: title,
	}

	// Project
	if f, ok := fs.GetField("project"); ok {
		project, err := a.projectRepo.GetByName(ctx, f.Value)
		if err != nil {
			return fmt.Errorf("project %q not found", f.Value)
		}
		task.ProjectID = &project.ID
	}

	// Priority
	if f, ok := fs.GetField("priority"); ok {
		p, err := filter.ParsePriorityValue(f.Value)
		if err != nil {
			return err
		}
		task.Priority = p
	}

	// Status (rarely used, defaults to pending in service)
	if f, ok := fs.GetField("status"); ok {
		task.Status = f.Value
	}

	// Due date
	if f, ok := fs.GetField("due"); ok {
		d, err := filter.ParseDateValue(f.Value)
		if err != nil {
			return err
		}
		task.DueAt = &d
	}

	// Parent
	if f, ok := fs.GetField("parent"); ok {
		parent, err := a.taskSvc.GetByShortID(ctx, f.Value)
		if err != nil {
			return fmt.Errorf("%s", formatError(err, f.Value))
		}
		task.ParentID = &parent.ID
	}

	if err := a.taskSvc.Create(ctx, task); err != nil {
		return fmt.Errorf("%s", err)
	}

	// Assign tags if any were specified
	incTags := fs.IncludeTags()
	if len(incTags) > 0 {
		if err := a.tagSvc.AssignToTask(ctx, task.ID, incTags); err != nil {
			return fmt.Errorf("assigning tags: %w", err)
		}
	}

	// Fetch tags for output
	tags, err := a.tagSvc.GetTaskTags(ctx, task.ID)
	if err != nil {
		return fmt.Errorf("loading tags: %w", err)
	}

	return renderMutationResult(cmd.OutOrStdout(), "Created", task, tags, a.format)
}
```

**Important:** This requires `parsePriorityValue` and `parseDate` to be exported from the filter package. Add these exported wrappers to the filter package:

In `internal/filter/validators.go`, add:

```go
// ParsePriorityValue is the exported version of parsePriorityValue for use
// by the TUI layer when creating tasks (not filtering).
func ParsePriorityValue(s string) (int, error) {
	return parsePriorityValue(s)
}
```

In `internal/filter/dates.go`, add:

```go
// ParseDateValue is the exported version of parseDate for use by the TUI
// layer when creating tasks (not filtering).
func ParseDateValue(s string) (time.Time, error) {
	return parseDate(s)
}
```

- [ ] **Step 3: Verify it compiles**

Run: `cd /Users/germanamz/projects/tusk && go build ./...`
Expected: No errors.

- [ ] **Step 4: Run existing tests**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/tui/ -v`
Expected: All PASS.

- [ ] **Step 5: Commit**

```bash
cd /Users/germanamz/projects/tusk
git add internal/tui/commands.go internal/filter/validators.go internal/filter/dates.go
git commit -m "refactor(tui): migrate runAdd to use filter.Parse for argument parsing"
```

---

### Task 3: Migrate runModify and Remove Old Code

**Files:**
- Modify: `internal/tui/commands.go` (update `runModify` if it uses `parseArgs`)
- Modify: `internal/tui/filter.go` (remove old functions)
- Modify: `internal/tui/filter_test.go` (remove old tests)

- [ ] **Step 1: Check if runModify uses parseArgs**

Read `internal/tui/commands.go` and search for all calls to `parseArgs`. Update any remaining callers to use `filter.Parse()` instead.

For `runModify`, the pattern should be similar to `runAdd`: parse the args after the short_id (first arg) using `filter.Parse()`, then apply the fields.

If `runModify` uses `parseArgs`, update it to:

```go
func (a *App) runModify(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	shortID := args[0]

	task, err := a.taskSvc.GetByShortID(ctx, shortID)
	if err != nil {
		return fmt.Errorf("%s", formatError(err, shortID))
	}

	input := strings.Join(args[1:], " ")
	fs, parseErrs := filter.Parse(input)
	if len(parseErrs) > 0 {
		return fmt.Errorf("%s", filter.FormatErrors(parseErrs))
	}

	// Apply fields from the parsed filter
	if f, ok := fs.GetField("priority"); ok {
		p, err := filter.ParsePriorityValue(f.Value)
		if err != nil {
			return err
		}
		task.Priority = p
	}

	if f, ok := fs.GetField("status"); ok {
		task.Status = f.Value
	}

	if f, ok := fs.GetField("due"); ok {
		d, err := filter.ParseDateValue(f.Value)
		if err != nil {
			return err
		}
		task.DueAt = &d
	}

	if f, ok := fs.GetField("project"); ok {
		project, err := a.projectRepo.GetByName(ctx, f.Value)
		if err != nil {
			return fmt.Errorf("project %q not found", f.Value)
		}
		task.ProjectID = &project.ID
	}

	// Title from free text (if any)
	if title := fs.Title(); title != "" {
		task.Title = title
	}

	if err := a.taskSvc.Update(ctx, task); err != nil {
		return fmt.Errorf("%s", err)
	}

	// Handle tag changes
	incTags := fs.IncludeTags()
	if len(incTags) > 0 {
		if err := a.tagSvc.AssignToTask(ctx, task.ID, incTags); err != nil {
			return fmt.Errorf("assigning tags: %w", err)
		}
	}
	excTags := fs.ExcludeTags()
	if len(excTags) > 0 {
		if err := a.tagSvc.RemoveFromTask(ctx, task.ID, excTags); err != nil {
			return fmt.Errorf("removing tags: %w", err)
		}
	}

	tags, err := a.tagSvc.GetTaskTags(ctx, task.ID)
	if err != nil {
		return fmt.Errorf("loading tags: %w", err)
	}

	return renderMutationResult(cmd.OutOrStdout(), "Modified", task, tags, a.format)
}
```

**Note:** Read the actual `runModify` implementation first. If it doesn't use `parseArgs` or uses it differently, adapt accordingly. The key point is that all calls to `parseArgs` must be removed.

- [ ] **Step 2: Remove old code from internal/tui/filter.go**

After confirming no callers remain, delete the following from `internal/tui/filter.go`:
- The `ParsedArgs` struct (lines 16-21)
- The `parseArgs` function (lines 31-51)
- The `parsePriority` function (lines 55-66) — replaced by `filter.ParsePriorityValue`
- The `parseDate` function (lines 71-107) — replaced by `filter.ParseDateValue`
- The `buildTaskFilter` function (lines 112-172) — replaced by `filter.Resolve`

If `filter.go` is now empty (no remaining functions), delete the file entirely.

- [ ] **Step 3: Remove old tests from internal/tui/filter_test.go**

Delete all tests that test the removed functions:
- `TestParseArgs_*` (all variants)
- `TestParsePriority_*` (all variants)
- `TestParseDate_*` (all variants)
- `TestBuildTaskFilter_*` (all variants)
- The `testProjectRepo` helper function

If the file is now empty (no remaining tests), delete the file entirely.

- [ ] **Step 4: Verify it compiles and tests pass**

Run: `cd /Users/germanamz/projects/tusk && go build ./... && go test ./... -v`
Expected: All compile, all tests pass.

- [ ] **Step 5: Commit**

```bash
cd /Users/germanamz/projects/tusk
git add -A
git commit -m "refactor(tui): remove old parseArgs/buildTaskFilter, all commands use filter package"
```

---

### Task 4: Update Callers That Create App and Final Verification

**Files:**
- Modify: any test files that construct `tui.App` via `tui.New(...)` (add the new `taskRepo` parameter)
- No new files

- [ ] **Step 1: Find all callers of tui.New**

Search the codebase for calls to `tui.New(` to find all places that need the updated signature:

Run: `grep -rn "tui.New(" --include="*.go" /Users/germanamz/projects/tusk/`

This will show `cmd/tusk/main.go` (already updated in Task 1) and any test files.

- [ ] **Step 2: Update test callers**

For each test file that calls `tui.New(...)`, add the fourth `taskRepo` parameter. If the test uses `nil` for the repos (as mentioned in `app.go` line 19: "taskSvc, tagSvc, and projectRepo may be nil for testing command registration"), pass `nil` for `taskRepo` too:

```go
// Before:
app := tui.New(taskSvc, tagSvc, projectRepo)

// After:
app := tui.New(taskSvc, tagSvc, projectRepo, taskRepo)
```

For tests that use `nil`:
```go
// Before:
app := tui.New(nil, nil, nil)

// After:
app := tui.New(nil, nil, nil, nil)
```

- [ ] **Step 3: Run full test suite**

Run: `cd /Users/germanamz/projects/tusk && go test ./... -v`
Expected: All PASS across all packages.

- [ ] **Step 4: Run go vet**

Run: `cd /Users/germanamz/projects/tusk && go vet ./...`
Expected: No issues.

- [ ] **Step 5: Commit**

```bash
cd /Users/germanamz/projects/tusk
git add -A
git commit -m "fix(tui): update all tui.New callers with new taskRepo parameter"
```

- [ ] **Step 6: Final verification — build binary and smoke test**

Run: `cd /Users/germanamz/projects/tusk && go build -o /tmp/tusk ./cmd/tusk/ && /tmp/tusk list --help`
Expected: The binary builds and the list command shows its help text with `[filters...]` usage.
