# Phase 2 — Schema trim, legacy hard error, sync removal

## Goal

Remove `[projects.*]` and `[workflows.*]` from the TOML config schema. Delete the `sqlite.SyncConfigToDB` seed path. Wire `cmd/tusk/main.go`, `client.go`, and the `sqlitetest` fixture to rely on migration-seeded DB state alone. Reject legacy config files that still carry the removed sections with a clear error at `config.Load` time.

After this phase, the TOML config file holds globals only: `[storage]`, `[urgency]`, `[tui]`, `[mcp]`. The database is the single source of truth for projects and workflows.

## Inherits From

Phase 1 left the following state in place:

- `tusk config show` already hydrates `[projects.*]` / `[workflows.*]` from the database via `internal/tui/config_render.go`. Its implementation uses fresh local view types (`configShowTOML`, `configShowJSON`, `projectJSON`, `workflowJSON`, etc.) that are independent of `config.ProjectConfig` / `config.WorkflowConfig`, so phase 2's schema trim does not require any changes to `runConfigShow` itself.
- `tusk config get` already routes `projects.*` / `workflows.*` keys to DB helpers.
- `tusk config set` and MCP `tusk_config_set` already reject `projects.*` / `workflows.*` writes with friendly errors.
- `config.Config.Workflows`, `config.Config.Projects`, `config.WorkflowConfig`, `config.ProjectConfig`, `config.ProjectSettingsConfig`, `config.ProjectUrgencyConfig`, `config.AutoCompleteParentConfig`, `config.AutoRevertParentConfig`, `config.WorkflowTransitionConfig`, `config.StatusConfig`, and the `Role*` constants still exist.
- `config/default.toml` still contains `[workflows.kanban.*]` and `[projects.default]` sections.
- `sqlite.SyncConfigToDB` still runs in both `cmd/tusk/main.go:78` and `client.go:153`. The MCP server still plumbs a `reloadHook` that re-invokes it.
- `sqlitetest.NewStore(t, cfg)` still takes a config and calls `SyncConfigToDB`.
- Migrations 003, 004, and 006 already seed the kanban workflow and the default project row. A fresh DB is fully populated without `SyncConfigToDB`.

## Prerequisites

Phase 1 must be merged. The phase 1 DB-hydration work is what makes it safe to remove the config schema types — once the renderer and get/set routing stop depending on `cfg.Workflows` / `cfg.Projects`, the types can go.

## Context the implementer must verify

Before starting, grep for external consumers of the types being deleted so none are missed. Expected call sites (from initial survey):

- `client.go:22-76` — `tusk.Config.Workflows`, `tusk.Config.Projects`, `defaultWorkflows`, `defaultProjects`, `validationCfg`, `SyncConfigToDB` call at line 153.
- `cmd/tusk/main.go:76-80, :150-152` — `projectRepo`/`workflowRepo` seed call, `reloadHook`.
- `sqlite/sync.go` — entire file to delete.
- `sqlite/sqlitetest/fixture.go` — `NewStore`, `KanbanConfig`, `KanbanWorkflow`.
- `config/config.go:19-140, :287-305` — types and `Validate` loops.
- `config/domain.go` — entire file to delete.
- `config/default.toml:53-107` — `[workflows.*]` and `[projects.*]` sections.
- `config/project.go` — `DefaultProjectID` constant; keep only if still used outside the removed code (grep first).
- `config/config_test.go`, `config/write_test.go` — existing tests over removed types.
- `service/*_test.go`, `internal/tui/commands_test.go`, `internal/mcp/*_test.go` — every caller of `sqlitetest.NewStore`.
- `internal/mcp/server.go:17-23, :36-71, :826-850` — `ConfigReloadHook` type, field, constructor arg, and invocation.
- `internal/tui/config.go:79` — phase 1 left `cfg.Workflows = nil` / `cfg.Projects = nil` assignments that must be deleted once the fields go away.

