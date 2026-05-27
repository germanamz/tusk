# Task 5.1 — Remove runtime-lifetime flock from MCP startup

**Phase:** 5
**Spec:** `docs/superpowers/specs/2026-05-26-tusk-concurrent-instances.md`
**Plan:** `docs/superpowers/plans/2026-05-26-concurrent-instances/PLAN.md`
**Prereqs:** Phase 3 + Phase 4 complete.
**Ships as:** one PR.

## Execution Rules

1. **This task = one PR.**
2. **Build green, full test suite green, lint clean.**
3. **No bridge code.** The flock acquisition is deleted, not stubbed.
4. **Tests asserting "second MCP startup fails" must be updated** in
   the same PR to assert success. Leaving them red is not acceptable.

## Goal

Stop MCP from acquiring the workspace flock at startup. After this
task, two `tusk mcp serve` processes start successfully against the
same workspace and run side by side.

## Scope

### Files to modify

- `internal/mcp/runtime.go`
  - Remove the flock acquisition block at lines 96-107:
    - `lockHandle, lockNewErr := lock.NewWorkspaceLock(ws.Root)`
    - `acquireCtx, acquireCancel := context.WithTimeout(...)`
    - `lockHandle.Acquire(acquireCtx)` and the error handling
    - The `defer` that releases the lock at shutdown
  - Remove the `lockHandle` field from the `Runtime` struct (if any).
  - Remove unused imports (`internal/lock`) if no other code in the
    file references it.

- `internal/mcp/runtime_test.go`
  - Find any test that asserts a second `Open` fails because the lock
    is held. Update it to assert two successful opens. Add a new test
    that confirms both runtimes can serve a `tusk_status` request.
  - If the existing test was named something like
    `TestOpen_LockBusy`, rename to `TestOpen_AllowsConcurrentInstances`
    (or similar) and rewrite its body.

- `internal/lock/lock.go` — leave the package comment alone in this
  task. T5.2 (CLI flock removal) owns the comment update, since CLI
  mutations are the last runtime callers of the flock; the "reserved
  for schema migrations only" wording is only accurate after T5.2.

### Files to leave alone

- CLI commands — they still wrap with `withWorkspaceLock`. T5.2 removes
  that.
- `internal/lock/lock.go` body — the implementation stays untouched.
- `cmd/tusk/cmd_lock_test.go` — leave alone unless it asserts MCP-vs-
  CLI lock contention; in that case update for the new behavior.

### Tests

- Existing tests in `internal/mcp/runtime_test.go` updated as above.
- New test: two `mcp.Open` calls against the same workspace both
  succeed and both produce working `Runtime` instances.

## Verification

1. `make build`, `make test`, `make vet`, `make lint`, `make fmt` green.
2. Manual smoke: in two terminals, run `tusk mcp serve` against the
   same workspace. Both should start. Send a query to each; both should
   respond.

## Out of Scope

- CLI flock removal — T5.2.
- Removing the `internal/lock` package — the package stays for
  schema-migration use.

## Notes for the Implementer

- The flock acquisition is the only thing being removed. The MCP
  runtime continues to call `indexopen.OpenOrRebuild` (which may run a
  rebuild under the flock — that's fine, rebuild is a schema-migration-
  shaped event).
- After this task, the MCP no longer holds the flock at all — CLI
  invocations stop waiting on or failing because of a running MCP
  server. CLI commands themselves still briefly acquire the flock via
  `withWorkspaceLock` until T5.2; that residual CLI ↔ CLI contention
  is brief and per-command. The `OpenOrRebuild` flow keeps using the
  flock for schema migrations, which is the package's documented
  long-term role. Mention the partial-removal state (CLI still locks
  itself; MCP does not) in the PR description.
