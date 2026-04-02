# SQLite Phase 4 --- TaskRepo Filters Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the placeholder `List` method in `TaskRepo` with a real implementation that dynamically builds SQL `WHERE` clauses from a `TaskFilter` struct. Also add the `buildFilter` helper function that does the actual SQL generation. This is the most complex SQL-building code in the Tusk project.

**Module:** `github.com/germanamz/tusk`

**Tech Stack:** Go 1.26, SQLite (via `database/sql` + CGo driver), `github.com/google/uuid`

---

## What Is Tusk?

Tusk is a terminal-based task manager written in Go. It stores tasks, projects, tags, and workflows in a local SQLite database. The codebase is organized into layers:

- `internal/domain/` --- Pure data structs (like `Task`, `Project`, `TaskFilter`) with no business logic.
- `internal/repository/` --- Go interfaces that describe what a storage layer must provide (like `TaskRepository`).
- `internal/sqlite/` --- The SQLite implementation of those repository interfaces. This is the layer we are working in.
- `internal/service/` --- Business logic (not relevant for this phase).

---

## What Prior Phases Produced

Before you start, you need to understand what code already exists. Do **not** rewrite or delete any of this.

### Phase 1 --- Store (`internal/sqlite/store.go`)

- A constant `timeFormat = "2006-01-02T15:04:05.000Z"` used to format Go `time.Time` values into strings that SQLite can store and compare.
- In `store_test.go`: a helper `testStore(t)` that creates an in-memory SQLite database with all migrations applied (so every table exists), and `mustTimeNow()` that returns the current time truncated to millisecond precision.

### Phase 2 --- ProjectRepo (`internal/sqlite/project.go`)

- `ProjectRepo` struct with `NewProjectRepo(db *sql.DB)` constructor.
- Full CRUD operations for projects. We need `ProjectRepo` in our tests to create projects that tasks can reference via `ProjectID`.

### Phase 3 --- TaskRepo (`internal/sqlite/task.go`)

This is the file you are modifying. It already contains:

- `TaskRepo` struct with a `db *sql.DB` field.
- `NewTaskRepo(db *sql.DB) *TaskRepo` constructor.
- `const taskColumns` --- a string listing every column in the SELECT clause (e.g., `"id, short_id, parent_id, ..."`).
- `scanTask(taskScanner)` --- a function that takes a row scanner and parses it into a `*domain.Task`.
- `scanRows(*sql.Rows)` --- a method on `TaskRepo` that iterates over multiple rows, calling `scanTask` for each.
- `Create`, `GetByID`, `GetByShortID`, `Update`, `Delete` --- fully working CRUD methods.
- `List` method --- currently a stub that returns `fmt.Errorf("not implemented: see Phase 4")`. **You replace this.**
- The file already imports: `context`, `database/sql`, `encoding/json`, `errors`, `fmt`, `strings`, `time`, `domain`, `uuid`.

### Phase 3 --- Task tests (`internal/sqlite/task_test.go`)

- `newTestTask()` --- creates a `*domain.Task` with a random UUID and ShortID, sensible defaults.
- `mustCreateTask(t, repo, task)` --- calls `repo.Create` and fails the test if it errors.
- Several CRUD test functions (`TestTaskCreate`, `TestTaskGetByID`, etc.). **Do not delete these.** You append new tests after them.

---

## Key Concepts You Need to Understand

This section explains the core ideas behind the code you are about to write. If any of this is new to you, read it carefully before touching code.

### 1. Dynamic SQL WHERE Clauses

When a user calls `List`, they pass a `TaskFilter` struct. Some fields might be set (non-nil) and others might be left empty (nil or zero-length). We need to build a SQL query that only includes conditions for the fields that are actually set.

For example, if the user sets `Statuses: []string{"active"}` and `PriorityMin: &2` but leaves everything else nil, the generated SQL should be:

```sql
SELECT ... FROM tasks WHERE status IN (?) AND priority >= ?
```

Not:

```sql
SELECT ... FROM tasks WHERE project_id = ? AND parent_id = ? AND status IN (?) AND ...
```

The way we do this is:

1. Start with an empty slice of condition strings: `var conditions []string`
2. For each non-nil filter field, append a SQL fragment to `conditions` and append the corresponding value to `args`.
3. At the end, join all conditions with `" AND "` using `strings.Join(conditions, " AND ")`.
4. If no conditions were added, the WHERE clause is empty and we select all rows.

### 2. Parameterized Queries (Why `?` Placeholders Matter)

You might wonder: why not just do `fmt.Sprintf("status = '%s'", status)`? This is called **string interpolation** and it is dangerous because it opens the door to **SQL injection attacks**. If someone passes a malicious string as a status value, it could modify the query in unexpected ways.

Instead, we use `?` placeholders. The database driver replaces each `?` with a properly escaped value from the `args` slice. This is safe because the driver treats the value as data, never as SQL code.

```go
// DANGEROUS - never do this:
query := fmt.Sprintf("SELECT * FROM tasks WHERE status = '%s'", userInput)

// SAFE - always do this:
query := "SELECT * FROM tasks WHERE status = ?"
rows, err := db.QueryContext(ctx, query, userInput)
```

