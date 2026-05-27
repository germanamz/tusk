# Task 6.4 — Parallel worker drain of reindex jobs

**Phase:** 6
**Spec:** `docs/superpowers/specs/2026-05-26-tusk-concurrent-instances.md`
**Plan:** `docs/superpowers/plans/2026-05-26-concurrent-instances/PLAN.md`
**Prereqs:** T6.3 landed.
**Ships as:** one PR.

## Execution Rules

1. **This task = one PR.**
2. **Build green, full test suite green, lint clean.**
3. **Removes bridge code** from T6.3: the in-walker per-file work block
   is deleted. The walker now only walks + enqueues; the queue is the
   single source of work.
4. **No schema changes.**

## Goal

Introduce a worker pool that drains `kind = 'reindex'` rows from
`embed_queue`, performing the per-file work (parse, upsert node row,
upsert edges, enqueue for embed) that previously ran inside the walker.
Workers in any MCP instance can participate; cross-process parallelism
falls out of the existing lease primitive.

## Scope

### Files to add

- `internal/reindex/worker.go` (or similar) — a worker pool that:
  - Calls `EmbedQueueRepo.DrainReindex(workerID, batch, ttl)` to claim
    a batch of reindex rows.
  - For each row, derive the file path from the node_id (per T6.3's
    convention — strip the `"reindex:"` prefix).
  - Parse the file, upsert node + edges, enqueue an `'embed'` row for
    the node. This is the same per-file work that T6.3's bridge was
    doing in-walker.
  - Call `Ack` on success (deletes the reindex row) or `Nack` on
    failure (re-leases with attempts++ and `last_error`). This reuses
    Phase 3's machinery.
  - Worker count: reuse Phase 7's config (when 7 lands; in the
    meantime, default to `max(1, runtime.NumCPU() / 2)` with a
    constant in code). **Bridge code, removal target: T7.1.** Mark
    the constant with a comment: `// BRIDGE: hardcoded reindex
    worker count; replaced by embedconfig.ResolveWorkers in task 7.1.`

### Files to modify

- `internal/index/embed_queue_repo.go` — rename T3.1's `Drain` to
  `DrainEmbed` (it already filters `kind='embed'`) and add a sibling
  `DrainReindex(workerID, batch, ttl)` that filters `kind='reindex'`.
  Both call a private helper that takes the kind as a parameter so the
  SQL exists once. Update T3.1's caller in `internal/embed/drain.go`
  to call `DrainEmbed`; the new reindex worker calls `DrainReindex`.
  Ack/Nack/Drop stay kind-agnostic (they key on `node_id` and
  `leased_by`).
- `internal/reindex/reindex.go`
  - Delete the in-walker per-file work block that T6.3 left in place
    (marked with the bridge comment). The walker now only walks +
    enqueues reindex jobs + updates `file_state.last_seen_gen`.
  - The reap pass (from T6.2) stays.
  - After the walk + reap, the walker either (a) waits for the
    reindex queue to drain to zero (synchronous mode, used by `tusk
    reindex` CLI), or (b) returns immediately and lets background
    workers handle the queue (async mode, used by `tusk watch`).
    Add an option struct or boolean to control this.

- `internal/mcp/runtime.go` — start the reindex worker pool on MCP
  startup (alongside the embed worker pool from Phase 3). Pool size
  follows the same config as embed workers; if Phase 7 hasn't shipped
  yet, use the constant.

### Tests

- New test: walker enqueues N reindex jobs; worker pool drains them;
  after drain, node rows match what the old in-walker work would have
  produced for the same files.
- New test: two MCP instances, both with workers; walker in instance A
  enqueues; both instances' workers drain in parallel; correct number
  of node rows ends up in the DB.
- New test: worker crash mid-reindex (simulated via context cancel)
  leaves the row leased; after TTL, another worker reclaims and
  processes it.
- Existing reindex integration tests pass with the queue-driven flow.

## Verification

1. `make build`, `make test`, `make vet`, `make lint`, `make fmt` green.
2. Smoke: `tusk reindex` against a vault with 1000+ markdown files;
   compare wall-clock time before and after this task. Should be
   noticeably faster with multiple workers.

## Out of Scope

- Worker opt-out / pool size config from manifest — Phase 7.
- Coalescing concurrent walks — generation-based reap already makes
  them safe; coalescing is a future efficiency optimization.

## Notes for the Implementer

- `DrainEmbed` / `DrainReindex` share a single private SQL helper that
  takes the kind. Don't repeat the lease query.
- The synchronous-vs-async mode is important for tests: most tests
  want synchronous so they can assert "after Run returns, the work is
  done." The `tusk reindex` CLI also wants synchronous. The `tusk
  watch` debounced reindex wants async (it should return quickly and
  let workers finish in the background).
- Worker pool startup adds a small permanent goroutine cost to every
  MCP instance. Document that the worker is opt-out via Phase 7's
  config (or the constant for now). After Phase 7 lands, workers=0
  bypasses pool startup.
- The bridge removal in this task is the largest delete of the work
  stream. Verify against `internal/reindex/reindex.go`'s git blame
  that you're removing exactly what T6.3 introduced and nothing more.
