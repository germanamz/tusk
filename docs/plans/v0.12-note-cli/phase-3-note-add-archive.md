# Phase 3: `tusk note add` + `tusk note archive`

**Initiative:** Note CLI (v0.12)
**Prerequisites:** Phase 1 (shared helpers) must be complete. Specifically, `repository.NoteRepository.FindByIDPrefix` and its SQLite implementation must exist.
**Design spec:** `docs/plans/v0.12-note-cli/design.md`

---

## Goal

Deliver the write side of the `tusk note` subcommand: creating a note and archiving one by UUID prefix. `tusk note list` is intentionally deferred to Phase 4 — it is **not** registered as a subcommand in this phase, so `tusk note list --help` is expected to print the default cobra "unknown command" message. That is acceptable; Phase 4 adds it.

After this phase, the following flows work end-to-end:

```bash
tusk note add "retry logic needed" project=backend --task a3f8b2c1 meta.topic=auth --player agent-1
tusk note add @./draft.md project=backend --player agent-1
cat draft.md | tusk note add @- project=backend --player agent-1
tusk note archive 5f3e2d1c --player agent-1        # prefix, min 8 chars
tusk note archive 5f3e2d1c-1234-4567-89ab-cdef01234567 --player agent-1   # full UUID
```

## Inherits From

- **Base codebase** — `domain.Note`, `service.NoteService` with `Create/Archive/List`, `repository.NoteRepository`, SQLite implementation, migrations 007 and 008 are all already present. `Client.Notes` is wired in `client.go` and exposes the service.
- **Phase 1** — `filter.ParseRelativeDuration` exists (unused in this phase but must compile) and `NoteRepository.FindByIDPrefix` exists with the SQLite implementation.
- **Not inherited** — `tui.App` does **not** yet hold a `noteSvc *service.NoteService` field. `tui.New` signature does not accept a note service. `cmd/tusk/main.go` does not pass one. This phase wires all three.

## Tasks

### Task 1: Thread `NoteService` through `tui.App` and `cmd/tusk`

**Files:** `internal/tui/app.go`, `cmd/tusk/main.go`

In `internal/tui/app.go`:

1. Add a new field to the `App` struct:
   ```go
   noteSvc *service.NoteService
   ```
   Place it next to `playerSvc` so the alphabetical/group ordering matches the existing layout.

2. Add a new parameter to `New` after `playerSvc`:
   ```go
   func New(
       taskSvc *service.TaskService,
       tagSvc *service.TagService,
       relationSvc *service.RelationService,
       projectSvc *service.ProjectService,
       workflowSvc *service.WorkflowService,
       playerSvc *service.PlayerService,
       noteSvc *service.NoteService,
       workflowRepo repository.WorkflowRepository,
       projectRepo repository.ProjectRepository,
       urgencyEngine *service.UrgencyEngine,
       vi VersionInfo,
       tuiCfg config.TUIConfig,
       mcpCfg config.MCPConfig,
       inlineCfg config.InlineConfig,
       loadOpts []config.Option,
   ) *App {
   ```
   Assign it inside the struct literal (`noteSvc: noteSvc`). Do **not** touch the MCP server's signature — this initiative does not expose notes via MCP.

3. Register the new command after `buildPlayerCmd`:
   ```go
   a.root.AddCommand(a.buildPlayerCmd())
   a.root.AddCommand(a.buildNoteCmd())
   a.root.AddCommand(a.buildConfigCmd())
   ```

In `cmd/tusk/main.go`:

1. Find the `tui.New(...)` call site and add `client.Notes` in the matching positional slot (after `playerSvc`). The call site today passes services pulled off `client.*` — just add `client.Notes` between `client.Players` and `workflowRepo`/`projectRepo` (match whichever variable names are in the current file).

**Acceptance:** `go build ./...` passes. `tusk note --help` prints the command group header (empty subcommand list until Task 2 wires children).

---

### Task 2: Create `internal/tui/note.go` with `buildNoteCmd`

**File:** `internal/tui/note.go` (new)

Package boilerplate and the command group scaffold. Only `add` and `archive` subcommands are registered in this phase. `list` is **not** registered here; Phase 4 adds it by editing this file.