### 3. SQL `IN` Clauses

When filtering by multiple statuses (e.g., "show me tasks that are pending OR active"), we use the SQL `IN` operator:

```sql
SELECT ... FROM tasks WHERE status IN ('pending', 'active')
```

With parameterized queries, we need one `?` for each value:

```sql
SELECT ... FROM tasks WHERE status IN (?, ?)
-- args: ["pending", "active"]
```

We build the placeholder string dynamically:

```go
placeholders := make([]string, len(filter.Statuses))
for i, s := range filter.Statuses {
    placeholders[i] = "?"
    args = append(args, s)
}
// placeholders is now ["?", "?"]
// strings.Join(placeholders, ",") gives "?,?"
```

### 4. Correlated Subqueries (Tag Filters)

Tags are not stored directly on the `tasks` table. They live in two separate tables:

- `tags` --- each tag has an `id` and a `name` (like "bug" or "urgent").
- `tag_assignments` --- a junction table that links a `task_id` to a `tag_id`.

To filter tasks by tags, we need a **correlated subquery**. "Correlated" means the inner query references a column from the outer query (`tasks.id`).

**Include tags** (`Tags` field --- task must have ALL of these tags):

```sql
(SELECT COUNT(DISTINCT tg.name)
 FROM tag_assignments ta
 JOIN tags tg ON ta.tag_id = tg.id
 WHERE ta.task_id = tasks.id AND tg.name IN (?, ?)) = ?
```

Step by step, this says:
1. Look at the `tag_assignments` table (`ta`) for the current task (`ta.task_id = tasks.id`).
2. Join to the `tags` table (`tg`) to get tag names.
3. Only count tags whose name is in our filter list (`tg.name IN (?, ?)`).
4. Count how many distinct matching tag names there are.
5. That count must equal the total number of tags we are filtering by (`= ?`). This ensures the task has ALL of them, not just some.

**Exclude tags** (`ExcludeTags` field --- task must have NONE of these tags):

```sql
NOT EXISTS (
  SELECT 1 FROM tag_assignments ta
  JOIN tags tg ON ta.tag_id = tg.id
  WHERE ta.task_id = tasks.id AND tg.name IN (?, ?)
)
```

This says: "There must be no row in `tag_assignments` for this task where the tag name is in our exclude list." `NOT EXISTS` returns true when the subquery returns zero rows.

### 5. CTE Prefix for `RootID` (Recursive Descendants)

When the `RootID` filter is set, we want to find all tasks that are descendants of a given root task (children, grandchildren, etc.). This uses a **Common Table Expression (CTE)** with the `RECURSIVE` keyword.

The CTE is prepended to the query as a prefix, before the `SELECT`:

```sql
WITH RECURSIVE descendants(id) AS (
    SELECT id FROM tasks WHERE parent_id = ?
    UNION ALL
    SELECT t.id FROM tasks t JOIN descendants d ON t.parent_id = d.id
)
SELECT ... FROM tasks WHERE tasks.id IN (SELECT id FROM descendants)
```

The CTE works in two parts:
1. **Base case:** Find all direct children of the root task (`parent_id = ?`).
2. **Recursive case:** Find all children of those children, and their children, and so on.

The full recursive CTE implementation will be expanded in Phase 5. For now, we build the prefix here and it works correctly.

Note that when `RootID` is set, the CTE arg (`filter.RootID.String()`) must come **first** in the args slice because it appears first in the SQL. The code uses `append([]any{filter.RootID.String()}, args...)` to prepend it.

### 6. `strings.Join` for Combining Conditions

After building all condition fragments, we combine them into a single WHERE clause string:

```go
where := strings.Join(conditions, " AND ")
// If conditions is ["status IN (?)", "priority >= ?"]
// then where is "status IN (?) AND priority >= ?"
```

If `conditions` is empty (no filters set), `strings.Join` returns an empty string `""`, and we skip the WHERE clause entirely --- returning all tasks.

---

## Database Schema (Relevant Tables)

For reference, here are the tables involved in filtering:

```sql
-- The main tasks table (columns relevant to filtering):
-- id TEXT PRIMARY KEY
-- project_id TEXT (nullable, FK to projects)
-- parent_id TEXT (nullable, FK to tasks for tree structure)
-- status TEXT NOT NULL
-- priority INTEGER NOT NULL
-- due_at TEXT (nullable, ISO 8601 timestamp)
-- wait_until TEXT (nullable, ISO 8601 timestamp)

-- Tags and their assignments to tasks:
CREATE TABLE tags (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    color TEXT
);

CREATE TABLE tag_assignments (
    task_id TEXT NOT NULL,
    tag_id TEXT NOT NULL,
    PRIMARY KEY (task_id, tag_id)
);
```

---

## The `TaskFilter` Struct

This struct lives in `internal/domain/filter.go` and was created in a previous phase. Here it is for reference:

```go
type TaskFilter struct {
    ProjectID   *uuid.UUID  // only tasks in this project
    ParentID    *uuid.UUID  // only direct children of this task
    RootID      *uuid.UUID  // all descendants of this task (recursive)
    Statuses    []string    // task status must be one of these (OR match)
    Tags        []string    // task must have ALL of these tags
    ExcludeTags []string    // task must have NONE of these tags
    PriorityMin *int        // priority must be >= this value
    PriorityMax *int        // priority must be <= this value
    DueAfter    *time.Time  // due_at must be after this time
    DueBefore   *time.Time  // due_at must be before this time
    WaitingOnly *bool       // if true, only tasks where wait_until is in the future
}
```

Fields that are `nil` (pointer fields) or empty (slice fields) are ignored --- no condition is added for them.

---

## Files Modified in This Phase

| Action | File | What Changes |
|--------|------|-------------|
| Modify | `internal/sqlite/task.go` | Replace `List` stub, add `buildFilter` function |
| Modify | `internal/sqlite/task_test.go` | Append 9 new filter test functions |

**No other files are created or modified.**

---

## Task 1: Append Failing Tests to `task_test.go`

**File:** `internal/sqlite/task_test.go`

We follow **test-driven development (TDD)**: write the tests first, watch them fail, then write the code to make them pass.

**What to do:** Open `task_test.go`. Scroll to the very bottom. Append all 9 test functions below **after** the existing CRUD tests. Do **not** delete or modify any existing test functions.

You will also need to add imports for `time` and for `NewProjectRepo` (used in `TestTaskListByProject`). The file already imports `context`, `testing`, `domain`, `repository`, and `uuid`. Make sure the import block includes `time` as well.

- [ ] **Step 1: Add new imports if needed**

The existing import block in `task_test.go` should already have most of what we need. Verify it includes `time`. If not, add it. The final import block should look like:

```go
import (
    "context"
    "testing"
    "time"

    "github.com/germanamz/tusk/internal/domain"
    "github.com/germanamz/tusk/internal/repository"
    "github.com/google/uuid"
)
```

Note: `repository` is already imported for the Phase 3 interface satisfaction test. If it is not there, add it.

- [ ] **Step 2: Append all 9 test functions**

Add these functions at the end of the file. Each test follows the same pattern:
1. Create an in-memory store with `testStore(t)`.
2. Create a `TaskRepo` (and sometimes a `ProjectRepo`).
3. Create some test tasks with specific field values.
4. Call `repo.List(ctx, domain.TaskFilter{...})` with the filter you want to test.
5. Assert the returned slice has the expected length and contents.

Here is the **complete content to append** to the bottom of `task_test.go`:

```go
func TestTaskListEmpty(t *testing.T) {
	s := testStore(t)
	repo := NewTaskRepo(s.DB())
	tasks, err := repo.List(context.Background(), domain.TaskFilter{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(tasks) != 0 {
		t.Fatalf("expected 0 tasks, got %d", len(tasks))
	}
}

func TestTaskListAll(t *testing.T) {
	s := testStore(t)
	repo := NewTaskRepo(s.DB())
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		mustCreateTask(t, repo, newTestTask())
	}
	tasks, err := repo.List(ctx, domain.TaskFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 3 {
		t.Fatalf("expected 3 tasks, got %d", len(tasks))
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
	if tasks[0].Status != "active" {
		t.Fatalf("expected active, got %s", tasks[0].Status)
	}
}

func TestTaskListByStatusMultiple(t *testing.T) {
	s := testStore(t)
	repo := NewTaskRepo(s.DB())
	ctx := context.Background()
	for _, status := range []string{"pending", "active", "completed"} {
		task := newTestTask(); task.Status = status; mustCreateTask(t, repo, task)
	}
	tasks, err := repo.List(ctx, domain.TaskFilter{Statuses: []string{"pending", "active"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(tasks))
	}
}

func TestTaskListByProject(t *testing.T) {
	s := testStore(t)
	repo := NewTaskRepo(s.DB())
	projRepo := NewProjectRepo(s.DB())
	ctx := context.Background()
	proj := &domain.Project{
		ID: uuid.New(), Name: "backend", DefaultWorkflow: "default",
		CreatedAt: time.Now().UTC().Truncate(time.Millisecond),
	}
	if err := projRepo.Create(ctx, proj); err != nil {
		t.Fatal(err)
	}
	t1 := newTestTask(); t1.ProjectID = &proj.ID; mustCreateTask(t, repo, t1)
	t2 := newTestTask(); mustCreateTask(t, repo, t2)
	tasks, err := repo.List(ctx, domain.TaskFilter{ProjectID: &proj.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
}

func TestTaskListByPriority(t *testing.T) {
	s := testStore(t)
	repo := NewTaskRepo(s.DB())
	ctx := context.Background()
	for _, p := range []int{1, 2, 3, 4} {
		task := newTestTask(); task.Priority = p; mustCreateTask(t, repo, task)
	}
	min, max := 2, 3
	tasks, err := repo.List(ctx, domain.TaskFilter{PriorityMin: &min, PriorityMax: &max})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(tasks))
	}
}

func TestTaskListByDueDate(t *testing.T) {
	s := testStore(t)
	repo := NewTaskRepo(s.DB())
	ctx := context.Background()
	d1 := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	d2 := time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)
	d3 := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	for _, d := range []*time.Time{&d1, &d2, &d3} {
		task := newTestTask(); task.DueAt = d; mustCreateTask(t, repo, task)
	}
	after := time.Date(2026, 5, 31, 0, 0, 0, 0, time.UTC)
	before := time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC)
	tasks, err := repo.List(ctx, domain.TaskFilter{DueAfter: &after, DueBefore: &before})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(tasks))
	}
}

func TestTaskListByParent(t *testing.T) {
	s := testStore(t)
	repo := NewTaskRepo(s.DB())
	ctx := context.Background()
	parent := newTestTask(); mustCreateTask(t, repo, parent)
	child := newTestTask(); child.ParentID = &parent.ID; mustCreateTask(t, repo, child)
	orphan := newTestTask(); mustCreateTask(t, repo, orphan)
	tasks, err := repo.List(ctx, domain.TaskFilter{ParentID: &parent.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 child, got %d", len(tasks))
	}
}

func TestTaskListWaitingOnly(t *testing.T) {
	s := testStore(t)
	repo := NewTaskRepo(s.DB())
	ctx := context.Background()
	future := time.Now().UTC().Add(24 * time.Hour)
	past := time.Now().UTC().Add(-24 * time.Hour)
	t1 := newTestTask(); t1.WaitUntil = &future; mustCreateTask(t, repo, t1)
	t2 := newTestTask(); t2.WaitUntil = &past; mustCreateTask(t, repo, t2)
	t3 := newTestTask(); mustCreateTask(t, repo, t3)
	waitingOnly := true
	tasks, err := repo.List(ctx, domain.TaskFilter{WaitingOnly: &waitingOnly})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 waiting task, got %d", len(tasks))
	}
}
```

