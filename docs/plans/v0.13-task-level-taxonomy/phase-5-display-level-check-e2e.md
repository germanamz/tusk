# Phase 5 — Display surfaces, `tusk task level-check`, error rendering, E2E suite

Initiative: v0.13 Task Level Taxonomy
Design spec: `docs/superpowers/specs/2026-04-22-task-level-taxonomy-design.md`

## Prerequisites

Phase 4 merged. Specifically:

- CLI and MCP can fully manage taxonomies and levels (task `level=`, project `taxonomy.*`, config `taxonomy.levels`).
- Filter grammar accepts `level=name` / `level=a,b`.
- Every task response carries `Level`; every project response carries `settings.taxonomy` + `effective_taxonomy`.
- `TaskService` validates on `Create` / `Update` and logs level changes in `task_modified` events.

## Inherits From

Codebase state at end of Phase 4:

- Functional taxonomy on every surface except:
  - No `tusk task level-check` command or service method.
  - `tusk task get` / `tusk task tree` / `tusk project show` / `tusk config show` text renderers do not render level or taxonomy.
  - `ErrTaxonomyViolation` surfaces with Go's default error string — no mapping to user-friendly messages.
  - No E2E scenarios.

## Goal

Close out the initiative with the read-side polish (renderers, error strings), the retroactive-violation reporter (`level-check`), and the E2E suite. After this phase the initiative checklist in ROADMAP.md can be marked complete.

## Tasks

### Task 5.1 — `TaskService.LevelCheck` + `LevelViolation`

Edit `service/task.go`:

- Add:
  ```go
  type LevelViolation struct {
      Task     *domain.Task
      Taxonomy domain.Taxonomy
      Source   TaxonomySource
      Err      *domain.TaxonomyError
  }

  // LevelCheck walks tasks matching filter and reports each task whose state
  // violates its project's effective taxonomy. Default filter (nil) scans every
  // task regardless of status (including terminal). Never mutates.
  func (s *TaskService) LevelCheck(ctx context.Context, filter domain.FilterExpr) ([]LevelViolation, error)
  ```

  Implementation:
  1. Default-status injection happens in the `filter.Resolver` (Phase 1 baseline code path) when no explicit status term is present. That logic injects `status=pending,active` — which would hide terminal violations from `level-check`. `LevelCheck` must scan *every* status. Two acceptable approaches:
     a. `LevelCheck` takes an already-resolved `domain.FilterExpr` and the CLI layer (Task 5.2) builds that expression bypassing the default-status injection — e.g., by constructing the filter via `filter.Resolver.ResolveExpr` but wrapping the result in an explicit all-status term, or by using a dedicated resolver entry point. Add `filter.ResolveForLevelCheck` (or similar) if needed.
     b. `LevelCheck` accepts `FilterExpr` as-is and post-filters by re-scanning terminal tasks directly from the repo. This double-scans and is wasteful.
     Prefer (a). Add a small helper in `filter/resolve.go` (e.g., `ResolveExprAllStatuses`) that skips the default-status wrapper; invoke it from the `level-check` CLI handler.
  2. List tasks using the chosen filter across every bundle in the workspace (use `s.projects(ctx)` to enumerate project IDs, then `s.resolve(ctx, pid)` per bundle — mirrors the fan-out pattern used by `TaskService.DeleteAnnotation`).
  3. Group tasks by `ProjectID`. For each project, resolve the effective taxonomy once via `s.projectSvc.EffectiveTaxonomy`. Skip projects whose taxonomy is empty.
  4. For each task in a project with a non-empty taxonomy:
     - If `task.ParentID != nil`, load parent (same bundle) to extract `parent.Level`.
     - Run `domain.TaxonomyValidator{}.Check`.
     - On `*TaxonomyError`, append to the result list.

  Note: parent loads are per-task. In pathological workspaces this is O(N) reads on top of the list; accept for this scope since `level-check` is human-invoked and not hot.

- Add `service/task_level_check_test.go`:
  - Seed: workspace with two projects, one with a taxonomy. Create tasks that violate each of the four `Reason` codes plus some valid tasks.
  - Assert `LevelCheck` returns exactly the violating tasks with correct reasons.
  - Assert tasks in projects with empty taxonomy are not flagged.
  - Assert terminal-status tasks (completed, deleted) are still scanned.