```go
package tui

import (
    "context"
    "encoding/json"
    "fmt"
    "os"
    "strings"
    "time"

    "github.com/germanamz/tusk/domain"
    "github.com/germanamz/tusk/filter"
    "github.com/google/uuid"
    "github.com/spf13/cobra"
)

// Phase 4 will add "github.com/germanamz/tusk/service", "io", "sort",
// and "charm.land/glamour/v2" imports when list rendering lands. Do not
// pre-import them here — Go rejects unused imports.

const defaultProjectName = "_default"

// buildNoteCmd creates the `tusk note` subcommand group.
// list is added in Phase 4 — do not register it here.
func (a *App) buildNoteCmd() *cobra.Command {
    noteCmd := &cobra.Command{
        Use:   "note",
        Short: "Player notebook — create, archive, and list notes",
    }

    addCmd := &cobra.Command{
        Use:   "add <body> [project=<name>] [meta.key=value...]",
        Short: "Create a note",
        Long: `Create a note in a project, optionally scoped to a task.

The body may be literal text, an @./path reference, or @- for stdin
(same conventions as task create/modify). Project defaults to the
built-in "_default" project when project=<name> is not supplied.

Arbitrary metadata is namespaced under meta. — e.g., meta.topic=auth.
Bare key=value tokens that are not reserved (project=, task=) are
rejected to surface typos.`,
        Args: cobra.MinimumNArgs(1),
        RunE: a.runNoteAdd,
    }
    addCmd.Flags().String("task", "", "attach the note to a task by short ID")

    archiveCmd := &cobra.Command{
        Use:   "archive <note_id_or_prefix>",
        Short: "Archive a note",
        Long: `Archive a note by its UUID or an 8+ character UUID prefix.

