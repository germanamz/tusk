# Phase 3 — Negative tests, doc sweep, roadmap tick, plan cleanup

## Goal

Fill in the tests that cover the rejection paths introduced in phases 1 and 2, update consumer-facing docs, mark the roadmap initiative complete, and delete the plan docs used by the implementer agents.

This is intentionally a narrow cleanup phase. The functional changes landed in phases 1 and 2; this phase hardens and documents them.

## Inherits From

Phase 2 left the codebase in this state:

- `config.Config` has no `Workflows` / `Projects` fields; those types are deleted from `config/`.
- `config/default.toml` has no `[projects.*]` / `[workflows.*]` sections.
- `config.Load` hard-errors on resolved files that contain top-level `projects` or `workflows` TOML tables, pointing at `tusk project` / `tusk workflow`.
- `sqlite.SyncConfigToDB` is deleted. `cmd/tusk/main.go`, `client.go`, and `internal/mcp/server.go` no longer call it.
- `sqlitetest.NewStore` takes only `t`. `KanbanConfig` / `KanbanWorkflow` are gone. `SeedProject` helper is available.
- CLI `tusk config set projects.*`/`workflows.*` and MCP `tusk_config_set` for the same keys already return friendly errors (landed in phase 1, unchanged in phase 2).
- `tusk config show` renders DB-hydrated `[projects.*]` / `[workflows.*]` sections in both text and JSON formats.
- `make build`, `make vet`, `make lint`, `make test`, `make test-race` are green at the phase 2 boundary.

## Prerequisites

Phase 2 must be merged. This phase touches tests and docs only; it does not depend on any phase 2 internal abstraction beyond the behavior described in the "Inherits From" section above.

## Tasks

### 1. Add `config.Load` legacy-section rejection tests

In `config/config_test.go` (or a new `config/legacy_test.go` if the existing test file is already crowded):

- Add `TestLoad_RejectsLegacyProjectSections`:
  1. Write a TOML file in `t.TempDir()` containing `[storage]` + `[projects.foo]` with `workflow = "kanban"`.
  2. Call `config.Load(config.WithExplicitFile(path))`.
  3. Assert the error is non-nil and the message contains both `projects` and the phrase pointing at `tusk project`.
  4. Assert the returned `*Config` is nil (no partial state).
- Add `TestLoad_RejectsLegacyWorkflowSections`:
  1. Same shape, but with `[workflows.custom.statuses.pending]` and `roles = ["initial"]`.
  2. Assert error mentions `workflows` and points at `tusk workflow`.
- Add `TestLoad_AcceptsTrimmedConfig`:
  1. Write a TOML file containing only `[storage]`, `[urgency]`, `[tui]`, `[mcp]` sections.
  2. Assert `Load` succeeds and returns a populated config.
- Add `TestLoad_AcceptsEmbeddedDefaults`:
  1. Call `Load` with `WithSearchPath(t.TempDir())` (isolated global dir, no local file).
  2. Assert success — the embedded defaults do not carry `projects` / `workflows` tables after phase 2, so the guard must not fire on the auto-created global config.
- Add `TestLoad_RejectsLegacyInWalkUpHit`:
  1. Create a fake project directory with `tusk.toml` containing `[projects.bar]`.
  2. Call `Load(WithStartDir(projectDir))`.
  3. Assert the error is returned (the guard must apply to walk-up hits, not only explicit files).

### 2. Add end-to-end config-set rejection test

In `tests/e2e/` (the harness lives there per `CLAUDE.md`):

- Add a scenario `config_set_rejects_db_keys` that:
  1. Runs `tusk config set projects.foo.workflow kanban` and asserts the exit code is non-zero and stderr contains `tusk project modify`.
  2. Runs `tusk config set workflows.kanban.statuses.pending.roles initial` and asserts the exit code is non-zero and stderr contains `tusk workflow modify`.
