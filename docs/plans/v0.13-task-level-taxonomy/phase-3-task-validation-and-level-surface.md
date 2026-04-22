# Phase 3 — Task validation, event diff, task `level=` CLI/MCP surface

Initiative: v0.13 Task Level Taxonomy
Design spec: `docs/superpowers/specs/2026-04-22-task-level-taxonomy-design.md`

## Prerequisites

Phase 2 merged. Specifically:

- `config.TaxonomyConfig` is loaded from `tusk.toml`; `Config.Validate` rejects malformed taxonomies.
- `ProjectService.EffectiveTaxonomy(p)` is implemented.
- `TaskService` has a `projectSvc *ProjectService` field, wired via `NewTaskService`.
- `domain.TaskFilter.Levels` and the SQLite predicate are in place.

## Inherits From

Codebase state at end of Phase 2:

- All taxonomy types + validator + resolver + config + SQLite predicate + DI are wired.
- `TaskService.Create` / `Update` do **not** yet invoke the validator.
- `task_modified` event diffs do **not** yet include `level`.
- CLI and MCP do **not** yet accept `level=` or emit it in responses.
- `TaskService.projectSvc` is installed but unused; this phase makes it load-bearing.

## Goal

Turn on taxonomy enforcement at the task layer and expose `level` on the task-facing CLI and MCP surfaces. After this phase, a user with a workspace-level taxonomy configured in `tusk.toml` can create and modify tasks with `level=` inline and get structured validation errors when the level is missing, unknown, or rank-incompatible with the parent. Per-project overrides still have no CLI or MCP setter — Phase 4 ships that.

## Tasks

### Task 3.1 — `validateTaxonomy` helper + wiring in `Create` / `Update`

Edit `service/task.go`:

- Add helper:
  ```go
  func (s *TaskService) validateTaxonomy(ctx context.Context, bundle *RepoBundle, task *domain.Task) error {
      project, err := s.projectRepo.GetByID(ctx, task.ProjectID)
      if err != nil {
          return err
      }
      tx, _ := s.projectSvc.EffectiveTaxonomy(project)
      if tx.IsEmpty() {
          return nil
      }

      var parentLevel *string
      if task.ParentID != nil {
          parent, err := bundle.Tasks.GetByID(ctx, *task.ParentID)
          if err != nil {
              return err
          }
          var lvl string
          if parent.Level != nil {
              lvl = *parent.Level
          }
          parentLevel = &lvl
      }

      return domain.TaxonomyValidator{}.Check(
          domain.ValidationContext{Taxonomy: tx, ParentLevel: parentLevel},
          task,
      )
  }
  ```

- `Create` — invoke after the existing parent-existence check (the block that loads `*task.ParentID`), before the `bundle.WriteTx.WithTx` call. On error, return it directly.
- `Update` — invoke when `upd.Level != nil` OR `upd.ParentID != nil` OR `upd.ProjectID != nil`. Place the call after the in-memory field merge and after `detectParentCycle`, before `applyValidatedUpdate`. On error, return `(nil, err)`.

Do not add the validator call to `Start`, `Claim`, `Release`, `Complete`, `Delete`, or `Pop` — none of those modify level, parent, or project.

### Task 3.2 — Event diff extension for `level`

Edit `service/task.go`:

- `taskSnapshot` — add `Level *string` field.
- `snapshotTask` — copy `t.Level`.
- `diffTaskFields` — append, using the existing `stringPtrEqual` helper:
  ```go
  if !stringPtrEqual(orig.Level, updated.Level) {
      changes["level"] = domain.FieldChange{From: stringPtrValue(orig.Level), To: stringPtrValue(updated.Level)}
  }
  ```

No new event type. `task_modified` picks up level changes automatically.

### Task 3.3 — Apply `upd.Level` in `Update` field merge

Edit `TaskService.Update` in `service/task.go`:

Locate the existing field-merge block (where `upd.Title`, `upd.Description`, etc. are applied). Add:

```go
if upd.Level != nil {
    if *upd.Level == nil {
        task.Level = nil
    } else {
        val := **upd.Level
        task.Level = &val
    }
}
```

Position it after `upd.Description` to keep nullable-string fields grouped.

Note: `validateTaxonomy` must run *after* this merge so the post-merge `task.Level` is what the validator sees.

### Task 3.4 — CLI `level=` on task create and modify

Edit `internal/tui/task.go` (the file that parses inline fields for `tusk task create` and `tusk task modify`). Locate the switch that routes inline keys to struct fields.

- On `tusk task create` (the handler in `internal/tui/commands.go` that builds `&domain.Task{...}` — see line ~356 for the existing pattern):
  - Route `level=<name>` → set `task.Level = ptr(value)` on the constructed `*domain.Task`.
  - Reject `level=` with an empty value: return a parse error (`"level= on create requires a value; use modify to clear"`).
  - Reject any modifier: `+level=` / `-level=` → parse error.

- On `tusk task modify`:
  - Route `level=<name>` → `TaskUpdate.Level = &&value` (non-empty).
  - Route `level=` (empty) → `TaskUpdate.Level = &nilPtr` (clear). Match the existing nullable-string pattern used for `description=` and `due=`.
  - Reject any modifier.

