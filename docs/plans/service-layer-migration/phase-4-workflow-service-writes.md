# Phase 4 — WorkflowService Writes + Caller Migration

## Inherits From

Phases 1–3 are complete. The implementer should expect:

- `WorkflowRepository` interface has `Create`, `Update`, `Delete(id, expectedVersion)`.
- `ProjectRepository` has `CountProjectsByWorkflow`.
- `cmd/tusk/main.go` and `client.go` wire SQLite repositories. A `projectSvc` variable is already constructed and handles all project-side mutations through `ProjectService`.
- `internal/tui/project.go` and `internal/mcp/project_handlers.go` no longer reference `config.CreateProject` / `ModifyProject` / `DeleteProject` — those functions are deleted.
- `config.CreateWorkflow`, `config.ModifyWorkflow`, `config.DeleteWorkflow` still exist in `config/workflow.go` and are still the write path for `tusk workflow` CLI and `tusk_workflow_*` MCP tools.
- Role-schema validation (exactly one `initial`, one `start`, ≥1 `terminal`, exactly one `done`, exactly one `delete`, roles mutual exclusion, transition validity) currently lives in `config.(*Config).Validate()` at `config/config.go:293–375`.
- `sqlite/sync.go` no longer touches existing project rows and no longer deletes DB project rows absent from the TOML — that was Phase 3's fix. The **workflow side** still has both behaviors: `sqlite/sync.go` UPDATEs existing workflow rows on every startup (~lines 37–55) and deletes any DB workflow row absent from the TOML `[workflows.*]` map (~lines 114–131). A TODO comment on each block left by Phase 3 marks them as "Phase 4 removes this symmetrically".

## Objective

Give `WorkflowService` full write capability backed by the repository. Move workflow role-schema validation out of `config/config.go` into a reusable `domain` or `service` function so both the service and the startup config loader call the same validator. Add a delete guard that uses `CountProjectsByWorkflow` (no full project list scan). Rewire CLI and MCP workflow handlers. Delete `config.CreateWorkflow` / `ModifyWorkflow` / `DeleteWorkflow`.

After this phase, every workflow mutation writes to SQLite through `WorkflowService`. The TOML file is never rewritten for workflow changes. `[workflows.<name>]` sections in the TOML continue to be read at startup via `SyncConfigToDB`, matching the project flow from Phase 3.

## Tasks

### Task 1 — Extract role-schema validation into a shared function

Create `domain/workflow_validation.go` with:

```go
func ValidateWorkflow(w *Workflow) error
```

Move the workflow-scoped validation logic currently at `config/config.go:293–375` into this function. It must check:

- Exactly one status has the `initial` role.
- Exactly one status has the `start` role.
- At least one status has the `terminal` role.
- Exactly one status has the `done` role, and that status also has `terminal`.
- Exactly one status has the `delete` role, and that status also has `terminal`.
- `highlight` and `dim` are mutually exclusive on any given status.
- A transition `initial → start` exists.
- Every transition references statuses that exist in the workflow.

Return descriptive wrapped errors using `domain.ErrInvalidWorkflow` (add the sentinel to `domain/errors.go` if missing).

Update `config.(*Config).Validate()` to iterate `cfg.Workflows`, build a `*domain.Workflow` via `config.WorkflowFromConfig` (the helper from Phase 2), and call `domain.ValidateWorkflow`. The config-level check for "every project references a known workflow" stays in `config.Validate` — it is a config-file concern, not a per-workflow concern.

Do not delete the old inline validation — replace it with the call to `domain.ValidateWorkflow`. The goal is a single source of truth, not dual maintenance.

### Task 2 — Add `WorkflowService` write methods

Edit `service/workflow.go`. The constructor already takes both repositories; leave the signature alone. Implement:

1. **`Create(ctx context.Context, input CreateWorkflowInput) (*domain.Workflow, error)`**
   - `CreateWorkflowInput` carries: `Name string`, `Statuses []domain.WorkflowStatus`, `Transitions []domain.WorkflowTransition`.
   - Reject empty name, reject names already present (via `GetByName`).
   - Build a `*domain.Workflow` with a fresh UUID and `version = 1`.
   - Call `domain.ValidateWorkflow`. If it fails, return the wrapped error.
   - Call `repo.Create`. Return the result.

