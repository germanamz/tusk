# Phase 5 — `inmem` Removal and Cleanup

## Inherits From

Phases 1–4 are complete. The implementer should expect:

- `ProjectService` and `WorkflowService` have full `Create` / `Modify` / `Delete` methods backed by SQLite repositories.
- All CLI handlers (`internal/tui/project.go`, `internal/tui/workflow.go`) call the services. No file under `internal/tui/` references `inmem` except possibly test files.
- All MCP handlers (`internal/mcp/project_handlers.go`, `internal/mcp/workflow_handlers.go`) call the services.
- `config.CreateProject` / `ModifyProject` / `DeleteProject` / `CreateWorkflow` / `ModifyWorkflow` / `DeleteWorkflow` are deleted from `config/`.
- `cmd/tusk/main.go` and `client.go` construct SQLite repositories directly. Neither file imports `inmem`.
- `service/task_routing_test.go` still compiles against Phase 2's new `SyncConfigToDB` signature, but may still reference `inmem` for other fixture setup.
- `inmem/project.go` and `inmem/workflow.go` still exist with bridge stub methods returning `domain.ErrReadOnlyRepository`. These stubs are never called in production.
- The MCP config mutex at `internal/mcp/server.go:32` (`configMu sync.Mutex`) is still present.

## Objective

Delete `inmem/project.go` and `inmem/workflow.go`, migrate every test that uses them to an equivalent SQLite fixture, and remove the MCP config mutex now that no MCP handler writes to the TOML config.

This phase is a cleanup — no new functionality. Its acceptance criterion is that the full test suite is green after the removals, and that no production or test file under the module imports the removed `inmem` types.

## Tasks

### Task 1 — Inventory remaining `inmem` references

Run:

```bash
grep -rn "inmem\." --include="*.go" .
grep -rn "\"github.com/germanamz/tusk/inmem\"" .
```

Record every hit. Expected locations based on the pre-phase survey:

- `service/*_test.go` — several service tests use `inmem.NewProjectRepository(cfg.Projects)` and `inmem.NewWorkflowRepository(cfg.Workflows)` as lightweight fixtures.
- `internal/tui/commands_test.go` — TUI tests do the same.
- `internal/mcp/*_test.go` — MCP handler tests.
- `service/task_routing_test.go` — per Phase 2 Task 4, this file was touched to fix `SyncConfigToDB`; it may still use `inmem` elsewhere.
- `internal/tui/app.go` and `internal/mcp/server.go` — if Phase 2 missed any import-level reference, catch it here.

**Do not** touch `inmem/task.go`, `inmem/relation.go`, `inmem/tag.go`, `inmem/player.go`, or any other `inmem` file. Only `inmem/project.go` and `inmem/workflow.go` are in scope for deletion — the rest of the `inmem` package, if present, is unrelated to this initiative.

### Task 2 — Migrate tests off `inmem` project / workflow fixtures

For every test file found in Task 1 that uses `inmem.NewProjectRepository` or `inmem.NewWorkflowRepository`:

1. Replace the fixture with a SQLite-backed equivalent. Use the same pattern that `service/task_routing_test.go` uses after Phase 2 — open an in-memory SQLite store via `sqlite.New(":memory:", migrations.FS)`, construct `sqlite.NewProjectRepo(db)` / `sqlite.NewWorkflowRepo(db)`, and call `sqlite.SyncConfigToDB(ctx, cfg, wfRepo, projRepo)` to seed from a synthetic `*config.Config`.

2. If a test file sets up many workflows and projects inline, consider extracting a shared helper into `sqlite/testsupport.go` (new file) with a single function like `NewTestStore(t *testing.T, cfg *config.Config) (*sqlite.Store, *sqlite.ProjectRepo, *sqlite.WorkflowRepo)`. This is optional — only do it if three or more tests benefit and the extraction is straightforward.

3. Every migrated test must still exercise the same behavior it did before. Do not change assertions. If a test was previously reading a field that only the `inmem` repo exposed, surface the equivalent through the SQLite repo or drop the test only with explicit justification in the commit message.

Run `make test` and `make test-race` after each file migration. Keep the module compiling throughout — do not delete `inmem/project.go` or `inmem/workflow.go` until every test is migrated.

### Task 3 — Delete `inmem/project.go` and `inmem/workflow.go`

Once `grep -rn "inmem\.NewProjectRepository\|inmem\.NewWorkflowRepository\|inmem\.ProjectRepository\|inmem\.WorkflowRepository" --include="*.go" .` returns zero hits:

- Delete `inmem/project.go`.
- Delete `inmem/workflow.go`.

If those were the only files in `inmem/`, delete the `inmem/` directory and the package. If other `inmem` files remain (task/relation/tag/player), leave the package alone — its `go.mod` entry and imports from unrelated code stay intact.

Run `go build ./...`. Any remaining `inmem` import referring to the deleted types will surface as a compile error; fix it.

### Task 4 — Remove the MCP config mutex

Edit `internal/mcp/server.go`:

- Delete the `configMu sync.Mutex` field at line 32 (or wherever it lives after upstream edits).
- Delete every `s.configMu.Lock()` / `s.configMu.Unlock()` call site in MCP project and workflow handlers. After Phases 3 and 4, those handlers call `projectSvc` / `workflowSvc`, which enforce optimistic locking at the DB level via `version`. The mutex is pure dead weight.
- If `sync` is no longer imported by `server.go`, remove the import.

Run the MCP test suite (`go test ./internal/mcp/...`) and the e2e suite (`make test-e2e`) to confirm no regression. Concurrent project/workflow mutations are now serialized by the DB, not by the mutex — existing optimistic-lock-conflict tests cover this.

### Task 5 — Final verification sweep

Run in order, treating any failure as a blocker:

```bash
go build ./...
go vet ./...
make test
make test-race
make test-e2e
```

Then run:

```bash
grep -rn "inmem\." --include="*.go" . | grep -v "inmem/task\|inmem/relation\|inmem/tag\|inmem/player"
```

Expected output: nothing. Any hit is a leftover reference to the deleted project/workflow `inmem` types and must be fixed before declaring the phase complete.

Confirm manually: start `bin/tusk` in a temp workspace, run through the following smoke flow:

```bash
bin/tusk workflow list
bin/tusk workflow create sprint status=pending(initial) status=active(start) status=done(terminal,done) transition=pending:active,active:done
bin/tusk project create demo workflow=sprint
bin/tusk task create "smoke" project=demo
bin/tusk task list
bin/tusk project delete demo --force
bin/tusk workflow delete sprint
```

Every command should succeed, and the TOML config file should remain unchanged throughout (check with `diff` before and after).

## User-Visible Behavior (Acceptance Criteria)

- Every CLI command (`task`, `project`, `workflow`, `tag`, `player`, `note`, `config`, `undo`, `export`, `dashboard`, `mcp serve`) behaves identically to Phase 4.
- Every MCP tool behaves identically to Phase 4. Concurrent project/workflow writes from multiple agents are serialized by DB optimistic locking; version conflicts surface as `ErrConflict` just as they do for task writes.
- Starting two `tusk` processes at the same time no longer deadlocks on the MCP config mutex (it does not exist).
- `go build`, `go vet`, `make test`, `make test-race`, `make test-e2e` all pass.
- The entire `grep -rn "inmem\." --include="*.go" .` output is limited to `inmem/task.go`, `inmem/relation.go`, `inmem/tag.go`, `inmem/player.go`, and their test files (if those still exist), or is empty if the whole `inmem` package is being removed.

## Changes Introduced

**Deleted files:**
- `inmem/project.go`
- `inmem/workflow.go`
- (Optional) the `inmem/` directory if nothing else lives there.

**Modified files:**
- Every test file under `service/`, `internal/tui/`, `internal/mcp/` that previously used `inmem` project / workflow fixtures — migrated to SQLite-backed fixtures.
- `internal/mcp/server.go` — `configMu` field and its call sites removed; possibly a `sync` import removed.

**New files:**
- (Optional) `sqlite/testsupport.go` — shared SQLite test fixture helper, only if at least three tests benefit.

**Modified interfaces:** none.

**Bridge code removed:**
- `inmem.(*ProjectRepository).{Create,Update,Delete,CountProjectsByWorkflow}` stubs (introduced Phase 1).
- `inmem.(*WorkflowRepository).{Create,Update,Delete}` stubs (introduced Phase 1).
- `internal/mcp/server.go` `configMu` mutex (introduced pre-Phase 1, retired here once the config write path is fully gone).

**Bridge code remaining after this phase:**
- `sqlite.SyncConfigToDB(ctx, *config.Config, …)` — still runs at startup and seeds config-defined projects and workflows into SQLite. Retirement is the job of the **Config Schema Trim** initiative.

**Schema migrations:** none.

**New dependencies / environment variables:** none.
