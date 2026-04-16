# Phase 4: `tusk note list` + Milestone Close

**Initiative:** Note CLI (v0.12)
**Prerequisites:** Phase 1 (for `filter.ParseRelativeDuration`) and Phase 3 (for `tui.App.noteSvc`, `buildNoteCmd`, `renderNoteResult`, and the e2e test file). Must not run before Phase 3 lands.
**Design spec:** `docs/plans/v0.12-note-cli/design.md`

---

## Goal

Add the read side of the `tusk note` subcommand: `tusk note list` with the full flag set documented in `ROADMAP.md` v0.12. After this phase, users and agents can inspect notes with project / task / player / window / since / archived filters, and the v0.12 Note CLI + Player window size preference roadmap stories are complete.

Final shippable CLI surface:

```
tusk note add ...        (from Phase 3)
tusk note archive ...    (from Phase 3)
tusk note list [project=<name>] [--task <short_id>]
               [--player <id>|--all-players]
               [--window <N>] [--since <7d|2w|24h|30m>]
               [--archived]
```

## Inherits From

- **Phase 3** — `internal/tui/note.go` exists with `buildNoteCmd`, `runNoteAdd`, `runNoteArchive`, `renderNoteResult`, `toNoteJSON`, and `resolveNoteID`. The `App` struct holds `noteSvc`. `cmd/tusk/main.go` passes it in. `tests/e2e/note_test.go` contains `TestNoteAddArchive`.
- **Phase 1** — `filter.ParseRelativeDuration(string) (time.Duration, error)` is available.
- **Base codebase** — `service.NoteService.List(ctx, params)` with `NoteListParams{ProjectID, PlayerID, CallerPlayerID, TaskID, Since, IncludeArchived, WindowOverride}` is fully wired and returns newest-first.

## Tasks

### Task 1: Register `list` subcommand and wire flags

**File:** `internal/tui/note.go`

Extend `buildNoteCmd` to add `listCmd`. Insert after `archiveCmd` is constructed, before the final `noteCmd.AddCommand` calls.

```go
listCmd := &cobra.Command{
    Use:   "list [project=<name>]",
    Short: "List notes within the trailing window",
    Long: `List notes in a project, newest first, respecting the trailing window.

By default shows the caller's own notes only. --all-players shows every
player's notes within the same window; --player <id> scopes to a specific
player. --task <short_id> restricts to notes attached to that task.

The window size is resolved through the chain:
  --window flag → player DB setting → project config → global config → 20`,
    Args: cobra.ArbitraryArgs,
    RunE: a.runNoteList,
}
listCmd.Flags().String("task", "", "show notes attached to a task by short ID")
listCmd.Flags().Bool("all-players", false, "show notes from every player")
listCmd.Flags().String("filter-player", "", "show notes from a specific player (alias: --player-filter)")
listCmd.Flags().Int("window", 0, "override trailing window size (>0 to override)")
listCmd.Flags().String("since", "", "only show notes newer than this relative duration (e.g. 7d, 24h)")
listCmd.Flags().Bool("archived", false, "include archived notes")

noteCmd.AddCommand(listCmd)
```

**Flag naming — caller identity vs. list filter.**

`tusk` already defines a persistent root flag `--player` on `app.go:131` that carries caller identity (`a.playerID`). Redefining `--player` on `note list` would shadow the persistent flag. To keep the persistent flag intact while honoring the PRODUCT and ROADMAP spelling, this phase introduces **two** mechanisms for the list-target filter:

- A local flag `--filter-player <id>` on `listCmd`.
- An inline token `player=<id>` parsed off the positional args (i.e., `tusk note list project=backend player=agent-1 --player caller`).

Either one fills the list-target filter. The persistent `--player` remains caller identity. This is the only approach this phase implements — do not attempt an alternative.

Document the two spellings in the command's `Long` text so users reading `--help` see both. Example long-description text:

```
Filter by player using either:
  --filter-player <id>     flag form
  player=<id>              inline token (symmetric with project=<name>)

Use --all-players to show every player's notes; it cannot be combined with
either filter form above.
```

**Acceptance:** `go build ./internal/tui/...` passes. `tusk note list --help` prints all documented flags.

---

### Task 2: Implement `runNoteList`

**File:** `internal/tui/note.go`

