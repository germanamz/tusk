# Declarative Workflows — Phase 2: Delete SQLite Workflow + Simplify TaskTxProvider

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Delete the now-dead SQLite workflow code, simplify `TaskTxProvider` to drop the `WorkflowRepository` parameter (no longer needed since workflows are in-memory), and clean the migration. Each task produces a compilable commit.

**Architecture:** Task 1 simplifies the transaction infrastructure first (making `Tx.Workflows()` dead code), Task 2 deletes the dead SQLite files, Task 3 cleans the migration. This ordering ensures each commit compiles.

**Tech Stack:** Go, standard library only

**Prerequisites:** Phase 1 must be complete (inmem repo wired, SQLite workflow code is dead code).

---

### Task 1: Simplify TaskTxProvider + propagation code

Remove `WorkflowRepository` from the `TaskTxProvider` callback since in-memory repos have no transactional state. Update propagation code to use the service-level `workflowSvc` instead of a transactional one. After this task, `Tx.Workflows()` and `sqlite/workflow.go` are dead code.

**Files:**
- Modify: `internal/service/task.go` (TaskTxProvider, WithTaskTx callback, checkAutoComplete, checkAutoRevert)
- Modify: `internal/sqlite/store.go` (WithTaskTx implementation)

- [ ] **Step 1: Simplify `TaskTxProvider` in `internal/service/task.go` (lines 19-24)**

Replace:

```go
type TaskTxProvider interface {
	WithTaskTx(ctx context.Context, fn func(tr repository.TaskRepository, wr repository.WorkflowRepository) error) error
}
```

With:

```go
type TaskTxProvider interface {
	WithTaskTx(ctx context.Context, fn func(tr repository.TaskRepository) error) error
}
```

- [ ] **Step 2: Update `WithTaskTx` callback + propagation calls in `TaskService.Update` (lines 271-288)**

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
			if err := s.checkAutoComplete(ctx, updated, txTaskRepo); err != nil {
				return err
			}
			return s.checkAutoRevert(ctx, updated, oldStatus, txTaskRepo)
		})
```

- [ ] **Step 3: Update `checkAutoComplete` (lines 415-419, 473)**

Remove `txWorkflowSvc` parameter from signature:

```go
func (s *TaskService) checkAutoComplete(
	ctx context.Context,
	task *domain.Task,
	txTaskRepo repository.TaskRepository,
) error {
```

Replace the `IsTransitionAllowed` call (line 473):

```go
		allowed, err := s.workflowSvc.IsTransitionAllowed(ctx, parent.ProjectID, project.Workflow, parent.Status, cfg.TargetStatus)
```

(Same call, just using `s.workflowSvc` instead of `txWorkflowSvc`.)

- [ ] **Step 4: Update `checkAutoRevert` (lines 502-508, 554)**

Remove `txWorkflowSvc` parameter from signature:

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
		allowed, err := s.workflowSvc.IsTransitionAllowed(ctx, parent.ProjectID, project.Workflow, parent.Status, revertCfg.TargetStatus)
```

(Same call, just using `s.workflowSvc` instead of `txWorkflowSvc`.)

- [ ] **Step 5: Simplify `Store.WithTaskTx` in `internal/sqlite/store.go` (lines 112-118)**

Replace:

```go
func (s *Store) WithTaskTx(ctx context.Context, fn func(tr repository.TaskRepository, wr repository.WorkflowRepository) error) error {
	return s.WithTx(ctx, func(tx *Tx) error {
		return fn(tx.Tasks(), tx.Workflows())
	})
}
```

With:

```go
func (s *Store) WithTaskTx(ctx context.Context, fn func(tr repository.TaskRepository) error) error {
	return s.WithTx(ctx, func(tx *Tx) error {
		return fn(tx.Tasks())
	})
}
```

- [ ] **Step 6: Verify compilation and tests**

Run: `cd /Users/germanamz/projects/tusk && go build ./... && make test`

Expected: PASS. `Tx.Workflows()` and `sqlite/workflow.go` are now dead code but still compile.

- [ ] **Step 7: Commit**

```bash
git add internal/service/task.go internal/sqlite/store.go
git commit -m "refactor: simplify TaskTxProvider to drop WorkflowRepository param

In-memory workflow repos have no transactional state. Propagation
code now uses service-level workflowSvc. Tx.Workflows() is dead code."
```

---

### Task 2: Delete SQLite workflow files

Remove the now-dead SQLite workflow implementation and `Tx.Workflows()`.

**Files:**
- Delete: `internal/sqlite/workflow.go`
- Delete: `internal/sqlite/workflow_test.go`
- Modify: `internal/sqlite/store.go` (remove Tx.Workflows)

- [ ] **Step 1: Delete files**

```bash
rm internal/sqlite/workflow.go internal/sqlite/workflow_test.go
```

- [ ] **Step 2: Remove `Tx.Workflows()` from `internal/sqlite/store.go` (lines 93-94)**

Remove:

```go
// Workflows returns a WorkflowRepo operating within this transaction.
func (t *Tx) Workflows() *WorkflowRepo { return NewWorkflowRepo(t.tx) }
```

- [ ] **Step 3: Verify compilation and tests**

Run: `cd /Users/germanamz/projects/tusk && go build ./... && make test`

Expected: PASS. Nothing references the deleted code.

- [ ] **Step 4: Commit**

```bash
git add -u internal/sqlite/
git commit -m "refactor(sqlite): delete dead workflow repo and Tx.Workflows()

No production or test code references these after the DI swap
to inmem.WorkflowRepository and TaskTxProvider simplification."
```

---

### Task 3: Clean migration + verify

Remove workflow tables and seed data from the migration.

**Files:**
- Modify: `migrations/001_initial.up.sql` (remove lines 65-96)
- Modify: `migrations/001_initial.down.sql` (remove first 2 lines)

- [ ] **Step 1: Clean `migrations/001_initial.up.sql`**

Remove everything from line 65 onwards: the comment, `CREATE TABLE workflows`, `CREATE TABLE workflow_transitions`, and all `INSERT` seed data. The file should end after:

```sql
CREATE INDEX idx_tag_assignments_tag ON tag_assignments(tag_id);
```

- [ ] **Step 2: Clean `migrations/001_initial.down.sql`**

Remove the first two lines:

```sql
DROP TABLE IF EXISTS workflow_transitions;
DROP TABLE IF EXISTS workflows;
```

The file should start with `DROP TABLE IF EXISTS tag_assignments;`.

- [ ] **Step 3: Run full test suite with race detector**

Run: `cd /Users/germanamz/projects/tusk && make test-race`

Expected: all PASS, no data races.

- [ ] **Step 4: Commit**

```bash
git add migrations/
git commit -m "refactor(migrations): remove workflow tables and seed data

Workflows are now config-driven in-memory entities.
No DB tables needed."
```
