# Phase 6: AnnotationRepo & RelationRepo — SQLite Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement two SQLite repositories — `AnnotationRepo` (simple CRUD, 3 methods) and `RelationRepo` (directional relationship queries, 6 methods with duplicate detection). Both satisfy their corresponding interfaces from `internal/repository/`.

**Prerequisites:** Phases 1-5 must be complete. You need:
- `internal/sqlite/store.go` — `Store`, `const timeFormat`, helper functions
- `internal/sqlite/store_test.go` — `testStore(t)`, `mustTimeNow()`
- `internal/sqlite/task_test.go` — `newTestTask()`, `mustCreateTask(t, repo, task)`, `NewTaskRepo(db *sql.DB)`

**What you'll learn:** ON DELETE CASCADE behavior, UNIQUE constraint violation detection, directed vs undirected graph queries, the EXISTS SQL subquery pattern, shared row-scanning helpers

**Estimated effort:** 1-1.5 hours

---

## Context

### What is Tusk?

Tusk is a concurrent-safe task management tool written in Go. It stores data in SQLite. The architecture has four layers:

1. **Domain** (`internal/domain/`) — pure data types and sentinel errors. No dependencies beyond stdlib and `uuid`.
2. **Repository** (`internal/repository/`) — Go interfaces that define what storage operations exist. No implementation details.
3. **SQLite** (`internal/sqlite/`) — concrete implementations of the repository interfaces using SQLite. This is the layer we are building.
4. **Service** and **CLI/MCP** — business logic and user-facing layers (built in later phases).

### What Prior Phases Produced

Here is what already exists that this phase depends on:

- **Phase 1 — `internal/sqlite/store.go`**: The `Store` struct that opens a SQLite database, runs migrations, and exposes `DB() *sql.DB`. Also defines `const timeFormat` (the time layout string `"2006-01-02T15:04:05.000Z"`) used to convert Go `time.Time` values to and from SQLite TEXT columns.
- **Phase 1 — `internal/sqlite/store_test.go`**: Test helpers shared by all `_test.go` files in the `sqlite` package:
  - `testStore(t *testing.T) *Store` — creates an in-memory SQLite database with all migrations applied and registers `t.Cleanup()` to close it.
  - `mustTimeNow() time.Time` — returns `time.Now().UTC().Truncate(time.Millisecond)` for consistent test precision.
- **Phase 3 — `internal/sqlite/task.go`**: `NewTaskRepo(db *sql.DB)` creates a `TaskRepo` for inserting tasks.
- **Phase 3 — `internal/sqlite/task_test.go`**: Helper functions we will reuse:
  - `newTestTask()` — returns a fresh `*domain.Task` with all required fields populated. We need this because annotations and relations have foreign keys pointing to tasks.
  - `mustCreateTask(t, repo, task)` — inserts a task and calls `t.Fatal` on error. We use this to set up the prerequisite tasks before testing annotations and relations.

### What This Phase Builds

This phase creates four files:

1. `internal/sqlite/annotation.go` — The `AnnotationRepo` struct implementing `repository.AnnotationRepository` (3 methods: Create, GetByTask, Delete).
2. `internal/sqlite/annotation_test.go` — Tests for all 3 AnnotationRepo methods.
3. `internal/sqlite/relation.go` — The `RelationRepo` struct implementing `repository.RelationRepository` (6 methods: Create, Delete, GetByTask, GetBlocking, GetBlockedBy, Exists). This also includes a private `isUniqueViolation` helper and a shared `scanRelations` helper.
4. `internal/sqlite/relation_test.go` — Tests for all 6 RelationRepo methods plus duplicate detection.

### How the Pieces Wire Together

```
Store (store.go)                 AnnotationRepo / RelationRepo
  |                                   |
  | .DB() returns *sql.DB             | both take *sql.DB in constructor
  |-----------------------------------+
  |
  | timeFormat constant               | used in Create (formatting) and
  |-----------------------------------| scan functions (parsing)
  |
  | testStore (store_test.go)         | used in all test files to get
  |-----------------------------------| a ready-to-use database
  |
  | newTestTask / mustCreateTask      | used in annotation_test.go and
  | (task_test.go)                    | relation_test.go to create the
  |-----------------------------------| parent tasks that annotations
                                      | and relations attach to
```

Both annotations and relations depend on tasks. In the database, they have foreign keys (`task_id`, `source_id`, `target_id`) that reference the `tasks` table. Before you can insert an annotation or a relation, the referenced task must exist. That is why every test function starts by creating one or more tasks using `newTestTask()` and `mustCreateTask()`.

---

## Key Concepts

Before you start coding, read through these concepts. They explain *why* the code is written the way it is.

### 1. ON DELETE CASCADE — What It Means

Look at the annotations table schema:

```sql
CREATE TABLE annotations (
    id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    ...
);
```

The `REFERENCES tasks(id)` part creates a **foreign key**. This tells SQLite: "the value in `task_id` must be the `id` of an existing row in the `tasks` table." If you try to insert an annotation with a `task_id` that does not exist in `tasks`, SQLite will reject the insert with a foreign key violation error.

The `ON DELETE CASCADE` part tells SQLite what to do when a referenced task is deleted. "CASCADE" means: **automatically delete all annotations that belong to that task**. Without this, deleting a task would fail (or leave orphaned annotations) because the foreign key would be violated.

The same applies to the `relations` table, which has TWO foreign keys:

```sql
source_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
target_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
```

If either the source task or the target task is deleted, all relations involving that task are automatically removed. This keeps the database consistent without requiring our Go code to manually delete relations before deleting a task.

**Why this matters for testing:** We do NOT need to test CASCADE behavior in our annotation or relation tests. The database handles it. But we DO need to create valid tasks before creating annotations/relations, or the foreign key check will reject our inserts.

### 2. UNIQUE Constraints — How They Prevent Duplicates

The relations table has this constraint:

```sql
UNIQUE(source_id, target_id, relation_type)
```

This is a **composite unique constraint** on three columns together. It means: "no two rows can have the same combination of source_id, target_id, AND relation_type." For example:

- Task A "blocks" Task B — OK, inserted.
- Task A "blocks" Task B — REJECTED, this exact combination already exists.
- Task A "relates_to" Task B — OK, different `relation_type`.
- Task B "blocks" Task A — OK, different `source_id`/`target_id` (direction matters!).

This prevents accidental duplicate relations at the database level. Even if our Go code has a bug and tries to insert the same relation twice, the database will reject the second insert. This is a safety net.

### 3. Detecting UNIQUE Violations in Go

When SQLite rejects an insert due to a UNIQUE constraint, it returns an error. But Go's `database/sql` package does not have a typed error for constraint violations like some ORMs do. The error comes back as a generic error whose message contains the text `"UNIQUE constraint failed"`.

We detect this by checking the error message string:

