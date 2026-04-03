# Phase 1: Circular Parent Detection — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Prevent circular parent-child relationships (A is parent of B, B is parent of A) by walking the ancestor chain before setting a task's parent.

**Architecture:** Add a `detectParentCycle` method to `TaskService` that walks up from the proposed parent via `GetByID` calls. If it reaches the task being modified, return a new `ErrCyclicParent` sentinel. Call this from both `Create` and `Update`.

**Tech Stack:** Go, SQLite (existing), standard library only.

---

### Task 1: Add `ErrCyclicParent` sentinel error

**Files:**
- Modify: `internal/domain/errors.go`

- [ ] **Step 1: Add the sentinel error**

Open `internal/domain/errors.go` and add `ErrCyclicParent` to the `var` block. Place it right after `ErrCyclicBlock` (line 11) to keep related errors together.

The full `var` block should become:

```go
var (
	ErrNotFound          = errors.New("not found")
	ErrConflict          = errors.New("version conflict")
	ErrCyclicBlock       = errors.New("relation would create a cycle in blocks graph")
	ErrCyclicParent      = errors.New("parent would create a cycle in task hierarchy")
	ErrInvalidTransition = errors.New("status transition not allowed by workflow")
	ErrDuplicateRelation = errors.New("relation already exists")
	ErrSourceNotFound    = fmt.Errorf("source task: %w", ErrNotFound)
	ErrTargetNotFound    = fmt.Errorf("target task: %w", ErrNotFound)
)
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./internal/domain/...`
Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add internal/domain/errors.go
git commit -m "feat(domain): add ErrCyclicParent sentinel error"
```

---

### Task 2: Add `detectParentCycle` method and wire into `Create`

**Files:**
- Modify: `internal/service/task.go`
- Modify: `internal/service/task_test.go`

- [ ] **Step 1: Write the failing test for cycle detection on Create**

Add this test to `internal/service/task_test.go` at the end of the file:

```go
func TestCreate_CyclicParentRejected(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	// Create A
	taskA := newMinimalTask("Task A")
	mustCreateTask(t, env.taskSvc, taskA)

	// Create B with parent A
	taskB := newMinimalTask("Task B")
	taskB.ParentID = &taskA.ID
	mustCreateTask(t, env.taskSvc, taskB)

	// Try to create C with parent B, then make A's parent C — but for Create,
	// the cycle case is: create a new task whose parent is B, where B's parent is A.
	// That's actually fine (linear chain). The cycle case on Create is limited:
	// you can't create a cycle because the new task doesn't exist yet, so no existing
	// task can point to it as a parent. The real cycle risk is in Update.
	//
	// However, we still test that the method exists and works for the base case.
	// Create with a valid parent chain should succeed.
	taskC := newMinimalTask("Task C")
	taskC.ParentID = &taskB.ID
	if err := env.taskSvc.Create(ctx, taskC); err != nil {
		t.Fatalf("Create with valid parent chain should succeed: %v", err)
	}
}
```

- [ ] **Step 2: Run the test to confirm it passes (this is a baseline — no cycle possible on create of a new task)**

Run: `go test -v ./internal/service -run TestCreate_CyclicParentRejected`
Expected: PASS. This confirms the chain A -> B -> C works correctly.

- [ ] **Step 3: Add the `detectParentCycle` method to `TaskService`**

Add this method to `internal/service/task.go`, right before the `generateShortID` method (before line 286):

```go
// detectParentCycle walks up the ancestor chain from proposedParentID.
// If it encounters taskID, the proposed parent relationship would create a cycle.
// Returns ErrCyclicParent if a cycle is detected, nil otherwise.
func (s *TaskService) detectParentCycle(ctx context.Context, taskID, proposedParentID uuid.UUID) error {
	current := proposedParentID
	for {
		if current == taskID {
			return domain.ErrCyclicParent
		}
		parent, err := s.taskRepo.GetByID(ctx, current)
		if err != nil {
			return fmt.Errorf("checking parent cycle: %w", err)
		}
		if parent.ParentID == nil {
			return nil
		}
		current = *parent.ParentID
	}
}
```

- [ ] **Step 4: Wire `detectParentCycle` into `Create`**

In `internal/service/task.go`, inside the `Create` method, replace the parent validation block (lines 71-80) with:

```go
	// Validate parent exists and would not create a cycle
	if task.ParentID != nil {
		_, err := s.taskRepo.GetByID(ctx, *task.ParentID)
		if err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				return fmt.Errorf("parent task not found: %w", err)
			}
			return fmt.Errorf("looking up parent task: %w", err)
		}
		// Note: On Create, cycles are impossible because the new task has no ID yet
		// that other tasks could reference as a parent. The cycle check is only
		// meaningful in Update. We keep the existence check here for clarity.
	}
