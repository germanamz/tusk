# SQLite Phase 3: TaskRepo CRUD Operations — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement the `TaskRepo` struct in `internal/sqlite/task.go` with full CRUD operations (Create, GetByID, GetByShortID, Update with optimistic locking, Delete with version check), a `scanTask` helper that parses all 15 columns (including nullable UUIDs, nullable times, and a JSON column), and stub methods for List, GetChildren, and GetDescendants (implemented in Phases 4 and 5). Also create shared test helpers (`newTestTask`, `mustCreateTask`) that all later phases (4, 5, 6, 7) depend on.

**Architecture:** `TaskRepo` lives in `internal/sqlite/` and implements the `TaskRepository` interface from `internal/repository/`. It receives a `*sql.DB` (obtained from `Store.DB()`) and uses the helper functions from `store.go` (`nullableUUID`, `nullableTime`, `nullableString`, `parseUUID`, `parseTime`, `marshalJSON`, `timeFormat`). Tests use the `testStore` helper from `store_test.go` to get an in-memory SQLite database with migrations already applied.

**Tech Stack:** Go 1.26, `github.com/google/uuid`, `database/sql`, `encoding/json`, CGo SQLite driver

---

## Context: What Has Been Built So Far

**Tusk** is a terminal-based task manager written in Go. It stores tasks, projects, tags, and relations in a local SQLite database.

**Phase 0 (Domain + Repository Interfaces):** Created all domain structs (`Task`, `Project`, `Tag`, etc.) in `internal/domain/` and all repository interfaces (`TaskRepository`, etc.) in `internal/repository/`. These are pure types with no logic.

**Phase 1 (SQLite Foundation):** Created the `Store` struct in `internal/sqlite/store.go` with:
- `New()` — opens the SQLite database and runs migrations
- `DB()` — returns the underlying `*sql.DB` so repos can use it
- `Close()` — closes the database connection
- `const timeFormat` — the standard time layout `"2006-01-02T15:04:05.000Z"` used for all time columns
- Helper functions: `nullableUUID`, `nullableTime`, `nullableString`, `parseUUID`, `parseTime`, `marshalJSON`
- Test helpers in `store_test.go`: `testStore(t)` (creates an in-memory DB with migrations) and `mustTimeNow()`

**Phase 2 (ProjectRepo + TagRepo):** Created simpler repos first to establish patterns. Phase 3 tackles the most complex repo: tasks.

**This Phase (Phase 3)** builds the `TaskRepo` — the biggest and most complex repository. Tasks have 15 columns, including nullable UUIDs (`parent_id`, `project_id`), nullable times (`due_at`, `wait_until`), a nullable string (`recurrence_rule`), and a JSON column (`uda`). The repo also introduces optimistic locking via a `version` column.

---

## Key Concepts You Need to Understand

### 1. Optimistic Locking (the `version` column)

**The problem:** Two users (or two goroutines) read the same task, both make changes, and both try to save. The second save silently overwrites the first user's changes. This is called a "lost update."

**The solution — optimistic locking:** Every task has a `version` column that starts at 1. When you update a task, the SQL says:

```sql
UPDATE tasks SET ... version = version + 1 WHERE id = ? AND version = ?
```

The `AND version = ?` clause is the key. If someone else already updated the task (bumping the version from 1 to 2), your update will match zero rows because you still think the version is 1. Zero rows affected means a conflict happened.

**How we detect it in Go:**

```go
res, err := r.db.ExecContext(ctx, `UPDATE ... WHERE id = ? AND version = ?`, ...)
n, _ := res.RowsAffected()   // How many rows did the UPDATE touch?
if n == 0 {
    return domain.ErrConflict  // Nobody was updated — stale version!
}
```

The same pattern is used for `Delete` — we only delete if the version matches, preventing you from deleting a task that someone else just modified.

**After a successful update**, we bump the in-memory version too (`task.Version++`) so the caller can make another update without re-fetching from the database.

### 2. `sql.NullString` — Handling Nullable Database Columns

SQLite columns like `parent_id`, `due_at`, and `recurrence_rule` can be `NULL`. In Go, you cannot scan a SQL `NULL` into a plain `string` — it will error. Instead, you scan into `sql.NullString`, which is a struct:

```go
type NullString struct {
    String string  // the value (only meaningful if Valid is true)
    Valid  bool    // true if the column is NOT null
}
```

