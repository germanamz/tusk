# SQLite Implementation with Migrations — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement the SQLite storage layer — concrete implementations of all 6 repository interfaces plus an embedded migration runner.

**Architecture:** A `Store` struct manages the SQLite connection, pragmas, and embedded migration runner. Six separate repo structs (`TaskRepo`, `ProjectRepo`, `RelationRepo`, `TagRepo`, `WorkflowRepo`, `AnnotationRepo`) each hold a `*sql.DB` and implement their respective repository interface. A small `migrations` package at the project root embeds the SQL files.

**Tech Stack:** `database/sql`, `github.com/mattn/go-sqlite3`, `embed`/`io/fs`, `encoding/json`

---

## File Structure

| File | Responsibility |
|---|---|
| `migrations/migrations.go` | Embeds `*.sql` files, exports `FS` |
| `internal/sqlite/store.go` | `Store` struct: open DB, set pragmas, run migrations, expose `DB()`, `Close()`. Shared helpers: `timeFormat` constant, `nullableUUID`, `nullableTime`, `nullableString`, `parseUUID`, `parseTime`, `marshalJSON` |
| `internal/sqlite/store_test.go` | Store tests + `testStore(t)` helper shared by all test files |
| `internal/sqlite/project.go` | `ProjectRepo` — implements `repository.ProjectRepository` |
| `internal/sqlite/project_test.go` | ProjectRepo tests |
| `internal/sqlite/task.go` | `TaskRepo` — implements `repository.TaskRepository`. Contains `scanTask` helper, `taskColumns` constant, dynamic filter builder, recursive CTE for descendants |
| `internal/sqlite/task_test.go` | TaskRepo tests: CRUD, filters, hierarchy |
| `internal/sqlite/annotation.go` | `AnnotationRepo` — implements `repository.AnnotationRepository` |
| `internal/sqlite/annotation_test.go` | AnnotationRepo tests |
| `internal/sqlite/relation.go` | `RelationRepo` — implements `repository.RelationRepository` |
| `internal/sqlite/relation_test.go` | RelationRepo tests |
| `internal/sqlite/tag.go` | `TagRepo` — implements `repository.TagRepository` (includes join table ops) |
| `internal/sqlite/tag_test.go` | TagRepo tests |
| `internal/sqlite/workflow.go` | `WorkflowRepo` — implements `repository.WorkflowRepository` (JSON statuses) |
| `internal/sqlite/workflow_test.go` | WorkflowRepo tests |

---

### Task 1: Store and Migration Runner

**Files:**
- Create: `migrations/migrations.go`
- Rewrite: `internal/sqlite/store.go`
- Create: `internal/sqlite/store_test.go`

- [ ] **Step 1: Create the migrations embed package**

Create `migrations/migrations.go`:

```go
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
```

- [ ] **Step 2: Write failing tests for Store**

Create `internal/sqlite/store_test.go`:

```go
package sqlite

import (
	"testing"

	"github.com/germanamz/tusk/migrations"
)

// testStore is shared by all test files in this package.
func testStore(t *testing.T) *Store {
	t.Helper()
	s, err := New(":memory:", migrations.FS)
	if err != nil {
		t.Fatalf("opening test store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestNew(t *testing.T) {
	s := testStore(t)
	if s.DB() == nil {
		t.Fatal("expected non-nil *sql.DB")
	}
}

func TestPragmas(t *testing.T) {
	s := testStore(t)

	var journalMode string
	if err := s.DB().QueryRow("PRAGMA journal_mode").Scan(&journalMode); err != nil {
		t.Fatal(err)
	}
	if journalMode != "wal" {
		t.Fatalf("expected wal, got %s", journalMode)
	}

	var fk int
	if err := s.DB().QueryRow("PRAGMA foreign_keys").Scan(&fk); err != nil {
		t.Fatal(err)
	}
	if fk != 1 {
		t.Fatalf("expected foreign_keys=1, got %d", fk)
	}

	var busyTimeout int
	if err := s.DB().QueryRow("PRAGMA busy_timeout").Scan(&busyTimeout); err != nil {
		t.Fatal(err)
	}
	if busyTimeout != 5000 {
		t.Fatalf("expected busy_timeout=5000, got %d", busyTimeout)
	}
}

func TestMigrations(t *testing.T) {
	s := testStore(t)

	// Verify schema_migrations table was populated
	var count int
	err := s.DB().QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&count)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected 1 migration applied, got %d", count)
	}

	// Verify seed data exists (default project)
	var name string
	err = s.DB().QueryRow("SELECT name FROM projects WHERE id = '00000000-0000-0000-0000-000000000000'").Scan(&name)
	if err != nil {
		t.Fatal(err)
	}
	if name != "_default" {
		t.Fatalf("expected _default project, got %s", name)
	}

	// Verify tables were created
	tables := []string{"projects", "tasks", "annotations", "relations", "tags", "tag_assignments", "workflows", "workflow_transitions"}
	for _, table := range tables {
		var n string
		err := s.DB().QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&n)
		if err != nil {
			t.Fatalf("table %s not found: %v", table, err)
		}
	}
}

func TestMigrationsIdempotent(t *testing.T) {
	s := testStore(t)

	// Running migrate again should be a no-op
	err := s.migrate(migrations.FS)
	if err != nil {
		t.Fatalf("second migrate call failed: %v", err)
	}

	var count int
	err = s.DB().QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&count)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected 1 migration after idempotent call, got %d", count)
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/sqlite/ -v -run "TestNew|TestPragmas|TestMigrations"`

Expected: Compilation error — `New`, `Store`, etc. not defined.

- [ ] **Step 4: Implement Store**

Rewrite `internal/sqlite/store.go`:

```go
package sqlite

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3"
)

const timeFormat = "2006-01-02T15:04:05.000Z"

// Store manages the SQLite connection and migrations.
type Store struct {
	db *sql.DB
}

// New opens a SQLite database, sets pragmas, and runs pending migrations.
func New(dbPath string, migrationsFS fs.FS) (*Store, error) {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("pinging database: %w", err)
	}

	// Pragmas must be set outside transactions.
	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA busy_timeout=5000",
		"PRAGMA foreign_keys=ON",
	} {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("setting %s: %w", pragma, err)
		}
	}

	s := &Store{db: db}
	if err := s.migrate(migrationsFS); err != nil {
		db.Close()
		return nil, fmt.Errorf("running migrations: %w", err)
	}

	return s, nil
}

// DB returns the underlying *sql.DB.
func (s *Store) DB() *sql.DB { return s.db }

// Close closes the database connection.
func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate(migrationsFS fs.FS) error {
	// Ensure schema_migrations table exists.
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER PRIMARY KEY,
			applied_at TEXT NOT NULL
		)`)
	if err != nil {
		return fmt.Errorf("creating schema_migrations: %w", err)
	}

	// Read already-applied versions.
	applied := map[int]bool{}
	rows, err := s.db.Query("SELECT version FROM schema_migrations")
	if err != nil {
		return fmt.Errorf("reading applied migrations: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			return err
		}
		applied[v] = true
	}
	if err := rows.Err(); err != nil {
		return err
	}

	// Discover *.up.sql files.
	entries, err := fs.Glob(migrationsFS, "*.up.sql")
	if err != nil {
		return fmt.Errorf("listing migration files: %w", err)
	}
	sort.Strings(entries)

	for _, name := range entries {
		// Parse version from filename prefix (e.g. "001_initial.up.sql" → 1).
		var version int
		if _, err := fmt.Sscanf(name, "%d_", &version); err != nil {
			return fmt.Errorf("parsing version from %s: %w", name, err)
		}

		if applied[version] {
			continue
		}

		data, err := fs.ReadFile(migrationsFS, name)
		if err != nil {
			return fmt.Errorf("reading %s: %w", name, err)
		}

		// Strip PRAGMA statements — they are handled in New() and cannot
		// run inside a transaction.
		statements := stripPragmas(string(data))

		tx, err := s.db.Begin()
		if err != nil {
			return fmt.Errorf("beginning tx for %s: %w", name, err)
		}

		if _, err := tx.Exec(statements); err != nil {
			tx.Rollback()
			return fmt.Errorf("executing %s: %w", name, err)
		}

		if _, err := tx.Exec(
			"INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)",
			version, time.Now().UTC().Format(timeFormat),
		); err != nil {
			tx.Rollback()
			return fmt.Errorf("recording %s: %w", name, err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("committing %s: %w", name, err)
		}
	}

	return nil
}

