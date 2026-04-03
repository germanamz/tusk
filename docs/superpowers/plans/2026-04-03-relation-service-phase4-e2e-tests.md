# Phase 4: E2E Tests

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add end-to-end tests for `link`, `unlink`, and the `info` relations display using the existing E2E test harness.

**Architecture:** Black-box CLI tests that build the binary and run it as a subprocess. Each scenario runs across 4 combinations (flag/env DB mode x text/json output format).

**Tech Stack:** Go testing, E2E harness in `tests/e2e/`

**Prerequisites:** Phases 1-3 must be complete.

**Design spec:** `docs/superpowers/specs/2026-04-03-relation-service-design.md` (Section 5, E2E tests)

**Key files to understand before starting:**
- `tests/e2e/harness.go` — `Step` struct (line 162), `Scenario` struct (line 182), `runScenarios` (line 189), `assertContains`/`assertEqual` helpers (line 232+)
- `tests/e2e/task_lifecycle_test.go` — example scenarios with `$N.short_id` references and `AssertJSON`/`AssertText` callbacks
- `tests/e2e/main_test.go` — binary build and `binPath` setup
- `tests/e2e/error_handling_test.go` — example of `WantErr: true` and `assertStderrContains`

**How the harness works:**
- Each `Step` has `Args` (CLI arguments without `--db`/`--format`, those are injected automatically)
- `$N.field` in Args references the JSON output of step N (e.g., `$0.short_id` gets the `short_id` from step 0's JSON output)
- `WantErr: true` means the step should produce a non-zero exit code
- `AssertJSON` runs only in JSON format mode — `parsed` is the unmarshaled JSON stdout
- `AssertText` runs only in text format mode — `output` is raw stdout string
- `Assert` runs for both formats — receives a `Result` with `Stdout`, `Stderr`, `Err`
- Each scenario runs 4 times: (flag, text), (flag, json), (env, text), (env, json)

---

### Task 1: Happy Path — Link, Info, Unlink

**Files:**
- Create: `tests/e2e/relations_test.go`

- [ ] **Step 1: Create the test file with happy-path scenarios**

Create `tests/e2e/relations_test.go`:

```go
package e2e

import (
	"testing"
)

func TestRelations(t *testing.T) {
	scenarios := []Scenario{
		{
			Name: "link_and_info",
			Steps: []Step{
				// Step 0: Create task A
				{
					Args: []string{"add", "Task A"},
				},
				// Step 1: Create task B
				{
					Args: []string{"add", "Task B"},
				},
				// Step 2: Link A blocks B
				{
					Args: []string{"link", "$0.short_id", "blocks", "$1.short_id"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						assertEqual(t, m["relation_type"], "blocks")
						if m["id"] == nil || m["id"] == "" {
							t.Fatal("expected relation id to be set")
						}
						if m["source_id"] == nil || m["source_id"] == "" {
							t.Fatal("expected source_id to be set")
						}
						if m["target_id"] == nil || m["target_id"] == "" {
							t.Fatal("expected target_id to be set")
						}
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Linked")
						assertContains(t, output, "blocks")
					},
				},
				// Step 3: Info on task A should show the relation
				{
					Args: []string{"info", "$0.short_id"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						relations := m["relations"].([]any)
						if len(relations) != 1 {
							t.Fatalf("expected 1 relation, got %d", len(relations))
						}
						rel := relations[0].(map[string]any)
						assertEqual(t, rel["relation_type"], "blocks")
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Relations:")
						assertContains(t, output, "blocks")
					},
				},
				// Step 4: Unlink A blocks B
				{
					Args: []string{"unlink", "$0.short_id", "blocks", "$1.short_id"},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Unlinked")
					},
				},
				// Step 5: Info on task A should show no relations
				{
					Args: []string{"info", "$0.short_id"},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertNotContains(t, output, "Relations:")
					},
				},
			},
		},
		{
			Name: "link_relates_to",
			Steps: []Step{
				{
					Args: []string{"add", "Task X"},
				},
				{
					Args: []string{"add", "Task Y"},
				},
				{
					Args: []string{"link", "$0.short_id", "relates_to", "$1.short_id"},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Linked")
						assertContains(t, output, "relates_to")
					},
				},
			},
		},
		{
			Name: "link_duplicates",
			Steps: []Step{
				{
					Args: []string{"add", "Task P"},
				},
				{
					Args: []string{"add", "Task Q"},
				},
				{
					Args: []string{"link", "$0.short_id", "duplicates", "$1.short_id"},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Linked")
						assertContains(t, output, "duplicates")
					},
				},
			},
		},
		{
			Name: "info_shows_inverse_relation",
			Steps: []Step{
				// Step 0: Create task A
				{
					Args: []string{"add", "Blocker task"},
				},
				// Step 1: Create task B
				{
					Args: []string{"add", "Blocked task"},
				},
				// Step 2: A blocks B
				{
					Args: []string{"link", "$0.short_id", "blocks", "$1.short_id"},
				},
				// Step 3: Info on task B (the target) should show "blocked_by"
				{
					Args: []string{"info", "$1.short_id"},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "blocked_by")
					},
				},
			},
		},
	}
	runScenarios(t, binPath, scenarios)
}
```

- [ ] **Step 2: Build the binary and run the tests**

Run:
```bash
make test-e2e
```

If the project's `make test-e2e` doesn't build the binary automatically, run:
```bash
go test -v ./tests/e2e/ -run TestRelations
```

Expected: All scenarios pass across all 4 combinations.

- [ ] **Step 3: Commit**

```bash
git add tests/e2e/relations_test.go
git commit -m "test(e2e): add happy-path scenarios for link, unlink, and info relations

Covers link with all three relation types, unlink, info showing
relations, and inverse relation labels (blocked_by)."
```

---

### Task 2: Cycle Detection E2E Tests

**Files:**
- Modify: `tests/e2e/relations_test.go`

- [ ] **Step 1: Add cycle detection scenarios**

Add a new test function to `tests/e2e/relations_test.go`:

```go
func TestRelationsCycleDetection(t *testing.T) {
	scenarios := []Scenario{
		{
			Name: "blocks_direct_cycle",
			Steps: []Step{
				// Step 0: Create task A
				{
					Args: []string{"add", "Task A"},
				},
				// Step 1: Create task B
				{
					Args: []string{"add", "Task B"},
				},
				// Step 2: A blocks B — succeeds
				{
					Args: []string{"link", "$0.short_id", "blocks", "$1.short_id"},
				},
				// Step 3: B blocks A — should fail (cycle)
				{
					Args:    []string{"link", "$1.short_id", "blocks", "$0.short_id"},
					WantErr: true,
					Assert: func(t *testing.T, r Result) {
						t.Helper()
						assertStderrContains(t, r, "cycle")
					},
				},
			},
		},
		{
			Name: "blocks_transitive_cycle",
			Steps: []Step{
				// Step 0: Task A
				{
					Args: []string{"add", "Task A"},
				},
				// Step 1: Task B
				{
					Args: []string{"add", "Task B"},
				},
				// Step 2: Task C
				{
					Args: []string{"add", "Task C"},
				},
				// Step 3: A blocks B
				{
					Args: []string{"link", "$0.short_id", "blocks", "$1.short_id"},
				},
				// Step 4: B blocks C
				{
					Args: []string{"link", "$1.short_id", "blocks", "$2.short_id"},
				},
				// Step 5: C blocks A — should fail (cycle: A->B->C->A)
				{
					Args:    []string{"link", "$2.short_id", "blocks", "$0.short_id"},
					WantErr: true,
					Assert: func(t *testing.T, r Result) {
						t.Helper()
						assertStderrContains(t, r, "cycle")
					},
				},
			},
		},
		{
			Name: "blocks_chain_no_cycle",
			Steps: []Step{
				{
					Args: []string{"add", "Task A"},
				},
				{
					Args: []string{"add", "Task B"},
				},
				{
					Args: []string{"add", "Task C"},
				},
				// A blocks B — ok
				{
					Args: []string{"link", "$0.short_id", "blocks", "$1.short_id"},
				},
				// B blocks C — ok (chain, not a cycle)
				{
					Args: []string{"link", "$1.short_id", "blocks", "$2.short_id"},
				},
			},
		},
	}
	runScenarios(t, binPath, scenarios)
}
```

- [ ] **Step 2: Run the cycle detection tests**

Run:
```bash
go test -v ./tests/e2e/ -run TestRelationsCycleDetection
```

Expected: All scenarios pass. The direct cycle and transitive cycle scenarios fail with non-zero exit and stderr containing "cycle". The chain scenario succeeds.

- [ ] **Step 3: Commit**

```bash
git add tests/e2e/relations_test.go
git commit -m "test(e2e): add cycle detection scenarios for link command

Covers direct cycle (A->B, B->A), transitive cycle (A->B->C, C->A),
and valid chain (A->B->C) to verify no false positives."
```

---

### Task 3: Error Handling E2E Tests

**Files:**
- Modify: `tests/e2e/relations_test.go`

- [ ] **Step 1: Add error scenarios**

Add another test function to `tests/e2e/relations_test.go`:

```go
func TestRelationsErrors(t *testing.T) {
	scenarios := []Scenario{
		{
			Name: "link_task_not_found",
			Steps: []Step{
				{
					Args: []string{"add", "Existing task"},
				},
				{
					Args:    []string{"link", "$0.short_id", "blocks", "nonexist"},
					WantErr: true,
					Assert: func(t *testing.T, r Result) {
						t.Helper()
						assertStderrContains(t, r, "not found")
					},
				},
			},
		},
		{
			Name: "link_duplicate",
			Steps: []Step{
				{
					Args: []string{"add", "Task A"},
				},
				{
					Args: []string{"add", "Task B"},
				},
				// First link — ok
				{
					Args: []string{"link", "$0.short_id", "blocks", "$1.short_id"},
				},
				// Second identical link — error
				{
					Args:    []string{"link", "$0.short_id", "blocks", "$1.short_id"},
					WantErr: true,
					Assert: func(t *testing.T, r Result) {
						t.Helper()
						assertStderrContains(t, r, "already exists")
					},
				},
			},
		},
		{
			Name: "link_invalid_type",
			Steps: []Step{
				{
					Args: []string{"add", "Task A"},
				},
				{
					Args: []string{"add", "Task B"},
				},
				{
					Args:    []string{"link", "$0.short_id", "depends_on", "$1.short_id"},
					WantErr: true,
					Assert: func(t *testing.T, r Result) {
						t.Helper()
						assertStderrContains(t, r, "invalid relation type")
					},
				},
			},
		},
		{
			Name: "unlink_not_found",
			Steps: []Step{
				{
					Args: []string{"add", "Task A"},
				},
				{
					Args: []string{"add", "Task B"},
				},
				{
					Args:    []string{"unlink", "$0.short_id", "blocks", "$1.short_id"},
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

- [ ] **Step 2: Run the error tests**

Run:
```bash
go test -v ./tests/e2e/ -run TestRelationsErrors
```

Expected: All scenarios pass. Each error step produces non-zero exit code and the expected error message in stderr.

- [ ] **Step 3: Run the full test suite to confirm everything works together**

Run:
```bash
make test
```

Expected: All unit tests, service tests, and E2E tests pass.

- [ ] **Step 4: Commit**

```bash
git add tests/e2e/relations_test.go
git commit -m "test(e2e): add error handling scenarios for link and unlink

Covers task not found, duplicate relation, invalid relation type,
and unlink on non-existent relation."
```
