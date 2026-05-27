# Task 4.5 — Convert `node_delete` (`node.Delete`) to `WriteWithLease`

**Phase:** 4
**Spec:** `docs/superpowers/specs/2026-05-26-tusk-concurrent-instances.md`
**Plan:** `docs/superpowers/plans/2026-05-26-concurrent-instances/PLAN.md`
**Prereqs:** T4.1 landed.
**Ships as:** one PR.

## Execution Rules

1. **This task = one PR.**
2. **Build green, full test suite green, lint clean.**
3. **The workspace flock stays in place.** Phase 5 removes it.
4. **No bridge code.**

## Goal

Route `node.Delete` (`internal/node/rename.go:17`) through
`WriteWithLease` with the Tombstone action. After this task, deleting a
node holds the file_state lease while removing the file from disk and
transitioning the row to `'tombstone'`.

## Scope

### Files to modify

- `internal/node/rename.go`
  - `Delete` currently takes the repo args directly. Add a
    `*index.FileStateRepo` parameter and a worker ID (or pass via a
    struct — match the project's existing pattern).
  - Call `WriteWithLease` with a Mutator that:
    - Confirms `current` is non-empty (file exists). If empty, the
      file is already gone — return `WriteResult{Action:
      WriteNoChange}` and let the helper release the lease cleanly.
    - Otherwise returns `WriteResult{Action: WriteTombstone}`. The
      helper handles the on-disk unlink and the state transition.
  - Post-write: delete the node row (`NodeRepo.DeleteByPath` or
    `DeleteByID` — pick whichever Delete uses today) and cascade-delete
    edges. These happen *after* `WriteWithLease` returns.

### Tests

- Existing `Delete` tests continue to pass.
- New test: Delete a node; confirm `file_state` row exists with
  `state = 'tombstone'` (soft delete per T2.1) and the file is gone
  from disk.
- New test: Delete a node that's already missing from disk (file was
  removed externally between the user's invocation and Delete's
  execution) — succeeds with WriteNoChange; the node row still gets
  cleaned up.
- New test: concurrent Delete + Modify on the same node — they
  serialize via the lease, one runs and the other sees the result.

## Verification

1. `make build`, `make test`, `make vet`, `make lint`, `make fmt` green.

## Out of Scope

- Removing the flock — Phase 5.
- Reaping tombstone rows from `file_state` — not part of this work
  stream; the rows live on as historical record.

## Notes for the Implementer

- The Tombstone action in `WriteWithLease` (T4.1) is what does the
  `os.Remove` of the file and sets `file_state.state = 'tombstone'`
  via `FileStateRepo.Tombstone` (soft delete per T2.1). Do not
  duplicate that work in Delete's body.
- The cascade-delete of edges runs *after* the lease releases. There's
  a brief window where the file is gone and the node row is gone but
  edge rows still reference it; SQLite's foreign-key cascade should
  handle this if FKs are enabled (they are — see spec § *Background*).
  Verify and rely on the FK rather than reimplementing.
- Don't try to coordinate Delete with Modify across files (e.g., delete
  A while edges from B point to A). That's edge-level work, not
  file-level, and lives outside this concurrency design.
