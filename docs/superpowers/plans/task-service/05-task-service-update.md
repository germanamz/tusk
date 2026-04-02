# Phase 5: TaskService Update Method

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Implement `TaskService.Update` with partial updates, validation, workflow enforcement, and optimistic locking.

**Prereqs:** Phase 4 must be complete (all read operations exist).

**Files:**
- Modify: `internal/service/task.go` (append method)
- Modify: `internal/service/task_test.go` (append tests)

---

## Background

### How `Update` works

The `Update` method is the most complex method in `TaskService`. It:

1. **Loads the current task** from the database by short ID
2. **Checks the version** — if the caller's version doesn't match, return `ErrConflict` (optimistic locking)
3. **Applies the patch** — for each non-nil field in `TaskUpdate`, overwrites the corresponding field on the loaded task
4. **Validates the patched state** — same rules as `Create` (title non-empty, priority 0–4, parent/project exist)
5. **Validates workflow transition** — if the status changed, checks that the transition is allowed
6. **Persists** via `taskRepo.Update` (which also checks the version in the `WHERE` clause)
7. **Re-reads** the task to return the persisted state with the bumped version

### The `TaskUpdate` struct (from Phase 1)

```go
type TaskUpdate struct {
	ShortID        string          // required — identifies the task
	Version        int             // required — optimistic locking
	Title          *string         // nil = don't change
	Description    *string
	Status         *string
	Priority       *int
	ParentID       **uuid.UUID     // nil = don't change, non-nil+nil = clear, non-nil+non-nil = set
	ProjectID      **uuid.UUID
	DueAt          **time.Time
	WaitUntil      **time.Time
	RecurrenceRule **string
	UDA            *map[string]any
}
```

### Version flow

```
Caller sends version=1 → Service checks task.Version==1 → Applies changes →
Repo does UPDATE ... WHERE version=1 → Repo sets version=2 → Service re-reads → Returns task with version=2
```

If another caller updated first (version is now 2), the service's early check catches it. If the race is very tight, the repo's `WHERE version=?` clause is the authoritative guard.

---

## Task 1: Write failing tests for `Update`

**Files:**
- Modify: `internal/service/task_test.go` (append)

- [ ] **Step 1: Add Update tests**

Append to `internal/service/task_test.go`:

```go
func TestUpdate_PartialUpdate(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	task := newMinimalTask("Original title")
	task.Priority = 1
	mustCreateTask(t, env.taskSvc, task)

	newTitle := "Updated title"
	updated, err := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID: task.ShortID,
		Version: 1,
		Title:   &newTitle,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Title != "Updated title" {
		t.Fatalf("expected 'Updated title', got %q", updated.Title)
	}
	// Priority should be unchanged
	if updated.Priority != 1 {
		t.Fatalf("expected priority 1 unchanged, got %d", updated.Priority)
	}
	// Version should be bumped
	if updated.Version != 2 {
		t.Fatalf("expected version 2, got %d", updated.Version)
	}
}

func TestUpdate_VersionConflict(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	task := newMinimalTask("Conflict test")
	mustCreateTask(t, env.taskSvc, task)

	newTitle := "First update"
	_, err := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID: task.ShortID,
		Version: 1,
		Title:   &newTitle,
	})
	if err != nil {
		t.Fatalf("first Update: %v", err)
	}

	// Try to update with stale version
	staleTitle := "Stale update"
	_, err = env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID: task.ShortID,
		Version: 1,
		Title:   &staleTitle,
	})
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("expected ErrConflict, got %v", err)
	}
}

func TestUpdate_StatusTransitionAllowed(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	task := newMinimalTask("Transition test")
	mustCreateTask(t, env.taskSvc, task)

	activeStatus := "active"
	updated, err := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID: task.ShortID,
		Version: 1,
		Status:  &activeStatus,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Status != "active" {
		t.Fatalf("expected status 'active', got %q", updated.Status)
	}
}

func TestUpdate_StatusTransitionDisallowed(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	task := newMinimalTask("Bad transition")
	mustCreateTask(t, env.taskSvc, task)

	// pending → completed is not allowed in the default workflow
	completedStatus := "completed"
	_, err := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID: task.ShortID,
		Version: 1,
		Status:  &completedStatus,
	})
	if !errors.Is(err, domain.ErrInvalidTransition) {
		t.Fatalf("expected ErrInvalidTransition, got %v", err)
	}
}

func TestUpdate_EmptyTitleRejected(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	task := newMinimalTask("Will be emptied")
	mustCreateTask(t, env.taskSvc, task)

	emptyTitle := ""
	_, err := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID: task.ShortID,
		Version: 1,
		Title:   &emptyTitle,
	})
	if err == nil {
		t.Fatal("expected error for empty title")
	}
}

func TestUpdate_InvalidPriorityRejected(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	task := newMinimalTask("Bad priority update")
	mustCreateTask(t, env.taskSvc, task)

	badPriority := 5
	_, err := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID:  task.ShortID,
		Version:  1,
		Priority: &badPriority,
	})
	if err == nil {
		t.Fatal("expected error for priority > 4")
	}
}

func TestUpdate_ParentCannotBeSelf(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	task := newMinimalTask("Self parent")
	mustCreateTask(t, env.taskSvc, task)

	selfRef := &task.ID
	_, err := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID:  task.ShortID,
		Version:  1,
		ParentID: &selfRef,
	})
	if err == nil {
		t.Fatal("expected error when setting parent to self")
	}
}

func TestUpdate_ClearNullableField(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Millisecond)
	due := now.Add(24 * time.Hour)
	task := newMinimalTask("Has due date")
	task.DueAt = &due
	mustCreateTask(t, env.taskSvc, task)

	// Clear the due date by setting outer pointer to non-nil, inner to nil
	var nilTime *time.Time
	updated, err := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID: task.ShortID,
		Version: 1,
		DueAt:   &nilTime,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.DueAt != nil {
		t.Fatal("expected DueAt to be cleared")
	}
}

func TestUpdate_NotFound(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	newTitle := "Doesn't matter"
	_, err := env.taskSvc.Update(ctx, domain.TaskUpdate{
		ShortID: "nonexist",
		Version: 1,
		Title:   &newTitle,
	})
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/service/ -run TestUpdate -v`