- [ ] **Step 3: Verify tests compile but fail**

Run:
```bash
cd /Users/germanamz/projects/tusk && go test ./internal/sqlite/ -run "TestTaskList" -v
```

Expected: The tests should **compile** but **fail** because `List` still returns `"not implemented: see Phase 4"`. This confirms the tests are wired up correctly. You should see errors like `List: not implemented: see Phase 4`.

- [ ] **Step 4: Commit**

```bash
git add internal/sqlite/task_test.go
git commit -m "test(sqlite): add TaskRepo filter tests (Phase 4, red)"
```

---

## Task 2: Replace List Stub and Add buildFilter in `task.go`

**File:** `internal/sqlite/task.go`

Now we make the tests pass. You need to do two things in this file:

1. **Replace** the existing `List` method (the stub that returns an error) with the real implementation.
2. **Add** a new `buildFilter` function (a package-level function, not a method on `TaskRepo`).

**Do not change anything else in the file.** All the other methods (`Create`, `GetByID`, `GetByShortID`, `Update`, `Delete`, `scanTask`, `scanRows`) stay exactly as they are.

### Where to Put the Code

- The `List` method replacement goes in the same place the stub currently is. Find the method that looks like:

```go
func (r *TaskRepo) List(ctx context.Context, filter domain.TaskFilter) ([]*domain.Task, error) {
	return nil, fmt.Errorf("not implemented: see Phase 4")
}
```

Replace it entirely with the new implementation shown below.

- The `buildFilter` function goes immediately **after** the `List` method. It is a standalone function (not a method on `TaskRepo`) because it does not need access to the database --- it just builds strings.

### Complete Updated `task.go`

Below is the **complete** `task.go` file. This includes everything from Phase 3 (unchanged) plus the new `List` and `buildFilter` code. Copy this entire file to replace the existing `task.go`.

**Why show the whole file?** Because when you are modifying existing code, it is easy to accidentally delete something or put new code in the wrong place. By showing the complete file, you can verify every piece is present.

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

// taskColumns lists every column in the tasks table in SELECT order.
// This constant is used by all query methods so column order stays consistent.
const taskColumns = `id, short_id, parent_id, project_id, title, description,
	status, priority, version, due_at, wait_until, recurrence_rule, uda,
	created_at, modified_at`

// taskScanner is any type that has a Scan method (like *sql.Row or the row
// returned by iterating *sql.Rows). We use this interface so scanTask can
// work with both QueryRow and Query results.
type taskScanner interface {
	Scan(dest ...any) error
}

