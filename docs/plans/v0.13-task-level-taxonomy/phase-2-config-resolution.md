# Phase 2 — Config layer, project resolution, filter field plumbing

Initiative: v0.13 Task Level Taxonomy
Design spec: `docs/superpowers/specs/2026-04-22-task-level-taxonomy-design.md`

## Prerequisites

Phase 1 merged. Specifically:

- `domain.Taxonomy`, `domain.TaxonomyValidator`, `domain.TaxonomyError`, `ErrTaxonomyViolation` exist in the `domain` package.
- `domain.ProjectSettings.Taxonomy *Taxonomy` exists and round-trips through JSON.
- `domain.Task.Level *string` and `domain.TaskUpdate.Level **string` exist.
- Migration `010_task_level` has added the `level` column with an index; `sqlite.TaskRepo` reads and writes it.

## Inherits From

Codebase state at end of Phase 1:

- All new domain types compile and have unit tests.
- SQLite task CRUD persists `level`.
- **Nothing** consumes `Taxonomy` yet — `ProjectService` doesn't know about it, `TaskService` doesn't validate with it, CLI and MCP don't accept it, and `config.Config` has no `Taxonomy` section.

## Goal

Wire the taxonomy into the layers that sit *below* CLI/MCP: config loading, project resolution, and the filter/SQL predicate. After this phase, the validator and `level` filter are fully available in code — a Go consumer using tusk as a library (per `client.go`) can set taxonomies and query by level. CLI / MCP surfaces remain unchanged; Phase 3 adds the task-level CLI/MCP plumbing.

User-visible CLI behavior remains unchanged. The `tusk.toml` file gains a documented (commented-out) `[taxonomy]` section.

## Tasks

### Task 2.1 — `TaxonomyConfig` on `config.Config`

Edit `config/config.go`:

- Add `TaxonomyConfig` type:
  ```go
  type TaxonomyConfig struct {
      Levels [][]string `mapstructure:"levels" toml:"levels" json:"levels"`
  }
  ```
- Add field to `Config`:
  ```go
  Taxonomy TaxonomyConfig `mapstructure:"taxonomy" toml:"taxonomy" json:"taxonomy"`
  ```
  Placed alphabetically near `TUI` / `MCP` / `Notes`.

Edit `config/default.toml`: append a commented-out `[taxonomy]` section documenting the shape and semantics, following the style of existing sections:

```toml
[taxonomy]
# Ordered list of rank groups, top rank first. Leave unset or empty to
# disable level validation by default. Each inner list is a peer set at
# that rank. Projects can override this via
# `tusk project modify <name> taxonomy.levels=...` once shipped.
# levels = [["milestone"], ["initiative"], ["story"], ["task", "spike"]]
```

Ensure the section is commented out so the embedded defaults parse to `Taxonomy.Levels == nil`.

Extend `config/config_test.go`:

- Load a TOML fixture with a populated `[taxonomy]` section; assert `cfg.Taxonomy.Levels` equals the expected `[][]string`.
- Load without the section; assert `len(cfg.Taxonomy.Levels) == 0`.

### Task 2.2 — `config.Validate` rejects malformed taxonomy

Extend `Config.Validate` in `config/config.go`:

```go
if len(c.Taxonomy.Levels) > 0 {
    if err := domain.Taxonomy(c.Taxonomy.Levels).Validate(); err != nil {
        return fmt.Errorf("invalid taxonomy: %w", err)
    }
}
```

Import `domain` — this is a boundary crossing that already exists elsewhere in config via the legacy project/workflow validation path, so no import cycle.

Extend `config/config_test.go`:

- A TOML fixture with a duplicate level name fails `Load` with a wrapped `Validate` error.
- A TOML fixture with an empty rank group (e.g., `[[...], [], [...]]`) fails.
- The well-formed fixture from Task 2.1 passes.

### Task 2.3 — `ProjectService.EffectiveTaxonomy`

Edit `service/project.go`:

- Add a `*config.Config` field to `ProjectService` and accept it in `NewProjectService`. Follow the pattern used by `UrgencyEngine` (`service/urgency.go`) which receives config defaults via constructor.
- Rewire `NewProjectService` call sites — primarily `cmd/tusk/` DI wiring. Grep for `NewProjectService(` to find all call sites; pass the loaded `*config.Config`. Tests that construct `ProjectService` inline must pass a zero-value or test-fixture config.
- Add the public resolver:
  ```go
  type TaxonomySource int
  const (
      TaxonomySourceNone TaxonomySource = iota
      TaxonomySourceWorkspace
      TaxonomySourceProjectOverride
  )

  func (s *ProjectService) EffectiveTaxonomy(p *domain.Project) (domain.Taxonomy, TaxonomySource)
  ```
  Resolution:
  1. If `p.Settings.Taxonomy != nil` (including `&empty` opt-out) → return its value. Source: `ProjectOverride`.
  2. If `s.cfg != nil && len(s.cfg.Taxonomy.Levels) > 0` → return `domain.Taxonomy(s.cfg.Taxonomy.Levels).Clone()`. Source: `Workspace`.
  3. Otherwise → return `domain.Taxonomy{}`, source `None`.