```go
func isUniqueViolation(err error) bool {
    return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}
```

This is a pragmatic approach. It is not the most elegant — checking error strings is fragile if the SQLite driver changes its error messages. But in practice, this message has been stable across all Go SQLite drivers for years. The alternative (using driver-specific error types) would couple our code to a specific SQLite driver.

In the `Create` method, we use this helper to translate the raw SQLite error into our domain error:

```go
if err != nil && isUniqueViolation(err) {
    return domain.ErrDuplicateRelation
}
return err
```

This means callers of `RelationRepo.Create` never see SQLite-specific errors — they see `domain.ErrDuplicateRelation`, which is meaningful in the business logic layer.

**Important:** `isUniqueViolation` is a **private** (lowercase) function. It is only visible within the `sqlite` package. This is intentional — it is an implementation detail of how we talk to SQLite, not something the rest of the application should know about.

### 4. Directed vs. Undirected Relations

Relations in Tusk are **directed** — they have a `source_id` and a `target_id`, like an arrow:

```
source_id  ──relation_type──>  target_id
Task A     ──blocks──>         Task B
```

This directionality affects how we query:

- **GetByTask(taskID)** — returns ALL relations where the task is involved, regardless of direction. The SQL uses `OR`:
  ```sql
  WHERE source_id = ? OR target_id = ?
  ```
  This gives you the complete picture: everything the task blocks AND everything that blocks the task, plus all other relation types.

- **GetBlocking(taskID)** — returns relations where THIS task is the SOURCE and the type is `'blocks'`. Think of it as "what is this task blocking?":
  ```sql
  WHERE source_id = ? AND relation_type = 'blocks'
  ```

- **GetBlockedBy(taskID)** — returns relations where THIS task is the TARGET and the type is `'blocks'`. Think of it as "what is blocking this task?":
  ```sql
  WHERE target_id = ? AND relation_type = 'blocks'
  ```

Understanding this directionality is critical. If Task A blocks Task B:
- `GetBlocking(A)` returns the relation (A is the source, A is doing the blocking)
- `GetBlockedBy(B)` returns the relation (B is the target, B is being blocked)
- `GetBlocking(B)` returns nothing (B is not blocking anything)
- `GetBlockedBy(A)` returns nothing (nothing is blocking A)

### 5. The EXISTS Subquery Pattern

The `Exists` method uses SQL's `EXISTS` pattern with a subquery:

```sql
SELECT EXISTS(SELECT 1 FROM relations WHERE source_id = ? AND target_id = ? AND relation_type = ?)
```

Here is what is happening:

1. The inner query `SELECT 1 FROM relations WHERE ...` looks for a matching row. It does not matter what we select (`1`, `*`, `id` — anything works) because we only care whether a row exists, not what it contains.
2. `EXISTS(...)` wraps the inner query and returns a boolean: `true` if the inner query found at least one row, `false` if it found zero rows.
3. The outer `SELECT EXISTS(...)` returns that boolean as a result we can scan into a Go `bool` variable.

This is more efficient than `SELECT COUNT(*) ... > 0` because `EXISTS` stops searching as soon as it finds the first matching row. With `COUNT(*)`, the database would scan all matching rows to count them — unnecessary when we only care about "at least one."

In Go:

```go
var exists bool
err := r.db.QueryRowContext(ctx, `SELECT EXISTS(...)`, ...).Scan(&exists)
return exists, err
```

`QueryRowContext` returns exactly one row (the boolean result). We scan it directly into a `bool` variable. Simple and efficient.

### 6. The `scanRelations` Shared Helper

Unlike `AnnotationRepo` (where the scan logic is inline in `GetByTask`), `RelationRepo` has THREE methods that all return `[]*domain.Relation` from multi-row queries: `GetByTask`, `GetBlocking`, and `GetBlockedBy`. Instead of duplicating the row-scanning loop three times, we extract it into a shared helper:

```go
func scanRelations(rows *sql.Rows) ([]*domain.Relation, error) {
    // iterate rows, scan each into a *domain.Relation, collect into slice
}
```

Each calling method:
1. Runs its own SQL query (each with different WHERE clauses)
2. Gets back `*sql.Rows`
3. Passes the rows to `scanRelations`

This is a standard Go pattern: extract repeated logic into a helper function. The helper is package-private (lowercase) because it is an implementation detail.

### 7. The `relationColumns` Constant

At the top of `relation.go`, we define:

```go
const relationColumns = `id, source_id, target_id, relation_type, created_at`
```

This constant is used in every SELECT query:

```go
`SELECT ` + relationColumns + ` FROM relations WHERE ...`
```

Why? Because if you ever add or remove a column, you only change it in one place. Without this constant, you would need to update the column list in every query AND make sure the `Scan` call in `scanRelations` still matches. The constant reduces the chance of a mismatch.

---

## File Structure

| Action | File | Responsibility |
|--------|------|----------------|
| Create | `internal/sqlite/annotation_test.go` | Tests for all 3 AnnotationRepo methods + compile-time interface check |
| Create | `internal/sqlite/annotation.go` | `AnnotationRepo` struct implementing `repository.AnnotationRepository` |
| Create | `internal/sqlite/relation_test.go` | Tests for all 6 RelationRepo methods + duplicate detection + compile-time check |
| Rewrite | `internal/sqlite/relation.go` | `RelationRepo` struct implementing `repository.RelationRepository` (replaces stub) |

**Files you will read but NOT modify:**
- `internal/sqlite/store.go` — provides `timeFormat` constant and `Store` struct
- `internal/sqlite/store_test.go` — provides `testStore()` and `mustTimeNow()` helpers
- `internal/sqlite/task.go` — provides `NewTaskRepo()`
- `internal/sqlite/task_test.go` — provides `newTestTask()` and `mustCreateTask()` helpers
- `internal/domain/annotation.go` — the `Annotation` struct
- `internal/domain/relation.go` — the `Relation` struct
- `internal/domain/errors.go` — `ErrNotFound` and `ErrDuplicateRelation` sentinel errors
- `internal/repository/annotation.go` — the `AnnotationRepository` interface
- `internal/repository/relation.go` — the `RelationRepository` interface

---

## Tasks

### Task 1: Write the Annotation Tests (`annotation_test.go`)

We write tests first (TDD). The tests will not compile yet because `AnnotationRepo` does not exist. That is expected — seeing the compile failure confirms that our tests reference the right type and methods.

**Files:**
- Create: `internal/sqlite/annotation_test.go`

- [ ] **Step 1: Write `annotation_test.go`**

Create the file `internal/sqlite/annotation_test.go` with the following COMPLETE contents:

