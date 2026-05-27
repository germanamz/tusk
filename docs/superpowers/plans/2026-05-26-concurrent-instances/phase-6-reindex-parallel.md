# Phase 6 — Reindex parallelization

**Spec:** `docs/superpowers/specs/2026-05-26-tusk-concurrent-instances.md`
**Plan:** `docs/superpowers/plans/2026-05-26-concurrent-instances/PLAN.md`
**Prereqs:** Phase 4 + Phase 5 both complete.
**Parallelization:** Sequential. T6.1 → T6.2 → T6.3 → T6.4.

## Inherits From

Phase 4: every handler populates `file_state` via the lease primitive.
Phase 5: the workspace flock is gone for runtime operations. The
`internal/lock` package is reserved for schema migrations.

Reindex today (`internal/reindex/reindex.go`):
- Single-process. Walks the workspace, parses every `.md`, upserts
  node + edge rows, enqueues for embed (one big function).
- Tracks `seenPaths` in memory (line 106), then post-walk deletes any
  row whose path isn't in the set (lines 417-439). This is the orphan-
  reap race the spec calls out as P4.
- Runs under the workspace flock today (until Phase 5 removes that).
  After Phase 5, two reindexes could race each other.

`embed_queue` has the `kind` column from Phase 2. No producer writes
`kind = 'reindex'` yet — Phase 6 introduces that producer.

## Execution Rules

1. **One task = one PR.** Four tasks, four PRs.
2. **Each PR independently shippable.** Build green, full test suite
   green, lint clean.
3. **No schema changes in this phase.** All needed columns and tables
   landed in Phase 2.
4. **Bridge code:** T6.3 introduces per-file reindex job enqueuing while
   the existing in-memory walker is still operating. T6.4 replaces the
   in-memory walker with the queue-driven worker pool and removes the
   bridge. The bridge is documented at the producer site.

## Goal

Replace the single-process reindex with a generation-based, queue-driven
model. After this phase:

- A `reindex_gen` counter in the `meta` table is bumped at the start of
  every walk.
- The walker updates `file_state.last_seen_gen` for every file it
  encounters.
- Orphan reaping uses generation-based detection plus `os.Stat`
  re-verification, replacing the in-memory `seenPaths` map.
- Walks produce per-file reindex jobs (`embed_queue.kind = 'reindex'`)
  that any worker in any MCP instance can claim and process.
- The walk itself remains lightweight; the heavy per-file work
  (parsing, upserting, enqueueing for embed) moves to worker draining
  the reindex queue.

Two concurrent reindex walks become safe. Reindex stops being a single
critical section.

## Tasks

| #     | Title                                                    | Prereqs |
|-------|----------------------------------------------------------|---------|
| 6.1   | `reindex_gen` counter + per-file `last_seen_gen` updates during walk | none (Phase 4+5) |
| 6.2   | Generation-based reap with `os.Stat` confirmation        | T6.1    |
| 6.3   | Enqueue per-file reindex jobs during walk                | T6.2    |
| 6.4   | Parallel worker drain of reindex jobs                    | T6.3    |

## Changes Introduced

- **New meta key:** `reindex_gen` (monotonic int64, stored in
  `MetaRepo`).
- **Modified walker:** instead of in-memory `seenPaths`, the walker
  bumps `reindex_gen` at start and writes `last_seen_gen` per file
  (T6.1).
- **Modified reap:** generation-based + re-stat (T6.2).
- **New producer:** walker enqueues `kind = 'reindex'` rows (T6.3).
- **New consumer:** worker pool drains `kind = 'reindex'` rows (T6.4).
- **Modified user-visible surface:** `tusk_status` queue-depth output
  distinguishes `embed` vs `reindex` rows (T6.3).
- **No schema changes.** All columns landed in Phase 2.
- **Bridge code:** T6.3 keeps the in-memory walker doing the actual
  reindex work while also enqueueing jobs. T6.4 makes the queue the
  source of truth and removes the in-walker work. The bridge lives
  between T6.3 and T6.4.
- **Bridge code (cross-phase):** T6.4 starts the new reindex worker
  pool with a hardcoded `max(1, runtime.NumCPU() / 2)` constant
  because Phase 7 may not have shipped yet. Phase 7's T7.1 replaces
  this constant with the resolved value from `embedconfig.ResolveWorkers`
  (the same resolver that governs embed-worker count). Removal target:
  **T7.1**.

## Acceptance Criteria

After Phase 6 ships:
- Two concurrent `tusk reindex` invocations against the same workspace
  complete without deleting each other's just-walked rows.
- A reindex started while live mutations are happening does not delete
  newly-created nodes (the orphan-reap race P4 from the spec is closed).
- Reindex throughput on a multi-MCP-instance workspace scales with
  worker count.
- `embed_queue` contains a mix of `kind = 'embed'` and `kind =
  'reindex'` rows during reindex; both kinds drain to completion.
- `tusk_status` reports queue depth broken out by kind (embed vs
  reindex).
- All existing reindex tests pass; new tests cover the concurrent walk,
  the orphan-reap fix, and the queue-driven worker path.