// scanTask reads one row of task columns into a *domain.Task.
// It handles nullable columns (parent_id, project_id, due_at, wait_until,
// recurrence_rule) via sql.Null* types and parses the JSON-encoded UDA map.
func scanTask(s taskScanner) (*domain.Task, error) {
	var t domain.Task
	var parentID, projectID sql.NullString
	var dueAt, waitUntil sql.NullString
	var recurrenceRule sql.NullString
	var udaJSON sql.NullString
	var createdAt, modifiedAt string

	err := s.Scan(
		&t.ID, &t.ShortID, &parentID, &projectID,
		&t.Title, &t.Description, &t.Status, &t.Priority, &t.Version,
		&dueAt, &waitUntil, &recurrenceRule, &udaJSON,
		&createdAt, &modifiedAt,
	)
	if err != nil {
		return nil, err
	}

	if parentID.Valid {
		id, err := uuid.Parse(parentID.String)
		if err != nil {
			return nil, fmt.Errorf("parse parent_id: %w", err)
		}
		t.ParentID = &id
	}
	if projectID.Valid {
		id, err := uuid.Parse(projectID.String)
		if err != nil {
			return nil, fmt.Errorf("parse project_id: %w", err)
		}
		t.ProjectID = &id
	}
	if dueAt.Valid {
		parsed, err := time.Parse(timeFormat, dueAt.String)
		if err != nil {
			return nil, fmt.Errorf("parse due_at: %w", err)
		}
		t.DueAt = &parsed
	}
	if waitUntil.Valid {
		parsed, err := time.Parse(timeFormat, waitUntil.String)
		if err != nil {
			return nil, fmt.Errorf("parse wait_until: %w", err)
		}
		t.WaitUntil = &parsed
	}
	if recurrenceRule.Valid {
		t.RecurrenceRule = &recurrenceRule.String
	}
	if udaJSON.Valid && udaJSON.String != "" {
		t.UDA = make(map[string]any)
		if err := json.Unmarshal([]byte(udaJSON.String), &t.UDA); err != nil {
			return nil, fmt.Errorf("parse uda: %w", err)
		}
	}

	parsed, err := time.Parse(timeFormat, createdAt)
	if err != nil {
		return nil, fmt.Errorf("parse created_at: %w", err)
	}
	t.CreatedAt = parsed

	parsed, err = time.Parse(timeFormat, modifiedAt)
	if err != nil {
		return nil, fmt.Errorf("parse modified_at: %w", err)
	}
	t.ModifiedAt = parsed

	return &t, nil
}

// TaskRepo implements repository.TaskRepository using SQLite.
type TaskRepo struct {
	db *sql.DB
}

// NewTaskRepo creates a new TaskRepo backed by the given database connection.
func NewTaskRepo(db *sql.DB) *TaskRepo {
	return &TaskRepo{db: db}
}

// scanRows iterates over all rows in the result set, calling scanTask for each
// row and collecting the results into a slice.
func (r *TaskRepo) scanRows(rows *sql.Rows) ([]*domain.Task, error) {
	var tasks []*domain.Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return tasks, nil
}