- Add `service/project_test.go` coverage for all four source/value combinations (`None`, `Workspace`, `ProjectOverride` with populated, `ProjectOverride` with opt-out).

### Task 2.4 — `TaskFilter.Levels` and SQLite predicate

Edit `domain/filter.go`:

- Add `Levels []string // OR match` to `TaskFilter`, placed after `Statuses` to keep OR-matched fields grouped.

Edit `sqlite/task.go` (the list-query predicate builder, search for the block that handles `filter.Statuses`):

- Mirror the status predicate: when `len(filter.Levels) > 0`, append `AND level IN (?, ?, ...)` and bind each value.
- Keep the builder's existing pattern — same placeholder construction helper, same error path.

Extend `sqlite/task_test.go`:

- Seed tasks with levels `story`, `task`, and `NULL`; list with `TaskFilter{Levels: []string{"story"}}` returns only the story task.
- `TaskFilter{Levels: []string{"story", "task"}}` returns both.
- `TaskFilter{Levels: nil}` (zero value) returns all three (no predicate applied).

### Task 2.5 — Thread `*ProjectService` into `TaskService`

Edit `service/task.go`:

- Add `projectSvc *ProjectService` field to `TaskService`.
- Update `NewTaskService` signature to accept it. Position it after the existing `projectRepo` parameter to keep related params adjacent.
- Rewire call sites in `cmd/tusk/` DI setup — grep for `NewTaskService(` and `service.NewTaskService(`. Existing tests that construct `TaskService` inline will pass `nil` for the new field; the service does not yet dereference it (validator wiring lands in Phase 3).

Bridge note: `projectSvc` is installed but unused in Phase 2. It becomes load-bearing in Phase 3. This is not a stub — it's a dependency injected ahead of its consumer to keep the DI-rewire diff localized to one phase.

### Task 2.6 — Tests recap

Confirm the following test files exist and pass:

- `domain/taxonomy_test.go` (Phase 1) + any new cases if regex / helpers extended.
- `config/config_test.go` — taxonomy load + validation cases.
- `service/project_test.go` — `EffectiveTaxonomy` coverage.
- `sqlite/task_test.go` — `Levels` filter predicate.

Run the full suite:

```bash
make test
make vet
make lint
```

All green.

## Changes Introduced

**New files:** none (all changes are in existing files plus test additions).

**Modified files:**
- `config/config.go` — `TaxonomyConfig`, field on `Config`, `Validate` hook
- `config/default.toml` — commented `[taxonomy]` section
- `config/config_test.go` — taxonomy load + validate cases
- `service/project.go` — `*config.Config` field, `EffectiveTaxonomy` method, `TaxonomySource` enum
- `service/project_test.go` — resolution tests
- `service/task.go` — `projectSvc *ProjectService` field on `TaskService`, threaded through `NewTaskService`
- `domain/filter.go` — `TaskFilter.Levels []string`
- `sqlite/task.go` — list predicate for `Levels`
- `sqlite/task_test.go` — level filter tests
- `cmd/tusk/` DI wiring — pass `*config.Config` to `NewProjectService`, pass `*ProjectService` to `NewTaskService`

**New environment variables / dependencies:** none.

**Schema migration:** none (Phase 1 handled it).

**Bridge code:** `TaskService.projectSvc` is installed but unused until Phase 3. Removal target: Phase 3 (where it becomes load-bearing via `validateTaxonomy`).

## Behavioral Acceptance

- `go build ./...`, `make test`, `make vet`, `make lint` all pass.
- Existing CLI commands and MCP tools are unchanged end-to-end.
- E2E suite passes.
- A Go consumer embedding tusk via `tusk.NewClient` can call `client.Projects.EffectiveTaxonomy(project)` and receive the correct taxonomy/source. (Not yet formally exposed on the client — internal service surface is sufficient for Phase 3.)
- `tusk.toml` with a valid `[taxonomy]` section loads; malformed taxonomies fail `Load` with a wrapped `Validate` error.
- `TaskFilter{Levels: []string{"story"}}` filters tasks by level via the repository layer.
