# Config-based Projects — Phase 3: Task Domain & Storage Layer

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Change `Task.ProjectID` from `*uuid.UUID` to `string`, update the SQLite schema and task repository, update `TaskService` for string project IDs, and simplify the `WithTaskTx` callback to remove project/workflow repos from the transaction path.

**Architecture:** This is the big-bang domain change. `Task.ProjectID` becomes a required `string` that defaults to `"default"`. The SQLite schema drops the `projects` table and removes the FK constraint from `tasks.project_id`. The `WithTaskTx` callback no longer passes project or workflow repos (they're in-memory, not transactional).

**Tech Stack:** Go, SQLite

**Prerequisite:** Phase 2 (domain.Project rewrite, read-only repo, in-memory implementation, ProjectService rewrite) must be complete.

**Design spec:** `docs/superpowers/specs/2026-04-04-config-based-projects-design.md`

---

### Task 1: Change Task.ProjectID from *uuid.UUID to string

**Files:**
- Modify: `internal/domain/task.go`
- Modify: `internal/domain/filter.go`

- [ ] **Step 1: Update domain/task.go**

In `internal/domain/task.go`, make two changes:

**Change 1:** In the `Task` struct, replace:
```go
ProjectID      *uuid.UUID
```
with:
```go
ProjectID      string
```

**Change 2:** In the `TaskUpdate` struct, replace:
```go
ProjectID      **uuid.UUID
```
with:
```go
ProjectID      *string
```

After these changes, check if `uuid` is still imported (it's used by `ParentID *uuid.UUID` and `ID uuid.UUID`). It should still be needed — do NOT remove it.

- [ ] **Step 2: Update domain/filter.go**

In `internal/domain/filter.go`, replace:
```go
ProjectID   *uuid.UUID
```
with:
```go
ProjectID   *string
```

Check if `uuid` is still imported — it's used by `ParentID *uuid.UUID` and `RootID *uuid.UUID`, so it should remain.

- [ ] **Step 3: Verify the domain package compiles**

Run: `go build ./internal/domain/...`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/domain/task.go internal/domain/filter.go
git commit -m "feat(domain): change Task.ProjectID from *uuid.UUID to string"
```

---

### Task 2: Update SQLite schema and task repository

**Files:**
- Modify: `migrations/001_initial.up.sql`
- Delete: `migrations/002_project_settings.up.sql`
- Delete: `migrations/002_project_settings.down.sql`
- Delete: `migrations/003_project_version.up.sql`
- Delete: `migrations/003_project_version.down.sql`
- Modify: `internal/sqlite/task.go`

- [ ] **Step 1: Update the initial migration**

In `migrations/001_initial.up.sql`, make these changes:

**Remove** the `CREATE TABLE projects` block (lines 5-11) and its seed `INSERT INTO projects` (line 90-91).

**Remove** the `CREATE TABLE workflows` block (lines 73-79), the `CREATE TABLE workflow_transitions` block (lines 81-87), and all seed `INSERT INTO workflows` and `INSERT INTO workflow_transitions` statements (lines 93-105).

**Change** the `tasks.project_id` column from:
```sql
project_id TEXT REFERENCES projects(id) ON DELETE SET NULL,
```
to:
```sql
project_id TEXT NOT NULL DEFAULT 'default',
```

The final migration file should look like this:

```sql
PRAGMA journal_mode=WAL;
PRAGMA busy_timeout=5000;
PRAGMA foreign_keys=ON;

CREATE TABLE tasks (
    id TEXT PRIMARY KEY,
    short_id TEXT NOT NULL UNIQUE,
    parent_id TEXT REFERENCES tasks(id) ON DELETE SET NULL,
    project_id TEXT NOT NULL DEFAULT 'default',
    title TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'pending',
    priority INTEGER NOT NULL DEFAULT 0,
    version INTEGER NOT NULL DEFAULT 1,
    due_at TEXT,
    wait_until TEXT,
    recurrence_rule TEXT,
    uda TEXT DEFAULT '{}',
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    modified_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX idx_tasks_short_id ON tasks(short_id);
CREATE INDEX idx_tasks_parent_id ON tasks(parent_id);
CREATE INDEX idx_tasks_project_id ON tasks(project_id);
CREATE INDEX idx_tasks_status ON tasks(status);
CREATE INDEX idx_tasks_due_at ON tasks(due_at);
CREATE INDEX idx_tasks_wait_until ON tasks(wait_until);

CREATE TABLE annotations (
    id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    body TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX idx_annotations_task_id ON annotations(task_id);

CREATE TABLE relations (
    id TEXT PRIMARY KEY,
    source_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    target_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    relation_type TEXT NOT NULL CHECK (relation_type IN ('blocks', 'relates_to', 'duplicates')),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE(source_id, target_id, relation_type)
);

CREATE INDEX idx_relations_source ON relations(source_id);
CREATE INDEX idx_relations_target ON relations(target_id);

CREATE TABLE tags (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    color TEXT
);

CREATE TABLE tag_assignments (
    task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    tag_id TEXT NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
    PRIMARY KEY (task_id, tag_id)
);

CREATE INDEX idx_tag_assignments_tag ON tag_assignments(tag_id);
```

- [ ] **Step 2: Delete old migrations**

Delete these files:
- `migrations/002_project_settings.up.sql`
- `migrations/002_project_settings.down.sql`
- `migrations/003_project_version.up.sql`
- `migrations/003_project_version.down.sql`

- [ ] **Step 3: Update sqlite/task.go — scanTask function**

In the `scanTask` function (around line 290), change the project_id scanning from UUID parsing to plain string.

**Find** this block:
```go
	projectID  sql.NullString
```
**Replace with:**
```go
	projectID  string
```

**Find** the `Scan` call line that has `&projectID` — it stays the same.

**Find and remove** these lines:
```go
	t.ProjectID, err = parseUUID(projectID)
	if err != nil {
		return nil, fmt.Errorf("parsing project_id: %w", err)
	}
```
**Replace with:**
```go
	t.ProjectID = projectID
```

- [ ] **Step 4: Update sqlite/task.go — Create function**

In the `Create` function (around line 30), change:
```go
		nullableUUID(task.ParentID), nullableUUID(task.ProjectID),
```
to:
```go
		nullableUUID(task.ParentID), task.ProjectID,
```

- [ ] **Step 5: Update sqlite/task.go — Update function**

Find the `Update` method. In its SQL parameter list, wherever `task.ProjectID` is written as `nullableUUID(task.ProjectID)`, change it to just `task.ProjectID`.

If the update uses `TaskUpdate` directly (building SET clauses dynamically), find where `ProjectID` is handled. The old code likely does:
```go
if upd.ProjectID != nil {
    // ... set nullableUUID(*upd.ProjectID)
}
```

This pattern doesn't apply here because `TaskService.Update` applies the patch to the `Task` struct and then calls `taskRepo.Update(ctx, task)`. So the fix is in the Create/Update methods that write `task.ProjectID`.

Look for any place that wraps `task.ProjectID` in `nullableUUID()` and replace with the raw string value.

- [ ] **Step 6: Update sqlite/task.go — buildFilter function**

In the `buildFilter` function (around line 180), change:
```go
	if filter.ProjectID != nil {
		conditions = append(conditions, "project_id = ?")
		args = append(args, filter.ProjectID.String())
	}
```
to:
```go
	if filter.ProjectID != nil {
		conditions = append(conditions, "project_id = ?")
		args = append(args, *filter.ProjectID)
	}
```

- [ ] **Step 7: Verify sqlite package compiles**

Run: `go build ./internal/sqlite/...`

This may fail if `store.go` still references `ProjectRepo` — that's fixed in Task 3. Focus on ensuring `task.go` changes are correct.

- [ ] **Step 8: Commit**

```bash
git add migrations/ internal/sqlite/task.go
git commit -m "feat(sqlite): update schema and task repo for string project_id"
```

---

### Task 3: Update SQLite store.go and WithTaskTx signature

**Files:**
- Modify: `internal/sqlite/store.go`
- Delete: `internal/sqlite/project.go`
- Modify: `internal/service/task.go` (only the `TaskTxProvider` interface and `WithTaskTx` callback signature)

Since projects are now in-memory and read-only, they don't need to participate in database transactions. The `WithTaskTx` callback should only pass the transactional `TaskRepository`.

**Note:** The current `WithTaskTx` also passes `WorkflowRepository`. If the Declarative Workflows initiative has already removed it, only the `ProjectRepository` parameter needs removal. If it hasn't shipped yet, remove both `ProjectRepository` and `WorkflowRepository` from the callback. Adjust accordingly based on the current state of the code.

- [ ] **Step 1: Delete internal/sqlite/project.go**

Delete the entire file. It's no longer needed — projects are served by `internal/inmem/project.go`.

- [ ] **Step 2: Update store.go — remove Projects() from Tx**

In `internal/sqlite/store.go`, remove the `Projects()` method from the `Tx` struct:

**Remove:**
```go
// Projects returns a ProjectRepo operating within this transaction.
func (t *Tx) Projects() *ProjectRepo { return NewProjectRepo(t.tx) }
```

Also remove the `Workflows()` method if Declarative Workflows hasn't removed it yet:
```go
// Workflows returns a WorkflowRepo operating within this transaction.
func (t *Tx) Workflows() *WorkflowRepo { return NewWorkflowRepo(t.tx) }
```

- [ ] **Step 3: Update store.go — WithTaskTx signature**

Change the `WithTaskTx` method from:
```go
func (s *Store) WithTaskTx(ctx context.Context, fn func(tr repository.TaskRepository, pr repository.ProjectRepository, wr repository.WorkflowRepository) error) error {
	return s.WithTx(ctx, func(tx *Tx) error {
		return fn(tx.Tasks(), tx.Projects(), tx.Workflows())
	})
}
```
to:
```go
func (s *Store) WithTaskTx(ctx context.Context, fn func(tr repository.TaskRepository) error) error {
	return s.WithTx(ctx, func(tx *Tx) error {
		return fn(tx.Tasks())
	})
}
```

- [ ] **Step 4: Update TaskTxProvider interface in service/task.go**

In `internal/service/task.go`, update the `TaskTxProvider` interface from:
```go
type TaskTxProvider interface {
	WithTaskTx(ctx context.Context, fn func(tr repository.TaskRepository, pr repository.ProjectRepository, wr repository.WorkflowRepository) error) error
}
```
to:
```go
type TaskTxProvider interface {
	WithTaskTx(ctx context.Context, fn func(tr repository.TaskRepository) error) error
}
```

- [ ] **Step 5: Verify store.go compiles**

Run: `go build ./internal/sqlite/...`
Expected: PASS (after deleting project.go and updating store.go)

- [ ] **Step 6: Commit**

```bash
git add internal/sqlite/store.go internal/service/task.go
git rm internal/sqlite/project.go
git commit -m "feat(sqlite): remove project repo from Tx, simplify WithTaskTx callback"
```

---

### Task 4: Update TaskService for string project IDs

**Files:**
- Modify: `internal/service/task.go`

Update `TaskService` to work with string project IDs: change `DefaultProjectID` to a string constant, update `Create`, `Update`, and the completion propagation methods (`checkAutoComplete`, `checkAutoRevert`) to use the service's in-memory `projectRepo` directly instead of transactional repos.

- [ ] **Step 1: Update DefaultProjectID**

Replace:
```go
var DefaultProjectID = uuid.MustParse("00000000-0000-0000-0000-000000000000")
```
with:
```go
const DefaultProjectID = "default"
```

Remove the `uuid` import if it's no longer used in this file. Check: `uuid` is still used for `task.ID`, `task.ParentID`, etc. — keep it if so.

- [ ] **Step 2: Update TaskService.Create**

In the `Create` method, change the default project assignment from:
```go
	if task.ProjectID == nil {
		id := DefaultProjectID
		task.ProjectID = &id
	}
```
to:
```go
	if task.ProjectID == "" {
		task.ProjectID = DefaultProjectID
	}
```

Change the project lookup from:
```go
	project, err := s.projectRepo.GetByID(ctx, *task.ProjectID)
```
to:
```go
	project, err := s.projectRepo.GetByID(ctx, task.ProjectID)
```

Change the workflow validation from:
```go
	statuses, err := s.workflowSvc.GetStatuses(ctx, *task.ProjectID, project.DefaultWorkflow)
```
to:
```go
	statuses, err := s.workflowSvc.GetStatuses(ctx, task.ProjectID, project.Workflow)
```

Note: `project.DefaultWorkflow` was renamed to `project.Workflow` in the domain rewrite (Phase 2, Task 1). Also note that `WorkflowService.GetStatuses` currently takes `projectID uuid.UUID` — if Declarative Workflows has changed this signature, adjust accordingly. If it still takes a UUID, you'll need to update the `WorkflowService` method signatures too (the spec says workflows are standalone config entities referenced by name, so the `projectID` parameter should already be gone).

- [ ] **Step 3: Update TaskService.Update**

In the `Update` method:

**Project validation block** — change from:
```go
	if upd.ProjectID != nil {
		if task.ProjectID == nil {
			return nil, fmt.Errorf("task must belong to a project")
		}
		_, err := s.projectRepo.GetByID(ctx, *task.ProjectID)
```
to:
```go
	if upd.ProjectID != nil {
		task.ProjectID = *upd.ProjectID
		_, err := s.projectRepo.GetByID(ctx, task.ProjectID)
```

Wait — the current code applies the patch before validation (lines 192-193 `if upd.ProjectID != nil { task.ProjectID = *upd.ProjectID }`). Then later it validates. Look at the exact flow:

Lines 192-193 already apply the patch:
```go
	if upd.ProjectID != nil {
		task.ProjectID = *upd.ProjectID
	}
```

Then lines 235-246 validate:
```go
	if upd.ProjectID != nil {
		if task.ProjectID == nil {
			return nil, fmt.Errorf("task must belong to a project")
		}
		_, err := s.projectRepo.GetByID(ctx, *task.ProjectID)
```

Change the validation block to:
```go
	if upd.ProjectID != nil {
		if task.ProjectID == "" {
			return nil, fmt.Errorf("task must belong to a project")
		}
		_, err := s.projectRepo.GetByID(ctx, task.ProjectID)
		if err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				return nil, fmt.Errorf("project not found: %w", err)
			}
			return nil, fmt.Errorf("looking up project: %w", err)
		}
	}
```

**Workflow validation for status changes** — change from:
```go
		project, err := s.projectRepo.GetByID(ctx, *task.ProjectID)
		if err != nil {
			return nil, fmt.Errorf("looking up project for workflow: %w", err)
		}
		allowed, err := s.workflowSvc.IsTransitionAllowed(ctx, *task.ProjectID, project.DefaultWorkflow, oldStatus, task.Status)
```
to:
```go
		project, err := s.projectRepo.GetByID(ctx, task.ProjectID)
		if err != nil {
			return nil, fmt.Errorf("looking up project for workflow: %w", err)
		}
		allowed, err := s.workflowSvc.IsTransitionAllowed(ctx, task.ProjectID, project.Workflow, oldStatus, task.Status)
```

- [ ] **Step 4: Update WithTaskTx callback in Update method**

The status-change transaction block currently passes three repos. Change from:
```go
		err := s.txProvider.WithTaskTx(ctx, func(txTaskRepo repository.TaskRepository, txProjectRepo repository.ProjectRepository, txWorkflowRepo repository.WorkflowRepository) error {
			if err := txTaskRepo.Update(ctx, task); err != nil {
				return err
			}
			updated, err := txTaskRepo.GetByID(ctx, task.ID)
			if err != nil {
				return err
			}
			result = updated
			if err := s.checkAutoComplete(ctx, updated, txTaskRepo, txProjectRepo, txWorkflowRepo); err != nil {
				return err
			}
			return s.checkAutoRevert(ctx, updated, oldStatus, txTaskRepo, txProjectRepo, txWorkflowRepo)
		})
```
to:
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

- [ ] **Step 5: Update checkAutoComplete**

Change the method signature from:
```go
func (s *TaskService) checkAutoComplete(
	ctx context.Context,
	task *domain.Task,
	txTaskRepo repository.TaskRepository,
	txProjectRepo repository.ProjectRepository,
	txWorkflowRepo repository.WorkflowRepository,
) error {
```
to:
```go
func (s *TaskService) checkAutoComplete(
	ctx context.Context,
	task *domain.Task,
	txTaskRepo repository.TaskRepository,
) error {
```

Inside the method, replace all `txProjectRepo` references with `s.projectRepo` (the service-level in-memory project repo):

Change:
```go
		if parent.ProjectID == nil {
			return nil
		}

		project, err := txProjectRepo.GetByID(ctx, *parent.ProjectID)
```
to:
```go
		if parent.ProjectID == "" {
			return nil
		}

		project, err := s.projectRepo.GetByID(ctx, parent.ProjectID)
```

Replace all `txWorkflowRepo` references. Change:
```go
		txWorkflowSvc := NewWorkflowService(txWorkflowRepo)
		allowed, err := txWorkflowSvc.IsTransitionAllowed(ctx, *parent.ProjectID, project.DefaultWorkflow, parent.Status, cfg.TargetStatus)
```
to:
```go
		allowed, err := s.workflowSvc.IsTransitionAllowed(ctx, parent.ProjectID, project.Workflow, parent.Status, cfg.TargetStatus)
```

- [ ] **Step 6: Update checkAutoRevert**

Apply the same pattern as checkAutoComplete. Change the signature from:
```go
func (s *TaskService) checkAutoRevert(
	ctx context.Context,
	task *domain.Task,
	oldStatus string,
	txTaskRepo repository.TaskRepository,
	txProjectRepo repository.ProjectRepository,
	txWorkflowRepo repository.WorkflowRepository,
) error {
```
to:
```go
func (s *TaskService) checkAutoRevert(
	ctx context.Context,
	task *domain.Task,
	oldStatus string,
	txTaskRepo repository.TaskRepository,
) error {
```

Inside the method:

Change:
```go
		if parent.ProjectID == nil {
			return nil
		}

		project, err := txProjectRepo.GetByID(ctx, *parent.ProjectID)
```
to:
```go
		if parent.ProjectID == "" {
			return nil
		}

		project, err := s.projectRepo.GetByID(ctx, parent.ProjectID)
```

Change:
```go
		txWorkflowSvc := NewWorkflowService(txWorkflowRepo)
		allowed, err := txWorkflowSvc.IsTransitionAllowed(ctx, *parent.ProjectID, project.DefaultWorkflow, parent.Status, revertCfg.TargetStatus)
```
to:
```go
		allowed, err := s.workflowSvc.IsTransitionAllowed(ctx, parent.ProjectID, project.Workflow, parent.Status, revertCfg.TargetStatus)
```

- [ ] **Step 7: Clean up unused imports in task.go**

Check if `repository` is still imported (it is — used in `TaskTxProvider` and function params). Remove any imports that are no longer used.

- [ ] **Step 8: Verify the service package compiles**

Run: `go build ./internal/service/...`

This may still fail if `WorkflowService` method signatures haven't been updated. If `WorkflowService.GetStatuses` and `IsTransitionAllowed` still take `uuid.UUID` for projectID, you need to update them too. See the note in the design spec about Declarative Workflows being a prerequisite.

- [ ] **Step 9: Commit**

```bash
git add internal/service/task.go
git commit -m "feat(service): update TaskService for string project IDs and in-memory project repo"
```
