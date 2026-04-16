# Note Service — Design Spec

**Initiative:** Note Service (v0.12)
**Scope:** `NoteService` with Create, List, Archive methods and configurable trailing-window resolution.

---

## Context

The Note entity, `NoteRepository` interface, SQLite implementation, and `notes` table migration (007) were all shipped in the prior "Note Entity & Storage" initiative. This initiative adds the business-logic layer (`NoteService`) and the window-size resolution chain that controls how many notes `List` returns by default.

### What already exists

| Artifact | Location | Notes |
|----------|----------|-------|
| `domain.Note` | `domain/note.go` | ID, ProjectID, PlayerID, TaskID (nullable), Body, Metadata, ArchivedAt, CreatedAt |
| `repository.NoteRepository` | `repository/note.go` | Create, GetByID, Archive, List |
| `repository.NoteListOptions` | `repository/note.go` | ProjectID, PlayerID, TaskID, Since, IncludeArchived, Limit |
| SQLite `NoteRepo` | `sqlite/note.go` | Full implementation; List builds dynamic WHERE + ORDER BY created_at DESC + optional LIMIT |
| Migration 007 | `migrations/007_notes.up.sql` | `notes` table with composite and partial indexes |
| `Tx.Notes()` | `sqlite/store.go:91` | Transactional NoteRepo access |
| `RepoBundle.Notes` | `service/repos.go` | Field exists on the bundle struct |
| `client.go` bundle | `client.go:91` | Notes wired into the library Client's bundle |

### Known gaps (pre-existing, to be fixed)

| Gap | Location | Fix phase |
|-----|----------|-----------|
| `Notes` missing from CLI bundle | `cmd/tusk/main.go:103-110` | Phase 1 |
| `Notes` missing from test bundle helper | `service/bundle_helpers_test.go:33-41` | Phase 1 |

---

## NoteService Design

### Dependencies

```go
type NoteService struct {
    notes             repository.NoteRepository
    players           repository.PlayerRepository
    projects          repository.ProjectRepository
    tasks             repository.TaskRepository
    defaultWindowSize int // from config or hardcoded fallback (20)
}
```

Follows the `PlayerService` pattern (direct repo references) rather than the `TaskService` pattern (`BundleResolver`). Rationale: notes don't need cross-store routing. The workspace is single-store. Direct repos are simpler and more testable.

### Constructor

```go
func NewNoteService(
    notes    repository.NoteRepository,
    players  repository.PlayerRepository,
    projects repository.ProjectRepository,
    tasks    repository.TaskRepository,
    defaultWindowSize int,
) *NoteService
```

### Methods

#### Create

```go
func (s *NoteService) Create(ctx context.Context, note *domain.Note) error
```

Validation:
1. `note.Body` must be non-empty (trimmed whitespace check).
2. `note.PlayerID` must reference an existing player (`players.GetByID`).
3. `note.ProjectID` must reference an existing project (`projects.GetByID`).
4. If `note.TaskID` is non-nil, the task must exist (`tasks.GetByID`) and its `ProjectID` must match `note.ProjectID`.
5. Generate `note.ID = uuid.New()` and `note.CreatedAt = time.Now().UTC()`.
6. `note.ArchivedAt` forced to nil on create.
7. Delegate to `notes.Create(ctx, note)`.

No transaction needed — single INSERT with FK constraints as backup validation.

#### Archive

```go
func (s *NoteService) Archive(ctx context.Context, id uuid.UUID, callerPlayerID string) error
```

Validation:
1. Fetch note via `notes.GetByID(ctx, id)` — propagates `domain.ErrNotFound`.
2. Verify `note.PlayerID == callerPlayerID`. If not, return a new sentinel error `domain.ErrForbidden` (or a plain `fmt.Errorf` — see decision below).
3. Delegate to `notes.Archive(ctx, id, time.Now().UTC())`.

**Decision: author-only guard error.** The roadmap says "validate caller is author." No existing sentinel covers this. Options:
- Add `domain.ErrForbidden` — clean, but introduces a new concept (authorization) that nothing else uses yet.
- Return `fmt.Errorf("note %s belongs to player %q, not %q", id, note.PlayerID, callerPlayerID)` — descriptive, no new sentinel.

**Chosen:** `fmt.Errorf` with a descriptive message. Wrap a new `domain.ErrForbidden` sentinel so callers can `errors.Is` on it. Pattern: `fmt.Errorf("...: %w", domain.ErrForbidden)`. Add `ErrForbidden` to `domain/errors.go`.

#### List

```go
type NoteListParams struct {
    ProjectID       uuid.UUID
    PlayerID        string     // filter; empty = all players' notes
    CallerPlayerID  string     // who's asking; used for window-size resolution
    TaskID          *uuid.UUID
    Since           *time.Time
    IncludeArchived bool
    WindowOverride  *int       // CLI/MCP flag; nil = not set
}

func (s *NoteService) List(ctx context.Context, params NoteListParams) ([]*domain.Note, error)
```

1. Resolve effective window size via the resolution chain (see below).
2. Map `NoteListParams` to `repository.NoteListOptions`, setting `Limit` to the resolved window.
3. Delegate to `notes.List(ctx, opts)`.