Expected: **compilation error** — `Update` method is not defined on `TaskService`.

- [ ] **Step 3: Commit**

```bash
git add internal/service/task_test.go
git commit -m "test(service): add failing TaskService.Update tests"
```

---

## Task 2: Implement `Update`

**Files:**
- Modify: `internal/service/task.go`

- [ ] **Step 1: Add the Update method**

Open `internal/service/task.go`. Find the last read method you added (`GetDescendants`). Add the `Update` method after it, but before `generateShortID`:

```go
// Update applies a partial update to a task. It validates the patched state,
// enforces workflow transitions, and uses optimistic locking.
// Returns the updated task with the new version number.
func (s *TaskService) Update(ctx context.Context, upd domain.TaskUpdate) (*domain.Task, error) {
	// Load current task
	task, err := s.taskRepo.GetByShortID(ctx, upd.ShortID)
	if err != nil {
		return nil, err
	}

	// Early version check
	if task.Version != upd.Version {
		return nil, domain.ErrConflict
	}

	oldStatus := task.Status

	// Apply patch
	if upd.Title != nil {
		task.Title = *upd.Title
	}
	if upd.Description != nil {
		task.Description = *upd.Description
	}
	if upd.Status != nil {
		task.Status = *upd.Status
	}
	if upd.Priority != nil {
		task.Priority = *upd.Priority
	}
	if upd.ParentID != nil {
		task.ParentID = *upd.ParentID
	}
	if upd.ProjectID != nil {
		task.ProjectID = *upd.ProjectID
	}
	if upd.DueAt != nil {
		task.DueAt = *upd.DueAt
	}
	if upd.WaitUntil != nil {
		task.WaitUntil = *upd.WaitUntil
	}
	if upd.RecurrenceRule != nil {
		task.RecurrenceRule = *upd.RecurrenceRule
	}
	if upd.UDA != nil {
		task.UDA = *upd.UDA
	}

	// Validate patched state
	if strings.TrimSpace(task.Title) == "" {
		return nil, fmt.Errorf("title must not be empty")
	}
	if task.Priority < 0 || task.Priority > 4 {
		return nil, fmt.Errorf("priority must be between 0 and 4")
	}

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
	}

	// Validate project if changed
	if upd.ProjectID != nil {
		if task.ProjectID == nil {
			return nil, fmt.Errorf("task must belong to a project")
		}
		_, err := s.projectRepo.GetByID(ctx, *task.ProjectID)
		if err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				return nil, fmt.Errorf("project not found: %w", err)
			}
			return nil, fmt.Errorf("looking up project: %w", err)
		}
	}

	// Workflow validation for status changes
	if task.Status != oldStatus {
		project, err := s.projectRepo.GetByID(ctx, *task.ProjectID)
		if err != nil {
			return nil, fmt.Errorf("looking up project for workflow: %w", err)
		}
		allowed, err := s.workflowSvc.IsTransitionAllowed(ctx, *task.ProjectID, project.DefaultWorkflow, oldStatus, task.Status)
		if err != nil {
			return nil, fmt.Errorf("checking transition: %w", err)
		}
		if !allowed {
			return nil, fmt.Errorf("transition %q → %q not allowed: %w", oldStatus, task.Status, domain.ErrInvalidTransition)
		}
	}

	// Update metadata
	task.ModifiedAt = time.Now().UTC().Truncate(time.Millisecond)

	// Persist (repo handles version increment)
	if err := s.taskRepo.Update(ctx, task); err != nil {
		return nil, err
	}

	// Re-read to get the persisted state with bumped version
	return s.taskRepo.GetByID(ctx, task.ID)
}
```

- [ ] **Step 2: Run the Update tests to verify they pass**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/service/ -run TestUpdate -v`

Expected output — all 9 tests PASS:

```
=== RUN   TestUpdate_PartialUpdate
--- PASS: TestUpdate_PartialUpdate
=== RUN   TestUpdate_VersionConflict
--- PASS: TestUpdate_VersionConflict
=== RUN   TestUpdate_StatusTransitionAllowed
--- PASS: TestUpdate_StatusTransitionAllowed
=== RUN   TestUpdate_StatusTransitionDisallowed
--- PASS: TestUpdate_StatusTransitionDisallowed
=== RUN   TestUpdate_EmptyTitleRejected
--- PASS: TestUpdate_EmptyTitleRejected
=== RUN   TestUpdate_InvalidPriorityRejected
--- PASS: TestUpdate_InvalidPriorityRejected
=== RUN   TestUpdate_ParentCannotBeSelf
--- PASS: TestUpdate_ParentCannotBeSelf
=== RUN   TestUpdate_ClearNullableField
--- PASS: TestUpdate_ClearNullableField
=== RUN   TestUpdate_NotFound
--- PASS: TestUpdate_NotFound
PASS
```

- [ ] **Step 3: Run the full service test suite**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/service/ -v`

Expected: all tests pass (5 workflow + 10 create + 7 read + 9 update = 31 tests).

- [ ] **Step 4: Commit**

```bash
git add internal/service/task.go
git commit -m "feat(service): implement TaskService.Update with partial updates and workflow validation"
```
