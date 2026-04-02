# SQLite Phase 5: Task Repository Hierarchy — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the `GetChildren` and `GetDescendants` stub methods in `TaskRepo` with real SQL queries so Tusk can navigate task trees. Also add a small helper function and a test that ties the Phase 4 `List` filter to the new hierarchy queries.

**Architecture:** `TaskRepo` in `internal/sqlite/task.go` already has stub methods that return `"not implemented"`. This phase replaces those stubs with working SQL. No new files are created — we only modify `internal/sqlite/task.go` and append tests to `internal/sqlite/task_test.go`.

**Tech Stack:** Go 1.26, `database/sql`, `github.com/google/uuid`, SQLite (via CGo driver)

**Go Module:** `github.com/germanamz/tusk`

---

## Context: What Is Tusk?

Tusk is a local-first task manager written in Go. It stores tasks in a SQLite database. Tasks can form a **tree**: every task has an optional `parent_id` that points to another task's `id`. This lets you break a big task ("Build website") into smaller sub-tasks ("Design header", "Write CSS"), and those sub-tasks can have their own children, and so on. This parent-child relationship is the "hierarchy" that this phase implements queries for.

### What Prior Phases Produced

Here is a summary of what already exists in the codebase. You do **not** need to create any of these — they are already written.

| Phase | File | What it contains |
|-------|------|-----------------|
| 1 | `internal/sqlite/store.go` | `Store` struct, `testStore(t)` helper (creates an in-memory SQLite DB for tests), `timeFormat` constant, and various DB helpers. |
| 3 | `internal/sqlite/task.go` | `TaskRepo` struct (holds `db *sql.DB`), the `taskColumns` constant listing all 15 column names, `scanTask()` to parse a DB row into a `*domain.Task`, `scanRows()` to iterate over `*sql.Rows` and call `scanTask` for each row, CRUD methods (`Create`, `GetByID`, `GetByShortID`, `Update`, `Delete`), and **stub** implementations of `GetChildren` and `GetDescendants` that return `errors.New("not implemented")`. |
| 3 | `internal/sqlite/task_test.go` | `newTestTask()` helper that creates a `domain.Task` with random valid fields, `mustCreateTask(t, repo, task)` helper that calls `repo.Create` and fails the test on error, and CRUD tests. |
| 4 | `internal/sqlite/task.go` | `buildFilter()` function that constructs a WHERE clause from a `domain.TaskFilter`, and the `List` method that uses `buildFilter`. The `RootID` filter in `buildFilter` already generates a recursive CTE to find all descendants of a given root task, then filters the main `tasks` table to only include rows whose `id` appears in that CTE. |

### What This Phase Modifies

- **`internal/sqlite/task.go`** — Replace two stub methods with real implementations. Add one new helper function (`prefixColumns`).
- **`internal/sqlite/task_test.go`** — Append five new test functions.

No new files are created.

---

## Key Concepts

Before you start coding, read through these concepts. They will help you understand **why** the code is written the way it is.

### 1. Parent-Child Hierarchy in SQL (Self-Referential Foreign Key)

The `tasks` table has a column called `parent_id`. This column stores the `id` of another row in the **same** `tasks` table. This is called a "self-referential foreign key" — the table points back to itself.

```
tasks table:
+------+------------+-----------+
|  id  | parent_id  |   title   |
+------+------------+-----------+
|  A   |   NULL     | Project X |   <-- root task (no parent)
|  B   |     A      | Design    |   <-- child of A
|  C   |     A      | Develop   |   <-- child of A
|  D   |     B      | Mockups   |   <-- child of B, grandchild of A
+------+------------+-----------+
```

- **Children** of A = tasks whose `parent_id` equals A's `id` = B and C.
- **Descendants** of A = children + grandchildren + great-grandchildren + ... = B, C, and D.

Getting **children** is easy: `SELECT * FROM tasks WHERE parent_id = ?`. It is a single, flat query.

Getting **descendants** is harder because the tree can be any number of levels deep. You cannot write `WHERE parent_id = ? OR parent_id IN (SELECT id FROM tasks WHERE parent_id = ?)` because that only covers two levels. What if the tree is 5 levels deep? 10? You need a query that can traverse the tree recursively, no matter how deep it is. That is where **recursive CTEs** come in.

### 2. Recursive CTEs (Common Table Expressions)

A **CTE** (Common Table Expression) is a temporary named result set that you define at the top of a query using the `WITH` keyword. Think of it like a temporary table that only exists for the duration of that one query.

A **recursive CTE** is a CTE that references itself. It has two parts:

```sql
WITH RECURSIVE cte_name AS (
    -- PART 1: Base case (the starting point)
    SELECT ... FROM some_table WHERE some_condition

    UNION ALL

    -- PART 2: Recursive case (how to find the next level)
    SELECT ... FROM some_table JOIN cte_name ON some_condition
)
SELECT * FROM cte_name;
```

Here is how it works, step by step:

1. **Base case runs first.** The database executes Part 1. This produces the first set of rows. Think of this as "level 1" of your tree.

2. **Recursive case runs next.** The database executes Part 2. In Part 2, `cte_name` refers to the rows that were just produced. The JOIN finds new rows that are connected to the previous batch. This produces "level 2".