```

Note: Since `Create` generates the task ID *after* parent validation (line 102), no existing task can have the new task as a parent. Cycle detection is only meaningful in `Update`. We do NOT call `detectParentCycle` in `Create` — only in `Update` (next task).

- [ ] **Step 5: Run all service tests to confirm nothing is broken**

Run: `go test -v ./internal/service/...`
Expected: all tests PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/service/task.go internal/service/task_test.go
git commit -m "feat(service): add detectParentCycle method"
```

---

### Task 3: Wire cycle detection into `Update` with tests

**Files:**
- Modify: `internal/service/task.go`
- Modify: `internal/service/task_test.go`

- [ ] **Step 1: Write the failing test for direct cycle (A->B, then set A.parent=B)**

Add to `internal/service/task_test.go`:

```go
func TestUpdate_CyclicParentDirectRejected(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	// Create A
	taskA := newMinimalTask("Task A")
	mustCreateTask(t, env.taskSvc, taskA)

	// Create B with parent A
	taskB := newMinimalTask("Task B")
	taskB.ParentID = &taskA.ID
	mustCreateTask(t, env.taskSvc, taskB)

	// Try to set A's parent to B — should fail (cycle: A->B->A)
	parentRef := &taskB.ID
	_, err := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID:  taskA.ShortID,
		Version:  taskA.Version,
		ParentID: &parentRef,
	})
	if !errors.Is(err, domain.ErrCyclicParent) {
		t.Fatalf("expected ErrCyclicParent, got %v", err)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test -v ./internal/service -run TestUpdate_CyclicParentDirectRejected`
Expected: FAIL — currently the service doesn't check for cycles, so it will succeed when it shouldn't.

- [ ] **Step 3: Write the failing test for transitive cycle (A->B->C, then set A.parent=C)**

Add to `internal/service/task_test.go`:

```go
func TestUpdate_CyclicParentTransitiveRejected(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	// Create chain: A -> B -> C (A is root, B's parent is A, C's parent is B)
	taskA := newMinimalTask("Task A")
	mustCreateTask(t, env.taskSvc, taskA)

	taskB := newMinimalTask("Task B")
	taskB.ParentID = &taskA.ID
	mustCreateTask(t, env.taskSvc, taskB)

	taskC := newMinimalTask("Task C")
	taskC.ParentID = &taskB.ID
	mustCreateTask(t, env.taskSvc, taskC)

	// Try to set A's parent to C — should fail (cycle: A->B->C->A)
	parentRef := &taskC.ID
	_, err := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID:  taskA.ShortID,
		Version:  taskA.Version,
		ParentID: &parentRef,
	})
	if !errors.Is(err, domain.ErrCyclicParent) {
		t.Fatalf("expected ErrCyclicParent, got %v", err)
	}
}
```

- [ ] **Step 4: Write a test that confirms a valid reparent still works**

Add to `internal/service/task_test.go`:

```go
func TestUpdate_ReparentNoCycle(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	// Create three independent tasks
	taskA := newMinimalTask("Task A")
	mustCreateTask(t, env.taskSvc, taskA)

	taskB := newMinimalTask("Task B")
	mustCreateTask(t, env.taskSvc, taskB)

	taskC := newMinimalTask("Task C")
	mustCreateTask(t, env.taskSvc, taskC)

	// Set B's parent to A — should succeed (no cycle)
	parentRef := &taskA.ID
	updated, err := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID:  taskB.ShortID,
		Version:  taskB.Version,
		ParentID: &parentRef,
	})
	if err != nil {
		t.Fatalf("expected reparent to succeed, got %v", err)
	}
	if updated.ParentID == nil || *updated.ParentID != taskA.ID {
		t.Fatalf("expected parent to be task A")
	}
}
```

