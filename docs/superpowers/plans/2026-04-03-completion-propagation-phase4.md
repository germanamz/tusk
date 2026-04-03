# Completion Propagation — Phase 4: E2E Tests

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add black-box E2E tests that exercise completion propagation through the real CLI binary, covering the full stack from CLI command through service through database.

**Architecture:** E2E tests use the existing test harness (`tests/e2e/harness.go`). Since propagation requires project settings to be enabled, and there is no `tusk project modify` command yet, we need a way to set project settings in E2E tests. We'll add a small helper to the E2E harness that writes settings directly to the SQLite database before running CLI commands.

**Tech Stack:** Go, E2E test harness, SQLite

**Spec:** `docs/superpowers/specs/2026-04-03-completion-propagation-design.md`

**Prerequisite:** Phase 3 must be completed (auto-complete + auto-revert logic fully working).

---

### Task 1: E2E Harness Helper for Project Settings

**Files:**
- Modify: `tests/e2e/harness.go` (add `SetProjectSettings` helper)

- [ ] **Step 1: Add the helper method to Env**

Add the following to `tests/e2e/harness.go`. This method directly writes to the SQLite database to set project settings on the `_default` project. Add `"database/sql"` and `"encoding/json"` to the imports.

```go
// SetDefaultProjectSettings updates the _default project's settings JSON
// directly in the database. This is needed because there is no CLI command
// to modify project settings yet.
func (e *Env) SetDefaultProjectSettings(settingsJSON string) {
	e.t.Helper()
	db, err := sql.Open("sqlite", e.dbPath+"?_pragma=foreign_keys(1)")
	if err != nil {
		e.t.Fatalf("opening db for settings: %v", err)
	}
	defer db.Close()

	defaultProjectID := "00000000-0000-0000-0000-000000000000"
	_, err = db.Exec(`UPDATE projects SET settings = ? WHERE id = ?`, settingsJSON, defaultProjectID)
	if err != nil {
		e.t.Fatalf("setting project settings: %v", err)
	}
}
```

Also add the SQLite driver import. The E2E tests already compile with the tusk binary, but the harness itself runs in the test process. Add this import alongside the existing ones:

```go
import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)
```

Check if `"encoding/json"` is already imported (it is, for `json.Unmarshal` in `expandRefs`). If so, don't add it again. Just add `"database/sql"` and the `_ "modernc.org/sqlite"` import.

- [ ] **Step 2: Verify the harness still compiles**

Run: `go test -v ./tests/e2e -run TestDoesNotExist -count=1`
Expected: Compiles and runs (no test matches, exits cleanly). This verifies the harness changes don't break compilation.

- [ ] **Step 3: Commit**

```bash
git add tests/e2e/harness.go
git commit -m "test(e2e): add SetDefaultProjectSettings helper to harness"
```

---

### Task 2: E2E Auto-Complete Tests

**Files:**
- Create: `tests/e2e/propagation_test.go`

- [ ] **Step 1: Write E2E propagation test scenarios**

Create `tests/e2e/propagation_test.go`:

```go
package e2e

import (
	"testing"
)

// autoCompleteSettings is the JSON to enable auto-complete on the default project.
const autoCompleteSettings = `{"auto_complete_parent":{"trigger_status":"completed","target_status":"completed"}}`

// bothPropagationSettings enables both auto-complete and auto-revert.
const bothPropagationSettings = `{"auto_complete_parent":{"trigger_status":"completed","target_status":"completed"},"auto_revert_parent":{"trigger_status":"completed","target_status":"active"}}`

func TestPropagation_Disabled(t *testing.T) {
	// Propagation is disabled by default — completing all children should NOT
	// auto-complete the parent.
	scenarios := []Scenario{
		{
			Name: "propagation_disabled_by_default",
			Steps: []Step{
				// Step 0: Create parent
				{Args: []string{"add", "Parent task"}},
				// Step 1: Start parent
				{Args: []string{"start", "$0.short_id"}},
				// Step 2: Create child
				{Args: []string{"add", "Child task", "parent:$0.short_id"}},
				// Step 3: Start child
				{Args: []string{"start", "$2.short_id"}},
				// Step 4: Complete child
				{Args: []string{"done", "$2.short_id"}},
				// Step 5: Check parent — should still be active
				{
					Args: []string{"info", "$0.short_id"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						if m["status"] != "active" {
							t.Fatalf("expected parent status 'active', got %v", m["status"])
						}
					},
				},
			},
		},
	}
	runScenarios(t, binPath, scenarios)
}

func TestPropagation_AutoComplete(t *testing.T) {
	scenarios := []Scenario{
		{
			Name: "auto_complete_all_children_done",
			Steps: []Step{
				// Step 0: Create parent
				{Args: []string{"add", "Parent task"}},
				// Step 1: Start parent
				{Args: []string{"start", "$0.short_id"}},
				// Step 2: Create child 1
				{Args: []string{"add", "Child one", "parent:$0.short_id"}},
				// Step 3: Create child 2
				{Args: []string{"add", "Child two", "parent:$0.short_id"}},
				// Step 4: Start child 1
				{Args: []string{"start", "$2.short_id"}},
				// Step 5: Complete child 1
				{Args: []string{"done", "$2.short_id"}},
				// Step 6: Check parent — should still be active (child 2 not done)
				{
					Args: []string{"info", "$0.short_id"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						if m["status"] != "active" {
							t.Fatalf("expected parent still 'active', got %v", m["status"])
						}
					},
				},
				// Step 7: Start child 2
				{Args: []string{"start", "$3.short_id"}},
				// Step 8: Complete child 2
				{Args: []string{"done", "$3.short_id"}},
				// Step 9: Check parent — should be auto-completed
				{
					Args: []string{"info", "$0.short_id"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						if m["status"] != "completed" {
							t.Fatalf("expected parent 'completed' (auto-complete), got %v", m["status"])
						}
					},
				},
			},
		},
		{
			Name: "auto_complete_deleted_child_ignored",
			Steps: []Step{
				// Step 0: Create parent
				{Args: []string{"add", "Parent"}},
				// Step 1: Start parent
				{Args: []string{"start", "$0.short_id"}},
				// Step 2: Create child 1
				{Args: []string{"add", "Child 1", "parent:$0.short_id"}},
				// Step 3: Create child 2 (will be deleted)
				{Args: []string{"add", "Child 2", "parent:$0.short_id"}},
				// Step 4: Delete child 2
				{Args: []string{"delete", "$3.short_id"}},
				// Step 5: Start child 1
				{Args: []string{"start", "$2.short_id"}},
				// Step 6: Complete child 1
				{Args: []string{"done", "$2.short_id"}},
				// Step 7: Check parent — should be auto-completed (deleted child ignored)
				{
					Args: []string{"info", "$0.short_id"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						if m["status"] != "completed" {
							t.Fatalf("expected parent 'completed' (deleted child ignored), got %v", m["status"])
						}
					},
				},
			},
		},
		{
			Name: "auto_complete_workflow_guard",
			Steps: []Step{
				// Step 0: Create parent (stays pending — not started)
				{Args: []string{"add", "Parent pending"}},
				// Step 1: Create child
				{Args: []string{"add", "Child", "parent:$0.short_id"}},
				// Step 2: Start child
				{Args: []string{"start", "$1.short_id"}},
				// Step 3: Complete child
				{Args: []string{"done", "$1.short_id"}},
				// Step 4: Check parent — should still be pending (pending->completed not allowed)
				{
					Args: []string{"info", "$0.short_id"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						if m["status"] != "pending" {
							t.Fatalf("expected parent still 'pending' (workflow guard), got %v", m["status"])
						}
					},
				},
			},
		},
	}

	// These tests need auto-complete enabled. We use a custom runner that
	// sets project settings before running the scenario steps.
	combos := combinations(
		[]string{"flag", "env"},
		[]string{"json"}, // JSON only for assertion simplicity
	)
	for _, sc := range scenarios {
		for _, combo := range combos {
			dbMode := combo[0]
			name := sc.Name + "/" + dbMode + "/json"
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				env := newEnv(t, binPath, dbMode, "json")
				// Enable auto-complete before running steps
				env.SetDefaultProjectSettings(autoCompleteSettings)
				for i, step := range sc.Steps {
					r := env.Run(step.Args...)
					if step.WantErr && r.Err == nil {
						t.Fatalf("step %d: expected error, got none. stdout:\n%s", i, r.Stdout)
					}
					if !step.WantErr && r.Err != nil {
						t.Fatalf("step %d: unexpected error: %v\nstderr: %s\nstdout: %s", i, r.Err, r.Stderr, r.Stdout)
					}
					if step.AssertJSON != nil {
						var parsed any
						if err := json.Unmarshal([]byte(r.Stdout), &parsed); err != nil {
							t.Fatalf("step %d: failed to parse JSON: %v\nraw:\n%s", i, err, r.Stdout)
						}
						step.AssertJSON(t, parsed)
					}
				}
			})
		}
	}
}

func TestPropagation_Recursive(t *testing.T) {
	scenarios := []Scenario{
		{
			Name: "auto_complete_recursive",
			Steps: []Step{
				// Step 0: Create grandparent
				{Args: []string{"add", "Grandparent"}},
				// Step 1: Start grandparent
				{Args: []string{"start", "$0.short_id"}},
				// Step 2: Create parent
				{Args: []string{"add", "Parent", "parent:$0.short_id"}},
				// Step 3: Start parent
				{Args: []string{"start", "$2.short_id"}},
				// Step 4: Create child
				{Args: []string{"add", "Child", "parent:$2.short_id"}},
				// Step 5: Start child
				{Args: []string{"start", "$4.short_id"}},
				// Step 6: Complete child — should cascade up
				{Args: []string{"done", "$4.short_id"}},
				// Step 7: Check parent — should be auto-completed
				{
					Args: []string{"info", "$2.short_id"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						if m["status"] != "completed" {
							t.Fatalf("expected parent 'completed', got %v", m["status"])
						}
					},
				},
				// Step 8: Check grandparent — should be auto-completed
				{
					Args: []string{"info", "$0.short_id"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						if m["status"] != "completed" {
							t.Fatalf("expected grandparent 'completed', got %v", m["status"])
						}
					},
				},
			},
		},
	}

	combos := combinations(
		[]string{"flag", "env"},
		[]string{"json"},
	)
	for _, sc := range scenarios {
		for _, combo := range combos {
			dbMode := combo[0]
			name := sc.Name + "/" + dbMode + "/json"
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				env := newEnv(t, binPath, dbMode, "json")
				env.SetDefaultProjectSettings(autoCompleteSettings)
				for i, step := range sc.Steps {
					r := env.Run(step.Args...)
					if step.WantErr && r.Err == nil {
						t.Fatalf("step %d: expected error, got none. stdout:\n%s", i, r.Stdout)
					}
					if !step.WantErr && r.Err != nil {
						t.Fatalf("step %d: unexpected error: %v\nstderr: %s\nstdout: %s", i, r.Err, r.Stderr, r.Stdout)
					}
					if step.AssertJSON != nil {
						var parsed any
						if err := json.Unmarshal([]byte(r.Stdout), &parsed); err != nil {
							t.Fatalf("step %d: failed to parse JSON: %v\nraw:\n%s", i, err, r.Stdout)
						}
						step.AssertJSON(t, parsed)
					}
				}
			})
		}
	}
}
```

