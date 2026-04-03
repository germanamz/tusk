# Completion Propagation — Phase 2: Transaction Coordination & Auto-Complete

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Introduce `TaskTxProvider` for atomic transaction coordination and implement the auto-complete propagation logic in `TaskService.Update()`.

**Architecture:** A new `TaskTxProvider` interface (following the existing `RelationTxProvider` pattern) lets `TaskService` run task updates and propagation checks in a single database transaction. When a status change occurs, `Update()` wraps the persist + propagation in `WithTaskTx`. The `checkAutoComplete` method walks up the parent chain, checking if all non-deleted siblings are at the trigger status, and auto-completes the parent if the workflow allows it.

**Tech Stack:** Go, SQLite

**Spec:** `docs/superpowers/specs/2026-04-03-completion-propagation-design.md`

**Prerequisite:** Phase 1 must be completed (domain types, migration, repo changes).

---

### Task 1: TaskTxProvider Interface and Store Implementation

**Files:**
- Modify: `internal/service/task.go` (add interface definition, update struct and constructor)
- Modify: `internal/sqlite/store.go` (add `WithTaskTx` method)
- Modify: `cmd/tusk/main.go` (pass `store` as `TaskTxProvider` to `NewTaskService`)

- [ ] **Step 1: Write a test that constructs TaskService with a TaskTxProvider**

Add a new test to `internal/service/task_test.go`. This test verifies the new constructor accepts a `TaskTxProvider` and that the service works as before (regression):

```go
func TestTaskService_WithTxProvider(t *testing.T) {
	store, err := sqlite.New(":memory:", migrations.FS)
	if err != nil {
		t.Fatalf("opening store: %v", err)
	}
	defer store.Close()

	db := store.DB()
	taskRepo := sqlite.NewTaskRepo(db)
	annotationRepo := sqlite.NewAnnotationRepo(db)
	projectRepo := sqlite.NewProjectRepo(db)
	workflowRepo := sqlite.NewWorkflowRepo(db)

	workflowSvc := NewWorkflowService(workflowRepo)
	// Pass store as the TaskTxProvider (5th argument)
	taskSvc := NewTaskService(taskRepo, annotationRepo, projectRepo, workflowSvc, store)

	ctx := context.Background()
	task := newMinimalTask("Test with tx provider")
	if err := taskSvc.Create(ctx, task); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Start and complete — basic lifecycle still works
	_, err = taskSvc.Start(ctx, task.ShortID, task.Version)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	started, _ := taskSvc.GetByShortID(ctx, task.ShortID)

	_, err = taskSvc.Complete(ctx, started.ShortID, started.Version)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -v ./internal/service -run TestTaskService_WithTxProvider`
Expected: FAIL — `NewTaskService` only accepts 4 arguments, not 5.

- [ ] **Step 3: Define TaskTxProvider interface and update TaskService**

Modify `internal/service/task.go`:

**3a. Add the `TaskTxProvider` interface** after the imports, before the `TaskService` struct:

```go
// TaskTxProvider gives TaskService a way to run task + project operations
// inside a database transaction for atomic propagation.
// The SQLite Store implements this via its WithTaskTx method.
type TaskTxProvider interface {
	WithTaskTx(ctx context.Context, fn func(tr repository.TaskRepository, pr repository.ProjectRepository) error) error
}
```

**3b. Add `txProvider` field to `TaskService`:**

```go
type TaskService struct {
	taskRepo       repository.TaskRepository
	annotationRepo repository.AnnotationRepository
	projectRepo    repository.ProjectRepository
	workflowSvc    *WorkflowService
	txProvider     TaskTxProvider
}
```

**3c. Update `NewTaskService` to accept the new parameter:**

```go
func NewTaskService(
	tr repository.TaskRepository,
	ar repository.AnnotationRepository,
	pr repository.ProjectRepository,
	ws *WorkflowService,
	txp TaskTxProvider,
) *TaskService {
	return &TaskService{
		taskRepo:       tr,
		annotationRepo: ar,
		projectRepo:    pr,
		workflowSvc:    ws,
		txProvider:     txp,
	}
}
```

- [ ] **Step 4: Add WithTaskTx to Store**

Modify `internal/sqlite/store.go`. Add this method after the existing `WithRelationTx`:

```go
// WithTaskTx executes fn with TaskRepository and ProjectRepository backed by
// a transaction. This is the concrete implementation of service.TaskTxProvider.
func (s *Store) WithTaskTx(ctx context.Context, fn func(tr repository.TaskRepository, pr repository.ProjectRepository) error) error {
	return s.WithTx(ctx, func(tx *Tx) error {
		return fn(tx.Tasks(), tx.Projects())
	})
}
```

