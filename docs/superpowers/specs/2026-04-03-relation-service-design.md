# RelationService with Cycle Detection — Design Spec

## Overview

Implement `RelationService` in the service layer with transactional cycle detection for `blocks` relations. Add `link`/`unlink` CLI commands and transaction support to the storage layer.

## Motivation

The domain types, repository interfaces, SQLite implementation, and database schema for relations already exist. What's missing is the service layer (business logic, cycle detection) and CLI commands. Cycle detection must be concurrency-safe because Tusk targets multi-agent MCP usage where parallel writes are common.

---

## 1. Transaction Support (`Store.WithTx`)

### Problem

Current repos each take `*sql.DB` in their constructor. There's no way to run operations across multiple repos in a single transaction. Cycle detection requires atomicity: the check and insert must happen within the same transaction to prevent concurrent writers from introducing cycles.

### DBTX Interface

Define a `DBTX` interface in the `sqlite` package:

```go
type DBTX interface {
    ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
    QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
    QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}
```

Both `*sql.DB` and `*sql.Tx` satisfy this. All repos already only use these three methods.

### Repo Constructor Change

Change all repo constructors from `NewXxxRepo(db *sql.DB)` to `NewXxxRepo(db DBTX)`. Internal field type changes from `*sql.DB` to `DBTX`. Non-breaking since `*sql.DB` implements `DBTX`.

### Tx Type and WithTx

```go
type Tx struct {
    tx *sql.Tx
}

func (t *Tx) Relations() *RelationRepo { return NewRelationRepo(t.tx) }
func (t *Tx) Tasks() *TaskRepo         { return NewTaskRepo(t.tx) }

func (s *Store) WithTx(ctx context.Context, fn func(tx *Tx) error) error {
    sqlTx, err := s.db.BeginTx(ctx, nil)
    if err != nil { return err }
    defer sqlTx.Rollback()
    if err := fn(&Tx{tx: sqlTx}); err != nil { return err }
    return sqlTx.Commit()
}
```

### Service-Layer Interface

To preserve the downward-only dependency rule, the service layer defines an abstract transaction provider rather than importing `sqlite`:

```go
// In service package
type RelationTxProvider interface {
    WithRelationTx(ctx context.Context, fn func(relationRepo repository.RelationRepository) error) error
}
```

The SQLite `Store` implements this:

```go
func (s *Store) WithRelationTx(ctx context.Context, fn func(rr repository.RelationRepository) error) error {
    return s.WithTx(ctx, func(tx *Tx) error {
        return fn(tx.Relations())
    })
}
```

---

## 2. RelationService Structure

### Dependencies

```go
type RelationService struct {
    relationRepo repository.RelationRepository
    taskRepo     repository.TaskRepository
    txProvider   RelationTxProvider
}

func NewRelationService(
    rr repository.RelationRepository,
    tr repository.TaskRepository,
    txp RelationTxProvider,
) *RelationService
```

- `relationRepo` — for non-transactional reads (`GetByTask`, `Remove`)
- `taskRepo` — to resolve short IDs to UUIDs
- `txProvider` — for atomic cycle-check + insert

### Methods

| Method | Signature | Description |
|--------|-----------|-------------|
| `Add` | `(ctx, sourceShortID, targetShortID, relType string) (*domain.Relation, error)` | Validate tasks exist, check cycles for `blocks`, create relation atomically |
| `Remove` | `(ctx, sourceShortID, targetShortID, relType string) error` | Look up and delete the relation |
| `GetByTask` | `(ctx, shortID string) ([]*domain.Relation, error)` | All relations involving a task |

`Add` and `Remove` take short IDs (CLI-friendly), resolve to UUIDs internally.

### Add Flow

1. Resolve source and target short IDs to tasks via `taskRepo.GetByShortID`
2. Validate `relType` is one of `blocks`, `relates_to`, `duplicates`
3. If `relType == "blocks"`, use `txProvider.WithRelationTx` to atomically:
   a. Run cycle detection (DFS from target)
   b. Insert the relation
4. If not `blocks`, insert directly via `relationRepo.Create` (no cycle concern)

### Remove Flow

1. Resolve source and target short IDs to tasks
2. Find the relation via `relationRepo.GetByTask` filtering by source, target, and type
3. Delete via `relationRepo.Delete`

---

## 3. Cycle Detection Algorithm

### When It Runs

Only for `blocks` relations. `relates_to` and `duplicates` don't imply dependency ordering and are safe to form cycles.

### Algorithm

DFS from the target task, following outgoing `blocks` edges. If we reach the source task, inserting source->target would create a cycle.

Adding `A blocks B`:
1. Start DFS from `B`
2. Follow all outgoing `blocks` edges from `B` (what does B block?)
3. If we reach `A`, reject with `ErrCyclicBlock`
4. If DFS exhausts without reaching `A`, safe to insert

Rationale for direction: the new edge is `A->B`. A cycle exists iff there's already a path `B->...->A`.

### Implementation