Note: The `TestPropagation_Disabled` test uses the standard `runScenarios` helper since it doesn't need project settings enabled. The other tests use a custom runner that calls `env.SetDefaultProjectSettings` before running steps.

Also add `"encoding/json"` to the imports at the top of `propagation_test.go`:

```go
package e2e

import (
	"encoding/json"
	"testing"
)
```

Wait — `json` is already used in the test file via `json.Unmarshal`. Make sure it's imported.

- [ ] **Step 2: Run the E2E propagation tests**

Run: `go test -v ./tests/e2e -run "TestPropagation_" -count=1`
Expected: PASS — all scenarios pass.

- [ ] **Step 3: Commit**

```bash
git add tests/e2e/propagation_test.go
git commit -m "test(e2e): add auto-complete propagation E2E tests"
```

---

### Task 3: E2E Auto-Revert Tests

**Files:**
- Modify: `tests/e2e/propagation_test.go`

- [ ] **Step 1: Add auto-revert E2E scenarios**

Append to `tests/e2e/propagation_test.go`:

```go
func TestPropagation_AutoRevert(t *testing.T) {
	scenarios := []Scenario{
		{
			Name: "auto_revert_child_reopened",
			Steps: []Step{
				// Step 0: Create parent
				{Args: []string{"add", "Parent"}},
				// Step 1: Start parent
				{Args: []string{"start", "$0.short_id"}},
				// Step 2: Create child
				{Args: []string{"add", "Child", "parent:$0.short_id"}},
				// Step 3: Start child
				{Args: []string{"start", "$2.short_id"}},
				// Step 4: Complete child — parent auto-completes
				{Args: []string{"done", "$2.short_id"}},
				// Step 5: Verify parent is completed
				{
					Args: []string{"info", "$0.short_id"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						if m["status"] != "completed" {
							t.Fatalf("expected parent 'completed', got %v", m["status"])
						}
					},
				},
				// Step 6: Re-open child (completed -> pending)
				{Args: []string{"modify", "$2.short_id", "status:pending"}},
				// Step 7: Check parent — should be reverted to active
				{
					Args: []string{"info", "$0.short_id"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						if m["status"] != "active" {
							t.Fatalf("expected parent 'active' after revert, got %v", m["status"])
						}
					},
				},
			},
		},
		{
			Name: "auto_revert_recursive",
			Steps: []Step{
				// Step 0: Create grandparent
				{Args: []string{"add", "Grandparent"}},
				// Step 1: Start grandparent
				{Args: []string{"start", "$0.short_id"}},
				// Step 2: Create parent
				{Args: []string{"add", "Parent", "parent:$0.short_id"}},
				// Step 3: Start parent
				{Args: []string{"start", "$2.short_id"}},
				// Step 4: Create child
				{Args: []string{"add", "Child", "parent:$2.short_id"}},
				// Step 5: Start child
				{Args: []string{"start", "$4.short_id"}},
				// Step 6: Complete child — cascades up
				{Args: []string{"done", "$4.short_id"}},
				// Step 7: Verify grandparent is completed
				{
					Args: []string{"info", "$0.short_id"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						if m["status"] != "completed" {
							t.Fatalf("expected grandparent 'completed', got %v", m["status"])
						}
					},
				},
				// Step 8: Re-open child — cascading revert
				{Args: []string{"modify", "$4.short_id", "status:pending"}},
				// Step 9: Check parent — should be reverted to active
				{
					Args: []string{"info", "$2.short_id"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						if m["status"] != "active" {
							t.Fatalf("expected parent 'active' after revert, got %v", m["status"])
						}
					},
				},
				// Step 10: Check grandparent — should be reverted to active
				{
					Args: []string{"info", "$0.short_id"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						if m["status"] != "active" {
							t.Fatalf("expected grandparent 'active' after revert, got %v", m["status"])
						}
					},
				},
			},
		},
	}

	combos := combinations(
		[]string{"flag", "env"},
		[]string{"json"},
	)
	for _, sc := range scenarios {
		for _, combo := range combos {
			dbMode := combo[0]
			name := sc.Name + "/" + dbMode + "/json"
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				env := newEnv(t, binPath, dbMode, "json")
				env.SetDefaultProjectSettings(bothPropagationSettings)
				for i, step := range sc.Steps {
					r := env.Run(step.Args...)
					if step.WantErr && r.Err == nil {
						t.Fatalf("step %d: expected error, got none. stdout:\n%s", i, r.Stdout)
					}
					if !step.WantErr && r.Err != nil {
						t.Fatalf("step %d: unexpected error: %v\nstderr: %s\nstdout: %s", i, r.Err, r.Stderr, r.Stdout)
					}
					if step.AssertJSON != nil {
						var parsed any
						if err := json.Unmarshal([]byte(r.Stdout), &parsed); err != nil {
							t.Fatalf("step %d: failed to parse JSON: %v\nraw:\n%s", i, err, r.Stdout)
						}
						step.AssertJSON(t, parsed)
					}
				}
			})
		}
	}
}
```

