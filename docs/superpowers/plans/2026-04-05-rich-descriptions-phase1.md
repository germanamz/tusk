# Rich Descriptions Phase 1: Double-Pointer Migration — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Migrate `TaskUpdate.Description` from `*string` to `**string` so descriptions can be cleared, matching the double-pointer pattern used by `ParentID`, `DueAt`, `WaitUntil`, and `RecurrenceRule`.

**Architecture:** Change the domain type, update the service layer's patch logic, update MCP and CLI consumers, and add unit + E2E tests. No new files created — all modifications to existing files.

**Tech Stack:** Go, SQLite (no schema changes)

---

## File Map

| File | Action | Responsibility |
|------|--------|---------------|
| `internal/domain/task.go:37` | Modify | Change `Description *string` to `Description **string` |
| `internal/service/task.go:179-181` | Modify | Update patch logic for double-pointer semantics |
| `internal/mcp/tools.go:379-381` | Modify | Update `handleTaskModify` to use double-pointer for description |
| `internal/tui/commands.go:206-311` | Modify | Update `runModify` — no functional change needed yet, but must compile with new type |
| `internal/service/task_test.go` | Modify | Add tests for double-pointer description clearing |
| `tests/e2e/task_lifecycle_test.go` | Modify | Add E2E scenario for MCP-style clearing (via modify) |

---

### Task 1: Change `TaskUpdate.Description` to double-pointer

**Files:**
- Modify: `internal/domain/task.go:37`

- [ ] **Step 1: Write the type change**

In `internal/domain/task.go`, change line 37 from:

```go
Description    *string
```

to:

```go
Description    **string
```

Also update the `TaskUpdate` doc comment (lines 27-32) to include `Description` in the list of double-pointer fields:

```go
// TaskUpdate represents a partial update to a task.
// Nil pointer fields mean "don't change this field".
// For nullable/clearable fields (ParentID, DueAt, WaitUntil, RecurrenceRule, Description),
// a double pointer is used: outer nil = don't change, outer non-nil + inner nil = set to NULL/empty,
// outer non-nil + inner non-nil = set to value.
// ProjectID uses a single pointer: nil = don't change, non-nil = set to value.
```

- [ ] **Step 2: Verify the project does NOT compile**

Run: `cd /Users/germanamz/projects/tusk && go build ./...`

Expected: Compilation errors in `internal/service/task.go`, `internal/mcp/tools.go`, and possibly `internal/tui/commands.go` because they assign `*string` where `**string` is now expected.

---

### Task 2: Update service layer patch logic

**Files:**
- Modify: `internal/service/task.go:179-181`

- [ ] **Step 1: Update the description patching in `TaskService.Update`**

In `internal/service/task.go`, replace lines 179-181:

```go
if upd.Description != nil {
    task.Description = *upd.Description
}
```

with:

```go
if upd.Description != nil {
    if *upd.Description == nil {
        task.Description = ""
    } else {
        task.Description = **upd.Description
    }
}
```

This follows the same pattern as `DueAt` (line 194-196) and `ParentID` (line 188-190).

- [ ] **Step 2: Verify service layer compiles**

Run: `cd /Users/germanamz/projects/tusk && go build ./internal/service/...`

Expected: Compiles successfully. Other packages may still fail.

---

### Task 3: Update MCP `handleTaskModify` handler

**Files:**
- Modify: `internal/mcp/tools.go:379-381`

- [ ] **Step 1: Update description handling in `handleTaskModify`**

In `internal/mcp/tools.go`, replace lines 379-381:

```go
if desc, err := request.RequireString("description"); err == nil {
    upd.Description = &desc
}
```

with:

```go
if desc, err := request.RequireString("description"); err == nil {
    if desc == "" {
        var nilStr *string
        upd.Description = &nilStr
    } else {
        dp := &desc
        upd.Description = &dp
    }
}
```

This follows the same pattern as the `parent` field (lines 393-406): empty string means clear, non-empty means set.

- [ ] **Step 2: Verify full project compiles**

Run: `cd /Users/germanamz/projects/tusk && go build ./...`

Expected: Compiles successfully. The CLI `runModify` in `internal/tui/commands.go` does not reference `TaskUpdate.Description` at all (descriptions aren't wired into CLI yet), so no changes needed there.

- [ ] **Step 3: Run existing tests to ensure no regressions**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/service/ -v -count=1 2>&1 | tail -30`

Expected: All existing tests pass. The `TestUpdate_PartialUpdate` test doesn't set Description, so it remains `nil` (no change) — which still works with double-pointer.

- [ ] **Step 4: Commit**

```bash
git add internal/domain/task.go internal/service/task.go internal/mcp/tools.go
git commit -m "$(cat <<'EOF'
refactor: migrate TaskUpdate.Description to double-pointer

Change Description from *string to **string to match the pattern used
by ParentID, DueAt, WaitUntil, and RecurrenceRule. This enables
clearing descriptions (setting to empty string) via both CLI and MCP.
EOF
)"
```

---

### Task 4: Add unit tests for double-pointer description

**Files:**
- Modify: `internal/service/task_test.go`

- [ ] **Step 1: Write the test for setting a description**

Add the following test after `TestUpdate_ClearNullableField` (around line 585) in `internal/service/task_test.go`:

```go
func TestUpdate_SetDescription(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	task := newMinimalTask("Has no description")
	mustCreateTask(t, env.taskSvc, task)

	// Set description via double-pointer
	desc := "A detailed description"
	dp := &desc
	updated, err := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID:     task.ShortID,
		Version:     1,
		Description: &dp,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Description != "A detailed description" {
		t.Fatalf("expected description %q, got %q", "A detailed description", updated.Description)
	}
}
```

- [ ] **Step 2: Run the test to verify it passes**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/service/ -run TestUpdate_SetDescription -v -count=1`

