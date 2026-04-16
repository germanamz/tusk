# Note Entity & Storage — Phase 1: Types, Migration, and Core Repo Methods

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Establish the Note domain entity, repository interface, SQLite migration, and the first two repository methods (Create, GetByID) — the type foundation that Phase 2 builds on.

**Architecture:** Notes follow the same layered pattern as Annotations: domain type in `domain/`, repository interface in `repository/`, SQLite implementation in `sqlite/`, migration in `migrations/`. Notes are more complex than annotations — they have project scope, player ownership, optional task binding, JSON metadata, and an archival timestamp. This phase defines all types and proves the round-trip through Create + GetByID.

**Tech Stack:** Go, SQLite (via `modernc.org/sqlite`), `github.com/google/uuid`, standard `database/sql`, `encoding/json`

**Prerequisites:** None — builds on the base codebase (v0.11 main branch).

---

## File Structure

| Action | Path | Responsibility |
|--------|------|----------------|
| Create | `domain/note.go` | Note entity struct |
| Create | `repository/note.go` | NoteRepository interface + NoteListOptions |
| Create | `migrations/007_notes.up.sql` | CREATE TABLE + indexes |
| Create | `migrations/007_notes.down.sql` | DROP TABLE rollback |
| Create | `sqlite/note.go` | NoteRepo struct, Create, GetByID, scanNote |
| Create | `sqlite/note_test.go` | Tests for Create and GetByID |
| Modify | `sqlite/store_test.go:62-96` | Update migration count (6→7) + add "notes" to table list |

---

### Task 1: Note Domain Entity

**Files:**
- Create: `domain/note.go`

- [ ] **Step 1: Create the domain entity**

```go
package domain

import (
	"time"

	"github.com/google/uuid"
)

// Note is a player-scoped entry in the project notebook. Notes are
// append-only: they can be archived but never edited after creation.
type Note struct {
	ID         uuid.UUID
	ProjectID  uuid.UUID
	PlayerID   string         // FK to Player.ID (string, not UUID)
	TaskID     *uuid.UUID     // optional — nil means project-level note
	Body       string
	Metadata   map[string]any // arbitrary key-value pairs (e.g. topic=auth)
	ArchivedAt *time.Time     // nil means active; non-nil means archived
	CreatedAt  time.Time
}
```

The field set matches the ROADMAP spec exactly: `id` UUID, `project_id`, `player_id`, `task_id` nullable, `body`, `metadata` JSON, `archived_at` nullable, `created_at`.

- [ ] **Step 2: Verify it compiles**

Run: `go build ./domain/...`
Expected: exit 0, no errors

- [ ] **Step 3: Commit**

```bash
git add domain/note.go
git commit -m "feat(domain): add Note entity for v0.12 trailing window notes"
```

---

### Task 2: NoteRepository Interface

**Files:**
- Create: `repository/note.go`

- [ ] **Step 1: Define the repository interface**

The ROADMAP specifies four methods: `Create`, `Archive`, `GetByID`, `List`. The `List` method takes a `NoteListOptions` struct so filtering and `LIMIT` happen in SQL, not post-fetch (ROADMAP: "window-aware `List` query (`LIMIT` in SQL, not post-fetch)"). `NoteListOptions` lives in `repository/` because it describes storage-level query parameters.

```go
package repository

import (
	"context"
	"time"

	"github.com/germanamz/tusk/domain"
	"github.com/google/uuid"
)

// NoteListOptions controls the List query at the storage level.
// All fields are optional — zero values mean "no filter".
type NoteListOptions struct {
	ProjectID       uuid.UUID  // required — scopes to a single project
	PlayerID        string     // empty = all players
	TaskID          *uuid.UUID // nil = all notes (project + task-scoped); non-nil = only that task
	Since           *time.Time // nil = no lower bound; non-nil = created_at >= since
	IncludeArchived bool       // false = active only (archived_at IS NULL)
	Limit           int        // 0 = no limit; positive = SQL LIMIT
}

// NoteRepository defines storage operations for Note entities.
// Create and Archive are the only mutations — notes cannot be edited.
// GetByID returns domain.ErrNotFound if no note matches.
// Archive returns domain.ErrNotFound if no note matches.
type NoteRepository interface {
	Create(ctx context.Context, note *domain.Note) error
	GetByID(ctx context.Context, id uuid.UUID) (*domain.Note, error)
	Archive(ctx context.Context, id uuid.UUID, archivedAt time.Time) error
	List(ctx context.Context, opts NoteListOptions) ([]*domain.Note, error)
}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./repository/...`
Expected: exit 0, no errors

