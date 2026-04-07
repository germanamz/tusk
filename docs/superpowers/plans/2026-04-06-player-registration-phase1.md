# Phase 1: Player Domain, Repository & SQLite Storage

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add the Player entity to the domain layer, define the repository interface, implement SQLite storage, and extend the Task entity with claim fields — all compilation-safe and fully tested.

**Architecture:** New `domain.Player` struct, `repository.PlayerRepository` interface, `sqlite.PlayerRepo` implementation, migration 002 adding `players` table and `claimed_by`/`claimed_at` columns to `tasks`. Task domain struct and SQLite scan/column logic extended for claim fields.

**Tech Stack:** Go, SQLite, github.com/google/uuid

**Prerequisites:** None — this phase builds on the base codebase only.

**Design Spec:** `docs/superpowers/specs/2026-04-06-player-entity-registration-design.md`

---

### Task 1: Player domain type and error sentinel

**Files:**
- Create: `internal/domain/player.go`
- Modify: `internal/domain/errors.go`

- [ ] **Step 1: Create `internal/domain/player.go`**

```go
package domain

import "time"

// Player represents a human or agent that interacts with tusk.
// Players self-register on first contact. The ID is a self-declared
// unique string (not a UUID). Type is immutable after creation.
type Player struct {
	ID           string
	Type         string // "human" or "agent"
	RegisteredAt time.Time
	LastSeenAt   time.Time
}
```

- [ ] **Step 2: Add `ErrTaskClaimed` to `internal/domain/errors.go`**

Add to the `var` block at `internal/domain/errors.go:8-18`:

```go
ErrTaskClaimed = errors.New("task is already claimed by another player")
```

- [ ] **Step 3: Run `go vet ./internal/domain/...`**

Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add internal/domain/player.go internal/domain/errors.go
git commit -m "feat(domain): add Player entity and ErrTaskClaimed sentinel"
```

---

### Task 2: Add claim fields to Task domain struct

**Files:**
- Modify: `internal/domain/task.go:11-28` (Task struct)
- Modify: `internal/domain/task.go:36-49` (TaskUpdate struct)

- [ ] **Step 1: Add `ClaimedBy` and `ClaimedAt` to `Task` struct**

In `internal/domain/task.go`, add two fields after `ModifiedAt` (line 26), before the `Urgency` field:

```go
ClaimedBy  *string    // FK to Player.ID — who holds the claim
ClaimedAt  *time.Time // when the claim was made
```

The `Task` struct should now end with:

```go
	CreatedAt      time.Time
	ModifiedAt     time.Time
	ClaimedBy      *string
	ClaimedAt      *time.Time
	Urgency        float64 // Computed at read time, not persisted in DB.
```

- [ ] **Step 2: Add `ClaimedBy` and `ClaimedAt` to `TaskUpdate` struct**

In `internal/domain/task.go`, add two fields at the end of `TaskUpdate` (after the `UDA` field at line 48):

```go
ClaimedBy **string    // nil = don't change, *nil = clear, *"value" = set
ClaimedAt **time.Time // nil = don't change, *nil = clear, *value = set
```

- [ ] **Step 3: Run `go build ./...`**

Expected: compiles cleanly. The new fields are pointers/double-pointers with zero values, so all existing code continues to work without changes.

- [ ] **Step 4: Commit**

```bash
git add internal/domain/task.go
git commit -m "feat(domain): add ClaimedBy/ClaimedAt fields to Task and TaskUpdate"
```

---

### Task 3: PlayerRepository interface

**Files:**
- Create: `internal/repository/player.go`

- [ ] **Step 1: Create `internal/repository/player.go`**

```go
package repository

import (
	"context"

	"github.com/germanamz/tusk/internal/domain"
)

