# Phase 3 — Embed queue lease-based draining

**Spec:** `docs/superpowers/specs/2026-05-26-tusk-concurrent-instances.md`
**Plan:** `docs/superpowers/plans/2026-05-26-concurrent-instances/PLAN.md`
**Prereqs:** Phase 2 complete (file_state table, embed_queue lease + kind
columns, lease primitive + worker ID all in place).
**Parallelization:** Sequential. T3.1 → T3.2.

## Inherits From

Phase 2 left in place:
- `embed_queue` table with new columns `leased_by`, `leased_until_ns`,
  `lease_started_at_ns`, `kind` (default `'embed'`).
- `QueueRow` struct extended to read the new columns.
- A worker identity `index.WorkerID()` returning a stable per-process
  UUID.
- The `FileStateRepo.Claim` / `Release` primitive pattern as a reference
  shape — but `EmbedQueueRepo` has its own equivalents to introduce in
  this phase because the commit semantics differ (delete the row on
  success, re-lease on failure).

The workspace flock at MCP startup (`internal/mcp/runtime.go:96-107`) is
still active. Concurrent MCP instances on the same workspace are still
blocked. The lease conversion in this phase prepares for Phase 5 (flock
removal) but does not depend on it.

The `embed.DrainQueue` callers (`internal/embed/drain.go`) currently
expect `EmbedQueueRepo.Drain` to return rows and delete them atomically.
T3.1 changes that contract; callers must be updated in the same PR.

## Execution Rules

1. **One task = one PR.** Two tasks, two PRs.
2. **Each PR independently shippable.** Build green, full Go test suite
   green, lint clean.
3. **No schema changes in this phase.** The columns landed in Phase 2.
4. **Bridge code is acceptable only as named in a task doc.** T3.1
   hardcodes the lease TTL to `60 * time.Second` at the embed-drain call
   site; T3.2 replaces the constant with the configured value. The
   bridge is marked with a comment at the call site.

## Goal

Convert `embed_queue` from "select + delete in one transaction" to
"atomic lease claim" so multiple processes can drain the queue
cooperatively without losing work or double-processing rows. Lease
expiry is the sole crash-recovery mechanism — no heartbeats, no liveness
probes.

After Phase 3:
- A worker calling `Drain` claims a batch of rows by setting `leased_by`
  / `leased_until_ns` atomically; another worker calling `Drain`
  concurrently gets a disjoint batch.
- A worker that successfully embeds a row deletes it (`DELETE WHERE
  node_id = ? AND leased_by = ?`).
- A worker that fails (error or panic) re-leases the row with `attempts
  + 1`; if it crashes, the lease expires after TTL and the row becomes
  claimable again by any worker.
- Lease TTL is configurable via env (`TUSK_LEASE_TTL_SECONDS`) and
  manifest (`lease.ttl_seconds`); T3.1 hardcodes 60s, T3.2 plumbs the
  config.

## Tasks

| #     | Title                                            | Prereqs |
|-------|--------------------------------------------------|---------|
| 3.1   | Convert `EmbedQueueRepo.Drain` to lease claim (Drain + Ack + Nack + Drop + reclamation) | none (Phase 2) |
| 3.2   | TTL configuration (env + manifest)               | T3.1    |

## Changes Introduced

- **Modified contract:** `EmbedQueueRepo.Drain` no longer deletes rows
  on read. It claims them via lease. Callers must call a new
  `EmbedQueueRepo.Ack(nodeID, workerID)` (or equivalent) on success and
  a `Nack(nodeID, workerID, err)` on failure. T3.1 introduces these
  methods.
- **New config:** `TUSK_LEASE_TTL_SECONDS` env var; `lease.ttl_seconds`
  manifest key. Default 60s. T3.2.
- **Modified callers:** `embed.DrainQueue` (`internal/embed/drain.go`)
  updates to the new Ack/Nack flow. T3.1.
- **No new tables, no schema bumps.**

## Acceptance Criteria

After Phase 3 ships:
- `embed_queue` workers in two separate `tusk` processes (run two
  instances of the test binary against a shared workspace, or simulate
  with goroutines in a test) drain disjoint batches; no row is
  double-processed.
- A worker killed mid-embed leaves a row with `leased_by` set and
  `leased_until_ns` in the near future; after TTL elapses, another
  worker's `Drain` reclaims it; the row is processed normally.
- A worker that returns an error from embedding re-queues the row with
  attempts incremented; `Drain` returns the row again on the next call
  (after a small backoff if implemented).
- TTL is configurable via env and manifest; the env value wins when both
  are set.
- All existing embed tests continue to pass; new tests cover the lease,
  reclamation, and Ack/Nack paths.