- [ ] **Step 3: Commit**

```bash
git add repository/note.go
git commit -m "feat(repository): add NoteRepository interface with window-aware List"
```

---

### Task 3: SQLite Migration

**Files:**
- Create: `migrations/007_notes.up.sql`
- Create: `migrations/007_notes.down.sql`

- [ ] **Step 1: Create the up migration**

The ROADMAP specifies: "composite index on `(project_id, player_id, created_at DESC)`" and "partial index on `task_id`".

Write `migrations/007_notes.up.sql`:

```sql
CREATE TABLE notes (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
    player_id TEXT NOT NULL REFERENCES players(id) ON DELETE CASCADE,
    task_id TEXT REFERENCES tasks(id) ON DELETE SET NULL,
    body TEXT NOT NULL,
    metadata TEXT NOT NULL DEFAULT '{}',
    archived_at TEXT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX idx_notes_project_player_created
    ON notes(project_id, player_id, created_at DESC);

CREATE INDEX idx_notes_task_id
    ON notes(task_id)
    WHERE task_id IS NOT NULL;
```

FK design decisions:
- `project_id ON DELETE RESTRICT` — deleting a project with notes should be rejected, same pattern as `tasks.project_id` (see `migrations/005_tasks_project_fk.up.sql:6`).
- `player_id ON DELETE CASCADE` — if a player is removed, their notes go with them.
- `task_id ON DELETE SET NULL` — if a task is deleted, the note survives as a project-level note. Notes are player context that outlives individual tasks.
- `metadata TEXT NOT NULL DEFAULT '{}'` — JSON column, matches the UDA pattern on tasks (`migrations/001_initial.up.sql:19`).
- `archived_at TEXT` — nullable timestamp, nil = active.

- [ ] **Step 2: Create the down migration**

Write `migrations/007_notes.down.sql`:

```sql
DROP TABLE IF EXISTS notes;
```

- [ ] **Step 3: Update store_test.go migration count and table list**

In `sqlite/store_test.go`, `TestMigrations` (around line 58-78) asserts:

```go
if count != 6 {
    t.Fatalf("expected 6 migrations applied, got %d", count)
}
```

Change to:

```go
if count != 7 {
    t.Fatalf("expected 7 migrations applied, got %d", count)
}
```

And add `"notes"` to the tables list:

```go
tables := []string{"tasks", "annotations", "relations", "tags", "tag_assignments", "players", "workflows", "projects", "notes"}
```

In `TestMigrationsIdempotent` (around line 80-96), change:

```go
if count != 6 {
    t.Fatalf("expected 6 migrations after idempotent call, got %d", count)
}
```

To:

```go
if count != 7 {
    t.Fatalf("expected 7 migrations after idempotent call, got %d", count)
}
```

- [ ] **Step 4: Verify migration applies and store tests pass**

Run: `go test -v ./sqlite -run "TestNew|TestMigrations"`
Expected: PASS — TestNew, TestMigrations, and TestMigrationsIdempotent all pass with the updated count (7).

- [ ] **Step 5: Commit**

```bash
git add migrations/007_notes.up.sql migrations/007_notes.down.sql sqlite/store_test.go
git commit -m "feat(migrations): add notes table with composite and partial indexes"
```

---

### Task 4: SQLite NoteRepo — Create, GetByID, and Tests

**Files:**
- Create: `sqlite/note.go`
- Create: `sqlite/note_test.go`

- [ ] **Step 1: Write the failing test for Create + GetByID round-trip**

Write `sqlite/note_test.go`:

```go
package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/repository"
	"github.com/google/uuid"
)

// Compile-time check: *NoteRepo must implement repository.NoteRepository.
var _ repository.NoteRepository = (*NoteRepo)(nil)

func mustCreatePlayer(t *testing.T, s *Store, id string) {
	t.Helper()
	repo := NewPlayerRepo(s.DB())
	p := &domain.Player{
		ID:           id,
		Type:         "human",
		RegisteredAt: time.Now().UTC().Truncate(time.Millisecond),
		LastSeenAt:   time.Now().UTC().Truncate(time.Millisecond),
	}
	if err := repo.Create(context.Background(), p); err != nil {
		t.Fatalf("mustCreatePlayer: %v", err)
	}
}

func TestNoteCreateAndGetByID(t *testing.T) {
	s := testStore(t)
	taskRepo := NewTaskRepo(s.DB())
	repo := NewNoteRepo(s.DB())
	ctx := context.Background()

	mustCreatePlayer(t, s, "german")
	task := newTestTask()
	mustCreateTask(t, taskRepo, task)

	now := time.Now().UTC().Truncate(time.Millisecond)
	taskID := task.ID
	note := &domain.Note{
		ID:        uuid.New(),
		ProjectID: domain.DefaultProjectUUID,
		PlayerID:  "german",
		TaskID:    &taskID,
		Body:      "Auth middleware needs retry logic",
		Metadata:  map[string]any{"topic": "auth"},
		CreatedAt: now,
	}

	if err := repo.Create(ctx, note); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.GetByID(ctx, note.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Body != note.Body {
		t.Fatalf("body: got %q, want %q", got.Body, note.Body)
	}
	if got.PlayerID != "german" {
		t.Fatalf("player_id: got %q, want %q", got.PlayerID, "german")
	}
	if got.TaskID == nil || *got.TaskID != taskID {
		t.Fatalf("task_id: got %v, want %v", got.TaskID, taskID)
	}
	if got.ArchivedAt != nil {
		t.Fatalf("archived_at: expected nil, got %v", got.ArchivedAt)
	}
	if got.Metadata["topic"] != "auth" {
		t.Fatalf("metadata: got %v, want topic=auth", got.Metadata)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -v ./sqlite -run TestNoteCreateAndGetByID`
Expected: FAIL — `NoteRepo` type does not exist yet

- [ ] **Step 3: Write NoteRepo struct, constructor, Create, GetByID, and scanNote**

Write `sqlite/note.go`:

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

	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/repository"
	"github.com/google/uuid"
)

// NoteRepo implements repository.NoteRepository using SQLite.
type NoteRepo struct {
	db DBTX
}

// NewNoteRepo creates a NoteRepo.
func NewNoteRepo(db DBTX) *NoteRepo {
	return &NoteRepo{db: db}
}

// Create inserts a new note. The caller must set ID, ProjectID, PlayerID,
// Body, and CreatedAt. TaskID and Metadata are optional.
func (r *NoteRepo) Create(ctx context.Context, note *domain.Note) error {
	meta, err := marshalJSON(note.Metadata)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx,
		`INSERT INTO notes (id, project_id, player_id, task_id, body, metadata, archived_at, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		note.ID.String(),
		note.ProjectID.String(),
		note.PlayerID,
		nullableUUID(note.TaskID),
		note.Body,
		meta,
		nullableTime(note.ArchivedAt),
		note.CreatedAt.UTC().Format(timeFormat),
	)
	return err
}

// GetByID retrieves a single note. Returns domain.ErrNotFound if missing.
func (r *NoteRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.Note, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, project_id, player_id, task_id, body, metadata, archived_at, created_at
		 FROM notes WHERE id = ?`,
		id.String(),
	)
	return scanNote(row)
}

// Archive sets the archived_at timestamp on a note.
// Returns domain.ErrNotFound if no note with that ID exists.
func (r *NoteRepo) Archive(ctx context.Context, id uuid.UUID, archivedAt time.Time) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE notes SET archived_at = ? WHERE id = ?`,
		archivedAt.UTC().Format(timeFormat),
		id.String(),
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

// List retrieves notes matching the given options, ordered by created_at DESC
// (newest first). Applies SQL-level filtering and LIMIT for the trailing window.
func (r *NoteRepo) List(ctx context.Context, opts repository.NoteListOptions) ([]*domain.Note, error) {
	var (
		where []string
		args  []any
	)

	if opts.ProjectID != uuid.Nil {
		where = append(where, "project_id = ?")
		args = append(args, opts.ProjectID.String())
	}

	if opts.PlayerID != "" {
		where = append(where, "player_id = ?")
		args = append(args, opts.PlayerID)
	}

	if opts.TaskID != nil {
		where = append(where, "task_id = ?")
		args = append(args, opts.TaskID.String())
	}

	if opts.Since != nil {
		where = append(where, "created_at >= ?")
		args = append(args, opts.Since.UTC().Format(timeFormat))
	}

	if !opts.IncludeArchived {
		where = append(where, "archived_at IS NULL")
	}

	query := `SELECT id, project_id, player_id, task_id, body, metadata, archived_at, created_at FROM notes`
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += " ORDER BY created_at DESC"

	if opts.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", opts.Limit)
	}

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]*domain.Note, 0)
	for rows.Next() {
		n, err := scanNote(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, n)
	}
	return result, rows.Err()
}

// noteScanner abstracts *sql.Row and *sql.Rows for scanNote.
type noteScanner interface {
	Scan(dest ...any) error
}

func scanNote(s noteScanner) (*domain.Note, error) {
	var (
		n                      domain.Note
		id, projectID          string
		taskID, archivedAt     sql.NullString
		metaStr, createdAt     string
	)
	err := s.Scan(&id, &projectID, &n.PlayerID, &taskID, &n.Body, &metaStr, &archivedAt, &createdAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	n.ID, err = uuid.Parse(id)
	if err != nil {
		return nil, err
	}
	n.ProjectID, err = uuid.Parse(projectID)
	if err != nil {
		return nil, err
	}
	n.TaskID, err = parseUUID(taskID)
	if err != nil {
		return nil, err
	}
	n.ArchivedAt, err = parseTime(archivedAt)
	if err != nil {
		return nil, err
	}
	n.CreatedAt, err = time.Parse(timeFormat, createdAt)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(metaStr), &n.Metadata); err != nil {
		return nil, err
	}
	return &n, nil
}
```

