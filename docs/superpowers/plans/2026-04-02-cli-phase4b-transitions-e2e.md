# CLI Phase 4b: Transition Commands & E2E Smoke Test

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement `start`, `done`, `delete` (which all share the same auto-fetch-version + transition pattern) and run a full end-to-end smoke test with the compiled binary.

**Architecture:** The three transition commands are nearly identical: fetch current version, call the corresponding service method, render result. The e2e test verifies the full binary workflow.

**Tech Stack:** Existing `internal/tui` functions, `internal/service.TaskService`.

**Depends on:** All previous phases (1a, 1b, 2, 3, 4a).

---

### Task 1: Implement `start`, `done`, `delete` commands

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

- [ ] **Step 4: Run the full test suite**

Run: `cd /Users/germanamz/projects/tusk && go test ./... -v`
Expected: All tests across all packages PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/commands.go internal/tui/commands_test.go
git commit -m "feat(tui): implement start, done, delete commands with auto-fetch version"
```

---

### Task 2: End-to-end smoke test with the compiled binary

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

- [ ] **Step 4: Verify no empty stub files remain**

Check that `internal/tui/render.go`, `internal/tui/filter.go`, and `internal/tui/tree.go` exist. If `tree.go` is still an empty stub (`package tui` only), leave it — it's for v0.2.

---

### Task 3: Remove old stub files if needed

**Files:**
- Check: `internal/tui/tree.go`

- [ ] **Step 1: Verify tree.go is still a valid empty stub**

Read `internal/tui/tree.go`. If it contains only `package tui`, leave it alone. If it was accidentally modified during this work, restore it to just `package tui`.

- [ ] **Step 2: Final commit if any cleanup was needed**

Only commit if changes were made. Otherwise skip.

```bash
# Only if changes exist:
git add internal/tui/
git commit -m "chore: clean up tui package stubs"
```