Role constants (`config.RoleInitial`, etc.) may still be referenced by `sqlite/sqlitetest/fixture.go` today. After fixture rewrite, grep again — if any other file still uses them, move the constants into `domain/` (where `domain.StatusRole` already lives) rather than deleting them.

## Tasks

### 1. Remove project/workflow types from the config package and add the legacy-section guard

In `config/config.go`:

- Delete `WorkflowTransitionConfig`, `StatusConfig`, `WorkflowConfig`, `AutoCompleteParentConfig`, `AutoRevertParentConfig`, `ProjectUrgencyConfig`, `ProjectSettingsConfig`, `ProjectConfig` struct definitions (lines 19-84 region).
- Delete the `Workflows` and `Projects` fields from `Config` (lines 99-100).
- Delete the `Role*` string constants (lines 30-38) if no other package imports them. If `sqlite/sqlitetest/fixture.go` or any production file still references them at this point, relocate the constants into `domain/errors.go` or a new `domain/roles.go` as `domain.Role*` values of type `domain.StatusRole`, and update the callers within this task. Do not leave the constants in `config`.
- In `Validate()`, delete the `for name, wfCfg := range c.Workflows` loop and the `for id, proj := range c.Projects` loop (lines 289-304). The function reduces to `return nil` for now; leave the function in place so callers don’t break.
- Add a legacy-section guard: before the Viper merge path in `Load()`, if `filePath != ""`, read the raw file with `os.ReadFile(filePath)` and decode into `map[string]any` via `toml.Unmarshal`. If the decoded map contains a `projects` key or a `workflows` key whose value is a map/table, return:

    ```go
    return nil, fmt.Errorf(
        "config file %s contains [%s.*] sections — projects and workflows are now managed in the database. "+
        "Remove the section(s) from the file and recreate the equivalent entries via `tusk project` / `tusk workflow`",
        filePath, sectionName,
    )
    ```

  Use `github.com/pelletier/go-toml/v2` (already imported in `config/write.go`). Apply the guard for both `WithExplicitFile` and walk-up hits; skip it when `filePath == ""` (embedded-defaults-only path).
- Do not attempt automatic migration. The error message is guidance only.

In `config/project.go`:

- If `DefaultProjectID` (the `"default"` constant) is still referenced after the type deletions (grep), leave it alone. Otherwise delete the file.

Delete `config/domain.go` entirely. Confirm no references remain: `grep -r 'config.WorkflowFromConfig\|config.ProjectFromConfig\|config.WorkflowID\|config.ProjectID' .`.

### 2. Trim `config/default.toml`

Delete lines `53-107` of `config/default.toml` — everything from the `# Workflows define...` comment block through the end of the `[projects.default]` / commented example section.

The file must end with the `[mcp]` section and its keys. Verify by opening the file after the edit and confirming no `[workflows` or `[projects` substrings remain.

### 3. Delete `sqlite.SyncConfigToDB` and remove its callers

- Delete `sqlite/sync.go` in its entirety (`SyncConfigToDB`, `isZeroProjectSettings`).
- In `cmd/tusk/main.go`:
  - Remove the `SyncConfigToDB` call at line 78 along with its `if err` branch.
  - Remove the `reloadHook := func(ctx ...) { return sqlite.SyncConfigToDB(...) }` block at lines 150-152.
  - Update the `tui.New(...)` call to no longer pass `reloadHook`. (Coordinate with the `tui.New` signature — see task 4.)
- In `client.go`:
  - Delete the `Workflows` and `Projects` fields from `tusk.Config`.
  - Delete `defaultWorkflows`, `defaultProjects`, and `defaultUrgency` stays.
  - Delete the `if len(cfg.Workflows) == 0 { cfg.Workflows = defaultWorkflows() }` and equivalent `Projects` branches in `NewClient`.
  - Delete the `validationCfg := config.Config{ Workflows: ..., Projects: ... }` block and its `Validate` call.
  - Delete the `syncCfg := &config.Config{...}` block and the `SyncConfigToDB` call at line 153.
  - Update the godoc on `tusk.Config` to drop the `Workflows` / `Projects` paragraphs.