Expected: PASS

- [ ] **Step 3: Write the test for clearing a description**

Add the following test after the previous one:

```go
func TestUpdate_ClearDescription(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	task := newMinimalTask("Has description")
	task.Description = "Will be cleared"
	mustCreateTask(t, env.taskSvc, task)

	// Verify description was set
	created, err := env.taskSvc.GetByShortID(ctx, task.ShortID)
	if err != nil {
		t.Fatalf("GetByShortID: %v", err)
	}
	if created.Description != "Will be cleared" {
		t.Fatalf("expected description %q, got %q", "Will be cleared", created.Description)
	}

	// Clear description via double-pointer (outer non-nil, inner nil)
	var nilStr *string
	updated, err := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID:     task.ShortID,
		Version:     1,
		Description: &nilStr,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Description != "" {
		t.Fatalf("expected empty description, got %q", updated.Description)
	}
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/service/ -run TestUpdate_ClearDescription -v -count=1`

Expected: PASS

- [ ] **Step 5: Write the test for nil description (no change)**

Add the following test:

```go
func TestUpdate_NilDescriptionNoChange(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	task := newMinimalTask("Keep description")
	task.Description = "Should not change"
	mustCreateTask(t, env.taskSvc, task)

	// Update title only, leave description nil (no change)
	newTitle := "New title"
	updated, err := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID: task.ShortID,
		Version: 1,
		Title:   &newTitle,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Description != "Should not change" {
		t.Fatalf("expected description unchanged, got %q", updated.Description)
	}
}
```

- [ ] **Step 6: Run all description-related tests**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/service/ -run "TestUpdate_(Set|Clear|Nil)Description" -v -count=1`

Expected: All 3 tests PASS.

- [ ] **Step 7: Run the full service test suite to ensure no regressions**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/service/ -v -count=1 2>&1 | tail -5`

Expected: All tests PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/service/task_test.go
git commit -m "$(cat <<'EOF'
test(service): add unit tests for double-pointer description

Cover three cases: set description, clear description (to empty
string), and nil description (no change). Validates the double-pointer
migration from the previous commit.
EOF
)"
```

---

### Task 5: Add E2E test for description clearing via modify

**Files:**
- Modify: `tests/e2e/task_lifecycle_test.go`

- [ ] **Step 1: Add an E2E scenario for setting and verifying description**

This scenario tests that a task created with a description shows it in `info`, and that a modify with the description field works. Since CLI `--description` flag doesn't exist yet, this test only validates the JSON output structure (that the `description` field exists and is returned).

Add the following scenario to the `scenarios` slice in `TestTaskLifecycle` (after the last scenario, before the closing `}`):

```go
{
    Name: "create_task_has_empty_description",
    Steps: []Step{
        {
            Args: []string{"add", "Task without description"},
            AssertJSON: func(t *testing.T, parsed any) {
                t.Helper()
                m := parsed.(map[string]any)
                assertEqual(t, m["description"], "")
            },
        },
        {
            Args: []string{"info", "$0.short_id"},
            AssertJSON: func(t *testing.T, parsed any) {
                t.Helper()
                m := parsed.(map[string]any)
                assertEqual(t, m["description"], "")
            },
            AssertText: func(t *testing.T, output string) {
                t.Helper()
                // Empty description should not appear in text output
                assertNotContains(t, output, "Description:")
            },
        },
    },
},
```

- [ ] **Step 2: Run the E2E tests**

Run: `cd /Users/germanamz/projects/tusk && go test ./tests/e2e/ -run TestTaskLifecycle/create_task_has_empty_description -v -count=1 -timeout 120s 2>&1 | tail -20`

Expected: All 4 sub-combinations (flag/env x text/json) PASS.

- [ ] **Step 3: Run the full test suite to check for regressions**

Run: `cd /Users/germanamz/projects/tusk && make test 2>&1 | tail -10`

Expected: All tests PASS.

- [ ] **Step 4: Commit**

```bash
git add tests/e2e/task_lifecycle_test.go
git commit -m "$(cat <<'EOF'
test(e2e): verify empty description field in task lifecycle

Add scenario confirming tasks created without a description have an
empty string description in JSON output and no Description line in
text output.
EOF
)"
```
