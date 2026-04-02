# Phase 4: TaskService Read Operations

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add `GetByID`, `List`, `GetChildren`, and `GetDescendants` methods to TaskService.

**Prereqs:** Phase 3 must be complete (`TaskService` struct, constructor, `Create`, and `GetByShortID` exist).

**Files:**
- Modify: `internal/service/task.go` (append methods)
- Modify: `internal/service/task_test.go` (append tests)

---

## Background

Read operations are thin pass-throughs to the repository layer. They don't add business logic — that's intentional. The service layer adds value on writes (validation, workflow enforcement). If we need computed fields (like urgency scores) on read results later, this is where they'd go.

`GetByShortID` was already implemented in Phase 3 because `Create` tests need it to verify persistence.

The `TaskFilter` struct (defined in `internal/domain/filter.go`) has these optional fields:

```go
type TaskFilter struct {
	ProjectID   *uuid.UUID
	ParentID    *uuid.UUID
	RootID      *uuid.UUID   // for tree: all descendants
	Statuses    []string     // OR match
	Tags        []string     // include
	ExcludeTags []string     // exclude
	PriorityMin *int
	PriorityMax *int
	DueAfter    *time.Time
	DueBefore   *time.Time
	WaitingOnly *bool
}
```

The `TaskRepository` interface methods you'll be delegating to:

```go
GetByID(ctx context.Context, id uuid.UUID) (*domain.Task, error)
List(ctx context.Context, filter domain.TaskFilter) ([]*domain.Task, error)
GetChildren(ctx context.Context, parentID uuid.UUID) ([]*domain.Task, error)
GetDescendants(ctx context.Context, rootID uuid.UUID) ([]*domain.Task, error)
```

---

## Task 1: Write failing tests for read operations

**Files:**
- Modify: `internal/service/task_test.go` (append)

- [ ] **Step 1: Add `errors` import and read operation tests**