- In `internal/mcp/server.go`:
  - Delete the `ConfigReloadHook` type (lines 17-23), the `reloadHook ConfigReloadHook` field (line 36), the constructor parameter (line 56), and the assignment in the constructor body (line 71).
  - In `reloadConfig` (lines 826-850), delete the `if s.reloadHook != nil { ... }` block. The urgency-engine reload stays as-is.
  - Update all internal callers of the constructor (including tests in `internal/mcp/server_test.go`, `internal/mcp/handlers_test.go`, `internal/mcp/config_handlers_test.go`) to stop passing the hook argument.
- Confirm `cmd/tusk/main.go` no longer imports `context` solely for `SyncConfigToDB` — adjust imports as needed.

### 4. Rewire `sqlitetest.NewStore` and update all callers

In `sqlite/sqlitetest/fixture.go`:

- Change `NewStore` to `NewStore(t testing.TB) (*sqlite.Store, *sqlite.ProjectRepo, *sqlite.WorkflowRepo)` — no `cfg` parameter. Migrations alone populate kanban and the default project.
- Delete `KanbanConfig`. Delete `KanbanWorkflow` (it produced a `config.WorkflowConfig`, which no longer exists).
- Add a new helper:

    ```go
    // SeedProject inserts a project with the given name bound to the kanban
    // workflow (uuid.Nil). Tests that need an extra project beyond the
    // migration-seeded default call this to avoid hand-rolling repo writes.
    func SeedProject(t testing.TB, repo *sqlite.ProjectRepo, name string) *domain.Project {
        t.Helper()
        p := &domain.Project{
            ID:         uuid.New(),
            Name:       name,
            WorkflowID: uuid.Nil,
            Version:    1,
            CreatedAt:  time.Now().UTC().Truncate(time.Millisecond),
            UpdatedAt:  time.Now().UTC().Truncate(time.Millisecond),
        }
        if err := repo.Create(context.Background(), p); err != nil {
            t.Fatalf("seed project %q: %v", name, err)
        }
        return p
    }
    ```

- Update every caller of `sqlitetest.NewStore` across the repo. From the initial survey:
  - `service/workflow_test.go`
  - `service/task_test.go`
  - `service/task_routing_test.go`
  - `service/task_claim_test.go`
  - `service/tag_test.go`
  - `service/relation_test.go`
  - `service/project_test.go`
  - `service/bundle_helpers_test.go`
  - `internal/tui/commands_test.go`
  - `internal/mcp/server_test.go`
  - `internal/mcp/handlers_test.go`
  - `internal/mcp/config_handlers_test.go`
  - `client_test.go` (if present)
  - Any other file that `grep` returns for `sqlitetest.NewStore` or `sqlitetest.KanbanConfig`.
- For each caller: drop the `cfg` argument. Where the old code passed `KanbanConfig("foo", "bar")` to create extra projects, replace with `SeedProject(t, projRepo, "foo")` / `SeedProject(t, projRepo, "bar")` after the `NewStore` call.
- For tests that need a non-kanban workflow, use `wfRepo.Create(...)` directly with a hand-built `*domain.Workflow` — no helper is added for this, the current test suite does not appear to exercise multi-workflow configurations via the fixture.

### 5. Run the build pipeline and fix residual references

Phase 1's `runConfigShow` uses fresh local view types that are independent of `config.ProjectConfig` / `config.WorkflowConfig`, so it requires no changes in this phase. However, other test files and utilities may still reference the deleted types or the old `sqlitetest.NewStore(t, cfg)` signature; they must all compile.

Run the full build pipeline:

```
make build
make vet
make lint
make test
make test-race
```

Fix any compilation or test failures introduced by the type/fixture changes. Compilation must be green before the phase is declared complete. It is expected that this task picks up residual references that tasks 1-4 did not anticipate; fix them in place and note them in the commit message.

## Acceptance criteria (user-visible behaviors after phase 2)

