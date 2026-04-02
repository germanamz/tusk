# Phase 2: ProjectRepo — SQLite Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement `ProjectRepo`, the first concrete SQLite repository. It satisfies the `ProjectRepository` interface with 6 methods: Create, GetByID, GetByName, List, Update, Delete. This establishes the patterns every later repository will follow.

**Prerequisites:** Phase 1 (Store & Migration Infrastructure) must be complete. You should already have `internal/sqlite/store.go` (with `Store`, `timeFormat`, and helper functions) and `internal/sqlite/store_test.go` (with `testStore` and `mustTimeNow`).

**What you'll learn:** scanner interface pattern, `sql.ErrNoRows` to `domain.ErrNotFound` mapping, `RowsAffected` for delete idempotency, `context.Context` in database calls, compile-time interface checks with `var _`

**Estimated effort:** 45 minutes - 1 hour

---

## Context

### What is Tusk?

Tusk is a concurrent-safe task management tool written in Go. It stores data in SQLite. The architecture has four layers:

1. **Domain** (`internal/domain/`) — pure data types and sentinel errors. No dependencies beyond stdlib and `uuid`.
2. **Repository** (`internal/repository/`) — Go interfaces that define what storage operations exist. No implementation details.
3. **SQLite** (`internal/sqlite/`) — concrete implementations of the repository interfaces using SQLite. This is the layer we are building.
4. **Service** and **CLI/MCP** — business logic and user-facing layers (built in later phases).

### What Phase 1 Produced

Phase 1 created the foundation that all repository implementations depend on:

- **`internal/sqlite/store.go`** — The `Store` struct that opens a SQLite database, runs migrations, and exposes `DB() *sql.DB`. It also defines `const timeFormat` and helper functions (`nullableUUID`, `nullableTime`, `nullableString`, `parseUUID`, `parseTime`, `marshalJSON`) used by all repos to convert between Go types and SQLite text columns.
- **`internal/sqlite/store_test.go`** — Test helpers available to all `_test.go` files in the `sqlite` package:
  - `testStore(t *testing.T) *Store` — creates an in-memory SQLite database with all migrations applied. It calls `t.Cleanup()` to close the database automatically when the test finishes.
  - `mustTimeNow() time.Time` — returns `time.Now().UTC().Truncate(time.Millisecond)` so test data has consistent precision matching SQLite's millisecond storage.
- **`migrations/`** — SQL migration files embedded via `migrations/migrations.go`. The migration for the `projects` table creates the table and seeds a default project with `id='00000000-0000-0000-0000-000000000000'` and `name='_default'`.

### What This Phase Builds

This phase creates two files:

1. `internal/sqlite/project.go` — The `ProjectRepo` struct that implements `repository.ProjectRepository`.
2. `internal/sqlite/project_test.go` — Tests that verify every method works correctly against a real (in-memory) SQLite database.

`ProjectRepo` is the simplest repository. It has no foreign keys to manage, no JSON columns, no versioning. That makes it the ideal first implementation. Every pattern you learn here — the scanner interface, the `scanOne` helper, `ErrNotFound` mapping, passing `context.Context` — will appear again in the more complex repos (Task, Relation, Tag, etc.).

### How ProjectRepo Connects to the Rest of the System

Here is how the pieces wire together:

```
Store (store.go)                 ProjectRepo (project.go)
  |                                   |
  | .DB() returns *sql.DB             | takes *sql.DB in constructor
  |-----------------------------------+
  |
  | timeFormat constant               | used in Create (formatting) and
  |-----------------------------------| scanProject (parsing)
  |
  | testStore (store_test.go)         | used in project_test.go to get
  |-----------------------------------| a ready-to-use database
```

In later phases, other repos will also need projects to exist first. For example, Phase 4 (TaskRepo) tests will create projects using `ProjectRepo` before creating tasks, because tasks have a `project_id` foreign key. So the patterns you establish here matter.

---

## Key Concepts

Before you start coding, read through these concepts. They explain *why* the code is written the way it is.

### 1. The Scanner Interface Pattern

In Go's `database/sql` package, there are two types that can scan a row of results:

- `*sql.Row` — returned by `QueryRowContext()`, represents exactly one row (or no rows).
- `*sql.Rows` — returned by `QueryContext()`, represents zero or more rows that you iterate with `.Next()`.

Both have a `.Scan(dest ...any) error` method with the same signature. But they are different types — `*sql.Row` and `*sql.Rows` are not interchangeable in Go's type system.

The problem: We want ONE function that can scan a project from either type. Without an interface, we would need to duplicate the scanning logic.

The solution: We define a tiny interface:

```go
type projectScanner interface {
    Scan(dest ...any) error
}
```

Both `*sql.Row` and `*sql.Rows` satisfy this interface. Now our `scanProject` function takes a `projectScanner` and works with both. This is a standard Go pattern — define a small interface where you need it, right next to where it is used.

### 2. `sql.ErrNoRows` to `domain.ErrNotFound` Mapping

When you call `QueryRowContext()` followed by `.Scan()`, and the query matched zero rows, Go returns `sql.ErrNoRows`. This is a database-specific error from the `database/sql` package.

Our domain layer does not know about `database/sql`. It defines its own `domain.ErrNotFound` error. The repository layer is responsible for translating between these two worlds.

The `scanOne` method does this translation:

```go
func (r *ProjectRepo) scanOne(row *sql.Row) (*domain.Project, error) {
    p, err := scanProject(row)
    if errors.Is(err, sql.ErrNoRows) {
        return nil, domain.ErrNotFound
    }
    return p, err
}
```

`errors.Is()` checks if the error matches `sql.ErrNoRows` (even if it is wrapped). If it does, we return `domain.ErrNotFound` instead. This means callers of `GetByID` and `GetByName` never see SQL-specific errors — they only see domain errors.

### 3. `RowsAffected` for Delete

When you run a `DELETE` SQL statement, the database tells you how many rows were actually deleted. In Go, `ExecContext()` returns a `sql.Result` which has a `RowsAffected()` method.

If we try to delete a project that does not exist, the SQL executes successfully (no error), but `RowsAffected()` returns 0. We check for this and return `domain.ErrNotFound`:

```go
res, err := r.db.ExecContext(ctx, `DELETE FROM projects WHERE id = ?`, id.String())
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
```

This pattern ensures that deleting a non-existent project is reported as an error rather than silently succeeding.

### 4. `context.Context` in Database Calls

Every repository method takes `ctx context.Context` as its first parameter. This is a Go convention for functions that do I/O (network calls, database queries, file operations).

Why? Context lets the caller:
- **Cancel operations** — if a user presses Ctrl+C, the context gets cancelled and the database query stops.
- **Set timeouts** — `context.WithTimeout(ctx, 5*time.Second)` ensures a query does not hang forever.
- **Pass request-scoped values** — like trace IDs for debugging.

We pass `ctx` to `ExecContext()` and `QueryContext()` (instead of `Exec()` and `Query()`) so that SQLite respects cancellation. Always use the `*Context` variants.

### 5. Compile-Time Interface Checks with `var _`

Go interfaces are satisfied implicitly — you do not write `implements`. This is powerful but has a downside: if you forget to implement a method, you only find out when something tries to use the type as that interface (which might be in a completely different package, much later).

We add this line at the top of the test file:

```go
var _ repository.ProjectRepository = (*ProjectRepo)(nil)
```

This line does the following:
- `(*ProjectRepo)(nil)` creates a nil pointer of type `*ProjectRepo`. No memory is allocated.
- `var _ repository.ProjectRepository = ...` assigns it to a variable of the interface type.
- `_` means we discard the variable — we do not need it at runtime.

If `*ProjectRepo` is missing any method from `ProjectRepository`, this line causes a **compile error**. You see the error immediately when you run `go build` or `go test`, not later when integration tests run. This is a safety net.

---

## File Structure

| Action | File | Responsibility |
|--------|------|----------------|
| Create | `internal/sqlite/project_test.go` | Tests for all 6 ProjectRepo methods + compile-time interface check |
| Modify | `internal/sqlite/project.go` | `ProjectRepo` struct implementing `repository.ProjectRepository` |

