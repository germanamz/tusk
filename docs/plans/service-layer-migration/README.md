# Plan: Service Layer Migration (v0.10)

Source: `ROADMAP.md` → "Initiative: Service Layer Migration".

## Goal

`ProjectService` and `WorkflowService` read and write the SQLite database directly through repository interfaces, replacing the current path that funnels mutations through `config.CreateProject` / `ModifyProject` / `DeleteProject` (and their workflow twins) and reads through the `inmem/` config-backed repositories. At the end of this plan:

- Services own all project and workflow business logic (validation, guards, optimistic locking).
- `inmem/project.go` and `inmem/workflow.go` are deleted.
- `cmd/tusk/main.go` and `client.go` wire SQLite repositories directly.
- The TOML config file is no longer a write target for projects or workflows. (The `[projects.*]` / `[workflows.*]` sections still exist as a read-only seed — they are removed in the follow-up **Config Schema Trim** initiative, which is out of scope here.)

## Phase Sequence

Each phase is a standalone doc and must be executed in order unless explicitly noted. All phases are compilation-safe and independently shippable.

1. **Phase 1 — Repository Interface Expansion** (`phase-1-repo-interface-expansion.md`)
   Promotes `Create` / `Update` / `Delete` (and `CountProjectsByWorkflow`) to the repository interfaces. Adds `inmem` bridge stubs. No caller change.

2. **Phase 2 — DI Pivot to SQLite Repos** (`phase-2-di-pivot.md`)
   `cmd/tusk/main.go` and `client.go` construct SQLite repositories for projects and workflows. `SyncConfigToDB` is refactored to read from `*config.Config` instead of a repository pair. Reads still succeed because `SyncConfigToDB` keeps the DB populated from config.

3. **Phase 3 — ProjectService Writes + Caller Migration** (`phase-3-project-service-writes.md`)
   Adds `ProjectService.Create` / `Modify` / `Delete` with service-level guards and optimistic locking. Rewires CLI and MCP project handlers. Deletes `config.CreateProject` / `ModifyProject` / `DeleteProject`.

4. **Phase 4 — WorkflowService Writes + Caller Migration** (`phase-4-workflow-service-writes.md`)
   Moves role-schema validation into the service/domain layer. Adds `WorkflowService.Create` / `Modify` / `Delete`. Rewires CLI and MCP workflow handlers. Deletes `config.CreateWorkflow` / `ModifyWorkflow` / `DeleteWorkflow`.

5. **Phase 5 — `inmem` Removal and Cleanup** (`phase-5-inmem-removal-cleanup.md`)
   Deletes `inmem/project.go` and `inmem/workflow.go`. Migrates unit tests that relied on them to SQLite fixtures. Removes the MCP config mutex.

## Parallelism

All phases are strictly sequential. Phase 4 depends on `projectSvc` being constructed in `cmd/tusk/main.go` / `client.go` during Phase 3, and on the `ProjectService` write patterns that Phase 3 establishes (service constructor shape, error sentinels, optimistic-lock convention, CLI/MCP rewiring style). Phase 5 is the final cleanup and must run last. Do not hand any phase to an implementer agent until the previous phase has landed.

## Bridge Code Ledger

| Introduced in | What | Removed in |
|---|---|---|
| Phase 1 | `inmem` write stubs returning `domain.ErrNotSupported` so `inmem.ProjectRepository` / `WorkflowRepository` satisfy the expanded interface | Phase 5 |
| Phase 2 | Temporary `SyncConfigToDB(ctx, *config.Config, …)` signature — still runs at startup, still seeds config into SQLite | Config Schema Trim initiative (out of scope) |

## Out of Scope

- Removing `[projects.*]` / `[workflows.*]` from the config schema → **Config Schema Trim** initiative.
- Changing CLI command names or inline syntax → **CLI & MCP Rewiring** initiative (storage-backend swap only).
- Any changes to `config show` / `config get` / `config set`.
