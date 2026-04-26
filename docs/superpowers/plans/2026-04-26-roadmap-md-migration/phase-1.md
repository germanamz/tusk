# Phase 1 — Project description: schema, domain, repository

**Initiative:** ROADMAP.md Migration (v0.13)
**Spec:** `docs/superpowers/specs/2026-04-26-roadmap-md-migration-design.md`
**Prerequisites:** none (starts from `feat/roadmap-migration` HEAD).

## Goal

Add a `description` column to the `projects` table and propagate it to the domain type, the `ProjectUpdate` shape, and the SQLite repository's read/write paths. The service layer, CLI, MCP, and codec are NOT touched in this phase — they are explicitly out of scope and arrive in Phase 2.

After this phase, the column exists end-to-end at the storage and domain layers, defaults to `''` for all existing projects, and round-trips through the repository. No user-visible CLI, MCP, or codec behavior changes.

## Tasks

### Task 1 — SQL migration

Add a new migration pair under `migrations/`:

- `migrations/013_project_description.up.sql`:
  ```sql
  ALTER TABLE projects ADD COLUMN description TEXT NOT NULL DEFAULT '';
  ```
- `migrations/013_project_description.down.sql`:
  ```sql
  ALTER TABLE projects DROP COLUMN description;
  ```

Migration filenames must keep the `NNN_name.up.sql` / `NNN_name.down.sql` convention seen in the directory (last existing pair is `012_task_urgency_overrides.*.sql`). Migrations are embedded by `migrations/migrations.go`; no edits to that file are needed since it uses `embed.FS` over the whole directory — the new files are picked up automatically.

### Task 2 — Domain types

In `domain/project.go`, add a `Description string` field to `domain.Project`:

```go
type Project struct {
    ID          uuid.UUID
    Name        string
    WorkflowID  uuid.UUID
    Description string         // NEW
    Settings    ProjectSettings
    Version     int
    CreatedAt   time.Time
    UpdatedAt   time.Time
}
```

`Description` is a plain `string` (not `*string`): empty string means "no description", which matches the SQL default. There is no need to distinguish "unset" from "explicitly empty" at the domain layer.

### Task 3 — Update `ModifyProjectInput`

In `service/project.go`, add a `Description **string` field to `ModifyProjectInput`. Outer `nil` = "no change"; outer non-nil + inner `nil` = "clear to empty"; outer non-nil + inner non-nil = "set to value". This matches the existing double-pointer convention used elsewhere in the codebase (e.g., `domain.TaskUpdate.Description`).

```go
type ModifyProjectInput struct {
    Name            string
    ExpectedVersion int
    WorkflowID      *uuid.UUID
    AutoComplete    *domain.AutoCompleteConfig
    AutoRevert      *domain.AutoRevertConfig
    Description     **string         // NEW
    Urgency         UrgencyMutation
    Taxonomy        *TaxonomyMutation
}
```

Also add `Description string` to `CreateProjectInput`. Empty default keeps Phase 1 behavior identical to today.

```go
type CreateProjectInput struct {
    Name        string
    WorkflowID  uuid.UUID
    Description string                  // NEW
    Settings    domain.ProjectSettings
}
```

**Bridge code (Phase 2 removes):** the `ProjectService.Create` body in `service/project.go` does **not** yet wire `input.Description` into the constructed `domain.Project`. It will continue to construct `&domain.Project{ID, Name, WorkflowID, Settings, Version, CreatedAt, UpdatedAt}` exactly as today. The same holds for `ProjectService.Modify` — the new `Description` field on `ModifyProjectInput` is accepted by the type system but ignored at runtime. Phase 2 fills both branches in.

This bridge is necessary because Phase 1 should not change user-visible behavior (rule 9): wiring the new field through the service before the CLI/MCP/codec are ready would let some test paths exercise a partially-plumbed feature. Tag the two ignored fields with a `// TODO(phase-2): plumb` comment.

### Task 4 — Repository