After scanning, you check `.Valid` to decide whether to set the Go field to `nil` or to a parsed value. The helpers `parseUUID(sql.NullString)` and `parseTime(sql.NullString)` from `store.go` do this for you.

### 3. JSON Columns (the `uda` field)

The `uda` column stores **User-Defined Attributes** as a JSON string in SQLite (e.g., `'{"custom":"value"}'`). In Go, the domain type is `map[string]any`.

- **Writing:** Before inserting, we call `marshalJSON(task.UDA)` (from `store.go`) to convert the map to a JSON string.
- **Reading:** After scanning the raw string, we call `json.Unmarshal([]byte(udaJSON), &t.UDA)` to parse it back into a map.

The database default is `'{}'` (empty JSON object), so even tasks with no custom attributes have valid JSON.

### 4. The `taskColumns` Constant

We define all 15 column names once:

```go
const taskColumns = `id, short_id, parent_id, project_id, title, description,
    status, priority, version, due_at, wait_until, recurrence_rule, uda,
    created_at, modified_at`
```

This prevents bugs where the `SELECT` lists columns in a different order than `Scan` reads them. Every query that returns a full task row uses this constant, and `scanTask` always scans in this exact order.

### 5. The `taskScanner` Interface

Both `*sql.Row` (from `QueryRowContext`, returns one row) and `*sql.Rows` (from `QueryContext`, returns many rows) have a `Scan(dest ...any) error` method. We define a tiny interface:

```go
type taskScanner interface {
    Scan(dest ...any) error
}
```

This lets us write `scanTask` once and use it for both single-row and multi-row queries. The `scanOne` method wraps `scanTask` for single-row queries (converting `sql.ErrNoRows` to `domain.ErrNotFound`). The `scanRows` method wraps `scanTask` for multi-row queries (looping over `rows.Next()`).

### 6. Stub Methods

`List`, `GetChildren`, and `GetDescendants` are part of the `TaskRepository` interface, so `TaskRepo` must have them to compile. But they involve complex SQL (dynamic filters, recursive CTEs) that belongs in later phases. We return a temporary error:

```go
func (r *TaskRepo) List(ctx context.Context, filter domain.TaskFilter) ([]*domain.Task, error) {
    return nil, fmt.Errorf("not implemented: see Phase 4")
}
```

This lets us satisfy the interface now and fill in the real logic later.

---

## How the Pieces Wire Together

```
store.go          task.go              task_test.go
────────          ───────              ────────────
Store.DB()  ───>  NewTaskRepo(db)      testStore(t) from store_test.go
                     │                      │
nullableUUID()       │ uses helpers         │ creates in-memory DB
nullableTime()       │                      │
nullableString()     │                      v
parseUUID()          │               NewTaskRepo(s.DB())
parseTime()          │                      │
marshalJSON()        │                      v
timeFormat           │               newTestTask()      ──> used by Phases 4-7
                     │               mustCreateTask()   ──> used by Phases 4-7
                     v
              scanTask()             ──> used by Phases 4 and 5
              scanRows()             ──> used by Phases 4 and 5
```

- **`NewTaskRepo(db *sql.DB)`** takes the raw `*sql.DB` from `Store.DB()`.
- **`scanTask`** and **`scanRows`** are reused by the List, GetChildren, and GetDescendants implementations in Phases 4 and 5.
- **`newTestTask`** and **`mustCreateTask`** are test helpers in `task_test.go` that every later phase's tests will call to create test data.

---

## File Structure

| Action | File | Responsibility |
|--------|------|----------------|
| Create | `internal/sqlite/task_test.go` | Test helpers + CRUD tests |
| Modify | `internal/sqlite/task.go` | `TaskRepo` struct with CRUD + stubs |

---

### Task 1: Write Failing Tests and Shared Test Helpers

**Files:**
- Create: `internal/sqlite/task_test.go`

This task creates the test file first. The tests will not compile yet because `TaskRepo`, `NewTaskRepo`, and its methods do not exist. This is intentional — we write tests first so we know exactly what we need to build, and we can verify that all tests pass after Task 2.

The test file includes two shared helpers that later phases depend on:

- **`newTestTask()`** — creates a minimal valid `domain.Task` with unique IDs, sensible defaults, and timestamps truncated to millisecond precision (because SQLite only stores 3 decimal places).
- **`mustCreateTask(t, repo, task)`** — inserts a task and fails the test immediately if it errors. The `t.Helper()` call makes error messages point to the caller's line, not this helper's line.