Note: The full implementation (all 4 interface methods) is included in a single file so the compile-time interface check (`var _ repository.NoteRepository = (*NoteRepo)(nil)`) passes. Archive and List are tested in Phase 2; their presence here satisfies the interface contract and avoids bridge code.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -v ./sqlite -run TestNoteCreateAndGetByID`
Expected: PASS

- [ ] **Step 5: Write test for GetByID not found**

Add to `sqlite/note_test.go`:

```go
func TestNoteGetByIDNotFound(t *testing.T) {
	s := testStore(t)
	repo := NewNoteRepo(s.DB())

	_, err := repo.GetByID(context.Background(), uuid.New())
	if err != domain.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
```

- [ ] **Step 6: Run test to verify it passes**

Run: `go test -v ./sqlite -run TestNoteGetByIDNotFound`
Expected: PASS

- [ ] **Step 7: Write test for Create with nil TaskID (project-level note)**

Add to `sqlite/note_test.go`:

```go
func TestNoteCreateProjectLevel(t *testing.T) {
	s := testStore(t)
	repo := NewNoteRepo(s.DB())
	ctx := context.Background()

	mustCreatePlayer(t, s, "agent-1")

	note := &domain.Note{
		ID:        uuid.New(),
		ProjectID: domain.DefaultProjectUUID,
		PlayerID:  "agent-1",
		TaskID:    nil,
		Body:      "Caching strategy won't work for this project",
		Metadata:  map[string]any{},
		CreatedAt: time.Now().UTC().Truncate(time.Millisecond),
	}

	if err := repo.Create(ctx, note); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.GetByID(ctx, note.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.TaskID != nil {
		t.Fatalf("task_id: expected nil, got %v", got.TaskID)
	}
}
```

- [ ] **Step 8: Run test to verify it passes**

Run: `go test -v ./sqlite -run TestNoteCreateProjectLevel`
Expected: PASS

- [ ] **Step 9: Commit**

```bash
git add sqlite/note.go sqlite/note_test.go
git commit -m "feat(sqlite): add NoteRepo with Create, GetByID, Archive, and List"
```

---

## Changes Introduced

| Category | Detail |
|----------|--------|
| **New files** | `domain/note.go`, `repository/note.go`, `migrations/007_notes.up.sql`, `migrations/007_notes.down.sql`, `sqlite/note.go`, `sqlite/note_test.go` |
| **Modified files** | `sqlite/store_test.go` (migration count 6→7, added "notes" to table list) |
| **New types** | `domain.Note`, `repository.NoteListOptions`, `repository.NoteRepository`, `sqlite.NoteRepo` |
| **Schema migrations** | Migration 007 adds `notes` table with composite index `(project_id, player_id, created_at DESC)` and partial index on `task_id WHERE task_id IS NOT NULL` |
| **Bridge code** | None — all 4 interface methods are implemented to satisfy the compile-time check. Archive and List are untested until Phase 2. |
| **Dependencies** | None new — uses existing `encoding/json`, `database/sql`, `github.com/google/uuid` |
| **Known state after this phase** | `NoteRepo` exists but is not wired into `RepoBundle`, `Tx`, or `Client`. All existing tests pass (including updated migration count assertions). |