No project/player existence validation on List — a missing project or player simply returns an empty result set, matching how `TaskService.List` works (filter, don't reject).

### Window Size Resolution

Resolution chain (highest to lowest priority):

1. **CLI/MCP flag** — `params.WindowOverride` (non-nil, > 0).
2. **Player DB setting** — `player.NoteWindowSize` (non-nil, > 0). Looked up via `players.GetByID(ctx, params.CallerPlayerID)`.
3. **Project settings** — `project.Settings.NoteWindowSize` (non-nil, > 0). Looked up via `projects.GetByID(ctx, params.ProjectID)`.
4. **Global config default** — `s.defaultWindowSize` (passed to constructor from `config.Notes.WindowSize`).
5. **Hardcoded fallback** — 20. Used when `defaultWindowSize` is 0 (e.g., zero-valued config in library usage).

Implementation: private method `resolveWindowSize(ctx, callerPlayerID, projectID, override *int) int`.

Errors during player/project lookup in the resolution chain are swallowed — they cause fallthrough to the next level. The caller's identity for window resolution is a best-effort optimization, not a hard requirement.

---

## Window Size Infrastructure

### Player: `note_window_size` column

- **Migration 008:** `ALTER TABLE players ADD COLUMN note_window_size INTEGER;` (nullable, no default).
- **Domain:** Add `NoteWindowSize *int` to `domain.Player`.
- **Repository:** Add `UpdateNoteWindowSize(ctx context.Context, id string, size *int) error` to `repository.PlayerRepository`.
- **SQLite:** Update `playerColumns`, `scanPlayer`, `Create` in `sqlite/player.go`. Add `UpdateNoteWindowSize` method.

### Project settings: `note_window_size`

- Add `NoteWindowSize *int `json:"note_window_size,omitempty"`` to `domain.ProjectSettings`.
- No migration needed — `ProjectSettings` is stored as a JSON column (`projects.settings`). Adding a new optional field to the JSON is backward-compatible.

### Global config: `[notes]` section

```toml
[notes]
window_size = 20  # Default trailing window for note listings
```

- Add `NotesConfig` struct with `WindowSize int` to `config/config.go`.
- Add `Notes NotesConfig` field to `Config`.
- Add `[notes]` section to `config/default.toml`.

### Client config

- Add `Notes config.NotesConfig` to `tusk.Config` (the library config in `client.go`).
- Pass `cfg.Notes.WindowSize` (or hardcoded 20 if zero) as `defaultWindowSize` to `NewNoteService`.

---

## Testing Strategy

Service tests follow the existing pattern: real SQLite via `sqlitetest.NewStore`, no mocks. Each test function creates a fresh DB, seeds required entities (player, project), and exercises one service method.

### Phase 1 tests (`service/note_test.go`)

- `TestNoteService_Create` — happy path with project-level note.
- `TestNoteService_Create_WithTask` — task-scoped note, task belongs to project.
- `TestNoteService_Create_TaskWrongProject` — task exists but belongs to different project → error.
- `TestNoteService_Create_EmptyBody` — trimmed empty body → error.
- `TestNoteService_Create_MissingPlayer` — nonexistent player → error.
- `TestNoteService_Create_MissingProject` — nonexistent project → error.
- `TestNoteService_Archive` — happy path.
- `TestNoteService_Archive_NotAuthor` — different player → `domain.ErrForbidden`.
- `TestNoteService_Archive_NotFound` — nonexistent note → `domain.ErrNotFound`.
- `TestNoteService_List_DefaultWindow` — returns at most `defaultWindowSize` notes.
- `TestNoteService_List_PlayerFilter` — scopes to specific player.

### Phase 2 tests

- `TestNoteService_ResolveWindow_CLIOverride` — override takes priority.
- `TestNoteService_ResolveWindow_PlayerSetting` — player's setting wins when no override.
- `TestNoteService_ResolveWindow_ProjectSetting` — project setting wins when player has none.
- `TestNoteService_ResolveWindow_ConfigDefault` — config default wins when no entity settings.
- `TestNoteService_ResolveWindow_HardcodedFallback` — 20 when defaultWindowSize is 0.
- `TestPlayerRepo_UpdateNoteWindowSize` — set, read back, clear.

---

## Files Modified/Created (full list across both phases)

| File | Action | Phase |
|------|--------|-------|
| `domain/errors.go` | Add `ErrForbidden` | 1 |
| `service/note.go` | **New** — NoteService | 1 |
| `service/note_test.go` | **New** — tests | 1, 2 |
| `service/bundle_helpers_test.go` | Add Notes to `bundleFromStore` | 1 |
| `client.go` | Add `Notes *service.NoteService` to Client, wire in NewClient | 1 |
| `client_test.go` | Add test for Notes field on Client | 1 |
| `cmd/tusk/main.go` | Add `Notes` to bundle literal | 1 |
| `migrations/008_player_note_window.up.sql` | **New** — ALTER TABLE players | 2 |
| `migrations/008_player_note_window.down.sql` | **New** — reverse migration | 2 |
| `domain/player.go` | Add `NoteWindowSize *int` | 2 |
| `domain/project_settings.go` | Add `NoteWindowSize *int` | 2 |
| `repository/player.go` | Add `UpdateNoteWindowSize` | 2 |
| `sqlite/player.go` | Update columns, scan, add method | 2 |
| `sqlite/player_test.go` | Test new method | 2 |
| `config/config.go` | Add `NotesConfig`, `Notes` field on `Config` | 2 |
| `config/default.toml` | Add `[notes]` section | 2 |
| `service/note.go` | Update List with resolution chain | 2 |
| `service/note_test.go` | Resolution chain tests | 2 |