### Task 5.2 — CLI `tusk task level-check`

Edit `internal/tui/task.go` (and the command tree registration in `cmd/tusk/`):

- Register `tusk task level-check` as a subcommand under the existing `tusk task` group.
- Accept the same inline filter syntax as `tusk task list` (delegate to the existing inline-filter parser). Default filter: none (scan whole workspace).
- Output:
  - Text (default): one line per violation, sorted by `project asc, short_id asc`. Format:
    ```
    <short_id>  <project>  <level-or-—>  <reason>  (parent: <parent_level>)
    ```
    Use the existing color helpers to style the reason code red.
  - JSON (`--output json`): array of `{"task": <taskResponse>, "reason": "...", "taxonomy": {"ranks": [...]}, "source": "..."}`. Reuse `toTaskResponse` (or its CLI equivalent) so the task shape matches every other task command.
- Exit code: `0` when no violations, `1` when any found.

Extend command tests (`internal/tui/commands_test.go` or the `task` group's existing test file) with:

- `level-check` under a workspace with no taxonomy returns zero violations and exit code 0.
- `level-check project=backend` scopes correctly.
- JSON output schema matches the spec.
- Exit code is 1 when violations exist.

### Task 5.3 — Renderers: `tusk project show`, `tusk config show`

Edit `internal/tui/project.go` (or wherever `tusk project show` text rendering lives):

- When rendering a project, call `projectSvc.EffectiveTaxonomy(p)` and add a block:
  ```
  Taxonomy: milestone:initiative:story:(task,spike)
    source: project override
  ```
  Map `TaxonomySource` → label:
  - `TaxonomySourceProjectOverride` with non-empty value → `"project override"`.
  - `TaxonomySourceProjectOverride` with empty value (opt-out) → render `Taxonomy: (disabled; project opted out)` and omit the `source:` line.
  - `TaxonomySourceWorkspace` → `"workspace default"`.
  - `TaxonomySourceNone` → `Taxonomy: (none)` and omit `source:`.

Edit `internal/tui/config_render.go`:

- When rendering `tusk config show`, add a `[taxonomy]` section rendered from the loaded `Config.Taxonomy.Levels` via `FormatTaxonomyInline` (from Phase 4). Omit the section when empty.

Add renderer tests (`internal/tui/config_render_test.go`, `internal/tui/project.go` test file) for each branch.

### Task 5.4 — Renderers: `tusk task get`, `tusk task tree`

Edit `internal/tui/render.go` (or wherever `tusk task get` text formatting lives):

- Before rendering, call `projectSvc.EffectiveTaxonomy(project)` for the task's project. When the result is non-empty, render `Level: <value or —>` as a field line (near `Status:` or `Priority:`).
- When the effective taxonomy is empty, omit the `Level:` line entirely (keeps narrow displays uncluttered).

Edit `internal/tui/tree.go` (or equivalent tree renderer):

- For each node whose project has a non-empty effective taxonomy, append ` [level]` to the node label. Use dim styling (consistent with status rendering) so the level doesn't dominate the line.

Extend `internal/tui/render_test.go` and `internal/tui/tree_test.go` accordingly.

### Task 5.5 — Error message mapping

Edit `internal/tui/app.go` (or wherever the root CLI error handler lives) and `internal/mcp/errors.go`:

- Detect `*domain.TaxonomyError` (or `errors.As`) and render per spec § 12:
  | Reason                   | CLI message                                                                                |
  | ------------------------ | ------------------------------------------------------------------------------------------ |
  | `missing`                | `project <name> requires a level; supply level=<top-rank peers> (or any rank on modify)`    |
  | `unknown_level`          | `level <name> is not in the taxonomy for <project>: <inline-rendered taxonomy>`             |
  | `root_requires_top_rank` | `root tasks must use the top-rank level (<top-rank peers>); got <name>`                     |
  | `parent_rank_not_lower`  | `<level> cannot sit under <parent_level> — parent rank must be strictly lower`              |

  Text renderer: colorize the level / parent-level tokens using the existing highlight helpers.

- MCP: surface as a structured tool-error payload:
  ```json
  {"code": "taxonomy_violation", "reason": "...", "level": "...", "parent_level": "...", "taxonomy": {"ranks": [...]}}
  ```
  Preserve the top-level error message for agents that ignore structured codes.

Extend tests:
- `internal/tui/render_test.go` or a new `errors_test.go` — each reason produces the expected string.
- `internal/mcp/errors_test.go` — each reason surfaces as the expected structured code.

### Task 5.6 — E2E scenarios

Add the following files under `tests/e2e/`:

- `tests/e2e/levels_basic_test.go` — workspace taxonomy, project, tasks at each rank, `tusk task get` and `tree` render levels.
- `tests/e2e/levels_project_override_test.go` — set per-project override, assert provenance in `project show` and MCP `effective_taxonomy.source`.
- `tests/e2e/levels_opt_out_test.go` — workspace default set; one project with `taxonomy.disable=true`; unlevelled tasks accepted in the opted-out project and rejected elsewhere.
- `tests/e2e/levels_validation_test.go` — all four `TaxonomyError` reasons surface with correct exit codes and error messages.
- `tests/e2e/levels_prospective_test.go` — create task; add taxonomy after; `level-check` surfaces the violation; `tusk task modify <id> title="new"` (not touching level/parent/project) still succeeds.
- `tests/e2e/levels_reassign_test.go` — move task between projects with differing taxonomies (both success with same-level and failure with incompatible level).
- `tests/e2e/levels_level_check_test.go` — filtered level-check (`project=...`, `tree=...`), text vs JSON output, exit codes.

Each follows the existing E2E harness conventions in `tests/e2e/` (each scenario run 4 times via the DB/output-format matrix already in place).

Run the full E2E suite:

```bash
make test-e2e
```

Run the full test-race pass:

```bash
make test-race
```

### Bonus cleanup

If `diffTaskFields` or `snapshotTask` picked up any dead references during earlier phases, clean them up here. Otherwise this phase adds no new types to existing structs.

## Changes Introduced

**New files:**
- `service/task_level_check_test.go`
- `tests/e2e/levels_basic_test.go`, `levels_project_override_test.go`, `levels_opt_out_test.go`, `levels_validation_test.go`, `levels_prospective_test.go`, `levels_reassign_test.go`, `levels_level_check_test.go`

**Modified files:**
- `service/task.go` — `LevelViolation`, `LevelCheck`
- `internal/tui/task.go`, `cmd/tusk/` — `level-check` subcommand registration + handler
- `internal/tui/project.go` — `project show` renders effective taxonomy + provenance
- `internal/tui/config_render.go` — `[taxonomy]` block in `config show`
- `internal/tui/render.go` — `tusk task get` renders `Level:` when applicable
- `internal/tui/tree.go` — tree nodes append `[level]`
- `internal/tui/app.go` — `*domain.TaxonomyError` → user-friendly CLI error
- `internal/mcp/errors.go` — structured payload for taxonomy violations
- Test files for each renderer / error path above

**New environment variables / dependencies:** none.

**Schema migration:** none.

**Bridge code:** none. All prior-phase scaffolding is now fully consumed.

## Behavioral Acceptance

- `tusk task level-check` with no filter lists every task that violates its project's effective taxonomy; exit code is 0 when clean, 1 when violations exist.
- `tusk task level-check project=backend tree=a3f8b2c1 --output json` produces the spec's structured JSON and the expected exit code.
- `tusk task get <id>` renders `Level:` only when the task's project has a non-empty effective taxonomy; otherwise the line is absent.
- `tusk task tree` appends `[<level>]` to nodes whose project has a non-empty effective taxonomy.
- `tusk project show <name>` renders the effective taxonomy with a `source:` provenance line; opt-out and none variants render the documented placeholder strings.
- `tusk config show` includes a `[taxonomy]` block rendered via `FormatTaxonomyInline` when the workspace default is set.
- Every `ErrTaxonomyViolation`-producing CLI command surfaces a user-friendly message keyed on the reason; MCP surfaces the structured payload with `code: "taxonomy_violation"`.
- All seven new E2E scenarios pass end-to-end across the DB-mode × output-format matrix.
- All prior-phase acceptance criteria still hold.
- `make test`, `make test-race`, `make vet`, `make lint`, `make test-e2e` all pass.
