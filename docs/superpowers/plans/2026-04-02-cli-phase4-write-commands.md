# CLI Phase 4: Write Commands (`add`, `modify`, `start`, `done`, `delete`, `annotate`)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement all mutation commands so users can create, modify, and transition tasks from the CLI.

**Architecture:** Each command parses args with `parseArgs()`, translates fields to domain types, calls the service layer, and renders a mutation result. The `start`, `done`, `delete` commands auto-fetch the task's current version before calling the service.

**Tech Stack:** Existing `internal/tui` functions, `internal/service.TaskService`, `internal/repository.ProjectRepository`.

**Depends on:** Phase 1 (arg parsing, rendering), Phase 2 (App struct, Cobra tree), Phase 3 (tests already use `testApp` helper).

---

### Task 1: Implement `add` command

**Files:**
- Modify: `internal/tui/commands.go`
- Modify: `internal/tui/commands_test.go`

- [ ] **Step 1: Replace the `runAdd` stub**

In `internal/tui/commands.go`, replace the `runAdd` method. Make sure imports include `"context"`, `"strings"`, `"github.com/germanamz/tusk/internal/domain"`:

```go
func (a *App) runAdd(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	parsed := parseArgs(args)

	if parsed.Title == "" {
		return fmt.Errorf("title is required")
	}

	// Tags not yet supported
	if len(parsed.Tags) > 0 || len(parsed.ExclTags) > 0 {
		return fmt.Errorf("tags not yet supported")
	}

	task := &domain.Task{
		Title: parsed.Title,
	}

	// Project
	if name, ok := parsed.Fields["project"]; ok {
		project, err := a.projectRepo.GetByName(ctx, name)
		if err != nil {
			return fmt.Errorf("project %q not found", name)
		}
		task.ProjectID = &project.ID
	}

	// Priority
	if s, ok := parsed.Fields["priority"]; ok {
		p, err := parsePriority(s)
		if err != nil {
			return err
		}
		task.Priority = p
	}

	// Status (rarely used, defaults to pending in service)
	if s, ok := parsed.Fields["status"]; ok {
		task.Status = s
	}

	// Due date
	if s, ok := parsed.Fields["due"]; ok {
		d, err := parseDate(s)
		if err != nil {
			return err
		}
		task.DueAt = &d
	}

	// Parent
	if shortID, ok := parsed.Fields["parent"]; ok {
		parent, err := a.taskSvc.GetByShortID(ctx, shortID)
		if err != nil {
			return fmt.Errorf("%s", formatError(err, shortID))
		}
		task.ParentID = &parent.ID
	}

	if err := a.taskSvc.Create(ctx, task); err != nil {
		return fmt.Errorf("%s", err)
	}

	return renderMutationResult(cmd.OutOrStdout(), "Created", task, a.format)
}
```

- [ ] **Step 2: Write integration tests**

Append to `internal/tui/commands_test.go`:

```go
func TestRunAdd_HappyPath(t *testing.T) {
	app, _ := testApp(t)

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"add", "My", "new", "task"})
	if err := app.root.Execute(); err != nil {
		t.Fatalf("add: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "Created task") {
		t.Fatalf("expected 'Created task' in output, got %q", out)
	}
}

func TestRunAdd_WithPriority(t *testing.T) {
	app, taskSvc := testApp(t)
	ctx := context.Background()

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"add", "Priority", "task", "priority:high"})
	if err := app.root.Execute(); err != nil {
		t.Fatalf("add: %v", err)
	}

	// Extract short ID from "Created task <id>\n"
	out := strings.TrimSpace(buf.String())
	parts := strings.Fields(out)
	shortID := parts[len(parts)-1]

	task, err := taskSvc.GetByShortID(ctx, shortID)
	if err != nil {
		t.Fatalf("GetByShortID: %v", err)
	}
	if task.Priority != 3 {
		t.Fatalf("expected priority 3, got %d", task.Priority)
	}
}

func TestRunAdd_WithDueDate(t *testing.T) {
	app, taskSvc := testApp(t)
	ctx := context.Background()

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"add", "Due", "task", "due:2026-04-10"})
	if err := app.root.Execute(); err != nil {
		t.Fatalf("add: %v", err)
	}

	out := strings.TrimSpace(buf.String())
	parts := strings.Fields(out)
	shortID := parts[len(parts)-1]

	task, err := taskSvc.GetByShortID(ctx, shortID)
	if err != nil {
		t.Fatalf("GetByShortID: %v", err)
	}
	if task.DueAt == nil {
		t.Fatal("expected DueAt to be set")
	}
	if task.DueAt.Format("2006-01-02") != "2026-04-10" {
		t.Fatalf("expected due 2026-04-10, got %s", task.DueAt.Format("2006-01-02"))
	}
}

func TestRunAdd_WithParent(t *testing.T) {
	app, taskSvc := testApp(t)
	ctx := context.Background()

	parent := &domain.Task{Title: "Parent"}
	if err := taskSvc.Create(ctx, parent); err != nil {
		t.Fatalf("Create parent: %v", err)
	}

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"add", "Child", "task", "parent:" + parent.ShortID})
	if err := app.root.Execute(); err != nil {
		t.Fatalf("add: %v", err)
	}

	out := strings.TrimSpace(buf.String())
	parts := strings.Fields(out)
	shortID := parts[len(parts)-1]

	child, err := taskSvc.GetByShortID(ctx, shortID)
	if err != nil {
		t.Fatalf("GetByShortID: %v", err)
	}
	if child.ParentID == nil || *child.ParentID != parent.ID {
		t.Fatal("expected child to reference parent")
	}
}

func TestRunAdd_TagsError(t *testing.T) {
	app, _ := testApp(t)

	app.root.SetArgs([]string{"add", "Tagged", "task", "+api"})
	err := app.root.Execute()
	if err == nil {
		t.Fatal("expected error for tags")
	}
	if !strings.Contains(err.Error(), "tags not yet supported") {
		t.Fatalf("expected 'tags not yet supported', got %q", err.Error())
	}
}

func TestRunAdd_NoTitle(t *testing.T) {
	app, _ := testApp(t)

	// Only key:value args, no title words
	app.root.SetArgs([]string{"add", "priority:3"})
	err := app.root.Execute()
	if err == nil {
		t.Fatal("expected error for missing title")
	}
}

func TestRunAdd_JSON(t *testing.T) {
	app, _ := testApp(t)

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"add", "JSON", "task", "--format", "json"})
	if err := app.root.Execute(); err != nil {
		t.Fatalf("add --format json: %v", err)
	}
	if !strings.Contains(buf.String(), `"short_id"`) {
		t.Fatalf("expected JSON output, got:\n%s", buf.String())
	}
}
```

- [ ] **Step 3: Run tests to verify they pass**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/tui/ -v -run TestRunAdd`
Expected: All 7 tests PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/tui/commands.go internal/tui/commands_test.go
git commit -m "feat(tui): implement add command with project, priority, due, parent support"
```

---

### Task 2: Implement `modify` command

**Files:**
- Modify: `internal/tui/commands.go`
- Modify: `internal/tui/commands_test.go`

- [ ] **Step 1: Replace the `runModify` stub**

In `internal/tui/commands.go`, replace the `runModify` method:

```go
func (a *App) runModify(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	shortID := args[0]

	// Tags not yet supported
	parsed := parseArgs(args[1:])
	if len(parsed.Tags) > 0 || len(parsed.ExclTags) > 0 {
		return fmt.Errorf("tags not yet supported")
	}

	// Auto-fetch current version
	current, err := a.taskSvc.GetByShortID(ctx, shortID)
	if err != nil {
		return fmt.Errorf("%s", formatError(err, shortID))
	}

	upd := domain.TaskUpdate{
		ShortID: shortID,
		Version: current.Version,
	}

	// Title
	if s, ok := parsed.Fields["title"]; ok {
		upd.Title = &s
	}

	// Priority
	if s, ok := parsed.Fields["priority"]; ok {
		p, err := parsePriority(s)
		if err != nil {
			return err
		}
		upd.Priority = &p
	}

	// Status
	if s, ok := parsed.Fields["status"]; ok {
		upd.Status = &s
	}

	// Due date
	if s, ok := parsed.Fields["due"]; ok {
		if s == "" {
			// Clear due date
			var nilTime *time.Time
			upd.DueAt = &nilTime
		} else {
			d, err := parseDate(s)
			if err != nil {
				return err
			}
			upd.DueAt = &(&d)
		}
	}

	// Project
	if name, ok := parsed.Fields["project"]; ok {
		project, err := a.projectRepo.GetByName(ctx, name)
		if err != nil {
			return fmt.Errorf("project %q not found", name)
		}
		upd.ProjectID = &(&project.ID)
	}

	// Parent
	if s, ok := parsed.Fields["parent"]; ok {
		if s == "" {
			// Clear parent
			var nilUUID *uuid.UUID
			upd.ParentID = &nilUUID
		} else {
			parent, err := a.taskSvc.GetByShortID(ctx, s)
			if err != nil {
				return fmt.Errorf("%s", formatError(err, s))
			}
			upd.ParentID = &(&parent.ID)
		}
	}

	updated, err := a.taskSvc.Update(ctx, upd)
	if err != nil {
		return fmt.Errorf("%s", formatError(err, shortID))
	}

	return renderMutationResult(cmd.OutOrStdout(), "Modified", updated, a.format)
}
```

Make sure the imports in `commands.go` include `"time"` and `"github.com/google/uuid"`.

Note: The double-pointer fields (`DueAt`, `ProjectID`, `ParentID`) use `&(&value)` pattern because `domain.TaskUpdate` uses `**Type`. You need to create a local variable first:

Actually, `&(&d)` won't compile. Fix the pattern by using a helper or local variable:

```go
	// Due date
	if s, ok := parsed.Fields["due"]; ok {
		if s == "" {
			var nilTime *time.Time
			upd.DueAt = &nilTime
		} else {
			d, err := parseDate(s)
			if err != nil {
				return err
			}
			dp := &d
			upd.DueAt = &dp
		}
	}

	// Project
	if name, ok := parsed.Fields["project"]; ok {
		project, err := a.projectRepo.GetByName(ctx, name)
		if err != nil {
			return fmt.Errorf("project %q not found", name)
		}
		pid := project.ID
		pp := &pid
		upd.ProjectID = &pp
	}

	// Parent
	if s, ok := parsed.Fields["parent"]; ok {
		if s == "" {
			var nilUUID *uuid.UUID
			upd.ParentID = &nilUUID
		} else {
			parent, err := a.taskSvc.GetByShortID(ctx, s)
			if err != nil {
				return fmt.Errorf("%s", formatError(err, s))
			}
			pid := parent.ID
			pp := &pid
			upd.ParentID = &pp
		}
	}
```

- [ ] **Step 2: Write integration tests**

Append to `internal/tui/commands_test.go`:

```go
func TestRunModify_Title(t *testing.T) {
	app, taskSvc := testApp(t)
	ctx := context.Background()

	task := &domain.Task{Title: "Original"}
	if err := taskSvc.Create(ctx, task); err != nil {
		t.Fatalf("Create: %v", err)
	}

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"modify", task.ShortID, "title:Updated"})
	if err := app.root.Execute(); err != nil {
		t.Fatalf("modify: %v", err)
	}

	got, _ := taskSvc.GetByShortID(ctx, task.ShortID)
	if got.Title != "Updated" {
		t.Fatalf("expected title 'Updated', got %q", got.Title)
	}
	if got.Version != 2 {
		t.Fatalf("expected version 2, got %d", got.Version)
	}
}

func TestRunModify_Priority(t *testing.T) {
	app, taskSvc := testApp(t)
	ctx := context.Background()

	task := &domain.Task{Title: "Modify priority"}
	if err := taskSvc.Create(ctx, task); err != nil {
		t.Fatalf("Create: %v", err)
	}

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"modify", task.ShortID, "priority:urgent"})
	if err := app.root.Execute(); err != nil {
		t.Fatalf("modify: %v", err)
	}

	got, _ := taskSvc.GetByShortID(ctx, task.ShortID)
	if got.Priority != 4 {
		t.Fatalf("expected priority 4, got %d", got.Priority)
	}
}

func TestRunModify_NotFound(t *testing.T) {
	app, _ := testApp(t)

	app.root.SetArgs([]string{"modify", "nonexist", "title:Nope"})
	err := app.root.Execute()
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected 'not found', got %q", err.Error())
	}
}

func TestRunModify_TagsError(t *testing.T) {
	app, taskSvc := testApp(t)
	ctx := context.Background()

	task := &domain.Task{Title: "Tag test"}
	taskSvc.Create(ctx, task)

	app.root.SetArgs([]string{"modify", task.ShortID, "+api"})
	err := app.root.Execute()
	if err == nil {
		t.Fatal("expected error for tags")
	}
	if !strings.Contains(err.Error(), "tags not yet supported") {
		t.Fatalf("expected 'tags not yet supported', got %q", err.Error())
	}
}
```

- [ ] **Step 3: Run tests to verify they pass**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/tui/ -v -run TestRunModify`
Expected: All 4 tests PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/tui/commands.go internal/tui/commands_test.go
git commit -m "feat(tui): implement modify command with auto-fetch version"
```

---

### Task 3: Implement `start`, `done`, `delete` commands

**Files:**
- Modify: `internal/tui/commands.go`
- Modify: `internal/tui/commands_test.go`

- [ ] **Step 1: Replace the three stubs**

In `internal/tui/commands.go`, replace `runStart`, `runDone`, and `runDelete`:

```go
func (a *App) runStart(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	shortID := args[0]

	current, err := a.taskSvc.GetByShortID(ctx, shortID)
	if err != nil {
		return fmt.Errorf("%s", formatError(err, shortID))
	}

	updated, err := a.taskSvc.Start(ctx, shortID, current.Version)
	if err != nil {
		return fmt.Errorf("%s", formatError(err, shortID))
	}

	return renderMutationResult(cmd.OutOrStdout(), "Started", updated, a.format)
}

func (a *App) runDone(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	shortID := args[0]

	current, err := a.taskSvc.GetByShortID(ctx, shortID)
	if err != nil {
		return fmt.Errorf("%s", formatError(err, shortID))
	}

	updated, err := a.taskSvc.Complete(ctx, shortID, current.Version)
	if err != nil {
		return fmt.Errorf("%s", formatError(err, shortID))
	}

	return renderMutationResult(cmd.OutOrStdout(), "Completed", updated, a.format)
}

func (a *App) runDelete(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	shortID := args[0]

	current, err := a.taskSvc.GetByShortID(ctx, shortID)
	if err != nil {
		return fmt.Errorf("%s", formatError(err, shortID))
	}

	updated, err := a.taskSvc.Delete(ctx, shortID, current.Version)
	if err != nil {
		return fmt.Errorf("%s", formatError(err, shortID))
	}

	return renderMutationResult(cmd.OutOrStdout(), "Deleted", updated, a.format)
}
```

- [ ] **Step 2: Write integration tests**

Append to `internal/tui/commands_test.go`:

```go
func TestRunStart_HappyPath(t *testing.T) {
	app, taskSvc := testApp(t)
	ctx := context.Background()

	task := &domain.Task{Title: "Start me"}
	taskSvc.Create(ctx, task)

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"start", task.ShortID})
	if err := app.root.Execute(); err != nil {
		t.Fatalf("start: %v", err)
	}

	out := strings.TrimSpace(buf.String())
	if out != "Started task "+task.ShortID {
		t.Fatalf("expected 'Started task %s', got %q", task.ShortID, out)
	}

	got, _ := taskSvc.GetByShortID(ctx, task.ShortID)
	if got.Status != "active" {
		t.Fatalf("expected active, got %q", got.Status)
	}
}

func TestRunStart_NotFound(t *testing.T) {
	app, _ := testApp(t)

	app.root.SetArgs([]string{"start", "nonexist"})
	err := app.root.Execute()
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected 'not found' error, got %v", err)
	}
}

func TestRunDone_HappyPath(t *testing.T) {
	app, taskSvc := testApp(t)
	ctx := context.Background()

	task := &domain.Task{Title: "Complete me"}
	taskSvc.Create(ctx, task)
	taskSvc.Start(ctx, task.ShortID, 1)

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"done", task.ShortID})
	if err := app.root.Execute(); err != nil {
		t.Fatalf("done: %v", err)
	}

	out := strings.TrimSpace(buf.String())
	if out != "Completed task "+task.ShortID {
		t.Fatalf("expected 'Completed task %s', got %q", task.ShortID, out)
	}

	got, _ := taskSvc.GetByShortID(ctx, task.ShortID)
	if got.Status != "completed" {
		t.Fatalf("expected completed, got %q", got.Status)
	}
}

func TestRunDone_FromPending(t *testing.T) {
	app, taskSvc := testApp(t)
	ctx := context.Background()

	task := &domain.Task{Title: "Skip start"}
	taskSvc.Create(ctx, task)

	app.root.SetArgs([]string{"done", task.ShortID})
	err := app.root.Execute()
	if err == nil {
		t.Fatal("expected error for invalid transition")
	}
}

func TestRunDelete_HappyPath(t *testing.T) {
	app, taskSvc := testApp(t)
	ctx := context.Background()

	task := &domain.Task{Title: "Delete me"}
	taskSvc.Create(ctx, task)

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"delete", task.ShortID})
	if err := app.root.Execute(); err != nil {
		t.Fatalf("delete: %v", err)
	}

	out := strings.TrimSpace(buf.String())
	if out != "Deleted task "+task.ShortID {
		t.Fatalf("expected 'Deleted task %s', got %q", task.ShortID, out)
	}

	got, _ := taskSvc.GetByShortID(ctx, task.ShortID)
	if got.Status != "deleted" {
		t.Fatalf("expected deleted, got %q", got.Status)
	}
}
```

- [ ] **Step 3: Run tests to verify they pass**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/tui/ -v -run "TestRunStart|TestRunDone|TestRunDelete"`
Expected: All 5 tests PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/tui/commands.go internal/tui/commands_test.go
git commit -m "feat(tui): implement start, done, delete commands with auto-fetch version"
```

---

### Task 4: Implement `annotate` command

**Files:**
- Modify: `internal/tui/commands.go`
- Modify: `internal/tui/commands_test.go`

- [ ] **Step 1: Replace the `runAnnotate` stub**

In `internal/tui/commands.go`, replace the `runAnnotate` method:

```go
func (a *App) runAnnotate(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	shortID := args[0]
	body := strings.Join(args[1:], " ")

	ann, err := a.taskSvc.Annotate(ctx, shortID, body)
	if err != nil {
		return fmt.Errorf("%s", formatError(err, shortID))
	}

	if a.format == "json" {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(map[string]string{
			"id":         ann.ID.String(),
			"task_id":    ann.TaskID.String(),
			"body":       ann.Body,
			"created_at": ann.CreatedAt.Format(time.RFC3339),
		})
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Annotated task %s\n", shortID)
	return nil
}
```

Make sure the imports in `commands.go` include `"encoding/json"`.

- [ ] **Step 2: Write integration tests**

Append to `internal/tui/commands_test.go`:

```go
func TestRunAnnotate_HappyPath(t *testing.T) {
	app, taskSvc := testApp(t)
	ctx := context.Background()

	task := &domain.Task{Title: "Annotate me"}
	taskSvc.Create(ctx, task)

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"annotate", task.ShortID, "This", "is", "a", "note"})
	if err := app.root.Execute(); err != nil {
		t.Fatalf("annotate: %v", err)
	}

	out := strings.TrimSpace(buf.String())
	if out != "Annotated task "+task.ShortID {
		t.Fatalf("expected 'Annotated task %s', got %q", task.ShortID, out)
	}

	annotations, _ := taskSvc.GetAnnotations(ctx, task.ShortID)
	if len(annotations) != 1 {
		t.Fatalf("expected 1 annotation, got %d", len(annotations))
	}
	if annotations[0].Body != "This is a note" {
		t.Fatalf("expected 'This is a note', got %q", annotations[0].Body)
	}
}

func TestRunAnnotate_NotFound(t *testing.T) {
	app, _ := testApp(t)

	app.root.SetArgs([]string{"annotate", "nonexist", "A", "note"})
	err := app.root.Execute()
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected 'not found' error, got %v", err)
	}
}

func TestRunAnnotate_JSON(t *testing.T) {
	app, taskSvc := testApp(t)
	ctx := context.Background()

	task := &domain.Task{Title: "JSON annotate"}
	taskSvc.Create(ctx, task)

	var buf bytes.Buffer
	app.root.SetOut(&buf)
	app.root.SetArgs([]string{"annotate", task.ShortID, "A", "note", "--format", "json"})
	if err := app.root.Execute(); err != nil {
		t.Fatalf("annotate --format json: %v", err)
	}
	if !strings.Contains(buf.String(), `"body"`) {
		t.Fatalf("expected JSON output, got:\n%s", buf.String())
	}
}
```

- [ ] **Step 3: Run tests to verify they pass**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/tui/ -v -run TestRunAnnotate`
Expected: All 3 tests PASS.

- [ ] **Step 4: Run the full test suite**

Run: `cd /Users/germanamz/projects/tusk && go test ./... -v`
Expected: All tests across all packages PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/commands.go internal/tui/commands_test.go
git commit -m "feat(tui): implement annotate command"
```

---

### Task 5: End-to-end smoke test with the compiled binary

**Files:** None (manual verification)

- [ ] **Step 1: Build the binary**

Run: `cd /Users/germanamz/projects/tusk && go build -o /tmp/tusk ./cmd/tusk/`
Expected: Compiles without errors.

- [ ] **Step 2: Run a full workflow**

Run these commands in sequence:

```bash
export TUSK_DB=/tmp/tusk-e2e.db

# Create tasks
/tmp/tusk add "Implement auth middleware" priority:high
/tmp/tusk add "Write tests for auth" priority:medium

# List tasks
/tmp/tusk list

# Get info on first task (use the short ID from the add output)
# /tmp/tusk info <short_id>

# Start a task
# /tmp/tusk start <short_id>

# Complete a task
# /tmp/tusk done <short_id>

# Annotate
# /tmp/tusk annotate <short_id> "Blocked by upstream API"

# JSON output
/tmp/tusk list --format json
```

Expected: Each command produces the expected output as described in the design spec.

- [ ] **Step 3: Clean up**

Run: `rm -f /tmp/tusk /tmp/tusk-e2e.db /tmp/tusk-e2e.db-wal /tmp/tusk-e2e.db-shm`

- [ ] **Step 4: Remove empty stub files that were replaced**

Verify that `internal/tui/render.go`, `internal/tui/filter.go`, and `internal/tui/tree.go` exist. If `tree.go` is still an empty stub (`package tui` only), leave it — it's for v0.2.

- [ ] **Step 5: Final commit if any cleanup was needed**

Only commit if changes were made in step 4. Otherwise skip.
