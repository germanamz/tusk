# Config-based Projects — Design Spec

## Overview

Projects become purely config-driven in-memory entities. The `projects` DB table is dropped. Project IDs become human-readable strings (e.g., `"default"`, `"backend"`). Tasks store `project_id` as a plain `TEXT NOT NULL` column validated at the service layer against config. A builtin `default` project exists when no config is present.

**Prerequisite:** Declarative Workflows must ship first. Workflow DB tables are already dropped and workflows are standalone config entities that projects reference by name.

## Design Decisions

| Decision | Choice | Rationale |
|---|---|---|
| Migration strategy | Breaking change — fresh DB | Pre-1.0, no compat baggage |
| Default project ID | `"default"` | Clean, matches spec, drops `_default` underscore convention |
| Unknown project_id on read | Error | Strict validation, config is source of truth |
| Task.ProjectID type | `string` (required, non-pointer) | Every task belongs to a project, `"default"` if omitted. Eliminates nil checks. |
| Config auto-creation | Write default config file when not found | Gives users a starting point to edit |

## Domain Changes

### `domain.Project`

Replace the current struct:

```go
// Before
type Project struct {
    ID              uuid.UUID
    Name            string
    Description     string
    DefaultWorkflow string
    Settings        ProjectSettings
    Version         int
    CreatedAt       time.Time
}

// After
type Project struct {
    ID       string          // human-readable, config key (e.g. "default", "backend")
    Workflow string          // name of workflow from config (e.g. "kanban")
    Settings ProjectSettings
}
```

Dropped fields: `uuid.UUID` ID, `Name`, `Description`, `Version`, `CreatedAt`. No optimistic locking — config is immutable at runtime.

`ProjectSettings` (`project_settings.go`) is unchanged — `AutoCompleteConfig` and `AutoRevertConfig` remain as-is.

### `domain.Task`

```go
// Before
ProjectID *uuid.UUID

// After
ProjectID string  // required, non-pointer, defaults to "default"
```

### `domain.TaskUpdate`

```go
// Before
ProjectID **uuid.UUID

// After
ProjectID *string  // nil = don't change, non-nil = set to value
```

### `domain.TaskFilter`

```go
// Before
ProjectID *uuid.UUID

// After
ProjectID *string
```

## Config Schema

### New config types in `internal/config/config.go`

```go
type Config struct {
    Storage  StorageConfig             `mapstructure:"storage"`
    Urgency  UrgencyConfig             `mapstructure:"urgency"`
    TUI      TUIConfig                 `mapstructure:"tui"`
    MCP      MCPConfig                 `mapstructure:"mcp"`
    Projects map[string]ProjectConfig  `mapstructure:"projects"`
}

type ProjectConfig struct {
    Workflow string                `mapstructure:"workflow"`
    Settings ProjectSettingsConfig `mapstructure:"settings"`
}

type ProjectSettingsConfig struct {
    AutoCompleteParent *AutoCompleteParentConfig `mapstructure:"auto_complete_parent"`
    AutoRevertParent   *AutoRevertParentConfig   `mapstructure:"auto_revert_parent"`
}

type AutoCompleteParentConfig struct {
    TriggerStatus string `mapstructure:"trigger_status"`
    TargetStatus  string `mapstructure:"target_status"`
}

type AutoRevertParentConfig struct {
    TriggerStatus string `mapstructure:"trigger_status"`
    TargetStatus  string `mapstructure:"target_status"`
}
```

### TOML format

```toml
[projects.default]
workflow = "kanban"

[projects.backend]
workflow = "kanban"

[projects.backend.settings.auto_complete_parent]
trigger_status = "completed"
target_status = "completed"
```

### Builtin default

If `Config.Projects` is empty after loading, inject:

```go
cfg.Projects = map[string]ProjectConfig{
    "default": {Workflow: "kanban"},
}
```

### Validation on load

After unmarshaling, `Load()` validates:
- Every project's `Workflow` value must reference a workflow that exists in `Config.Workflows` (or the builtin `kanban` if workflows config is also empty).
- Return a descriptive error if validation fails (e.g., `project "backend" references unknown workflow "nonexistent"`).

## Config Auto-Creation

When `config.Load()` does not find a config file:

1. Determine the config directory (`~/.config/tusk/`).
2. Create the directory if it doesn't exist (`os.MkdirAll`).
3. Write a default config file to `~/.config/tusk/config.toml` with default values and comments explaining each section.
4. The template includes the new `[projects]` and `[workflows]` sections with their defaults.
5. Proceed with normal loading (the just-written file will be read).

This only happens when no config file exists. If the file exists but is empty or partial, Viper's defaults fill the gaps as today.

The auto-created file content is the updated `config/default.toml` template, which will be extended with `[projects]` and `[workflows]` sections.

## Repository Interface

### `repository.ProjectRepository`

Simplified to read-only:

```go
// Before
type ProjectRepository interface {
    Create(ctx context.Context, project *domain.Project) error
    GetByID(ctx context.Context, id uuid.UUID) (*domain.Project, error)
    GetByName(ctx context.Context, name string) (*domain.Project, error)
    List(ctx context.Context) ([]*domain.Project, error)
    Update(ctx context.Context, project *domain.Project) error
    Delete(ctx context.Context, id uuid.UUID) error
}

// After
type ProjectRepository interface {
    GetByID(ctx context.Context, id string) (*domain.Project, error)
    List(ctx context.Context) ([]*domain.Project, error)
}
```

Dropped: `Create`, `Update`, `Delete`, `GetByName`. `GetByName` is replaced by `GetByID` since the ID is now the human-readable name.

## In-Memory Implementation

New file: `internal/inmem/project.go`

```go
type ProjectRepository struct {
    projects map[string]*domain.Project
}

func NewProjectRepository(cfg map[string]config.ProjectConfig) *ProjectRepository {
    // Build map[string]*domain.Project from config
}

func (r *ProjectRepository) GetByID(ctx context.Context, id string) (*domain.Project, error) {
    // Map lookup, return domain.ErrNotFound on miss
}

func (r *ProjectRepository) List(ctx context.Context) ([]*domain.Project, error) {
    // Return all projects, sorted by ID for deterministic output
}
```

No locking needed — the map is built once at startup and never mutated.

## SQLite Changes

### Deleted files
- `internal/sqlite/project.go` — entire file removed

### `internal/sqlite/store.go`
- Remove `Projects()` method from `Tx` struct
- Remove `txProjectRepo` from `WithTaskTx` callback signature — completion propagation reads projects from the in-memory repo, not from the DB transaction

### Schema (`migrations/001_initial.up.sql`)
- Drop the `CREATE TABLE projects` statement and its indexes
- Drop the `INSERT INTO projects` seed data
- Drop `REFERENCES projects(id)` FK constraint from `tasks.project_id`
- Change `tasks.project_id` to `TEXT NOT NULL DEFAULT 'default'` (plain string, no FK)
- Drop the `CREATE TABLE workflows` and related tables (already handled by Declarative Workflows prerequisite)

### Dropped migrations
- `002_project_settings.up.sql` / `002_project_settings.down.sql`
- `003_project_version.up.sql` / `003_project_version.down.sql`

These are project-table alterations that no longer apply.

### `internal/sqlite/task.go`
- `scanTask`: read `project_id` as a plain `string` instead of `sql.NullString` → `parseUUID`
- `Create`: write `task.ProjectID` as string directly
- `Update`: write `task.ProjectID` as string directly
- `buildFilter`: `WHERE project_id = ?` with string value instead of UUID

## Service Changes

### `service.ProjectService`

Becomes read-only:

```go
type ProjectService struct {
    projectRepo repository.ProjectRepository
}

func (s *ProjectService) List(ctx context.Context) ([]*domain.Project, error)
func (s *ProjectService) GetByID(ctx context.Context, id string) (*domain.Project, error)
```

Removed: `Create`, `Modify`, `GetByName`, `ModifyOptions`, `applySettingsChanges`, `validSetPaths`.

### `service.TaskService`

- `DefaultProjectID` changes from `uuid.MustParse("00000000-...")` to `const DefaultProjectID = "default"`
- `projectRepo` field stays as `repository.ProjectRepository` but backed by in-memory impl
- **Create**: if `task.ProjectID == ""`, set to `DefaultProjectID`. Validate via `projectRepo.GetByID(ctx, task.ProjectID)`. Get `project.Workflow` for status validation.
- **Update**: if `upd.ProjectID != nil`, validate via `projectRepo.GetByID`. Get workflow for status validation.
- **Completion propagation** (`checkAutoComplete` / `checkAutoRevert`): these currently receive `txProjectRepo` from `WithTaskTx`. Since projects are now in-memory and read-only, the propagation callbacks should use the service's `projectRepo` field directly instead of a transactional project repo. This eliminates the need for `Tx.Projects()`.

### `WithTaskTx` callback signature

```go
// Before
type TaskTxFunc func(ctx context.Context, txTaskRepo repository.TaskRepository, txProjectRepo repository.ProjectRepository, txWorkflowRepo repository.WorkflowRepository) error

// After (projects and workflows are in-memory, don't participate in DB tx)
type TaskTxFunc func(ctx context.Context, txTaskRepo repository.TaskRepository) error
```

Note: If Declarative Workflows already removed `txWorkflowRepo` from this signature, only `txProjectRepo` needs removal here.

## CLI Changes

### `internal/tui/project.go`
- Remove `tusk project create` subcommand and handler
- Remove `tusk project modify` subcommand and handler (including `--set`/`--unset` flag handling)
- Keep `tusk project list` — calls `projectSvc.List()`

### `internal/tui/commands.go`
- `runAdd`: parse `project:<name>`, set `task.ProjectID = name` (string). Validate will happen in service layer.
- `runModify`: parse `project:<name>`, set `upd.ProjectID = &name` (string pointer).
- `runInfo`: display `Project: <id>` instead of `Project: <name> (<uuid>)`. No need to resolve project for display — the ID is already human-readable.

