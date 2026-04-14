# Phase 3 — ProjectService Writes + Caller Migration

## Inherits From

Phases 1 and 2 are complete. The implementer should expect:

- `ProjectRepository` interface has `Create`, `Update`, `Delete(id, expectedVersion)`, `CountProjectsByWorkflow`.
- `cmd/tusk/main.go` and `client.go` construct `sqlite.NewProjectRepo(db)` directly and pass it to `service.NewWorkflowService` and (wherever it is used) any project-consuming code.
- `sqlite.SyncConfigToDB(ctx, *config.Config, …)` runs at startup and upserts config-defined projects into SQLite. As a result, any project defined in the TOML `[projects.<name>]` block already exists as a SQLite row by the time the services are constructed.
- `config.CreateProject` / `ModifyProject` / `DeleteProject` still exist in `config/project.go` and are still called by `internal/tui/project.go` and `internal/mcp/project_handlers.go`.

## Objective

Give `ProjectService` full write capability (`Create`, `Modify`, `Delete`) backed by the repository, including the service-level delete guards (`_default` rejection, task-reference rejection, `force` bypass) and optimistic locking. Rewire every caller — CLI and MCP — to use the service. Delete `config.CreateProject`, `config.ModifyProject`, `config.DeleteProject` and any now-dead helpers in `config/project.go`.

After this phase, every project mutation is written to SQLite through `ProjectService` and **no longer touches the TOML file**. The `[projects.<name>]` sections in the TOML continue to be read at startup and seeded into SQLite via `SyncConfigToDB`, but they are no longer written back to — editing a project via CLI or MCP updates the DB only. This is the desired end state for this initiative; reconciling startup drift is the job of the Config Schema Trim initiative.

## Tasks

### Task 1 — Add `ProjectService` write methods

Edit `service/project.go`. Extend the constructor so the service has access to the dependencies it needs for guard enforcement and delta urgency resolution:

```go
func NewProjectService(
    projectRepo repository.ProjectRepository,
    taskCounter TaskCountByProject,
    defaults ProjectDefaults,
) *ProjectService
```

where `TaskCountByProject` is a small interface defined in the same file:

```go
type TaskCountByProject interface {
    CountByProject(ctx context.Context, projectID uuid.UUID) (int, error)
}
```

and `ProjectDefaults` is a plain value struct that carries the global urgency weights used as the baseline for `+urgency.xxx-weight=N` / `-urgency.xxx-weight=N` delta operations. Auto-complete and auto-revert have no global defaults in `config.Config` — they are per-project only (`ProjectSettingsConfig.AutoCompleteParent` / `AutoRevertParent` at `config/config.go:80–83`) — so `ProjectDefaults` carries urgency only:

```go
type ProjectDefaults struct {
    Urgency service.UrgencyWeights // same type already used by UrgencyEngine
}
```

The goal is to capture the global urgency baseline as a value the service owns, not to pull in the whole `*config.Config` struct. Extend the `ProjectService` struct to store `defaults ProjectDefaults` as a private field alongside the existing `projectRepo` and the new `taskCounter` field.

Implement three methods on `ProjectService`:

1. **`Create(ctx context.Context, input CreateProjectInput) (*domain.Project, error)`**
   - `CreateProjectInput` carries: `Name string`, `WorkflowID uuid.UUID` (resolved by the caller from a workflow name), `Settings domain.ProjectSettings` (urgency overrides, auto-complete/auto-revert).
   - Validates the name (non-empty, not `_default`, not already present via `GetByName`).
   - Builds a `*domain.Project` with a fresh UUID and `version = 1`, then calls `repo.Create`.
   - Returns the created project.

2. **`Modify(ctx context.Context, input ModifyProjectInput) (*domain.Project, error)`**
   - Caller passes the current version (optimistic locking).
   - Fetch via `GetByID` (or `GetByName`), validate that the fetched `version` equals the caller's expected version; if not, return `domain.ErrConflict`.
   - Apply mutations to fields allowed by the existing `config.ModifyProject` surface (workflow ID, urgency weight overrides, auto-complete trigger, auto-revert trigger).
   - Numeric delta resolution for `+urgency.xxx-weight=N` / `-urgency.xxx-weight=N` operations reads the effective baseline from `s.defaults.Urgency` (the `ProjectDefaults` stored at construction time) and writes the resolved absolute value into the project's settings. Bare `urgency.xxx-weight=N` sets the absolute value directly, bypassing the baseline.
   - Call `repo.Update`. Return the updated project.

