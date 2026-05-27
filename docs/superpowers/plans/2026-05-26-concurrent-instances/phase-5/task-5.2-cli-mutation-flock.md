# Task 5.2 — Remove `withWorkspaceLock` from CLI mutation commands

**Phase:** 5
**Spec:** `docs/superpowers/specs/2026-05-26-tusk-concurrent-instances.md`
**Plan:** `docs/superpowers/plans/2026-05-26-concurrent-instances/PLAN.md`
**Prereqs:** Phase 3 + Phase 4 complete; T5.1 landed (so the package
comment update can describe the actual post-state).
**Ships as:** one PR.

## Execution Rules

1. **This task = one PR.**
2. **Build green, full test suite green, lint clean.**
3. **No bridge code.** Each removal site is unconditional.
4. **The `withWorkspaceLock` helper itself stays** in the codebase. The
   `tusk_doctor` migration path (or any future migration command) keeps
   using it. This task removes only the call sites that wrap normal
   runtime mutations.

## Goal

Stop CLI mutation commands from acquiring the workspace flock. After
this task, CLI mutations coordinate via the per-file lease (added in
Phase 4) just like MCP mutations.

## Scope

### Files to modify

Confirmed call sites (grep result):
- `cmd/tusk/cmd_node_create.go`
- `cmd/tusk/cmd_node_modify.go`
- `cmd/tusk/cmd_node_move.go`
- `cmd/tusk/cmd_node_delete.go`
- `cmd/tusk/cmd_edge_add.go`
- `cmd/tusk/cmd_edge_remove.go`
- `cmd/tusk/cmd_watch.go`
- `cmd/tusk/cmd_reindex.go`
- `cmd/tusk/cmd_doctor.go`
- `cmd/tusk/cmd_context.go`
- `cmd/tusk/cmd_run.go`
- `cmd/tusk/cmd_node.go`

For each: unwrap the `withWorkspaceLock(ws, func() error { ... })`
pattern to inline the body. The function it wraps stays; only the
wrapping is removed.

**Exception:** `cmd_doctor.go` may keep its flock if the doctor command
runs schema migrations or other workspace-wide reorganizations. The
implementer must check: if doctor only performs operations that already
coordinate via lease (workflow drift detection, property drift writes,
etc.), remove the flock. If it runs anything that requires exclusion
beyond what leases provide, leave the flock and document why.

### Package comment update

- `internal/lock/lock.go` — update the package comment to make explicit
  that the lock is now reserved for schema migrations only. Suggested
  new comment: *"Package lock provides an advisory cross-process
  workspace write lock backed by a flock at .tusk/lock. As of the
  concurrent-instances work stream, this lock is used only by schema
  migrations — runtime mutations coordinate via the per-file lease in
  `internal/index` (file_state) and the per-job lease on
  `embed_queue`."* This update belongs to T5.2 (not T5.1) because the
  CLI flock callers removed here are the last runtime callers of the
  lock; until they're gone, the comment would be inaccurate.

### Files to leave alone

- `cmd/tusk/cmd_lock_test.go` — leave alone unless it asserts CLI-vs-
  CLI lock contention; in that case update for the new behavior (no
  contention).
- `internal/lock/lock.go` body and exports — stay.
- `cmd/tusk/withWorkspaceLock` helper itself (likely in
  `cmd_node.go` or a util file — find it) — the helper stays in the
  codebase. The `tusk_doctor` migration use (if retained) keeps
  calling it; other future commands may need it.

### Tests

- Update any test that asserts a CLI command waits on the flock or
  fails with `ErrBusy` due to another process holding the workspace
  lock. They should now succeed.
- `cmd/tusk/cmd_lock_test.go` likely needs updates — review its
  assertions and update or remove as appropriate.

## Verification

1. `make build`, `make test`, `make vet`, `make lint`, `make fmt` green.
2. Manual smoke: with `tusk mcp serve` running, invoke
   `tusk node create some/file.md --type note` from a shell. It
   should complete without contending with MCP.
3. Two concurrent CLI mutations to different files run in parallel
   (timeable in a shell script with `&` and `wait`).

## Out of Scope

- Removing the `internal/lock` package — stays.
- Removing the `withWorkspaceLock` helper from `cmd/tusk/` — stays for
  migration paths.

## Notes for the Implementer

- The grep at planning time enumerated every CLI file that imports the
  lock helper. Re-grep before submitting in case any new commands have
  been added since planning.
- For commands that don't actually mutate (e.g., `cmd_context.go`,
  `cmd_run.go`), check whether the flock was being acquired even though
  no mutation happens. If so, removing the flock there is a pure win.
- The `internal/typepacks/pack.go` grep hit is unrelated to the
  workspace lock; do not touch it.
