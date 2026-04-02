# Basic CLI Design

**Date:** 2026-04-02
**Scope:** v0.1 roadmap item — `add`, `list`, `info`, `modify`, `start`, `done`, `delete` commands
**Status:** Approved

---

## Decisions

| Decision              | Choice                                                        |
| --------------------- | ------------------------------------------------------------- |
| Output style          | TaskWarrior-style tabular + `--format=json`                   |
| DB path resolution    | `--db` flag > `TUSK_DB` env > `~/.local/share/tusk/tusk.db`  |
| Arg syntax            | Key:value positional + `+tag`/`-tag`, no per-field flags      |
| Version handling      | Auto-fetch for all CLI commands (user never sees versions)    |
| Default list filter   | `pending` + `active`                                          |
| Architecture          | Cobra in `cmd/tusk/`, logic in `internal/tui/`                |
| Tags in `add`/`modify`| Error "tags not yet supported" (next roadmap item)            |
| Project filter        | Resolve name via existing `projectRepo.GetByName()`           |
| Sort order            | DB order (`created_at`) for v0.1; urgency in v0.4             |

---

## Package Structure

### `cmd/tusk/main.go` — Entry point

Responsibilities:
- Parse root flags (`--db`, `--format`)
- Resolve DB path: `--db` flag > `TUSK_DB` env > `~/.local/share/tusk/tusk.db`
- `os.MkdirAll` on parent directory
- Open `sqlite.NewStore(path)` (WAL mode, migrations)
- Construct repos: TaskRepo, ProjectRepo, WorkflowRepo, AnnotationRepo
- Construct services: WorkflowService > TaskService
- Construct `tui.App`, call `app.Run(os.Args[1:])`

Wiring sequence:
```
parse flags -> resolve DB path -> sqlite.NewStore(path)
-> construct repos (TaskRepo, ProjectRepo, WorkflowRepo, AnnotationRepo)
-> construct WorkflowService -> construct TaskService
-> construct tui.App -> app.Run(os.Args[1:])
```

No config file, no daemon. Single binary, runs the command, exits.

### `internal/tui/app.go` — App struct and Cobra tree

```go
type App struct {
    taskSvc    *service.TaskService
    projectRepo repository.ProjectRepository
    root       *cobra.Command
    format     string // "text" or "json"
}

func New(taskSvc *service.TaskService, projectRepo repository.ProjectRepository) *App
func (a *App) Run(args []string) error
```

`New()` builds the Cobra command tree:
- Root command `tusk` with persistent `--format` flag
- Subcommands: `add`, `list`, `info`, `modify`, `start`, `done`, `delete`
- Each subcommand's `RunE` calls a method in `commands.go`

### `internal/tui/commands.go` — Command implementations

One method per command on the `App` struct:

- `(a *App) runAdd(cmd *cobra.Command, args []string) error`
- `(a *App) runList(cmd *cobra.Command, args []string) error`
- `(a *App) runInfo(cmd *cobra.Command, args []string) error`
- `(a *App) runModify(cmd *cobra.Command, args []string) error`
- `(a *App) runStart(cmd *cobra.Command, args []string) error`
- `(a *App) runDone(cmd *cobra.Command, args []string) error`
- `(a *App) runDelete(cmd *cobra.Command, args []string) error`

Each method: parse args > call service > render output.

### `internal/tui/filter.go` — Arg parsing

```go
type ParsedArgs struct {
    Title    string            // non-key:value, non-tag args joined with spaces
    Fields   map[string]string // key:value pairs
    Tags     []string          // +tag inclusions
    ExclTags []string          // -tag exclusions
}

func parseArgs(args []string) ParsedArgs
```

Rules:
- `key:value` -> `Fields` map
- `+word` -> `Tags`
- `-word` -> `ExclTags`
- Everything else joined as `Title` (for `add`) or first positional is short ID (for `modify`, `info`, `start`, `done`, `delete`)
- **Important distinction:** `add` uses positional text as the title. All other commands use the first positional arg as a short ID and require `title:` key:value syntax to set/change the title.

Field converters:
- `parsePriority(s string) (int, error)` — "0"-"4" or "none"/"low"/"medium"/"high"/"urgent"
- `parseDate(s string) (*time.Time, error)` — RFC 3339 or relative: "today", "tomorrow", "monday", etc.
- `buildTaskFilter(p ParsedArgs, projectRepo repository.ProjectRepository) (domain.TaskFilter, error)` — converts ParsedArgs to `domain.TaskFilter`

### `internal/tui/render.go` — Output formatting

Format flag: persistent `--format` on root command, values: `text` (default), `json`.

**Text — `list`:**
```
ID       Status   Pri  Age  Title
a3f8b2c1 active   H    3d   Implement auth middleware
b7c9d4e2 pending  M    1d   Write tests for auth
```