```go
func (s *RelationService) checkCycle(ctx context.Context, repo repository.RelationRepository, sourceID, targetID uuid.UUID) error {
    visited := map[uuid.UUID]bool{}
    stack := []uuid.UUID{targetID}
    for len(stack) > 0 {
        current := stack[len(stack)-1]
        stack = stack[:len(stack)-1]
        if current == sourceID {
            return domain.ErrCyclicBlock
        }
        if visited[current] {
            continue
        }
        visited[current] = true
        blocking, err := repo.GetBlocking(ctx, current)
        if err != nil {
            return fmt.Errorf("checking cycle: %w", err)
        }
        for _, rel := range blocking {
            if !visited[rel.TargetID] {
                stack = append(stack, rel.TargetID)
            }
        }
    }
    return nil
}
```

### Properties

- Runs inside the transaction — no concurrent writer can insert a conflicting edge between check and insert
- Uses the transactional `RelationRepository`, reading committed + in-flight state
- `O(V + E)` where V and E are reachable from the target in the `blocks` subgraph
- Self-referential edge (`A blocks A`) caught naturally: target == source on first iteration

---

## 4. CLI Commands

### `link`

```bash
tusk link <short_id> <relation_type> <short_id>
# e.g.: tusk link a3f8b2c1 blocks b7c9d4e2
```

- 3 positional args: source, relation type, target
- Validates relation type is one of `blocks`, `relates_to`, `duplicates`
- Calls `RelationService.Add`
- Text output on success: `Linked a3f8b2c1 blocks b7c9d4e2`
- JSON output mode returns the created `Relation` object
- Error cases: `ErrCyclicBlock`, `ErrDuplicateRelation`, `ErrNotFound`

### `unlink`

```bash
tusk unlink <short_id> <relation_type> <short_id>
# e.g.: tusk unlink a3f8b2c1 blocks b7c9d4e2
```

- Same arg pattern as `link`
- Calls `RelationService.Remove`
- Text output on success: `Unlinked a3f8b2c1 blocks b7c9d4e2`
- Error on not found: `Error: relation not found`

### `info` Enhancement

The existing `info` command should display a task's relations after the main fields:

```
Relations:
  blocks     b7c9d4e2  Implement auth middleware
  relates_to c5e1f3a8  Write API docs
  blocked_by d4e2f3a8  Set up CI pipeline
```

Inverse relations (`blocked_by`, `related_to`, `duplicated_by`) derived by checking whether the task is the source or target of each relation. Related task titles resolved via `TaskService`.

### Wiring

- `tui.App` gains a `relationSvc *service.RelationService` field
- `main.go` constructs `RelationService` with relation repo, task repo, and `Store` (as `RelationTxProvider`)
- `Store` passed to `tui.New` alongside the new service

---

## 5. Testing Strategy

### Unit Tests (`internal/service/relation_test.go`)

Real SQLite in-memory database with full migrations (matching existing service test patterns).

```go
type testRelationEnv struct {
    relationSvc *RelationService
    taskSvc     *TaskService
    store       *sqlite.Store
}
```

| Test | Verifies |
|------|----------|
| `TestRelationAdd` | Happy path: creates relation, returns populated struct |
| `TestRelationAddBlocks_NoCycle` | A->B, B->C allowed |
| `TestRelationAddBlocks_DirectCycle` | A->B then B->A rejected with `ErrCyclicBlock` |
| `TestRelationAddBlocks_TransitiveCycle` | A->B, B->C, then C->A rejected |
| `TestRelationAddBlocks_SelfReference` | A->A rejected with `ErrCyclicBlock` |
| `TestRelationAddDuplicate` | Same relation twice returns `ErrDuplicateRelation` |
| `TestRelationAddTaskNotFound` | Non-existent short ID returns `ErrNotFound` |
| `TestRelationAddInvalidType` | Bad relation type rejected |
| `TestRelationRemove` | Happy path: relation deleted |
| `TestRelationRemoveNotFound` | Non-existent relation returns `ErrNotFound` |
| `TestRelationGetByTask` | Returns both directions, derives inverse types |
| `TestRelationNonBlocksNoCycleCheck` | `relates_to` and `duplicates` allow bidirectional relations |

### E2E Tests (`tests/e2e/`)

Using the existing harness with `$N.short_id` references:

- `link_and_info` — create two tasks, link, verify in info output, unlink
- `link_cycle_detection` — create chain, attempt cycle, verify error
- `link_duplicate` — same link twice, verify error
- `link_not_found` — link with non-existent short ID
- `unlink_not_found` — unlink non-existent relation

---

## 6. File Change Summary

| Layer | File | Change |
|-------|------|--------|
| Storage | `internal/sqlite/store.go` | Add `DBTX` interface, `Tx` type, `WithTx`, `WithRelationTx` |
| Storage | `internal/sqlite/{task,relation,annotation,project,tag,workflow}.go` | Change `*sql.DB` field to `DBTX` |
| Service | `internal/service/relation.go` | `RelationService` with `Add`, `Remove`, `GetByTask`, `checkCycle`; `RelationTxProvider` interface |
| Service | `internal/service/relation_test.go` | Full unit test suite |
| TUI | `internal/tui/app.go` | Add `relationSvc` field, register `link`/`unlink` commands |
| TUI | `internal/tui/commands.go` | Implement `link`, `unlink`; enhance `info` with relations |
| Wiring | `cmd/tusk/main.go` | Construct `RelationService`, pass to `tui.New` |
| E2E | `tests/e2e/` | Link/unlink/cycle/error scenarios |

## Out of Scope

- MCP tools for relations (v0.3)
- Completion propagation (separate roadmap item)
- `tree` command (separate roadmap item)
- Parent-child task creation (separate roadmap item)