The file also includes a **compile-time interface check**:

```go
var _ repository.TaskRepository = (*TaskRepo)(nil)
```

This line does not run any code. It tells the Go compiler: "verify that `*TaskRepo` implements `repository.TaskRepository`." If any method is missing or has the wrong signature, the build fails with a clear error. This catches interface drift early.

- [ ] **Step 1: Write `task_test.go`**

Create the file `internal/sqlite/task_test.go` with the following exact contents:

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

var _ repository.TaskRepository = (*TaskRepo)(nil)

func newTestTask() *domain.Task {
	now := time.Now().UTC().Truncate(time.Millisecond)
	return &domain.Task{
		ID:          uuid.New(),
		ShortID:     uuid.New().String()[:8],
		Title:       "Test task",
		Description: "A test task",
		Status:      "pending",
		Priority:    2,
		Version:     1,
		UDA:         map[string]any{},
		CreatedAt:   now,
		ModifiedAt:  now,
	}
}

func mustCreateTask(t *testing.T, repo *TaskRepo, task *domain.Task) {
	t.Helper()
	if err := repo.Create(context.Background(), task); err != nil {
		t.Fatalf("mustCreateTask: %v", err)
	}
}

func TestTaskCreate(t *testing.T) {
	s := testStore(t)
	repo := NewTaskRepo(s.DB())
	ctx := context.Background()
	task := newTestTask()
	if err := repo.Create(ctx, task); err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := repo.GetByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Title != "Test task" {
		t.Fatalf("expected title 'Test task', got %q", got.Title)
	}
	if got.Version != 1 {
		t.Fatalf("expected version 1, got %d", got.Version)
	}
}

func TestTaskCreateWithNullables(t *testing.T) {
	s := testStore(t)
	repo := NewTaskRepo(s.DB())
	ctx := context.Background()
	defaultProjectID := uuid.MustParse("00000000-0000-0000-0000-000000000000")
	due := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	wait := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	rrule := "FREQ=WEEKLY;BYDAY=MO"
	task := newTestTask()
	task.ProjectID = &defaultProjectID
	task.DueAt = &due
	task.WaitUntil = &wait
	task.RecurrenceRule = &rrule
	task.UDA = map[string]any{"custom": "value"}
	if err := repo.Create(ctx, task); err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := repo.GetByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.ProjectID == nil || *got.ProjectID != defaultProjectID {
		t.Fatalf("expected project ID %s, got %v", defaultProjectID, got.ProjectID)
	}
	if got.DueAt == nil || !got.DueAt.Equal(due) {
		t.Fatalf("expected due %v, got %v", due, got.DueAt)
	}
	if got.WaitUntil == nil || !got.WaitUntil.Equal(wait) {
		t.Fatalf("expected wait %v, got %v", wait, got.WaitUntil)
	}
	if got.RecurrenceRule == nil || *got.RecurrenceRule != rrule {
		t.Fatalf("expected rrule %s, got %v", rrule, got.RecurrenceRule)
	}
	if got.UDA["custom"] != "value" {
		t.Fatalf("expected UDA custom=value, got %v", got.UDA)
	}
}