3. **Repeat.** The database runs Part 2 again. Now `cte_name` refers to the level-2 rows. This produces "level 3".

4. **Termination.** When Part 2 produces zero new rows, the recursion stops. `UNION ALL` combines all the rows from every level into a single result set.

For our task tree:

```sql
WITH RECURSIVE descendants AS (
    -- Base case: direct children of the root
    SELECT id, parent_id, title, ... FROM tasks WHERE parent_id = ?

    UNION ALL

    -- Recursive case: children of the previous level
    SELECT t.id, t.parent_id, t.title, ...
    FROM tasks t
    JOIN descendants d ON t.parent_id = d.id
)
SELECT * FROM descendants;
```

Walking through the example tree (root = A):

- **Iteration 1 (base case):** `WHERE parent_id = 'A'` finds B and C. The `descendants` CTE now contains {B, C}.
- **Iteration 2 (recursive):** `JOIN descendants d ON t.parent_id = d.id` looks for tasks whose `parent_id` is B or C. It finds D (parent_id = B). The `descendants` CTE now contains {B, C, D}.
- **Iteration 3 (recursive):** Looks for tasks whose `parent_id` is D. Finds nothing. Recursion stops.
- **Final result:** {B, C, D} — all descendants of A.

**Why `UNION ALL` and not `UNION`?** `UNION` removes duplicates, which adds overhead. In a proper tree (no cycles), there will never be duplicates, so `UNION ALL` is faster and correct.

**Why does it terminate?** Because each iteration only finds rows that have not been found yet (they are at a deeper level). Eventually you reach leaf nodes (tasks with no children), and the recursive case returns zero rows, stopping the loop. If your data has cycles (A is parent of B, B is parent of A), the recursion would loop forever — but a well-designed task tree should never have cycles.

### 3. The `prefixColumns` Helper

When you write a JOIN between two tables (or between a table and a CTE), both sides might have columns with the same name. For example, `tasks` has a column called `id`, and the `descendants` CTE also has a column called `id`. If you write:

```sql
SELECT id FROM tasks t JOIN descendants d ON t.parent_id = d.id
```

The database does not know which `id` you mean — is it `t.id` or `d.id`? This is called **column ambiguity**. To fix it, you prefix each column name with the table alias:

```sql
SELECT t.id, t.parent_id, t.title, ... FROM tasks t JOIN descendants d ON t.parent_id = d.id
```

We already have a constant called `taskColumns` that lists all 15 column names as a comma-separated string:

```go
const taskColumns = "id, short_id, parent_id, project_id, title, description, status, priority, version, due_at, wait_until, recurrence_rule, uda, created_at, modified_at"
```

We use `taskColumns` in the **base case** of the CTE (no prefix needed because there is only one table). But in the **recursive case**, we need `t.id, t.short_id, t.parent_id, ...` to avoid ambiguity with the `descendants` CTE's columns.

Rather than manually writing out all 15 column names with the `t.` prefix, we write a helper function that takes the alias (`"t"`) and the column string, and produces the prefixed version automatically:

```go
prefixColumns("t", "id, short_id, parent_id, ...")
// returns: "t.id, t.short_id, t.parent_id, ..."
```

This is a pure string manipulation function. It splits the column string by commas, trims whitespace from each part, adds the alias and a dot in front, and joins them back together.

### 4. `fmt.Sprintf` Positional Arguments (`%[1]s` and `%[2]s`)

Normally, `fmt.Sprintf` fills placeholders in order:

```go
fmt.Sprintf("%s and %s", "hello", "world")
// → "hello and world"
```

But you can also use **positional arguments** to refer to a specific argument by its position (1-indexed):

```go
fmt.Sprintf("%[1]s and %[2]s and %[1]s again", "hello", "world")
// → "hello and world and hello again"
```

- `%[1]s` means "use the 1st argument as a string"
- `%[2]s` means "use the 2nd argument as a string"

In our `GetDescendants` query, we use this to insert two different versions of the column list:

```go
fmt.Sprintf(`
    WITH RECURSIVE descendants AS (
        SELECT %[1]s FROM tasks WHERE parent_id = ?
        UNION ALL
        SELECT %[2]s FROM tasks t JOIN descendants d ON t.parent_id = d.id
    )
    SELECT * FROM descendants`, taskColumns, prefixColumns("t", taskColumns))
```

- `%[1]s` is replaced with `taskColumns` (unprefixed: `id, short_id, ...`)
- `%[2]s` is replaced with `prefixColumns("t", taskColumns)` (prefixed: `t.id, t.short_id, ...`)

### 5. GetChildren vs GetDescendants

These are two different queries that answer two different questions:

| Method | Question it answers | Depth | SQL technique |
|--------|-------------------|-------|---------------|
| `GetChildren(parentID)` | "What tasks are **directly** under this parent?" | 1 level only | Simple `WHERE parent_id = ?` |
| `GetDescendants(rootID)` | "What tasks are under this root, at **any** depth?" | All levels | Recursive CTE |

Both methods return `[]*domain.Task` and both use the existing `scanRows()` helper to parse the results.

---

## File Structure