- [ ] **Step 2: Run the auto-revert E2E tests**

Run: `go test -v ./tests/e2e -run "TestPropagation_AutoRevert" -count=1`
Expected: PASS

- [ ] **Step 3: Run all E2E tests**

Run: `make test-e2e`
Expected: PASS — both new and existing E2E tests pass.

- [ ] **Step 4: Commit**

```bash
git add tests/e2e/propagation_test.go
git commit -m "test(e2e): add auto-revert propagation E2E tests"
```

---

### Task 4: Final Verification

**Files:**
- No new files

- [ ] **Step 1: Run the complete test suite with race detector**

Run: `make test-race`
Expected: PASS

- [ ] **Step 2: Run vet and lint**

Run: `make vet && make lint`
Expected: PASS

- [ ] **Step 3: Run all tests one more time**

Run: `make test`
Expected: PASS — everything green.

- [ ] **Step 4: Verify the propagation test count**

Run: `go test -v ./tests/e2e -run "TestPropagation" -count=1 2>&1 | grep -c "PASS:"`
Expected: Should show all propagation test cases passing. Verify the count matches expectations:
- `TestPropagation_Disabled`: 4 combos (2 dbModes x 2 formats)
- `TestPropagation_AutoComplete`: 3 scenarios x 2 dbModes = 6
- `TestPropagation_Recursive`: 1 scenario x 2 dbModes = 2
- `TestPropagation_AutoRevert`: 2 scenarios x 2 dbModes = 4