- [ ] **Step 5: Fix all existing callers of NewTaskService**

**5a. Update `cmd/tusk/main.go`:**

Find the line:
```go
taskSvc := service.NewTaskService(taskRepo, annotationRepo, projectRepo, workflowSvc)
```
Replace with:
```go
taskSvc := service.NewTaskService(taskRepo, annotationRepo, projectRepo, workflowSvc, store)
```

**5b. Update `testTaskEnv` in `internal/service/task_test.go`:**

Find the line:
```go
taskSvc := NewTaskService(taskRepo, annotationRepo, projectRepo, workflowSvc)
```
Replace with:
```go
taskSvc := NewTaskService(taskRepo, annotationRepo, projectRepo, workflowSvc, store)
```

- [ ] **Step 6: Run tests to verify everything compiles and passes**

Run: `go test -v ./internal/service -run TestTaskService_WithTxProvider`
Expected: PASS

Then run: `make test`
Expected: PASS — all existing tests still work.

- [ ] **Step 7: Commit**

```bash
git add internal/service/task.go internal/service/task_test.go internal/sqlite/store.go cmd/tusk/main.go
git commit -m "feat(service): add TaskTxProvider interface for atomic propagation"
```

---

### Task 2: Wrap Status Changes in Transaction

**Files:**
- Modify: `internal/service/task.go` (refactor `Update()` to use transaction for status changes)

This task changes `Update()` so that when a status change occurs, the persist step runs inside `WithTaskTx`. No propagation logic yet — just the transactional wrapping.

- [ ] **Step 1: Write a test that verifies transactional update still works**

Add to `internal/service/task_test.go`:

```go
func TestUpdate_StatusChange_Transactional(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	task := newMinimalTask("Transactional test")
	mustCreateTask(t, env.taskSvc, task)

	// Start the task (pending -> active) — this triggers the transactional path
	updated, err := env.taskSvc.Start(ctx, task.ShortID, task.Version)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if updated.Status != "active" {
		t.Fatalf("expected status 'active', got %q", updated.Status)
	}
	if updated.Version != 2 {
		t.Fatalf("expected version 2, got %d", updated.Version)
	}

	// Complete it (active -> completed)
	completed, err := env.taskSvc.Complete(ctx, updated.ShortID, updated.Version)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if completed.Status != "completed" {
		t.Fatalf("expected status 'completed', got %q", completed.Status)
	}
	if completed.Version != 3 {
		t.Fatalf("expected version 3, got %d", completed.Version)
	}
}
```

- [ ] **Step 2: Run test to verify it passes (baseline)**

Run: `go test -v ./internal/service -run TestUpdate_StatusChange_Transactional`
Expected: PASS — this is testing existing behavior, it should pass before refactoring.

- [ ] **Step 3: Refactor Update() to use transaction for status changes**

Modify `internal/service/task.go`. Replace the persist + re-read section at the end of `Update()` (lines 253-262, the section after workflow validation). Currently it reads:

```go
	// Update metadata
	task.ModifiedAt = time.Now().UTC().Truncate(time.Millisecond)

	// Persist (repo handles version increment)
	if err := s.taskRepo.Update(ctx, task); err != nil {
		return nil, err
	}

	// Re-read to get the persisted state with bumped version
	return s.taskRepo.GetByID(ctx, task.ID)
```

Replace with:

```go
	// Update metadata
	task.ModifiedAt = time.Now().UTC().Truncate(time.Millisecond)

	statusChanged := task.Status != oldStatus

	// If status changed, wrap persist + propagation in a transaction.
	// Otherwise, persist directly (no transaction needed).
	if statusChanged && s.txProvider != nil {
		var result *domain.Task
		err := s.txProvider.WithTaskTx(ctx, func(txTaskRepo repository.TaskRepository, txProjectRepo repository.ProjectRepository) error {
			if err := txTaskRepo.Update(ctx, task); err != nil {
				return err
			}
			updated, err := txTaskRepo.GetByID(ctx, task.ID)
			if err != nil {
				return err
			}
			result = updated
			// Propagation will be added in the next task
			return nil
		})
		if err != nil {
			return nil, err
		}
		return result, nil
	}

	// Non-status-change path: persist directly
	if err := s.taskRepo.Update(ctx, task); err != nil {
		return nil, err
	}
	return s.taskRepo.GetByID(ctx, task.ID)
```

- [ ] **Step 4: Run the test again to verify the refactor works**

Run: `go test -v ./internal/service -run TestUpdate_StatusChange_Transactional`
Expected: PASS