- [ ] **Step 5: Wire `detectParentCycle` into `Update`**

In `internal/service/task.go`, replace the parent validation block in `Update` (lines 203-215) with:

```go
	// Validate parent if changed
	if upd.ParentID != nil && task.ParentID != nil {
		if *task.ParentID == task.ID {
			return nil, fmt.Errorf("task cannot be its own parent")
		}
		_, err := s.taskRepo.GetByID(ctx, *task.ParentID)
		if err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				return nil, fmt.Errorf("parent task not found: %w", err)
			}
			return nil, fmt.Errorf("looking up parent task: %w", err)
		}
		// Check for cycles: walk up from proposed parent — if we reach this task, it's a cycle
		if err := s.detectParentCycle(ctx, task.ID, *task.ParentID); err != nil {
			return nil, err
		}
	}
```

- [ ] **Step 6: Run all three new tests to verify they pass**

Run: `go test -v ./internal/service -run "TestUpdate_CyclicParent|TestUpdate_ReparentNoCycle"`
Expected: all 3 tests PASS.

- [ ] **Step 7: Run the full service test suite to confirm no regressions**

Run: `go test -v ./internal/service/...`
Expected: all tests PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/service/task.go internal/service/task_test.go
git commit -m "feat(service): wire cycle detection into task Update"
```

---

### Task 4: Surface `ErrCyclicParent` in CLI error formatting

**Files:**
- Modify: `internal/tui/commands.go`
- Modify: `internal/tui/commands_test.go`

- [ ] **Step 1: Write the failing test for the error formatting**

Add to `internal/tui/commands_test.go`:

```go
func TestFormatError_CyclicParent(t *testing.T) {
	err := domain.ErrCyclicParent
	got := formatError(err, "abc12345")
	want := "parent would create a cycle in task hierarchy"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test -v ./internal/tui -run TestFormatError_CyclicParent`
Expected: FAIL — the current `formatError` doesn't handle `ErrCyclicParent`, so it falls through to the default case which returns `err.Error()`. Actually, since `ErrCyclicParent` is a plain sentinel and `formatError` falls through to `err.Error()`, the output would be the sentinel's message. Let's check: `domain.ErrCyclicParent.Error()` returns `"parent would create a cycle in task hierarchy"`, which matches `want`. So this test will actually PASS with the default handler.

Let's update the test to verify a wrapped error also works, since the service wraps errors:

```go
func TestFormatError_CyclicParent(t *testing.T) {
	err := fmt.Errorf("setting parent: %w", domain.ErrCyclicParent)
	got := formatError(err, "abc12345")
	want := "parent would create a cycle in task hierarchy"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}
```

Run: `go test -v ./internal/tui -run TestFormatError_CyclicParent`
Expected: FAIL — the default case returns the full wrapped message.

- [ ] **Step 3: Add `ErrCyclicParent` handling to `formatError`**

In `internal/tui/commands.go`, update the `formatError` function (lines 16-27). Add a new case after the `ErrInvalidTransition` case:

```go
func formatError(err error, shortID string) string {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return fmt.Sprintf("Task not found: %s", shortID)
	case errors.Is(err, domain.ErrConflict):
		return "Version conflict - task was modified by another process"
	case errors.Is(err, domain.ErrInvalidTransition):
		return err.Error()
	case errors.Is(err, domain.ErrCyclicParent):
		return domain.ErrCyclicParent.Error()
	default:
		return err.Error()
	}
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test -v ./internal/tui -run TestFormatError_CyclicParent`
Expected: PASS.

- [ ] **Step 5: Run the full TUI test suite**

Run: `go test -v ./internal/tui/...`
Expected: all tests PASS.

- [ ] **Step 6: Run the full project test suite**

Run: `make test`
Expected: all tests PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/tui/commands.go internal/tui/commands_test.go
git commit -m "feat(tui): surface ErrCyclicParent in CLI error formatting"
```
