# Task 4.4 — Convert `node_move` (`node.Rename`) to `WriteWithLease`

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

Route `node.Rename` (`internal/node/rename.go:57`) through the lease
primitive. Move is the most subtle handler: it touches **two paths** (old
and new) and must hold both leases to atomically rewrite incoming edges
while the file relocates.

## Scope

### Files to modify

- `internal/node/rename.go`
  - `Rename` currently takes a constellation of repos as arguments. It
    must now also accept a `*index.FileStateRepo` and a worker ID, or
    receive them via a struct. Pick whatever fits the project's
    existing pattern; minimize signature churn for callers.
  - Acquire two leases in **deterministic order** (lexicographic by
    path) to avoid two concurrent moves deadlocking — if A moves x→y
    and B moves y→x, they must both acquire `min(x,y)` then `max(x,y)`.
    The implementer codes this ordering explicitly.
  - Acquisition pattern:
    - Insert a placeholder `file_state` row for the destination path
      using `INSERT … ON CONFLICT DO NOTHING` with empty
      `content_hash`, zero `mtime_ns`/`size`, `state = 'live'`,
      `last_seen_gen = 0`. (Move bypasses `WriteWithLease` and so
      must replicate the helper's lazy-create step manually for the
      destination. The source path's row may also be missing on
      pre-existing nodes — apply the same lazy-create there before
      claiming.)
    - Claim leases on both paths in lexicographic order.
  - Mutation:
    - Read the source file via `WriteWithLease`'s Mutator semantics, or
      inline since the helper expects one path. **This is the snag:**
      `WriteWithLease` as designed in T4.1 is single-path. T4.1's
      signature accommodates Tombstone for the source and Replace for
      the destination, but composing two calls into one atomic move
      requires either:
      - (a) a separate `WriteMoveWithLeases` helper in T4.1, or
      - (b) manual composition here using the lease primitives directly.
    - **Decision:** option (b). T4.4 uses the lease primitives directly
      and does not call `WriteWithLease`. Move's two-path nature does
      not generalize and a dedicated helper would have only one caller.
      Document this choice in a comment.
  - The atomic body:
    - Read source file bytes (lease on source held).
    - `os.Rename(srcAbs, dstAbs)` — atomic on the same filesystem.
    - Commit dst lease: `content_hash` = source hash, `mtime_ns` /
      `size` from `os.Stat(dstAbs)`, `state = 'live'`.
    - Commit src lease as tombstone: `state = 'tombstone'`, clear
      `pending_*`, release.
  - Post-rename: update node row's `path`, rewrite incoming edges'
    `source_path`, the existing logic that produces `RenamePlan`. These
    happen *after* the lease releases.

### Tests

- Existing `Rename` tests continue to pass.
- New test: concurrent move of two different files (no path overlap) —
  both succeed in parallel.
- New test: concurrent move of two files that swap names (A→B, B→A) —
  both serialize correctly without deadlock thanks to the lexicographic
  lock ordering.
- New test: source path lease busy → Rename returns `ErrBusy`,
  destination lease never taken.

## Verification

1. `make build`, `make test`, `make vet`, `make lint`, `make fmt` green.
2. Manual smoke: rename a node via the CLI; inspect `file_state` for
   both source (tombstoned) and destination (live with new hash).

## Out of Scope

- Removing the flock — Phase 5.
- A general two-path helper (`WriteMoveWithLeases`) — not justified by a
  single caller.

## Notes for the Implementer

- The lexicographic lock ordering is the standard fix for two-lock
  deadlocks. Document it with a comment naming the deadlock scenario
  it prevents (A→B vs B→A).
- The `Delete` package function (T4.5's target) and `Rename` share a
  file (`internal/node/rename.go`). Do not bundle T4.4 and T4.5 into
  one PR even though they sit next to each other; the per-task PR
  discipline matters more than code locality.
- `os.Rename` is atomic on the same filesystem. The workspace and
  `.tusk/` directory are always on the same filesystem by construction
  (`.tusk/` is a subdirectory of the workspace), so cross-directory
  renames are safe.
