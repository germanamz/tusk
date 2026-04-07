# Task Queue — Phase 1: Repository & Domain Foundation

**Prerequisites:** None beyond current codebase (v0.7 player claiming complete).

---

## Goal

Add the `CountBlockedByIncompleteTasks` repository method and the `ErrNoAvailableTasks` sentinel error. After this phase, the repository interface is extended, SQLite implements it, all test stubs compile, and existing tests still pass. No user-visible behavior changes.

---

## Tasks

### Task 1: Add `ErrNoAvailableTasks` sentinel error

**File:** `internal/domain/errors.go`

Add after the existing `ErrTaskClaimed` declaration (line 16):

```go
ErrNoAvailableTasks = errors.New("no available tasks")
```

### Task 2: Add `CountBlockedByIncompleteTasks` to `RelationRepository` interface

**File:** `internal/repository/relation.go`

Add to the `RelationRepository` interface after `CountBlockedByTasks` (line 19):

```go
CountBlockedByIncompleteTasks(ctx context.Context, taskIDs []uuid.UUID) (map[uuid.UUID]int, error)
```

Same signature as `CountBlockedByTasks` — returns a map of task ID to count of incomplete blockers.

### Task 3: Implement `CountBlockedByIncompleteTasks` in SQLite

**File:** `internal/sqlite/relation.go`

Add a new method on `RelationRepo`. Unlike `countRelationsByTasks` (line 160), this method needs a JOIN with the `tasks` table to filter by blocker status. Do NOT reuse `countRelationsByTasks` — write a standalone method.

SQL logic:
```sql
SELECT r.target_id, COUNT(*)
FROM relations r
JOIN tasks t ON r.source_id = t.id
WHERE r.target_id IN (?, ?, ...)
  AND r.relation_type = 'blocks'
  AND t.status NOT IN ('completed', 'deleted')
GROUP BY r.target_id
```

Return `map[uuid.UUID]int` — tasks not present in the map have zero incomplete blockers. Handle empty `taskIDs` slice by returning an empty map immediately (same pattern as `countRelationsByTasks`).

### Task 4: Add unit test for `CountBlockedByIncompleteTasks`

**File:** `internal/sqlite/relation_count_test.go`

Add a test following the pattern of existing tests in this file. Test scenarios:

1. Task A blocks Task B, A is `pending` → B has count 1
2. Task A blocks Task B, A is `completed` → B has count 0 (not in map)
3. Task A blocks Task B, A is `deleted` → B has count 0
4. Task A and Task C both block Task B, A is `completed`, C is `active` → B has count 1
5. Empty task IDs slice → empty map

### Task 5: Update test stub for `RelationRepository`

**File:** `internal/repository/repository_test.go`

Add stub method after `CountBlockedByTasks` (line 55):

```go
func (s *stubRelationRepo) CountBlockedByIncompleteTasks(_ context.Context, _ []uuid.UUID) (map[uuid.UUID]int, error) {
	return map[uuid.UUID]int{}, nil
}
```

This satisfies the interface so that existing service-layer tests compile.

---

## Verification

After this phase:
- `go build ./...` compiles cleanly
- `make test` passes (all existing tests + new relation count test)
- `make vet` passes
- No user-visible behavior changes — CLI and MCP operate identically

---

## Changes Introduced

| Type | Detail |
|------|--------|
| New sentinel error | `domain.ErrNoAvailableTasks` in `internal/domain/errors.go` |
| Modified interface | `RelationRepository` in `internal/repository/relation.go` — added `CountBlockedByIncompleteTasks` |
| New method | `RelationRepo.CountBlockedByIncompleteTasks` in `internal/sqlite/relation.go` |
| New test | `TestCountBlockedByIncompleteTasks` in `internal/sqlite/relation_count_test.go` |
| Updated stub | `stubRelationRepo.CountBlockedByIncompleteTasks` in `internal/repository/repository_test.go` |
| No bridge code | All additions are final implementations |