**Files you will read but NOT modify:**
- `internal/sqlite/store.go` — provides `timeFormat` constant and `Store` struct
- `internal/sqlite/store_test.go` — provides `testStore()` and `mustTimeNow()` helpers
- `internal/domain/project.go` — the `Project` struct
- `internal/domain/errors.go` — `ErrNotFound` sentinel error
- `internal/repository/project.go` — the `ProjectRepository` interface

---

## Tasks

### Task 1: Write the Tests (`project_test.go`)

We write tests first (TDD). The tests will not compile yet because `ProjectRepo` does not exist. That is expected — seeing the compile failure confirms that our tests are actually testing the right thing.

**Files:**
- Create: `internal/sqlite/project_test.go`

- [ ] **Step 1: Write `project_test.go`**

Create the file `internal/sqlite/project_test.go` with the following COMPLETE contents:

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

// Compile-time check: *ProjectRepo must implement repository.ProjectRepository.
// If ProjectRepo is missing any method, this line produces a compile error.
// The nil pointer is never dereferenced — it costs nothing at runtime.
var _ repository.ProjectRepository = (*ProjectRepo)(nil)

// TestProjectCreate verifies that we can insert a new project and read it back.
// It exercises Create and GetByID together because you need GetByID to verify
// that Create actually persisted the data.
func TestProjectCreate(t *testing.T) {
	// testStore creates an in-memory SQLite database with all migrations applied.
	// It registers t.Cleanup to close the DB when this test finishes.
	s := testStore(t)

	// NewProjectRepo takes a *sql.DB (not a *Store). We get the *sql.DB via s.DB().
	// This keeps ProjectRepo decoupled from the Store type — it only needs
	// the standard library's database interface.
	repo := NewProjectRepo(s.DB())

	// context.Background() returns a non-nil, empty context. It is never cancelled.
	// In tests, this is fine. In production, you would use a context with a timeout.
	ctx := context.Background()

	// Build a Project value. We set all fields explicitly so there are no surprises.
	// time.Now().UTC().Truncate(time.Millisecond) matches SQLite's millisecond
	// precision — without Truncate, the round-trip would lose sub-millisecond data
	// and the comparison would fail.
	p := &domain.Project{
		ID:              uuid.New(),
		Name:            "backend",
		Description:     "Backend services",
		DefaultWorkflow: "default",
		CreatedAt:       time.Now().UTC().Truncate(time.Millisecond),
	}

	// Create should succeed with no error.
	if err := repo.Create(ctx, p); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Read it back by ID and verify the name survived the round-trip.
	got, err := repo.GetByID(ctx, p.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Name != "backend" {
		t.Fatalf("expected name backend, got %s", got.Name)
	}
}

// TestProjectGetByName verifies lookup by name works.
// The migration seeds a "_default" project, so we do not need to create one.
func TestProjectGetByName(t *testing.T) {
	s := testStore(t)
	repo := NewProjectRepo(s.DB())
	ctx := context.Background()

	// "_default" was inserted by the migration SQL. It should always exist.
	got, err := repo.GetByName(ctx, "_default")
	if err != nil {
		t.Fatalf("GetByName: %v", err)
	}
	if got.Name != "_default" {
		t.Fatalf("expected _default, got %s", got.Name)
	}
}

// TestProjectGetByIDNotFound verifies that looking up a non-existent ID
// returns domain.ErrNotFound (not sql.ErrNoRows or nil).
func TestProjectGetByIDNotFound(t *testing.T) {
	s := testStore(t)
	repo := NewProjectRepo(s.DB())
	ctx := context.Background()

	// uuid.New() generates a random UUID that definitely does not exist in the DB.
	_, err := repo.GetByID(ctx, uuid.New())
	if err != domain.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// TestProjectGetByNameNotFound verifies the same ErrNotFound behavior for GetByName.
func TestProjectGetByNameNotFound(t *testing.T) {
	s := testStore(t)
	repo := NewProjectRepo(s.DB())
	ctx := context.Background()

	_, err := repo.GetByName(ctx, "nonexistent")
	if err != domain.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// TestProjectList verifies that List returns all projects.
// The migration seeds 1 project ("_default"). We add 1 more, so we expect 2.
func TestProjectList(t *testing.T) {
	s := testStore(t)
	repo := NewProjectRepo(s.DB())
	ctx := context.Background()

	p := &domain.Project{
		ID: uuid.New(), Name: "frontend", Description: "Frontend app",
		DefaultWorkflow: "default",
		CreatedAt:       time.Now().UTC().Truncate(time.Millisecond),
	}
	if err := repo.Create(ctx, p); err != nil {
		t.Fatal(err)
	}

	projects, err := repo.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// 1 seeded ("_default") + 1 we just created = 2
	if len(projects) != 2 {
		t.Fatalf("expected 2 projects, got %d", len(projects))
	}
}

// TestProjectUpdate verifies that Update changes a field and GetByID sees the change.
func TestProjectUpdate(t *testing.T) {
	s := testStore(t)
	repo := NewProjectRepo(s.DB())
	ctx := context.Background()

	p := &domain.Project{
		ID: uuid.New(), Name: "mobile", Description: "Mobile app",
		DefaultWorkflow: "default",
		CreatedAt:       time.Now().UTC().Truncate(time.Millisecond),
	}
	if err := repo.Create(ctx, p); err != nil {
		t.Fatal(err)
	}

	// Mutate the in-memory struct and call Update.
	p.Description = "Mobile applications"
	if err := repo.Update(ctx, p); err != nil {
		t.Fatalf("Update: %v", err)
	}

	// Read it back and verify the change persisted.
	got, err := repo.GetByID(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Description != "Mobile applications" {
		t.Fatalf("expected updated description, got %s", got.Description)
	}
}

// TestProjectDelete verifies that Delete removes a project, and that
// GetByID returns ErrNotFound afterward.
func TestProjectDelete(t *testing.T) {
	s := testStore(t)
	repo := NewProjectRepo(s.DB())
	ctx := context.Background()

	p := &domain.Project{
		ID: uuid.New(), Name: "temp", Description: "Temporary",
		DefaultWorkflow: "default",
		CreatedAt:       time.Now().UTC().Truncate(time.Millisecond),
	}
	if err := repo.Create(ctx, p); err != nil {
		t.Fatal(err)
	}

	// Delete should succeed.
	if err := repo.Delete(ctx, p.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// After deletion, GetByID should return ErrNotFound.
	_, err := repo.GetByID(ctx, p.ID)
	if err != domain.ErrNotFound {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
}
```

**What each test does — summary table:**

| Test | Creates data? | Method under test | What it checks |
|------|--------------|-------------------|----------------|
| `TestProjectCreate` | Yes (new project) | `Create` + `GetByID` | Round-trip: data survives insert and select |
| `TestProjectGetByName` | No (uses seeded data) | `GetByName` | Can find project by name |
| `TestProjectGetByIDNotFound` | No | `GetByID` | Returns `domain.ErrNotFound` for missing ID |
| `TestProjectGetByNameNotFound` | No | `GetByName` | Returns `domain.ErrNotFound` for missing name |
| `TestProjectList` | Yes (1 new project) | `List` | Returns all projects (seeded + new) |
| `TestProjectUpdate` | Yes (new project) | `Update` + `GetByID` | Mutated field persists after update |
| `TestProjectDelete` | Yes (new project) | `Delete` + `GetByID` | Project is gone after delete |

- [ ] **Step 2: Verify the tests do NOT compile**

Run:
```bash
cd /Users/germanamz/projects/tusk && go test ./internal/sqlite/ -run TestProject -count=1
```

Expected: **Compile error**. You should see errors like `undefined: NewProjectRepo` because we have not written the implementation yet. This is the "red" phase of TDD — the tests exist but the code to make them pass does not.

If you see a different error (like an import error or a syntax error in your test file), fix the test file before proceeding.

- [ ] **Step 3: Commit the test file**

```bash
git add internal/sqlite/project_test.go
git commit -m "test(sqlite): add ProjectRepo tests (red phase — implementation pending)"
```

---

### Task 2: Implement `ProjectRepo` (`project.go`)

Now we write the implementation to make the tests pass.

**Files:**
- Modify: `internal/sqlite/project.go` (currently contains only `package sqlite`)

- [ ] **Step 1: Write `project.go`**

Replace the contents of `internal/sqlite/project.go` with the following COMPLETE file:

```go
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/germanamz/tusk/internal/domain"
	"github.com/google/uuid"
)

// ProjectRepo implements repository.ProjectRepository using SQLite.
//
// It stores *sql.DB (not *Store) so it depends only on the standard library's
// database abstraction. The Store is responsible for opening the DB and running
// migrations; ProjectRepo just runs queries.
type ProjectRepo struct {
	db *sql.DB
}

// NewProjectRepo creates a ProjectRepo. Pass in the *sql.DB from Store.DB().
//
// Example:
//
//	store, _ := sqlite.New("tusk.db", migrations.FS)
//	repo := sqlite.NewProjectRepo(store.DB())
func NewProjectRepo(db *sql.DB) *ProjectRepo {
	return &ProjectRepo{db: db}
}

// Create inserts a new project into the database.
//
// The caller must set all fields on the Project struct before calling Create.
// The ID should be generated with uuid.New() and CreatedAt should be set to
// time.Now().UTC().
//
// If a project with the same name already exists, SQLite returns a UNIQUE
// constraint error (because the name column has a UNIQUE constraint).
func (r *ProjectRepo) Create(ctx context.Context, project *domain.Project) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO projects (id, name, description, default_workflow, created_at)
		 VALUES (?, ?, ?, ?, ?)`,
		project.ID.String(), project.Name, project.Description,
		project.DefaultWorkflow,
		project.CreatedAt.UTC().Format(timeFormat),
	)
	return err
}

// GetByID retrieves a project by its UUID primary key.
// Returns domain.ErrNotFound if no project has that ID.
func (r *ProjectRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.Project, error) {
	return r.scanOne(r.db.QueryRowContext(ctx,
		`SELECT id, name, description, default_workflow, created_at
		 FROM projects WHERE id = ?`, id.String()))
}

// GetByName retrieves a project by its unique name.
// Returns domain.ErrNotFound if no project has that name.
func (r *ProjectRepo) GetByName(ctx context.Context, name string) (*domain.Project, error) {
	return r.scanOne(r.db.QueryRowContext(ctx,
		`SELECT id, name, description, default_workflow, created_at
		 FROM projects WHERE name = ?`, name))
}

// List returns all projects in the database.
// The result includes the seeded "_default" project.
// Returns an empty slice (not nil) if somehow there are no projects,
// though in practice the seeded project is always present.
func (r *ProjectRepo) List(ctx context.Context) ([]*domain.Project, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, name, description, default_workflow, created_at FROM projects`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []*domain.Project
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, p)
	}
	return result, rows.Err()
}

// Update modifies an existing project's name, description, and default_workflow.
// The project is identified by its ID. CreatedAt is NOT updated (it is immutable).
//
// Note: this method does NOT check RowsAffected. If the ID does not exist,
// the UPDATE silently affects 0 rows. This is intentional — Update is typically
// called right after GetByID, so the project is known to exist.
func (r *ProjectRepo) Update(ctx context.Context, project *domain.Project) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE projects SET name = ?, description = ?, default_workflow = ?
		 WHERE id = ?`,
		project.Name, project.Description, project.DefaultWorkflow,
		project.ID.String(),
	)
	return err
}

// Delete removes a project by ID.
// Returns domain.ErrNotFound if no project with that ID exists.
//
// Unlike Update, Delete checks RowsAffected because deleting a non-existent
// resource is an error the caller should know about (e.g., to return 404).
func (r *ProjectRepo) Delete(ctx context.Context, id uuid.UUID) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM projects WHERE id = ?`, id.String())
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

// scanOne is a helper that scans a single row and translates sql.ErrNoRows
// into domain.ErrNotFound. It is used by GetByID and GetByName.
//
// Why a separate method instead of inlining this in GetByID/GetByName?
// Because the translation logic (sql.ErrNoRows -> domain.ErrNotFound) is
// easy to forget or get wrong. Having it in one place means we only need
// to get it right once.
func (r *ProjectRepo) scanOne(row *sql.Row) (*domain.Project, error) {
	p, err := scanProject(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	return p, err
}

// projectScanner is a tiny interface satisfied by both *sql.Row and *sql.Rows.
// This lets scanProject work with either type, avoiding code duplication.
//
// *sql.Row is returned by QueryRowContext (single row lookup).
// *sql.Rows is returned by QueryContext (multiple row iteration).
// Both have a Scan method with the same signature.
type projectScanner interface {
	Scan(dest ...any) error
}

// scanProject reads one row of project data from a scanner.
// It is a package-level function (not a method on ProjectRepo) because it
// does not need access to the database — it only needs the scanner.
//
// The function:
// 1. Scans the 5 columns (id, name, description, default_workflow, created_at)
//    into local variables. id and created_at are scanned as strings because
//    SQLite stores them as TEXT.
// 2. Parses the id string into a uuid.UUID.
// 3. Parses the created_at string into a time.Time using timeFormat.
// 4. Returns the assembled *domain.Project.
func scanProject(s projectScanner) (*domain.Project, error) {
	var (
		p         domain.Project
		id        string
		createdAt string
	)
	err := s.Scan(&id, &p.Name, &p.Description, &p.DefaultWorkflow, &createdAt)
	if err != nil {
		return nil, err
	}
	p.ID, err = uuid.Parse(id)
	if err != nil {
		return nil, err
	}
	p.CreatedAt, err = time.Parse(timeFormat, createdAt)
	if err != nil {
		return nil, err
	}
	return &p, nil
}
```

**Line-by-line explanation of important parts:**

**The struct and constructor (lines 1-20 of the type):**
- `ProjectRepo` holds a `*sql.DB`. This is Go's standard database handle. It manages a pool of connections internally.
- `NewProjectRepo` is a constructor function. In Go, constructors are regular functions that return a pointer to the struct. There is no `new` keyword like in Java.
- We take `*sql.DB` instead of `*Store` to keep the dependency narrow. `ProjectRepo` does not need to know about migrations or file paths.

**Create method:**
- `ExecContext` runs an SQL statement that does not return rows (INSERT, UPDATE, DELETE).
- `?` is a parameter placeholder. SQLite replaces each `?` with the corresponding argument, properly escaping it. This prevents SQL injection.
- `project.ID.String()` converts the UUID to its string representation (`"550e8400-e29b-41d4-a716-446655440000"`) because SQLite stores it as TEXT.
- `project.CreatedAt.UTC().Format(timeFormat)` formats the time as `"2006-01-02T15:04:05.000Z"`. The `.UTC()` ensures we always store UTC, and `timeFormat` matches what SQLite's `strftime('%Y-%m-%dT%H:%M:%fZ', 'now')` produces.

**GetByID / GetByName methods:**
- `QueryRowContext` runs a SELECT that returns at most one row. It returns a `*sql.Row`.
- We pass the `*sql.Row` directly to `scanOne`, which calls `scanProject` and translates errors.
- These methods are very concise because all the complexity lives in `scanOne` and `scanProject`.

**List method:**
- `QueryContext` runs a SELECT that can return multiple rows. It returns `*sql.Rows`.
- `defer rows.Close()` ensures the result set is closed even if we return early due to an error. Always close rows — failing to do so leaks database connections.
- The `for rows.Next()` loop advances through each row. `rows.Next()` returns false when there are no more rows or an error occurred.
- `rows.Err()` at the end checks if the iteration stopped due to an error (as opposed to running out of rows).
- We call `scanProject(rows)` for each row. This works because `*sql.Rows` satisfies `projectScanner`.

**Delete method:**
- `ExecContext` returns a `sql.Result` interface. We call `RowsAffected()` to check if the DELETE actually removed anything.
- Two error checks: first for the SQL execution itself, then for `RowsAffected()` (which can fail on some drivers, though SQLite always supports it).

**scanOne and scanProject:**
- `scanOne` wraps `scanProject` to add the `sql.ErrNoRows` translation. It only works with `*sql.Row` (single-row queries).
- `scanProject` is the workhorse. It takes the `projectScanner` interface so it can work with both `*sql.Row` and `*sql.Rows`.
- The `Scan` call reads columns in the exact order of the SELECT statement. If you change the column order in the SQL, you must change the `Scan` arguments to match.

- [ ] **Step 2: Verify the tests pass**

Run:
```bash
cd /Users/germanamz/projects/tusk && go test ./internal/sqlite/ -run TestProject -v -count=1
```

Expected output (names will vary, but all should say `PASS`):
```
=== RUN   TestProjectCreate
--- PASS: TestProjectCreate
=== RUN   TestProjectGetByName
--- PASS: TestProjectGetByName
=== RUN   TestProjectGetByIDNotFound
--- PASS: TestProjectGetByIDNotFound
=== RUN   TestProjectGetByNameNotFound
--- PASS: TestProjectGetByNameNotFound
=== RUN   TestProjectList
--- PASS: TestProjectList
=== RUN   TestProjectUpdate
--- PASS: TestProjectUpdate
=== RUN   TestProjectDelete
--- PASS: TestProjectDelete
PASS
```

If any test fails, read the error message carefully. Common issues:
- **"undefined: testStore"** — means `store_test.go` is missing or not in the `sqlite` package. Phase 1 must be complete.
- **"undefined: timeFormat"** — means `store.go` does not define the `timeFormat` constant. Phase 1 must be complete.
- **Time mismatch** — make sure you are using `time.Now().UTC().Truncate(time.Millisecond)` in tests, not bare `time.Now()`.
- **"table projects has no column named ..."** — the migration SQL does not match the INSERT columns. Check that Phase 1 migrations are correct.

- [ ] **Step 3: Commit the implementation**

```bash
git add internal/sqlite/project.go
git commit -m "feat(sqlite): implement ProjectRepo with full CRUD operations"
```

---

### Final Verification

After both tasks are complete, run the full test suite for the `sqlite` package to make sure `ProjectRepo` does not break anything from Phase 1:

- [ ] **Step 1: Run all sqlite tests**

```bash
cd /Users/germanamz/projects/tusk && go test ./internal/sqlite/ -v -count=1
```

Expected: ALL tests pass — both the Phase 1 Store tests and the new ProjectRepo tests.

- [ ] **Step 2: Run go vet**

```bash
cd /Users/germanamz/projects/tusk && go vet ./internal/sqlite/
```

Expected: No output (clean). `go vet` catches common mistakes like unused variables, incorrect printf format strings, and unreachable code.

- [ ] **Step 3: Verify the build**

```bash
cd /Users/germanamz/projects/tusk && go build ./...
```

Expected: No errors. The entire project still compiles.

---

## Summary of Patterns Established

After completing this phase, you have established these patterns that every future repo will follow:

| Pattern | Where it appears | Reused in |
|---------|-----------------|-----------|
| Scanner interface (`fooScanner`) | `projectScanner` in `project.go` | `taskScanner`, `relationScanner`, `tagScanner`, etc. |
| `scanFoo(s fooScanner)` function | `scanProject` in `project.go` | `scanTask`, `scanRelation`, `scanTag`, etc. |
| `scanOne` method with ErrNoRows mapping | `ProjectRepo.scanOne` | Every repo's single-row lookups |
| `RowsAffected` check in Delete | `ProjectRepo.Delete` | Every repo's Delete method |
| Compile-time interface check | `var _ repository.ProjectRepository = (*ProjectRepo)(nil)` | Every repo's test file |
| `testStore(t)` for test setup | `project_test.go` | Every repo's test file |
| `time.Now().UTC().Truncate(time.Millisecond)` for test times | `project_test.go` | Every test that creates timestamped data |

These patterns will be copy-pasted and adapted for TaskRepo, RelationRepo, TagRepo, WorkflowRepo, and AnnotationRepo in later phases.
