# Phase 2 — DI Pivot to SQLite Repos

## Inherits From

Phase 1 has been applied. The implementer should expect:

- `repository.ProjectRepository` and `repository.WorkflowRepository` include `Create` / `Update` / `Delete(id, expectedVersion)` and (on `ProjectRepository`) `CountProjectsByWorkflow`.
- `sqlite.(*ProjectRepo)` and `sqlite.(*WorkflowRepo)` satisfy those interfaces. `CountByWorkflow` has been renamed to `CountProjectsByWorkflow`.
- `inmem.(*ProjectRepository)` and `inmem.(*WorkflowRepository)` satisfy the interfaces via stubs that return `domain.ErrReadOnlyRepository` on any write call. These stubs are **bridge code scheduled for removal in Phase 5** — do not rely on them.
- All tests pass on `main` + Phase 1.

## Objective

Switch production wiring (`cmd/tusk/main.go` and the `Client` in `client.go`) so that `ProjectService` and `WorkflowService` receive SQLite-backed repositories directly. Refactor `sqlite.SyncConfigToDB` so that it reads its input from `*config.Config` rather than from a pair of repositories. After this phase, no production code path constructs an `inmem` project or workflow repository; the `inmem` package still exists only because Phase 3, Phase 4, and a handful of tests still reference it.

The service-side behavior is unchanged: both services still only call read methods on the repositories. The writes they will eventually perform are introduced in Phase 3 and Phase 4.

## Tasks

### Task 1 — Refactor `sqlite.SyncConfigToDB` signature

Edit `sqlite/sync.go`. Replace the current signature:

```go
func SyncConfigToDB(
    ctx context.Context,
    workflows repository.WorkflowRepository,
    projects repository.ProjectRepository,
    wfRepo *WorkflowRepo,
    projRepo *ProjectRepo,
) error
```

with:

```go
func SyncConfigToDB(
    ctx context.Context,
    cfg *config.Config,
    wfRepo *WorkflowRepo,
    projRepo *ProjectRepo,
) error
```

The new implementation iterates `cfg.Workflows` and `cfg.Projects` (the TOML-loaded maps), builds `domain.Workflow` and `domain.Project` values the same way `inmem.NewWorkflowRepository` / `inmem.NewProjectRepository` do today (including the SHA1-derived UUID for names), and upserts into SQLite using the same `GetByID` → `Create` logic already present. The new function must not import `repository` or `inmem`.

Extract the "build `domain.Workflow` from `config.WorkflowConfig`" and "build `domain.Project` from `config.ProjectConfig`" helpers out of `inmem/workflow.go` and `inmem/project.go` into a new `config/domain.go` (or equivalent) so that `sync.go` does not duplicate that logic and the helper survives `inmem`'s removal in Phase 5. Re-export the helpers as `config.WorkflowFromConfig(name string, wc WorkflowConfig) (*domain.Workflow, error)` and `config.ProjectFromConfig(name string, pc ProjectConfig, workflows map[string]*domain.Workflow) (*domain.Project, error)`. Update `inmem/workflow.go` and `inmem/project.go` to call these helpers in-place (keeps their current behavior while removing the duplication).

### Task 2 — Swap `cmd/tusk/main.go` wiring

Edit `cmd/tusk/main.go`. Current wiring (around lines 76–95):

```go
projectRepo := inmem.NewProjectRepository(cfg.Projects)
workflowRepo := inmem.NewWorkflowRepository(cfg.Workflows)
workflowSvc := service.NewWorkflowService(workflowRepo, projectRepo)
// …
if err := sqlite.SyncConfigToDB(ctx, workflowRepo, projectRepo, sqlite.NewWorkflowRepo(db), sqlite.NewProjectRepo(db)); err != nil {
    return err
}
```

Replace with:

```go
projectRepo := sqlite.NewProjectRepo(db)
workflowRepo := sqlite.NewWorkflowRepo(db)
if err := sqlite.SyncConfigToDB(ctx, cfg, workflowRepo, projectRepo); err != nil {
    return err
}
workflowSvc := service.NewWorkflowService(workflowRepo, projectRepo)
```

**Order matters:** `SyncConfigToDB` must run **before** the first service call that depends on the `_default` project existing, which today is `projectLister` / any task command. Keep the sync as early as possible after the DB is opened.

Remove the `"github.com/germanamz/tusk/inmem"` import from `cmd/tusk/main.go`.

### Task 3 — Swap `client.go` wiring

Edit `client.go` (lines ~148–170). Apply the same substitution as Task 2:

```go
projectRepo := sqlite.NewProjectRepo(db)
workflowRepo := sqlite.NewWorkflowRepo(db)
if err := sqlite.SyncConfigToDB(context.Background(), cfg, workflowRepo, projectRepo); err != nil {
    return nil, fmt.Errorf("syncing config to database: %w", err)
}
```

The `projectLister` closure on line 157 already calls `projectRepo.List(ctx)` — it continues to work unchanged because the SQLite `ProjectRepo` implements `List` identically.

Remove the `"github.com/germanamz/tusk/inmem"` import from `client.go`.

### Task 4 — Update `service/task_routing_test.go`

`service/task_routing_test.go:71` calls `sqlite.SyncConfigToDB` with the old four-argument signature. Update it to pass `*config.Config` (construct a minimal `config.Config{Workflows: …, Projects: …}` literal). If the test relies on `inmem.NewProjectRepository` for something else, leave those references in place — `inmem` is still present until Phase 5, and this test file is migrated to SQLite fixtures as part of Phase 5 Task 2.

### Task 5 — Smoke tests

- `go build ./...` must succeed with no `inmem` imports in `cmd/tusk` or the root module.
- `make test` and `make test-race` must pass.
- `make test-e2e` must pass — all CLI project and workflow commands still write to the TOML file via `config.CreateProject` etc. (caller migration happens in Phase 3 / Phase 4), but reads now come from SQLite. Because `SyncConfigToDB` upserts config → SQLite at every startup, a freshly created project in TOML appears in SQLite on the next invocation, keeping e2e scenarios green.
- Manually verify: `bin/tusk project list`, `bin/tusk workflow list`, `bin/tusk task create "smoke"` in a temp workspace.

## User-Visible Behavior (Acceptance Criteria)

- `tusk project list` / `tusk project create` / `modify` / `delete` continue to work — **creation and modification still round-trip through the TOML file** (unchanged in this phase) and are reflected on subsequent invocations via `SyncConfigToDB`.
- `tusk workflow list` / `create` / `modify` / `delete` — same.
- `tusk task create`, `start`, `done`, etc. continue to work.
- All MCP project and workflow tools still work.
- `config show`, `config get`, `config set` are unchanged.

## Changes Introduced

**Modified files:**
- `sqlite/sync.go` — new signature `SyncConfigToDB(ctx, *config.Config, *WorkflowRepo, *ProjectRepo)`.
- `cmd/tusk/main.go` — constructs `sqlite.NewProjectRepo(db)` / `sqlite.NewWorkflowRepo(db)`; removes `inmem` import.
- `client.go` — same substitution; removes `inmem` import.
- `service/task_routing_test.go` — updated to call the new `SyncConfigToDB` signature.

**New files:**
- `config/domain.go` (name flexible) — houses `config.WorkflowFromConfig` and `config.ProjectFromConfig` helpers lifted out of `inmem`.

**Modified interfaces:** none.

**Bridge code:** none new. The Phase 1 `inmem` stubs remain in place and are still dead code until Phase 5.

**Schema migrations:** none.

**New dependencies:** none.

**Caveat:** `SyncConfigToDB` is itself a legacy bridge between the TOML config and the SQLite tables. It survives Phases 3–5 unchanged. Removing it is the responsibility of the **Config Schema Trim** initiative, out of scope here.
