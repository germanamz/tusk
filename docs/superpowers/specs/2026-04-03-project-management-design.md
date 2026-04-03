# Project Management — Design Spec

**Date:** 2026-04-03
**Initiative:** Project Management (v0.2)
**Scope:** `tusk project {list,create,modify}` subcommands, ProjectService, optimistic locking, dot-path settings mutation

---

## Overview

Add CLI commands for project CRUD and settings configuration. This is the first noun-verb subcommand group in Tusk, establishing the pattern for `tusk tag` and future entity commands.

## 1. Database Migration

New migration `003_project_version`:

**Up:**
```sql
ALTER TABLE projects ADD COLUMN version INTEGER NOT NULL DEFAULT 1;
```

**Down:**
```sql
ALTER TABLE projects DROP COLUMN version;
```

The `domain.Project` struct gains a `Version int` field.

`ProjectRepo.Update` changes to:
```sql
UPDATE projects
SET name=?, description=?, default_workflow=?, settings=?, version=version+1
WHERE id=? AND version=?
```

Returns `domain.ErrConflict` when `RowsAffected == 0`. Same pattern as `TaskRepo.Update`.

## 2. ProjectService

New service in `internal/service/project.go` (replacing the current empty file).

**Struct:**
```go
type ProjectService struct {
    projectRepo repository.ProjectRepository
}
```

**Methods:**

| Method | Signature | Behavior |
|---|---|---|
| `Create` | `(ctx, name, description string) (*domain.Project, error)` | Generate UUID, set `DefaultWorkflow: "default"`, empty `ProjectSettings`, `Version: 1`. Call `projectRepo.Create`. |
| `List` | `(ctx) ([]*domain.Project, error)` | Pass-through to `projectRepo.List`. |
| `GetByName` | `(ctx, name string) (*domain.Project, error)` | Pass-through to `projectRepo.GetByName`. |
| `GetByID` | `(ctx, id uuid.UUID) (*domain.Project, error)` | Pass-through to `projectRepo.GetByID`. Used by `runInfo` to resolve project name from task's `ProjectID`. |
| `Modify` | `(ctx, name string, opts ModifyOptions) (*domain.Project, error)` | Fetch by name, apply changes, call `projectRepo.Update` (version check). Return updated project. |

**ModifyOptions:**
```go
type ModifyOptions struct {
    Description *string            // nil = don't change
    Sets        map[string]string  // dot-path key → value
    Unsets      []string           // dot-path keys to nil out
}
```

Single pointer for `Description` — project description is `NOT NULL DEFAULT ''`, never nullable.

### Settings Merge Logic

Dot-path resolution is limited to the known `ProjectSettings` shape (not a generic JSON walker). Valid paths:

- `auto_complete_parent.trigger_status`
- `auto_complete_parent.target_status`
- `auto_revert_parent.trigger_status`
- `auto_revert_parent.target_status`

**Rules:**
- Setting any field on a nil config struct auto-initializes it (e.g., `--set auto_complete_parent.trigger_status=completed` on a project with no `AutoCompleteParent` creates the struct).
- Unsetting a top-level key (e.g., `--unset auto_complete_parent`) nils the entire sub-config.
- Unknown dot-paths return an error.
- `Modify` returns an error if no modification options are provided (no description change, no sets, no unsets).

## 3. CLI Commands

New file `internal/tui/project.go`. A `projectCmd` Cobra parent with three subcommands, registered via `a.root.AddCommand(projectCmd)` in `app.go`.

### `tusk project list`

- No arguments or flags.
- Calls `projectSvc.List`.
- Text output: table with columns `NAME`, `DESCRIPTION`, `WORKFLOW`, `SETTINGS`.
- JSON output: array of project objects.
- Settings column in text mode: compact summary like `auto-complete:on` or `-` if empty.

### `tusk project create <name>`

