# Parent-Child Task Creation & `tree` CLI Command

**Date:** 2026-04-03
**Roadmap:** v0.2 — Relations and hierarchy
**Status:** Approved

---

## Summary

Add circular parent-child detection to the service layer, implement the `tusk tree` CLI command with compact indented rendering, and cover the feature with E2E tests. Most infrastructure (parent_id schema, recursive CTE queries, filter parsing, `parent:` syntax in add/modify) already exists.

## Existing Infrastructure

| Component | Status | Location |
|-----------|--------|----------|
| `parent_id` field in domain/DB | Done | `domain/task.go:12`, `migrations/001_initial.up.sql:16` |
| Double-pointer `ParentID` in `TaskUpdate` | Done | `domain/task.go:39` |
| `GetChildren` / `GetDescendants` (repo + SQLite) | Done | `sqlite/task.go:137-169`, `service/task.go:136-144` |
| Recursive CTE for descendants | Done | `sqlite/task.go:155-161` |
| `parent:` and `tree:` filter parsing | Done | `filter/parser.go:12-13`, `filter/resolve.go:116-139` |
| `parent:` in `add` and `modify` commands | Done | `tui/commands.go:79-86, 284-298` |
| Parent existence validation | Done | `service/task.go:71-80, 208-214` |
| Self-reference prevention | Done | `service/task.go:205-206` |
| Circular parent detection | **Not done** | — |
| `tusk tree` command | **Not done** | Placeholder at `tui/tree.go` |
| Tree rendering | **Not done** | — |
| Parent/tree E2E tests | **Not done** | — |

## Design

### 1. Circular Parent Detection

**Location:** `internal/service/task.go` — `Create` and `Update` methods.

**New error:** `domain.ErrCyclicParent` in `internal/domain/errors.go`.

**Algorithm:** When a task's parent is being set, walk up the ancestor chain starting from the proposed parent. If the walk encounters the task being created/modified, reject with `ErrCyclicParent`.

```
detectParentCycle(ctx, taskID, proposedParentID):
    current = proposedParentID
    while current != nil:
        if current == taskID:
            return ErrCyclicParent
        parent = repo.GetByID(current)
        current = parent.ParentID
    return nil
```

**Characteristics:**
- O(depth) — practically shallow, spec says ~4 levels
- Uses existing `GetByID` — no new repo methods needed
- The existing self-reference check (`task.ParentID == task.ID`) becomes redundant but stays as a fast path
- Applies to both `Create` (when `task.ParentID` is non-nil) and `Update` (when `ParentID` double-pointer is non-nil with non-nil inner value)

### 2. `tusk tree` Command

#### Command Registration

New Cobra subcommand registered in `internal/tui/app.go`:

- `tusk tree` — full task hierarchy (all top-level tasks with descendants)
- `tusk tree <short_id>` — subtree rooted at the specified task
- `--all` flag to include deleted tasks (default: exclude deleted)

#### Data Fetching

**Full tree (`tusk tree`):**
Fetch all tasks with a single `List` call (filtered to exclude deleted unless `--all`), build the tree in memory by grouping on `parent_id`. This avoids N+1 queries.

**Rooted subtree (`tusk tree <short_id>`):**
`GetByShortID` for the root task, then `GetDescendants` for its descendants. Build tree in memory from the combined set.

#### Tree Building

In-memory tree construction in `internal/tui/tree.go`:

1. Index all tasks by ID into a map
2. Group tasks by parent_id
3. For full tree: iterate tasks where parent_id is NULL (or parent not in result set) as roots
4. For subtree: the specified task is the single root
5. Sort children at each level by urgency score (or creation date as fallback)

#### Rendering — Text Mode

Compact format with 2-space indentation per level:

```
a3f8b2c1 [active]  Implement auth middleware
  b7c9d4e2 [pending] Write unit tests
  c5e1f3a8 [pending] Write integration tests
    d2f4a6b8 [pending] Set up test fixtures
e9c1d3f5 [active]  Design API schema
```

Each line: `{indent}{short_id} [{status}] {title}`

Empty tree: print "No tasks." to stderr (consistent with `tusk list` empty behavior).

#### Rendering — JSON Mode

Nested structure with `children` arrays:

```json
[
  {
    "short_id": "a3f8b2c1",
    "title": "Implement auth middleware",
    "status": "active",
    "priority": 3,
    "parent_id": null,
    "children": [
      {
        "short_id": "b7c9d4e2",
        "title": "Write unit tests",
        "status": "pending",
        "priority": 0,
        "parent_id": "a3f8b2c1-...",
        "children": []
      }
    ]
  }
]
```

JSON output includes all task fields (same as `taskJSON` struct in `render.go`) plus a `children` field. Leaf tasks have `"children": []`.

### 3. E2E Tests

New file: `tests/e2e/hierarchy_test.go`

**Scenarios:**

1. **Create with parent** — `tusk add "Parent"`, then `tusk add "Child" parent:$0.short_id`, verify child's parent_id via `tusk info`
2. **Modify parent (set)** — Create two tasks, modify second to set parent to first, verify
3. **Modify parent (clear)** — Create parent+child, modify child with empty `parent:`, verify parent_id is cleared
4. **Tree full view** — Create parent with 2 children, run `tusk tree`, verify indented output contains all three
5. **Tree subtree view** — Create parent with child and grandchild, run `tusk tree <parent_short_id>`, verify subtree
6. **Tree empty** — Run `tusk tree` with no tasks, verify empty message
7. **Circular parent rejection** — Create A and B with B's parent=A, then try to modify A's parent to B, verify error
8. **Parent not found** — `tusk add "Task" parent:nonexist`, verify error

## Out of Scope

- Completion propagation (separate roadmap item)
- `tusk tag` subcommand (separate roadmap item)
- Interactive TUI tree (bubbletea — future roadmap)
- Sort flags for tree (use default urgency/creation order)

## Files to Create/Modify

| File | Action | Description |
|------|--------|-------------|
| `internal/domain/errors.go` | Modify | Add `ErrCyclicParent` sentinel |
| `internal/service/task.go` | Modify | Add `detectParentCycle` method, call from Create/Update |
| `internal/service/task_test.go` | Modify | Add cycle detection unit tests |
| `internal/tui/tree.go` | Rewrite | Tree building logic and rendering (text + JSON) |
| `internal/tui/app.go` | Modify | Register `tree` subcommand |
| `internal/tui/tree_test.go` | Create | Unit tests for tree building and rendering |
| `tests/e2e/hierarchy_test.go` | Create | E2E scenarios listed above |