2. **`Modify(ctx context.Context, input ModifyWorkflowInput) (*domain.Workflow, error)`**
   - Caller passes the expected version (optimistic locking).
   - Fetch via `GetByName` (or `GetByID`), assert version match, apply mutations.
   - Mutations must support the same inline-syntax operations `config.ModifyWorkflow` supports today (see `config/workflow.go:55–111`): set/add/remove statuses, add/remove transitions, update status roles. Port that mutation logic verbatim into the service — it is pure data manipulation and has no config-specific dependencies.
   - When statuses are removed, prune orphaned transitions (same behavior as today).
   - Run `domain.ValidateWorkflow` on the mutated copy before writing. Reject the update if validation fails.
   - Call `repo.Update`. Return the result.

3. **`Delete(ctx context.Context, id uuid.UUID, expectedVersion int64) error`**
   - Call `projectRepo.CountProjectsByWorkflow(ctx, id)`. If count > 0, return `domain.ErrWorkflowInUse` (add the sentinel to `domain/errors.go`). **Do not accept a `force` flag** — this matches the current `config.DeleteWorkflow` behavior, which has no force bypass.
   - Reject deletion of the built-in `kanban` workflow (UUID all-zero). Return `domain.ErrBuiltInWorkflow` (add sentinel if missing).
   - Call `repo.Delete(ctx, id, expectedVersion)`.

All three methods must produce the same structured error shapes as `ProjectService` from Phase 3.

### Task 3 — Rewire `internal/tui/workflow.go`

Edit `internal/tui/workflow.go`. Replace every `config.CreateWorkflow` / `ModifyWorkflow` / `DeleteWorkflow` call with the corresponding `workflowSvc` method.

- `tusk workflow create <name> status=… transition=…` handler → parse inline syntax into `CreateWorkflowInput` (the parser already produces the structured form — confirm and reuse), call `workflowSvc.Create`.
- `tusk workflow modify <name> +status=… +transition=… status=<name>(<roles>)` → fetch via `GetByName` to retrieve current version, build `ModifyWorkflowInput`, call `workflowSvc.Modify`. Pass the `version` through.
- `tusk workflow delete <name>` → fetch, call `workflowSvc.Delete(id, version)`.

Output rendering (text + JSON) must continue to show the resulting workflow struct with its new version. Run the workflow-related e2e scenarios to confirm parity.

### Task 4 — Rewire `internal/mcp/workflow_handlers.go`

Edit `internal/mcp/workflow_handlers.go`. Apply the same migration as Task 3 for the three MCP tools:

- `tusk_workflow_create` → `workflowSvc.Create`.
- `tusk_workflow_modify` → `workflowSvc.Modify`, threading the inbound `version` into `ExpectedVersion`.
- `tusk_workflow_delete` → `workflowSvc.Delete`, threading `version`.

Each tool response continues to include the current `version` so agents can chain mutations.

### Task 5 — Delete `config.*Workflow` functions and patch `SyncConfigToDB` workflow side

**5a. Delete the TOML-write functions**

Edit `config/workflow.go`:

- Delete `CreateWorkflow`, `ModifyWorkflow`, `DeleteWorkflow`.
- Leave `WorkflowConfig`, `config.WorkflowFromConfig` (from Phase 2), and any load-time parsing in place — `SyncConfigToDB` still depends on them.
- Run `go build ./...` and delete any helpers flagged as dead by the compiler, but only if they are unreferenced (do not delete helpers still used by the load path or by `config.Validate`).

**5b. Make `SyncConfigToDB` workflow seeding seed-only**

Edit `sqlite/sync.go`. Mirror the project-side fix that Phase 3 applied. Two behaviors must be removed:

1. **Workflow UPDATE branch** (~`sqlite/sync.go:37–55`, look for the `if getErr == nil` block inside the workflow loop that runs `UPDATE workflows SET name = ?, statuses = ?, transitions = ?, …`) — overwrites the DB row with TOML-sourced values on every startup. After Phase 4, `workflowSvc.Modify` persists to the DB only, and this branch would silently clobber modifications. Replace the branch with `continue` so an existing workflow row is left untouched. Preserve the `wf.Version = existing.Version` line if it is still needed by the seeding branch — otherwise drop it.

