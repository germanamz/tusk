# Declarative Workflows — Phase 2: Service Layer & TaskTxProvider

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Update `WorkflowService` to use name-only lookup, add new `List`/`GetByName`/`GetWorkflowWithProjects` methods, simplify `TaskTxProvider` to drop the workflow repo parameter, and update all `TaskService` call sites.

**Architecture:** `WorkflowService` gains a `projectRepo` dependency for the `GetWorkflowWithProjects` method. The `TaskTxProvider` signature is simplified because in-memory workflow repos have no transactional state. The propagation code in `TaskService` switches from creating a transactional `WorkflowService` to using the service-level one.

**Tech Stack:** Go, standard library only

**Prerequisites:** Phase 1 must be complete (domain types simplified, in-memory repo implemented, SQLite workflow code deleted).

---

### Task 1: Rewrite WorkflowService

Update `WorkflowService` to use the new `WorkflowRepository` interface (name-only lookup), add new methods, and add `projectRepo` dependency.

**Files:**
- Modify: `internal/service/workflow.go` (full rewrite, currently 61 lines)
- Modify: `internal/service/workflow_test.go` (full rewrite, currently 93 lines)

- [ ] **Step 1: Write the test file `internal/service/workflow_test.go`**

Replace the entire file with:

```go
package service

import (
	"context"
	"errors"
	"testing"

	"github.com/germanamz/tusk/internal/config"
	"github.com/germanamz/tusk/internal/domain"
	"github.com/germanamz/tusk/internal/inmem"
)

func testWorkflowEnv(t *testing.T) *WorkflowService {
	t.Helper()
	workflowRepo := inmem.NewWorkflowRepository(map[string]config.WorkflowConfig{
		"kanban": {
			Statuses: []string{"pending", "active", "completed", "deleted"},
			Transitions: []config.WorkflowTransitionConfig{
				{From: "pending", To: "active"},
				{From: "pending", To: "deleted"},
				{From: "active", To: "completed"},
				{From: "active", To: "pending"},
				{From: "active", To: "deleted"},
				{From: "completed", To: "pending"},
			},
		},
	})
	projectRepo := inmem.NewProjectRepository(map[string]config.ProjectConfig{
		"default": {Workflow: "kanban"},
		"backend": {Workflow: "kanban"},
	})
	return NewWorkflowService(workflowRepo, projectRepo)
}

func TestIsTransitionAllowed_Allowed(t *testing.T) {
	svc := testWorkflowEnv(t)
	ctx := context.Background()

	allowed, err := svc.IsTransitionAllowed(ctx, "kanban", "pending", "active")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !allowed {
		t.Fatal("expected pending->active to be allowed")
	}
}

func TestIsTransitionAllowed_Disallowed(t *testing.T) {
	svc := testWorkflowEnv(t)
	ctx := context.Background()

	allowed, err := svc.IsTransitionAllowed(ctx, "kanban", "pending", "completed")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if allowed {
		t.Fatal("expected pending->completed to be disallowed")
	}
}

func TestIsTransitionAllowed_WorkflowNotFound(t *testing.T) {
	svc := testWorkflowEnv(t)
	ctx := context.Background()

	_, err := svc.IsTransitionAllowed(ctx, "nonexistent", "pending", "active")
	if err == nil {
		t.Fatal("expected error for nonexistent workflow")
	}
}

func TestGetStatuses(t *testing.T) {
	svc := testWorkflowEnv(t)
	ctx := context.Background()

	statuses, err := svc.GetStatuses(ctx, "kanban")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := []string{"pending", "active", "completed", "deleted"}
	if len(statuses) != len(expected) {
		t.Fatalf("expected %d statuses, got %d", len(expected), len(statuses))
	}
	for i, s := range statuses {
		if s != expected[i] {
			t.Fatalf("status[%d]: expected %q, got %q", i, expected[i], s)
		}
	}
}

func TestGetStatuses_WorkflowNotFound(t *testing.T) {
	svc := testWorkflowEnv(t)
	ctx := context.Background()

	_, err := svc.GetStatuses(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent workflow")
	}
}

func TestGetTransitions(t *testing.T) {
	svc := testWorkflowEnv(t)
	ctx := context.Background()

	transitions, err := svc.GetTransitions(ctx, "kanban")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(transitions) != 6 {
		t.Fatalf("expected 6 transitions, got %d", len(transitions))
	}
}

func TestGetTransitions_WorkflowNotFound(t *testing.T) {
	svc := testWorkflowEnv(t)
	ctx := context.Background()

	_, err := svc.GetTransitions(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent workflow")
	}
}

func TestList(t *testing.T) {
	svc := testWorkflowEnv(t)
	ctx := context.Background()

	workflows, err := svc.List(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(workflows) != 1 {
		t.Fatalf("expected 1 workflow, got %d", len(workflows))
	}
	if workflows[0].Name != "kanban" {
		t.Fatalf("expected 'kanban', got %q", workflows[0].Name)
	}
}

func TestGetByName(t *testing.T) {
	svc := testWorkflowEnv(t)
	ctx := context.Background()

	wf, err := svc.GetByName(ctx, "kanban")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if wf.Name != "kanban" {
		t.Fatalf("expected 'kanban', got %q", wf.Name)
	}
}

func TestGetByName_NotFound(t *testing.T) {
	svc := testWorkflowEnv(t)
	ctx := context.Background()

	_, err := svc.GetByName(ctx, "nonexistent")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestGetWorkflowWithProjects(t *testing.T) {
	svc := testWorkflowEnv(t)
	ctx := context.Background()

	wf, projectIDs, err := svc.GetWorkflowWithProjects(ctx, "kanban")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if wf.Name != "kanban" {
		t.Fatalf("expected 'kanban', got %q", wf.Name)
	}
	if len(projectIDs) != 2 {
		t.Fatalf("expected 2 projects, got %d", len(projectIDs))
	}
	// Projects should be sorted (from List)
	if projectIDs[0] != "backend" || projectIDs[1] != "default" {
		t.Fatalf("expected [backend, default], got %v", projectIDs)
	}
}

func TestGetWorkflowWithProjects_NotFound(t *testing.T) {
	svc := testWorkflowEnv(t)
	ctx := context.Background()

	_, _, err := svc.GetWorkflowWithProjects(ctx, "nonexistent")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
```