// PlayerRepository defines storage operations for Player entities.
// Create returns domain.ErrConflict if a player with the same ID already exists.
// GetByID returns domain.ErrNotFound if no player matches.
type PlayerRepository interface {
	Create(ctx context.Context, player *domain.Player) error
	GetByID(ctx context.Context, id string) (*domain.Player, error)
	UpdateLastSeen(ctx context.Context, id string) error
	List(ctx context.Context) ([]*domain.Player, error)
}
```

- [ ] **Step 2: Run `go build ./internal/repository/...`**

Expected: compiles cleanly.

- [ ] **Step 3: Commit**

```bash
git add internal/repository/player.go
git commit -m "feat(repository): add PlayerRepository interface"
```

---

### Task 4: SQLite migration 002 — players table and task claim columns

**Files:**
- Create: `migrations/002_players.up.sql`
- Create: `migrations/002_players.down.sql`

- [ ] **Step 1: Create `migrations/002_players.up.sql`**

```sql
CREATE TABLE players (
    id TEXT PRIMARY KEY,
    type TEXT NOT NULL CHECK (type IN ('human', 'agent')),
    registered_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    last_seen_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

ALTER TABLE tasks ADD COLUMN claimed_by TEXT REFERENCES players(id);
ALTER TABLE tasks ADD COLUMN claimed_at TEXT;

CREATE INDEX idx_tasks_claimed_by ON tasks(claimed_by);
```

- [ ] **Step 2: Create `migrations/002_players.down.sql`**

SQLite doesn't support `ALTER TABLE DROP COLUMN` before 3.35.0, and the modernc.org driver used by tusk targets broader compatibility. Use the rename-recreate pattern:

```sql
-- Recreate tasks without claimed_by/claimed_at
CREATE TABLE tasks_backup AS SELECT
    id, short_id, parent_id, project_id, title, description,
    status, priority, version, due_at, wait_until, recurrence_rule, uda,
    created_at, modified_at
FROM tasks;

DROP TABLE tasks;

CREATE TABLE tasks (
    id TEXT PRIMARY KEY,
    short_id TEXT NOT NULL UNIQUE,
    parent_id TEXT REFERENCES tasks(id) ON DELETE SET NULL,
    project_id TEXT NOT NULL DEFAULT 'default',
    title TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'pending',
    priority INTEGER NOT NULL DEFAULT 0,
    version INTEGER NOT NULL DEFAULT 1,
    due_at TEXT,
    wait_until TEXT,
    recurrence_rule TEXT,
    uda TEXT DEFAULT '{}',
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    modified_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

INSERT INTO tasks SELECT * FROM tasks_backup;
DROP TABLE tasks_backup;

CREATE INDEX idx_tasks_short_id ON tasks(short_id);
CREATE INDEX idx_tasks_parent_id ON tasks(parent_id);
CREATE INDEX idx_tasks_project_id ON tasks(project_id);
CREATE INDEX idx_tasks_status ON tasks(status);
CREATE INDEX idx_tasks_due_at ON tasks(due_at);
CREATE INDEX idx_tasks_wait_until ON tasks(wait_until);

DROP TABLE players;
```

- [ ] **Step 3: Verify migration embeds compile**

```bash
go build ./migrations/...
```

Expected: compiles (the `//go:embed *.sql` in `migrations/migrations.go` will pick up the new files automatically).

- [ ] **Step 4: Commit**

```bash
git add migrations/002_players.up.sql migrations/002_players.down.sql
git commit -m "feat(sqlite): add migration 002 for players table and task claim columns"
```

---

### Task 5: SQLite PlayerRepo implementation

**Files:**
- Create: `internal/sqlite/player.go`
- Create: `internal/sqlite/player_test.go`

- [ ] **Step 1: Create `internal/sqlite/player.go`**

```go
package sqlite

import (
	"context"
	"database/sql"
	"time"

	"github.com/germanamz/tusk/internal/domain"
)

const playerColumns = `id, type, registered_at, last_seen_at`

// PlayerRepo implements repository.PlayerRepository using SQLite.
type PlayerRepo struct {
	db DBTX
}

// NewPlayerRepo creates a PlayerRepo.
func NewPlayerRepo(db DBTX) *PlayerRepo {
	return &PlayerRepo{db: db}
}

// Create inserts a new player. Returns domain.ErrConflict if the ID already exists.
func (r *PlayerRepo) Create(ctx context.Context, player *domain.Player) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO players (id, type, registered_at, last_seen_at) VALUES (?, ?, ?, ?)`,
		player.ID, player.Type,
		player.RegisteredAt.UTC().Format(timeFormat),
		player.LastSeenAt.UTC().Format(timeFormat),
	)
	if err != nil {
		// SQLite returns UNIQUE constraint error for duplicate PK.
		// Check if it's a conflict by attempting to look up the ID.
		if _, lookupErr := r.GetByID(ctx, player.ID); lookupErr == nil {
			return domain.ErrConflict
		}
		return err
	}
	return nil
}