// Create inserts a new task into the database.
func (r *TaskRepo) Create(ctx context.Context, task *domain.Task) error {
	var parentID, projectID *string
	if task.ParentID != nil {
		s := task.ParentID.String()
		parentID = &s
	}
	if task.ProjectID != nil {
		s := task.ProjectID.String()
		projectID = &s
	}
	var dueAt, waitUntil *string
	if task.DueAt != nil {
		s := task.DueAt.UTC().Format(timeFormat)
		dueAt = &s
	}
	if task.WaitUntil != nil {
		s := task.WaitUntil.UTC().Format(timeFormat)
		waitUntil = &s
	}
	var udaJSON *string
	if task.UDA != nil {
		b, err := json.Marshal(task.UDA)
		if err != nil {
			return fmt.Errorf("marshal uda: %w", err)
		}
		s := string(b)
		udaJSON = &s
	}
	now := time.Now().UTC().Format(timeFormat)
	_, err := r.db.ExecContext(ctx, `INSERT INTO tasks
		(id, short_id, parent_id, project_id, title, description,
		 status, priority, version, due_at, wait_until, recurrence_rule, uda,
		 created_at, modified_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		task.ID.String(), task.ShortID, parentID, projectID,
		task.Title, task.Description, task.Status, task.Priority, task.Version,
		dueAt, waitUntil, task.RecurrenceRule, udaJSON,
		now, now,
	)
	if err != nil {
		return err
	}
	// Update the in-memory struct with the stored timestamps.
	parsed, _ := time.Parse(timeFormat, now)
	task.CreatedAt = parsed
	task.ModifiedAt = parsed
	return nil
}

// GetByID retrieves a task by its full UUID.
func (r *TaskRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.Task, error) {
	row := r.db.QueryRowContext(ctx,
		fmt.Sprintf(`SELECT %s FROM tasks WHERE id = ?`, taskColumns),
		id.String(),
	)
	t, err := scanTask(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	return t, err
}

// GetByShortID retrieves a task by its short (8-character hex) identifier.
func (r *TaskRepo) GetByShortID(ctx context.Context, shortID string) (*domain.Task, error) {
	row := r.db.QueryRowContext(ctx,
		fmt.Sprintf(`SELECT %s FROM tasks WHERE short_id = ?`, taskColumns),
		shortID,
	)
	t, err := scanTask(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	return t, err
}

// Update modifies an existing task. It uses optimistic concurrency control:
// the UPDATE only succeeds if the version in the database matches the expected
// version. On success the task's Version is incremented and ModifiedAt updated.
func (r *TaskRepo) Update(ctx context.Context, task *domain.Task) error {
	var parentID, projectID *string
	if task.ParentID != nil {
		s := task.ParentID.String()
		parentID = &s
	}
	if task.ProjectID != nil {
		s := task.ProjectID.String()
		projectID = &s
	}
	var dueAt, waitUntil *string
	if task.DueAt != nil {
		s := task.DueAt.UTC().Format(timeFormat)
		dueAt = &s
	}
	if task.WaitUntil != nil {
		s := task.WaitUntil.UTC().Format(timeFormat)
		waitUntil = &s
	}
	var udaJSON *string
	if task.UDA != nil {
		b, err := json.Marshal(task.UDA)
		if err != nil {
			return fmt.Errorf("marshal uda: %w", err)
		}
		s := string(b)
		udaJSON = &s
	}
	now := time.Now().UTC().Format(timeFormat)
	res, err := r.db.ExecContext(ctx, `UPDATE tasks SET
		parent_id = ?, project_id = ?, title = ?, description = ?,
		status = ?, priority = ?, version = version + 1,
		due_at = ?, wait_until = ?, recurrence_rule = ?, uda = ?,
		modified_at = ?
		WHERE id = ? AND version = ?`,
		parentID, projectID, task.Title, task.Description,
		task.Status, task.Priority,
		dueAt, waitUntil, task.RecurrenceRule, udaJSON,
		now,
		task.ID.String(), task.Version,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return domain.ErrConflict
	}
	task.Version++
	parsed, _ := time.Parse(timeFormat, now)
	task.ModifiedAt = parsed
	return nil
}

// Delete removes a task by ID, but only if the version matches (optimistic
// concurrency). Returns ErrNotFound if no matching row exists.
func (r *TaskRepo) Delete(ctx context.Context, id uuid.UUID, version int) error {
	res, err := r.db.ExecContext(ctx,
		`DELETE FROM tasks WHERE id = ? AND version = ?`,
		id.String(), version,
	)
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

// List retrieves tasks matching the given filter. An empty filter returns all
// tasks. The filter fields are combined with AND logic --- a task must match
// every non-nil/non-empty filter field to be included.
func (r *TaskRepo) List(ctx context.Context, filter domain.TaskFilter) ([]*domain.Task, error) {
	ctePrefix, where, args := buildFilter(filter)
	query := ctePrefix + fmt.Sprintf(`SELECT %s FROM tasks`, taskColumns)
	if where != "" {
		query += " WHERE " + where
	}
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return r.scanRows(rows)
}

// GetChildren retrieves all direct children of the given parent task.
func (r *TaskRepo) GetChildren(ctx context.Context, parentID uuid.UUID) ([]*domain.Task, error) {
	rows, err := r.db.QueryContext(ctx,
		fmt.Sprintf(`SELECT %s FROM tasks WHERE parent_id = ?`, taskColumns),
		parentID.String(),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return r.scanRows(rows)
}

// GetDescendants retrieves all descendants (children, grandchildren, etc.)
// of the given root task using a recursive CTE.
func (r *TaskRepo) GetDescendants(ctx context.Context, rootID uuid.UUID) ([]*domain.Task, error) {
	rows, err := r.db.QueryContext(ctx, fmt.Sprintf(`
		WITH RECURSIVE descendants(id) AS (
			SELECT id FROM tasks WHERE parent_id = ?
			UNION ALL
			SELECT t.id FROM tasks t JOIN descendants d ON t.parent_id = d.id
		)
		SELECT %s FROM tasks WHERE tasks.id IN (SELECT id FROM descendants)`,
		taskColumns),
		rootID.String(),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return r.scanRows(rows)
}

// buildFilter translates a TaskFilter struct into SQL fragments:
//   - ctePrefix: a WITH RECURSIVE clause (only set when RootID is used)
//   - where: the WHERE clause body (conditions joined by AND)
//   - args: the parameter values corresponding to ? placeholders
//
// This is a standalone function (not a method) because it has no side effects
// and does not need database access --- it only builds strings.
func buildFilter(filter domain.TaskFilter) (ctePrefix string, where string, args []any) {
	var conditions []string

	if filter.ProjectID != nil {
		conditions = append(conditions, "project_id = ?")
		args = append(args, filter.ProjectID.String())
	}
	if filter.ParentID != nil {
		conditions = append(conditions, "parent_id = ?")
		args = append(args, filter.ParentID.String())
	}
	if filter.RootID != nil {
		ctePrefix = `WITH RECURSIVE descendants(id) AS (
			SELECT id FROM tasks WHERE parent_id = ?
			UNION ALL
			SELECT t.id FROM tasks t JOIN descendants d ON t.parent_id = d.id
		) `
		args = append([]any{filter.RootID.String()}, args...)
		conditions = append(conditions, "tasks.id IN (SELECT id FROM descendants)")
	}
	if len(filter.Statuses) > 0 {
		placeholders := make([]string, len(filter.Statuses))
		for i, s := range filter.Statuses {
			placeholders[i] = "?"
			args = append(args, s)
		}
		conditions = append(conditions, fmt.Sprintf("status IN (%s)", strings.Join(placeholders, ",")))
	}
	if len(filter.Tags) > 0 {
		placeholders := make([]string, len(filter.Tags))
		for i, tag := range filter.Tags {
			placeholders[i] = "?"
			args = append(args, tag)
		}
		conditions = append(conditions, fmt.Sprintf(
			`(SELECT COUNT(DISTINCT tg.name) FROM tag_assignments ta
			  JOIN tags tg ON ta.tag_id = tg.id
			  WHERE ta.task_id = tasks.id AND tg.name IN (%s)) = ?`,
			strings.Join(placeholders, ",")))
		args = append(args, len(filter.Tags))
	}
	if len(filter.ExcludeTags) > 0 {
		placeholders := make([]string, len(filter.ExcludeTags))
		for i, tag := range filter.ExcludeTags {
			placeholders[i] = "?"
			args = append(args, tag)
		}
		conditions = append(conditions, fmt.Sprintf(
			`NOT EXISTS (SELECT 1 FROM tag_assignments ta
			 JOIN tags tg ON ta.tag_id = tg.id
			 WHERE ta.task_id = tasks.id AND tg.name IN (%s))`,
			strings.Join(placeholders, ",")))
	}
	if filter.PriorityMin != nil {
		conditions = append(conditions, "priority >= ?")
		args = append(args, *filter.PriorityMin)
	}
	if filter.PriorityMax != nil {
		conditions = append(conditions, "priority <= ?")
		args = append(args, *filter.PriorityMax)
	}
	if filter.DueAfter != nil {
		conditions = append(conditions, "due_at > ?")
		args = append(args, filter.DueAfter.UTC().Format(timeFormat))
	}
	if filter.DueBefore != nil {
		conditions = append(conditions, "due_at < ?")
		args = append(args, filter.DueBefore.UTC().Format(timeFormat))
	}
	if filter.WaitingOnly != nil && *filter.WaitingOnly {
		conditions = append(conditions, "wait_until > ?")
		args = append(args, time.Now().UTC().Format(timeFormat))
	}
	return ctePrefix, strings.Join(conditions, " AND "), args
}
```

- [ ] **Step 1: Replace `task.go` with the complete file above**

Open `internal/sqlite/task.go` and replace its entire contents with the code above. This preserves all Phase 3 code (the struct, constructor, CRUD methods, scan helpers) and adds:
- The updated `List` method (replacing the stub)
- The new `buildFilter` function

- [ ] **Step 2: Verify it compiles**

Run:
```bash
cd /Users/germanamz/projects/tusk && go build ./internal/sqlite/
```

Expected: No output (success). If you see errors, check that:
- All imports are present (the import block should be unchanged from Phase 3).
- The `buildFilter` function is outside of any other function body.
- You did not accidentally delete any closing braces.

- [ ] **Step 3: Run the new filter tests**

Run:
```bash
cd /Users/germanamz/projects/tusk && go test ./internal/sqlite/ -run "TestTaskList" -v
```

Expected: All 9 tests pass:
```
--- PASS: TestTaskListEmpty
--- PASS: TestTaskListAll
--- PASS: TestTaskListByStatus
--- PASS: TestTaskListByStatusMultiple
--- PASS: TestTaskListByProject
--- PASS: TestTaskListByPriority
--- PASS: TestTaskListByDueDate
--- PASS: TestTaskListByParent
--- PASS: TestTaskListWaitingOnly
PASS
```

- [ ] **Step 4: Run ALL sqlite tests (old + new)**

Run:
```bash
cd /Users/germanamz/projects/tusk && go test ./internal/sqlite/ -v
```

Expected: Every test passes --- both the CRUD tests from Phase 3 and the new filter tests from this phase. This confirms you did not break anything.

- [ ] **Step 5: Run vet**

Run:
```bash
cd /Users/germanamz/projects/tusk && go vet ./internal/sqlite/
```

Expected: No output (clean). `go vet` catches common mistakes like unreachable code or incorrect printf format strings.

- [ ] **Step 6: Commit**

```bash
git add internal/sqlite/task.go
git commit -m "feat(sqlite): implement TaskRepo.List with dynamic filter (Phase 4)"
```

---

## Complete Updated `task_test.go`

For reference, here is what the **complete** `task_test.go` should look like after both phases (Phase 3 CRUD tests + Phase 4 filter tests). The Phase 3 tests are shown as placeholders (`// ... existing Phase 3 tests ...`) since their exact content depends on what Phase 3 produced. The important thing is that you **append** the new tests and do not remove the old ones.

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

// --- Phase 3 helpers (already present, do not modify) ---

// Compile-time check that TaskRepo satisfies the TaskRepository interface.
var _ repository.TaskRepository = (*TaskRepo)(nil)

func newTestTask() *domain.Task {
	id := uuid.New()
	return &domain.Task{
		ID:          id,
		ShortID:     id.String()[:8],
		Title:       "Test Task",
		Description: "A test task",
		Status:      "pending",
		Priority:    2,
		Version:     1,
		CreatedAt:   time.Now().UTC().Truncate(time.Millisecond),
		ModifiedAt:  time.Now().UTC().Truncate(time.Millisecond),
	}
}

func mustCreateTask(t *testing.T, repo *TaskRepo, task *domain.Task) {
	t.Helper()
	if err := repo.Create(context.Background(), task); err != nil {
		t.Fatalf("mustCreateTask: %v", err)
	}
}

// --- Phase 3 CRUD tests (already present, do not modify) ---

// TestTaskCreate, TestTaskGetByID, TestTaskGetByShortID, TestTaskUpdate,
// TestTaskDelete, etc. are already here from Phase 3.
// DO NOT DELETE THEM. The new tests go BELOW them.

// ... existing Phase 3 tests ...

// --- Phase 4 filter tests (append these) ---

func TestTaskListEmpty(t *testing.T) {
	s := testStore(t)
	repo := NewTaskRepo(s.DB())
	tasks, err := repo.List(context.Background(), domain.TaskFilter{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(tasks) != 0 {
		t.Fatalf("expected 0 tasks, got %d", len(tasks))
	}
}

func TestTaskListAll(t *testing.T) {
	s := testStore(t)
	repo := NewTaskRepo(s.DB())
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		mustCreateTask(t, repo, newTestTask())
	}
	tasks, err := repo.List(ctx, domain.TaskFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 3 {
		t.Fatalf("expected 3 tasks, got %d", len(tasks))
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
	if tasks[0].Status != "active" {
		t.Fatalf("expected active, got %s", tasks[0].Status)
	}
}

func TestTaskListByStatusMultiple(t *testing.T) {
	s := testStore(t)
	repo := NewTaskRepo(s.DB())
	ctx := context.Background()
	for _, status := range []string{"pending", "active", "completed"} {
		task := newTestTask(); task.Status = status; mustCreateTask(t, repo, task)
	}
	tasks, err := repo.List(ctx, domain.TaskFilter{Statuses: []string{"pending", "active"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(tasks))
	}
}

func TestTaskListByProject(t *testing.T) {
	s := testStore(t)
	repo := NewTaskRepo(s.DB())
	projRepo := NewProjectRepo(s.DB())
	ctx := context.Background()
	proj := &domain.Project{
		ID: uuid.New(), Name: "backend", DefaultWorkflow: "default",
		CreatedAt: time.Now().UTC().Truncate(time.Millisecond),
	}
	if err := projRepo.Create(ctx, proj); err != nil {
		t.Fatal(err)
	}
	t1 := newTestTask(); t1.ProjectID = &proj.ID; mustCreateTask(t, repo, t1)
	t2 := newTestTask(); mustCreateTask(t, repo, t2)
	tasks, err := repo.List(ctx, domain.TaskFilter{ProjectID: &proj.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
}

func TestTaskListByPriority(t *testing.T) {
	s := testStore(t)
	repo := NewTaskRepo(s.DB())
	ctx := context.Background()
	for _, p := range []int{1, 2, 3, 4} {
		task := newTestTask(); task.Priority = p; mustCreateTask(t, repo, task)
	}
	min, max := 2, 3
	tasks, err := repo.List(ctx, domain.TaskFilter{PriorityMin: &min, PriorityMax: &max})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(tasks))
	}
}

func TestTaskListByDueDate(t *testing.T) {
	s := testStore(t)
	repo := NewTaskRepo(s.DB())
	ctx := context.Background()
	d1 := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	d2 := time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)
	d3 := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	for _, d := range []*time.Time{&d1, &d2, &d3} {
		task := newTestTask(); task.DueAt = d; mustCreateTask(t, repo, task)
	}
	after := time.Date(2026, 5, 31, 0, 0, 0, 0, time.UTC)
	before := time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC)
	tasks, err := repo.List(ctx, domain.TaskFilter{DueAfter: &after, DueBefore: &before})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(tasks))
	}
}

func TestTaskListByParent(t *testing.T) {
	s := testStore(t)
	repo := NewTaskRepo(s.DB())
	ctx := context.Background()
	parent := newTestTask(); mustCreateTask(t, repo, parent)
	child := newTestTask(); child.ParentID = &parent.ID; mustCreateTask(t, repo, child)
	orphan := newTestTask(); mustCreateTask(t, repo, orphan)
	tasks, err := repo.List(ctx, domain.TaskFilter{ParentID: &parent.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 child, got %d", len(tasks))
	}
}

func TestTaskListWaitingOnly(t *testing.T) {
	s := testStore(t)
	repo := NewTaskRepo(s.DB())
	ctx := context.Background()
	future := time.Now().UTC().Add(24 * time.Hour)
	past := time.Now().UTC().Add(-24 * time.Hour)
	t1 := newTestTask(); t1.WaitUntil = &future; mustCreateTask(t, repo, t1)
	t2 := newTestTask(); t2.WaitUntil = &past; mustCreateTask(t, repo, t2)
	t3 := newTestTask(); mustCreateTask(t, repo, t3)
	waitingOnly := true
	tasks, err := repo.List(ctx, domain.TaskFilter{WaitingOnly: &waitingOnly})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 waiting task, got %d", len(tasks))
	}
}
```

---

## Summary of What You Did

After completing both tasks:

1. `internal/sqlite/task_test.go` has 9 new test functions covering: empty list, list all, filter by status (single and multiple), filter by project, filter by priority range, filter by due date range, filter by parent, and waiting-only filter.
2. `internal/sqlite/task.go` has a working `List` method that delegates SQL generation to `buildFilter`, and a `buildFilter` function that dynamically constructs WHERE clauses from `TaskFilter` fields.
3. All existing Phase 3 CRUD tests still pass.
4. You have two new commits: one for the tests (red) and one for the implementation (green).