- [ ] **Step 5: Run the full test suite**

Run: `make test`
Expected: PASS — all tests still pass with the transactional wrapping.

- [ ] **Step 6: Commit**

```bash
git add internal/service/task.go internal/service/task_test.go
git commit -m "refactor(service): wrap status changes in transaction for propagation"
```

---

### Task 3: Auto-Complete Propagation Logic

**Files:**
- Modify: `internal/service/task.go` (add `checkAutoComplete` method, wire into transaction)

- [ ] **Step 1: Write a test for auto-complete propagation**

Add to `internal/service/task_test.go`:

```go
func TestAutoComplete_AllChildrenCompleted(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	// Enable auto-complete on the default project
	projRepo := sqlite.NewProjectRepo(env.store.DB())
	proj, err := projRepo.GetByID(ctx, DefaultProjectID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	proj.Settings = domain.ProjectSettings{
		AutoCompleteParent: &domain.AutoCompleteConfig{
			TriggerStatus: "completed",
			TargetStatus:  "completed",
		},
	}
	if err := projRepo.Update(ctx, proj); err != nil {
		t.Fatalf("Update project: %v", err)
	}

	// Create parent
	parent := newMinimalTask("Parent")
	mustCreateTask(t, env.taskSvc, parent)
	// Start parent (pending -> active) so it can later transition to completed
	parent, err = env.taskSvc.Start(ctx, parent.ShortID, parent.Version)
	if err != nil {
		t.Fatalf("Start parent: %v", err)
	}

	// Create two children
	child1 := &domain.Task{Title: "Child 1", ParentID: &parent.ID}
	mustCreateTask(t, env.taskSvc, child1)
	child2 := &domain.Task{Title: "Child 2", ParentID: &parent.ID}
	mustCreateTask(t, env.taskSvc, child2)

	// Start and complete child1
	child1, err = env.taskSvc.Start(ctx, child1.ShortID, child1.Version)
	if err != nil {
		t.Fatalf("Start child1: %v", err)
	}
	child1, err = env.taskSvc.Complete(ctx, child1.ShortID, child1.Version)
	if err != nil {
		t.Fatalf("Complete child1: %v", err)
	}

	// Parent should NOT be auto-completed yet (child2 still pending)
	parentCheck, _ := env.taskSvc.GetByShortID(ctx, parent.ShortID)
	if parentCheck.Status != "active" {
		t.Fatalf("expected parent still 'active' after first child completed, got %q", parentCheck.Status)
	}

	// Start and complete child2
	child2, err = env.taskSvc.Start(ctx, child2.ShortID, child2.Version)
	if err != nil {
		t.Fatalf("Start child2: %v", err)
	}
	child2, err = env.taskSvc.Complete(ctx, child2.ShortID, child2.Version)
	if err != nil {
		t.Fatalf("Complete child2: %v", err)
	}

	// Parent SHOULD be auto-completed now
	parentCheck, _ = env.taskSvc.GetByShortID(ctx, parent.ShortID)
	if parentCheck.Status != "completed" {
		t.Fatalf("expected parent 'completed' after all children completed, got %q", parentCheck.Status)
	}
}

func TestAutoComplete_Disabled_ByDefault(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	// Do NOT enable auto-complete — default settings

	parent := newMinimalTask("Parent")
	mustCreateTask(t, env.taskSvc, parent)
	parent, err := env.taskSvc.Start(ctx, parent.ShortID, parent.Version)
	if err != nil {
		t.Fatalf("Start parent: %v", err)
	}

	child := &domain.Task{Title: "Child", ParentID: &parent.ID}
	mustCreateTask(t, env.taskSvc, child)
	child, err = env.taskSvc.Start(ctx, child.ShortID, child.Version)
	if err != nil {
		t.Fatalf("Start child: %v", err)
	}
	_, err = env.taskSvc.Complete(ctx, child.ShortID, child.Version)
	if err != nil {
		t.Fatalf("Complete child: %v", err)
	}

	// Parent should NOT be auto-completed (feature disabled)
	parentCheck, _ := env.taskSvc.GetByShortID(ctx, parent.ShortID)
	if parentCheck.Status != "active" {
		t.Fatalf("expected parent still 'active' (propagation disabled), got %q", parentCheck.Status)
	}
}

func TestAutoComplete_DeletedChildrenIgnored(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	// Enable auto-complete
	projRepo := sqlite.NewProjectRepo(env.store.DB())
	proj, _ := projRepo.GetByID(ctx, DefaultProjectID)
	proj.Settings = domain.ProjectSettings{
		AutoCompleteParent: &domain.AutoCompleteConfig{
			TriggerStatus: "completed",
			TargetStatus:  "completed",
		},
	}
	projRepo.Update(ctx, proj)

	parent := newMinimalTask("Parent")
	mustCreateTask(t, env.taskSvc, parent)
	parent, _ = env.taskSvc.Start(ctx, parent.ShortID, parent.Version)

	child1 := &domain.Task{Title: "Child 1", ParentID: &parent.ID}
	mustCreateTask(t, env.taskSvc, child1)
	child2 := &domain.Task{Title: "Child 2", ParentID: &parent.ID}
	mustCreateTask(t, env.taskSvc, child2)

	// Delete child2
	child2, _ = env.taskSvc.Delete(ctx, child2.ShortID, child2.Version)

	// Start and complete child1
	child1, _ = env.taskSvc.Start(ctx, child1.ShortID, child1.Version)
	child1, _ = env.taskSvc.Complete(ctx, child1.ShortID, child1.Version)

	// Parent should be auto-completed (deleted child ignored)
	parentCheck, _ := env.taskSvc.GetByShortID(ctx, parent.ShortID)
	if parentCheck.Status != "completed" {
		t.Fatalf("expected parent 'completed' (deleted child ignored), got %q", parentCheck.Status)
	}
}

func TestAutoComplete_WorkflowGuard(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	// Enable auto-complete
	projRepo := sqlite.NewProjectRepo(env.store.DB())
	proj, _ := projRepo.GetByID(ctx, DefaultProjectID)
	proj.Settings = domain.ProjectSettings{
		AutoCompleteParent: &domain.AutoCompleteConfig{
			TriggerStatus: "completed",
			TargetStatus:  "completed",
		},
	}
	projRepo.Update(ctx, proj)

	// Create parent but do NOT start it — leave in "pending"
	parent := newMinimalTask("Parent pending")
	mustCreateTask(t, env.taskSvc, parent)

	child := &domain.Task{Title: "Child", ParentID: &parent.ID}
	mustCreateTask(t, env.taskSvc, child)
	child, _ = env.taskSvc.Start(ctx, child.ShortID, child.Version)
	child, _ = env.taskSvc.Complete(ctx, child.ShortID, child.Version)

	// Parent should NOT be auto-completed (pending -> completed is not allowed)
	parentCheck, _ := env.taskSvc.GetByShortID(ctx, parent.ShortID)
	if parentCheck.Status != "pending" {
		t.Fatalf("expected parent still 'pending' (workflow blocks transition), got %q", parentCheck.Status)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -v ./internal/service -run "TestAutoComplete_"`