// GetByID retrieves a player by ID. Returns domain.ErrNotFound if missing.
func (r *PlayerRepo) GetByID(ctx context.Context, id string) (*domain.Player, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT `+playerColumns+` FROM players WHERE id = ?`, id)
	return scanPlayer(row)
}

// UpdateLastSeen updates the last_seen_at timestamp for a player.
// Returns domain.ErrNotFound if the player does not exist.
func (r *PlayerRepo) UpdateLastSeen(ctx context.Context, id string) error {
	now := time.Now().UTC().Truncate(time.Millisecond).Format(timeFormat)
	res, err := r.db.ExecContext(ctx,
		`UPDATE players SET last_seen_at = ? WHERE id = ?`, now, id)
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

// List returns all registered players ordered by registration time.
func (r *PlayerRepo) List(ctx context.Context) ([]*domain.Player, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+playerColumns+` FROM players ORDER BY registered_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]*domain.Player, 0)
	for rows.Next() {
		p, err := scanPlayer(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, p)
	}
	return result, rows.Err()
}

// playerScanner abstracts *sql.Row and *sql.Rows for scanPlayer.
type playerScanner interface {
	Scan(dest ...any) error
}

func scanPlayer(s playerScanner) (*domain.Player, error) {
	var (
		p            domain.Player
		registeredAt string
		lastSeenAt   string
	)
	err := s.Scan(&p.ID, &p.Type, &registeredAt, &lastSeenAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	p.RegisteredAt, err = time.Parse(timeFormat, registeredAt)
	if err != nil {
		return nil, err
	}
	p.LastSeenAt, err = time.Parse(timeFormat, lastSeenAt)
	if err != nil {
		return nil, err
	}
	return &p, nil
}
```

- [ ] **Step 2: Write tests in `internal/sqlite/player_test.go`**

```go
package sqlite_test

import (
	"context"
	"testing"
	"time"

	"github.com/germanamz/tusk/internal/domain"
	"github.com/germanamz/tusk/internal/sqlite"
	"github.com/germanamz/tusk/migrations"
)

func newTestPlayerRepo(t *testing.T) *sqlite.PlayerRepo {
	t.Helper()
	store, err := sqlite.New(t.TempDir()+"/test.db", migrations.FS)
	if err != nil {
		t.Fatalf("opening test db: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return sqlite.NewPlayerRepo(store.DB())
}

func TestPlayerRepo_CreateAndGetByID(t *testing.T) {
	repo := newTestPlayerRepo(t)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Millisecond)
	player := &domain.Player{
		ID:           "agent-1",
		Type:         "agent",
		RegisteredAt: now,
		LastSeenAt:   now,
	}
	if err := repo.Create(ctx, player); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.GetByID(ctx, "agent-1")
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.ID != "agent-1" {
		t.Errorf("got ID %q, want %q", got.ID, "agent-1")
	}
	if got.Type != "agent" {
		t.Errorf("got Type %q, want %q", got.Type, "agent")
	}
}

func TestPlayerRepo_CreateDuplicate(t *testing.T) {
	repo := newTestPlayerRepo(t)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Millisecond)
	player := &domain.Player{ID: "dup-1", Type: "human", RegisteredAt: now, LastSeenAt: now}
	if err := repo.Create(ctx, player); err != nil {
		t.Fatalf("first Create: %v", err)
	}
	err := repo.Create(ctx, player)
	if err != domain.ErrConflict {
		t.Fatalf("second Create: got %v, want ErrConflict", err)
	}
}

func TestPlayerRepo_GetByID_NotFound(t *testing.T) {
	repo := newTestPlayerRepo(t)
	ctx := context.Background()

	_, err := repo.GetByID(ctx, "nonexistent")
	if err != domain.ErrNotFound {
		t.Fatalf("GetByID: got %v, want ErrNotFound", err)
	}
}

func TestPlayerRepo_UpdateLastSeen(t *testing.T) {
	repo := newTestPlayerRepo(t)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Millisecond)
	player := &domain.Player{ID: "agent-2", Type: "agent", RegisteredAt: now, LastSeenAt: now}
	if err := repo.Create(ctx, player); err != nil {
		t.Fatalf("Create: %v", err)
	}

	time.Sleep(10 * time.Millisecond)
	if err := repo.UpdateLastSeen(ctx, "agent-2"); err != nil {
		t.Fatalf("UpdateLastSeen: %v", err)
	}

	got, _ := repo.GetByID(ctx, "agent-2")
	if !got.LastSeenAt.After(now) {
		t.Errorf("LastSeenAt not updated: got %v, registered %v", got.LastSeenAt, now)
	}
}