- [ ] **Step 2: Rewrite `internal/service/workflow.go`**

Replace the entire file with:

```go
package service

import (
	"context"
	"fmt"

	"github.com/germanamz/tusk/internal/domain"
	"github.com/germanamz/tusk/internal/repository"
)

// WorkflowService validates status transitions and provides read access
// to workflow definitions from config.
type WorkflowService struct {
	workflowRepo repository.WorkflowRepository
	projectRepo  repository.ProjectRepository
}

// NewWorkflowService creates a new WorkflowService.
func NewWorkflowService(wr repository.WorkflowRepository, pr repository.ProjectRepository) *WorkflowService {
	return &WorkflowService{workflowRepo: wr, projectRepo: pr}
}

// IsTransitionAllowed checks whether a status transition is permitted
// by the named workflow.
func (s *WorkflowService) IsTransitionAllowed(ctx context.Context, workflowName string, from string, to string) (bool, error) {
	wf, err := s.workflowRepo.GetByName(ctx, workflowName)
	if err != nil {
		return false, fmt.Errorf("loading workflow %q: %w", workflowName, err)
	}

	for _, t := range wf.Transitions {
		if t.FromStatus == from && t.ToStatus == to {
			return true, nil
		}
	}
	return false, nil
}

// GetStatuses returns the ordered list of valid statuses for the named workflow.
func (s *WorkflowService) GetStatuses(ctx context.Context, workflowName string) ([]string, error) {
	wf, err := s.workflowRepo.GetByName(ctx, workflowName)
	if err != nil {
		return nil, fmt.Errorf("loading workflow %q: %w", workflowName, err)
	}
	return wf.Statuses, nil
}

// GetTransitions returns all allowed transitions for the named workflow.
func (s *WorkflowService) GetTransitions(ctx context.Context, workflowName string) ([]domain.WorkflowTransition, error) {
	wf, err := s.workflowRepo.GetByName(ctx, workflowName)
	if err != nil {
		return nil, fmt.Errorf("loading workflow %q: %w", workflowName, err)
	}
	return wf.Transitions, nil
}

// List returns all workflows from config.
func (s *WorkflowService) List(ctx context.Context) ([]*domain.Workflow, error) {
	return s.workflowRepo.List(ctx)
}

// GetByName returns a single workflow by name.
// Returns domain.ErrNotFound if the workflow does not exist.
func (s *WorkflowService) GetByName(ctx context.Context, name string) (*domain.Workflow, error) {
	return s.workflowRepo.GetByName(ctx, name)
}

// GetWorkflowWithProjects returns a workflow and the sorted list of project IDs
// that reference it. Returns domain.ErrNotFound if the workflow does not exist.
func (s *WorkflowService) GetWorkflowWithProjects(ctx context.Context, name string) (*domain.Workflow, []string, error) {
	wf, err := s.workflowRepo.GetByName(ctx, name)
	if err != nil {
		return nil, nil, err
	}

	projects, err := s.projectRepo.List(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("listing projects: %w", err)
	}

	var projectIDs []string
	for _, p := range projects {
		if p.Workflow == name {
			projectIDs = append(projectIDs, p.ID)
		}
	}
	return wf, projectIDs, nil
}
```

