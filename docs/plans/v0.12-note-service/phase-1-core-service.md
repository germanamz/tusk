# Phase 1: NoteService Core

**Initiative:** Note Service (v0.12)
**Prerequisites:** None — builds on the base codebase. The Note entity, repository, SQLite implementation, and migration 007 already exist.
**Design spec:** `docs/plans/v0.12-note-service/design.md`

---

## Goal

Deliver a working `NoteService` with Create, Archive, and List methods backed by the existing `NoteRepository`. Wire the service into the library `Client` so programmatic consumers can use it immediately. Fix the missing `Notes` repo in the CLI bundle and test-helper bundle.

List uses a hardcoded default window size of 20 in this phase. Phase 2 replaces the hardcoded default with a multi-level resolution chain (CLI flag → player DB → project settings → config).

---

## Tasks

### Task 1: Add `ErrForbidden` sentinel error

**File:** `domain/errors.go`

Add a new sentinel error for the author-only guard on `Archive`:

```go
var ErrForbidden = errors.New("forbidden")
```

Place it alongside the existing sentinels (`ErrNotFound`, `ErrConflict`, etc.). No other changes to this file.

**Acceptance:** `errors.Is(domain.ErrForbidden, domain.ErrForbidden)` returns true. Compiles.

---

### Task 2: Create `service/note.go` — NoteService

**File:** `service/note.go` (new)

Create the NoteService struct, constructor, and three methods.

#### Struct and constructor

```go
type NoteService struct {
    notes             repository.NoteRepository
    players           repository.PlayerRepository
    projects          repository.ProjectRepository
    tasks             repository.TaskRepository
    defaultWindowSize int
}

func NewNoteService(
    notes    repository.NoteRepository,
    players  repository.PlayerRepository,
    projects repository.ProjectRepository,
    tasks    repository.TaskRepository,
    defaultWindowSize int,
) *NoteService {
    if defaultWindowSize <= 0 {
        defaultWindowSize = 20
    }
    return &NoteService{
        notes:             notes,
        players:           players,
        projects:          projects,
        tasks:             tasks,
        defaultWindowSize: defaultWindowSize,
    }
}
```

#### `Create(ctx context.Context, note *domain.Note) error`

1. Trim `note.Body`. If empty, return `fmt.Errorf("note body must not be empty")`.
2. Validate player exists: `s.players.GetByID(ctx, note.PlayerID)`. On error, return `fmt.Errorf("player %q: %w", note.PlayerID, err)` — this propagates `domain.ErrNotFound` from the repo.
3. Validate project exists: `s.projects.GetByID(ctx, note.ProjectID)`. On error, return `fmt.Errorf("project %v: %w", note.ProjectID, err)`.
4. If `note.TaskID != nil`:
   a. `task, err := s.tasks.GetByID(ctx, *note.TaskID)`. On error, return `fmt.Errorf("task %v: %w", *note.TaskID, err)`.
   b. If `task.ProjectID != note.ProjectID`, return `fmt.Errorf("task %s belongs to project %v, not %v", task.ShortID, task.ProjectID, note.ProjectID)`.
5. Set `note.ID = uuid.New()`.
6. Set `note.CreatedAt = time.Now().UTC().Truncate(time.Millisecond)`.
7. Force `note.ArchivedAt = nil`.
8. If `note.Metadata == nil`, set `note.Metadata = make(map[string]any)`.
9. Return `s.notes.Create(ctx, note)`.

#### `Archive(ctx context.Context, id uuid.UUID, callerPlayerID string) error`

1. `note, err := s.notes.GetByID(ctx, id)`. Propagate error (includes `domain.ErrNotFound`).
2. If `note.PlayerID != callerPlayerID`, return `fmt.Errorf("note %s belongs to player %q, not %q: %w", id, note.PlayerID, callerPlayerID, domain.ErrForbidden)`.
3. Return `s.notes.Archive(ctx, id, time.Now().UTC().Truncate(time.Millisecond))`.

#### `NoteListParams` and `List`