First, add `"errors"` to the import block at the top of `internal/service/task_test.go` (it's needed for `errors.Is` calls in `TestGetByShortID_NotFound`):

```go
import (
	"context"
	"errors"    // ← add this line
	"testing"
	"time"
	// ... rest of imports unchanged
)
```

Then append these test functions to the file:

```go
func TestGetByShortID_Found(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	task := newMinimalTask("Find me")
	mustCreateTask(t, env.taskSvc, task)

	got, err := env.taskSvc.GetByShortID(ctx, task.ShortID)
	if err != nil {
		t.Fatalf("GetByShortID: %v", err)
	}
	if got.Title != "Find me" {
		t.Fatalf("expected 'Find me', got %q", got.Title)
	}
}

func TestGetByShortID_NotFound(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	_, err := env.taskSvc.GetByShortID(ctx, "nonexist")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestGetByID_Found(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	task := newMinimalTask("Get by ID")
	mustCreateTask(t, env.taskSvc, task)

	got, err := env.taskSvc.GetByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Title != "Get by ID" {
		t.Fatalf("expected 'Get by ID', got %q", got.Title)
	}
}

func TestList_Empty(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	tasks, err := env.taskSvc.List(ctx, domain.TaskFilter{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(tasks) != 0 {
		t.Fatalf("expected 0 tasks, got %d", len(tasks))
	}
}

func TestList_WithFilter(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	t1 := newMinimalTask("Task one")
	t1.Priority = 3
	mustCreateTask(t, env.taskSvc, t1)

	t2 := newMinimalTask("Task two")
	t2.Priority = 1
	mustCreateTask(t, env.taskSvc, t2)

	minPri := 3
	tasks, err := env.taskSvc.List(ctx, domain.TaskFilter{PriorityMin: &minPri})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task with priority >= 3, got %d", len(tasks))
	}
	if tasks[0].Title != "Task one" {
		t.Fatalf("expected 'Task one', got %q", tasks[0].Title)
	}
}

func TestGetChildren(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	parent := newMinimalTask("Parent")
	mustCreateTask(t, env.taskSvc, parent)

	child1 := newMinimalTask("Child 1")
	child1.ParentID = &parent.ID
	mustCreateTask(t, env.taskSvc, child1)

	child2 := newMinimalTask("Child 2")
	child2.ParentID = &parent.ID
	mustCreateTask(t, env.taskSvc, child2)

	children, err := env.taskSvc.GetChildren(ctx, parent.ID)
	if err != nil {
		t.Fatalf("GetChildren: %v", err)
	}
	if len(children) != 2 {
		t.Fatalf("expected 2 children, got %d", len(children))
	}
}

func TestGetDescendants(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	root := newMinimalTask("Root")
	mustCreateTask(t, env.taskSvc, root)

	child := newMinimalTask("Child")
	child.ParentID = &root.ID
	mustCreateTask(t, env.taskSvc, child)

	grandchild := newMinimalTask("Grandchild")
	grandchild.ParentID = &child.ID
	mustCreateTask(t, env.taskSvc, grandchild)

	descendants, err := env.taskSvc.GetDescendants(ctx, root.ID)
	if err != nil {
		t.Fatalf("GetDescendants: %v", err)
	}
	if len(descendants) != 2 {
		t.Fatalf("expected 2 descendants, got %d", len(descendants))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/service/ -run "TestGetBy|TestList|TestGetChildren|TestGetDescendants" -v`

Expected: **compilation error** — `GetByID`, `List`, `GetChildren`, `GetDescendants` are not defined on `TaskService`.

- [ ] **Step 3: Commit**

```bash
git add internal/service/task_test.go
git commit -m "test(service): add failing read operation tests"
```

---

## Task 2: Implement read operations

**Files:**
- Modify: `internal/service/task.go`

- [ ] **Step 1: Add read methods**

Open `internal/service/task.go`. Find the `GetByShortID` method (added in Phase 3). Add the following methods directly after it:

```go
// GetByID retrieves a task by its full UUID.
func (s *TaskService) GetByID(ctx context.Context, id uuid.UUID) (*domain.Task, error) {
	return s.taskRepo.GetByID(ctx, id)
}

// List returns tasks matching the given filter.
func (s *TaskService) List(ctx context.Context, filter domain.TaskFilter) ([]*domain.Task, error) {
	return s.taskRepo.List(ctx, filter)
}

// GetChildren returns the direct children of a task.
func (s *TaskService) GetChildren(ctx context.Context, parentID uuid.UUID) ([]*domain.Task, error) {
	return s.taskRepo.GetChildren(ctx, parentID)
}

// GetDescendants returns all descendants of a task (recursive).
func (s *TaskService) GetDescendants(ctx context.Context, rootID uuid.UUID) ([]*domain.Task, error) {
	return s.taskRepo.GetDescendants(ctx, rootID)
}
```

- [ ] **Step 2: Run the read tests to verify they pass**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/service/ -run "TestGetBy|TestList|TestGetChildren|TestGetDescendants" -v`

Expected output — all 7 tests PASS:

```
=== RUN   TestGetByShortID_Found
--- PASS: TestGetByShortID_Found
=== RUN   TestGetByShortID_NotFound
--- PASS: TestGetByShortID_NotFound
=== RUN   TestGetByID_Found
--- PASS: TestGetByID_Found
=== RUN   TestList_Empty
--- PASS: TestList_Empty
=== RUN   TestList_WithFilter
--- PASS: TestList_WithFilter
=== RUN   TestGetChildren
--- PASS: TestGetChildren
=== RUN   TestGetDescendants
--- PASS: TestGetDescendants
PASS
```

- [ ] **Step 3: Run the full service test suite**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/service/ -v`

Expected: all tests pass (5 workflow + 10 create + 7 read = 22 tests).

- [ ] **Step 4: Commit**

```bash
git add internal/service/task.go
git commit -m "feat(service): implement TaskService read operations"
```
