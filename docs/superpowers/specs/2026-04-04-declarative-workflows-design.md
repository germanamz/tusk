# Declarative Workflows Design Spec

## Summary

Replace the SQLite-backed workflow system with config-driven in-memory workflows, mirroring the pattern established by config-based projects (v0.4). Workflows become global entities keyed by name, defined in `config.toml`. The `workflows` and `workflow_transitions` DB tables are dropped. New CLI commands and an MCP tool are added.

## Motivation

Workflows are static configuration — they rarely change at runtime and are already defined declaratively in `config.toml`. The current dual source of truth (config file + DB tables) adds complexity without benefit. The config-based projects migration proved this pattern works; workflows follow the same path.

## Design

### Domain Changes

Simplify `domain.Workflow` and `domain.WorkflowTransition` to remove DB artifacts.

**Before:**

```go
type Workflow struct {
    ID        uuid.UUID
    ProjectID string
    Name      string
    Statuses  []string
}

type WorkflowTransition struct {
    ID         uuid.UUID
    WorkflowID uuid.UUID
    FromStatus string
    ToStatus   string
}
```

**After:**

```go
type Workflow struct {
    Name        string
    Statuses    []string
    Transitions []WorkflowTransition
}

type WorkflowTransition struct {
    FromStatus string
    ToStatus   string
}
```

Removed fields: `Workflow.ID`, `Workflow.ProjectID`, `WorkflowTransition.ID`, `WorkflowTransition.WorkflowID`. These were DB schema artifacts. Workflows are identified solely by `Name`. Transitions are embedded in the `Workflow` struct rather than queried separately.

### Repository Interface

Simplify `WorkflowRepository` to read-only, name-keyed.

**Before:**

```go
type WorkflowRepository interface {
    GetByProjectAndName(ctx context.Context, projectID string, name string) (*domain.Workflow, error)
    GetTransitions(ctx context.Context, workflowID uuid.UUID) ([]*domain.WorkflowTransition, error)
    Create(ctx context.Context, wf *domain.Workflow) error
    AddTransition(ctx context.Context, t *domain.WorkflowTransition) error
}
```

**After:**

```go
type WorkflowRepository interface {
    GetByName(ctx context.Context, name string) (*domain.Workflow, error)
    List(ctx context.Context) ([]*domain.Workflow, error)
}
```

- Write methods removed (config is the source of truth).
- `GetTransitions` removed — transitions are embedded in the `Workflow` struct.
- Lookup by name only, no `projectID`.

### In-Memory Implementation

New file: `internal/inmem/workflow.go`

Follows the same pattern as `inmem/project.go`:

- Constructor: `NewWorkflowRepository(cfg map[string]config.WorkflowConfig) *WorkflowRepository`
- Converts config structs to domain structs at construction time, stores in `map[string]*domain.Workflow`
- `GetByName`: map lookup, returns `domain.ErrNotFound` on miss
- `List`: returns all workflows sorted alphabetically by name

### SQLite Cleanup

- Delete `internal/sqlite/workflow.go` and `internal/sqlite/workflow_test.go`
- Remove `Tx.Workflows()` method from `internal/sqlite/store.go`
- Rewrite `migrations/001_initial.up.sql` to remove `workflows` and `workflow_transitions` tables and their seed data
- Rewrite `migrations/001_initial.down.sql` accordingly

### TaskTxProvider Simplification

The `TaskTxProvider` interface currently passes both `TaskRepository` and `WorkflowRepository` into the transaction callback because the propagation logic needed a transactional workflow repo. With workflows in-memory, the workflow repo has no transactional state.

**Before:**

```go
type TaskTxProvider interface {
    WithTaskTx(ctx context.Context, fn func(tr repository.TaskRepository, wr repository.WorkflowRepository) error) error
}
```

**After:**

```go
type TaskTxProvider interface {
    WithTaskTx(ctx context.Context, fn func(tr repository.TaskRepository) error) error
}
```

`Store.WithTaskTx` drops the workflow repo parameter. Propagation code uses the service-level `workflowSvc` field.

### WorkflowService Changes

**File:** `internal/service/workflow.go`

Updated struct:

```go
type WorkflowService struct {
    workflowRepo repository.WorkflowRepository
    projectRepo  repository.ProjectRepository
}
```

The `projectRepo` dependency is added to support `GetWorkflowWithProjects`.