Only the note's author can archive it.`,
        Args: cobra.ExactArgs(1),
        RunE: a.runNoteArchive,
    }

    noteCmd.AddCommand(addCmd)
    noteCmd.AddCommand(archiveCmd)
    return noteCmd
}
```

**Acceptance:** `go build ./internal/tui/...` passes. `tusk note --help` lists `add` and `archive`. `tusk note list --help` fails with the default cobra "unknown command" message — this is expected, Phase 4 adds it.

---

### Task 3: Implement `runNoteAdd`

**File:** `internal/tui/note.go` (same file as Task 2)

Handler that parses the body, resolves project/task/player, collects `meta.*` fields, creates the note, and renders the result.

```go
func (a *App) runNoteAdd(cmd *cobra.Command, args []string) error {
    ctx := cmd.Context()

    if a.playerID == "" {
        return fmt.Errorf("--player flag is required for note add")
    }
    if err := a.ensurePlayer(ctx); err != nil {
        return err
    }

    // Body is the first positional arg; everything after is inline fields.
    rawBody := args[0]
    fs, parseErrs := filter.Parse(strings.Join(args[1:], " "))
    if len(parseErrs) > 0 {
        return fmt.Errorf("%s", filter.FormatErrors(parseErrs))
    }

    // Stdin handle for @- expansion.
    var stdinFile *os.File
    if f, ok := cmd.InOrStdin().(*os.File); ok {
        stdinFile = f
    }
    state := &expandState{}

    body, err := a.expandRefsWithState(rawBody, stdinFile, state)
    if err != nil {
        return err
    }
    if strings.TrimSpace(body) == "" {
        return fmt.Errorf("note body must not be empty")
    }

    // Project (default "_default").
    projectName := defaultProjectName
    if f, ok := fs.GetField("project"); ok {
        if f.Modifier != 0 {
            return fmt.Errorf("project field does not accept %q prefix", string(f.Modifier))
        }
        if f.Value == "" {
            return fmt.Errorf("project value must not be empty")
        }
        projectName = f.Value
    }
    project, err := a.projectSvc.GetByName(ctx, projectName)
    if err != nil {
        return fmt.Errorf("resolving project %q: %w", projectName, err)
    }

    // Task (optional, via --task flag, resolved to short ID then UUID).
    var taskID *uuid.UUID
    if taskShort, _ := cmd.Flags().GetString("task"); taskShort != "" {
        task, err := a.taskSvc.GetByShortID(ctx, taskShort)
        if err != nil {
            return fmt.Errorf("resolving task %q: %w", taskShort, err)
        }
        id := task.ID
        taskID = &id
    }

    // Metadata from meta.<key>=<value> tokens; reject unknown bare fields.
    metadata := map[string]any{}
    for _, f := range fs.Fields {
        if f.Key == "project" {
            continue
        }
        if !strings.HasPrefix(f.Key, "meta.") {
            return fmt.Errorf("unknown field %q on note add (metadata keys must be prefixed with meta.)", f.Key)
        }
        if f.Modifier != 0 {
            return fmt.Errorf("metadata field %q does not accept %q prefix", f.Key, string(f.Modifier))
        }
        metaKey := strings.TrimPrefix(f.Key, "meta.")
        if metaKey == "" {
            return fmt.Errorf("metadata key must not be empty")
        }
        metadata[metaKey] = f.Value
    }

    note := &domain.Note{
        ProjectID: project.ID,
        PlayerID:  a.playerID,
        TaskID:    taskID,
        Body:      body,
        Metadata:  metadata,
    }
    if err := a.noteSvc.Create(ctx, note); err != nil {
        return fmt.Errorf("creating note: %w", err)
    }

    return a.renderNoteResult(cmd, "Created", note)
}
```

**Notes for the implementer:**

- Free text tokens (`fs.Text`) are ignored — the body is always `args[0]`. If the user passes multiple positional args (e.g. `tusk note add foo bar`), `cobra.MinimumNArgs(1)` admits them; everything after `args[0]` is handed to `filter.Parse`. The `filter` package treats anything that is not a `key=value` as free text, so it will not error — it will simply be ignored. This matches task create semantics where the title is resolved from `title=...` or from free text; for notes we always take the first positional as the body and ignore stray free text. Document this choice in the command `Long` description if needed (not required).
- `expandState` and `expandRefsWithState` are defined in `internal/tui/expand.go` and already in scope via the `tui` package.

**Acceptance:** `go build ./internal/tui/...` passes. Manual smoke: running `tusk note add "hello" project=_default --player tester` creates a note and prints the new ID. (Real assertions come from Task 6 e2e.)

---

### Task 4: Implement `runNoteArchive`

**File:** `internal/tui/note.go` (same file)

Resolves the argument to a single UUID via full-match or prefix lookup, then calls `NoteService.Archive`.

```go
func (a *App) runNoteArchive(cmd *cobra.Command, args []string) error {
    ctx := cmd.Context()

    if a.playerID == "" {
        return fmt.Errorf("--player flag is required for note archive")
    }
    if err := a.ensurePlayer(ctx); err != nil {
        return err
    }

    token := strings.ToLower(strings.TrimSpace(args[0]))
    id, err := a.resolveNoteID(ctx, token)
    if err != nil {
        return err
    }

    if err := a.noteSvc.Archive(ctx, id, a.playerID); err != nil {
        return fmt.Errorf("archiving note: %w", err)
    }

    // Reload the note so we can render the archived timestamp.
    archived, err := a.noteSvc.GetByID(ctx, id)
    if err != nil {
        // The service layer may not expose GetByID; see note below.
        return a.renderNoteArchivedBare(cmd, id)
    }
    return a.renderNoteResult(cmd, "Archived", archived)
}