```go
func (a *App) runNoteList(cmd *cobra.Command, args []string) error {
    ctx := cmd.Context()

    if a.playerID == "" {
        return fmt.Errorf("--player flag is required for note list (caller identity)")
    }
    if err := a.ensurePlayer(ctx); err != nil {
        return err
    }

    fs, parseErrs := filter.Parse(strings.Join(args, " "))
    if len(parseErrs) > 0 {
        return fmt.Errorf("%s", filter.FormatErrors(parseErrs))
    }

    // Project (default "_default").
    projectName := defaultProjectName
    if f, ok := fs.GetField("project"); ok {
        if f.Value == "" {
            return fmt.Errorf("project value must not be empty")
        }
        projectName = f.Value
    }
    project, err := a.projectSvc.GetByName(ctx, projectName)
    if err != nil {
        return fmt.Errorf("resolving project %q: %w", projectName, err)
    }

    // Filter player resolution — see Task 1 for the --filter-player / player= arg choice.
    allPlayers, _ := cmd.Flags().GetBool("all-players")
    filterPlayer, _ := cmd.Flags().GetString("filter-player")
    if f, ok := fs.GetField("player"); ok {
        if filterPlayer != "" {
            return fmt.Errorf("player filter set via both --filter-player and player=; choose one")
        }
        filterPlayer = f.Value
    }
    if allPlayers && filterPlayer != "" {
        return fmt.Errorf("--all-players cannot be combined with a specific player filter")
    }

    // Reject any bare key=value that is neither "project" nor "player".
    for _, f := range fs.Fields {
        switch f.Key {
        case "project", "player":
            continue
        default:
            return fmt.Errorf("unknown field %q on note list", f.Key)
        }
    }

    // Default scope: own notes only.
    targetPlayer := a.playerID
    if allPlayers {
        targetPlayer = ""
    } else if filterPlayer != "" {
        targetPlayer = filterPlayer
    }

    // Task filter.
    var taskID *uuid.UUID
    if taskShort, _ := cmd.Flags().GetString("task"); taskShort != "" {
        task, err := a.taskSvc.GetByShortID(ctx, taskShort)
        if err != nil {
            return fmt.Errorf("resolving task %q: %w", taskShort, err)
        }
        id := task.ID
        taskID = &id
    }

    // Window override.
    var windowOverride *int
    if w, _ := cmd.Flags().GetInt("window"); w > 0 {
        windowOverride = &w
    } else if w < 0 {
        return fmt.Errorf("--window must be positive, got %d", w)
    }

    // Since filter.
    var since *time.Time
    if sinceStr, _ := cmd.Flags().GetString("since"); sinceStr != "" {
        d, err := filter.ParseRelativeDuration(sinceStr)
        if err != nil {
            return fmt.Errorf("parsing --since: %w", err)
        }
        t := time.Now().UTC().Add(-d)
        since = &t
    }

    includeArchived, _ := cmd.Flags().GetBool("archived")

    notes, err := a.noteSvc.List(ctx, service.NoteListParams{
        ProjectID:       project.ID,
        PlayerID:        targetPlayer,
        CallerPlayerID:  a.playerID,
        TaskID:          taskID,
        Since:           since,
        IncludeArchived: includeArchived,
        WindowOverride:  windowOverride,
    })
    if err != nil {
        return fmt.Errorf("listing notes: %w", err)
    }

    return a.renderNoteList(cmd, notes)
}
```

**Acceptance:** `go build ./internal/tui/...` passes.

---

### Task 3: Implement list rendering with glamour body

**File:** `internal/tui/note.go`

Add list rendering that mirrors task list output conventions.