```go
type NoteListParams struct {
    ProjectID       uuid.UUID
    PlayerID        string     // filter; empty = all players' notes
    CallerPlayerID  string     // who's asking; used for window resolution in phase 2
    TaskID          *uuid.UUID
    Since           *time.Time
    IncludeArchived bool
    WindowOverride  *int       // CLI/MCP flag; nil = not set
}

func (s *NoteService) List(ctx context.Context, params NoteListParams) ([]*domain.Note, error) {
    limit := s.defaultWindowSize
    if params.WindowOverride != nil && *params.WindowOverride > 0 {
        limit = *params.WindowOverride
    }

    opts := repository.NoteListOptions{
        ProjectID:       params.ProjectID,
        PlayerID:        params.PlayerID,
        TaskID:          params.TaskID,
        Since:           params.Since,
        IncludeArchived: params.IncludeArchived,
        Limit:           limit,
    }
    return s.notes.List(ctx, opts)
}
```

**Note for phase 2:** The `resolveWindowSize` method will replace the two-line `limit` logic in `List`. `CallerPlayerID` is carried in the params struct now but only used starting in phase 2. This avoids changing the method signature across phases.

**Acceptance:** Compiles. All three methods have the signatures above. `strings.TrimSpace` is used on body check. UUID and time generation use the same patterns as `TaskService.Create` (`uuid.New()`, `time.Now().UTC().Truncate(time.Millisecond)`).

---

### Task 3: Create `service/note_test.go` — unit tests

**File:** `service/note_test.go` (new)

Follow the existing test pattern from `service/player_test.go`: real SQLite via `sqlitetest.NewStore`, test helper to create a fresh environment, `context.Background()` for ctx.

#### Test helper

```go
func newNoteTestEnv(t *testing.T) (*service.NoteService, *sqlite.NoteRepo, *sqlite.ProjectRepo, *sqlite.PlayerRepo) {
    t.Helper()
    store, projRepo, _ := sqlitetest.NewStore(t)
    db := store.DB()
    noteRepo := sqlite.NewNoteRepo(db)
    playerRepo := sqlite.NewPlayerRepo(db)
    taskRepo := sqlite.NewTaskRepo(db)
    svc := service.NewNoteService(noteRepo, playerRepo, projRepo, taskRepo, 20)
    return svc, noteRepo, projRepo, playerRepo
}
```

Seed a player before each test that needs one:
```go
playerRepo.Create(ctx, &domain.Player{ID: "p1", Type: "human", RegisteredAt: now, LastSeenAt: now})
```

The default project (UUID `uuid.Nil`) and kanban workflow are seeded by migrations.

#### Tests

| Test function | Setup | Assert |
|---------------|-------|--------|
| `TestNoteService_Create` | Seed player "p1". Create note with `ProjectID=uuid.Nil`, `PlayerID="p1"`, `Body="hello"`. | No error. `note.ID` non-zero. `note.CreatedAt` non-zero. `note.ArchivedAt` nil. |
| `TestNoteService_Create_WithTask` | Seed player "p1". Create a task in default project via `taskRepo`. Create note with `TaskID=&task.ID`. | No error. |
| `TestNoteService_Create_TaskWrongProject` | Seed player "p1". Seed extra project "other" via `sqlitetest.SeedProject`. Create task in default project. Create note with `ProjectID=other.ID` and `TaskID=&task.ID`. | Error containing "belongs to project". |
| `TestNoteService_Create_EmptyBody` | Seed player "p1". Create note with `Body="  "`. | Error containing "body must not be empty". |
| `TestNoteService_Create_MissingPlayer` | Create note with `PlayerID="ghost"`. | Error wrapping `domain.ErrNotFound`. |
| `TestNoteService_Create_MissingProject` | Seed player "p1". Create note with `ProjectID=uuid.New()`. | Error wrapping `domain.ErrNotFound`. |
| `TestNoteService_Archive` | Seed player "p1". Create a note. Archive it with `callerPlayerID="p1"`. | No error. Re-read note via `noteRepo.GetByID`; `ArchivedAt` non-nil. |
| `TestNoteService_Archive_NotAuthor` | Seed players "p1" and "p2". Create note as "p1". Archive with `callerPlayerID="p2"`. | Error wrapping `domain.ErrForbidden`. |
| `TestNoteService_Archive_NotFound` | Archive random UUID. | Error wrapping `domain.ErrNotFound`. |
| `TestNoteService_List_DefaultWindow` | Seed player "p1". Create 25 notes. List with no override. | Returns exactly 20 (the default window). |
| `TestNoteService_List_PlayerFilter` | Seed "p1" and "p2". Create 3 notes as "p1", 2 as "p2". List with `PlayerID="p1"`. | Returns 3. |

