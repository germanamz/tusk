# Phase 6: TaskService Convenience Transitions

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Implement `Start`, `Complete`, and `Delete` methods as convenience wrappers around `Update`.

**Prereqs:** Phase 5 must be complete (`Update` method exists).

**Files:**
- Modify: `internal/service/task.go` (append methods)
- Modify: `internal/service/task_test.go` (append tests)

---

## Background

These three methods are thin wrappers that build a `TaskUpdate` with the appropriate status and delegate to `Update`. They exist to make the API ergonomic for CLI commands like `tusk start`, `tusk done`, and `tusk delete`.

**Important:** `Delete` is a **soft delete** — it transitions the status to `"deleted"` via the workflow. It does NOT remove the row from the database. The `taskRepo.Delete` method (which does hard delete) is intentionally unused.

The default workflow allows these transitions:

| Method | Transition | Allowed from |
|---|---|---|
| `Start` | → `active` | `pending` |
| `Complete` | → `completed` | `active` |
| `Delete` | → `deleted` | `pending`, `active` |

All three use the `ptr[T]` generic helper (already defined in `task.go`) to create pointers:

```go
func ptr[T any](v T) *T {
    return &v
}
```

---

## Task 1: Write failing tests for convenience transitions

**Files:**
- Modify: `internal/service/task_test.go` (append)

- [ ] **Step 1: Add transition tests**

Append to `internal/service/task_test.go`:

```go
func TestStart_HappyPath(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	task := newMinimalTask("Start me")
	mustCreateTask(t, env.taskSvc, task)

	updated, err := env.taskSvc.Start(ctx, task.ShortID, 1)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if updated.Status != "active" {
		t.Fatalf("expected status 'active', got %q", updated.Status)
	}
	if updated.Version != 2 {
		t.Fatalf("expected version 2, got %d", updated.Version)
	}
}

func TestStart_AlreadyActive(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	task := newMinimalTask("Already active")
	mustCreateTask(t, env.taskSvc, task)

	_, err := env.taskSvc.Start(ctx, task.ShortID, 1)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	// active → active is not a valid transition
	_, err = env.taskSvc.Start(ctx, task.ShortID, 2)
	if !errors.Is(err, domain.ErrInvalidTransition) {
		t.Fatalf("expected ErrInvalidTransition, got %v", err)
	}
}

func TestComplete_HappyPath(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	task := newMinimalTask("Complete me")
	mustCreateTask(t, env.taskSvc, task)

	// Must start first: pending → active
	started, err := env.taskSvc.Start(ctx, task.ShortID, 1)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Then complete: active → completed
	completed, err := env.taskSvc.Complete(ctx, task.ShortID, started.Version)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if completed.Status != "completed" {
		t.Fatalf("expected status 'completed', got %q", completed.Status)
	}
}

func TestComplete_FromPending(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	task := newMinimalTask("Skip start")
	mustCreateTask(t, env.taskSvc, task)

	// pending → completed is not allowed
	_, err := env.taskSvc.Complete(ctx, task.ShortID, 1)
	if !errors.Is(err, domain.ErrInvalidTransition) {
		t.Fatalf("expected ErrInvalidTransition, got %v", err)
	}
}

func TestDelete_HappyPath(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	task := newMinimalTask("Delete me")
	mustCreateTask(t, env.taskSvc, task)

	// pending → deleted is allowed
	deleted, err := env.taskSvc.Delete(ctx, task.ShortID, 1)
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if deleted.Status != "deleted" {
		t.Fatalf("expected status 'deleted', got %q", deleted.Status)
	}
}

func TestDelete_FromCompleted(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	task := newMinimalTask("Complete then delete")
	mustCreateTask(t, env.taskSvc, task)

	started, _ := env.taskSvc.Start(ctx, task.ShortID, 1)
	completed, _ := env.taskSvc.Complete(ctx, task.ShortID, started.Version)

	// completed → deleted is not allowed in default workflow
	_, err := env.taskSvc.Delete(ctx, task.ShortID, completed.Version)
	if !errors.Is(err, domain.ErrInvalidTransition) {
		t.Fatalf("expected ErrInvalidTransition, got %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/service/ -run "TestStart|TestComplete|TestDelete" -v`

Expected: **compilation error** — `Start`, `Complete`, `Delete` methods are not defined on `TaskService`.

- [ ] **Step 3: Commit**

```bash
git add internal/service/task_test.go
git commit -m "test(service): add failing convenience transition tests"
```

---

## Task 2: Implement `Start`, `Complete`, `Delete`

**Files:**
- Modify: `internal/service/task.go`

- [ ] **Step 1: Add convenience transition methods**

Open `internal/service/task.go`. Find the `Update` method. Add the following three methods directly after it:

```go
// Start transitions a task from its current status to "active".
func (s *TaskService) Start(ctx context.Context, shortID string, version int) (*domain.Task, error) {
	return s.Update(ctx, domain.TaskUpdate{
		ShortID: shortID,
		Version: version,
		Status:  ptr("active"),
	})
}

// Complete transitions a task from its current status to "completed".
func (s *TaskService) Complete(ctx context.Context, shortID string, version int) (*domain.Task, error) {
	return s.Update(ctx, domain.TaskUpdate{
		ShortID: shortID,
		Version: version,
		Status:  ptr("completed"),
	})
}

// Delete soft-deletes a task by transitioning its status to "deleted".
func (s *TaskService) Delete(ctx context.Context, shortID string, version int) (*domain.Task, error) {
	return s.Update(ctx, domain.TaskUpdate{
		ShortID: shortID,
		Version: version,
		Status:  ptr("deleted"),
	})
}
```

- [ ] **Step 2: Run the transition tests to verify they pass**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/service/ -run "TestStart|TestComplete|TestDelete" -v`

Expected output — all 6 tests PASS:

```
=== RUN   TestStart_HappyPath
--- PASS: TestStart_HappyPath
=== RUN   TestStart_AlreadyActive
--- PASS: TestStart_AlreadyActive
=== RUN   TestComplete_HappyPath
--- PASS: TestComplete_HappyPath
=== RUN   TestComplete_FromPending
--- PASS: TestComplete_FromPending
=== RUN   TestDelete_HappyPath
--- PASS: TestDelete_HappyPath
=== RUN   TestDelete_FromCompleted
--- PASS: TestDelete_FromCompleted
PASS
```

- [ ] **Step 3: Run the full service test suite**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/service/ -v`

Expected: all tests pass (5 workflow + 10 create + 7 read + 9 update + 6 transitions = 37 tests).

- [ ] **Step 4: Commit**

```bash
git add internal/service/task.go
git commit -m "feat(service): implement Start, Complete, Delete convenience transitions"
```
