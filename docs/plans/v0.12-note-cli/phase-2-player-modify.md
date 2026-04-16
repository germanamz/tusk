# Phase 2: Player Modify CLI

**Initiative:** Note CLI (v0.12)
**Prerequisites:** None — builds on the base codebase. May run in parallel with Phase 1.
**Design spec:** `docs/plans/v0.12-note-cli/design.md`

---

## Goal

Expose the existing `PlayerRepository.UpdateNoteWindowSize` via a service method and a new `tusk player modify` subcommand. After this phase, a user can set or clear a per-player note-window override:

```bash
tusk player modify agent-1 note-window-size=50   # set
tusk player modify agent-1 note-window-size=     # clear (fall back to project/global)
```

This closes the v0.12 "Story: Player window size preference" roadmap item. Note CLI commands (`tusk note add|list|archive`) are **not** part of this phase and are added in Phase 3.

## Inherits From

Base codebase as of main. No prior phases touched. Specifically:

- `domain.Player.NoteWindowSize *int` already exists (`domain/player.go`).
- `repository.PlayerRepository.UpdateNoteWindowSize(ctx, id, size *int)` already exists (`repository/player.go`).
- `sqlite.PlayerRepo.UpdateNoteWindowSize` already implements the storage side (`sqlite/player.go`).
- `service.PlayerService` exists with `Register`, `GetByID`, `UpdateLastSeen`, `List` (`service/player.go`).
- `internal/tui/commands.go` exposes `buildPlayerCmd` with only a `register` subcommand (lines 1106–1137 of current main).
- `internal/tui/render.go` has `toPlayerJSON` / `renderPlayerResult`; the JSON shape does **not** currently include `note_window_size`.

## Tasks

### Task 1: Add `PlayerService.SetNoteWindowSize`

**File:** `service/player.go`

Append a new method on `*PlayerService`. Follow the existing method style (context, sentinel errors wrapped via `fmt.Errorf`, returns updated domain type where reasonable).

```go
// SetNoteWindowSize updates the caller-scoped note-window override on a
// player. Passing a non-nil size persists that size as the override;
// passing nil clears the override so the player falls back to project and
// global defaults. Size must be positive when non-nil.
//
// Returns domain.ErrNotFound if no player matches.
func (s *PlayerService) SetNoteWindowSize(ctx context.Context, id string, size *int) (*domain.Player, error) {
    if id == "" {
        return nil, fmt.Errorf("player ID must not be empty")
    }
    if size != nil && *size <= 0 {
        return nil, fmt.Errorf("note window size must be positive, got %d", *size)
    }

    // Confirm the player exists before writing — UpdateNoteWindowSize
    // currently uses a simple UPDATE and returns ErrNotFound on 0 rows,
    // but doing an explicit GetByID first lets us return a consistent
    // error shape and gives us the record to return on success.
    if _, err := s.repo.GetByID(ctx, id); err != nil {
        return nil, fmt.Errorf("player %q: %w", id, err)
    }

    if err := s.repo.UpdateNoteWindowSize(ctx, id, size); err != nil {
        return nil, fmt.Errorf("updating player %q note window size: %w", id, err)
    }

    updated, err := s.repo.GetByID(ctx, id)
    if err != nil {
        return nil, fmt.Errorf("reloading player %q: %w", id, err)
    }
    return updated, nil
}
```

**Acceptance:** `go build ./service/...` passes. Method is callable from a test file in the same package.

---

### Task 2: Test `PlayerService.SetNoteWindowSize`

**File:** `service/player_test.go`