```go
package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/germanamz/tusk/internal/domain"
	"github.com/germanamz/tusk/internal/repository"
	"github.com/google/uuid"
)

// Compile-time check: *AnnotationRepo must implement repository.AnnotationRepository.
// If AnnotationRepo is missing any method, this line produces a compile error.
// The nil pointer is never dereferenced — it costs nothing at runtime.
var _ repository.AnnotationRepository = (*AnnotationRepo)(nil)

// TestAnnotationCreate verifies that we can insert a new annotation and read it back
// via GetByTask. It exercises Create and GetByTask together because you need
// GetByTask to verify that Create actually persisted the data.
//
// Note: we must create a task first because annotations have a foreign key
// (task_id) pointing to the tasks table. Without a valid task, the INSERT would
// fail with a foreign key constraint error.
func TestAnnotationCreate(t *testing.T) {
	// testStore creates an in-memory SQLite database with all migrations applied.
	s := testStore(t)

	// We need a TaskRepo to create the parent task.
	taskRepo := NewTaskRepo(s.DB())

	// This is the repo we are actually testing.
	repo := NewAnnotationRepo(s.DB())

	ctx := context.Background()

	// Create a parent task. newTestTask() returns a *domain.Task with all fields
	// populated. mustCreateTask inserts it and calls t.Fatal if anything goes wrong.
	task := newTestTask()
	mustCreateTask(t, taskRepo, task)

	// Build an annotation attached to the task we just created.
	// time.Now().UTC().Truncate(time.Millisecond) matches SQLite's millisecond
	// precision — without Truncate, the round-trip would lose sub-millisecond
	// data and comparisons would fail.
	ann := &domain.Annotation{
		ID:        uuid.New(),
		TaskID:    task.ID,
		Body:      "Blocked by upstream API changes",
		CreatedAt: time.Now().UTC().Truncate(time.Millisecond),
	}

	// Create should succeed with no error.
	if err := repo.Create(ctx, ann); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Read back all annotations for this task and verify our annotation is there.
	anns, err := repo.GetByTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(anns) != 1 {
		t.Fatalf("expected 1 annotation, got %d", len(anns))
	}
	if anns[0].Body != "Blocked by upstream API changes" {
		t.Fatalf("wrong body: %q", anns[0].Body)
	}
}

// TestAnnotationGetByTaskEmpty verifies that GetByTask returns an empty slice
// (not an error) when a task has no annotations. This is important: "no results"
// is not an error condition for a list query.
func TestAnnotationGetByTaskEmpty(t *testing.T) {
	s := testStore(t)
	taskRepo := NewTaskRepo(s.DB())
	repo := NewAnnotationRepo(s.DB())
	ctx := context.Background()

	// Create a task but do NOT create any annotations for it.
	task := newTestTask()
	mustCreateTask(t, taskRepo, task)

	anns, err := repo.GetByTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Should be 0 annotations, not an error.
	if len(anns) != 0 {
		t.Fatalf("expected 0 annotations, got %d", len(anns))
	}
}

// TestAnnotationGetByTaskMultiple verifies that GetByTask returns all annotations
// for a given task, not just the first one.
func TestAnnotationGetByTaskMultiple(t *testing.T) {
	s := testStore(t)
	taskRepo := NewTaskRepo(s.DB())
	repo := NewAnnotationRepo(s.DB())
	ctx := context.Background()

	task := newTestTask()
	mustCreateTask(t, taskRepo, task)

	// Create 3 annotations on the same task.
	for _, body := range []string{"First", "Second", "Third"} {
		ann := &domain.Annotation{
			ID:        uuid.New(),
			TaskID:    task.ID,
			Body:      body,
			CreatedAt: time.Now().UTC().Truncate(time.Millisecond),
		}
		if err := repo.Create(ctx, ann); err != nil {
			t.Fatal(err)
		}
	}

	anns, err := repo.GetByTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(anns) != 3 {
		t.Fatalf("expected 3 annotations, got %d", len(anns))
	}
}

// TestAnnotationDelete verifies that Delete removes an annotation and that
// GetByTask no longer returns it.
func TestAnnotationDelete(t *testing.T) {
	s := testStore(t)
	taskRepo := NewTaskRepo(s.DB())
	repo := NewAnnotationRepo(s.DB())
	ctx := context.Background()

	task := newTestTask()
	mustCreateTask(t, taskRepo, task)

	ann := &domain.Annotation{
		ID:        uuid.New(),
		TaskID:    task.ID,
		Body:      "To be deleted",
		CreatedAt: time.Now().UTC().Truncate(time.Millisecond),
	}
	if err := repo.Create(ctx, ann); err != nil {
		t.Fatal(err)
	}

	// Delete the annotation we just created.
	if err := repo.Delete(ctx, ann.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// Verify it is gone.
	anns, err := repo.GetByTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(anns) != 0 {
		t.Fatalf("expected 0 after delete, got %d", len(anns))
	}
}

// TestAnnotationDeleteNotFound verifies that deleting a non-existent annotation
// returns domain.ErrNotFound. This uses the RowsAffected pattern: the DELETE
// SQL succeeds but affects 0 rows, which we translate to ErrNotFound.
func TestAnnotationDeleteNotFound(t *testing.T) {
	s := testStore(t)
	repo := NewAnnotationRepo(s.DB())

	// uuid.New() generates a random UUID that does not exist in the DB.
	err := repo.Delete(context.Background(), uuid.New())
	if err != domain.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
```

**What each test does — summary table:**

| Test | Creates task? | Creates annotation? | Method under test | What it checks |
|------|:---:|:---:|---|---|
| `TestAnnotationCreate` | Yes | Yes (1) | `Create` + `GetByTask` | Round-trip: annotation body survives insert and select |
| `TestAnnotationGetByTaskEmpty` | Yes | No | `GetByTask` | Returns empty slice (not error) for task with no annotations |
| `TestAnnotationGetByTaskMultiple` | Yes | Yes (3) | `GetByTask` | Returns all annotations for a task, not just the first |
| `TestAnnotationDelete` | Yes | Yes (1) | `Delete` + `GetByTask` | Annotation is gone after delete |
| `TestAnnotationDeleteNotFound` | No | No | `Delete` | Returns `domain.ErrNotFound` for non-existent ID |

- [ ] **Step 2: Verify the tests do NOT compile**

Run:
```bash
cd /Users/germanamz/projects/tusk && go test ./internal/sqlite/ -run TestAnnotation -count=1
```

Expected: **Compile error**. You should see errors like `undefined: NewAnnotationRepo` and `undefined: AnnotationRepo`. This is the "red" phase of TDD — the tests exist but the code to make them pass does not.

If you see a different error (like an import error or syntax error), fix the test file before proceeding.

- [ ] **Step 3: Commit the test file**

```bash
git add internal/sqlite/annotation_test.go
git commit -m "test(sqlite): add AnnotationRepo tests (red phase — implementation pending)"
```

---

### Task 2: Implement `AnnotationRepo` (`annotation.go`)

Now we write the implementation to make the annotation tests pass.

**Files:**
- Create: `internal/sqlite/annotation.go`