func TestTaskGetByShortID(t *testing.T) {
	s := testStore(t)
	repo := NewTaskRepo(s.DB())
	ctx := context.Background()
	task := newTestTask()
	mustCreateTask(t, repo, task)
	got, err := repo.GetByShortID(ctx, task.ShortID)
	if err != nil {
		t.Fatalf("GetByShortID: %v", err)
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

func TestTaskGetByShortIDNotFound(t *testing.T) {
	s := testStore(t)
	repo := NewTaskRepo(s.DB())
	_, err := repo.GetByShortID(context.Background(), "nonexist")
	if err != domain.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestTaskUpdate(t *testing.T) {
	s := testStore(t)
	repo := NewTaskRepo(s.DB())
	ctx := context.Background()
	task := newTestTask()
	mustCreateTask(t, repo, task)
	task.Title = "Updated title"
	task.Priority = 4
	if err := repo.Update(ctx, task); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if task.Version != 2 {
		t.Fatalf("expected version bumped to 2, got %d", task.Version)
	}
	got, err := repo.GetByID(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "Updated title" {
		t.Fatalf("expected updated title, got %q", got.Title)
	}
	if got.Version != 2 {
		t.Fatalf("expected version 2, got %d", got.Version)
	}
}

func TestTaskUpdateConflict(t *testing.T) {
	s := testStore(t)
	repo := NewTaskRepo(s.DB())
	ctx := context.Background()
	task := newTestTask()
	mustCreateTask(t, repo, task)
	task.Version = 99
	task.Title = "Stale update"
	err := repo.Update(ctx, task)
	if err != domain.ErrConflict {
		t.Fatalf("expected ErrConflict, got %v", err)
	}
}

func TestTaskDelete(t *testing.T) {
	s := testStore(t)
	repo := NewTaskRepo(s.DB())
	ctx := context.Background()
	task := newTestTask()
	mustCreateTask(t, repo, task)
	if err := repo.Delete(ctx, task.ID, task.Version); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	_, err := repo.GetByID(ctx, task.ID)
	if err != domain.ErrNotFound {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestTaskDeleteConflict(t *testing.T) {
	s := testStore(t)
	repo := NewTaskRepo(s.DB())
	ctx := context.Background()
	task := newTestTask()
	mustCreateTask(t, repo, task)
	err := repo.Delete(ctx, task.ID, 99)
	if err != domain.ErrConflict {
		t.Fatalf("expected ErrConflict, got %v", err)
	}
}
```

**What each test verifies:**

| Test | What it proves |
|------|----------------|
| `TestTaskCreate` | Insert a task and read it back; title and version survive the round-trip |
| `TestTaskCreateWithNullables` | All nullable fields (ProjectID, DueAt, WaitUntil, RecurrenceRule) and the JSON UDA column round-trip correctly |
| `TestTaskGetByShortID` | Lookup by the human-friendly short ID works |
| `TestTaskGetByIDNotFound` | Querying a nonexistent UUID returns `domain.ErrNotFound` |
| `TestTaskGetByShortIDNotFound` | Querying a nonexistent short ID returns `domain.ErrNotFound` |
| `TestTaskUpdate` | Changing fields and calling Update persists the changes and bumps the version |
| `TestTaskUpdateConflict` | Updating with a stale version returns `domain.ErrConflict` |
| `TestTaskDelete` | Deleting with the correct version removes the task |
| `TestTaskDeleteConflict` | Deleting with a wrong version returns `domain.ErrConflict` |

- [ ] **Step 2: Verify the tests do NOT compile yet**

Run: `cd /Users/germanamz/projects/tusk && go build ./internal/sqlite/`
Expected: Compilation errors like `undefined: NewTaskRepo`. This is correct — we have not written the implementation yet.

- [ ] **Step 3: Commit the test file**

```bash
git add internal/sqlite/task_test.go
git commit -m "test(sqlite): add TaskRepo CRUD tests and shared test helpers"
```

---

### Task 2: Implement TaskRepo

**Files:**
- Modify: `internal/sqlite/task.go` (currently contains only `package sqlite`)

Now we write the implementation that makes all the tests from Task 1 pass.

- [ ] **Step 1: Write `task.go`**

Replace the contents of `internal/sqlite/task.go` with the following exact code:

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

const taskColumns = `id, short_id, parent_id, project_id, title, description,
	status, priority, version, due_at, wait_until, recurrence_rule, uda,
	created_at, modified_at`

type TaskRepo struct {
	db *sql.DB
}

func NewTaskRepo(db *sql.DB) *TaskRepo {
	return &TaskRepo{db: db}
}

func (r *TaskRepo) Create(ctx context.Context, task *domain.Task) error {
	_, err := r.db.ExecContext(ctx, fmt.Sprintf(
		`INSERT INTO tasks (%s) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		taskColumns),
		task.ID.String(), task.ShortID,
		nullableUUID(task.ParentID), nullableUUID(task.ProjectID),
		task.Title, task.Description, task.Status, task.Priority, task.Version,
		nullableTime(task.DueAt), nullableTime(task.WaitUntil),
		nullableString(task.RecurrenceRule), marshalJSON(task.UDA),
		task.CreatedAt.UTC().Format(timeFormat),
		task.ModifiedAt.UTC().Format(timeFormat),
	)
	return err
}

func (r *TaskRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.Task, error) {
	row := r.db.QueryRowContext(ctx,
		fmt.Sprintf(`SELECT %s FROM tasks WHERE id = ?`, taskColumns), id.String())
	return r.scanOne(row)
}

func (r *TaskRepo) GetByShortID(ctx context.Context, shortID string) (*domain.Task, error) {
	row := r.db.QueryRowContext(ctx,
		fmt.Sprintf(`SELECT %s FROM tasks WHERE short_id = ?`, taskColumns), shortID)
	return r.scanOne(row)
}

func (r *TaskRepo) Update(ctx context.Context, task *domain.Task) error {
	now := time.Now().UTC().Format(timeFormat)
	res, err := r.db.ExecContext(ctx, `
		UPDATE tasks SET
			parent_id = ?, project_id = ?, title = ?, description = ?,
			status = ?, priority = ?, due_at = ?, wait_until = ?,
			recurrence_rule = ?, uda = ?, version = version + 1, modified_at = ?
		WHERE id = ? AND version = ?`,
		nullableUUID(task.ParentID), nullableUUID(task.ProjectID),
		task.Title, task.Description, task.Status, task.Priority,
		nullableTime(task.DueAt), nullableTime(task.WaitUntil),
		nullableString(task.RecurrenceRule), marshalJSON(task.UDA),
		now, task.ID.String(), task.Version,
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
	return nil
}

func (r *TaskRepo) Delete(ctx context.Context, id uuid.UUID, version int) error {
	res, err := r.db.ExecContext(ctx,
		`DELETE FROM tasks WHERE id = ? AND version = ?`, id.String(), version)
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
	return nil
}

// Stubs — implemented in Phase 4 and Phase 5
func (r *TaskRepo) List(ctx context.Context, filter domain.TaskFilter) ([]*domain.Task, error) {
	return nil, fmt.Errorf("not implemented: see Phase 4")
}
func (r *TaskRepo) GetChildren(ctx context.Context, parentID uuid.UUID) ([]*domain.Task, error) {
	return nil, fmt.Errorf("not implemented: see Phase 5")
}
func (r *TaskRepo) GetDescendants(ctx context.Context, rootID uuid.UUID) ([]*domain.Task, error) {
	return nil, fmt.Errorf("not implemented: see Phase 5")
}

func (r *TaskRepo) scanOne(row *sql.Row) (*domain.Task, error) {
	t, err := scanTask(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	return t, err
}

func (r *TaskRepo) scanRows(rows *sql.Rows) ([]*domain.Task, error) {
	var result []*domain.Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, t)
	}
	return result, rows.Err()
}

type taskScanner interface {
	Scan(dest ...any) error
}

func scanTask(s taskScanner) (*domain.Task, error) {
	var (
		t          domain.Task
		id         string
		parentID   sql.NullString
		projectID  sql.NullString
		dueAt      sql.NullString
		waitUntil  sql.NullString
		recurrence sql.NullString
		udaJSON    string
		createdAt  string
		modifiedAt string
	)
	err := s.Scan(
		&id, &t.ShortID, &parentID, &projectID,
		&t.Title, &t.Description, &t.Status, &t.Priority, &t.Version,
		&dueAt, &waitUntil, &recurrence, &udaJSON,
		&createdAt, &modifiedAt,
	)
	if err != nil {
		return nil, err
	}
	t.ID, err = uuid.Parse(id)
	if err != nil {
		return nil, fmt.Errorf("parsing task ID: %w", err)
	}
	t.ParentID, err = parseUUID(parentID)
	if err != nil {
		return nil, fmt.Errorf("parsing parent_id: %w", err)
	}
	t.ProjectID, err = parseUUID(projectID)
	if err != nil {
		return nil, fmt.Errorf("parsing project_id: %w", err)
	}
	t.DueAt, err = parseTime(dueAt)
	if err != nil {
		return nil, fmt.Errorf("parsing due_at: %w", err)
	}
	t.WaitUntil, err = parseTime(waitUntil)
	if err != nil {
		return nil, fmt.Errorf("parsing wait_until: %w", err)
	}
	if recurrence.Valid {
		t.RecurrenceRule = &recurrence.String
	}
	if err := json.Unmarshal([]byte(udaJSON), &t.UDA); err != nil {
		return nil, fmt.Errorf("parsing uda: %w", err)
	}
	t.CreatedAt, err = time.Parse(timeFormat, createdAt)
	if err != nil {
		return nil, fmt.Errorf("parsing created_at: %w", err)
	}
	t.ModifiedAt, err = time.Parse(timeFormat, modifiedAt)
	if err != nil {
		return nil, fmt.Errorf("parsing modified_at: %w", err)
	}
	return &t, nil
}
```

**Line-by-line walkthrough of the important parts:**

**`taskColumns` constant (line 14-16):**
Lists all 15 columns in the exact order that `scanTask` expects them. Used by every SELECT and the INSERT. If you ever add a column to the tasks table, you update this constant and `scanTask` together.

**`TaskRepo` struct (line 18-20):**
Holds a single field: `db *sql.DB`. This is the standard Go database handle. It manages a connection pool internally — you do not open/close connections yourself.

**`NewTaskRepo(db *sql.DB)` constructor (line 22-24):**
Takes the database handle and returns a ready-to-use repo. In production code, you call `NewTaskRepo(store.DB())`.

**`Create` method (line 26-39):**
Builds an INSERT with 15 `?` placeholders. Each nullable Go field is passed through the appropriate `nullable*` helper from `store.go`:
- `nullableUUID(task.ParentID)` — returns `nil` (SQL NULL) if the pointer is nil, or the UUID string if set
- `nullableTime(task.DueAt)` — returns `nil` or the formatted time string
- `nullableString(task.RecurrenceRule)` — returns `nil` or the string value
- `marshalJSON(task.UDA)` — converts the map to a JSON string (e.g., `"{}"`)
- Time values are formatted with `timeFormat` to match SQLite's text format

**`GetByID` and `GetByShortID` methods (line 41-50):**
Both are simple SELECT queries that delegate to `scanOne`. The only difference is the WHERE clause (`id = ?` vs `short_id = ?`).

**`Update` method (line 52-76):**
This is where optimistic locking happens:
1. The SQL sets `version = version + 1` (the DB increments it)
2. The WHERE clause includes `AND version = ?` (only matches if version has not changed)
3. `res.RowsAffected()` tells us if the UPDATE matched any row
4. If 0 rows affected, we return `domain.ErrConflict`
5. On success, we bump `task.Version++` in memory so the caller's struct stays in sync

Note: `modified_at` is set to `time.Now().UTC()` by the Go code, not by a SQLite trigger. This gives us deterministic timestamps in tests.

**`Delete` method (line 78-92):**
Same optimistic locking pattern as Update. The WHERE clause includes `AND version = ?`. If the version does not match (0 rows deleted), we return `domain.ErrConflict`.

**Stub methods (line 94-102):**
Three methods that return "not implemented" errors. They exist solely so `TaskRepo` satisfies the `TaskRepository` interface. Phases 4 and 5 will replace these with real implementations.

**`scanOne` method (line 104-110):**
Wraps `scanTask` for single-row queries. The key detail: when `QueryRowContext` finds no matching row, `Scan` returns `sql.ErrNoRows`. We translate that to `domain.ErrNotFound` so callers do not need to know about `database/sql` internals.

**`scanRows` method (line 112-123):**
Wraps `scanTask` for multi-row queries. Loops over `rows.Next()`, calling `scanTask` on each row. After the loop, `rows.Err()` catches any error that happened during iteration (like a network timeout). This method is not used yet but will be used by List, GetChildren, and GetDescendants in Phases 4-5.

**`taskScanner` interface (line 125-127):**
A one-method interface that both `*sql.Row` and `*sql.Rows` satisfy. This is a common Go pattern — define the smallest interface you need, and the standard library types automatically implement it.

**`scanTask` function (line 129-184):**
The workhorse that parses all 15 columns:
1. Declares local variables for nullable columns (`sql.NullString`) and string columns that need parsing
2. Calls `s.Scan(...)` with pointers to all 15 variables, in the same order as `taskColumns`
3. Parses the `id` string into `uuid.UUID`
4. Parses nullable UUIDs (`parentID`, `projectID`) via `parseUUID` — returns `*uuid.UUID` or nil
5. Parses nullable times (`dueAt`, `waitUntil`) via `parseTime` — returns `*time.Time` or nil
6. Handles `recurrence` manually (just checks `.Valid` and sets the pointer)
7. Unmarshals the `udaJSON` string into `map[string]any`
8. Parses `createdAt` and `modifiedAt` strings into `time.Time`

Every parse step wraps errors with context (e.g., `fmt.Errorf("parsing parent_id: %w", err)`) so you can tell which column failed.

**Note about unused imports:** The `strings` import is included in the implementation because it will be needed by the `List` method in Phase 4 (for building dynamic WHERE clauses). If your Go version or linter complains about unused imports, you may remove `strings` for now and add it back in Phase 4.

- [ ] **Step 2: Verify it compiles**

Run: `cd /Users/germanamz/projects/tusk && go build ./internal/sqlite/`
Expected: No errors. If you see an "imported and not used: strings" error, remove the `"strings"` import line and try again. It will be added back in Phase 4.

- [ ] **Step 3: Run the tests**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/sqlite/ -v -run TestTask`

Expected output (all 9 tests pass):

```
=== RUN   TestTaskCreate
--- PASS: TestTaskCreate
=== RUN   TestTaskCreateWithNullables
--- PASS: TestTaskCreateWithNullables
=== RUN   TestTaskGetByShortID
--- PASS: TestTaskGetByShortID
=== RUN   TestTaskGetByIDNotFound
--- PASS: TestTaskGetByIDNotFound
=== RUN   TestTaskGetByShortIDNotFound
--- PASS: TestTaskGetByShortIDNotFound
=== RUN   TestTaskUpdate
--- PASS: TestTaskUpdate
=== RUN   TestTaskUpdateConflict
--- PASS: TestTaskUpdateConflict
=== RUN   TestTaskDelete
--- PASS: TestTaskDelete
=== RUN   TestTaskDeleteConflict
--- PASS: TestTaskDeleteConflict
PASS
```

If any test fails, read the error message carefully. Common issues:
- **"no such table: tasks"** — the migrations in `testStore` did not create the tasks table. Check that your migration files are correct.
- **"parsing created_at: ..."** — the time format in the INSERT does not match `timeFormat`. Make sure you are using `task.CreatedAt.UTC().Format(timeFormat)`.
- **"expected ErrNotFound, got <nil>"** — `scanOne` is not converting `sql.ErrNoRows` to `domain.ErrNotFound`.

- [ ] **Step 4: Run the full test suite**

Run: `cd /Users/germanamz/projects/tusk && go test ./... -v`
Expected: All tests across all packages pass. No regressions.

- [ ] **Step 5: Run vet**

Run: `cd /Users/germanamz/projects/tusk && go vet ./internal/sqlite/`
Expected: No output (clean).

- [ ] **Step 6: Commit**

```bash
git add internal/sqlite/task.go internal/sqlite/task_test.go
git commit -m "feat(sqlite): implement TaskRepo CRUD with optimistic locking"
```

---

### Final Verification

- [ ] **Step 1: Confirm all tests pass**

Run: `cd /Users/germanamz/projects/tusk && go test ./... -count=1`

The `-count=1` flag disables test caching, forcing every test to run fresh. All tests should pass.

- [ ] **Step 2: Confirm the interface is satisfied**

The compile-time check `var _ repository.TaskRepository = (*TaskRepo)(nil)` in the test file guarantees that `TaskRepo` implements every method in the `TaskRepository` interface. If this line compiles, the interface is satisfied.

- [ ] **Step 3: Review what was built**

After this phase, the following exists in `internal/sqlite/`:

| File | What it contains |
|------|-----------------|
| `store.go` | `Store` struct, `New()`, `DB()`, `Close()`, helper functions (from Phase 1) |
| `store_test.go` | `testStore(t)`, `mustTimeNow()` (from Phase 1) |
| `task.go` | `TaskRepo` struct, `NewTaskRepo()`, Create/GetByID/GetByShortID/Update/Delete, stubs for List/GetChildren/GetDescendants, `scanTask`, `scanRows`, `taskColumns` |
| `task_test.go` | `newTestTask()`, `mustCreateTask()`, 9 CRUD tests |

**What later phases will use from this phase:**
- **Phase 4 (List with filters):** Replaces the `List` stub. Uses `taskColumns`, `scanRows`, and `scanTask`.
- **Phase 5 (Tree queries):** Replaces `GetChildren` and `GetDescendants` stubs. Uses `taskColumns`, `scanRows`, and `scanTask`.
- **Phases 4-7 (all tests):** Use `newTestTask()` and `mustCreateTask()` to create test data.