2. **Workflow stale-cleanup loop** (~`sqlite/sync.go:114–131`, the block starting with `workflowIDs := make(map[string]struct{}, …)` through the matching closing `}`) — deletes any DB workflow row whose ID is not in the TOML `[workflows.*]` map. After Phase 4, `workflowSvc.Create` writes DB-only rows that have no TOML counterpart, and the next startup would wipe them. **Delete the entire workflow stale-cleanup block**.

After this task, `SyncConfigToDB` is a pure seed-only function: if a workflow or project is missing from the DB and present in TOML, it is `Create`d; otherwise the DB row is left alone. Remove any TODO comments Phase 3 left behind on the workflow blocks.

The function is still the bridge for first-run population of TOML-defined projects and workflows. Retiring it entirely is the Config Schema Trim initiative's job.

### Task 6 — Tests

- Add unit tests in `service/workflow_test.go` for each new method: create/modify/delete happy paths, role-schema validation failures, optimistic-lock conflicts, `ErrWorkflowInUse` when a project references the workflow, `ErrBuiltInWorkflow` on kanban delete.
- Add a unit test in `domain/workflow_validation_test.go` covering each failure mode of `domain.ValidateWorkflow` (missing initial, duplicate start, missing terminal, done without terminal, highlight+dim conflict, missing initial→start transition, orphan transition).
- Run `make test`, `make test-race`, `make test-e2e`. E2E scenarios for `tusk workflow` must pass unchanged (they exercise the CLI surface, which now writes to SQLite).

## User-Visible Behavior (Acceptance Criteria)

- `tusk workflow create sprint status=pending(initial) status=active(start,highlight) status=done(terminal,done,dim) transition=pending:active,active:done` creates the workflow in SQLite with version 1.
- `tusk workflow modify sprint +status=in-review +transition=active:in-review` updates the DB row, bumps the version.
- `tusk workflow modify sprint status=active(start,highlight)` updates just the roles on that status.
- `tusk workflow delete sprint` succeeds if no project references it; fails with `ErrWorkflowInUse` otherwise; always fails on `kanban`.
- A role-schema violation on any create or modify is rejected before the write, with a descriptive error naming the rule that failed.
- MCP `tusk_workflow_*` tools match the CLI behavior and round-trip the `version` field.
- `tusk project create backend workflow=sprint` (from Phase 3) resolves the workflow from the DB — the workflow created here via the service is immediately referenceable.
- `tusk workflow create` / `modify` / `delete` **do not rewrite the TOML file**. The file is untouched regardless of outcome.
- A workflow created via `tusk workflow create` survives across process restarts — `SyncConfigToDB` no longer deletes DB workflow rows that are not present in the TOML `[workflows.*]` map.
- A workflow modified via `tusk workflow modify` keeps its modifications across restarts — `SyncConfigToDB` no longer overwrites existing workflow rows on startup.
- `go build`, `make test`, `make test-race`, `make test-e2e` all pass.

## Changes Introduced

**New files:**
- `domain/workflow_validation.go` — `ValidateWorkflow(w *Workflow) error` shared validator.
- `domain/workflow_validation_test.go` — unit tests for each validation rule.

**Modified files:**
- `domain/errors.go` — possibly adds `ErrInvalidWorkflow`, `ErrWorkflowInUse`, `ErrBuiltInWorkflow`.
- `service/workflow.go` — new `CreateWorkflowInput`, `ModifyWorkflowInput`; new `Create`, `Modify`, `Delete` methods.
- `service/workflow_test.go` — extended with write-path coverage.
- `internal/tui/workflow.go` — calls `workflowSvc` instead of `config.*Workflow`.
- `internal/mcp/workflow_handlers.go` — calls `workflowSvc`; threads `version`.
- `config/workflow.go` — `CreateWorkflow`, `ModifyWorkflow`, `DeleteWorkflow` **deleted**.
- `config/config.go` — `(*Config).Validate` delegates workflow-scoped validation to `domain.ValidateWorkflow`.
- `sqlite/sync.go` — workflow UPDATE-on-exists branch deleted; workflow stale-cleanup loop deleted. After this phase the function is seed-only for both projects and workflows.

**Modified interfaces:** none (additions in Phase 1 already cover it).

**Bridge code:** none introduced. The Phase 1 `inmem` stubs remain in place for Phase 5 to delete. `SyncConfigToDB` survives but is now seed-only — its final retirement belongs to the Config Schema Trim initiative.

**Schema migrations:** none.

**New dependencies / environment variables:** none.