- [ ] **Step 1: Write `annotation.go`**

Create the file `internal/sqlite/annotation.go` with the following COMPLETE contents:

```go
package sqlite

import (
	"context"
	"database/sql"
	"time"

	"github.com/germanamz/tusk/internal/domain"
	"github.com/google/uuid"
)

// AnnotationRepo implements repository.AnnotationRepository using SQLite.
//
// It stores *sql.DB (not *Store) so it depends only on the standard library's
// database abstraction. The Store is responsible for opening the DB and running
// migrations; AnnotationRepo just runs queries.
type AnnotationRepo struct {
	db *sql.DB
}

// NewAnnotationRepo creates an AnnotationRepo. Pass in the *sql.DB from Store.DB().
//
// Example:
//
//	store, _ := sqlite.New("tusk.db", migrations.FS)
//	repo := sqlite.NewAnnotationRepo(store.DB())
func NewAnnotationRepo(db *sql.DB) *AnnotationRepo {
	return &AnnotationRepo{db: db}
}

// Create inserts a new annotation into the database.
//
// The caller must set all fields on the Annotation struct before calling Create.
// The ID should be generated with uuid.New(), TaskID must reference an existing
// task, and CreatedAt should be set to time.Now().UTC().
//
// The annotation's task_id must reference an existing task (foreign key constraint).
// If the task does not exist, SQLite returns a foreign key violation error.
func (r *AnnotationRepo) Create(ctx context.Context, ann *domain.Annotation) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO annotations (id, task_id, body, created_at) VALUES (?, ?, ?, ?)`,
		ann.ID.String(), ann.TaskID.String(), ann.Body,
		ann.CreatedAt.UTC().Format(timeFormat),
	)
	return err
}