- [ ] **Step 3: Run workflow service tests**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/service/ -run "TestIsTransition|TestGetStatuses|TestGetTransitions|TestList|TestGetByName|TestGetWorkflowWithProjects" -v`

Expected: all PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/service/workflow.go internal/service/workflow_test.go
git commit -m "refactor(service): rewrite WorkflowService for config-driven workflows

Drop projectID from all method signatures, add List, GetByName,
and GetWorkflowWithProjects methods. Add projectRepo dependency
for project-workflow cross-referencing. Tests use inmem repos."
```

---

### Task 2: Simplify TaskTxProvider and Store.WithTaskTx

Remove `WorkflowRepository` from the `TaskTxProvider` callback signature and update `Store.WithTaskTx`.

**Files:**
- Modify: `internal/service/task.go:19-24` (TaskTxProvider interface)
- Modify: `internal/sqlite/store.go:112-118` (WithTaskTx implementation)

- [ ] **Step 1: Update `TaskTxProvider` in `internal/service/task.go`**

Replace lines 19-24:

```go
// TaskTxProvider gives TaskService a way to run task + project operations
// inside a database transaction for atomic propagation.
// The SQLite Store implements this via its WithTaskTx method.
type TaskTxProvider interface {
	WithTaskTx(ctx context.Context, fn func(tr repository.TaskRepository, wr repository.WorkflowRepository) error) error
}
```

With:

```go
// TaskTxProvider gives TaskService a way to run task operations
// inside a database transaction for atomic propagation.
// The SQLite Store implements this via its WithTaskTx method.
type TaskTxProvider interface {
	WithTaskTx(ctx context.Context, fn func(tr repository.TaskRepository) error) error
}
```

- [ ] **Step 2: Update `Store.WithTaskTx` in `internal/sqlite/store.go`**

Replace lines 112-118:

```go
// WithTaskTx executes fn with TaskRepository and WorkflowRepository backed by
// a transaction. This is the concrete implementation of service.TaskTxProvider.
func (s *Store) WithTaskTx(ctx context.Context, fn func(tr repository.TaskRepository, wr repository.WorkflowRepository) error) error {
	return s.WithTx(ctx, func(tx *Tx) error {
		return fn(tx.Tasks(), tx.Workflows())
	})
}
```

With:

```go
// WithTaskTx executes fn with a TaskRepository backed by a transaction.
// This is the concrete implementation of service.TaskTxProvider.
func (s *Store) WithTaskTx(ctx context.Context, fn func(tr repository.TaskRepository) error) error {
	return s.WithTx(ctx, func(tx *Tx) error {
		return fn(tx.Tasks())
	})
}
```

- [ ] **Step 3: Verify SQLite package compiles**

Run: `cd /Users/germanamz/projects/tusk && go build ./internal/sqlite/...`

Expected: PASS. The `Tx.Workflows()` method was already deleted in Phase 1.

- [ ] **Step 4: Commit**

```bash
git add internal/service/task.go internal/sqlite/store.go
git commit -m "refactor: simplify TaskTxProvider to drop WorkflowRepository param

In-memory workflow repos have no transactional state, so the callback
no longer needs a WorkflowRepository. Propagation code will use the
service-level WorkflowService instead."
```

---

### Task 3: Update TaskService call sites

Update all places in `TaskService` that call `workflowSvc` methods — drop the `projectID` parameter, and update the `WithTaskTx` callback to use the simplified signature.

**Files:**
- Modify: `internal/service/task.go` (multiple call sites)

- [ ] **Step 1: Update `Create` method — status validation (line 98)**

Replace:

```go
	statuses, err := s.workflowSvc.GetStatuses(ctx, task.ProjectID, project.Workflow)
```

With:

```go
	statuses, err := s.workflowSvc.GetStatuses(ctx, project.Workflow)
```

- [ ] **Step 2: Update `Update` method — transition validation (line 253)**

Replace:

```go
		allowed, err := s.workflowSvc.IsTransitionAllowed(ctx, task.ProjectID, project.Workflow, oldStatus, task.Status)
```

With:

```go
		allowed, err := s.workflowSvc.IsTransitionAllowed(ctx, project.Workflow, oldStatus, task.Status)
```

- [ ] **Step 3: Update `Update` method — WithTaskTx callback (lines 271-288)**

Replace the entire `WithTaskTx` block:

```go
		err := s.txProvider.WithTaskTx(ctx, func(txTaskRepo repository.TaskRepository, txWorkflowRepo repository.WorkflowRepository) error {
			if err := txTaskRepo.Update(ctx, task); err != nil {
				return err
			}
			updated, err := txTaskRepo.GetByID(ctx, task.ID)
			if err != nil {
				return err
			}
			result = updated
			txWorkflowSvc := NewWorkflowService(txWorkflowRepo)
			// Propagation: auto-complete and auto-revert are mutually exclusive
			// in practice — a single status change cannot simultaneously reach
			// and leave the trigger status — so at most one of these fires.
			if err := s.checkAutoComplete(ctx, updated, txTaskRepo, txWorkflowSvc); err != nil {
				return err
			}
			return s.checkAutoRevert(ctx, updated, oldStatus, txTaskRepo, txWorkflowSvc)
		})
```

With:

```go
		err := s.txProvider.WithTaskTx(ctx, func(txTaskRepo repository.TaskRepository) error {
			if err := txTaskRepo.Update(ctx, task); err != nil {
				return err
			}
			updated, err := txTaskRepo.GetByID(ctx, task.ID)
			if err != nil {
				return err
			}
			result = updated
			// Propagation: auto-complete and auto-revert are mutually exclusive
			// in practice — a single status change cannot simultaneously reach
			// and leave the trigger status — so at most one of these fires.
			if err := s.checkAutoComplete(ctx, updated, txTaskRepo); err != nil {
				return err
			}
			return s.checkAutoRevert(ctx, updated, oldStatus, txTaskRepo)
		})
```

- [ ] **Step 4: Update `checkAutoComplete` signature and body (lines 415-495)**

Replace the function signature:

```go
func (s *TaskService) checkAutoComplete(
	ctx context.Context,
	task *domain.Task,
	txTaskRepo repository.TaskRepository,
	txWorkflowSvc *WorkflowService,
) error {
```

With:

```go
func (s *TaskService) checkAutoComplete(
	ctx context.Context,
	task *domain.Task,
	txTaskRepo repository.TaskRepository,
) error {
```

Inside the function body, replace the `IsTransitionAllowed` call (line 473):

```go
		allowed, err := txWorkflowSvc.IsTransitionAllowed(ctx, parent.ProjectID, project.Workflow, parent.Status, cfg.TargetStatus)
```

With:

```go
		allowed, err := s.workflowSvc.IsTransitionAllowed(ctx, project.Workflow, parent.Status, cfg.TargetStatus)
```

- [ ] **Step 5: Update `checkAutoRevert` signature and body (lines 502-578)**

Replace the function signature:

```go
func (s *TaskService) checkAutoRevert(
	ctx context.Context,
	task *domain.Task,
	oldStatus string,
	txTaskRepo repository.TaskRepository,
	txWorkflowSvc *WorkflowService,
) error {
```

With:

```go
func (s *TaskService) checkAutoRevert(
	ctx context.Context,
	task *domain.Task,
	oldStatus string,
	txTaskRepo repository.TaskRepository,
) error {
```

Inside the function body, replace the `IsTransitionAllowed` call (line 554):

```go
		allowed, err := txWorkflowSvc.IsTransitionAllowed(ctx, parent.ProjectID, project.Workflow, parent.Status, revertCfg.TargetStatus)
```

With:

```go
		allowed, err := s.workflowSvc.IsTransitionAllowed(ctx, project.Workflow, parent.Status, revertCfg.TargetStatus)
```

- [ ] **Step 6: Remove unused import**

After these changes, `internal/service/task.go` should no longer need the `repository` import for the `WithTaskTx` callback parameter type. Check if `repository` is still used elsewhere in the file (it is — `repository.TaskRepository` is still referenced in `checkAutoComplete` and `checkAutoRevert`). So keep the import.

- [ ] **Step 7: Verify the service package compiles**

Run: `cd /Users/germanamz/projects/tusk && go build ./internal/service/...`

