# E2E Tests Phase 2: Core Scenarios — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add the core e2e scenario files — full task lifecycle workflows and error handling — that cover the most critical regression paths.

**Architecture:** Two test files in `tests/e2e/` that use the harness from Phase 1. Each file defines `[]Scenario` and calls `runScenarios()`. Every scenario runs 4x (flag/env x text/json).

**Tech Stack:** Go standard library, harness from Phase 1 (`tests/e2e/harness.go`).

**Prerequisites:** Phase 1 must be complete. The following files must exist and work:
- `tests/e2e/harness.go` — provides `Env`, `Step`, `Scenario`, `runScenarios`, assertion helpers
- `tests/e2e/main_test.go` — provides `binPath` (compiled tusk binary)

**Key reference for assertions:**
- **Text mutation output** format is: `"Created task XXXXXXXX\n"`, `"Started task XXXXXXXX\n"`, etc. (see `internal/tui/render.go:283-293`)
- **JSON mutation output** is a full task object with fields: `id`, `short_id`, `parent_id`, `project_id`, `title`, `description`, `status`, `priority`, `version`, `tags`, `due_at`, `wait_until`, `recurrence_rule`, `uda`, `created_at`, `modified_at` (see `internal/tui/render.go:49-67`)
- **Text list output** header: `"ID       Status    Pri  Age   Title\n"` (see `internal/tui/render.go:124`)
- **JSON list output** is a JSON array of task objects (see `internal/tui/render.go:110-118`)
- **Text info output** is key-value lines like `"ID:           XXXXXXXX\n"` (see `internal/tui/render.go:198-265`)
- **JSON info output** is a task object with an additional `annotations` array (see `internal/tui/render.go:174-177`)
- **Error output**: errors go to stderr via Cobra's `SilenceErrors: true` + `main.go`'s `fmt.Fprintf(os.Stderr, "Error: %s\n", err)`. The process exits with code 1.

---

### Task 1: `task_lifecycle_test.go` — Full Lifecycle Scenarios

**Files:**
- Modify: `tests/e2e/task_lifecycle_test.go` (created in Phase 1 with smoke scenarios)

Replace the file content with the full set of lifecycle scenarios. The smoke scenarios from Phase 1 are included and expanded.

- [ ] **Step 1: Write the complete test file**

Replace the entire contents of `tests/e2e/task_lifecycle_test.go` with:

```go
package e2e

import "testing"

func TestTaskLifecycle(t *testing.T) {
	scenarios := []Scenario{
		{
			Name: "create_single_task",
			Steps: []Step{
				{
					Args: []string{"add", "Buy milk", "priority:3"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						assertEqual(t, m["title"], "Buy milk")
						assertEqual(t, m["status"], "pending")
						assertEqual(t, m["priority"], float64(3))
						if m["short_id"] == nil || m["short_id"] == "" {
							t.Fatal("expected short_id to be set")
						}
						if m["version"] != float64(1) {
							t.Fatalf("expected version 1, got %v", m["version"])
						}
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Created task")
					},
				},
			},
		},
		{
			Name: "create_start_done",
			Steps: []Step{
				{
					Args: []string{"add", "Full lifecycle task"},
				},
				{
					Args: []string{"start", "$0.short_id"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						assertEqual(t, m["status"], "active")
						assertEqual(t, m["version"], float64(2))
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Started task")
					},
				},
				{
					Args: []string{"done", "$0.short_id"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						assertEqual(t, m["status"], "completed")
						assertEqual(t, m["version"], float64(3))
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Completed task")
					},
				},
			},
		},
		{
			Name: "create_delete",
			Steps: []Step{
				{
					Args: []string{"add", "Delete me"},
				},
				{
					Args: []string{"delete", "$0.short_id"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						assertEqual(t, m["status"], "deleted")
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Deleted task")
					},
				},
			},
		},
		{
			Name: "create_start_back_to_pending",
			Steps: []Step{
				{
					Args: []string{"add", "Back and forth"},
				},
				{
					Args: []string{"start", "$0.short_id"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						assertEqual(t, parsed.(map[string]any)["status"], "active")
					},
				},
				{
					Args: []string{"modify", "$0.short_id", "status:pending"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						assertEqual(t, m["status"], "pending")
						assertEqual(t, m["version"], float64(3))
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Modified task")
					},
				},
			},
		},
		{
			Name: "completed_reopen",
			Steps: []Step{
				{
					Args: []string{"add", "Reopen me"},
				},
				{
					Args: []string{"start", "$0.short_id"},
				},
				{
					Args: []string{"done", "$0.short_id"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						assertEqual(t, parsed.(map[string]any)["status"], "completed")
					},
				},
				{
					Args: []string{"modify", "$0.short_id", "status:pending"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						assertEqual(t, m["status"], "pending")
						assertEqual(t, m["version"], float64(4))
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Modified task")
					},
				},
			},
		},
		{
			Name: "create_with_project",
			Steps: []Step{
				{
					// _default project is seeded by the migration
					Args: []string{"add", "Project task", "project:_default"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						assertEqual(t, m["title"], "Project task")
						if m["project_id"] == nil {
							t.Fatal("expected project_id to be set")
						}
					},
				},
			},
		},
		{
			Name: "create_multiple_list_shows_all",
			Steps: []Step{
				{
					Args: []string{"add", "Task one"},
				},
				{
					Args: []string{"add", "Task two"},
				},
				{
					Args: []string{"add", "Task three"},
				},
				{
					Args: []string{"list"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						arr := jsonArray(t, parsed)
						if len(arr) != 3 {
							t.Fatalf("expected 3 tasks, got %d", len(arr))
						}
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Task one")
						assertContains(t, output, "Task two")
						assertContains(t, output, "Task three")
					},
				},
			},
		},
		{
			Name: "info_shows_task_details",
			Steps: []Step{
				{
					Args: []string{"add", "Detail task", "priority:2"},
				},
				{
					Args: []string{"info", "$0.short_id"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						assertEqual(t, m["title"], "Detail task")
						assertEqual(t, m["status"], "pending")
						assertEqual(t, m["priority"], float64(2))
						assertEqual(t, m["version"], float64(1))
						if m["created_at"] == nil {
							t.Fatal("expected created_at")
						}
						if m["modified_at"] == nil {
							t.Fatal("expected modified_at")
						}
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Detail task")
						assertContains(t, output, "pending")
						assertContains(t, output, "medium")
					},
				},
			},
		},
		{
			Name: "modify_title_and_priority",
			Steps: []Step{
				{
					Args: []string{"add", "Original title", "priority:1"},
				},
				{
					Args: []string{"modify", "$0.short_id", "New title", "priority:4"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						assertEqual(t, m["title"], "New title")
						assertEqual(t, m["priority"], float64(4))
						assertEqual(t, m["version"], float64(2))
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Modified task")
					},
				},
				{
					// Verify via info that the changes persisted
					Args: []string{"info", "$0.short_id"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						assertEqual(t, m["title"], "New title")
						assertEqual(t, m["priority"], float64(4))
					},
				},
			},
		},
	}

	runScenarios(t, binPath, scenarios)
}
```

- [ ] **Step 2: Run the tests**

Run:
```bash
cd /Users/germanamz/projects/tusk && CGO_ENABLED=1 go test ./tests/e2e/... -v -count=1 -run TestTaskLifecycle
```

Expected: All subtests pass. You should see 9 scenarios x 4 combinations = 36 subtests.

- [ ] **Step 3: Commit**

```bash
git add tests/e2e/task_lifecycle_test.go
git commit -m "test(e2e): add full task lifecycle scenarios (create, start, done, delete, modify, reopen)"
```

---

### Task 2: `error_handling_test.go` — Error Scenarios

**Files:**
- Create: `tests/e2e/error_handling_test.go`

Tests that the CLI returns proper errors (non-zero exit, meaningful stderr) for invalid operations.

**Important:** When a command errors, the tusk binary writes `"Error: <message>\n"` to stderr and exits with code 1 (see `cmd/tusk/main.go:17-19`). However, Cobra subcommand errors (from `RunE`) are handled differently: `SilenceErrors: true` means Cobra doesn't print the error itself — instead the error propagates up to `app.Run()` which returns it to `main.go`'s `run()`, which prints `"Error: <message>\n"` to stderr. So all errors end up on stderr with the `"Error: "` prefix.

- [ ] **Step 1: Write the complete test file**