| Method | Change |
|--------|--------|
| `IsTransitionAllowed(ctx, workflowName, from, to)` | Remove `projectID` param. Lookup by name only. |
| `GetStatuses(ctx, workflowName)` | Remove `projectID` param. |
| `GetTransitions(ctx, workflowName)` | Remove `projectID` param. Read from `Workflow.Transitions` directly. |
| `List(ctx)` | **New.** Delegates to `workflowRepo.List`. |
| `GetByName(ctx, name)` | **New.** Delegates to `workflowRepo.GetByName`. |
| `GetWorkflowWithProjects(ctx, name)` | **New.** Returns workflow + list of project IDs that reference it. |

### TaskService Changes

**File:** `internal/service/task.go`

All `workflowSvc` call sites updated to drop the `projectID` argument. The project-to-workflow resolution already happens before these calls via `projectRepo.GetByID`.

Affected call sites:

- `Create` (status validation): `workflowSvc.GetStatuses(ctx, project.Workflow)`
- `Update` (transition validation): `workflowSvc.IsTransitionAllowed(ctx, project.Workflow, oldStatus, newStatus)`
- `checkAutoComplete` / `checkAutoRevert`: use service-level `workflowSvc` instead of creating a transactional one
- `WithTaskTx` callback: simplified to `func(tr repository.TaskRepository) error`

### CLI Commands

New `tusk workflow` subcommand in `internal/tui/commands.go`.

**`tusk workflow list`** — tabular output of all workflows:

```
Name            Statuses
kanban          pending, active, completed, deleted
bug-tracking    open, investigating, fixed, verified, closed
```

**`tusk workflow info <name>`** — detailed view with referencing projects:

```
Workflow: kanban
Statuses: pending, active, completed, deleted
Transitions:
  pending   -> active
  pending   -> deleted
  active    -> completed
  active    -> pending
  active    -> deleted
  completed -> pending
Projects: default, backend
```

Both commands support `--output json` via the existing output format system.

### MCP Tool

**`tusk_workflow_list`** — returns all workflows with statuses, transitions, and referencing projects. Registered in the `"workflow"` tool group, respects `mcp.disabled_tool_groups` and `mcp.disabled_tools` config.

The existing `tusk://projects/{name}/workflow` MCP resource continues to work unchanged — it reads through `WorkflowService`.

### DI Wiring (`cmd/tusk/main.go`)

```go
// Before
workflowRepo := sqlite.NewWorkflowRepo(db)
workflowSvc := service.NewWorkflowService(workflowRepo)

// After
workflowRepo := inmem.NewWorkflowRepository(cfg.Workflows)
workflowSvc := service.NewWorkflowService(workflowRepo, projectRepo)
```

### Config

No config schema changes needed. `config.WorkflowConfig` and the `[workflows.<name>]` TOML sections already exist and define the target format. Validation in `config.go:validate()` already confirms every project references a valid workflow name. The builtin `kanban` workflow is defined in `internal/config/default.toml`.

## Files Changed

| File | Action |
|------|--------|
| `internal/domain/workflow.go` | Rewrite — remove UUID/DB fields, embed transitions |
| `internal/repository/workflow.go` | Rewrite — read-only interface with `GetByName`, `List` |
| `internal/inmem/workflow.go` | **New** — config-backed in-memory implementation |
| `internal/inmem/workflow_test.go` | **New** — unit tests |
| `internal/sqlite/workflow.go` | **Delete** |
| `internal/sqlite/workflow_test.go` | **Delete** |
| `internal/sqlite/store.go` | Edit — remove `Tx.Workflows()`, simplify `WithTaskTx` |
| `internal/service/workflow.go` | Rewrite — new methods, drop `projectID` from signatures |
| `internal/service/workflow_test.go` | Rewrite — test against new interface |
| `internal/service/task.go` | Edit — update `workflowSvc` calls, simplify `WithTaskTx` usage |
| `internal/service/task_test.go` | Edit — update mocks/expectations |
| `internal/tui/commands.go` | Edit — add `workflow list` and `workflow info` commands |
| `internal/tui/render.go` | Edit — add workflow rendering functions |
| `internal/mcp/server.go` | Edit — add `tusk_workflow_list` tool, update resource handler |
| `internal/mcp/resources.go` | Edit — update `handleWorkflowResource` for new service signatures |
| `cmd/tusk/main.go` | Edit — swap SQLite workflow repo for in-memory, update DI |
| `migrations/001_initial.up.sql` | Edit — remove workflow tables and seed data |
| `migrations/001_initial.down.sql` | Edit — remove workflow table recreation |
| `tests/e2e/` | Edit — add workflow CLI e2e tests |

## Out of Scope

- Custom workflow creation at runtime (workflows are config-only)
- Workflow migration tooling (pre-release, rewrite `001` migration)
- Parameterizing hardcoded status strings in `TaskService` (`"pending"`, `"active"`, `"completed"`, `"deleted"`) — these are convenience methods tied to the builtin kanban workflow and work correctly as-is