Extend `internal/tui/task_test.go` (or the relevant parse test file) with:

- `tusk task create ... level=story` parses to `Level: ptr("story")`.
- `tusk task create ... level=` returns a parse error.
- `tusk task modify ... level=task` parses to `Level: ptrPtr(ptr("task"))`.
- `tusk task modify ... level=` parses to clear (`Level: ptrPtr(nil)`).
- `tusk task modify ... +level=story` and `-level=story` return parse errors.

### Task 3.5 — MCP `level` on task create/modify input and every task response

Edit `internal/mcp/tools.go`:

- Extend the task create/modify input schemas (tool parameter definitions) with an optional `level` string parameter. Reference the existing `priority` handling for the idiomatic pattern.
- In the handler bodies:
  - Create: if present and non-empty, set `task.Level = ptr(value)`; if explicitly empty, reject. Omitted → `Level = nil`.
  - Modify: if present and non-empty, set `upd.Level = &&value`; if explicitly empty, set `upd.Level = &nilPtr` (clear). Omitted → `upd.Level = nil` (no change).

- Extend `taskResponse`:
  ```go
  Level *string `json:"level,omitempty"`
  ```
- `toTaskResponse` — copy from `t.Level`.

Every existing task-returning handler (`tusk_task_create`, `tusk_task_modify`, `tusk_task_get`, `tusk_task_list`, `tusk_task_tree`, `tusk_task_start`, `tusk_task_complete`, `tusk_task_delete`, `tusk_task_pop`, `tusk_task_claim`, `tusk_task_release`, `tusk_task_next`, `tusk_task_available`) picks up the field automatically via `toTaskResponse`.

### Task 3.6 — Tests

**Unit tests:**

- `service/task_taxonomy_test.go` (new):
  - Create with missing level under a project with effective taxonomy → `ErrTaxonomyViolation` with `Reason: "missing"`.
  - Create with unknown level → `Reason: "unknown_level"`.
  - Create root with non-top-rank → `Reason: "root_requires_top_rank"`.
  - Create child with `parent.rank >= task.rank` → `Reason: "parent_rank_not_lower"`.
  - Update that re-parents under an incompatible rank → rejected with same reason.
  - Update that reassigns `ProjectID` to a project with an incompatible taxonomy → rejected.
  - Update that only changes `Level` → validated; parent is re-loaded.
  - Update that clears `Level` on a project *without* taxonomy → accepted (validator short-circuits).
  - Update that changes `Level` emits a `task_modified` event whose diff includes `level`.
  - Every non-mutating path (`Start`, `Claim`, `Release`, `Complete`, `Delete`, `Pop`) does not call the validator — verify via a project that would fail validation but the operation is allowed.

- `internal/tui/task_test.go`:
  - `level=` parse cases from Task 3.4.

- `internal/mcp/handlers_test.go`:
  - `tusk_task_create` with `level` → response carries `level`.
  - `tusk_task_modify` with empty `level` → clears field; response omits `level`.
  - `tusk_task_create` with empty `level` → tool error.
  - `tusk_task_list` response objects include `level` when set.

**Integration check:**

Run `make test-race` once to catch any concurrency issues introduced by the added `projectRepo.GetByID` call in validator wiring (should be safe — same pattern as the existing workflow lookups).

## Changes Introduced

**New files:**
- `service/task_taxonomy_test.go`

**Modified files:**
- `service/task.go` — `validateTaxonomy`, wiring in `Create` and `Update`, `taskSnapshot.Level`, `snapshotTask`/`diffTaskFields` extension, `upd.Level` merge logic
- `internal/tui/task.go` (+ its test file) — `level=` parse on create/modify
- `internal/mcp/tools.go` — input schema + handler logic + `taskResponse.Level` + `toTaskResponse`
- `internal/mcp/handlers_test.go` — task tool `level` coverage

**New environment variables / dependencies:** none.

**Schema migration:** none.

**Bridge code removed:** `TaskService.projectSvc` (installed in Phase 2) is now load-bearing — dereferenced by `validateTaxonomy`.

**Bridge code added:** none.

## Behavioral Acceptance

After this phase the following user-visible behavior must hold:

- In a workspace without `[taxonomy]` in `tusk.toml` and no per-project override, `tusk task create "foo"` continues to work (no level required, no validation).
- In a workspace with `[taxonomy] levels = [["milestone"], ["story"]]` in `tusk.toml`:
  - `tusk task create "Goal" level=milestone` succeeds.
  - `tusk task create "Goal"` fails with `ErrTaxonomyViolation (missing)` and a helpful CLI error message.
  - `tusk task create "Work" level=bogus` fails with `unknown_level`.
  - `tusk task create "Work" level=story` (no parent) fails with `root_requires_top_rank`.
  - `tusk task create "Work" parent=<milestone-id> level=story` succeeds.
  - `tusk task modify <id> level=story` changes level; `tusk undo`-style replay via the event log shows the `level` change in `task_modified`.
- The MCP `tusk_task_create` tool accepts `level` and surfaces the structured error when validation fails.
- Every task response (CLI `--output json`, MCP) carries `level` when set, omits it otherwise.
- All Phase 1 / Phase 2 acceptance criteria still hold.
- `make test`, `make test-race`, `make vet`, `make lint` pass.