- Fixed columns: ID (8), Status (9), Pri (4), Age (5), Title (remaining)
- Priority: `-` (none), `L`, `M`, `H`, `U`
- Age: `2m` (minutes), `3h`, `4d`, `2w`, `3mo`, `1y`
- No output + exit 0 if no tasks match

**Text — `info`:**
```
ID:          a3f8b2c1
Title:       Implement auth middleware
Status:      active
Priority:    high
Project:     backend
Created:     2026-04-01 10:30:00
Modified:    2026-04-02 08:15:00
Due:         2026-04-10
Version:     3

Annotations:
  2026-04-01 11:00 - Blocked by upstream API changes
  2026-04-02 08:15 - Unblocked, API deployed
```

Key-value pairs, left-aligned labels. Nullable fields omitted when empty. Annotations listed chronologically.

**Text — mutations (`add`, `modify`, `start`, `done`, `delete`):**
```
Created task a3f8b2c1
Modified task a3f8b2c1
Started task a3f8b2c1
Completed task a3f8b2c1
Deleted task a3f8b2c1
```

**JSON output (`--format=json`):**
All commands emit task(s) as JSON. `list` emits an array, single-task commands emit an object. Fields use snake_case matching the domain model. Includes `version`.

Key functions:
```go
func (a *App) renderTaskList(tasks []*domain.Task, format string) error
func (a *App) renderTaskInfo(task *domain.Task, annotations []*domain.Annotation, format string) error
func (a *App) renderMutationResult(action string, task *domain.Task, format string) error
```

---

## Command Behavior

### `tusk add <title> [key:value...] [+tag...]`

- Title required (all non-key:value, non-tag args joined with spaces)
- Supported fields: `project:`, `priority:`, `parent:`, `due:`, `status:`
- Tags: parsed but error with "tags not yet supported" (next roadmap item wires them in)
- Calls `TaskService.Create`, renders mutation result
- `project:` accepts a project name, resolved via `projectRepo.GetByName()`

### `tusk list [filters...]`

- Default filter: `status:pending,active`
- Supported filters: `project:<name>`, `status:<s1,s2>`, `priority:<min>..<max>`, `parent:<short_id>`
- `project:<name>` resolved to UUID via `projectRepo.GetByName()`
- Tags in filters (`+tag`, `-tag`) error with "tag filtering not yet supported"
- Sort: DB order (`created_at`) — urgency sorting comes in v0.4
- No output + exit 0 when no tasks match

### `tusk info <short_id>`

- Required: one positional arg (short ID)
- Calls `GetByShortID`, then `GetAnnotations`
- Renders full detail view

### `tusk modify <short_id> [key:value...]`

- Required: first positional arg is short ID
- Auto-fetches current version (user never passes version)
- Supported fields: `title:`, `priority:`, `status:`, `project:`, `parent:`, `due:`
- Builds `domain.TaskUpdate` from parsed fields, calls `TaskService.Update`
- Tags error with "tags not yet supported"

### `tusk start <short_id>`

- Auto-fetches version, calls `TaskService.Start`
- Renders: `Started task <short_id>`

### `tusk done <short_id>`

- Auto-fetches version, calls `TaskService.Complete`
- Renders: `Completed task <short_id>`

### `tusk delete <short_id>`

- Auto-fetches version, calls `TaskService.Delete`
- Renders: `Deleted task <short_id>`

---

## Error Handling

All errors go to stderr, non-zero exit code. Typed domain errors get friendly messages:

| Domain Error            | CLI Message                                              |
| ----------------------- | -------------------------------------------------------- |
| `ErrNotFound`           | `Task not found: <short_id>`                             |
| `ErrConflict`           | `Version conflict - task was modified by another process` |
| `ErrInvalidTransition`  | `Transition not allowed: <from> -> <to>`                 |
| Validation errors       | Pass through as-is (e.g., "title must not be empty")     |

---

## Roadmap Update

v0.1 order becomes:

1. ~~Domain types and repository interfaces~~ (done)
2. ~~SQLite implementation with migrations~~ (done)
3. ~~TaskService with CRUD, workflow validation, optimistic locking~~ (done)
4. **Basic CLI: `add`, `list`, `info`, `modify`, `start`, `done`, `delete`** (this spec)
5. **Tag support: TagService, wire into CLI `add`/`modify`/`list`** (new item)
6. Filter syntax parser

---

## Out of Scope

- Config file parsing (`config.toml`) — deferred until v0.4 when urgency weights need it
- Interactive TUI (bubbletea) — v0.4+
- `--sort` flag — v0.4 with urgency engine
- `tree` command — v0.2 with hierarchy work
- `annotate` CLI command — could be added alongside this work but not in scope
- Relation commands (`link`, `unlink`) — v0.2
- `undo` — v0.4
