# Declarative Workflows — Phase 2: SQLite Cleanup, TaskTxProvider & Wiring

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Delete the SQLite workflow implementation, simplify `TaskTxProvider`, update TaskService propagation code, wire the in-memory repo in `main.go`, and clean the migration. After this phase, `go build ./...` passes and all tests are green.

**Architecture:** The SQLite workflow code is deleted. `TaskTxProvider` drops its `WorkflowRepository` parameter since the in-memory repo has no transactional state. Propagation code uses the service-level `workflowSvc`. `main.go` swaps to `inmem.NewWorkflowRepository`.

**Tech Stack:** Go, standard library only

**Prerequisites:** Phase 1 must be complete (domain types simplified, inmem repo created, transitional WorkflowService in place).

---

### Task 1: Delete SQLite workflow + simplify TaskTxProvider

Delete the old SQLite workflow code and update the transaction infrastructure. These must change together because `Tx.Workflows()` returns `*WorkflowRepo` (being deleted), and `Store.WithTaskTx` calls `Tx.Workflows()`.

**Files:**
- Delete: `internal/sqlite/workflow.go`
- Delete: `internal/sqlite/workflow_test.go`
- Modify: `internal/sqlite/store.go` (remove Tx.Workflows, simplify WithTaskTx)
- Modify: `internal/service/task.go` (TaskTxProvider interface)

- [ ] **Step 1: Delete SQLite workflow files**

```bash
rm internal/sqlite/workflow.go internal/sqlite/workflow_test.go
```

- [ ] **Step 2: Remove `Tx.Workflows()` from `internal/sqlite/store.go`**

Remove lines 93-94:

```go
// Workflows returns a WorkflowRepo operating within this transaction.
func (t *Tx) Workflows() *WorkflowRepo { return NewWorkflowRepo(t.tx) }
```

- [ ] **Step 3: Simplify `WithTaskTx` in `internal/sqlite/store.go`**

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

- [ ] **Step 4: Simplify `TaskTxProvider` in `internal/service/task.go`**

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

- [ ] **Step 5: Commit**

```bash
git add -u internal/sqlite/ internal/service/task.go
git commit -m "refactor: delete SQLite workflow code and simplify TaskTxProvider

Remove sqlite.WorkflowRepo, Tx.Workflows(), workflow DB tables.
Simplify TaskTxProvider to single TaskRepository param — in-memory
workflow repos have no transactional state."
```

---

### Task 2: Update TaskService propagation code + MCP + main.go

Update the `WithTaskTx` callback in `TaskService.Update`, the propagation function signatures, the MCP resource handler, and wire the inmem repo in `main.go`.

**Files:**
- Modify: `internal/service/task.go` (WithTaskTx callback, checkAutoComplete, checkAutoRevert)
- Modify: `internal/mcp/resources.go` (GetTransitions return type)
- Modify: `cmd/tusk/main.go` (wire inmem workflow repo)

- [ ] **Step 1: Update `WithTaskTx` callback in `TaskService.Update` (lines 271-288)**

Replace:

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

- [ ] **Step 2: Update `checkAutoComplete` signature and `IsTransitionAllowed` call**

Replace the signature (line 415-419):

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

Replace the `IsTransitionAllowed` call (line 473):

```go
		allowed, err := txWorkflowSvc.IsTransitionAllowed(ctx, parent.ProjectID, project.Workflow, parent.Status, cfg.TargetStatus)
```

With:

```go
		allowed, err := s.workflowSvc.IsTransitionAllowed(ctx, parent.ProjectID, project.Workflow, parent.Status, cfg.TargetStatus)
```

- [ ] **Step 3: Update `checkAutoRevert` signature and `IsTransitionAllowed` call**

Replace the signature (line 502-508):

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

Replace the `IsTransitionAllowed` call (line 554):

```go
		allowed, err := txWorkflowSvc.IsTransitionAllowed(ctx, parent.ProjectID, project.Workflow, parent.Status, revertCfg.TargetStatus)
```

With:

```go
		allowed, err := s.workflowSvc.IsTransitionAllowed(ctx, parent.ProjectID, project.Workflow, parent.Status, revertCfg.TargetStatus)
```

- [ ] **Step 4: Update `cmd/tusk/main.go` (line 58)**

Replace:

```go
	workflowRepo := sqlite.NewWorkflowRepo(db)
```

With:

```go
	workflowRepo := inmem.NewWorkflowRepository(cfg.Workflows)
```

The `inmem` package is already imported (line 10).

- [ ] **Step 5: Commit**

```bash
git add internal/service/task.go internal/mcp/resources.go cmd/tusk/main.go
git commit -m "refactor: update TaskService propagation, MCP, and wire inmem workflow repo

Simplify WithTaskTx callback, propagation functions use service-level
workflowSvc. Wire inmem.WorkflowRepository in main.go."
```

---

### Task 3: Update tests, clean migration, verify

Update `task_test.go` to use inmem workflow repo, clean the migration, and run the full test suite.

**Files:**
- Modify: `internal/service/task_test.go` (test setup)
- Modify: `migrations/001_initial.up.sql` (remove workflow tables/seed)
- Modify: `migrations/001_initial.down.sql` (remove workflow table drops)

- [ ] **Step 1: Update `testTaskEnvWithSettings` in `internal/service/task_test.go`**

Replace the function (lines 19-43):

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

	workflowSvc := NewWorkflowService(workflowRepo)
	taskSvc := NewTaskService(taskRepo, annotationRepo, projectRepo, workflowSvc, store)

	return &testEnv{
		taskSvc:     taskSvc,
		workflowSvc: workflowSvc,
		store:       store,
	}
}
```

Replace `testTaskEnv` (lines 54-78) with:

```go
func testTaskEnv(t *testing.T) *testEnv {
	t.Helper()
	return testTaskEnvWithSettings(t, config.ProjectSettingsConfig{})
}
```

- [ ] **Step 2: Remove workflow tables from `migrations/001_initial.up.sql`**

Remove everything from line 65 onwards (the workflow comment, `CREATE TABLE workflows`, `CREATE TABLE workflow_transitions`, and all seed `INSERT` statements). The file should end after:

```sql
CREATE INDEX idx_tag_assignments_tag ON tag_assignments(tag_id);
```

- [ ] **Step 3: Remove workflow drops from `migrations/001_initial.down.sql`**

Remove the first two lines:

```sql
DROP TABLE IF EXISTS workflow_transitions;
DROP TABLE IF EXISTS workflows;
```

The file should start with `DROP TABLE IF EXISTS tag_assignments;`.

- [ ] **Step 4: Run full test suite**

Run: `cd /Users/germanamz/projects/tusk && go build ./... && make test`

Expected: full compilation PASS, all tests PASS.

- [ ] **Step 5: Run with race detector**

Run: `cd /Users/germanamz/projects/tusk && make test-race`

Expected: PASS with no data races.

- [ ] **Step 6: Commit**

```bash
git add internal/service/task_test.go migrations/
git commit -m "refactor: update task tests for inmem workflow repo, clean migration

Use inmem.WorkflowRepository in test setup. Remove workflow tables
and seed data from 001_initial migration."
```