### `internal/tui/render.go`
- Update `projectJSON` and `renderProjectResult` for the new `domain.Project` struct (no UUID, no description, no version, no created_at).
- Update `renderProjectList` output format.
- Remove `renderProjectModifyResult` if it exists.

### `internal/tui/app.go`
- No structural changes — `projectSvc` field type stays the same.

## MCP Changes

### `internal/mcp/server.go`
- Remove `tusk_project_create` tool registration
- Keep `tusk_project_list` tool

### `internal/mcp/tools.go`
- `handleTaskCreate`: `project` param → `task.ProjectID = project` (string, no UUID resolution)
- `handleTaskList`: `project` param → `filter.ProjectID = &project` (string pointer)
- `handleTaskModify`: `project` param → `upd.ProjectID = &project` (string pointer)
- Remove `handleProjectCreate` handler
- Update `projectResponse` struct for new `domain.Project` fields

### `internal/mcp/resources.go`
- `tusk://projects/{name}` → `projectSvc.GetByID(name)` — works as-is since ID is now the name
- Update resource response format for new `domain.Project` fields

## Filter Changes

### `internal/filter/resolve.go`

The `ProjectLookup` interface simplifies:

```go
// Before
type ProjectLookup interface {
    GetByName(ctx context.Context, name string) (*domain.Project, error)
}

// After — can be removed entirely
// The resolver maps project:<id> directly to filter.ProjectID = id
// No lookup needed since the ID is already the human-readable name
```

The resolver no longer needs to resolve project names to UUIDs. `project:backend` maps directly to `filter.ProjectID = "backend"`. Validation that the project exists happens at write time in the service layer, not at query time.

This also simplifies `filter.NewResolver` — it no longer needs a `ProjectLookup` dependency.

## DI Wiring (`cmd/tusk/main.go`)

```go
cfg, err := config.Load()
// ...

// Projects — in-memory from config
projectRepo := inmem.NewProjectRepository(cfg.Projects)
projectSvc := service.NewProjectService(projectRepo)

// Tasks — projectRepo injected for validation, no longer in Tx path
taskSvc := service.NewTaskService(taskRepo, annotationRepo, projectRepo, workflowSvc, store)
```

Remove: `sqlite.NewProjectRepo(db)`.

## Updated `config/default.toml`

Add projects and workflows sections to the template:

```toml
# ... existing sections ...

# Workflows define allowed status transitions.
# The builtin "kanban" workflow is always available even without config.
[workflows.kanban]
statuses = ["pending", "active", "completed", "deleted"]
transitions = [
  { from = "pending",   to = "active" },
  { from = "pending",   to = "deleted" },
  { from = "active",    to = "completed" },
  { from = "active",    to = "pending" },
  { from = "active",    to = "deleted" },
  { from = "completed", to = "pending" },
]

# Projects group tasks and assign workflows.
# The builtin "default" project uses the "kanban" workflow.
[projects.default]
workflow = "kanban"

# Example: custom project with auto-completion
# [projects.backend]
# workflow = "kanban"
# [projects.backend.settings.auto_complete_parent]
# trigger_status = "completed"
# target_status = "completed"
```

## E2E Test Impact

Tests reference the `_default` project and UUID-based project IDs. All E2E tests that touch projects need updating:
- Replace UUID project references with string IDs
- Replace `_default` with `"default"`
- Remove tests for `tusk project create` and `tusk project modify`
- Add tests for project config validation (unknown project → error)
- Config-based test scenarios may need a test config file with custom projects

## Files Changed Summary

| Action | File |
|---|---|
| Modify | `internal/domain/project.go` |
| Keep | `internal/domain/project_settings.go` |
| Modify | `internal/domain/task.go` |
| Modify | `internal/repository/project.go` |
| Delete | `internal/sqlite/project.go` |
| Modify | `internal/sqlite/store.go` |
| Modify | `internal/sqlite/task.go` |
| Modify | `internal/config/config.go` |
| Create | `internal/inmem/project.go` |
| Modify | `internal/service/project.go` |
| Modify | `internal/service/task.go` |
| Modify | `internal/tui/project.go` |
| Modify | `internal/tui/commands.go` |
| Modify | `internal/tui/render.go` |
| Modify | `internal/mcp/server.go` |
| Modify | `internal/mcp/tools.go` |
| Modify | `internal/mcp/resources.go` |
| Modify | `internal/filter/resolve.go` |
| Modify | `cmd/tusk/main.go` |
| Modify | `migrations/001_initial.up.sql` |
| Delete | `migrations/002_project_settings.up.sql` |
| Delete | `migrations/002_project_settings.down.sql` |
| Delete | `migrations/003_project_version.up.sql` |
| Delete | `migrations/003_project_version.down.sql` |
| Modify | `config/default.toml` |
| Modify | `tests/e2e/` (multiple files) |