Expected: FAIL — `TestAutoComplete_AllChildrenCompleted` and `TestAutoComplete_DeletedChildrenIgnored` fail because no propagation logic exists yet. `TestAutoComplete_Disabled_ByDefault` and `TestAutoComplete_WorkflowGuard` should pass (they assert propagation does NOT happen).

- [ ] **Step 3: Implement checkAutoComplete**

Add the following method to `internal/service/task.go`:

```go
// checkAutoComplete checks whether completing a task should trigger automatic
// completion of its parent. If the task has a parent, all non-deleted siblings
// are at the trigger status, and the workflow allows the transition, the parent
// is auto-completed. This recurses up the ancestor chain.
func (s *TaskService) checkAutoComplete(
	ctx context.Context,
	task *domain.Task,
	txTaskRepo repository.TaskRepository,
	txProjectRepo repository.ProjectRepository,
) error {
	if task.ParentID == nil {
		return nil
	}

	parent, err := txTaskRepo.GetByID(ctx, *task.ParentID)
	if err != nil {
		return fmt.Errorf("loading parent for propagation: %w", err)
	}

	if parent.ProjectID == nil {
		return nil
	}

	project, err := txProjectRepo.GetByID(ctx, *parent.ProjectID)
	if err != nil {
		return fmt.Errorf("loading project for propagation: %w", err)
	}

	cfg := project.Settings.AutoCompleteParent
	if cfg == nil {
		return nil
	}

	// Check if the completed task reached the trigger status
	if task.Status != cfg.TriggerStatus {
		return nil
	}

	// Load all children of the parent
	children, err := txTaskRepo.GetChildren(ctx, parent.ID)
	if err != nil {
		return fmt.Errorf("loading siblings for propagation: %w", err)
	}

	// Check if all non-deleted children are at the trigger status
	for _, child := range children {
		if child.Status == "deleted" {
			continue
		}
		if child.Status != cfg.TriggerStatus {
			return nil // not all children ready
		}
	}

	// Validate workflow transition for the parent
	allowed, err := s.workflowSvc.IsTransitionAllowed(ctx, *parent.ProjectID, project.DefaultWorkflow, parent.Status, cfg.TargetStatus)
	if err != nil {
		return fmt.Errorf("checking propagation transition: %w", err)
	}
	if !allowed {
		return nil // workflow doesn't allow it — silently skip
	}

	// Auto-complete the parent
	parent.Status = cfg.TargetStatus
	parent.ModifiedAt = time.Now().UTC().Truncate(time.Millisecond)
	if err := txTaskRepo.Update(ctx, parent); err != nil {
		return fmt.Errorf("auto-completing parent: %w", err)
	}

	// Recurse up the ancestor chain
	updatedParent, err := txTaskRepo.GetByID(ctx, parent.ID)
	if err != nil {
		return fmt.Errorf("re-reading parent after propagation: %w", err)
	}
	return s.checkAutoComplete(ctx, updatedParent, txTaskRepo, txProjectRepo)
}
```