// GetByTask retrieves all annotations for a given task, ordered by creation time.
//
// Returns an empty slice (not nil and not an error) if the task has no annotations.
// The ORDER BY created_at ensures annotations appear in chronological order,
// which is the natural reading order for notes/comments.
//
// How it works step by step:
// 1. Run the SELECT query with the taskID as a parameter.
// 2. Iterate over each returned row with rows.Next().
// 3. For each row, scan the 4 columns (id, task_id, body, created_at) into
//    local variables. id, task_id, and created_at are scanned as strings because
//    SQLite stores UUIDs and timestamps as TEXT.
// 4. Parse the string values into Go types (uuid.UUID and time.Time).
// 5. Append each assembled *domain.Annotation to the result slice.
// 6. After the loop, check rows.Err() for any iteration errors.
func (r *AnnotationRepo) GetByTask(ctx context.Context, taskID uuid.UUID) ([]*domain.Annotation, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, task_id, body, created_at FROM annotations WHERE task_id = ? ORDER BY created_at`,
		taskID.String())
	if err != nil {
		return nil, err
	}
	// Always close rows when done. defer ensures this happens even if we
	// return early due to an error. Failing to close rows leaks database
	// connections.
	defer rows.Close()

	var result []*domain.Annotation
	for rows.Next() {
		var (
			a                    domain.Annotation
			id, tid, createdAt string
		)
		if err := rows.Scan(&id, &tid, &a.Body, &createdAt); err != nil {
			return nil, err
		}
		a.ID, err = uuid.Parse(id)
		if err != nil {
			return nil, err
		}
		a.TaskID, err = uuid.Parse(tid)
		if err != nil {
			return nil, err
		}
		a.CreatedAt, err = time.Parse(timeFormat, createdAt)
		if err != nil {
			return nil, err
		}
		result = append(result, &a)
	}
	return result, rows.Err()
}

// Delete removes an annotation by its ID.
// Returns domain.ErrNotFound if no annotation with that ID exists.
//
// This uses the RowsAffected pattern:
// 1. Run the DELETE statement. Even if no rows match, the SQL itself succeeds.
// 2. Check RowsAffected(). If it is 0, no row was deleted, meaning the ID
//    did not exist. We return domain.ErrNotFound in that case.
func (r *AnnotationRepo) Delete(ctx context.Context, id uuid.UUID) error {
	res, err := r.db.ExecContext(ctx,
		`DELETE FROM annotations WHERE id = ?`, id.String())
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return domain.ErrNotFound
	}
	return nil
}
```

**Line-by-line explanation of important parts:**

**The struct and constructor:**
- `AnnotationRepo` holds a `*sql.DB`. This is Go's standard database handle. It manages a connection pool internally.
- `NewAnnotationRepo` is a constructor function. In Go, constructors are regular functions that return a pointer to the struct.
- We take `*sql.DB` instead of `*Store` to keep the dependency narrow.

**Create method:**
- `ExecContext` runs an SQL statement that does not return rows (INSERT, UPDATE, DELETE).
- `?` is a parameter placeholder. SQLite replaces each `?` with the corresponding argument, properly escaping it. This prevents SQL injection.
- `ann.ID.String()` converts the UUID to its string representation because SQLite stores UUIDs as TEXT.
- `ann.CreatedAt.UTC().Format(timeFormat)` formats the time to match SQLite's format.

**GetByTask method:**
- `QueryContext` returns `*sql.Rows` — zero or more rows to iterate.
- `defer rows.Close()` is critical. Without it, the database connection used by this query is never returned to the pool.
- The `for rows.Next()` loop advances through each row. `rows.Next()` returns false when there are no more rows.
- `rows.Err()` at the end checks if the iteration stopped due to an error (as opposed to running out of rows).
- Unlike `RelationRepo`, we do NOT use a shared scan helper here because there is only one method that returns `[]*domain.Annotation`. If we later add more methods that scan annotations, we would extract a `scanAnnotations` helper at that point.

**Delete method:**
- Same `RowsAffected` pattern used in every repo's Delete: execute the DELETE, check how many rows were affected, return `ErrNotFound` if zero.

- [ ] **Step 2: Verify the annotation tests pass**

Run:
```bash
cd /Users/germanamz/projects/tusk && go test ./internal/sqlite/ -run TestAnnotation -v -count=1
```

Expected output (all should say PASS):
```
=== RUN   TestAnnotationCreate
--- PASS: TestAnnotationCreate
=== RUN   TestAnnotationGetByTaskEmpty
--- PASS: TestAnnotationGetByTaskEmpty
=== RUN   TestAnnotationGetByTaskMultiple
--- PASS: TestAnnotationGetByTaskMultiple
=== RUN   TestAnnotationDelete
--- PASS: TestAnnotationDelete
=== RUN   TestAnnotationDeleteNotFound
--- PASS: TestAnnotationDeleteNotFound
PASS
```

If any test fails, read the error message carefully. Common issues:
- **"undefined: testStore"** — means `store_test.go` is missing or not in the `sqlite` package. Prior phases must be complete.
- **"undefined: newTestTask"** — means `task_test.go` is missing. Phase 3 must be complete.
- **"FOREIGN KEY constraint failed"** — you forgot to create the parent task before creating the annotation. Make sure `mustCreateTask` is called before `repo.Create`.
- **Time mismatch** — make sure you are using `time.Now().UTC().Truncate(time.Millisecond)` in tests.

- [ ] **Step 3: Commit the implementation**

```bash
git add internal/sqlite/annotation.go
git commit -m "feat(sqlite): implement AnnotationRepo with Create, GetByTask, Delete"
```

---

### Task 3: Write the Relation Tests (`relation_test.go`)

Now we switch to RelationRepo. Again, tests first.

**Files:**
- Create: `internal/sqlite/relation_test.go`

- [ ] **Step 1: Write `relation_test.go`**

Create the file `internal/sqlite/relation_test.go` with the following COMPLETE contents:

```go
package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/germanamz/tusk/internal/domain"
	"github.com/germanamz/tusk/internal/repository"
	"github.com/google/uuid"
)

// Compile-time check: *RelationRepo must implement repository.RelationRepository.
var _ repository.RelationRepository = (*RelationRepo)(nil)

// newTestRelation is a test helper that creates a *domain.Relation with all
// fields populated. It generates a fresh UUID for the ID and uses the current
// time (truncated to milliseconds) for CreatedAt.
//
// Parameters:
//   - sourceID: the UUID of the source task (the task "doing" the action)
//   - targetID: the UUID of the target task (the task "receiving" the action)
//   - relType: one of "blocks", "relates_to", "duplicates"
func newTestRelation(sourceID, targetID uuid.UUID, relType string) *domain.Relation {
	return &domain.Relation{
		ID:           uuid.New(),
		SourceID:     sourceID,
		TargetID:     targetID,
		RelationType: relType,
		CreatedAt:    time.Now().UTC().Truncate(time.Millisecond),
	}
}

// TestRelationCreate verifies that we can insert a new relation and read it back
// via GetByTask.
//
// Setup: create two tasks (source and target), then create a "blocks" relation
// from task1 to task2. Verify that GetByTask(task1.ID) returns the relation.
func TestRelationCreate(t *testing.T) {
	s := testStore(t)
	taskRepo := NewTaskRepo(s.DB())
	repo := NewRelationRepo(s.DB())
	ctx := context.Background()

	// Create two tasks. Relations connect two tasks, so we need both to exist.
	t1 := newTestTask()
	t2 := newTestTask()
	mustCreateTask(t, taskRepo, t1)
	mustCreateTask(t, taskRepo, t2)

	// Create a "blocks" relation: t1 blocks t2.
	rel := newTestRelation(t1.ID, t2.ID, "blocks")
	if err := repo.Create(ctx, rel); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Verify via GetByTask. Since t1 is the source, it should appear.
	rels, err := repo.GetByTask(ctx, t1.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rels) != 1 {
		t.Fatalf("expected 1, got %d", len(rels))
	}
	if rels[0].RelationType != "blocks" {
		t.Fatalf("expected blocks, got %s", rels[0].RelationType)
	}
}

// TestRelationCreateDuplicate verifies that inserting the same (source, target, type)
// combination twice returns domain.ErrDuplicateRelation.
//
// This tests the UNIQUE(source_id, target_id, relation_type) constraint and our
// isUniqueViolation helper that translates the SQLite error into a domain error.
func TestRelationCreateDuplicate(t *testing.T) {
	s := testStore(t)
	taskRepo := NewTaskRepo(s.DB())
	repo := NewRelationRepo(s.DB())
	ctx := context.Background()

	t1 := newTestTask()
	t2 := newTestTask()
	mustCreateTask(t, taskRepo, t1)
	mustCreateTask(t, taskRepo, t2)

	// First insert: should succeed.
	if err := repo.Create(ctx, newTestRelation(t1.ID, t2.ID, "blocks")); err != nil {
		t.Fatal(err)
	}

	// Second insert with same source, target, and type: should fail.
	// Note that newTestRelation generates a NEW uuid.UUID for the ID field,
	// but the UNIQUE constraint is on (source_id, target_id, relation_type),
	// not on id. So even though the IDs differ, the constraint fires.
	err := repo.Create(ctx, newTestRelation(t1.ID, t2.ID, "blocks"))
	if err != domain.ErrDuplicateRelation {
		t.Fatalf("expected ErrDuplicateRelation, got %v", err)
	}
}

// TestRelationDelete verifies that Delete removes a relation and that
// GetByTask no longer returns it.
func TestRelationDelete(t *testing.T) {
	s := testStore(t)
	taskRepo := NewTaskRepo(s.DB())
	repo := NewRelationRepo(s.DB())
	ctx := context.Background()

	t1 := newTestTask()
	t2 := newTestTask()
	mustCreateTask(t, taskRepo, t1)
	mustCreateTask(t, taskRepo, t2)

	rel := newTestRelation(t1.ID, t2.ID, "relates_to")
	if err := repo.Create(ctx, rel); err != nil {
		t.Fatal(err)
	}

	// Delete the relation.
	if err := repo.Delete(ctx, rel.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// Verify it is gone.
	rels, err := repo.GetByTask(ctx, t1.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rels) != 0 {
		t.Fatalf("expected 0 after delete, got %d", len(rels))
	}
}

// TestRelationGetByTask verifies that GetByTask returns relations where the
// task is EITHER the source OR the target.
//
// Setup:
//   - t1 -> t2 (blocks) — t1 is source
//   - t3 -> t1 (relates_to) — t1 is target
//
// GetByTask(t1) should return BOTH relations (2 total) because t1 appears
// as source in one and target in the other.
func TestRelationGetByTask(t *testing.T) {
	s := testStore(t)
	taskRepo := NewTaskRepo(s.DB())
	repo := NewRelationRepo(s.DB())
	ctx := context.Background()

	t1 := newTestTask()
	t2 := newTestTask()
	t3 := newTestTask()
	mustCreateTask(t, taskRepo, t1)
	mustCreateTask(t, taskRepo, t2)
	mustCreateTask(t, taskRepo, t3)

	// t1 is source in this relation.
	if err := repo.Create(ctx, newTestRelation(t1.ID, t2.ID, "blocks")); err != nil {
		t.Fatal(err)
	}
	// t1 is target in this relation.
	if err := repo.Create(ctx, newTestRelation(t3.ID, t1.ID, "relates_to")); err != nil {
		t.Fatal(err)
	}

	rels, err := repo.GetByTask(ctx, t1.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Both relations involve t1, so both should be returned.
	if len(rels) != 2 {
		t.Fatalf("expected 2, got %d", len(rels))
	}
}

// TestRelationGetBlocking verifies that GetBlocking returns only relations
// where the given task is the SOURCE and the type is "blocks".
//
// Setup:
//   - t1 blocks t2 — should be returned (t1 is source, type is "blocks")
//   - t1 blocks t3 — should be returned (t1 is source, type is "blocks")
//   - t2 relates_to t3 — should NOT be returned (different type)
//
// GetBlocking(t1) should return 2 relations.
func TestRelationGetBlocking(t *testing.T) {
	s := testStore(t)
	taskRepo := NewTaskRepo(s.DB())
	repo := NewRelationRepo(s.DB())
	ctx := context.Background()

	t1 := newTestTask()
	t2 := newTestTask()
	t3 := newTestTask()
	mustCreateTask(t, taskRepo, t1)
	mustCreateTask(t, taskRepo, t2)
	mustCreateTask(t, taskRepo, t3)

	// t1 is the blocker in both of these.
	if err := repo.Create(ctx, newTestRelation(t1.ID, t2.ID, "blocks")); err != nil {
		t.Fatal(err)
	}
	if err := repo.Create(ctx, newTestRelation(t1.ID, t3.ID, "blocks")); err != nil {
		t.Fatal(err)
	}
	// This is a different type — should not appear in GetBlocking results.
	if err := repo.Create(ctx, newTestRelation(t2.ID, t3.ID, "relates_to")); err != nil {
		t.Fatal(err)
	}

	blocking, err := repo.GetBlocking(ctx, t1.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(blocking) != 2 {
		t.Fatalf("expected 2, got %d", len(blocking))
	}
}

// TestRelationGetBlockedBy verifies that GetBlockedBy returns only relations
// where the given task is the TARGET and the type is "blocks".
//
// Setup:
//   - t1 blocks t2 — GetBlockedBy(t2) should return this (t2 is the target)
//
// GetBlockedBy(t2) should return 1 relation, and its SourceID should be t1.
func TestRelationGetBlockedBy(t *testing.T) {
	s := testStore(t)
	taskRepo := NewTaskRepo(s.DB())
	repo := NewRelationRepo(s.DB())
	ctx := context.Background()

	t1 := newTestTask()
	t2 := newTestTask()
	mustCreateTask(t, taskRepo, t1)
	mustCreateTask(t, taskRepo, t2)

	// t1 blocks t2: t1 is the source (blocker), t2 is the target (blocked).
	if err := repo.Create(ctx, newTestRelation(t1.ID, t2.ID, "blocks")); err != nil {
		t.Fatal(err)
	}

	// Ask "what is blocking t2?" — should return the relation with t1 as source.
	blockedBy, err := repo.GetBlockedBy(ctx, t2.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(blockedBy) != 1 {
		t.Fatalf("expected 1, got %d", len(blockedBy))
	}
	// Verify that the source of the blocking relation is t1.
	if blockedBy[0].SourceID != t1.ID {
		t.Fatal("expected source to be t1")
	}
}

// TestRelationExists verifies the Exists method, which checks whether a
// specific (source, target, type) combination exists in the database.
//
// This test checks three scenarios:
// 1. The exact combination exists — should return true.
// 2. The reverse direction (swap source and target) — should return false
//    because relations are directional.
// 3. Same source and target but different type — should return false.
func TestRelationExists(t *testing.T) {
	s := testStore(t)
	taskRepo := NewTaskRepo(s.DB())
	repo := NewRelationRepo(s.DB())
	ctx := context.Background()

	t1 := newTestTask()
	t2 := newTestTask()
	mustCreateTask(t, taskRepo, t1)
	mustCreateTask(t, taskRepo, t2)

	// Create: t1 blocks t2.
	if err := repo.Create(ctx, newTestRelation(t1.ID, t2.ID, "blocks")); err != nil {
		t.Fatal(err)
	}

	// Scenario 1: exact match — should be true.
	exists, err := repo.Exists(ctx, t1.ID, t2.ID, "blocks")
	if err != nil {
		t.Fatal(err)
	}
	if !exists {
		t.Fatal("expected true")
	}

	// Scenario 2: reversed direction — should be false.
	// t2 does NOT block t1. Relations are directional!
	exists, err = repo.Exists(ctx, t2.ID, t1.ID, "blocks")
	if err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Fatal("expected false for reverse")
	}

	// Scenario 3: different type — should be false.
	// t1 blocks t2, but t1 does NOT "relates_to" t2.
	exists, err = repo.Exists(ctx, t1.ID, t2.ID, "relates_to")
	if err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Fatal("expected false for different type")
	}
}
```

**What each test does — summary table:**

| Test | Tasks created | Relations created | Method under test | What it checks |
|------|:---:|:---:|---|---|
| `TestRelationCreate` | 2 | 1 (blocks) | `Create` + `GetByTask` | Round-trip: relation survives insert and select |
| `TestRelationCreateDuplicate` | 2 | 2 (same combo) | `Create` | Returns `domain.ErrDuplicateRelation` on UNIQUE violation |
| `TestRelationDelete` | 2 | 1 (relates_to) | `Delete` + `GetByTask` | Relation is gone after delete |
| `TestRelationGetByTask` | 3 | 2 (mixed) | `GetByTask` | Returns relations where task is source OR target |
| `TestRelationGetBlocking` | 3 | 3 (2 blocks + 1 relates_to) | `GetBlocking` | Returns only source + "blocks" relations |
| `TestRelationGetBlockedBy` | 2 | 1 (blocks) | `GetBlockedBy` | Returns only target + "blocks" relations, verifies source ID |
| `TestRelationExists` | 2 | 1 (blocks) | `Exists` | True for exact match, false for reverse, false for different type |

- [ ] **Step 2: Verify the tests do NOT compile**

Run:
```bash
cd /Users/germanamz/projects/tusk && go test ./internal/sqlite/ -run TestRelation -count=1
```

Expected: **Compile error**. You should see errors like `undefined: NewRelationRepo` because the current `relation.go` is just an empty stub (`package sqlite`). This confirms our tests reference the right things.

- [ ] **Step 3: Commit the test file**

```bash
git add internal/sqlite/relation_test.go
git commit -m "test(sqlite): add RelationRepo tests (red phase — implementation pending)"
```

---

### Task 4: Implement `RelationRepo` (`relation.go`)

Now we write the implementation to make the relation tests pass. This **replaces** the existing stub file.

**Files:**
- Rewrite: `internal/sqlite/relation.go` (currently contains only `package sqlite`)

- [ ] **Step 1: Write `relation.go`**

Replace the contents of `internal/sqlite/relation.go` with the following COMPLETE file:

```go
package sqlite

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/germanamz/tusk/internal/domain"
	"github.com/google/uuid"
)

// relationColumns lists the columns selected in every relation query.
// Having this as a constant means if we ever add or remove a column, we change
// it in ONE place instead of in every SQL string. The order must match what
// scanRelations expects in its Scan call.
const relationColumns = `id, source_id, target_id, relation_type, created_at`

// RelationRepo implements repository.RelationRepository using SQLite.
//
// It manages directed relationships between tasks. Each relation has a source
// task and a target task, plus a type ("blocks", "relates_to", "duplicates").
// The direction matters: "A blocks B" is different from "B blocks A".
type RelationRepo struct {
	db *sql.DB
}

// NewRelationRepo creates a RelationRepo. Pass in the *sql.DB from Store.DB().
func NewRelationRepo(db *sql.DB) *RelationRepo {
	return &RelationRepo{db: db}
}

// Create inserts a new relation into the database.
//
// The caller must set all fields on the Relation struct before calling Create.
// Both SourceID and TargetID must reference existing tasks (foreign key constraint).
//
// If a relation with the same (source_id, target_id, relation_type) already exists,
// the UNIQUE constraint fires and this method returns domain.ErrDuplicateRelation.
// This prevents accidental duplicate relations.
//
// How duplicate detection works:
// 1. We attempt the INSERT.
// 2. If SQLite rejects it with "UNIQUE constraint failed", ExecContext returns an error.
// 3. isUniqueViolation checks the error message string for that phrase.
// 4. If it matches, we return domain.ErrDuplicateRelation instead of the raw SQLite error.
func (r *RelationRepo) Create(ctx context.Context, rel *domain.Relation) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO relations (id, source_id, target_id, relation_type, created_at) VALUES (?, ?, ?, ?, ?)`,
		rel.ID.String(), rel.SourceID.String(), rel.TargetID.String(),
		rel.RelationType, rel.CreatedAt.UTC().Format(timeFormat),
	)
	if err != nil && isUniqueViolation(err) {
		return domain.ErrDuplicateRelation
	}
	return err
}