In `sqlite/project.go`:

1. Extend the `projectColumns` constant to include the new column:
   ```go
   const projectColumns = `id, name, workflow_id, description, settings, version, created_at, updated_at`
   ```
2. Update the `Create` method's `INSERT` placeholder count from 7 to 8 and pass `p.Description` in the `ExecContext` argument list.
3. Update `scanProject` to scan the new column into `&p.Description` between `&workflowStr` and `&settingsJSON`. Scan the column directly into the `Description string` field — no parsing needed.
4. Update the `Update` method's `UPDATE … SET` clause to include `description = ?`, and add `p.Description` to the argument list before `nowStr`.

`GetByID`, `GetByName`, and `List` route through `scanProject`, so they pick up the change automatically.

### Task 5 — Repository tests

In `sqlite/project_test.go`:

1. Extend `TestProjectRepo_CreateAndGetByID` to seed a non-empty description on creation, then assert it round-trips through `GetByID`.
2. Add a new test `TestProjectRepo_Update_Description` that creates a project with empty description, calls `Update` after setting `p.Description = "vision text"`, then re-reads via `GetByName` and asserts the new value.
3. Add a test `TestProjectRepo_Default_HasEmptyDescription` that resolves the `_default` project (already seeded) and asserts `Description == ""`. This proves the migration's default value applies to existing rows.

The SQLite test harness (`sqlite/sqlite_test.go` / `newTestDB`) runs all migrations on each fresh DB, so no extra setup is needed — the new migration is exercised by every test in the package.

### Task 6 — Verify the migration runs cleanly

Run `make test` and confirm:

- The migration package's tests still pass (it ships its own embed checks).
- The `sqlite` package tests all pass, including the seed test (`project_seed_test.go`).
- Existing service-layer tests still pass — none of them inspect `Description`, but they exercise `Create` and `Modify` flows that must continue to compile and run.

If a failure surfaces in `service/project_test.go` because of unspecified field initialization, fix the *test* call sites to use named struct literals; do not change the production code.

## User-visible behaviors (acceptance criteria)

- `tusk project create` and `tusk project modify` work exactly as today (no new flags or fields exposed at the CLI). The new `Description` plumbing is invisible to end users at this phase.
- `tusk project show` continues to render today's fields. No new fields appear yet (Phase 2 adds the rendering).
- The `_default` project is still seeded and accessible by name and UUID (`uuid.Nil`).
- Existing JSON portability dumps round-trip without error. New dumps written from this phase will not yet contain a `description` key (Phase 2 adds it to `PortableProject`); Go's JSON decoder treats the missing key as the zero value, so existing test fixtures keep working.
- `make test` and `make test-race` pass.

## Bridge code introduced

| Bridge | Where | Removed in |
|---|---|---|
| `ProjectService.Create` ignores `input.Description` | `service/project.go` | Phase 2 |
| `ProjectService.Modify` ignores `input.Description` | `service/project.go` | Phase 2 |
| No CLI/MCP/codec exposure of the field | `internal/tui/project*`, `internal/mcp/project_handlers.go`, `internal/portability/portable.go` | Phase 2 |

Each bridge is tagged with a `// TODO(phase-2): plumb description` comment so the Phase 2 implementer can grep for the removal points.

## Changes Introduced

- **New files:** `migrations/013_project_description.up.sql`, `migrations/013_project_description.down.sql`.
- **Modified files:** `domain/project.go` (new field), `service/project.go` (input struct fields, with TODOs), `sqlite/project.go` (column list + scan + insert + update), `sqlite/project_test.go` (extended + new tests).
- **Schema migration:** `013_project_description` adds `description TEXT NOT NULL DEFAULT ''` to `projects`.
- **No new dependencies.**
- **No new environment variables.**
- **Public API additions:** `domain.Project.Description string`, `service.CreateProjectInput.Description string`, `service.ModifyProjectInput.Description **string`. None are wired through the service body yet (bridge tagged above).