| Action | File | What changes |
|--------|------|-------------|
| Modify | `internal/sqlite/task.go` | Replace `GetChildren` and `GetDescendants` stubs with real implementations; add `prefixColumns` helper function |
| Modify | `internal/sqlite/task_test.go` | Append 5 new test functions: `TestTaskGetChildren`, `TestTaskGetChildrenEmpty`, `TestTaskGetDescendants`, `TestTaskGetDescendantsEmpty`, `TestTaskListByRootID` |

---

## Complete Updated Files

Since this phase modifies existing files, here are the **complete** file contents after all changes from Phases 3, 4, **and** 5 are applied. When implementing each task below, make sure your final file matches these exactly.

### Complete `internal/sqlite/task.go` (Phases 3 + 4 + 5)

```go
package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/germanamz/tusk/internal/domain"
	"github.com/google/uuid"
)

// TaskRepo implements repository.TaskRepository using SQLite.
type TaskRepo struct {
	db *sql.DB
}

// NewTaskRepo returns a TaskRepo backed by the given database connection.
func NewTaskRepo(db *sql.DB) *TaskRepo {
	return &TaskRepo{db: db}
}

// taskColumns is the canonical SELECT list for the tasks table.
// Every query and scan must use this exact order.
const taskColumns = `id, short_id, parent_id, project_id, title, description, status, priority, version, due_at, wait_until, recurrence_rule, uda, created_at, modified_at`

// taskScanner is satisfied by *sql.Row and *sql.Rows.
type taskScanner interface {
	Scan(dest ...any) error
}

// scanTask reads one row into a *domain.Task.
func scanTask(sc taskScanner) (*domain.Task, error) {
	var t domain.Task
	var (
		rawID       string
		parentID    sql.NullString
		projectID   sql.NullString
		dueAt       sql.NullString
		waitUntil   sql.NullString
		recurrence  sql.NullString
		udaJSON     []byte
		createdAt   string
		modifiedAt  string
	)

	err := sc.Scan(
		&rawID, &t.ShortID, &parentID, &projectID,
		&t.Title, &t.Description, &t.Status, &t.Priority, &t.Version,
		&dueAt, &waitUntil, &recurrence, &udaJSON,
		&createdAt, &modifiedAt,
	)
	if err != nil {
		return nil, err
	}

	t.ID = uuid.MustParse(rawID)

	if parentID.Valid {
		pid := uuid.MustParse(parentID.String)
		t.ParentID = &pid
	}
	if projectID.Valid {
		pid := uuid.MustParse(projectID.String)
		t.ProjectID = &pid
	}
	if dueAt.Valid {
		parsed, _ := time.Parse(timeFormat, dueAt.String)
		t.DueAt = &parsed
	}
	if waitUntil.Valid {
		parsed, _ := time.Parse(timeFormat, waitUntil.String)
		t.WaitUntil = &parsed
	}
	if recurrence.Valid {
		t.RecurrenceRule = &recurrence.String
	}
	if len(udaJSON) > 0 {
		_ = json.Unmarshal(udaJSON, &t.UDA)
	}

	t.CreatedAt, _ = time.Parse(timeFormat, createdAt)
	t.ModifiedAt, _ = time.Parse(timeFormat, modifiedAt)

	return &t, nil
}

// scanRows iterates over rows and scans each into a *domain.Task.
func (r *TaskRepo) scanRows(rows *sql.Rows) ([]*domain.Task, error) {
	var tasks []*domain.Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

// Create inserts a new task.
func (r *TaskRepo) Create(ctx context.Context, t *domain.Task) error {
	udaJSON, err := json.Marshal(t.UDA)
	if err != nil {
		return fmt.Errorf("marshal uda: %w", err)
	}

	var parentID, projectID, dueAt, waitUntil, recurrence any
	if t.ParentID != nil {
		parentID = t.ParentID.String()
	}
	if t.ProjectID != nil {
		projectID = t.ProjectID.String()
	}
	if t.DueAt != nil {
		dueAt = t.DueAt.Format(timeFormat)
	}
	if t.WaitUntil != nil {
		waitUntil = t.WaitUntil.Format(timeFormat)
	}
	if t.RecurrenceRule != nil {
		recurrence = *t.RecurrenceRule
	}

	_, err = r.db.ExecContext(ctx, fmt.Sprintf(
		`INSERT INTO tasks (%s) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, taskColumns),
		t.ID.String(), t.ShortID, parentID, projectID,
		t.Title, t.Description, t.Status, t.Priority, t.Version,
		dueAt, waitUntil, recurrence, string(udaJSON),
		t.CreatedAt.Format(timeFormat), t.ModifiedAt.Format(timeFormat),
	)
	return err
}