// Delete removes a relation by its ID.
// Returns domain.ErrNotFound if no relation with that ID exists.
//
// Uses the same RowsAffected pattern as AnnotationRepo.Delete.
func (r *RelationRepo) Delete(ctx context.Context, id uuid.UUID) error {
	res, err := r.db.ExecContext(ctx,
		`DELETE FROM relations WHERE id = ?`, id.String())
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// GetByTask retrieves ALL relations where the given task is involved, regardless
// of whether it is the source or the target.
//
// The SQL uses OR: WHERE source_id = ? OR target_id = ?
//
// This gives the caller the complete picture of all relationships for a task.
// For example, if Task A blocks Task B and Task C relates_to Task A:
//   - GetByTask(A) returns both relations
//   - GetByTask(B) returns only the "A blocks B" relation
//   - GetByTask(C) returns only the "C relates_to A" relation
func (r *RelationRepo) GetByTask(ctx context.Context, taskID uuid.UUID) ([]*domain.Relation, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+relationColumns+` FROM relations WHERE source_id = ? OR target_id = ?`,
		taskID.String(), taskID.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRelations(rows)
}

// GetBlocking retrieves relations where the given task is the SOURCE and the
// relation type is "blocks". In other words: "what tasks does this task block?"
//
// The SQL: WHERE source_id = ? AND relation_type = 'blocks'
//
// Example: if Task A blocks Task B and Task A blocks Task C,
// GetBlocking(A) returns both relations.
// GetBlocking(B) returns nothing (B is not blocking anything).
func (r *RelationRepo) GetBlocking(ctx context.Context, taskID uuid.UUID) ([]*domain.Relation, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+relationColumns+` FROM relations WHERE source_id = ? AND relation_type = 'blocks'`,
		taskID.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRelations(rows)
}

// GetBlockedBy retrieves relations where the given task is the TARGET and the
// relation type is "blocks". In other words: "what tasks are blocking this task?"
//
// The SQL: WHERE target_id = ? AND relation_type = 'blocks'
//
// Example: if Task A blocks Task B, GetBlockedBy(B) returns the relation
// (with SourceID = A). GetBlockedBy(A) returns nothing (nothing is blocking A).
func (r *RelationRepo) GetBlockedBy(ctx context.Context, taskID uuid.UUID) ([]*domain.Relation, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+relationColumns+` FROM relations WHERE target_id = ? AND relation_type = 'blocks'`,
		taskID.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRelations(rows)
}

// Exists checks whether a specific (source, target, type) combination exists.
// Returns true if it does, false if it does not.
//
// This uses SQL's EXISTS subquery pattern:
//   SELECT EXISTS(SELECT 1 FROM relations WHERE ...)
//
// How it works:
// 1. The inner SELECT looks for a matching row. "SELECT 1" is used because we
//    do not care about the actual data — only whether a row exists.
// 2. EXISTS(...) returns true if the inner query found at least one row.
// 3. The outer SELECT returns that boolean, which we scan into a Go bool.
//
// EXISTS is more efficient than COUNT(*) because it stops as soon as it finds
// the first matching row, rather than counting all matches.
//
// Note: direction matters! Exists(A, B, "blocks") is NOT the same as
// Exists(B, A, "blocks").
func (r *RelationRepo) Exists(ctx context.Context, sourceID, targetID uuid.UUID, relType string) (bool, error) {
	var exists bool
	err := r.db.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM relations WHERE source_id = ? AND target_id = ? AND relation_type = ?)`,
		sourceID.String(), targetID.String(), relType).Scan(&exists)
	return exists, err
}

// scanRelations iterates over sql.Rows and assembles a slice of *domain.Relation.
// This is a shared helper used by GetByTask, GetBlocking, and GetBlockedBy to
// avoid duplicating the row-scanning loop three times.
//
// The function:
// 1. Loops through each row with rows.Next().
// 2. Scans the 5 columns (matching the order in relationColumns) into local
//    string variables for the TEXT columns (id, source_id, target_id, created_at)
//    and directly into the struct for relation_type (which is already a string).
// 3. Parses the UUID and time strings into Go types.
// 4. Appends each assembled *domain.Relation to the result slice.
// 5. After the loop, returns the result plus any iteration error from rows.Err().
//
// This function does NOT call rows.Close() — that is the caller's responsibility
// (via defer rows.Close()). This is a common Go pattern: the function that opens
// a resource is responsible for closing it.
func scanRelations(rows *sql.Rows) ([]*domain.Relation, error) {
	var result []*domain.Relation
	for rows.Next() {
		var (
			r                               domain.Relation
			id, sourceID, targetID, createdAt string
		)
		if err := rows.Scan(&id, &sourceID, &targetID, &r.RelationType, &createdAt); err != nil {
			return nil, err
		}
		var err error
		r.ID, err = uuid.Parse(id)
		if err != nil {
			return nil, err
		}
		r.SourceID, err = uuid.Parse(sourceID)
		if err != nil {
			return nil, err
		}
		r.TargetID, err = uuid.Parse(targetID)
		if err != nil {
			return nil, err
		}
		r.CreatedAt, err = time.Parse(timeFormat, createdAt)
		if err != nil {
			return nil, err
		}
		result = append(result, &r)
	}
	return result, rows.Err()
}

// isUniqueViolation checks whether an error is a SQLite UNIQUE constraint violation.
//
// When you try to INSERT a row that violates a UNIQUE constraint (like our
// UNIQUE(source_id, target_id, relation_type) on the relations table), the
// SQLite driver returns an error whose message contains "UNIQUE constraint failed".
//
// We check for this by looking at the error message string. This is not the most
// elegant approach — ideally we would check a typed error code. But Go's
// database/sql package does not define typed constraint errors, and the SQLite
// driver's error string has been stable for years. This pragmatic approach works
// reliably in practice.
//
// This function is private (lowercase) because it is an implementation detail
// of the sqlite package. No code outside this package should need to detect
// UNIQUE violations directly — they should use domain errors like
// ErrDuplicateRelation instead.
func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}
```

**Detailed explanation of the most important parts:**

**`relationColumns` constant:**
This string lists the 5 columns in the exact order that `scanRelations` expects them. Every SELECT query uses this constant: `` `SELECT ` + relationColumns + ` FROM relations WHERE ...` ``. If you ever rename a column or add a new one, you change it here and in `scanRelations` — two places instead of six (one per query).

**`Create` with duplicate detection:**
The flow is: attempt INSERT, check if the error is a UNIQUE violation, translate to domain error if so. The `isUniqueViolation` check happens AFTER `ExecContext` returns. We do not pre-check with `Exists` because that would create a race condition (another goroutine could insert between our check and our insert). Letting the database enforce uniqueness is the correct approach.

**`GetByTask` vs `GetBlocking` vs `GetBlockedBy`:**
These three methods have the same structure — run a query, scan the rows — but different WHERE clauses:
- `GetByTask`: `source_id = ? OR target_id = ?` (both directions)
- `GetBlocking`: `source_id = ? AND relation_type = 'blocks'` (outgoing blocks only)
- `GetBlockedBy`: `target_id = ? AND relation_type = 'blocks'` (incoming blocks only)

All three delegate to `scanRelations` for the row-scanning loop.

**`Exists` method:**
This is the simplest method — it does not build a slice, it just checks for existence. `QueryRowContext` always returns exactly one row (the boolean), so there is no loop. We scan directly into a `bool` variable.

**`scanRelations` helper:**
Notice that inside the loop, the variable is named `r` (for relation), which shadows the receiver name `r` used on the `RelationRepo` methods. This is fine because `scanRelations` is a package-level function, not a method — it has no receiver. The `r` inside the loop is a local `domain.Relation` value.

**`isUniqueViolation` helper:**
This is the only place in the codebase where we inspect SQLite error strings. It is intentionally isolated in one function so that if the error message ever changes, we only need to update one place. The `strings.Contains` check is case-sensitive, which matches SQLite's actual error format.

- [ ] **Step 2: Verify the relation tests pass**

Run:
```bash
cd /Users/germanamz/projects/tusk && go test ./internal/sqlite/ -run TestRelation -v -count=1
```

Expected output (all should say PASS):
```
=== RUN   TestRelationCreate
--- PASS: TestRelationCreate
=== RUN   TestRelationCreateDuplicate
--- PASS: TestRelationCreateDuplicate
=== RUN   TestRelationDelete
--- PASS: TestRelationDelete
=== RUN   TestRelationGetByTask
--- PASS: TestRelationGetByTask
=== RUN   TestRelationGetBlocking
--- PASS: TestRelationGetBlocking
=== RUN   TestRelationGetBlockedBy
--- PASS: TestRelationGetBlockedBy
=== RUN   TestRelationExists
--- PASS: TestRelationExists
PASS
```

If any test fails, common issues:
- **"UNIQUE constraint failed" not detected** — check that `isUniqueViolation` is defined and that `strings` is imported.
- **"FOREIGN KEY constraint failed"** — make sure both tasks are created with `mustCreateTask` before creating the relation.
- **Wrong count in GetByTask** — remember that `GetByTask` uses `OR` (both directions). Double-check which tasks are source vs target.
- **Wrong count in GetBlocking/GetBlockedBy** — remember these are directional. `GetBlocking` checks `source_id`, `GetBlockedBy` checks `target_id`.

- [ ] **Step 3: Commit the implementation**

```bash
git add internal/sqlite/relation.go
git commit -m "feat(sqlite): implement RelationRepo with directional queries and duplicate detection"
```

---

### Final Verification

After all four tasks are complete, run the full test suite to make sure nothing is broken.

- [ ] **Step 1: Run ALL sqlite tests**

```bash
cd /Users/germanamz/projects/tusk && go test ./internal/sqlite/ -v -count=1
```

Expected: ALL tests pass — Store tests from Phase 1, ProjectRepo tests from Phase 2, TaskRepo tests from Phase 3, and the new AnnotationRepo and RelationRepo tests from this phase.

- [ ] **Step 2: Run go vet**

```bash
cd /Users/germanamz/projects/tusk && go vet ./internal/sqlite/
```

Expected: No output (clean). `go vet` catches common mistakes like unused variables, incorrect printf format strings, and unreachable code.

- [ ] **Step 3: Verify the full build**

```bash
cd /Users/germanamz/projects/tusk && go build ./...
```

Expected: No errors. The entire project still compiles.

---

## Summary of Patterns Used and Established

### Patterns Reused from Prior Phases

| Pattern | Where it came from | How we use it here |
|---------|-------------------|-------------------|
| `testStore(t)` for test setup | Phase 1 `store_test.go` | Every test function in both test files |
| `timeFormat` constant | Phase 1 `store.go` | `Create` (formatting) and scan functions (parsing) |
| `newTestTask()` / `mustCreateTask()` | Phase 3 `task_test.go` | Setting up parent tasks before creating annotations/relations |
| `RowsAffected` check in Delete | Phase 2 `project.go` | Both `AnnotationRepo.Delete` and `RelationRepo.Delete` |
| Compile-time interface check (`var _`) | Phase 2 `project_test.go` | Both test files |
| `defer rows.Close()` | Phase 2 `project.go` | Every method that uses `QueryContext` |

### New Patterns Introduced in This Phase

| Pattern | Where it appears | When to reuse |
|---------|-----------------|---------------|
| `isUniqueViolation(err)` helper | `relation.go` | Any repo that needs to detect UNIQUE constraint violations |
| `scanRelations(rows)` shared helper | `relation.go` | When multiple methods scan the same row shape |
| `relationColumns` constant | `relation.go` | When the same column list appears in multiple queries |
| `EXISTS` subquery for boolean checks | `RelationRepo.Exists` | Any "does this exist?" check |
| `strings.Contains` for SQLite error detection | `isUniqueViolation` | Detecting specific SQLite errors when typed errors are unavailable |
| Directional query patterns (source vs target) | `GetBlocking` / `GetBlockedBy` | Any directed graph queries |