3. **`Delete(ctx context.Context, id uuid.UUID, expectedVersion int64, force bool) error`**
   - Reject `_default` (UUID `00000000-...`) unless `force == true`. Reuse the same constant used today in `config/project.go`.
   - Call `taskCounter.CountByProject(ctx, id)`. If count > 0 and `force == false`, return a descriptive error (reuse the sentinel used today, or add `domain.ErrProjectHasTasks`).
   - Call `repo.Delete(ctx, id, expectedVersion)`. Optimistic-lock conflicts bubble as `domain.ErrConflict`.

All three methods must emit structured errors using `fmt.Errorf("%w: …", domain.ErrXxx, …)` style so callers can `errors.Is` them.

### Task 2 — Add `CountByProject` to `TaskRepository` (if missing)

Check `repository/task.go` for a `CountByProject(ctx, projectID uuid.UUID) (int, error)` method. If it does not exist, add it to the interface and implement it in `sqlite/task.go` using a simple `SELECT COUNT(*) FROM tasks WHERE project_id = ?` query. The existing `config.DeleteProject` uses a `TaskRefChecker` callback injected from `internal/tui/project.go:116` and `internal/mcp/project_handlers.go:255` — those call sites are rewired in Task 4, and the new counter replaces the callback.

If `sqlite.TaskRepo` already has such a method under a different name, reuse it by adding the interface method and pointing the existing implementation at it — do not create a duplicate.

### Task 3 — Update DI wiring

Edit `cmd/tusk/main.go` and `client.go`:

- After constructing `projectRepo := sqlite.NewProjectRepo(db)` and `taskRepo := sqlite.NewTaskRepo(db)` (or wherever the task repo is built today — check the `RepoBundle`), build the project service as:

  ```go
  projectSvc := service.NewProjectService(projectRepo, taskRepo, projectDefaults)
  ```

  where `projectDefaults` is a `service.ProjectDefaults` value whose `Urgency` field is built from `cfg.Urgency` (map the nine `PriorityWeight` / `DueWeight` / … fields on `config.UrgencyConfig` to the equivalent fields on `service.UrgencyWeights`, same mapping already used when constructing `UrgencyEngine` in both `cmd/tusk/main.go` and `client.go`). `taskRepo` here is `sqlite.NewTaskRepo(db)`, which satisfies the `service.TaskCountByProject` interface via the `CountByProject` method added in Task 2.

- If there is no `projectSvc` variable today, add one. Inject it into any handler that needs it (CLI root, MCP server — see Task 4).

### Task 4 — Rewire `internal/tui/project.go`

Edit `internal/tui/project.go`. Replace every call to `config.CreateProject`, `config.ModifyProject`, and `config.DeleteProject` with calls to the corresponding `projectSvc` method. Specifically:

- `tusk project create` handler → `projectSvc.Create(ctx, …)`. The inline-syntax parser already produces a struct containing `name`, `workflow=…`, and urgency/automation overrides — map those into `CreateProjectInput`. The `workflow=…` value is a **name**, not an ID — resolve it via `workflowSvc.GetByName(ctx, name)` before calling `Create`.
- `tusk project modify` handler → fetch via `projectSvc.GetByName`, feed the version into `ModifyProjectInput`, call `projectSvc.Modify`. Delta urgency operations (`+urgency.blocking-weight=2`) are resolved by the service using the injected defaults — the handler only needs to pass raw deltas through.
- `tusk project delete` handler → `projectSvc.Delete(ctx, id, version, force)`. The CLI `--force` flag maps to the `force` argument.

Remove the old `TaskRefChecker` plumbing from this file.

Ensure output formatting (text + JSON) still matches what `config.CreateProject` produced — the renderer probably prints the resulting project struct; confirm by running the relevant e2e scenarios.

### Task 5 — Rewire `internal/mcp/project_handlers.go`

Edit `internal/mcp/project_handlers.go`. Apply the same migration as Task 4:

- `tusk_project_create` tool → `projectSvc.Create`.
- `tusk_project_modify` tool → `projectSvc.Modify`. The tool already accepts a `version` parameter per the MCP optimistic-locking convention; thread it into `ModifyProjectInput.ExpectedVersion`.
- `tusk_project_delete` tool → `projectSvc.Delete` with `force` from the tool input.

Tool responses must continue to include the updated `version` so agents can chain calls. The MCP layer already has helpers for marshaling a `domain.Project` to the wire — reuse them.

The `configMu sync.Mutex` at `internal/mcp/server.go:32` stays in place for this phase — it is removed in Phase 5 once the config write path is fully gone.

### Task 6 — Delete `config.CreateProject` / `ModifyProject` / `DeleteProject`

Edit `config/project.go`:

- Delete the functions `CreateProject`, `ModifyProject`, `DeleteProject`, and `TaskRefChecker` (the callback type is unused after Task 4 / Task 5).
- Leave `ProjectConfig` struct, `config.ProjectFromConfig` helper (added in Phase 2), and any load-time validation paths in place — they still feed `SyncConfigToDB` at startup.
- Run `go build ./...` and delete any now-dead helpers flagged by the compiler (e.g., internal `writeProjectToConfig` helpers used only by the removed functions). Do not remove anything that is still referenced by the load path.

The `config.WriteConfig` helper at `config/write.go:74` may still be used by other paths (workflow writes in Phase 4, `config set` / `config init`) — leave it alone.

## User-Visible Behavior (Acceptance Criteria)

- `tusk project create <name> workflow=<wf>` creates a project row in SQLite. A subsequent `tusk project list` shows it. Creating twice with the same name returns a clear duplicate error.
- `tusk project modify <name> urgency.blocking-weight=15` updates the SQLite row. The new value is visible on the next `tusk project list`.
- `tusk project modify <name> +urgency.blocking-weight=2` applies the delta against the effective global weight.
- `tusk project delete <name>` rejects `_default` without `--force`, rejects projects with open tasks without `--force`, and succeeds otherwise. On version conflict, returns a `domain.ErrConflict`-wrapped error.
- `tusk project create` / `modify` / `delete` **no longer rewrite the TOML file** — the file is left untouched regardless of what happens in the DB. Users who edit the TOML and restart see their changes merged into the DB via `SyncConfigToDB` (existing behavior from Phase 2).
- MCP `tusk_project_*` tools behave the same and return a `version` field on success.
- `tusk workflow ...` commands and MCP tools are unchanged (see Phase 4).
- `tusk task create project=<name>` resolves the project from SQLite as before.
- `tusk undo` continues to function for task mutations (project-level undo is not part of this initiative).
- `go build`, `make test`, `make test-race`, `make test-e2e` all pass.

## Changes Introduced

**Modified files:**
- `service/project.go` — new `CreateProjectInput`, `ModifyProjectInput`, `ProjectDefaults`, `TaskCountByProject` types; `NewProjectService` signature extended; new `Create` / `Modify` / `Delete` methods.
- `repository/task.go` — possibly adds `CountByProject(ctx, uuid.UUID) (int, error)` to the interface.
- `sqlite/task.go` — possibly adds `CountByProject` implementation (if missing).
- `cmd/tusk/main.go` — builds `projectSvc` via the extended constructor.
- `client.go` — same.
- `internal/tui/project.go` — calls `projectSvc` instead of `config.Create/Modify/DeleteProject`; drops `TaskRefChecker` plumbing.
- `internal/mcp/project_handlers.go` — calls `projectSvc`; threads `version` through for optimistic locking.
- `config/project.go` — `CreateProject`, `ModifyProject`, `DeleteProject`, `TaskRefChecker` **deleted**.

**New files:** none (unless splitting `service/project.go` for readability is preferred — an optional `service/project_write.go` is acceptable).

**New interfaces:**
- `service.TaskCountByProject` (internal to the service package).

**Sentinel errors (if not already present):**
- `domain.ErrProjectHasTasks` — reject delete when tasks reference the project without `--force`.

**Bridge code:** none introduced. The Phase 1 `inmem` stubs and the Phase 2 `SyncConfigToDB` bridge are both still in place and are not touched in this phase.

**Schema migrations:** none.

**New environment variables / dependencies:** none.
