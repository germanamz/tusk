# Completion Propagation Design

## Overview

Auto-transition parent tasks when all children reach a configurable trigger status.
Optionally auto-revert parents when a child moves away from the trigger status.
Both behaviors are opt-in per project, disabled by default.

---

## Data Model

### Migration: `002_project_settings.up.sql`

```sql
ALTER TABLE projects ADD COLUMN settings TEXT NOT NULL DEFAULT '{}';
```

### Domain Types

```go
type AutoCompleteConfig struct {
    TriggerStatus string `json:"trigger_status"` // child status that triggers check (e.g. "completed")
    TargetStatus  string `json:"target_status"`  // status to set on parent (e.g. "completed")
}

type AutoRevertConfig struct {
    TriggerStatus string `json:"trigger_status"` // child moving away from this triggers revert (e.g. "completed")
    TargetStatus  string `json:"target_status"`  // status to revert parent to (e.g. "active")
}

type ProjectSettings struct {
    AutoCompleteParent *AutoCompleteConfig `json:"auto_complete_parent,omitempty"`
    AutoRevertParent   *AutoRevertConfig   `json:"auto_revert_parent,omitempty"`
}
```

- `nil` = disabled (default)
- `Settings` field added to `domain.Project`, deserialized from the JSON column
- SQLite project repo updated to read/write the `settings` column

---

## Transaction Coordination

### `TaskTxProvider` Interface

```go
type TaskTxProvider interface {
    WithTaskTx(ctx context.Context, fn func(tr repository.TaskRepository, pr repository.ProjectRepository) error) error
}
```

Follows the existing `RelationTxProvider` pattern. Gives `TaskService` atomic access to both task and project repos within a single transaction.

### Store Implementation

```go
func (s *Store) WithTaskTx(ctx context.Context, fn func(tr repository.TaskRepository, pr repository.ProjectRepository) error) error {
    return s.WithTx(ctx, func(tx *Tx) error {
        return fn(tx.Tasks(), tx.Projects())
    })
}
```

### TaskService Changes

- Add `txProvider TaskTxProvider` field, injected via constructor
- Existing `taskRepo` and `projectRepo` fields remain for non-transactional reads

---

## Propagation Logic

### Auto-Complete (upward)

Fires when a task transitions **to** `AutoCompleteConfig.TriggerStatus`:

1. Check if the task has a `ParentID` -- if not, stop
2. Load the parent task
3. Load the parent's project settings -- if `AutoCompleteParent` is nil, stop
4. Load all children of the parent via `GetChildren(parentID)`
5. Filter out deleted children (`status == "deleted"`)
6. If all remaining children are at `TriggerStatus`:
   a. Validate the workflow transition (parent's current status -> `TargetStatus`)
   b. If transition not allowed, stop silently (no error)
   c. Update the parent (set status to `TargetStatus`, increment version)
7. Recurse: repeat from step 1 with the newly completed parent

### Auto-Revert (upward)

Fires when a task transitions **away from** `AutoRevertConfig.TriggerStatus`:

1. Check if the task has a `ParentID` -- if not, stop
2. Load the parent task -- if parent is not at `AutoCompleteConfig.TargetStatus` (i.e., wasn't auto-completed), stop
3. Load the parent's project settings -- if `AutoRevertParent` is nil, stop
4. Validate the workflow transition (parent's current status -> revert `TargetStatus`)
5. If allowed, update the parent to revert `TargetStatus`
6. Recurse upward

### Key Behaviors

- Both paths run inside the same transaction as the original update
- Workflow transitions are always respected -- propagation silently stops if invalid
- Deleted children are excluded from the "all at trigger status?" check
- Only one of auto-complete or auto-revert fires per update (mutually exclusive)
- No error is raised if propagation can't proceed

---

## Integration with `Update()`

### Flow

```
Update(ctx, upd)
  +-- validate fields, workflow transition (reads via non-tx repos)
  +-- txProvider.WithTaskTx(ctx, func(txTaskRepo, txProjectRepo) {
        +-- txTaskRepo.Update(task)
        +-- re-read task via txTaskRepo (get bumped version)
        +-- checkAutoComplete(ctx, task, txTaskRepo, txProjectRepo)
        +-- checkAutoRevert(ctx, task, oldStatus, txTaskRepo, txProjectRepo)
      })
```

- When no status change occurs: `Update()` works as today, no transaction wrapping
- When status changes: wraps persist + propagation in a single transaction
- `Complete()` and `Start()` remain thin wrappers -- they inherit propagation

---

## CLI

### No Changes to Existing Commands

Propagation is transparent. `tusk done`, `tusk modify`, and MCP tools that call
`Complete()`/`Update()` automatically benefit.

### New: `tusk project modify`

Required to configure propagation settings. Part of the new **Project Management**
initiative in v0.2 (separate story).

```
tusk project modify <name> --set auto_complete_parent.trigger_status=completed \
                           --set auto_complete_parent.target_status=completed
```

Dot-path `--set key=value` syntax for the JSON settings field.

---

## E2E Test Scenarios

1. **Propagation disabled (default):** Complete all children -> parent stays unchanged
2. **Auto-complete happy path:** Enable setting, create parent + 2 children, complete both -> parent auto-completes
3. **Partial completion:** Complete 1 of 2 children -> parent stays unchanged
4. **Deleted children ignored:** Create 3 children, delete one, complete other two -> parent auto-completes
5. **Workflow guard:** Parent is `pending`, trigger fires but `pending -> completed` not allowed -> parent stays `pending`
6. **Recursive propagation:** Grandparent -> parent -> child chain, complete child -> parent -> grandparent all auto-complete
7. **Auto-revert:** Re-open a child -> parent reverts to configured status
8. **Recursive revert:** Grandparent -> parent -> child, re-open child -> parent reverts -> grandparent reverts
9. **Custom trigger/target statuses:** Configure non-default statuses, verify propagation uses them

---

## Dependencies

- **Project Management initiative (v0.2):** `tusk project modify` command needed to configure settings via CLI
- Completion propagation can be implemented and tested independently (E2E tests can set project settings directly via DB setup), but full user-facing configurability depends on the project modify command

---

## Out of Scope

- Per-project workflow customization (already exists)
- Downward propagation (completing parent does not complete children)
- Notification/logging of auto-propagated transitions (future enhancement)