1. Fresh install without any config file: `tusk` runs, the global config gets auto-created from defaults, the database is initialized with the kanban workflow and default project via migrations, and `tusk task add "hello"` succeeds. The global config file contains no `[projects.*]` or `[workflows.*]` sections.
2. `tusk config show` still prints storage/urgency/tui/mcp plus DB-hydrated project and workflow sections. Output matches phase 1 shape.
3. `tusk config get projects.default.workflow` → `kanban`. `tusk config get workflows.kanban.statuses.pending.roles` → `[initial]`.
4. `tusk config set projects.foo.workflow kanban` and `tusk_config_set` with the same key still return the phase 1 rejection error.
5. A user running `tusk` with a legacy `tusk.toml` containing `[projects.foo]` or `[workflows.bar]` sees:

    ```
    Error: loading config: config file /path/to/tusk.toml contains [projects.*] sections — projects and workflows are now managed in the database. Remove the section(s) from the file and recreate the equivalent entries via `tusk project` / `tusk workflow`
    ```

   and exits non-zero. No sync occurs, no partial state is persisted.
6. `tusk project list`, `tusk project create`, `tusk project modify`, `tusk project delete`, `tusk workflow list`, `tusk workflow create`, `tusk workflow modify`, `tusk workflow delete` all continue to work exactly as today — the service layer, DI wiring, and command definitions are unchanged.
7. `tusk` as a Go library (via `tusk.NewClient`) still exposes `Tasks`, `Tags`, `Relations`, `Projects`, `Workflows`, `Players`. Programs passing an empty `tusk.Config{DBPath: ...}` still get a working client with the default project and kanban workflow. The `Workflows` and `Projects` fields of `tusk.Config` are gone — consumers that set them must be found and migrated, which this phase explicitly owns for in-repo call sites (task 3).
8. `make test` and `make test-race` are green.

## Changes Introduced

**Deleted files**

- `sqlite/sync.go`
- `config/domain.go`
- `config/project.go` — conditional on no remaining references.

**Modified files**

- `config/config.go` — type deletions, `Validate` trimmed, legacy-section guard added to `Load`.
- `config/default.toml` — `[workflows.*]` and `[projects.*]` sections removed.
- `cmd/tusk/main.go` — sync call and reload hook removed, `tui.New` arguments trimmed.
- `client.go` — `tusk.Config` trimmed, `defaultWorkflows` / `defaultProjects` removed, sync call removed.
- `internal/mcp/server.go` — `ConfigReloadHook` type and plumbing removed.
- `internal/mcp/server_test.go`, `handlers_test.go`, `config_handlers_test.go` — constructor calls updated.
- `sqlite/sqlitetest/fixture.go` — `NewStore` signature simplified, `KanbanConfig` / `KanbanWorkflow` removed, `SeedProject` added.
- `service/*_test.go`, `internal/tui/commands_test.go`, `internal/mcp/*_test.go`, and any other callers of `sqlitetest.NewStore` — signature updates, `SeedProject` adoption where needed.
- `internal/tui/app.go` and `tui.New` — if the signature drops the `reloadHook` parameter in task 3, update here too.

**Modified interfaces**

- `tusk.Config` (public): `Workflows` and `Projects` fields removed.
- `tui.New` / `internal/mcp/server.NewServer` (internal): `reloadHook` parameter removed.
- `sqlitetest.NewStore` (test-only): `cfg` parameter removed; `KanbanConfig` / `KanbanWorkflow` deleted; `SeedProject` added.
- `config.Config`: `Workflows` and `Projects` fields removed; all related struct types deleted.

**New environment variables**: none.

**Schema migrations**: none — migrations 003/004/006 already seed the needed rows.

**Added dependencies**: none.

**Bridge code**: none. This phase removes bridge code (the `SyncConfigToDB` seed path acted as the config↔DB bridge since the phase 5 service layer migration). No new bridge is introduced.

**Removed bridge code**

- `sqlite.SyncConfigToDB` (was a no-op for the default case since migrations 003/004 landed; fully removed here).
- `ConfigReloadHook` plumbing in `internal/mcp/server.go` (its only consumer was `SyncConfigToDB`).
