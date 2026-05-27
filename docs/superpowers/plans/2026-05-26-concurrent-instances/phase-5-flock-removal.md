# Phase 5 — Remove workspace flock from runtime

**Spec:** `docs/superpowers/specs/2026-05-26-tusk-concurrent-instances.md`
**Plan:** `docs/superpowers/plans/2026-05-26-concurrent-instances/PLAN.md`
**Prereqs:** Phase 3 + Phase 4 both complete. Every WRITE path must use
the lease primitive before this phase removes the flock.
**Parallelization:** Sequential. T5.1 → T5.2. The serialization is
defensive: T5.2 owns the `internal/lock/lock.go` package-comment update
("reserved for schema migrations only"), and that comment is only
accurate after the CLI flock call sites are removed. Running them in
parallel would leave the comment briefly stale (tree stays green; docs
lag). One-task sequencing keeps the package comment in lockstep with
the code.

## Inherits From

Phase 3 left in place:
- `EmbedQueueRepo.Drain` claims rows by lease; multiple workers can drain
  in parallel.
- TTL configuration is plumbed through env + manifest.

Phase 4 left in place:
- Every WRITE-BOTH handler (`node_create`, `node_modify`, `node_move`,
  `node_delete`) uses `WriteWithLease` or the underlying lease primitive
  to coordinate FS writes.
- `file_state` rows exist for every node touched since Phase 4 shipped.
- The workspace flock at `internal/mcp/runtime.go:96-107` and around
  every CLI mutation (via `withWorkspaceLock` in `cmd/tusk/`) is still
  acquired — Phase 5 is what removes it.

The `internal/lock` package itself stays. It will remain available for
schema migrations as the one legitimate use of a coarse workspace-wide
lock (see spec § *Workspace lock removal*).

## Execution Rules

1. **One task = one PR.** Two tasks, two PRs.
2. **Each PR independently shippable.** Build green, full Go test suite
   green, lint clean.
3. **No schema changes in this phase.**
4. **No bridge code.** The flock is removed, not stubbed.

## Goal

Stop acquiring the workspace flock for runtime operations. After this
phase, two MCP servers start successfully against the same workspace,
and CLI mutations no longer wait on (or fail because of) a running MCP
server holding the lock.

The `internal/lock` package and `lock.WorkspaceLock` type remain. The
package comment is updated to make clear that the lock is now reserved
for index schema migrations only.

## Tasks

| #     | Title                                                       | Prereqs |
|-------|-------------------------------------------------------------|---------|
| 5.1   | Remove runtime-lifetime flock from MCP startup              | none (Phases 3+4) |
| 5.2   | Remove `withWorkspaceLock` wrapping from CLI mutation commands + update package comment | T5.1 |

T5.2 ships after T5.1 so the `internal/lock/lock.go` package comment
update reflects the actually-finished state.

## Changes Introduced

- **Removed:** `lockHandle.Acquire(acquireCtx)` call and release defer in
  `internal/mcp/runtime.go:96-107` (T5.1).
- **Removed:** `withWorkspaceLock` wrapping around every mutation command
  in `cmd/tusk/cmd_node_*.go`, `cmd_edge_*.go`, `cmd_watch.go`,
  `cmd_doctor.go`, `cmd_reindex.go` (T5.2). The `withWorkspaceLock`
  helper itself stays in the codebase for the migration path; it is
  simply not called by these commands anymore.
- **Modified:** `internal/lock/lock.go` package comment, documenting
  that the lock is now reserved for schema migrations (T5.2 owns this
  update, since CLI mutations are the last runtime callers of the
  flock; the comment becomes accurate only after T5.2 lands).
- **No new code. No schema. No env vars.**

## Acceptance Criteria

After Phase 5 ships:
- `tusk mcp serve` started twice against the same workspace returns
  success both times. Both processes accept queries and mutations.
- `tusk node create` invoked from a shell while an MCP server is
  running completes without waiting on or contending with the MCP
  server.
- Concurrent mutations from CLI and MCP to *different* files run
  truly in parallel.
- Concurrent mutations to the *same* file serialize via the
  `file_state` lease (already covered by Phase 4 tests, but verify
  end-to-end here).
- `internal/lock/lock.go`'s package comment makes explicit that the
  lock is reserved for migrations.
- All existing tests pass; tests that asserted "second MCP startup
  fails with ErrBusy" are updated to assert success.