For creating tasks in tests, use the task repo directly:
```go
task := &domain.Task{
    ID:        uuid.New(),
    ShortID:   "test1234",
    ProjectID: uuid.Nil,
    Title:     "test task",
    Status:    "pending",
    Version:   1,
    CreatedAt: now,
    ModifiedAt: now,
}
taskRepo.Create(ctx, task)
```

**Acceptance:** `go test ./service/ -run TestNoteService` passes. All 11 test functions pass.

---

### Task 4: Wire NoteService into Client and fix CLI bundle

#### 4a: `client.go`

Add `Notes *service.NoteService` to the `Client` struct:

```go
type Client struct {
    Tasks     *service.TaskService
    Tags      *service.TagService
    Relations *service.RelationService
    Projects  *service.ProjectService
    Workflows *service.WorkflowService
    Players   *service.PlayerService
    Notes     *service.NoteService  // <-- add

    store *sqlite.Store
}
```

In `NewClient`, after `playerSvc` construction (around line 146), add:

```go
noteSvc := service.NewNoteService(bundle.Notes, bundle.Players, projectRepo, bundle.Tasks, 20)
```

Add `Notes: noteSvc` to the Client struct literal (around line 148).

The hardcoded `20` will be replaced in phase 2 with `cfg.Notes.WindowSize` once the config struct is extended.

#### 4b: `client_test.go`

Add a test that exercises the Notes field:

```go
func TestClient_Notes(t *testing.T) {
    client := newTestClient(t)
    defer client.Close()

    if client.Notes == nil {
        t.Fatal("Notes service should not be nil")
    }
}
```

If `newTestClient` doesn't exist, check how existing client tests are structured and follow the same pattern.

#### 4c: `cmd/tusk/main.go`

Add `Notes` to the bundle literal at lines 103-110:

```go
bundle := &service.RepoBundle{
    Store:       store,
    Tasks:       sqlite.NewTaskRepo(db),
    Annotations: sqlite.NewAnnotationRepo(db),
    Notes:       sqlite.NewNoteRepo(db),  // <-- add
    Relations:   sqlite.NewRelationRepo(db),
    Tags:        sqlite.NewTagRepo(db),
    Players:     sqlite.NewPlayerRepo(db),
}
```

Do **not** construct a `NoteService` in `main.go` or pass it to `tui.New` yet — that belongs to the Note CLI initiative (a separate v0.12 initiative).

#### 4d: `service/bundle_helpers_test.go`

Add `Notes` to the `bundleFromStore` function:

```go
func bundleFromStore(store *sqlite.Store) *RepoBundle {
    db := store.DB()
    return &RepoBundle{
        Store:       store,
        Tasks:       sqlite.NewTaskRepo(db),
        Annotations: sqlite.NewAnnotationRepo(db),
        Notes:       sqlite.NewNoteRepo(db),  // <-- add
        Relations:   sqlite.NewRelationRepo(db),
        Tags:        sqlite.NewTagRepo(db),
        Players:     sqlite.NewPlayerRepo(db),
    }
}
```

**Acceptance:** `go build ./cmd/tusk` succeeds. `go test ./...` passes (no regressions). `client.Notes` is non-nil after `NewClient`.

---

## User-Visible Behaviors After This Phase

1. Library consumers (`tusk.NewClient`) can call `client.Notes.Create`, `client.Notes.Archive`, and `client.Notes.List`.
2. `List` returns at most 20 notes by default. `WindowOverride` can raise or lower that.
3. `Archive` rejects attempts by non-authors with `domain.ErrForbidden`.
4. `Create` validates player, project, task-project association, and non-empty body.
5. CLI and MCP are unchanged — no new commands or tools. The Notes repo gap in `main.go` is silently fixed.
6. All existing tests continue to pass.

---

## Changes Introduced

| Category | Detail |
|----------|--------|
| **New files** | `service/note.go`, `service/note_test.go` |
| **Modified files** | `domain/errors.go` (add `ErrForbidden`), `client.go` (add `Notes` field + wiring), `client_test.go` (new test), `cmd/tusk/main.go` (add `Notes` to bundle), `service/bundle_helpers_test.go` (add `Notes` to helper) |
| **New domain errors** | `domain.ErrForbidden` |
| **New service types** | `service.NoteService`, `service.NoteListParams` |
| **Bridge code** | Hardcoded `defaultWindowSize = 20` in `NewNoteService` constructor and `client.go` wiring. **Removal target: Phase 2** — replaced by config-driven `defaultWindowSize` via `config.Notes.WindowSize`. |
| **Schema migrations** | None |
| **Dependencies** | None |