func TestPlayerRepo_UpdateLastSeen_NotFound(t *testing.T) {
	repo := newTestPlayerRepo(t)
	ctx := context.Background()

	err := repo.UpdateLastSeen(ctx, "ghost")
	if err != domain.ErrNotFound {
		t.Fatalf("UpdateLastSeen: got %v, want ErrNotFound", err)
	}
}

func TestPlayerRepo_List(t *testing.T) {
	repo := newTestPlayerRepo(t)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Millisecond)
	for _, id := range []string{"a", "b", "c"} {
		p := &domain.Player{ID: id, Type: "agent", RegisteredAt: now, LastSeenAt: now}
		if err := repo.Create(ctx, p); err != nil {
			t.Fatalf("Create %s: %v", id, err)
		}
	}

	players, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(players) != 3 {
		t.Fatalf("List: got %d players, want 3", len(players))
	}
}
```

- [ ] **Step 3: Run tests**

```bash
go test -v ./internal/sqlite/ -run TestPlayerRepo
```

Expected: all 5 tests pass.

- [ ] **Step 4: Commit**

```bash
git add internal/sqlite/player.go internal/sqlite/player_test.go
git commit -m "feat(sqlite): implement PlayerRepo with tests"
```

---

### Task 6: Extend SQLite TaskRepo for claim columns

**Files:**
- Modify: `internal/sqlite/task.go:17-19` (taskColumns constant)
- Modify: `internal/sqlite/task.go:60-94` (Update method)
- Modify: `internal/sqlite/task.go:362-416` (scanTask function)

- [ ] **Step 1: Update `taskColumns` constant**

In `internal/sqlite/task.go`, change the `taskColumns` constant (lines 17-19) from:

```go
const taskColumns = `id, short_id, parent_id, project_id, title, description,
	status, priority, version, due_at, wait_until, recurrence_rule, uda,
	created_at, modified_at`
```

to:

```go
const taskColumns = `id, short_id, parent_id, project_id, title, description,
	status, priority, version, due_at, wait_until, recurrence_rule, uda,
	created_at, modified_at, claimed_by, claimed_at`