Add cases, reusing the existing in-package fake or test fixture already used by `Register` / `GetByID` tests in this file. If a repository stub is required, mirror whatever pattern the existing tests in `service/player_test.go` already use (don't introduce a new fake style).

Cases:

1. **Set override on existing player** — register a player, call `SetNoteWindowSize(ctx, "agent-1", ptrInt(50))`, assert returned player has `NoteWindowSize == &50` and the repo reports the same after a fresh `GetByID`.
2. **Clear override** — register a player with a non-nil override, call `SetNoteWindowSize(ctx, "agent-1", nil)`, assert returned player has `NoteWindowSize == nil`.
3. **Non-existent player** — call on a never-registered ID, assert `errors.Is(err, domain.ErrNotFound)`.
4. **Empty ID** — call with `id == ""`, assert error containing "must not be empty". Repo should never be touched.
5. **Invalid size** — call with `ptrInt(0)` and `ptrInt(-3)`, assert errors containing "must be positive". Repo should never be touched.

Helper:

```go
func ptrInt(v int) *int { return &v }
```

(If a `ptrInt` helper already exists in the package test files, reuse it.)

**Acceptance:** `go test -v ./service -run TestPlayerSetNoteWindowSize` passes. `go test -race ./service/...` passes.

---

### Task 3: Add `modify` subcommand to `buildPlayerCmd`

**File:** `internal/tui/commands.go`

Extend `buildPlayerCmd` (currently at lines 1106–1122 of main). Register a new `modify` subcommand alongside the existing `register`.

```go
modifyCmd := &cobra.Command{
    Use:   "modify <id> [fields...]",
    Short: "Modify a player's settings",
    Long: `Update a player's configurable fields.

Supported fields:
  note-window-size=<N>   per-player override for the notes trailing window
  note-window-size=      clear the override (fall back to project/global default)

Examples:
  tusk player modify agent-1 note-window-size=50
  tusk player modify agent-1 note-window-size=`,
    Args: cobra.MinimumNArgs(2),
    RunE: a.runPlayerModify,
}
```

Register it on `playerCmd` after the existing `registerCmd.AddCommand` line:

```go
playerCmd.AddCommand(registerCmd)
playerCmd.AddCommand(modifyCmd)
```

Immediately below `runPlayerRegister`, add the handler:

```go
func (a *App) runPlayerModify(cmd *cobra.Command, args []string) error {
    ctx := cmd.Context()
    id := args[0]
    if id == "" {
        return fmt.Errorf("player ID must not be empty")
    }

    // Parse inline fields via the filter lexer so we stay consistent with
    // the rest of the CLI (task modify, project modify, etc.).
    fs, parseErrs := filter.Parse(strings.Join(args[1:], " "))
    if len(parseErrs) > 0 {
        return fmt.Errorf("%s", filter.FormatErrors(parseErrs))
    }

    var (
        sawNoteWindow bool
        newSize       *int
    )

    for _, f := range fs.Fields {
        switch f.Key {
        case "note-window-size":
            if f.Modifier != 0 {
                return fmt.Errorf("note-window-size does not accept %q prefix", string(f.Modifier))
            }
            sawNoteWindow = true
            if f.Value == "" {
                newSize = nil
                continue
            }
            n, parseErr := strconv.Atoi(f.Value)
            if parseErr != nil {
                return fmt.Errorf("note-window-size must be an integer, got %q", f.Value)
            }
            if n <= 0 {
                return fmt.Errorf("note-window-size must be positive, got %d", n)
            }
            v := n
            newSize = &v
        default:
            return fmt.Errorf("unknown field %q on player modify", f.Key)
        }
    }

    if !sawNoteWindow {
        return fmt.Errorf("no modifiable fields supplied")
    }

    player, err := a.playerSvc.SetNoteWindowSize(ctx, id, newSize)
    if err != nil {
        return fmt.Errorf("%s", err)
    }

    r := a.newRenderer(cmd.Context(), cmd.OutOrStdout(), nil)
    return r.renderPlayerResult("Updated", player)
}
```

**Imports to add to `internal/tui/commands.go`** if not already present in the file:

- `strconv`
- `strings`
- `github.com/germanamz/tusk/filter`

Check the existing imports before editing and only add what is missing. Do not reorder existing imports.

**Acceptance:** `go build ./internal/tui/...` passes. `tusk player modify --help` prints the new usage string.

---

### Task 4: Extend player rendering with `note_window_size`

**File:** `internal/tui/render.go`

Update `playerJSON` (currently around line 838) and `toPlayerJSON` to include the override:

```go
type playerJSON struct {
    ID             string `json:"id"`
    Type           string `json:"type"`
    NoteWindowSize *int   `json:"note_window_size,omitempty"`
    RegisteredAt   string `json:"registered_at"`
    LastSeenAt     string `json:"last_seen_at"`
}

func toPlayerJSON(p *domain.Player) playerJSON {
    return playerJSON{
        ID:             p.ID,
        Type:           p.Type,
        NoteWindowSize: p.NoteWindowSize,
        RegisteredAt:   p.RegisteredAt.Format(time.RFC3339),
        LastSeenAt:     p.LastSeenAt.Format(time.RFC3339),
    }
}
```

Update `renderPlayerResult` text output to report the override when set:

```go
_, err := fmt.Fprintf(r.w, "%s player %s (type: %s)\n", action, player.ID, player.Type)
if err != nil {
    return err
}
if player.NoteWindowSize != nil {
    _, err = fmt.Fprintf(r.w, "  note_window_size: %d\n", *player.NoteWindowSize)
}
return err
```

**Acceptance:** `go build ./internal/tui/...` passes. `go test ./internal/tui/...` passes (no existing tests should have asserted on the absence of `note_window_size` — if any hard-coded expected-JSON string is missing this key, update it in the same edit).

---

### Task 5: E2E scenarios

**File:** `tests/e2e/player_test.go` (create if absent; otherwise append to the existing file)

Follow the existing harness style — see `tests/e2e/annotations_test.go` for a template. Every scenario runs in both JSON and text modes automatically via the harness.

Add scenarios under a single `TestPlayerModify` function:

1. **Register then set window size** — `player register agent-1 --type agent`, then `player modify agent-1 note-window-size=50`. Assert JSON payload contains `"note_window_size": 50`. Assert text output contains `note_window_size: 50`.
2. **Clear window size** — chain from previous, then `player modify agent-1 note-window-size=`. The `playerJSON` type uses `*int` + `omitempty`, so the key must be **absent** from the JSON payload. Assert `note_window_size` absent. Assert text output has no `note_window_size:` line.
3. **Invalid value — negative** — `player modify agent-1 note-window-size=-5`. Assert the command fails with non-zero exit and error message includes "must be positive".
4. **Invalid value — non-integer** — `player modify agent-1 note-window-size=abc`. Assert command fails with "must be an integer".
5. **Unknown field** — `player modify agent-1 bogus=1`. Assert command fails with "unknown field \"bogus\"".
6. **Non-existent player** — `player modify ghost note-window-size=50` without registering `ghost`. Assert command fails with error containing `not found` (sentinel `ErrNotFound` bubbles through).

**Acceptance:** `go test -v ./tests/e2e -run TestPlayerModify` passes. `make test-e2e` passes.

---

## User-visible behavior preserved

- `tusk player register` continues to work identically. Existing scenarios that create a player and compare JSON payloads must continue to pass; the new `note_window_size` key is omitted via `omitempty` when the override is nil, so the wire format stays identical for the default case.
- All other commands (`task`, `project`, `workflow`, `tag`, `config`, `mcp serve`, `completion`) are untouched.

## Bridge code introduced

None. All additions are real implementations.

## Changes Introduced

**New files:**
- `tests/e2e/player_test.go` (if not already present)

**Modified files:**
- `service/player.go` — added `SetNoteWindowSize`
- `service/player_test.go` — added unit tests
- `internal/tui/commands.go` — added `modify` subcommand and `runPlayerModify` handler
- `internal/tui/render.go` — `playerJSON` gained `note_window_size`; `renderPlayerResult` prints it when set

**New interfaces / methods:**
- `service.PlayerService.SetNoteWindowSize(ctx, id string, size *int) (*domain.Player, error)`

**Dependencies / migrations / env vars:** none.

**Bridge code removal targets:** none.