// resolveNoteID accepts either a full UUID or a prefix of at least 8
// characters and returns the matching note's UUID. Prefix ambiguity is
// reported with the candidate prefixes so the caller can disambiguate.
func (a *App) resolveNoteID(ctx context.Context, token string) (uuid.UUID, error) {
    if id, err := uuid.Parse(token); err == nil {
        return id, nil
    }
    if len(token) < 8 {
        return uuid.Nil, fmt.Errorf("note id prefix must be at least 8 characters, got %d", len(token))
    }
    matches, err := a.noteSvc.FindByIDPrefix(ctx, token)
    if err != nil {
        return uuid.Nil, fmt.Errorf("resolving note id: %w", err)
    }
    switch len(matches) {
    case 0:
        return uuid.Nil, fmt.Errorf("no note matches id prefix %q", token)
    case 1:
        return matches[0].ID, nil
    default:
        ids := make([]string, 0, len(matches))
        for _, n := range matches {
            ids = append(ids, n.ID.String())
        }
        return uuid.Nil, fmt.Errorf("note id prefix %q is ambiguous: matches %s", token, strings.Join(ids, ", "))
    }
}
```

**Two service-layer gaps to address in this phase:**

1. `NoteService` in `service/note.go` currently does **not** expose `GetByID`. Add it:
   ```go
   // GetByID retrieves a note by its UUID.
   func (s *NoteService) GetByID(ctx context.Context, id uuid.UUID) (*domain.Note, error) {
       return s.notes.GetByID(ctx, id)
   }
   ```
   Place it next to the existing read methods. Add a unit test in `service/note_test.go` confirming it delegates correctly.

2. `App` does not hold a `NoteRepository` directly — it only holds the service. Add a pass-through on `NoteService`:
   ```go
   // FindByIDPrefix resolves a UUID prefix to notes. Used by the CLI to
   // accept short IDs for archive. See repository.NoteRepository for
   // semantics.
   func (s *NoteService) FindByIDPrefix(ctx context.Context, prefix string) ([]*domain.Note, error) {
       return s.notes.FindByIDPrefix(ctx, prefix)
   }
   ```
   Add a matching unit test in `service/note_test.go` confirming it delegates correctly. The `resolveNoteID` handler above calls `a.noteSvc.FindByIDPrefix` directly — no helper method on `App` is needed.

**Acceptance:** `go build ./...` passes. `go test ./service -run Note` passes (existing + new coverage).

---

### Task 5: Implement note rendering — single note

**File:** `internal/tui/note.go` (same file; rendering helpers live alongside handlers for now, list rendering moves to its own file only if Phase 4 finds it necessary)

Two renderers: `renderNoteResult` (action + note, used by add/archive) and `renderNoteArchivedBare` (fallback when the archive path can't reload the note).

```go
// noteJSON is the JSON serialization format for a note.
type noteJSON struct {
    ID         string         `json:"id"`
    ProjectID  string         `json:"project_id"`
    PlayerID   string         `json:"player_id"`
    TaskID     *string        `json:"task_id,omitempty"`
    Body       string         `json:"body"`
    Metadata   map[string]any `json:"metadata,omitempty"`
    CreatedAt  string         `json:"created_at"`
    ArchivedAt *string        `json:"archived_at,omitempty"`
}

func toNoteJSON(n *domain.Note) noteJSON {
    j := noteJSON{
        ID:        n.ID.String(),
        ProjectID: n.ProjectID.String(),
        PlayerID:  n.PlayerID,
        Body:      n.Body,
        Metadata:  n.Metadata,
        CreatedAt: n.CreatedAt.Format(time.RFC3339),
    }
    if n.TaskID != nil {
        s := n.TaskID.String()
        j.TaskID = &s
    }
    if n.ArchivedAt != nil {
        s := n.ArchivedAt.Format(time.RFC3339)
        j.ArchivedAt = &s
    }
    return j
}

func (a *App) renderNoteResult(cmd *cobra.Command, action string, note *domain.Note) error {
    w := cmd.OutOrStdout()
    if a.format == "json" {
        enc := json.NewEncoder(w)
        enc.SetIndent("", "  ")
        return enc.Encode(map[string]any{
            "action": strings.ToLower(action),
            "note":   toNoteJSON(note),
        })
    }
    shortID := note.ID.String()[:8]
    archivedMarker := ""
    if note.ArchivedAt != nil {
        archivedMarker = " [archived]"
    }
    _, err := fmt.Fprintf(w, "%s note %s%s\n", action, shortID, archivedMarker)
    return err
}