- [ ] **Step 4: Wire checkAutoComplete into the transaction in Update()**

In `internal/service/task.go`, find the comment `// Propagation will be added in the next task` inside the `WithTaskTx` callback. Replace:

```go
			// Propagation will be added in the next task
			return nil
```

with:

```go
			// Auto-complete propagation: check if parent should be auto-completed
			return s.checkAutoComplete(ctx, updated, txTaskRepo, txProjectRepo)
```

- [ ] **Step 5: Run the auto-complete tests**

Run: `go test -v ./internal/service -run "TestAutoComplete_"`
Expected: PASS — all four test cases pass.

- [ ] **Step 6: Run the full test suite**

Run: `make test`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/service/task.go internal/service/task_test.go
git commit -m "feat(service): add auto-complete parent propagation logic"
```

---

### Task 4: Recursive Auto-Complete Test

**Files:**
- Modify: `internal/service/task_test.go` (add recursive propagation test)

- [ ] **Step 1: Write a test for recursive propagation**

Add to `internal/service/task_test.go`:

```go
func TestAutoComplete_Recursive(t *testing.T) {
	env := testTaskEnv(t)
	ctx := context.Background()

	// Enable auto-complete
	projRepo := sqlite.NewProjectRepo(env.store.DB())
	proj, _ := projRepo.GetByID(ctx, DefaultProjectID)
	proj.Settings = domain.ProjectSettings{
		AutoCompleteParent: &domain.AutoCompleteConfig{
			TriggerStatus: "completed",
			TargetStatus:  "completed",
		},
	}
	projRepo.Update(ctx, proj)

	// Create grandparent -> parent -> child chain
	grandparent := newMinimalTask("Grandparent")
	mustCreateTask(t, env.taskSvc, grandparent)
	grandparent, _ = env.taskSvc.Start(ctx, grandparent.ShortID, grandparent.Version)

	parent := &domain.Task{Title: "Parent", ParentID: &grandparent.ID}
	mustCreateTask(t, env.taskSvc, parent)
	parent, _ = env.taskSvc.Start(ctx, parent.ShortID, parent.Version)

	child := &domain.Task{Title: "Child", ParentID: &parent.ID}
	mustCreateTask(t, env.taskSvc, child)
	child, _ = env.taskSvc.Start(ctx, child.ShortID, child.Version)

	// Complete child — should cascade: child done -> parent auto-done -> grandparent auto-done
	child, err := env.taskSvc.Complete(ctx, child.ShortID, child.Version)
	if err != nil {
		t.Fatalf("Complete child: %v", err)
	}

	parentCheck, _ := env.taskSvc.GetByShortID(ctx, parent.ShortID)
	if parentCheck.Status != "completed" {
		t.Fatalf("expected parent 'completed', got %q", parentCheck.Status)
	}

	grandparentCheck, _ := env.taskSvc.GetByShortID(ctx, grandparent.ShortID)
	if grandparentCheck.Status != "completed" {
		t.Fatalf("expected grandparent 'completed', got %q", grandparentCheck.Status)
	}
}
```

- [ ] **Step 2: Run the test**

Run: `go test -v ./internal/service -run TestAutoComplete_Recursive`
Expected: PASS — the recursive logic in `checkAutoComplete` handles this.

If this test fails, debug the issue (likely the re-read after parent update isn't getting the updated status within the transaction). Fix and re-run.

- [ ] **Step 3: Run the full test suite**

Run: `make test`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/service/task_test.go
git commit -m "test(service): add recursive auto-complete propagation test"
```