```go
func (a *App) renderNoteList(cmd *cobra.Command, notes []*domain.Note) error {
    w := cmd.OutOrStdout()
    if a.format == "json" {
        enc := json.NewEncoder(w)
        enc.SetIndent("", "  ")
        jsonItems := make([]noteJSON, 0, len(notes))
        for _, n := range notes {
            jsonItems = append(jsonItems, toNoteJSON(n))
        }
        return enc.Encode(jsonItems)
    }

    if len(notes) == 0 {
        _, err := fmt.Fprintln(w, "No notes.")
        return err
    }

    bodyRenderer := a.noteBodyRenderer()
    for i, n := range notes {
        if i > 0 {
            if _, err := fmt.Fprintln(w); err != nil {
                return err
            }
        }
        if err := a.renderNoteEntry(w, n, bodyRenderer); err != nil {
            return err
        }
    }
    return nil
}

func (a *App) renderNoteEntry(w io.Writer, n *domain.Note, body func(string) string) error {
    shortID := n.ID.String()[:8]
    taskPart := ""
    if n.TaskID != nil {
        taskPart = fmt.Sprintf("  task=%s", n.TaskID.String()[:8])
    }
    archivedPart := ""
    if n.ArchivedAt != nil {
        archivedPart = "  [archived]"
    }
    header := fmt.Sprintf("● %s  %s  %s%s%s\n",
        shortID,
        n.PlayerID,
        n.CreatedAt.Local().Format("2006-01-02 15:04"),
        taskPart,
        archivedPart,
    )
    if _, err := fmt.Fprint(w, header); err != nil {
        return err
    }

    rendered := body(n.Body)
    // Indent each line of the body by two spaces.
    for _, line := range strings.Split(strings.TrimRight(rendered, "\n"), "\n") {
        if _, err := fmt.Fprintf(w, "  %s\n", line); err != nil {
            return err
        }
    }

    if len(n.Metadata) > 0 {
        keys := make([]string, 0, len(n.Metadata))
        for k := range n.Metadata {
            keys = append(keys, k)
        }
        sort.Strings(keys)
        parts := make([]string, 0, len(keys))
        for _, k := range keys {
            parts = append(parts, fmt.Sprintf("meta.%s=%v", k, n.Metadata[k]))
        }
        if _, err := fmt.Fprintf(w, "  %s\n", strings.Join(parts, "  ")); err != nil {
            return err
        }
    }
    return nil
}

// noteBodyRenderer returns a function that renders a note body as styled
// markdown if color is enabled, or returns the raw body otherwise.
func (a *App) noteBodyRenderer() func(string) string {
    if !a.colorEnabled() {
        return func(s string) string { return s }
    }
    r, err := glamour.NewTermRenderer(
        glamour.WithAutoStyle(),
        glamour.WithWordWrap(100),
    )
    if err != nil {
        return func(s string) string { return s }
    }
    return func(s string) string {
        out, err := r.Render(s)
        if err != nil {
            return s
        }
        return out
    }
}
```

**Imports to add to `internal/tui/note.go`:**

- `io`
- `sort`
- `github.com/germanamz/tusk/service` (for `service.NoteListParams` in Task 2)
- `charm.land/glamour/v2` (imported as `glamour`)

If the existing `go.mod` pins glamour at a version that uses a different import path (`github.com/charmbracelet/glamour/...`), use whatever path is already on disk — check `go.mod` and `go.sum` before importing. From the codebase scan, `charm.land/glamour/v2` is the current pin.

**Acceptance:** `go build ./internal/tui/...` passes. Manual smoke: `tusk note list` with a populated project prints notes with headers, two-space indented bodies, and metadata lines.

---

### Task 4: E2E scenarios for `note list`

**File:** `tests/e2e/note_test.go` (append to the file created in Phase 3)

Add a new function `TestNoteList` containing these scenarios:

1. **Own notes by default**
   - Setup: create two notes as `alice`, one as `bob`.
   - Step: `note list project=_default --player alice` — assert JSON length 2, every entry has `player_id == "alice"`.

2. **`--all-players` shows every note**
   - Same setup as (1).
   - Step: `note list project=_default --all-players --player alice` — assert JSON length 3.

3. **`player=` inline filter**
   - Same setup.
   - Step: `note list project=_default player=bob --player alice` — assert JSON length 1, entry `player_id == "bob"`.

4. **`--filter-player` flag filter**
   - Same setup.
   - Step: `note list project=_default --filter-player bob --player alice` — assert JSON length 1, entry `player_id == "bob"`.

5. **`--all-players` + explicit filter rejects**
   - Step: `note list project=_default --all-players --filter-player bob --player alice` — assert failure, stderr contains `cannot be combined`.

6. **`--window` override**
   - Setup: create 5 notes as `alice`.
   - Step: `note list project=_default --window 2 --player alice` — assert JSON length 2, newest first (compare `created_at` descending).

