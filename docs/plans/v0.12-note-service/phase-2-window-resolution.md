# Phase 2: Window Size Resolution

**Initiative:** Note Service (v0.12)
**Prerequisites:** Phase 1 (NoteService Core) must be complete.
**Design spec:** `docs/plans/v0.12-note-service/design.md`

---

## Inherits From

Phase 1 introduced:

- `domain.ErrForbidden` in `domain/errors.go`.
- `service.NoteService` in `service/note.go` — struct with `notes`, `players`, `projects`, `tasks` repo fields and `defaultWindowSize int`. Methods: `Create`, `Archive`, `List`.
- `service.NoteListParams` — carries `WindowOverride *int` and `CallerPlayerID string` (present but unused until this phase).
- `NoteService` wired into `client.go` with hardcoded `defaultWindowSize = 20` (**bridge code — removed in this phase**).
- `Notes` repo field added to the CLI bundle in `cmd/tusk/main.go` and `service/bundle_helpers_test.go`.

The implementer can rely on:
- `NoteService.List` currently resolves window size as: `WindowOverride` if set, else `defaultWindowSize`. This phase extends that to a full resolution chain.
- `CallerPlayerID` is already a field on `NoteListParams` but is currently ignored by `List`.
- Tests in `service/note_test.go` cover Create, Archive, and basic List behavior.

---

## Goal

Implement the full window-size resolution chain: CLI flag → player DB setting → project settings → global config → hardcoded fallback (20). This requires changes across four layers: migration, domain, config, and service.

---

## Tasks

### Task 1: Migration 008 — add `note_window_size` to players table

**New files:**
- `migrations/008_player_note_window.up.sql`
- `migrations/008_player_note_window.down.sql`

#### Up migration

```sql
ALTER TABLE players ADD COLUMN note_window_size INTEGER;
```

Nullable, no default. NULL means "no preference set."

#### Down migration

SQLite does not support `ALTER TABLE ... DROP COLUMN` before version 3.35.0 (2021-03-12). The project already uses `ALTER TABLE ... DROP COLUMN` in existing down migrations (check migration 007 for precedent). Follow the same pattern:

```sql
ALTER TABLE players DROP COLUMN note_window_size;
```

If existing down migrations use the table-rebuild approach instead, follow that pattern. Check `migrations/002_players.down.sql` and `migrations/007_notes.down.sql` for the convention.

**Acceptance:** `go test ./...` passes (migrations run on every test store creation via `sqlitetest.NewStore`). The `players` table now has a `note_window_size` column.

---

### Task 2: Update domain and repository interfaces

#### 2a: `domain/player.go`

Add `NoteWindowSize *int` to the `Player` struct:

```go
type Player struct {
    ID             string
    Type           string    // "human" or "agent"
    NoteWindowSize *int      // nil = no preference; player uses project/global default
    RegisteredAt   time.Time
    LastSeenAt     time.Time
}
```

#### 2b: `repository/player.go`

Add `UpdateNoteWindowSize` to the `PlayerRepository` interface:

```go
type PlayerRepository interface {
    Create(ctx context.Context, player *domain.Player) error
    GetByID(ctx context.Context, id string) (*domain.Player, error)
    UpdateLastSeen(ctx context.Context, id string) error
    UpdateNoteWindowSize(ctx context.Context, id string, size *int) error
    List(ctx context.Context) ([]*domain.Player, error)
}
```

`size *int`: nil clears the preference (sets column to NULL), non-nil sets it.

#### 2c: `domain/project_settings.go`

Add `NoteWindowSize *int` to `ProjectSettings`:

```go
type ProjectSettings struct {
    AutoCompleteParent *AutoCompleteConfig `json:"auto_complete_parent,omitempty"`
    AutoRevertParent   *AutoRevertConfig   `json:"auto_revert_parent,omitempty"`
    Urgency            *UrgencyOverrides   `json:"urgency,omitempty"`
    NoteWindowSize     *int                `json:"note_window_size,omitempty"`
}
```

No migration needed — `ProjectSettings` is a JSON column. The new field is optional and `omitempty` means existing rows (which have no `note_window_size` key in their JSON) unmarshal with `nil`, which is the correct "inherit from global" behavior.

**Acceptance:** Compiles. The new field appears on the types. Existing tests that construct `Player` or `ProjectSettings` still compile (new fields are zero-valued by default).

---

### Task 3: Update SQLite player implementation

**File:** `sqlite/player.go`

#### 3a: Update `playerColumns`

Change line 12:

```go
const playerColumns = `id, type, note_window_size, registered_at, last_seen_at`
```

#### 3b: Update `scanPlayer`

Add `noteWindowSize` scan variable:

```go
func scanPlayer(s playerScanner) (*domain.Player, error) {
    var (
        p              domain.Player
        noteWindowSize sql.NullInt64
        registeredAt   string
        lastSeenAt     string
    )
    err := s.Scan(&p.ID, &p.Type, &noteWindowSize, &registeredAt, &lastSeenAt)
    if err != nil {
        if errors.Is(err, sql.ErrNoRows) {
            return nil, domain.ErrNotFound
        }
        return nil, err
    }
    if noteWindowSize.Valid {
        v := int(noteWindowSize.Int64)
        p.NoteWindowSize = &v
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

#### 3c: Update `Create`

The INSERT statement must include the new column. The `Player.NoteWindowSize` field may be nil (maps to SQL NULL):

```go
func (r *PlayerRepo) Create(ctx context.Context, player *domain.Player) error {
    var noteWindowSize any
    if player.NoteWindowSize != nil {
        noteWindowSize = *player.NoteWindowSize
    }
    _, err := r.db.ExecContext(ctx,
        `INSERT INTO players (id, type, note_window_size, registered_at, last_seen_at)
         VALUES (?, ?, ?, ?, ?)`,
        player.ID, player.Type, noteWindowSize,
        player.RegisteredAt.UTC().Format(timeFormat),
        player.LastSeenAt.UTC().Format(timeFormat),
    )
    if err != nil {
        if _, lookupErr := r.GetByID(ctx, player.ID); lookupErr == nil {
            return domain.ErrConflict
        }
        return err
    }
    return nil
}
```

#### 3d: Add `UpdateNoteWindowSize`

```go
func (r *PlayerRepo) UpdateNoteWindowSize(ctx context.Context, id string, size *int) error {
    var val any
    if size != nil {
        val = *size
    }
    res, err := r.db.ExecContext(ctx,
        `UPDATE players SET note_window_size = ? WHERE id = ?`, val, id)
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
```

Follows the same `RowsAffected` pattern as `UpdateLastSeen`.

**Acceptance:** `go test ./sqlite/ -run TestPlayer` passes. The existing player tests still work with the updated column list. New method compiles and follows the established error-handling pattern.

---

### Task 4: Add `NotesConfig` to the config package

#### 4a: `config/config.go`

Add the `NotesConfig` struct and field:

```go
// NotesConfig controls note listing defaults.
type NotesConfig struct {
    // WindowSize is the default number of recent notes to display.
    WindowSize int `mapstructure:"window_size" toml:"window_size" json:"window_size"`
}
```

Add to the `Config` struct, between `Inline` and `Sources`:

```go
type Config struct {
    Storage StorageConfig `mapstructure:"storage" toml:"storage" json:"storage"`
    Urgency UrgencyConfig `mapstructure:"urgency" toml:"urgency" json:"urgency"`
    TUI     TUIConfig     `mapstructure:"tui"     toml:"tui"     json:"tui"`
    MCP     MCPConfig     `mapstructure:"mcp"     toml:"mcp"     json:"mcp"`
    Inline  InlineConfig  `mapstructure:"inline"  toml:"inline"  json:"inline"`
    Notes   NotesConfig   `mapstructure:"notes"   toml:"notes"   json:"notes"`

    Sources ConfigSources `mapstructure:"-" toml:"-" json:"-"`
}
```

Optionally add a validation check in `Validate()`:

```go
if c.Notes.WindowSize <= 0 {
    return fmt.Errorf("notes.window_size must be > 0, got %d", c.Notes.WindowSize)
}
```

#### 4b: `config/default.toml`

Append after the `[inline]` section:

```toml
[notes]
window_size = 20  # Default trailing window size for note listings
```

**Acceptance:** `config.Load()` returns a `Config` with `Notes.WindowSize == 20` when using defaults. `go test ./config/` passes. Environment variable `TUSK_NOTES_WINDOW_SIZE=50` overrides the default.

---

### Task 5: Implement window resolution in NoteService

**File:** `service/note.go`

#### 5a: Add `resolveWindowSize` private method

```go
const defaultHardcodedWindowSize = 20

func (s *NoteService) resolveWindowSize(ctx context.Context, callerPlayerID string, projectID uuid.UUID, override *int) int {
    // 1. CLI/MCP flag override
    if override != nil && *override > 0 {
        return *override
    }

    // 2. Player DB setting
    if callerPlayerID != "" {
        if player, err := s.players.GetByID(ctx, callerPlayerID); err == nil && player.NoteWindowSize != nil && *player.NoteWindowSize > 0 {
            return *player.NoteWindowSize
        }
    }

    // 3. Project settings
    if project, err := s.projects.GetByID(ctx, projectID); err == nil && project.Settings.NoteWindowSize != nil && *project.Settings.NoteWindowSize > 0 {
        return *project.Settings.NoteWindowSize
    }

    // 4. Global config default (passed to constructor)
    if s.defaultWindowSize > 0 {
        return s.defaultWindowSize
    }

    // 5. Hardcoded fallback
    return defaultHardcodedWindowSize
}
```

Errors in player/project lookup are swallowed — they cause fallthrough. This is intentional: the resolution chain is best-effort for window sizing, not a hard validation gate.

#### 5b: Update `List` to use `resolveWindowSize`

Replace the current limit logic in `List`:

```go
func (s *NoteService) List(ctx context.Context, params NoteListParams) ([]*domain.Note, error) {
    limit := s.resolveWindowSize(ctx, params.CallerPlayerID, params.ProjectID, params.WindowOverride)

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

This replaces the Phase 1 bridge code (the two-line `limit` assignment that only checked `WindowOverride` and `defaultWindowSize`).

#### 5c: Remove bridge code from `client.go`

Update the `NewNoteService` call in `client.go` to pass the config-driven window size instead of hardcoded `20`. Since `tusk.Config` doesn't yet have a `Notes` field, add one:

In `client.go`, update the `Config` struct:

```go
type Config struct {
    DBPath  string
    Urgency config.UrgencyConfig
    Notes   config.NotesConfig  // <-- add
}
```

In `NewClient`, update the `NoteService` construction:

```go
windowSize := cfg.Notes.WindowSize
if windowSize <= 0 {
    windowSize = 20
}
noteSvc := service.NewNoteService(bundle.Notes, bundle.Players, projectRepo, bundle.Tasks, windowSize)
```

**Acceptance:** `go build ./...` succeeds. `go test ./service/ -run TestNoteService` passes. The Phase 1 bridge code (hardcoded `20`) is replaced by config-driven values.

---

### Task 6: Tests for window resolution and player column

**File:** `service/note_test.go` (extend), `sqlite/player_test.go` (extend or new)

#### 6a: SQLite player tests

Add to `sqlite/player_test.go` (or create if it doesn't exist as a standalone file — check whether `sqlite/player_test.go` exists; player tests may live in `service/player_test.go` instead; in that case add a new `sqlite/player_test.go`):

```go
func TestPlayerRepo_UpdateNoteWindowSize(t *testing.T) {
    store, _, _ := sqlitetest.NewStore(t)
    repo := sqlite.NewPlayerRepo(store.DB())
    ctx := context.Background()

    now := time.Now().UTC().Truncate(time.Millisecond)
    repo.Create(ctx, &domain.Player{ID: "p1", Type: "human", RegisteredAt: now, LastSeenAt: now})

    // Set window size
    size := 50
    if err := repo.UpdateNoteWindowSize(ctx, "p1", &size); err != nil {
        t.Fatalf("set: %v", err)
    }
    p, _ := repo.GetByID(ctx, "p1")
    if p.NoteWindowSize == nil || *p.NoteWindowSize != 50 {
        t.Fatalf("got %v, want 50", p.NoteWindowSize)
    }

    // Clear window size
    if err := repo.UpdateNoteWindowSize(ctx, "p1", nil); err != nil {
        t.Fatalf("clear: %v", err)
    }
    p, _ = repo.GetByID(ctx, "p1")
    if p.NoteWindowSize != nil {
        t.Fatalf("got %v, want nil", p.NoteWindowSize)
    }

    // Not found
    err := repo.UpdateNoteWindowSize(ctx, "ghost", &size)
    if !errors.Is(err, domain.ErrNotFound) {
        t.Fatalf("got %v, want ErrNotFound", err)
    }
}
```

Also add a test that `Create` with `NoteWindowSize` set round-trips correctly:

```go
func TestPlayerRepo_CreateWithWindowSize(t *testing.T) {
    store, _, _ := sqlitetest.NewStore(t)
    repo := sqlite.NewPlayerRepo(store.DB())
    ctx := context.Background()

    now := time.Now().UTC().Truncate(time.Millisecond)
    size := 30
    repo.Create(ctx, &domain.Player{ID: "p1", Type: "human", NoteWindowSize: &size, RegisteredAt: now, LastSeenAt: now})
    p, _ := repo.GetByID(ctx, "p1")
    if p.NoteWindowSize == nil || *p.NoteWindowSize != 30 {
        t.Fatalf("got %v, want 30", p.NoteWindowSize)
    }
}
```

#### 6b: NoteService resolution chain tests

Add to `service/note_test.go`. These tests need to seed players with specific `NoteWindowSize` values and projects with specific `ProjectSettings.NoteWindowSize` values.

To seed a project with settings, use `projectRepo.Update` after fetching the migration-seeded default project, or seed a new project with `sqlitetest.SeedProject` and then update its settings via the project repo.

The test helper needs to accept a custom `defaultWindowSize` to test the config layer:

```go
func newNoteTestEnvWithWindow(t *testing.T, defaultWindow int) (*service.NoteService, *sqlite.NoteRepo, *sqlite.ProjectRepo, *sqlite.PlayerRepo) {
    // same as newNoteTestEnv but passes defaultWindow instead of 20
}
```

| Test function | Setup | Assert |
|---------------|-------|--------|
| `TestNoteService_ResolveWindow_CLIOverride` | Seed player with `NoteWindowSize=50`, project settings with `NoteWindowSize=30`, service `defaultWindowSize=20`. List with `WindowOverride=intPtr(10)`. | Returns at most 10 notes (create 15 to verify). |
| `TestNoteService_ResolveWindow_PlayerSetting` | Seed player with `NoteWindowSize=5`, project settings with `NoteWindowSize=30`, service `defaultWindowSize=20`. List with `WindowOverride=nil`, `CallerPlayerID="p1"`. | Returns at most 5 notes (create 10 to verify). |
| `TestNoteService_ResolveWindow_ProjectSetting` | Seed player with `NoteWindowSize=nil`, project settings with `NoteWindowSize=8`, service `defaultWindowSize=20`. List with `CallerPlayerID="p1"`. | Returns at most 8 notes. |
| `TestNoteService_ResolveWindow_ConfigDefault` | Seed player with `NoteWindowSize=nil`, project settings with `NoteWindowSize=nil`, service `defaultWindowSize=15`. List with `CallerPlayerID="p1"`. | Returns at most 15 notes. |
| `TestNoteService_ResolveWindow_HardcodedFallback` | Service `defaultWindowSize=0`. Seed player with `NoteWindowSize=nil`. | Returns at most 20 notes (hardcoded fallback). |

Helper for creating N notes:
```go
func seedNotes(t *testing.T, svc *service.NoteService, n int, playerID string, projectID uuid.UUID) {
    t.Helper()
    ctx := context.Background()
    for i := 0; i < n; i++ {
        note := &domain.Note{
            ProjectID: projectID,
            PlayerID:  playerID,
            Body:      fmt.Sprintf("note %d", i),
        }
        if err := svc.Create(ctx, note); err != nil {
            t.Fatalf("seed note %d: %v", i, err)
        }
    }
}
```

To set `NoteWindowSize` on the default project's settings, use the project repo directly:
```go
proj, _ := projRepo.GetByID(ctx, uuid.Nil) // default project
windowSize := 8
proj.Settings.NoteWindowSize = &windowSize
projRepo.Update(ctx, proj)
```

Verify `projRepo.Update` exists and accepts a `*domain.Project` — check `repository/project.go` for the interface. If `Update` requires version matching, fetch first, then update.

**Acceptance:** `go test ./service/ -run TestNoteService_ResolveWindow` passes all 5 tests. `go test ./sqlite/ -run TestPlayerRepo` passes all player tests including the new ones. `go test ./...` passes with no regressions.

---

## User-Visible Behaviors After This Phase

1. **Window resolution chain works end-to-end.** `NoteService.List` resolves the effective window through CLI override → player preference → project settings → global config → hardcoded 20.
2. **Player window size preference persists.** The `note_window_size` column on `players` stores an optional per-player preference. `PlayerRepository.UpdateNoteWindowSize` sets or clears it.
3. **Project-level window size override works.** Setting `NoteWindowSize` in a project's `ProjectSettings` JSON overrides the global default for notes listed under that project.
4. **Config governs the global default.** The `[notes].window_size` key in `config.toml` (default 20) sets the baseline when no entity-level override is present. Environment variable `TUSK_NOTES_WINDOW_SIZE` overrides it.
5. **Library Client respects config.** `tusk.Config.Notes.WindowSize` propagates to the NoteService constructor.
6. All Phase 1 behaviors (Create validation, Archive author guard, List filtering) continue to work unchanged.

---

## Changes Introduced

| Category | Detail |
|----------|--------|
| **New files** | `migrations/008_player_note_window.up.sql`, `migrations/008_player_note_window.down.sql`, `sqlite/player_test.go` (if new) |
| **Modified files** | `domain/player.go`, `domain/project_settings.go`, `repository/player.go`, `sqlite/player.go`, `config/config.go`, `config/default.toml`, `service/note.go`, `service/note_test.go`, `client.go` |
| **Schema migrations** | 008 — adds `note_window_size INTEGER` to `players` table |
| **New config keys** | `notes.window_size` (default 20), env `TUSK_NOTES_WINDOW_SIZE` |
| **New repository methods** | `PlayerRepository.UpdateNoteWindowSize(ctx, id, *int) error` |
| **New domain fields** | `Player.NoteWindowSize *int`, `ProjectSettings.NoteWindowSize *int` |
| **Bridge code removed** | Hardcoded `20` in `client.go`'s `NewNoteService` call → replaced by `cfg.Notes.WindowSize` |
| **Bridge code introduced** | None |
| **Dependencies** | None |