- Add a scenario `config_show_includes_db_project` that:
  1. Runs `tusk project create scratch workflow=kanban`.
  2. Runs `tusk config show` and asserts the output contains `[projects.scratch]` and `workflow = "kanban"`.
  3. Runs `tusk config show` with `--output json` and asserts the JSON has `projects.scratch.workflow == "kanban"`.
- Use the existing scenario patterns — each scenario runs across the 4 config/output combinations per the CLAUDE.md harness description.

### 3. Regenerate and verify e2e golden outputs

If any existing e2e scenario pins the exact output of `tusk config show`, its golden file will have drifted as a result of phase 1 + phase 2 format changes:

- Run the full e2e suite (`make test-e2e`) and diagnose any golden diffs.
- For each failing golden, inspect the diff to confirm it reflects the expected new format (DB-hydrated sections, possibly reordered from the prior map-iteration ordering). Regenerate the golden or update the inline assertion to match.
- Grep `tests/e2e/` for any `projects.` / `workflows.` substrings that were assumptions about the old TOML shape and update them.
- Grep `tests/e2e/` for any test config files that still contained `[projects.foo]` or `[workflows.bar]` sections and replace them with `tusk project create` / `tusk workflow create` setup steps. Any such file would cause `config.Load` to hard-error at scenario startup after phase 2.

### 4. Update `docs/programmatic-usage.md`

Open `docs/programmatic-usage.md`. Find and remove:

- Any example that sets `tusk.Config{Workflows: ..., Projects: ...}`.
- Any discussion of `config.WorkflowConfig` / `config.ProjectConfig` as part of the programmatic API surface.
- Any mention of `sqlite.SyncConfigToDB`.

Replace with: "A fresh database is seeded with the built-in `default` project and `kanban` workflow via migrations. Additional projects and workflows are created via `client.Projects.Create(...)` and `client.Workflows.Create(...)` at runtime." Add a short example showing `client.Projects.Create` usage.

Leave historical `docs/releases/v0.8.md`, `docs/releases/v0.9.md`, `docs/status/v0.8-status.md`, `docs/status/v0.9-status.md`, `docs/status/v0.4-status.md` untouched — those are frozen release notes.

### 5. Update `ROADMAP.md` and delete plan docs

In `ROADMAP.md`, under `Initiative: Config Schema Trim`:

- Tick the top-level initiative box.
- Tick both story boxes and every leaf task box.
- Leave surrounding initiatives untouched.

Delete the plan directory used by the implementer agents:

- `rm -rf docs/plans/config-schema-trim/`

The plan docs served the implementer handoffs; with the initiative complete they are no longer needed. (This matches the phase-planning-rules convention: plan docs are cleaned up after the final review.)

## Acceptance criteria (user-visible behaviors after phase 3)

All phase 2 behaviors remain unchanged. Additionally:

1. `go test ./config/...` covers the legacy-section hard error on both explicit-file and walk-up-hit paths.
2. `make test-e2e` exercises `config set` rejection of `projects.*` / `workflows.*` keys and `config show` DB hydration end-to-end.
3. `docs/programmatic-usage.md` reflects the trimmed `tusk.Config` API.
4. `ROADMAP.md` marks the initiative complete.
5. `docs/plans/config-schema-trim/` is removed from the tree.

## Changes Introduced

**New tests**

- `config/config_test.go` (or new file) — 5 new `TestLoad_*` cases.
- `tests/e2e/...` — 2 new scenarios (`config_set_rejects_db_keys`, `config_show_includes_db_project`).

**Modified files**

- `docs/programmatic-usage.md` — updated examples.
- `ROADMAP.md` — boxes ticked.
- Any e2e golden or fixture files that drifted due to phase 2 format changes (list assembled during task 3 execution; cannot be enumerated in advance).

**Deleted files**

- `docs/plans/config-schema-trim/` (entire directory, at the end of the phase).

**Modified interfaces**: none.

**New environment variables**: none.

**Schema migrations**: none.

**Added dependencies**: none.

**Bridge code**: none.