func stripPragmas(sql string) string {
	var lines []string
	for _, line := range strings.Split(sql, "\n") {
		trimmed := strings.TrimSpace(strings.ToUpper(line))
		if strings.HasPrefix(trimmed, "PRAGMA ") {
			continue
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

// --- Shared helpers for nullable column handling ---

func nullableUUID(id *uuid.UUID) any {
	if id == nil {
		return nil
	}
	return id.String()
}

func nullableTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UTC().Format(timeFormat)
}

func nullableString(s *string) any {
	if s == nil {
		return nil
	}
	return *s
}

func parseUUID(ns sql.NullString) (*uuid.UUID, error) {
	if !ns.Valid {
		return nil, nil
	}
	id, err := uuid.Parse(ns.String)
	if err != nil {
		return nil, err
	}
	return &id, nil
}

func parseTime(ns sql.NullString) (*time.Time, error) {
	if !ns.Valid {
		return nil, nil
	}
	t, err := time.Parse(timeFormat, ns.String)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func marshalJSON(v any) string {
	if v == nil {
		return "{}"
	}
	b, _ := json.Marshal(v)
	return string(b)
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/sqlite/ -v -run "TestNew|TestPragmas|TestMigrations"`

Expected: All 4 tests PASS.

- [ ] **Step 6: Commit**

```bash
git add migrations/migrations.go internal/sqlite/store.go internal/sqlite/store_test.go
git commit -m "feat(sqlite): add Store with connection, pragmas, and migration runner"
```

---

### Task 2: ProjectRepo

**Files:**
- Rewrite: `internal/sqlite/project.go`
- Create: `internal/sqlite/project_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/sqlite/project_test.go`:

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

var _ repository.ProjectRepository = (*ProjectRepo)(nil)

func TestProjectCreate(t *testing.T) {
	s := testStore(t)
	repo := NewProjectRepo(s.DB())
	ctx := context.Background()

	p := &domain.Project{
		ID:              uuid.New(),
		Name:            "backend",
		Description:     "Backend services",
		DefaultWorkflow: "default",
		CreatedAt:       time.Now().UTC().Truncate(time.Millisecond),
	}
	if err := repo.Create(ctx, p); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.GetByID(ctx, p.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Name != "backend" {
		t.Fatalf("expected name backend, got %s", got.Name)
	}
}

func TestProjectGetByName(t *testing.T) {
	s := testStore(t)
	repo := NewProjectRepo(s.DB())
	ctx := context.Background()

	// _default project is seeded by migration
	got, err := repo.GetByName(ctx, "_default")
	if err != nil {
		t.Fatalf("GetByName: %v", err)
	}
	if got.Name != "_default" {
		t.Fatalf("expected _default, got %s", got.Name)
	}
}

func TestProjectGetByIDNotFound(t *testing.T) {
	s := testStore(t)
	repo := NewProjectRepo(s.DB())
	ctx := context.Background()

	_, err := repo.GetByID(ctx, uuid.New())
	if err != domain.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestProjectGetByNameNotFound(t *testing.T) {
	s := testStore(t)
	repo := NewProjectRepo(s.DB())
	ctx := context.Background()

	_, err := repo.GetByName(ctx, "nonexistent")
	if err != domain.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestProjectList(t *testing.T) {
	s := testStore(t)
	repo := NewProjectRepo(s.DB())
	ctx := context.Background()

	p := &domain.Project{
		ID:              uuid.New(),
		Name:            "frontend",
		Description:     "Frontend app",
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
	// _default + frontend
	if len(projects) != 2 {
		t.Fatalf("expected 2 projects, got %d", len(projects))
	}
}

func TestProjectUpdate(t *testing.T) {
	s := testStore(t)
	repo := NewProjectRepo(s.DB())
	ctx := context.Background()

	p := &domain.Project{
		ID:              uuid.New(),
		Name:            "mobile",
		Description:     "Mobile app",
		DefaultWorkflow: "default",
		CreatedAt:       time.Now().UTC().Truncate(time.Millisecond),
	}
	if err := repo.Create(ctx, p); err != nil {
		t.Fatal(err)
	}

	p.Description = "Mobile applications"
	if err := repo.Update(ctx, p); err != nil {
		t.Fatalf("Update: %v", err)
	}

	got, err := repo.GetByID(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Description != "Mobile applications" {
		t.Fatalf("expected updated description, got %s", got.Description)
	}
}

func TestProjectDelete(t *testing.T) {
	s := testStore(t)
	repo := NewProjectRepo(s.DB())
	ctx := context.Background()

	p := &domain.Project{
		ID:              uuid.New(),
		Name:            "temp",
		Description:     "Temporary",
		DefaultWorkflow: "default",
		CreatedAt:       time.Now().UTC().Truncate(time.Millisecond),
	}
	if err := repo.Create(ctx, p); err != nil {
		t.Fatal(err)
	}

	if err := repo.Delete(ctx, p.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	_, err := repo.GetByID(ctx, p.ID)
	if err != domain.ErrNotFound {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/sqlite/ -v -run "TestProject"`

Expected: Compilation error — `ProjectRepo`, `NewProjectRepo` not defined.

- [ ] **Step 3: Implement ProjectRepo**

Rewrite `internal/sqlite/project.go`:

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

type ProjectRepo struct {
	db *sql.DB
}

func NewProjectRepo(db *sql.DB) *ProjectRepo {
	return &ProjectRepo{db: db}
}

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

func (r *ProjectRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.Project, error) {
	return r.scanOne(r.db.QueryRowContext(ctx,
		`SELECT id, name, description, default_workflow, created_at
		 FROM projects WHERE id = ?`, id.String()))
}

func (r *ProjectRepo) GetByName(ctx context.Context, name string) (*domain.Project, error) {
	return r.scanOne(r.db.QueryRowContext(ctx,
		`SELECT id, name, description, default_workflow, created_at
		 FROM projects WHERE name = ?`, name))
}

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

func (r *ProjectRepo) Update(ctx context.Context, project *domain.Project) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE projects SET name = ?, description = ?, default_workflow = ?
		 WHERE id = ?`,
		project.Name, project.Description, project.DefaultWorkflow,
		project.ID.String(),
	)
	return err
}

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

func (r *ProjectRepo) scanOne(row *sql.Row) (*domain.Project, error) {
	p, err := scanProject(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	return p, err
}

type projectScanner interface {
	Scan(dest ...any) error
}

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

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/sqlite/ -v -run "TestProject"`

Expected: All 7 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/sqlite/project.go internal/sqlite/project_test.go
git commit -m "feat(sqlite): implement ProjectRepo"
```

---

### Task 3: TaskRepo CRUD

**Files:**
- Rewrite: `internal/sqlite/task.go`
- Create: `internal/sqlite/task_test.go`

- [ ] **Step 1: Write failing tests for CRUD operations**

Create `internal/sqlite/task_test.go`:

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
	ctx := context.Background()

	_, err := repo.GetByID(ctx, uuid.New())
	if err != domain.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestTaskGetByShortIDNotFound(t *testing.T) {
	s := testStore(t)
	repo := NewTaskRepo(s.DB())
	ctx := context.Background()

	_, err := repo.GetByShortID(ctx, "nonexist")
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
	if got.Priority != 4 {
		t.Fatalf("expected priority 4, got %d", got.Priority)
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

	// Simulate stale version
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

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/sqlite/ -v -run "TestTask"`

Expected: Compilation error — `TaskRepo`, `NewTaskRepo` not defined.

- [ ] **Step 3: Implement TaskRepo CRUD**

Rewrite `internal/sqlite/task.go`:

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

func (r *TaskRepo) List(ctx context.Context, filter domain.TaskFilter) ([]*domain.Task, error) {
	// Implemented in Task 4
	return nil, fmt.Errorf("not implemented")
}

func (r *TaskRepo) GetChildren(ctx context.Context, parentID uuid.UUID) ([]*domain.Task, error) {
	// Implemented in Task 5
	return nil, fmt.Errorf("not implemented")
}

func (r *TaskRepo) GetDescendants(ctx context.Context, rootID uuid.UUID) ([]*domain.Task, error) {
	// Implemented in Task 5
	return nil, fmt.Errorf("not implemented")
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

// buildFilter constructs a dynamic WHERE clause from a TaskFilter.
// Used by List. Defined here, implemented in Task 4.
func buildFilter(filter domain.TaskFilter) (ctePrefix string, where string, args []any) {
	var conditions []string
	// Placeholder — replaced in Task 4.
	return "", strings.Join(conditions, " AND "), args
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/sqlite/ -v -run "TestTask"`

Expected: All 10 CRUD tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/sqlite/task.go internal/sqlite/task_test.go
git commit -m "feat(sqlite): implement TaskRepo CRUD with optimistic locking"
```

---

### Task 4: TaskRepo List with Dynamic Filters

**Files:**
- Modify: `internal/sqlite/task.go` (replace `buildFilter` and `List`)
- Modify: `internal/sqlite/task_test.go` (add filter tests)

- [ ] **Step 1: Write failing tests for List**

Append to `internal/sqlite/task_test.go`:

```go
func TestTaskListEmpty(t *testing.T) {
	s := testStore(t)
	repo := NewTaskRepo(s.DB())
	ctx := context.Background()

	tasks, err := repo.List(ctx, domain.TaskFilter{})
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

	t1 := newTestTask()
	t1.Status = "pending"
	mustCreateTask(t, repo, t1)

	t2 := newTestTask()
	t2.Status = "active"
	mustCreateTask(t, repo, t2)

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
		task := newTestTask()
		task.Status = status
		mustCreateTask(t, repo, task)
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

	t1 := newTestTask()
	t1.ProjectID = &proj.ID
	mustCreateTask(t, repo, t1)

	t2 := newTestTask()
	mustCreateTask(t, repo, t2) // no project

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
		task := newTestTask()
		task.Priority = p
		mustCreateTask(t, repo, task)
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
		task := newTestTask()
		task.DueAt = d
		mustCreateTask(t, repo, task)
	}

	after := time.Date(2026, 5, 31, 0, 0, 0, 0, time.UTC)
	before := time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC)
	tasks, err := repo.List(ctx, domain.TaskFilter{DueAfter: &after, DueBefore: &before})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks (June 1 and 15), got %d", len(tasks))
	}
}

func TestTaskListByParent(t *testing.T) {
	s := testStore(t)
	repo := NewTaskRepo(s.DB())
	ctx := context.Background()

	parent := newTestTask()
	mustCreateTask(t, repo, parent)

	child := newTestTask()
	child.ParentID = &parent.ID
	mustCreateTask(t, repo, child)

	orphan := newTestTask()
	mustCreateTask(t, repo, orphan)

	tasks, err := repo.List(ctx, domain.TaskFilter{ParentID: &parent.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 child, got %d", len(tasks))
	}
	if tasks[0].ID != child.ID {
		t.Fatalf("expected child task")
	}
}

func TestTaskListWaitingOnly(t *testing.T) {
	s := testStore(t)
	repo := NewTaskRepo(s.DB())
	ctx := context.Background()

	future := time.Now().UTC().Add(24 * time.Hour)
	past := time.Now().UTC().Add(-24 * time.Hour)

	t1 := newTestTask()
	t1.WaitUntil = &future
	mustCreateTask(t, repo, t1)

	t2 := newTestTask()
	t2.WaitUntil = &past
	mustCreateTask(t, repo, t2)

	t3 := newTestTask()
	mustCreateTask(t, repo, t3) // no wait_until

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

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/sqlite/ -v -run "TestTaskList"`

Expected: Tests fail — `List` returns "not implemented" error.

- [ ] **Step 3: Implement buildFilter and replace List**

Replace the `buildFilter` function and `List` method in `internal/sqlite/task.go`:

```go
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

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/sqlite/ -v -run "TestTaskList"`

Expected: All 9 List tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/sqlite/task.go internal/sqlite/task_test.go
git commit -m "feat(sqlite): implement TaskRepo.List with dynamic filter builder"
```

---

### Task 5: TaskRepo Hierarchy (GetChildren, GetDescendants)

**Files:**
- Modify: `internal/sqlite/task.go` (replace stub methods)
- Modify: `internal/sqlite/task_test.go` (add hierarchy tests)

- [ ] **Step 1: Write failing tests**

Append to `internal/sqlite/task_test.go`:

```go
func TestTaskGetChildren(t *testing.T) {
	s := testStore(t)
	repo := NewTaskRepo(s.DB())
	ctx := context.Background()

	parent := newTestTask()
	mustCreateTask(t, repo, parent)

	child1 := newTestTask()
	child1.ParentID = &parent.ID
	mustCreateTask(t, repo, child1)

	child2 := newTestTask()
	child2.ParentID = &parent.ID
	mustCreateTask(t, repo, child2)

	// grandchild should NOT appear
	grandchild := newTestTask()
	grandchild.ParentID = &child1.ID
	mustCreateTask(t, repo, grandchild)

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
	ctx := context.Background()

	task := newTestTask()
	mustCreateTask(t, repo, task)

	children, err := repo.GetChildren(ctx, task.ID)
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

	// root → child1 → grandchild
	//      → child2
	root := newTestTask()
	mustCreateTask(t, repo, root)

	child1 := newTestTask()
	child1.ParentID = &root.ID
	mustCreateTask(t, repo, child1)

	child2 := newTestTask()
	child2.ParentID = &root.ID
	mustCreateTask(t, repo, child2)

	grandchild := newTestTask()
	grandchild.ParentID = &child1.ID
	mustCreateTask(t, repo, grandchild)

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
	ctx := context.Background()

	task := newTestTask()
	mustCreateTask(t, repo, task)

	descendants, err := repo.GetDescendants(ctx, task.ID)
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

	root := newTestTask()
	mustCreateTask(t, repo, root)

	child := newTestTask()
	child.ParentID = &root.ID
	mustCreateTask(t, repo, child)

	grandchild := newTestTask()
	grandchild.ParentID = &child.ID
	mustCreateTask(t, repo, grandchild)

	unrelated := newTestTask()
	mustCreateTask(t, repo, unrelated)

	tasks, err := repo.List(ctx, domain.TaskFilter{RootID: &root.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 2 {
		t.Fatalf("expected 2 descendants via List, got %d", len(tasks))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/sqlite/ -v -run "TestTaskGetChildren|TestTaskGetDescendants|TestTaskListByRootID"`

Expected: Tests fail — `GetChildren` and `GetDescendants` return "not implemented".

- [ ] **Step 3: Implement GetChildren and GetDescendants**

Replace the stub methods in `internal/sqlite/task.go`:

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
// e.g. prefixColumns("t", "id, name") → "t.id, t.name"
func prefixColumns(alias, cols string) string {
	parts := strings.Split(cols, ",")
	for i, p := range parts {
		parts[i] = alias + "." + strings.TrimSpace(p)
	}
	return strings.Join(parts, ", ")
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/sqlite/ -v -run "TestTaskGetChildren|TestTaskGetDescendants|TestTaskListByRootID"`

Expected: All 5 hierarchy tests PASS.

- [ ] **Step 5: Run all TaskRepo tests to confirm nothing broke**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/sqlite/ -v -run "TestTask"`

Expected: All task tests PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/sqlite/task.go internal/sqlite/task_test.go
git commit -m "feat(sqlite): implement TaskRepo hierarchy with recursive CTE"
```

---

### Task 6: AnnotationRepo

**Files:**
- Create: `internal/sqlite/annotation.go`
- Create: `internal/sqlite/annotation_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/sqlite/annotation_test.go`:

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

var _ repository.AnnotationRepository = (*AnnotationRepo)(nil)

func TestAnnotationCreate(t *testing.T) {
	s := testStore(t)
	taskRepo := NewTaskRepo(s.DB())
	repo := NewAnnotationRepo(s.DB())
	ctx := context.Background()

	task := newTestTask()
	mustCreateTask(t, taskRepo, task)

	ann := &domain.Annotation{
		ID:        uuid.New(),
		TaskID:    task.ID,
		Body:      "Blocked by upstream API changes",
		CreatedAt: time.Now().UTC().Truncate(time.Millisecond),
	}
	if err := repo.Create(ctx, ann); err != nil {
		t.Fatalf("Create: %v", err)
	}

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

func TestAnnotationGetByTaskEmpty(t *testing.T) {
	s := testStore(t)
	taskRepo := NewTaskRepo(s.DB())
	repo := NewAnnotationRepo(s.DB())
	ctx := context.Background()

	task := newTestTask()
	mustCreateTask(t, taskRepo, task)

	anns, err := repo.GetByTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(anns) != 0 {
		t.Fatalf("expected 0 annotations, got %d", len(anns))
	}
}

func TestAnnotationGetByTaskMultiple(t *testing.T) {
	s := testStore(t)
	taskRepo := NewTaskRepo(s.DB())
	repo := NewAnnotationRepo(s.DB())
	ctx := context.Background()

	task := newTestTask()
	mustCreateTask(t, taskRepo, task)

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

	if err := repo.Delete(ctx, ann.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	anns, err := repo.GetByTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(anns) != 0 {
		t.Fatalf("expected 0 annotations after delete, got %d", len(anns))
	}
}

func TestAnnotationDeleteNotFound(t *testing.T) {
	s := testStore(t)
	repo := NewAnnotationRepo(s.DB())
	ctx := context.Background()

	err := repo.Delete(ctx, uuid.New())
	if err != domain.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/sqlite/ -v -run "TestAnnotation"`

Expected: Compilation error — `AnnotationRepo`, `NewAnnotationRepo` not defined.

- [ ] **Step 3: Implement AnnotationRepo**

Create `internal/sqlite/annotation.go`:

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

type AnnotationRepo struct {
	db *sql.DB
}

func NewAnnotationRepo(db *sql.DB) *AnnotationRepo {
	return &AnnotationRepo{db: db}
}

func (r *AnnotationRepo) Create(ctx context.Context, ann *domain.Annotation) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO annotations (id, task_id, body, created_at) VALUES (?, ?, ?, ?)`,
		ann.ID.String(), ann.TaskID.String(), ann.Body,
		ann.CreatedAt.UTC().Format(timeFormat),
	)
	return err
}

func (r *AnnotationRepo) GetByTask(ctx context.Context, taskID uuid.UUID) ([]*domain.Annotation, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, task_id, body, created_at FROM annotations WHERE task_id = ? ORDER BY created_at`,
		taskID.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []*domain.Annotation
	for rows.Next() {
		var (
			a         domain.Annotation
			id        string
			tid       string
			createdAt string
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

// Verify interface satisfaction at compile time.
var _ interface {
	Create(context.Context, *domain.Annotation) error
	GetByTask(context.Context, uuid.UUID) ([]*domain.Annotation, error)
	Delete(context.Context, uuid.UUID) error
} = (*AnnotationRepo)(nil)
```

Remove the redundant compile-time check from `annotation.go` — the one in the test file using `repository.AnnotationRepository` is more precise. Delete the `var _` block at the bottom of the implementation file above (keep only the test file's check).

Actually, remove that `var _` block entirely from the implementation — the test file already has `var _ repository.AnnotationRepository = (*AnnotationRepo)(nil)`.

Final `annotation.go` should NOT include the `var _` block at the bottom.

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/sqlite/ -v -run "TestAnnotation"`

Expected: All 5 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/sqlite/annotation.go internal/sqlite/annotation_test.go
git commit -m "feat(sqlite): implement AnnotationRepo"
```

---

### Task 7: RelationRepo

**Files:**
- Rewrite: `internal/sqlite/relation.go`
- Create: `internal/sqlite/relation_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/sqlite/relation_test.go`:

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

var _ repository.RelationRepository = (*RelationRepo)(nil)

func newTestRelation(sourceID, targetID uuid.UUID, relType string) *domain.Relation {
	return &domain.Relation{
		ID:           uuid.New(),
		SourceID:     sourceID,
		TargetID:     targetID,
		RelationType: relType,
		CreatedAt:    time.Now().UTC().Truncate(time.Millisecond),
	}
}

func TestRelationCreate(t *testing.T) {
	s := testStore(t)
	taskRepo := NewTaskRepo(s.DB())
	repo := NewRelationRepo(s.DB())
	ctx := context.Background()

	t1 := newTestTask()
	t2 := newTestTask()
	mustCreateTask(t, taskRepo, t1)
	mustCreateTask(t, taskRepo, t2)

	rel := newTestRelation(t1.ID, t2.ID, "blocks")
	if err := repo.Create(ctx, rel); err != nil {
		t.Fatalf("Create: %v", err)
	}

	rels, err := repo.GetByTask(ctx, t1.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rels) != 1 {
		t.Fatalf("expected 1 relation, got %d", len(rels))
	}
	if rels[0].RelationType != "blocks" {
		t.Fatalf("expected blocks, got %s", rels[0].RelationType)
	}
}

func TestRelationCreateDuplicate(t *testing.T) {
	s := testStore(t)
	taskRepo := NewTaskRepo(s.DB())
	repo := NewRelationRepo(s.DB())
	ctx := context.Background()

	t1 := newTestTask()
	t2 := newTestTask()
	mustCreateTask(t, taskRepo, t1)
	mustCreateTask(t, taskRepo, t2)

	rel1 := newTestRelation(t1.ID, t2.ID, "blocks")
	if err := repo.Create(ctx, rel1); err != nil {
		t.Fatal(err)
	}

	rel2 := newTestRelation(t1.ID, t2.ID, "blocks")
	err := repo.Create(ctx, rel2)
	if err != domain.ErrDuplicateRelation {
		t.Fatalf("expected ErrDuplicateRelation, got %v", err)
	}
}

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

	if err := repo.Delete(ctx, rel.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	rels, err := repo.GetByTask(ctx, t1.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rels) != 0 {
		t.Fatalf("expected 0 relations after delete, got %d", len(rels))
	}
}

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

	// t1 blocks t2, t3 relates_to t1
	if err := repo.Create(ctx, newTestRelation(t1.ID, t2.ID, "blocks")); err != nil {
		t.Fatal(err)
	}
	if err := repo.Create(ctx, newTestRelation(t3.ID, t1.ID, "relates_to")); err != nil {
		t.Fatal(err)
	}

	// GetByTask returns relations where task is source OR target
	rels, err := repo.GetByTask(ctx, t1.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rels) != 2 {
		t.Fatalf("expected 2 relations for t1, got %d", len(rels))
	}
}

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

	// t1 blocks t2 and t3
	if err := repo.Create(ctx, newTestRelation(t1.ID, t2.ID, "blocks")); err != nil {
		t.Fatal(err)
	}
	if err := repo.Create(ctx, newTestRelation(t1.ID, t3.ID, "blocks")); err != nil {
		t.Fatal(err)
	}
	// unrelated relation
	if err := repo.Create(ctx, newTestRelation(t2.ID, t3.ID, "relates_to")); err != nil {
		t.Fatal(err)
	}

	blocking, err := repo.GetBlocking(ctx, t1.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(blocking) != 2 {
		t.Fatalf("expected 2 blocking relations, got %d", len(blocking))
	}
}

func TestRelationGetBlockedBy(t *testing.T) {
	s := testStore(t)
	taskRepo := NewTaskRepo(s.DB())
	repo := NewRelationRepo(s.DB())
	ctx := context.Background()

	t1 := newTestTask()
	t2 := newTestTask()
	mustCreateTask(t, taskRepo, t1)
	mustCreateTask(t, taskRepo, t2)

	// t1 blocks t2 → t2 is blocked by t1
	if err := repo.Create(ctx, newTestRelation(t1.ID, t2.ID, "blocks")); err != nil {
		t.Fatal(err)
	}

	blockedBy, err := repo.GetBlockedBy(ctx, t2.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(blockedBy) != 1 {
		t.Fatalf("expected 1 blocked_by relation, got %d", len(blockedBy))
	}
	if blockedBy[0].SourceID != t1.ID {
		t.Fatalf("expected source to be t1")
	}
}

func TestRelationExists(t *testing.T) {
	s := testStore(t)
	taskRepo := NewTaskRepo(s.DB())
	repo := NewRelationRepo(s.DB())
	ctx := context.Background()

	t1 := newTestTask()
	t2 := newTestTask()
	mustCreateTask(t, taskRepo, t1)
	mustCreateTask(t, taskRepo, t2)

	if err := repo.Create(ctx, newTestRelation(t1.ID, t2.ID, "blocks")); err != nil {
		t.Fatal(err)
	}

	exists, err := repo.Exists(ctx, t1.ID, t2.ID, "blocks")
	if err != nil {
		t.Fatal(err)
	}
	if !exists {
		t.Fatal("expected relation to exist")
	}

	exists, err = repo.Exists(ctx, t2.ID, t1.ID, "blocks")
	if err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Fatal("expected reverse relation to not exist")
	}

	exists, err = repo.Exists(ctx, t1.ID, t2.ID, "relates_to")
	if err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Fatal("expected different type to not exist")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/sqlite/ -v -run "TestRelation"`

Expected: Compilation error — `RelationRepo`, `NewRelationRepo` not defined.

- [ ] **Step 3: Implement RelationRepo**

Rewrite `internal/sqlite/relation.go`:

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

const relationColumns = `id, source_id, target_id, relation_type, created_at`

type RelationRepo struct {
	db *sql.DB
}

func NewRelationRepo(db *sql.DB) *RelationRepo {
	return &RelationRepo{db: db}
}

func (r *RelationRepo) Create(ctx context.Context, rel *domain.Relation) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO relations (id, source_id, target_id, relation_type, created_at)
		 VALUES (?, ?, ?, ?, ?)`,
		rel.ID.String(), rel.SourceID.String(), rel.TargetID.String(),
		rel.RelationType,
		rel.CreatedAt.UTC().Format(timeFormat),
	)
	if err != nil && isUniqueViolation(err) {
		return domain.ErrDuplicateRelation
	}
	return err
}

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

func (r *RelationRepo) GetByTask(ctx context.Context, taskID uuid.UUID) ([]*domain.Relation, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+relationColumns+` FROM relations
		 WHERE source_id = ? OR target_id = ?`,
		taskID.String(), taskID.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRelations(rows)
}

func (r *RelationRepo) GetBlocking(ctx context.Context, taskID uuid.UUID) ([]*domain.Relation, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+relationColumns+` FROM relations
		 WHERE source_id = ? AND relation_type = 'blocks'`,
		taskID.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRelations(rows)
}

func (r *RelationRepo) GetBlockedBy(ctx context.Context, taskID uuid.UUID) ([]*domain.Relation, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+relationColumns+` FROM relations
		 WHERE target_id = ? AND relation_type = 'blocks'`,
		taskID.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRelations(rows)
}

func (r *RelationRepo) Exists(ctx context.Context, sourceID, targetID uuid.UUID, relType string) (bool, error) {
	var exists bool
	err := r.db.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM relations
		 WHERE source_id = ? AND target_id = ? AND relation_type = ?)`,
		sourceID.String(), targetID.String(), relType).Scan(&exists)
	return exists, err
}

func scanRelations(rows *sql.Rows) ([]*domain.Relation, error) {
	var result []*domain.Relation
	for rows.Next() {
		var (
			r         domain.Relation
			id        string
			sourceID  string
			targetID  string
			createdAt string
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

// isUniqueViolation checks if a SQLite error is a UNIQUE constraint violation.
func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/sqlite/ -v -run "TestRelation"`

Expected: All 7 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/sqlite/relation.go internal/sqlite/relation_test.go
git commit -m "feat(sqlite): implement RelationRepo"
```

---

### Task 8: TagRepo

**Files:**
- Rewrite: `internal/sqlite/tag.go`
- Create: `internal/sqlite/tag_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/sqlite/tag_test.go`:

```go
package sqlite

import (
	"context"
	"testing"

	"github.com/germanamz/tusk/internal/domain"
	"github.com/germanamz/tusk/internal/repository"
	"github.com/google/uuid"
)

var _ repository.TagRepository = (*TagRepo)(nil)

func TestTagCreate(t *testing.T) {
	s := testStore(t)
	repo := NewTagRepo(s.DB())
	ctx := context.Background()

	color := "#ff0000"
	tag := &domain.Tag{
		ID:    uuid.New(),
		Name:  "bug",
		Color: &color,
	}
	if err := repo.Create(ctx, tag); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.GetByName(ctx, "bug")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "bug" {
		t.Fatalf("expected bug, got %s", got.Name)
	}
	if got.Color == nil || *got.Color != "#ff0000" {
		t.Fatalf("expected color #ff0000, got %v", got.Color)
	}
}

func TestTagCreateNullColor(t *testing.T) {
	s := testStore(t)
	repo := NewTagRepo(s.DB())
	ctx := context.Background()

	tag := &domain.Tag{ID: uuid.New(), Name: "frontend"}
	if err := repo.Create(ctx, tag); err != nil {
		t.Fatal(err)
	}

	got, err := repo.GetByName(ctx, "frontend")
	if err != nil {
		t.Fatal(err)
	}
	if got.Color != nil {
		t.Fatalf("expected nil color, got %v", got.Color)
	}
}

func TestTagGetByNameNotFound(t *testing.T) {
	s := testStore(t)
	repo := NewTagRepo(s.DB())
	ctx := context.Background()

	_, err := repo.GetByName(ctx, "nonexistent")
	if err != domain.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestTagList(t *testing.T) {
	s := testStore(t)
	repo := NewTagRepo(s.DB())
	ctx := context.Background()

	for _, name := range []string{"bug", "feature", "docs"} {
		tag := &domain.Tag{ID: uuid.New(), Name: name}
		if err := repo.Create(ctx, tag); err != nil {
			t.Fatal(err)
		}
	}

	tags, err := repo.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(tags) != 3 {
		t.Fatalf("expected 3 tags, got %d", len(tags))
	}
}

func TestTagAssignToTask(t *testing.T) {
	s := testStore(t)
	taskRepo := NewTaskRepo(s.DB())
	tagRepo := NewTagRepo(s.DB())
	ctx := context.Background()

	task := newTestTask()
	mustCreateTask(t, taskRepo, task)

	tag := &domain.Tag{ID: uuid.New(), Name: "urgent"}
	if err := tagRepo.Create(ctx, tag); err != nil {
		t.Fatal(err)
	}

	if err := tagRepo.AssignToTask(ctx, task.ID, tag.ID); err != nil {
		t.Fatalf("AssignToTask: %v", err)
	}

	tags, err := tagRepo.GetTaskTags(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tags) != 1 {
		t.Fatalf("expected 1 tag, got %d", len(tags))
	}
	if tags[0].Name != "urgent" {
		t.Fatalf("expected urgent, got %s", tags[0].Name)
	}
}

func TestTagRemoveFromTask(t *testing.T) {
	s := testStore(t)
	taskRepo := NewTaskRepo(s.DB())
	tagRepo := NewTagRepo(s.DB())
	ctx := context.Background()

	task := newTestTask()
	mustCreateTask(t, taskRepo, task)

	tag := &domain.Tag{ID: uuid.New(), Name: "temp"}
	if err := tagRepo.Create(ctx, tag); err != nil {
		t.Fatal(err)
	}
	if err := tagRepo.AssignToTask(ctx, task.ID, tag.ID); err != nil {
		t.Fatal(err)
	}

	if err := tagRepo.RemoveFromTask(ctx, task.ID, tag.ID); err != nil {
		t.Fatalf("RemoveFromTask: %v", err)
	}

	tags, err := tagRepo.GetTaskTags(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tags) != 0 {
		t.Fatalf("expected 0 tags after remove, got %d", len(tags))
	}
}

func TestTagGetTaskTagsEmpty(t *testing.T) {
	s := testStore(t)
	taskRepo := NewTaskRepo(s.DB())
	tagRepo := NewTagRepo(s.DB())
	ctx := context.Background()

	task := newTestTask()
	mustCreateTask(t, taskRepo, task)

	tags, err := tagRepo.GetTaskTags(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tags) != 0 {
		t.Fatalf("expected 0 tags, got %d", len(tags))
	}
}

func TestTagFilterIntegration(t *testing.T) {
	s := testStore(t)
	taskRepo := NewTaskRepo(s.DB())
	tagRepo := NewTagRepo(s.DB())
	ctx := context.Background()

	// Create tags
	bugTag := &domain.Tag{ID: uuid.New(), Name: "bug"}
	apiTag := &domain.Tag{ID: uuid.New(), Name: "api"}
	docsTag := &domain.Tag{ID: uuid.New(), Name: "docs"}
	for _, tag := range []*domain.Tag{bugTag, apiTag, docsTag} {
		if err := tagRepo.Create(ctx, tag); err != nil {
			t.Fatal(err)
		}
	}

	// t1 has bug+api, t2 has bug+docs, t3 has api only
	t1 := newTestTask()
	t2 := newTestTask()
	t3 := newTestTask()
	for _, task := range []*domain.Task{t1, t2, t3} {
		mustCreateTask(t, taskRepo, task)
	}

	for _, pair := range [][2]uuid.UUID{
		{t1.ID, bugTag.ID}, {t1.ID, apiTag.ID},
		{t2.ID, bugTag.ID}, {t2.ID, docsTag.ID},
		{t3.ID, apiTag.ID},
	} {
		if err := tagRepo.AssignToTask(ctx, pair[0], pair[1]); err != nil {
			t.Fatal(err)
		}
	}

	// Filter: must have bug AND api → only t1
	tasks, err := taskRepo.List(ctx, domain.TaskFilter{Tags: []string{"bug", "api"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 || tasks[0].ID != t1.ID {
		t.Fatalf("expected only t1, got %d tasks", len(tasks))
	}

	// Filter: exclude docs → t1 and t3
	tasks, err = taskRepo.List(ctx, domain.TaskFilter{ExcludeTags: []string{"docs"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks excluding docs, got %d", len(tasks))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/sqlite/ -v -run "TestTag"`

Expected: Compilation error — `TagRepo`, `NewTagRepo` not defined.

- [ ] **Step 3: Implement TagRepo**

Rewrite `internal/sqlite/tag.go`:

```go
package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/germanamz/tusk/internal/domain"
	"github.com/google/uuid"
)

type TagRepo struct {
	db *sql.DB
}

func NewTagRepo(db *sql.DB) *TagRepo {
	return &TagRepo{db: db}
}

func (r *TagRepo) Create(ctx context.Context, tag *domain.Tag) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO tags (id, name, color) VALUES (?, ?, ?)`,
		tag.ID.String(), tag.Name, nullableString(tag.Color),
	)
	return err
}

func (r *TagRepo) GetByName(ctx context.Context, name string) (*domain.Tag, error) {
	var (
		tag   domain.Tag
		id    string
		color sql.NullString
	)
	err := r.db.QueryRowContext(ctx,
		`SELECT id, name, color FROM tags WHERE name = ?`, name,
	).Scan(&id, &tag.Name, &color)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	tag.ID, err = uuid.Parse(id)
	if err != nil {
		return nil, err
	}
	if color.Valid {
		tag.Color = &color.String
	}
	return &tag, nil
}

func (r *TagRepo) List(ctx context.Context) ([]*domain.Tag, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id, name, color FROM tags`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []*domain.Tag
	for rows.Next() {
		var (
			tag   domain.Tag
			id    string
			color sql.NullString
		)
		if err := rows.Scan(&id, &tag.Name, &color); err != nil {
			return nil, err
		}
		tag.ID, err = uuid.Parse(id)
		if err != nil {
			return nil, err
		}
		if color.Valid {
			tag.Color = &color.String
		}
		result = append(result, &tag)
	}
	return result, rows.Err()
}

func (r *TagRepo) AssignToTask(ctx context.Context, taskID, tagID uuid.UUID) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO tag_assignments (task_id, tag_id) VALUES (?, ?)`,
		taskID.String(), tagID.String(),
	)
	return err
}

func (r *TagRepo) RemoveFromTask(ctx context.Context, taskID, tagID uuid.UUID) error {
	_, err := r.db.ExecContext(ctx,
		`DELETE FROM tag_assignments WHERE task_id = ? AND tag_id = ?`,
		taskID.String(), tagID.String(),
	)
	return err
}

func (r *TagRepo) GetTaskTags(ctx context.Context, taskID uuid.UUID) ([]*domain.Tag, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT t.id, t.name, t.color FROM tags t
		 JOIN tag_assignments ta ON t.id = ta.tag_id
		 WHERE ta.task_id = ?`, taskID.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []*domain.Tag
	for rows.Next() {
		var (
			tag   domain.Tag
			id    string
			color sql.NullString
		)
		if err := rows.Scan(&id, &tag.Name, &color); err != nil {
			return nil, err
		}
		tag.ID, err = uuid.Parse(id)
		if err != nil {
			return nil, err
		}
		if color.Valid {
			tag.Color = &color.String
		}
		result = append(result, &tag)
	}
	return result, rows.Err()
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/sqlite/ -v -run "TestTag"`

Expected: All 8 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/sqlite/tag.go internal/sqlite/tag_test.go
git commit -m "feat(sqlite): implement TagRepo with join table operations"
```

---

### Task 9: WorkflowRepo

**Files:**
- Create: `internal/sqlite/workflow.go`
- Create: `internal/sqlite/workflow_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/sqlite/workflow_test.go`:

```go
package sqlite

import (
	"context"
	"testing"

	"github.com/germanamz/tusk/internal/domain"
	"github.com/germanamz/tusk/internal/repository"
	"github.com/google/uuid"
)

var _ repository.WorkflowRepository = (*WorkflowRepo)(nil)

func TestWorkflowGetByProjectAndName(t *testing.T) {
	s := testStore(t)
	repo := NewWorkflowRepo(s.DB())
	ctx := context.Background()

	// Default workflow is seeded by migration
	defaultProjectID := uuid.MustParse("00000000-0000-0000-0000-000000000000")
	wf, err := repo.GetByProjectAndName(ctx, defaultProjectID, "default")
	if err != nil {
		t.Fatalf("GetByProjectAndName: %v", err)
	}
	if wf.Name != "default" {
		t.Fatalf("expected default, got %s", wf.Name)
	}
	if len(wf.Statuses) != 4 {
		t.Fatalf("expected 4 statuses, got %d", len(wf.Statuses))
	}
	expected := []string{"pending", "active", "completed", "deleted"}
	for i, s := range expected {
		if wf.Statuses[i] != s {
			t.Fatalf("status[%d]: expected %s, got %s", i, s, wf.Statuses[i])
		}
	}
}

func TestWorkflowGetByProjectAndNameNotFound(t *testing.T) {
	s := testStore(t)
	repo := NewWorkflowRepo(s.DB())
	ctx := context.Background()

	_, err := repo.GetByProjectAndName(ctx, uuid.New(), "nonexistent")
	if err != domain.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestWorkflowGetTransitions(t *testing.T) {
	s := testStore(t)
	repo := NewWorkflowRepo(s.DB())
	ctx := context.Background()

	defaultProjectID := uuid.MustParse("00000000-0000-0000-0000-000000000000")
	wf, err := repo.GetByProjectAndName(ctx, defaultProjectID, "default")
	if err != nil {
		t.Fatal(err)
	}

	transitions, err := repo.GetTransitions(ctx, wf.ID)
	if err != nil {
		t.Fatal(err)
	}
	// 6 default transitions seeded by migration
	if len(transitions) != 6 {
		t.Fatalf("expected 6 transitions, got %d", len(transitions))
	}
}

func TestWorkflowCreate(t *testing.T) {
	s := testStore(t)
	projRepo := NewProjectRepo(s.DB())
	repo := NewWorkflowRepo(s.DB())
	ctx := context.Background()

	proj := &domain.Project{
		ID: uuid.New(), Name: "kanban-project",
		DefaultWorkflow: "kanban",
		CreatedAt:       mustTimeNow(),
	}
	if err := projRepo.Create(ctx, proj); err != nil {
		t.Fatal(err)
	}

	wf := &domain.Workflow{
		ID:        uuid.New(),
		ProjectID: proj.ID,
		Name:      "kanban",
		Statuses:  []string{"backlog", "in_progress", "review", "done"},
	}
	if err := repo.Create(ctx, wf); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.GetByProjectAndName(ctx, proj.ID, "kanban")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Statuses) != 4 {
		t.Fatalf("expected 4 statuses, got %d", len(got.Statuses))
	}
	if got.Statuses[0] != "backlog" {
		t.Fatalf("expected first status backlog, got %s", got.Statuses[0])
	}
}

func TestWorkflowAddTransition(t *testing.T) {
	s := testStore(t)
	projRepo := NewProjectRepo(s.DB())
	repo := NewWorkflowRepo(s.DB())
	ctx := context.Background()

	proj := &domain.Project{
		ID: uuid.New(), Name: "test-proj",
		DefaultWorkflow: "simple",
		CreatedAt:       mustTimeNow(),
	}
	if err := projRepo.Create(ctx, proj); err != nil {
		t.Fatal(err)
	}

	wf := &domain.Workflow{
		ID:        uuid.New(),
		ProjectID: proj.ID,
		Name:      "simple",
		Statuses:  []string{"open", "closed"},
	}
	if err := repo.Create(ctx, wf); err != nil {
		t.Fatal(err)
	}

	tr := &domain.WorkflowTransition{
		ID:         uuid.New(),
		WorkflowID: wf.ID,
		FromStatus: "open",
		ToStatus:   "closed",
	}
	if err := repo.AddTransition(ctx, tr); err != nil {
		t.Fatalf("AddTransition: %v", err)
	}

	transitions, err := repo.GetTransitions(ctx, wf.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(transitions) != 1 {
		t.Fatalf("expected 1 transition, got %d", len(transitions))
	}
	if transitions[0].FromStatus != "open" || transitions[0].ToStatus != "closed" {
		t.Fatalf("unexpected transition: %s → %s", transitions[0].FromStatus, transitions[0].ToStatus)
	}
}
```

Also add the `mustTimeNow` helper used above. Append to `internal/sqlite/store_test.go`:

```go
func mustTimeNow() time.Time {
	return time.Now().UTC().Truncate(time.Millisecond)
}
```

(Add `"time"` to the imports in `store_test.go` if not already present.)

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/sqlite/ -v -run "TestWorkflow"`

Expected: Compilation error — `WorkflowRepo`, `NewWorkflowRepo` not defined.

- [ ] **Step 3: Implement WorkflowRepo**

Create `internal/sqlite/workflow.go`:

```go
package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/germanamz/tusk/internal/domain"
	"github.com/google/uuid"
)

type WorkflowRepo struct {
	db *sql.DB
}

func NewWorkflowRepo(db *sql.DB) *WorkflowRepo {
	return &WorkflowRepo{db: db}
}

func (r *WorkflowRepo) GetByProjectAndName(ctx context.Context, projectID uuid.UUID, name string) (*domain.Workflow, error) {
	var (
		wf          domain.Workflow
		id          string
		pid         string
		statusesJSON string
	)
	err := r.db.QueryRowContext(ctx,
		`SELECT id, project_id, name, statuses FROM workflows
		 WHERE project_id = ? AND name = ?`,
		projectID.String(), name,
	).Scan(&id, &pid, &wf.Name, &statusesJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	wf.ID, err = uuid.Parse(id)
	if err != nil {
		return nil, err
	}
	wf.ProjectID, err = uuid.Parse(pid)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(statusesJSON), &wf.Statuses); err != nil {
		return nil, err
	}
	return &wf, nil
}

func (r *WorkflowRepo) GetTransitions(ctx context.Context, workflowID uuid.UUID) ([]*domain.WorkflowTransition, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, workflow_id, from_status, to_status
		 FROM workflow_transitions WHERE workflow_id = ?`,
		workflowID.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []*domain.WorkflowTransition
	for rows.Next() {
		var (
			t   domain.WorkflowTransition
			id  string
			wid string
		)
		if err := rows.Scan(&id, &wid, &t.FromStatus, &t.ToStatus); err != nil {
			return nil, err
		}
		t.ID, err = uuid.Parse(id)
		if err != nil {
			return nil, err
		}
		t.WorkflowID, err = uuid.Parse(wid)
		if err != nil {
			return nil, err
		}
		result = append(result, &t)
	}
	return result, rows.Err()
}

func (r *WorkflowRepo) Create(ctx context.Context, wf *domain.Workflow) error {
	statusesJSON, err := json.Marshal(wf.Statuses)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx,
		`INSERT INTO workflows (id, project_id, name, statuses) VALUES (?, ?, ?, ?)`,
		wf.ID.String(), wf.ProjectID.String(), wf.Name, string(statusesJSON),
	)
	return err
}

func (r *WorkflowRepo) AddTransition(ctx context.Context, t *domain.WorkflowTransition) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO workflow_transitions (id, workflow_id, from_status, to_status)
		 VALUES (?, ?, ?, ?)`,
		t.ID.String(), t.WorkflowID.String(), t.FromStatus, t.ToStatus,
	)
	return err
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/sqlite/ -v -run "TestWorkflow"`

Expected: All 5 tests PASS.

- [ ] **Step 5: Run the full test suite**

Run: `cd /Users/germanamz/projects/tusk && go test ./... -v`

Expected: ALL tests across all packages PASS (domain, repository, sqlite).

- [ ] **Step 6: Commit**

```bash
git add internal/sqlite/workflow.go internal/sqlite/workflow_test.go internal/sqlite/store_test.go
git commit -m "feat(sqlite): implement WorkflowRepo with JSON statuses"
```