- Required arg: `name`.
- Optional flag: `--description` / `-d`.
- Calls `projectSvc.Create`.
- Outputs created project (text or JSON).

### `tusk project modify <name>`

- Required arg: `name`.
- Optional flags:
  - `--description` / `-d` — set description
  - `--set key=value` — repeatable, dot-path settings
  - `--unset key` — repeatable, remove settings keys
- Calls `projectSvc.Modify`.
- Outputs modified project (text or JSON).
- Error if no modification flags provided.

## 4. Rendering

Follow existing pattern in `internal/tui/render.go`:

- `renderProject(w, *domain.Project, format)` — single project display (used by create and modify).
- `renderProjectList(w, []*domain.Project, format)` — table/array display (used by list).

Text format for single project shows key-value pairs (similar to `tusk info` for tasks). JSON format outputs the domain struct directly.

## 5. Wiring & Integration

### `cmd/tusk/main.go`

- Create `projectSvc := service.NewProjectService(projectRepo)` after `projectRepo`.
- Pass `projectSvc` to `tui.New()` instead of `projectRepo`.

### `internal/tui/app.go`

- `New()` accepts `*service.ProjectService` instead of `ProjectLookup`.
- Remove the `ProjectLookup` interface — the service covers both lookup and mutation.
- Store as `projectSvc` field on `App`.

### Existing Command Updates

- `runAdd`: `a.projectRepo.GetByName(...)` → `a.projectSvc.GetByName(...)`
- `runModify`: same change.
- `runInfo`: `a.projectRepo.GetByID(...)` → `a.projectSvc.GetByID(...)`

## 6. Testing

### Unit Tests

**`internal/service/project_test.go`:**
- `Create` — happy path, duplicate name error
- `List` — returns projects
- `GetByName` — found and not found
- `Modify` — description change, `--set` applies settings, `--unset` nils sub-config, version conflict error, unknown dot-path error, no-op error when no options provided
- Settings merge: set field on nil sub-config auto-initializes, unset top-level key nils entire struct

**`internal/sqlite/project_test.go`** (additions):
- Update with correct version succeeds and increments version
- Update with stale version returns `ErrConflict`

### E2E Tests

**`tests/e2e/project_test.go`:**
- `project_create_and_list` — create a project, list shows it
- `project_modify_description` — create, modify description, verify
- `project_modify_settings` — create, `--set auto_complete_parent.trigger_status=completed`, verify settings in JSON output
- `project_unset_settings` — set then unset, verify settings cleared
- `project_create_duplicate` — create same name twice, expect error
- `project_modify_not_found` — modify non-existent project, expect error

**Harness cleanup:** Remove `SetDefaultProjectSettings` helper from `tests/e2e/harness.go`. Replace its usage in `propagation_test.go` with `tusk project modify _default --set ...` CLI steps.

## 7. Files Changed

| Component | File(s) | Action |
|---|---|---|
| Migration | `migrations/003_project_version.{up,down}.sql` | Create |
| Domain | `internal/domain/project.go` | Modify — add `Version` field |
| SQLite repo | `internal/sqlite/project.go` | Modify — version in Update, scan |
| SQLite tests | `internal/sqlite/project_test.go` | Modify — add version locking tests |
| Service | `internal/service/project.go` | Rewrite — full ProjectService |
| Service tests | `internal/service/project_test.go` | Create |
| CLI commands | `internal/tui/project.go` | Create |
| Rendering | `internal/tui/render.go` | Modify — add project renderers |
| App wiring | `internal/tui/app.go` | Modify — replace ProjectLookup with ProjectService |
| Main wiring | `cmd/tusk/main.go` | Modify — create and wire ProjectService |
| E2E tests | `tests/e2e/project_test.go` | Create |
| E2E harness | `tests/e2e/harness.go` | Modify — remove SetDefaultProjectSettings |
| E2E propagation | `tests/e2e/propagation_test.go` | Modify — use CLI for settings |
