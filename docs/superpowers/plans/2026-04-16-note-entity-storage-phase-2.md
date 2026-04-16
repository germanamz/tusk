# Note Entity & Storage — Phase 2: Archive, List, Wiring, and Verification

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Test the Archive and List methods, wire `NoteRepo` into the DI infrastructure (`RepoBundle`, `Tx`, `Client`), and verify the full test suite passes.

**Architecture:** Phase 1 implemented all four `NoteRepository` methods in `sqlite/note.go` but only tested Create and GetByID. This phase adds comprehensive tests for Archive and List (window limit, since filter, archived exclusion, task filtering), then wires the repo into the existing DI graph so it's accessible to services and the programmatic client.

**Tech Stack:** Go, SQLite (via `modernc.org/sqlite`), `github.com/google/uuid`

**Prerequisites:** Phase 1 must be completed. Phase 1 introduced `domain/note.go`, `repository/note.go`, `migrations/007_notes.{up,down}.sql`, `sqlite/note.go`, and `sqlite/note_test.go`.

---

## Inherits From Phase 1

Phase 1 introduced:
- `domain.Note` — entity with ID, ProjectID, PlayerID, TaskID (nullable), Body, Metadata (JSON), ArchivedAt (nullable), CreatedAt
- `repository.NoteRepository` — interface with Create, GetByID, Archive, List
- `repository.NoteListOptions` — struct with ProjectID, PlayerID, TaskID, Since, IncludeArchived, Limit
- `migrations/007_notes.up.sql` — notes table with composite index `(project_id, player_id, created_at DESC)` and partial index on `task_id`
- `sqlite.NoteRepo` — full implementation of all 4 methods, with `scanNote` helper
- Tests for Create + GetByID round-trip, GetByID not found, and project-level note (nil TaskID)
- `mustCreatePlayer` test helper in `sqlite/note_test.go`

**Known state:** All tests pass. `sqlite/store_test.go` was updated in Phase 1 to expect 7 migrations. `NoteRepo` is not yet wired into `RepoBundle`, `Tx`, or `Client`.

---

## File Structure

| Action | Path | Responsibility |
|--------|------|----------------|
| Modify | `sqlite/note_test.go` | Add Archive and List tests |
| Modify | `service/repos.go:18-25` | Add Notes field to RepoBundle |
| Modify | `sqlite/store.go:86-88` | Add Notes() method to Tx |
| Modify | `client.go:87-94` | Wire NoteRepo into RepoBundle |

---

### Task 1: Archive Tests

**Files:**
- Modify: `sqlite/note_test.go`

- [ ] **Step 1: Write test for Archive**

Add to `sqlite/note_test.go`:

```go
func TestNoteArchive(t *testing.T) {
	s := testStore(t)
	repo := NewNoteRepo(s.DB())
	ctx := context.Background()

	mustCreatePlayer(t, s, "german")

	note := &domain.Note{
		ID:        uuid.New(),
		ProjectID: domain.DefaultProjectUUID,
		PlayerID:  "german",
		Body:      "Will be archived",
		Metadata:  map[string]any{},
		CreatedAt: time.Now().UTC().Truncate(time.Millisecond),
	}
	if err := repo.Create(ctx, note); err != nil {
		t.Fatal(err)
	}

	archivedAt := time.Now().UTC().Truncate(time.Millisecond)
	if err := repo.Archive(ctx, note.ID, archivedAt); err != nil {
		t.Fatalf("Archive: %v", err)
	}

	got, err := repo.GetByID(ctx, note.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ArchivedAt == nil {
		t.Fatal("expected archived_at to be set")
	}
	if !got.ArchivedAt.Equal(archivedAt) {
		t.Fatalf("archived_at: got %v, want %v", got.ArchivedAt, archivedAt)
	}
}
```

- [ ] **Step 2: Run test to verify it passes**

Run: `go test -v ./sqlite -run TestNoteArchive$`
Expected: PASS — Archive was already implemented in Phase 1's `sqlite/note.go`

- [ ] **Step 3: Write test for Archive not found**

Add to `sqlite/note_test.go`:

```go
func TestNoteArchiveNotFound(t *testing.T) {
	s := testStore(t)
	repo := NewNoteRepo(s.DB())

	err := repo.Archive(context.Background(), uuid.New(), time.Now().UTC())
	if err != domain.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -v ./sqlite -run TestNoteArchiveNotFound`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add sqlite/note_test.go
git commit -m "test(sqlite): add NoteRepo.Archive tests"
```

---

### Task 2: List Tests — Basic, Window Limit, and Since Filter

**Files:**
- Modify: `sqlite/note_test.go`

- [ ] **Step 1: Add `"fmt"` to the imports in `sqlite/note_test.go`**

The existing import block needs `"fmt"` for `fmt.Sprintf` used in the test loops below. Add it to the imports alongside the existing `"context"`, `"testing"`, `"time"` imports.

- [ ] **Step 2: Write test for List basic case**

Add to `sqlite/note_test.go`:

```go
func TestNoteList(t *testing.T) {
	s := testStore(t)
	taskRepo := NewTaskRepo(s.DB())
	repo := NewNoteRepo(s.DB())
	ctx := context.Background()

	mustCreatePlayer(t, s, "german")
	task := newTestTask()
	mustCreateTask(t, taskRepo, task)

	// Create 3 notes with increasing timestamps.
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i, body := range []string{"First", "Second", "Third"} {
		taskID := task.ID
		note := &domain.Note{
			ID:        uuid.New(),
			ProjectID: domain.DefaultProjectUUID,
			PlayerID:  "german",
			TaskID:    &taskID,
			Body:      body,
			Metadata:  map[string]any{},
			CreatedAt: base.Add(time.Duration(i) * time.Hour),
		}
		if err := repo.Create(ctx, note); err != nil {
			t.Fatal(err)
		}
	}

	// List all notes for this player+project, no limit.
	notes, err := repo.List(ctx, repository.NoteListOptions{
		ProjectID: domain.DefaultProjectUUID,
		PlayerID:  "german",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 3 {
		t.Fatalf("expected 3 notes, got %d", len(notes))
	}
	// Should be in descending created_at order (newest first).
	if notes[0].Body != "Third" {
		t.Fatalf("expected newest first, got %q", notes[0].Body)
	}
}
```

- [ ] **Step 3: Run test to verify it passes**

Run: `go test -v ./sqlite -run TestNoteList$`
Expected: PASS

- [ ] **Step 4: Write test for List with window limit**

Add to `sqlite/note_test.go`:

```go
func TestNoteListWindowLimit(t *testing.T) {
	s := testStore(t)
	repo := NewNoteRepo(s.DB())
	ctx := context.Background()

	mustCreatePlayer(t, s, "german")

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		note := &domain.Note{
			ID:        uuid.New(),
			ProjectID: domain.DefaultProjectUUID,
			PlayerID:  "german",
			Body:      fmt.Sprintf("Note %d", i),
			Metadata:  map[string]any{},
			CreatedAt: base.Add(time.Duration(i) * time.Hour),
		}
		if err := repo.Create(ctx, note); err != nil {
			t.Fatal(err)
		}
	}

	// Window of 2 should return only the 2 newest.
	notes, err := repo.List(ctx, repository.NoteListOptions{
		ProjectID: domain.DefaultProjectUUID,
		PlayerID:  "german",
		Limit:     2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 2 {
		t.Fatalf("expected 2 notes, got %d", len(notes))
	}
	if notes[0].Body != "Note 4" {
		t.Fatalf("expected newest first, got %q", notes[0].Body)
	}
	if notes[1].Body != "Note 3" {
		t.Fatalf("expected second newest, got %q", notes[1].Body)
	}
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test -v ./sqlite -run TestNoteListWindowLimit`
Expected: PASS

- [ ] **Step 6: Write test for List with --since filter**

Add to `sqlite/note_test.go`:

```go
func TestNoteListSince(t *testing.T) {
	s := testStore(t)
	repo := NewNoteRepo(s.DB())
	ctx := context.Background()

	mustCreatePlayer(t, s, "german")

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		note := &domain.Note{
			ID:        uuid.New(),
			ProjectID: domain.DefaultProjectUUID,
			PlayerID:  "german",
			Body:      fmt.Sprintf("Note %d", i),
			Metadata:  map[string]any{},
			CreatedAt: base.Add(time.Duration(i) * 24 * time.Hour),
		}
		if err := repo.Create(ctx, note); err != nil {
			t.Fatal(err)
		}
	}

	// Since the second day — should return notes 1 and 2.
	since := base.Add(24 * time.Hour)
	notes, err := repo.List(ctx, repository.NoteListOptions{
		ProjectID: domain.DefaultProjectUUID,
		PlayerID:  "german",
		Since:     &since,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 2 {
		t.Fatalf("expected 2 notes since day 2, got %d", len(notes))
	}
}
```

- [ ] **Step 7: Run test to verify it passes**

Run: `go test -v ./sqlite -run TestNoteListSince`
Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add sqlite/note_test.go
git commit -m "test(sqlite): add NoteRepo.List tests for basic, window limit, and since filter"
```

---

### Task 3: List Tests — Archived Exclusion and Task Filtering

**Files:**
- Modify: `sqlite/note_test.go`

- [ ] **Step 1: Write test for List excluding archived notes**

Add to `sqlite/note_test.go`:

```go
func TestNoteListExcludesArchived(t *testing.T) {
	s := testStore(t)
	repo := NewNoteRepo(s.DB())
	ctx := context.Background()

	mustCreatePlayer(t, s, "german")

	now := time.Now().UTC().Truncate(time.Millisecond)
	active := &domain.Note{
		ID:        uuid.New(),
		ProjectID: domain.DefaultProjectUUID,
		PlayerID:  "german",
		Body:      "Active note",
		Metadata:  map[string]any{},
		CreatedAt: now,
	}
	archived := &domain.Note{
		ID:        uuid.New(),
		ProjectID: domain.DefaultProjectUUID,
		PlayerID:  "german",
		Body:      "Archived note",
		Metadata:  map[string]any{},
		CreatedAt: now.Add(-time.Hour),
	}
	if err := repo.Create(ctx, active); err != nil {
		t.Fatal(err)
	}
	if err := repo.Create(ctx, archived); err != nil {
		t.Fatal(err)
	}
	if err := repo.Archive(ctx, archived.ID, now); err != nil {
		t.Fatal(err)
	}

	// Default: exclude archived.
	notes, err := repo.List(ctx, repository.NoteListOptions{
		ProjectID: domain.DefaultProjectUUID,
		PlayerID:  "german",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 1 {
		t.Fatalf("expected 1 active note, got %d", len(notes))
	}

	// Include archived.
	notes, err = repo.List(ctx, repository.NoteListOptions{
		ProjectID:       domain.DefaultProjectUUID,
		PlayerID:        "german",
		IncludeArchived: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 2 {
		t.Fatalf("expected 2 notes with archived, got %d", len(notes))
	}
}
```

- [ ] **Step 2: Run test to verify it passes**

Run: `go test -v ./sqlite -run TestNoteListExcludesArchived`
Expected: PASS

- [ ] **Step 3: Write test for List filtering by task_id**

Add to `sqlite/note_test.go`:

```go
func TestNoteListByTask(t *testing.T) {
	s := testStore(t)
	taskRepo := NewTaskRepo(s.DB())
	repo := NewNoteRepo(s.DB())
	ctx := context.Background()

	mustCreatePlayer(t, s, "german")
	task := newTestTask()
	mustCreateTask(t, taskRepo, task)

	now := time.Now().UTC().Truncate(time.Millisecond)
	taskID := task.ID

	// One task-scoped note, one project-level note.
	taskNote := &domain.Note{
		ID:        uuid.New(),
		ProjectID: domain.DefaultProjectUUID,
		PlayerID:  "german",
		TaskID:    &taskID,
		Body:      "Task-scoped",
		Metadata:  map[string]any{},
		CreatedAt: now,
	}
	projectNote := &domain.Note{
		ID:        uuid.New(),
		ProjectID: domain.DefaultProjectUUID,
		PlayerID:  "german",
		TaskID:    nil,
		Body:      "Project-level",
		Metadata:  map[string]any{},
		CreatedAt: now.Add(-time.Hour),
	}
	if err := repo.Create(ctx, taskNote); err != nil {
		t.Fatal(err)
	}
	if err := repo.Create(ctx, projectNote); err != nil {
		t.Fatal(err)
	}

	// Filter by task — should return only the task-scoped note.
	notes, err := repo.List(ctx, repository.NoteListOptions{
		ProjectID: domain.DefaultProjectUUID,
		PlayerID:  "german",
		TaskID:    &taskID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 1 {
		t.Fatalf("expected 1 task-scoped note, got %d", len(notes))
	}
	if notes[0].Body != "Task-scoped" {
		t.Fatalf("expected task-scoped note, got %q", notes[0].Body)
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -v ./sqlite -run TestNoteListByTask`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add sqlite/note_test.go
git commit -m "test(sqlite): add NoteRepo.List tests for archived exclusion and task filtering"
```

---

### Task 4: Wire NoteRepo into RepoBundle, Tx, Client, and Verify

**Files:**
- Modify: `service/repos.go:18-25`
- Modify: `sqlite/store.go:86-88`
- Modify: `client.go:87-94`

- [ ] **Step 1: Add Notes to RepoBundle**

In `service/repos.go`, the `RepoBundle` struct currently looks like:

```go
type RepoBundle struct {
	Store       *sqlite.Store
	Tasks       repository.TaskRepository
	Annotations repository.AnnotationRepository
	Relations   repository.RelationRepository
	Tags        repository.TagRepository
	Players     repository.PlayerRepository
}
```

Add `Notes` after `Annotations`:

```go
type RepoBundle struct {
	Store       *sqlite.Store
	Tasks       repository.TaskRepository
	Annotations repository.AnnotationRepository
	Notes       repository.NoteRepository
	Relations   repository.RelationRepository
	Tags        repository.TagRepository
	Players     repository.PlayerRepository
}
```

- [ ] **Step 2: Add Notes() method to Tx**

In `sqlite/store.go`, add after the existing `Annotations()` method (line 88):

```go
// Notes returns a NoteRepo operating within this transaction.
func (t *Tx) Notes() *NoteRepo { return NewNoteRepo(t.tx) }
```

- [ ] **Step 3: Wire NoteRepo in client.go**

In `client.go`, the bundle construction (around line 87-94) currently reads:

```go
bundle := &service.RepoBundle{
	Store:       store,
	Tasks:       sqlite.NewTaskRepo(db),
	Annotations: sqlite.NewAnnotationRepo(db),
	Relations:   sqlite.NewRelationRepo(db),
	Tags:        sqlite.NewTagRepo(db),
	Players:     sqlite.NewPlayerRepo(db),
}
```

Add `Notes` after `Annotations`:

```go
bundle := &service.RepoBundle{
	Store:       store,
	Tasks:       sqlite.NewTaskRepo(db),
	Annotations: sqlite.NewAnnotationRepo(db),
	Notes:       sqlite.NewNoteRepo(db),
	Relations:   sqlite.NewRelationRepo(db),
	Tags:        sqlite.NewTagRepo(db),
	Players:     sqlite.NewPlayerRepo(db),
}
```

- [ ] **Step 4: Verify everything compiles**

Run: `go build ./...`
Expected: exit 0, no errors

- [ ] **Step 5: Run the full test suite**

Run: `make test`
Expected: all tests PASS

- [ ] **Step 6: Run tests with race detector**

Run: `make test-race`
Expected: all tests PASS, no race conditions

- [ ] **Step 7: Run vet and lint**

Run: `make vet && make lint`
Expected: no issues

- [ ] **Step 8: Commit**

```bash
git add service/repos.go sqlite/store.go client.go
git commit -m "feat: wire NoteRepo into RepoBundle, Tx, and Client"
```

---

## Changes Introduced

| Category | Detail |
|----------|--------|
| **New files** | None |
| **Modified files** | `sqlite/note_test.go`, `service/repos.go`, `sqlite/store.go`, `client.go` |
| **Modified interfaces** | `service.RepoBundle` gains `Notes repository.NoteRepository` field |
| **New methods** | `sqlite.Tx.Notes() *NoteRepo` |
| **Schema migrations** | None (migration 007 was added in Phase 1) |
| **Bridge code** | None introduced, none removed |
| **Dependencies** | None new |
| **User-visible behavior preserved** | All existing CLI commands, MCP tools, and programmatic client functionality unchanged. No new user-facing surface yet — that comes in the Note Service and Note CLI initiatives. |