7. **`--since` filter**
   - Setup: create two notes as `alice`. After the harness reports both `id` values, open the test's SQLite file directly (`database/sql` with the `modernc.org/sqlite` driver already vendored in `go.mod`) and execute `UPDATE notes SET created_at = ? WHERE id = ?` to backdate the first note to 48h ago. The test harness exposes the DB path through its internal `dbPath` field — add a small exported helper on the test env if one is not already present (`func (e *Env) DBPath() string { return e.dbPath }`). No sleeps — relying on wall-clock timing is forbidden.
   - Step: `note list project=_default --since 24h --player alice` — assert JSON length 1, equal to the recent note.

8. **`--archived` toggles archived visibility**
   - Setup: create a note, archive it.
   - Step 1: `note list project=_default --player alice` — assert length 0.
   - Step 2: `note list project=_default --archived --player alice` — assert length 1, entry `archived_at` non-null.

9. **`--task` scopes to a task**
   - Setup: create task `T`, then two notes — one with `--task T`, one without.
   - Step: `note list project=_default --task <T.short_id> --player alice` — assert JSON length 1, entry `task_id == T.id`.

10. **Unknown bare field rejects**
    - Step: `note list project=_default bogus=1 --player alice` — assert failure, stderr contains `unknown field "bogus"`.

11. **Invalid `--since` rejects**
    - Step: `note list project=_default --since bogus --player alice` — assert failure, stderr contains `parsing --since`.

Text-mode assertions should mirror the JSON ones where reasonable (e.g. count matches substring `●` occurrences). Don't over-specify glamour-rendered bytes; assert substring presence of the body's plain text instead.

**Acceptance:** `go test -v ./tests/e2e -run TestNoteList` passes. `make test` passes. `make test-race` passes.

---

### Task 5: Tick ROADMAP stories for the v0.12 Note CLI initiative

**File:** `ROADMAP.md`

Locate the `## v0.12 — Trailing Window Notes` section (currently around line 895). Flip the three incomplete stories to `[x]`:

- `### Initiative: Note CLI` → `Story: Note write commands` — all subitems to `[x]`
- `### Initiative: Note CLI` → `Story: Note read commands` — all subitems to `[x]`
- `### Initiative: Note CLI` → `Story: Player window size preference` — all subitems to `[x]`

Do **not** tick the `### Initiative: Note MCP Tools` or `### Initiative: MCP Field Restrictions` sections — those are separate initiatives in the same milestone, outside the Note CLI scope.

Do **not** create `docs/status/v0.12-status.md` or `docs/releases/v0.12.md` in this phase. Per project feedback memory, status and release docs are milestone-close deliverables, not per-initiative ones.

**Acceptance:** `rg '^- \[ \] ' ROADMAP.md | grep -i 'Note CLI'` returns nothing. `rg '^- \[ \] ' ROADMAP.md | grep -i 'window size preference'` returns nothing.

---

## User-visible behavior preserved

- Phase 3's `tusk note add` and `tusk note archive` continue to work identically.
- Phase 2's `tusk player modify note-window-size=<N>` continues to work, and its effect is now observable: when a player has a non-nil `NoteWindowSize`, calls to `tusk note list --player <caller>` from that player use the override (the service resolves this; the CLI just threads `CallerPlayerID`).
- All other commands untouched.

## Bridge code introduced

None.

## Changes Introduced

**Modified files:**
- `internal/tui/note.go` — added `listCmd` registration in `buildNoteCmd`; added `runNoteList`, `renderNoteList`, `renderNoteEntry`, `noteBodyRenderer`; imports expanded to include `io`, `sort`, and `charm.land/glamour/v2`.
- `tests/e2e/note_test.go` — added `TestNoteList`.
- `ROADMAP.md` — ticked Note CLI + Player window size preference stories.

**New interfaces / methods:**
- `tui.App.runNoteList`, `renderNoteList`, `renderNoteEntry`, `noteBodyRenderer` (all package-private).

**Dependencies / migrations / env vars:**
- No new direct dependency — `charm.land/glamour/v2` is already in `go.mod`.

**Bridge code removal targets:** none.

---

## Milestone Close Check

After Phase 4 merges and CI is green, the v0.12 Note CLI initiative is complete. Three incomplete initiatives remain in the v0.12 milestone:

- Note MCP Tools
- MCP Field Restrictions

Those are separate initiatives with their own phase plans, drafted under different planning sessions. When all v0.12 initiatives land, the planning agent for the final initiative is responsible for creating `docs/status/v0.12-status.md` and `docs/releases/v0.12.md`.