Expected: PASS (or compilation errors in `task_test.go` due to the old `NewWorkflowService` call signature — that's fixed in the next step).

- [ ] **Step 8: Commit**

```bash
git add internal/service/task.go
git commit -m "refactor(service): update TaskService for simplified workflow interface

Drop projectID from workflowSvc calls, simplify WithTaskTx callback
to single TaskRepository param, use service-level workflowSvc in
propagation code instead of transactional one."
```

---

### Task 4: Update TaskService tests

Update `internal/service/task_test.go` to use the new `NewWorkflowService` signature and in-memory workflow repo.

**Files:**
- Modify: `internal/service/task_test.go:16-78` (test environment setup)

- [ ] **Step 1: Update `testTaskEnvWithSettings` (lines 19-43)**

Replace:

```go
func testTaskEnvWithSettings(t *testing.T, settings config.ProjectSettingsConfig) *testEnv {
	t.Helper()
	store, err := sqlite.New(":memory:", migrations.FS)
	if err != nil {
		t.Fatalf("opening test store: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	db := store.DB()
	taskRepo := sqlite.NewTaskRepo(db)
	annotationRepo := sqlite.NewAnnotationRepo(db)
	projectRepo := inmem.NewProjectRepository(map[string]config.ProjectConfig{
		"default": {Workflow: "kanban", Settings: settings},
	})
	workflowRepo := sqlite.NewWorkflowRepo(db)

	workflowSvc := NewWorkflowService(workflowRepo)
	taskSvc := NewTaskService(taskRepo, annotationRepo, projectRepo, workflowSvc, store)

	return &testEnv{
		taskSvc:     taskSvc,
		workflowSvc: workflowSvc,
		store:       store,
	}
}
```

With:

```go
func testTaskEnvWithSettings(t *testing.T, settings config.ProjectSettingsConfig) *testEnv {
	t.Helper()
	store, err := sqlite.New(":memory:", migrations.FS)
	if err != nil {
		t.Fatalf("opening test store: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	db := store.DB()
	taskRepo := sqlite.NewTaskRepo(db)
	annotationRepo := sqlite.NewAnnotationRepo(db)
	projectRepo := inmem.NewProjectRepository(map[string]config.ProjectConfig{
		"default": {Workflow: "kanban", Settings: settings},
	})
	workflowRepo := inmem.NewWorkflowRepository(map[string]config.WorkflowConfig{
		"kanban": {
			Statuses: []string{"pending", "active", "completed", "deleted"},
			Transitions: []config.WorkflowTransitionConfig{
				{From: "pending", To: "active"},
				{From: "pending", To: "deleted"},
				{From: "active", To: "completed"},
				{From: "active", To: "pending"},
				{From: "active", To: "deleted"},
				{From: "completed", To: "pending"},
			},
		},
	})

	workflowSvc := NewWorkflowService(workflowRepo, projectRepo)
	taskSvc := NewTaskService(taskRepo, annotationRepo, projectRepo, workflowSvc, store)

	return &testEnv{
		taskSvc:     taskSvc,
		workflowSvc: workflowSvc,
		store:       store,
	}
}
```

- [ ] **Step 2: Update `testTaskEnv` (lines 54-78)**

Replace:

```go
func testTaskEnv(t *testing.T) *testEnv {
	t.Helper()
	store, err := sqlite.New(":memory:", migrations.FS)
	if err != nil {
		t.Fatalf("opening test store: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	db := store.DB()
	taskRepo := sqlite.NewTaskRepo(db)
	annotationRepo := sqlite.NewAnnotationRepo(db)
	projectRepo := inmem.NewProjectRepository(map[string]config.ProjectConfig{
		"default": {Workflow: "kanban"},
	})
	workflowRepo := sqlite.NewWorkflowRepo(db)

	workflowSvc := NewWorkflowService(workflowRepo)
	taskSvc := NewTaskService(taskRepo, annotationRepo, projectRepo, workflowSvc, store)

	return &testEnv{
		taskSvc:     taskSvc,
		workflowSvc: workflowSvc,
		store:       store,
	}
}
```

With:

```go
func testTaskEnv(t *testing.T) *testEnv {
	t.Helper()
	return testTaskEnvWithSettings(t, config.ProjectSettingsConfig{})
}
```

- [ ] **Step 3: Run all service tests**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/service/ -v`

Expected: all PASS.

- [ ] **Step 4: Run full test suite**

Run: `cd /Users/germanamz/projects/tusk && make test`

Expected: all unit tests PASS. E2E tests may fail if `main.go` DI wiring hasn't been updated yet — that's Phase 3.

- [ ] **Step 5: Commit**

```bash
git add internal/service/task_test.go
git commit -m "test(service): update TaskService tests for in-memory workflow repo

Replace sqlite.WorkflowRepo with inmem.WorkflowRepository in test
setup. Simplify testTaskEnv to delegate to testTaskEnvWithSettings."
```