```go
package e2e

import "testing"

func TestErrorHandling(t *testing.T) {
	scenarios := []Scenario{
		{
			Name: "info_not_found",
			Steps: []Step{
				{
					Args:    []string{"info", "nonexist"},
					WantErr: true,
					Assert: func(t *testing.T, r Result) {
						t.Helper()
						assertStderrContains(t, r, "not found")
					},
				},
			},
		},
		{
			Name: "modify_not_found",
			Steps: []Step{
				{
					Args:    []string{"modify", "nonexist", "New title"},
					WantErr: true,
					Assert: func(t *testing.T, r Result) {
						t.Helper()
						assertStderrContains(t, r, "not found")
					},
				},
			},
		},
		{
			Name: "start_not_found",
			Steps: []Step{
				{
					Args:    []string{"start", "nonexist"},
					WantErr: true,
					Assert: func(t *testing.T, r Result) {
						t.Helper()
						assertStderrContains(t, r, "not found")
					},
				},
			},
		},
		{
			Name: "done_not_found",
			Steps: []Step{
				{
					Args:    []string{"done", "nonexist"},
					WantErr: true,
					Assert: func(t *testing.T, r Result) {
						t.Helper()
						assertStderrContains(t, r, "not found")
					},
				},
			},
		},
		{
			Name: "delete_not_found",
			Steps: []Step{
				{
					Args:    []string{"delete", "nonexist"},
					WantErr: true,
					Assert: func(t *testing.T, r Result) {
						t.Helper()
						assertStderrContains(t, r, "not found")
					},
				},
			},
		},
		{
			Name: "done_from_pending_invalid_transition",
			Steps: []Step{
				{
					Args: []string{"add", "Cannot skip to done"},
				},
				{
					// pending -> completed is not an allowed transition
					Args:    []string{"done", "$0.short_id"},
					WantErr: true,
					Assert: func(t *testing.T, r Result) {
						t.Helper()
						assertStderrContains(t, r, "not allowed")
					},
				},
			},
		},
		{
			Name: "add_no_args",
			Steps: []Step{
				{
					Args:    []string{"add"},
					WantErr: true,
					Assert: func(t *testing.T, r Result) {
						t.Helper()
						// Cobra enforces MinimumNArgs(1) — error goes to stderr
						combined := r.Stderr + r.Stdout
						if combined == "" {
							t.Fatal("expected some error output")
						}
					},
				},
			},
		},
		{
			Name: "add_no_title_only_filters",
			Steps: []Step{
				{
					// Only key:value args, no free text for title
					Args:    []string{"add", "priority:3"},
					WantErr: true,
					Assert: func(t *testing.T, r Result) {
						t.Helper()
						assertStderrContains(t, r, "title is required")
					},
				},
			},
		},
		{
			Name: "annotate_not_found",
			Steps: []Step{
				{
					Args:    []string{"annotate", "nonexist", "A note"},
					WantErr: true,
					Assert: func(t *testing.T, r Result) {
						t.Helper()
						assertStderrContains(t, r, "not found")
					},
				},
			},
		},
		{
			Name: "start_already_active",
			Steps: []Step{
				{
					Args: []string{"add", "Already active"},
				},
				{
					Args: []string{"start", "$0.short_id"},
				},
				{
					// Starting an already-active task: active -> active is not a valid transition
					Args:    []string{"start", "$0.short_id"},
					WantErr: true,
					Assert: func(t *testing.T, r Result) {
						t.Helper()
						assertStderrContains(t, r, "not allowed")
					},
				},
			},
		},
		{
			Name: "add_invalid_project",
			Steps: []Step{
				{
					Args:    []string{"add", "Bad project", "project:nonexistent_project"},
					WantErr: true,
					Assert: func(t *testing.T, r Result) {
						t.Helper()
						assertStderrContains(t, r, "not found")
					},
				},
			},
		},
	}

	runScenarios(t, binPath, scenarios)
}
```

- [ ] **Step 2: Run the tests**

Run:
```bash
cd /Users/germanamz/projects/tusk && CGO_ENABLED=1 go test ./tests/e2e/... -v -count=1 -run TestErrorHandling
```

Expected: All subtests pass. 11 scenarios x 4 combinations = 44 subtests.

- [ ] **Step 3: Commit**

```bash
git add tests/e2e/error_handling_test.go
git commit -m "test(e2e): add error handling scenarios (not found, invalid transitions, bad args)"
```