func (a *App) renderNoteArchivedBare(cmd *cobra.Command, id uuid.UUID) error {
    w := cmd.OutOrStdout()
    if a.format == "json" {
        enc := json.NewEncoder(w)
        enc.SetIndent("", "  ")
        return enc.Encode(map[string]any{
            "action": "archived",
            "id":     id.String(),
        })
    }
    _, err := fmt.Fprintf(w, "Archived note %s\n", id.String()[:8])
    return err
}
```

Glamour-based body rendering is **not** used here; that richer rendering is only needed by the list view and is implemented in Phase 4. For add/archive, a one-line confirmation is enough — users see the full body only when they listed the note.

**Acceptance:** `go build ./internal/tui/...` passes. Unit-level rendering is exercised only via e2e in Task 6.

---

### Task 6: E2E scenarios for `note add` + `note archive`

**File:** `tests/e2e/note_test.go` (new)

Follow the harness style in `tests/e2e/annotations_test.go`. Use the default `_default` project unless a scenario needs a specific project (the harness already creates an empty workspace with the default project available).

Scenarios under `TestNoteAddArchive`:

1. **Add with body + metadata, then archive by prefix**
   - Step 1: `note add "first note body" meta.topic=planning --player tester`
     - Assert JSON `action == "created"`, `note.body == "first note body"`, `note.metadata.topic == "planning"`, `note.task_id` absent, `note.archived_at` absent.
     - Capture `$0.note.id` for later reference.
   - Step 2: `note archive $0.note.id[:8] --player tester` — archive using an 8-char prefix.
     - Assert JSON `action == "archived"`, `note.archived_at` non-empty.

   *Harness note: if the `$N.field[:8]` slice syntax does not yet exist in the harness, use `$0.note.id` (full UUID) for the archive step in the JSON scenario and add a separate scenario for prefix archive that seeds a UUID with a known prefix via a service-level setup step. Prefer extending the harness slice syntax if the change is small; otherwise use the full UUID and keep prefix-archive coverage in a dedicated Go-level test in `tests/e2e/note_prefix_test.go`.*

2. **Add with @-file body**
   - Setup: write a small markdown file to the test temp dir (use `t.TempDir()` + `os.WriteFile`).
   - Step: `note add @./body.md project=_default --player tester` with `cwd` set to the temp dir.
   - Assert JSON `note.body` equals the file's contents.

3. **Add with stdin**
   - Step: `note add @- project=_default --player tester` with stdin set to `"stdin body\n"`.
   - Assert JSON `note.body == "stdin body"` (trailing newline preservation follows existing `expandRefs` behavior; match whatever `expand_test.go` asserts).

4. **Missing `--player` rejects**
   - Step: `note add "body" project=_default`
   - Assert command fails, stderr contains `--player flag is required`.

5. **Empty body rejects**
   - Step: `note add "   " project=_default --player tester`
   - Assert command fails, stderr contains `body must not be empty`.

6. **Unknown field rejects**
   - Step: `note add "body" bogus=1 project=_default --player tester`
   - Assert command fails, stderr contains `unknown field "bogus"` and the hint about `meta.`.

7. **Archive unknown id rejects**
   - Step: `note archive 00000000 --player tester` (no such prefix).
   - Assert command fails, stderr contains `no note matches`.

8. **Archive non-author rejects**
   - Create a note as `tester`, register a second player `intruder`, try `note archive $0.note.id --player intruder`.
   - Assert command fails, error bubbles up from `domain.ErrForbidden`.

**Acceptance:** `go test -v ./tests/e2e -run TestNoteAddArchive` passes in both JSON and text modes. `make test` passes.

---

## User-visible behavior preserved

- All pre-existing commands (`task`, `project`, `workflow`, `tag`, `player register`, `config`, `completion`, `mcp serve`) continue to work. The `tui.New` signature change is localized to the `cmd/tusk` entrypoint.
- Phase 2's `tusk player modify` remains functional (if Phase 2 has already landed).
- `tusk player register` output is unchanged beyond what Phase 2 already did.

## Bridge code introduced

None functional — but note that `tusk note list` is deliberately not registered. That is not bridge code; it is scope deferral, and Phase 4 adds it. If an implementer is tempted to register a stub `list` handler that prints "not implemented", do not — leaving cobra's default "unknown command" message keeps behavior honest.

## Changes Introduced

**New files:**
- `internal/tui/note.go` — command builder, two handlers, rendering helpers
- `tests/e2e/note_test.go` — e2e scenarios

**Modified files:**
- `internal/tui/app.go` — `App.noteSvc` field, `New` parameter, `buildNoteCmd` registration
- `cmd/tusk/main.go` — pass `client.Notes` into `tui.New`
- `service/note.go` — added `GetByID` and `FindByIDPrefix` pass-throughs
- `service/note_test.go` — added unit tests for the new pass-throughs

**New interfaces / methods:**
- `service.NoteService.GetByID(ctx, id uuid.UUID) (*domain.Note, error)`
- `service.NoteService.FindByIDPrefix(ctx, prefix string) ([]*domain.Note, error)`
- `tui.App.buildNoteCmd()`, `runNoteAdd`, `runNoteArchive`, `renderNoteResult`, `renderNoteArchivedBare`, `resolveNoteID`

**Dependencies / migrations / env vars:** none. Glamour is imported only in Phase 4.

**Bridge code removal targets:** none in this phase.