```

- [ ] **Step 2: Update `scanTask` function**

In `internal/sqlite/task.go`, modify the `scanTask` function (starting at line 362). Add `claimedBy` and `claimedAt` variables and scan them:

```go
func scanTask(s taskScanner) (*domain.Task, error) {
	var (
		t          domain.Task
		id         string
		parentID   sql.NullString
		projectID  string
		dueAt      sql.NullString
		waitUntil  sql.NullString
		recurrence sql.NullString
		udaJSON    string
		createdAt  string
		modifiedAt string
		claimedBy  sql.NullString
		claimedAt  sql.NullString
	)
	err := s.Scan(
		&id, &t.ShortID, &parentID, &projectID,
		&t.Title, &t.Description, &t.Status, &t.Priority, &t.Version,
		&dueAt, &waitUntil, &recurrence, &udaJSON,
		&createdAt, &modifiedAt, &claimedBy, &claimedAt,
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
	t.ProjectID = projectID
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
	if claimedBy.Valid {
		t.ClaimedBy = &claimedBy.String
	}
	t.ClaimedAt, err = parseTime(claimedAt)
	if err != nil {
		return nil, fmt.Errorf("parsing claimed_at: %w", err)
	}
	return &t, nil
}
```

- [ ] **Step 3: Update `Update` method to include claim columns**

In `internal/sqlite/task.go`, the `Update` method (starting at line 60) writes all task fields. Add `claimed_by` and `claimed_at` to the UPDATE statement. Change:

```go
res, err := r.db.ExecContext(ctx, `
    UPDATE tasks SET
        parent_id = ?, project_id = ?, title = ?, description = ?,
        status = ?, priority = ?, due_at = ?, wait_until = ?,
        recurrence_rule = ?, uda = ?, version = version + 1, modified_at = ?
    WHERE id = ? AND version = ?`,
    nullableUUID(task.ParentID), task.ProjectID,
    task.Title, task.Description, task.Status, task.Priority,
    nullableTime(task.DueAt), nullableTime(task.WaitUntil),
    nullableString(task.RecurrenceRule), udaJSON,
    nowStr, task.ID.String(), task.Version,
)
```

to:

```go
res, err := r.db.ExecContext(ctx, `
    UPDATE tasks SET
        parent_id = ?, project_id = ?, title = ?, description = ?,
        status = ?, priority = ?, due_at = ?, wait_until = ?,
        recurrence_rule = ?, uda = ?, version = version + 1, modified_at = ?,
        claimed_by = ?, claimed_at = ?
    WHERE id = ? AND version = ?`,
    nullableUUID(task.ParentID), task.ProjectID,
    task.Title, task.Description, task.Status, task.Priority,
    nullableTime(task.DueAt), nullableTime(task.WaitUntil),
    nullableString(task.RecurrenceRule), udaJSON,
    nowStr, nullableString(task.ClaimedBy), nullableTime(task.ClaimedAt),
    task.ID.String(), task.Version,
)
```

- [ ] **Step 4: Also update the `Create` method to include claim columns**

In the `Create` method, add `claimed_by` and `claimed_at` to the INSERT. Find the existing INSERT statement and add the two columns. The INSERT column list and VALUES should be extended:

```go
_, err = r.db.ExecContext(ctx,
    fmt.Sprintf(`INSERT INTO tasks (%s) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
        taskColumns),
    task.ID.String(), task.ShortID,
    nullableUUID(task.ParentID), task.ProjectID,
    task.Title, task.Description, task.Status, task.Priority, task.Version,
    nullableTime(task.DueAt), nullableTime(task.WaitUntil),
    nullableString(task.RecurrenceRule), udaJSON,
    task.CreatedAt.UTC().Format(timeFormat),
    task.ModifiedAt.UTC().Format(timeFormat),
    nullableString(task.ClaimedBy), nullableTime(task.ClaimedAt),
)
```

Note: the number of `?` placeholders must match the number of columns in `taskColumns` (now 17 instead of 15).

- [ ] **Step 5: Run all existing tests to verify nothing is broken**

```bash
go test ./internal/sqlite/... ./internal/service/...
```

Expected: all tests pass. The new columns are nullable, so existing tasks scan correctly with NULL values.

- [ ] **Step 6: Commit**

```bash
git add internal/sqlite/task.go
git commit -m "feat(sqlite): extend TaskRepo for claimed_by/claimed_at columns"
```

---

## Changes Introduced

**New files:**
- `internal/domain/player.go` — Player entity struct
- `internal/repository/player.go` — PlayerRepository interface
- `internal/sqlite/player.go` — SQLite PlayerRepo implementation
- `internal/sqlite/player_test.go` — PlayerRepo unit tests
- `migrations/002_players.up.sql` — players table + task claim columns
- `migrations/002_players.down.sql` — rollback migration

**Modified files:**
- `internal/domain/errors.go` — added `ErrTaskClaimed`
- `internal/domain/task.go` — added `ClaimedBy`/`ClaimedAt` to Task and TaskUpdate structs
- `internal/sqlite/task.go` — extended `taskColumns`, `scanTask`, `Create`, and `Update` for claim columns

**New dependencies:** None.

**Schema migrations:** Migration 002 adds `players` table and `claimed_by`/`claimed_at` columns to `tasks` table.

**Bridge code:** None — all changes are additive (new nullable fields, new table). The service layer does not yet use claim fields; that comes in Phase 2.

**User-visible behavior preserved:**
- All existing CLI commands work identically (claim fields default to NULL)
- All existing MCP tools work identically
- All existing E2E tests pass
- Database auto-migrates on startup