// GetByID returns a single task by primary key.
func (r *TaskRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.Task, error) {
	row := r.db.QueryRowContext(ctx,
		fmt.Sprintf(`SELECT %s FROM tasks WHERE id = ?`, taskColumns),
		id.String())
	t, err := scanTask(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	return t, err
}

// GetByShortID returns a single task by its human-friendly short ID.
func (r *TaskRepo) GetByShortID(ctx context.Context, shortID string) (*domain.Task, error) {
	row := r.db.QueryRowContext(ctx,
		fmt.Sprintf(`SELECT %s FROM tasks WHERE short_id = ?`, taskColumns),
		shortID)
	t, err := scanTask(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	return t, err
}

// Update modifies an existing task with optimistic-locking check on version.
func (r *TaskRepo) Update(ctx context.Context, t *domain.Task) error {
	udaJSON, err := json.Marshal(t.UDA)
	if err != nil {
		return fmt.Errorf("marshal uda: %w", err)
	}

	var parentID, projectID, dueAt, waitUntil, recurrence any
	if t.ParentID != nil {
		parentID = t.ParentID.String()
	}
	if t.ProjectID != nil {
		projectID = t.ProjectID.String()
	}
	if t.DueAt != nil {
		dueAt = t.DueAt.Format(timeFormat)
	}
	if t.WaitUntil != nil {
		waitUntil = t.WaitUntil.Format(timeFormat)
	}
	if t.RecurrenceRule != nil {
		recurrence = *t.RecurrenceRule
	}

	res, err := r.db.ExecContext(ctx, `UPDATE tasks SET
		short_id = ?, parent_id = ?, project_id = ?,
		title = ?, description = ?, status = ?, priority = ?,
		version = version + 1,
		due_at = ?, wait_until = ?, recurrence_rule = ?, uda = ?,
		modified_at = ?
		WHERE id = ? AND version = ?`,
		t.ShortID, parentID, projectID,
		t.Title, t.Description, t.Status, t.Priority,
		dueAt, waitUntil, recurrence, string(udaJSON),
		t.ModifiedAt.Format(timeFormat),
		t.ID.String(), t.Version,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return domain.ErrConflict
	}
	t.Version++
	return nil
}

// Delete removes a task by ID with an optimistic-locking version check.
func (r *TaskRepo) Delete(ctx context.Context, id uuid.UUID, version int) error {
	res, err := r.db.ExecContext(ctx,
		`DELETE FROM tasks WHERE id = ? AND version = ?`,
		id.String(), version)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// buildFilter constructs a WHERE clause and argument list from a TaskFilter.
func buildFilter(f domain.TaskFilter) (string, []any) {
	var clauses []string
	var args []any

	if f.ProjectID != nil {
		clauses = append(clauses, "project_id = ?")
		args = append(args, f.ProjectID.String())
	}
	if f.ParentID != nil {
		clauses = append(clauses, "parent_id = ?")
		args = append(args, f.ParentID.String())
	}
	if f.RootID != nil {
		clauses = append(clauses, fmt.Sprintf(`id IN (
			WITH RECURSIVE descendants AS (
				SELECT id FROM tasks WHERE parent_id = ?
				UNION ALL
				SELECT t.id FROM tasks t JOIN descendants d ON t.parent_id = d.id
			)
			SELECT id FROM descendants
		)`))
		args = append(args, f.RootID.String())
	}
	if len(f.Statuses) > 0 {
		placeholders := strings.Repeat("?,", len(f.Statuses))
		placeholders = placeholders[:len(placeholders)-1]
		clauses = append(clauses, fmt.Sprintf("status IN (%s)", placeholders))
		for _, s := range f.Statuses {
			args = append(args, s)
		}
	}
	if f.PriorityMin != nil {
		clauses = append(clauses, "priority >= ?")
		args = append(args, *f.PriorityMin)
	}
	if f.PriorityMax != nil {
		clauses = append(clauses, "priority <= ?")
		args = append(args, *f.PriorityMax)
	}
	if f.DueAfter != nil {
		clauses = append(clauses, "due_at >= ?")
		args = append(args, f.DueAfter.Format(timeFormat))
	}
	if f.DueBefore != nil {
		clauses = append(clauses, "due_at <= ?")
		args = append(args, f.DueBefore.Format(timeFormat))
	}
	if f.WaitingOnly != nil && *f.WaitingOnly {
		clauses = append(clauses, "wait_until > ?")
		args = append(args, time.Now().Format(timeFormat))
	}

	if len(clauses) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(clauses, " AND "), args
}

// List returns tasks matching the given filter.
func (r *TaskRepo) List(ctx context.Context, f domain.TaskFilter) ([]*domain.Task, error) {
	where, args := buildFilter(f)
	rows, err := r.db.QueryContext(ctx,
		fmt.Sprintf(`SELECT %s FROM tasks%s`, taskColumns, where),
		args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return r.scanRows(rows)
}

// GetChildren returns the direct children of a task (one level only).
func (r *TaskRepo) GetChildren(ctx context.Context, parentID uuid.UUID) ([]*domain.Task, error) {
	rows, err := r.db.QueryContext(ctx,
		fmt.Sprintf(`SELECT %s FROM tasks WHERE parent_id = ?`, taskColumns),
		parentID.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return r.scanRows(rows)
}

// GetDescendants returns all descendants of a task at any depth using a recursive CTE.
func (r *TaskRepo) GetDescendants(ctx context.Context, rootID uuid.UUID) ([]*domain.Task, error) {
	rows, err := r.db.QueryContext(ctx, fmt.Sprintf(`
		WITH RECURSIVE descendants AS (
			SELECT %[1]s FROM tasks WHERE parent_id = ?
			UNION ALL
			SELECT %[2]s FROM tasks t JOIN descendants d ON t.parent_id = d.id
		)
		SELECT * FROM descendants`, taskColumns, prefixColumns("t", taskColumns)),
		rootID.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return r.scanRows(rows)
}

// prefixColumns adds a table alias prefix to each column name.
// e.g. prefixColumns("t", "id, name") -> "t.id, t.name"
func prefixColumns(alias, cols string) string {
	parts := strings.Split(cols, ",")
	for i, p := range parts {
		parts[i] = alias + "." + strings.TrimSpace(p)
	}
	return strings.Join(parts, ", ")
}
```

### Complete `internal/sqlite/task_test.go` (Phases 3 + 4 + 5)

```go
package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/germanamz/tusk/internal/domain"
	"github.com/google/uuid"
)

// newTestTask returns a valid Task with random fields, ready to insert.
func newTestTask() *domain.Task {
	now := time.Now().Truncate(time.Second).UTC()
	return &domain.Task{
		ID:          uuid.New(),
		ShortID:     uuid.New().String()[:8],
		Title:       "test task",
		Description: "description",
		Status:      "pending",
		Priority:    3,
		Version:     1,
		UDA:         map[string]any{"key": "val"},
		CreatedAt:   now,
		ModifiedAt:  now,
	}
}

// mustCreateTask inserts a task and fails the test on error.
func mustCreateTask(t *testing.T, repo *TaskRepo, task *domain.Task) {
	t.Helper()
	if err := repo.Create(context.Background(), task); err != nil {
		t.Fatalf("mustCreateTask: %v", err)
	}
}

func TestTaskCreate(t *testing.T) {
	s := testStore(t)
	repo := NewTaskRepo(s.DB())
	task := newTestTask()
	if err := repo.Create(context.Background(), task); err != nil {
		t.Fatal(err)
	}
}

func TestTaskGetByID(t *testing.T) {
	s := testStore(t)
	repo := NewTaskRepo(s.DB())
	task := newTestTask()
	mustCreateTask(t, repo, task)
	got, err := repo.GetByID(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != task.ID {
		t.Fatalf("expected ID %s, got %s", task.ID, got.ID)
	}
}

func TestTaskGetByIDNotFound(t *testing.T) {
	s := testStore(t)
	repo := NewTaskRepo(s.DB())
	_, err := repo.GetByID(context.Background(), uuid.New())
	if err != domain.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestTaskGetByShortID(t *testing.T) {
	s := testStore(t)
	repo := NewTaskRepo(s.DB())
	task := newTestTask()
	mustCreateTask(t, repo, task)
	got, err := repo.GetByShortID(context.Background(), task.ShortID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ShortID != task.ShortID {
		t.Fatalf("expected ShortID %s, got %s", task.ShortID, got.ShortID)
	}
}

func TestTaskUpdate(t *testing.T) {
	s := testStore(t)
	repo := NewTaskRepo(s.DB())
	task := newTestTask()
	mustCreateTask(t, repo, task)
	task.Title = "updated"
	task.ModifiedAt = time.Now().Truncate(time.Second).UTC()
	if err := repo.Update(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	got, _ := repo.GetByID(context.Background(), task.ID)
	if got.Title != "updated" {
		t.Fatalf("expected title 'updated', got %q", got.Title)
	}
	if got.Version != 2 {
		t.Fatalf("expected version 2, got %d", got.Version)
	}
}

func TestTaskUpdateConflict(t *testing.T) {
	s := testStore(t)
	repo := NewTaskRepo(s.DB())
	task := newTestTask()
	mustCreateTask(t, repo, task)
	task.Version = 999
	err := repo.Update(context.Background(), task)
	if err != domain.ErrConflict {
		t.Fatalf("expected ErrConflict, got %v", err)
	}
}

func TestTaskDelete(t *testing.T) {
	s := testStore(t)
	repo := NewTaskRepo(s.DB())
	task := newTestTask()
	mustCreateTask(t, repo, task)
	if err := repo.Delete(context.Background(), task.ID, task.Version); err != nil {
		t.Fatal(err)
	}
	_, err := repo.GetByID(context.Background(), task.ID)
	if err != domain.ErrNotFound {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestTaskDeleteNotFound(t *testing.T) {
	s := testStore(t)
	repo := NewTaskRepo(s.DB())
	err := repo.Delete(context.Background(), uuid.New(), 1)
	if err != domain.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestTaskList(t *testing.T) {
	s := testStore(t)
	repo := NewTaskRepo(s.DB())
	ctx := context.Background()
	t1 := newTestTask(); t1.Status = "pending"; mustCreateTask(t, repo, t1)
	t2 := newTestTask(); t2.Status = "active"; mustCreateTask(t, repo, t2)
	tasks, err := repo.List(ctx, domain.TaskFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(tasks))
	}
}

func TestTaskListByStatus(t *testing.T) {
	s := testStore(t)
	repo := NewTaskRepo(s.DB())
	ctx := context.Background()
	t1 := newTestTask(); t1.Status = "pending"; mustCreateTask(t, repo, t1)
	t2 := newTestTask(); t2.Status = "active"; mustCreateTask(t, repo, t2)
	tasks, err := repo.List(ctx, domain.TaskFilter{Statuses: []string{"active"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
}

// ── Phase 5: Hierarchy tests ───────────────────────────────────────────

func TestTaskGetChildren(t *testing.T) {
	s := testStore(t)
	repo := NewTaskRepo(s.DB())
	ctx := context.Background()
	parent := newTestTask(); mustCreateTask(t, repo, parent)
	child1 := newTestTask(); child1.ParentID = &parent.ID; mustCreateTask(t, repo, child1)
	child2 := newTestTask(); child2.ParentID = &parent.ID; mustCreateTask(t, repo, child2)
	grandchild := newTestTask(); grandchild.ParentID = &child1.ID; mustCreateTask(t, repo, grandchild)
	children, err := repo.GetChildren(ctx, parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(children) != 2 {
		t.Fatalf("expected 2 children, got %d", len(children))
	}
}

func TestTaskGetChildrenEmpty(t *testing.T) {
	s := testStore(t)
	repo := NewTaskRepo(s.DB())
	task := newTestTask(); mustCreateTask(t, repo, task)
	children, err := repo.GetChildren(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(children) != 0 {
		t.Fatalf("expected 0 children, got %d", len(children))
	}
}

func TestTaskGetDescendants(t *testing.T) {
	s := testStore(t)
	repo := NewTaskRepo(s.DB())
	ctx := context.Background()
	root := newTestTask(); mustCreateTask(t, repo, root)
	child1 := newTestTask(); child1.ParentID = &root.ID; mustCreateTask(t, repo, child1)
	child2 := newTestTask(); child2.ParentID = &root.ID; mustCreateTask(t, repo, child2)
	grandchild := newTestTask(); grandchild.ParentID = &child1.ID; mustCreateTask(t, repo, grandchild)
	descendants, err := repo.GetDescendants(ctx, root.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(descendants) != 3 {
		t.Fatalf("expected 3 descendants, got %d", len(descendants))
	}
	ids := map[uuid.UUID]bool{}
	for _, d := range descendants {
		ids[d.ID] = true
	}
	for _, expected := range []uuid.UUID{child1.ID, child2.ID, grandchild.ID} {
		if !ids[expected] {
			t.Fatalf("missing descendant %s", expected)
		}
	}
}

func TestTaskGetDescendantsEmpty(t *testing.T) {
	s := testStore(t)
	repo := NewTaskRepo(s.DB())
	task := newTestTask(); mustCreateTask(t, repo, task)
	descendants, err := repo.GetDescendants(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(descendants) != 0 {
		t.Fatalf("expected 0 descendants, got %d", len(descendants))
	}
}

func TestTaskListByRootID(t *testing.T) {
	s := testStore(t)
	repo := NewTaskRepo(s.DB())
	ctx := context.Background()
	root := newTestTask(); mustCreateTask(t, repo, root)
	child := newTestTask(); child.ParentID = &root.ID; mustCreateTask(t, repo, child)
	grandchild := newTestTask(); grandchild.ParentID = &child.ID; mustCreateTask(t, repo, grandchild)
	unrelated := newTestTask(); mustCreateTask(t, repo, unrelated)
	tasks, err := repo.List(ctx, domain.TaskFilter{RootID: &root.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 2 {
		t.Fatalf("expected 2 descendants via List, got %d", len(tasks))
	}
}
```

---

## Tasks

### Task 1: Append Failing Tests

**Files:**
- Modify: `internal/sqlite/task_test.go`

The goal of this task is to write the tests **first**, before writing the implementation. This is called "test-driven development" (TDD). You write tests that describe the behavior you want, watch them fail (because the code does not exist yet), then write the code to make them pass.

- [ ] **Step 1: Append the five new test functions to `task_test.go`**

Open `internal/sqlite/task_test.go` and append the following five test functions at the end of the file. Do **not** modify any existing tests — just add these below them.

```go
// ── Phase 5: Hierarchy tests ───────────────────────────────────────────

func TestTaskGetChildren(t *testing.T) {
	s := testStore(t)
	repo := NewTaskRepo(s.DB())
	ctx := context.Background()
	parent := newTestTask(); mustCreateTask(t, repo, parent)
	child1 := newTestTask(); child1.ParentID = &parent.ID; mustCreateTask(t, repo, child1)
	child2 := newTestTask(); child2.ParentID = &parent.ID; mustCreateTask(t, repo, child2)
	grandchild := newTestTask(); grandchild.ParentID = &child1.ID; mustCreateTask(t, repo, grandchild)
	children, err := repo.GetChildren(ctx, parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(children) != 2 {
		t.Fatalf("expected 2 children, got %d", len(children))
	}
}

func TestTaskGetChildrenEmpty(t *testing.T) {
	s := testStore(t)
	repo := NewTaskRepo(s.DB())
	task := newTestTask(); mustCreateTask(t, repo, task)
	children, err := repo.GetChildren(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(children) != 0 {
		t.Fatalf("expected 0 children, got %d", len(children))
	}
}

func TestTaskGetDescendants(t *testing.T) {
	s := testStore(t)
	repo := NewTaskRepo(s.DB())
	ctx := context.Background()
	root := newTestTask(); mustCreateTask(t, repo, root)
	child1 := newTestTask(); child1.ParentID = &root.ID; mustCreateTask(t, repo, child1)
	child2 := newTestTask(); child2.ParentID = &root.ID; mustCreateTask(t, repo, child2)
	grandchild := newTestTask(); grandchild.ParentID = &child1.ID; mustCreateTask(t, repo, grandchild)
	descendants, err := repo.GetDescendants(ctx, root.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(descendants) != 3 {
		t.Fatalf("expected 3 descendants, got %d", len(descendants))
	}
	ids := map[uuid.UUID]bool{}
	for _, d := range descendants {
		ids[d.ID] = true
	}
	for _, expected := range []uuid.UUID{child1.ID, child2.ID, grandchild.ID} {
		if !ids[expected] {
			t.Fatalf("missing descendant %s", expected)
		}
	}
}

func TestTaskGetDescendantsEmpty(t *testing.T) {
	s := testStore(t)
	repo := NewTaskRepo(s.DB())
	task := newTestTask(); mustCreateTask(t, repo, task)
	descendants, err := repo.GetDescendants(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(descendants) != 0 {
		t.Fatalf("expected 0 descendants, got %d", len(descendants))
	}
}

func TestTaskListByRootID(t *testing.T) {
	s := testStore(t)
	repo := NewTaskRepo(s.DB())
	ctx := context.Background()
	root := newTestTask(); mustCreateTask(t, repo, root)
	child := newTestTask(); child.ParentID = &root.ID; mustCreateTask(t, repo, child)
	grandchild := newTestTask(); grandchild.ParentID = &child.ID; mustCreateTask(t, repo, grandchild)
	unrelated := newTestTask(); mustCreateTask(t, repo, unrelated)
	tasks, err := repo.List(ctx, domain.TaskFilter{RootID: &root.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 2 {
		t.Fatalf("expected 2 descendants via List, got %d", len(tasks))
	}
}
```

- [ ] **Step 2: Verify the tests compile but fail**

Run:
```bash
cd /Users/germanamz/projects/tusk && go test ./internal/sqlite/ -run "TestTaskGetChildren|TestTaskGetDescendants|TestTaskListByRootID" -v
```

Expected: The tests should compile (no syntax errors) but **fail** because `GetChildren` and `GetDescendants` still return `"not implemented"` errors. You should see output like:

```
--- FAIL: TestTaskGetChildren (...)
    task_test.go:XX: not implemented
```

This is the expected behavior at this stage. The tests are correctly detecting that the stubs do not work yet.

- [ ] **Step 3: Commit**

```bash
git add internal/sqlite/task_test.go
git commit -m "test(sqlite): add hierarchy tests for GetChildren, GetDescendants, and ListByRootID"
```

---

### Task 2: Replace Stubs with Real Implementations

**Files:**
- Modify: `internal/sqlite/task.go`

Now we make the failing tests pass by replacing the stub methods with real SQL queries and adding the `prefixColumns` helper.

- [ ] **Step 1: Add the `prefixColumns` helper function**

Add the following function to `internal/sqlite/task.go`. You can place it at the bottom of the file (after the `List` method):

```go
// prefixColumns adds a table alias prefix to each column name.
// e.g. prefixColumns("t", "id, name") -> "t.id, t.name"
func prefixColumns(alias, cols string) string {
	parts := strings.Split(cols, ",")
	for i, p := range parts {
		parts[i] = alias + "." + strings.TrimSpace(p)
	}
	return strings.Join(parts, ", ")
}
```

**How this works line by line:**

1. `strings.Split(cols, ",")` — Takes the comma-separated column string (like `"id, short_id, parent_id"`) and splits it into a slice of strings: `["id", " short_id", " parent_id"]`. Notice some parts have leading spaces.
2. `for i, p := range parts` — Loops over each part.
3. `strings.TrimSpace(p)` — Removes leading and trailing whitespace from the part. `" short_id"` becomes `"short_id"`.
4. `alias + "." + ...` — Prepends the alias and a dot. `"short_id"` becomes `"t.short_id"`.
5. `strings.Join(parts, ", ")` — Joins all the prefixed parts back together with commas.

- [ ] **Step 2: Replace the `GetChildren` stub**

Find the existing `GetChildren` method in `task.go` (it currently returns `errors.New("not implemented")`) and replace its **entire body** with:

```go
func (r *TaskRepo) GetChildren(ctx context.Context, parentID uuid.UUID) ([]*domain.Task, error) {
	rows, err := r.db.QueryContext(ctx,
		fmt.Sprintf(`SELECT %s FROM tasks WHERE parent_id = ?`, taskColumns),
		parentID.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return r.scanRows(rows)
}
```

**How this works:**

- `fmt.Sprintf(`SELECT %s FROM tasks WHERE parent_id = ?`, taskColumns)` builds the full SQL query by inserting all 15 column names into the SELECT clause.
- `parentID.String()` converts the Go `uuid.UUID` to a string because SQLite stores UUIDs as text.
- `defer rows.Close()` ensures the database cursor is closed when the function returns, even if `scanRows` returns an error.
- `r.scanRows(rows)` iterates over every returned row and converts each one into a `*domain.Task`.

This is a simple, flat query. It only returns tasks whose `parent_id` exactly matches the given ID — meaning only **direct** children, not grandchildren.

- [ ] **Step 3: Replace the `GetDescendants` stub**

Find the existing `GetDescendants` method and replace its **entire body** with:

```go
func (r *TaskRepo) GetDescendants(ctx context.Context, rootID uuid.UUID) ([]*domain.Task, error) {
	rows, err := r.db.QueryContext(ctx, fmt.Sprintf(`
		WITH RECURSIVE descendants AS (
			SELECT %[1]s FROM tasks WHERE parent_id = ?
			UNION ALL
			SELECT %[2]s FROM tasks t JOIN descendants d ON t.parent_id = d.id
		)
		SELECT * FROM descendants`, taskColumns, prefixColumns("t", taskColumns)),
		rootID.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return r.scanRows(rows)
}
```

**How this works, piece by piece:**

1. **`WITH RECURSIVE descendants AS (...)`** — Declares a recursive CTE named `descendants`. See the Key Concepts section above for a full explanation of recursive CTEs.

2. **`SELECT %[1]s FROM tasks WHERE parent_id = ?`** — The base case. `%[1]s` is replaced by `taskColumns` (the first argument to `Sprintf`). This selects all direct children of `rootID`.

3. **`UNION ALL`** — Combines the base case results with the recursive case results. Each iteration adds more rows.

4. **`SELECT %[2]s FROM tasks t JOIN descendants d ON t.parent_id = d.id`** — The recursive case. `%[2]s` is replaced by `prefixColumns("t", taskColumns)` (the second argument to `Sprintf`), which produces `t.id, t.short_id, t.parent_id, ...`. The `t.` prefix is necessary because both the `tasks` table (aliased as `t`) and the `descendants` CTE (aliased as `d`) have columns with the same names. Without the prefix, SQLite would not know which `id` you mean. The JOIN condition `t.parent_id = d.id` means "find tasks whose parent is one of the previously found descendants."

5. **`SELECT * FROM descendants`** — After the recursion finishes, select everything from the CTE. This is the final result set.

6. **`rootID.String()`** — The single `?` parameter in the base case is bound to the root task's ID.

- [ ] **Step 4: Verify the file compiles**

Run:
```bash
cd /Users/germanamz/projects/tusk && go build ./internal/sqlite/
```

Expected: No output (success). If you see errors, double-check that `prefixColumns` is defined, that the import for `"strings"` exists (it should already be there from Phase 3), and that you did not accidentally delete any existing methods.

- [ ] **Step 5: Run the new hierarchy tests**

Run:
```bash
cd /Users/germanamz/projects/tusk && go test ./internal/sqlite/ -run "TestTaskGetChildren|TestTaskGetDescendants|TestTaskListByRootID" -v
```

Expected: All five tests pass:
```
--- PASS: TestTaskGetChildren
--- PASS: TestTaskGetChildrenEmpty
--- PASS: TestTaskGetDescendants
--- PASS: TestTaskGetDescendantsEmpty
--- PASS: TestTaskListByRootID
PASS
```

Here is what each test validates:

- **TestTaskGetChildren** — Creates a parent with 2 children and 1 grandchild. Calls `GetChildren(parent.ID)`. Expects exactly 2 results (the grandchild should NOT appear because `GetChildren` only returns direct children).
- **TestTaskGetChildrenEmpty** — Creates a lone task with no children. Calls `GetChildren(task.ID)`. Expects 0 results (not an error — an empty slice is a valid result).
- **TestTaskGetDescendants** — Creates a root with 2 children and 1 grandchild. Calls `GetDescendants(root.ID)`. Expects exactly 3 results (all descendants at all levels). Also verifies that each expected ID is present in the result.
- **TestTaskGetDescendantsEmpty** — Creates a lone task with no descendants. Calls `GetDescendants(task.ID)`. Expects 0 results.
- **TestTaskListByRootID** — Creates a root with a child and grandchild, plus an unrelated task. Calls `List` with a `TaskFilter{RootID: &root.ID}`. Expects exactly 2 results. This test validates that Phase 4's `buildFilter` function correctly generates a recursive CTE for the `RootID` filter, and that it works with real hierarchical data. The unrelated task should NOT appear in the results.

- [ ] **Step 6: Commit**

```bash
git add internal/sqlite/task.go
git commit -m "feat(sqlite): implement GetChildren, GetDescendants, and prefixColumns helper"
```

---

### Final Verification

- [ ] **Run ALL task tests together**

This ensures that nothing from earlier phases was broken by the new code:

```bash
cd /Users/germanamz/projects/tusk && go test ./internal/sqlite/ -v
```

Expected: Every test passes — the CRUD tests from Phase 3, the filter tests from Phase 4, and the hierarchy tests from Phase 5. You should see output like:

```
--- PASS: TestTaskCreate
--- PASS: TestTaskGetByID
--- PASS: TestTaskGetByIDNotFound
--- PASS: TestTaskGetByShortID
--- PASS: TestTaskUpdate
--- PASS: TestTaskUpdateConflict
--- PASS: TestTaskDelete
--- PASS: TestTaskDeleteNotFound
--- PASS: TestTaskList
--- PASS: TestTaskListByStatus
--- PASS: TestTaskGetChildren
--- PASS: TestTaskGetChildrenEmpty
--- PASS: TestTaskGetDescendants
--- PASS: TestTaskGetDescendantsEmpty
--- PASS: TestTaskListByRootID
PASS
```

- [ ] **Run go vet**

```bash
cd /Users/germanamz/projects/tusk && go vet ./internal/sqlite/
```

Expected: No output (clean).
