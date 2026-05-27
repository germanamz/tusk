# Task 3.1 — Convert `EmbedQueueRepo.Drain` to lease claim

**Phase:** 3
**Spec:** `docs/superpowers/specs/2026-05-26-tusk-concurrent-instances.md`
**Plan:** `docs/superpowers/plans/2026-05-26-concurrent-instances/PLAN.md`
**Prereqs:** Phase 2 complete.
**Ships as:** one PR.

## Execution Rules

1. **This task = one PR.**
2. **Build green, full Go test suite green, lint clean.** This task
   changes a public contract (`Drain` no longer deletes rows). All
   callers and tests must be updated in the same PR — partial conversion
   is not acceptable.
3. **Bridge code:** none. The `Ack`/`Nack` methods introduced here are
   net-new; callers are updated to use them immediately. The Phase 5
   flock removal is what unlocks concurrent claims actually competing,
   but the lease mechanism must work correctly even under the existing
   single-process flock.

## Goal

Replace the existing `Drain` body (`internal/index/embed_queue_repo.go:67-114`)
with an atomic lease-claim query. Introduce `Ack` (success → delete row)
and `Nack` (failure → release lease, increment attempts, set
`last_error`). Update `embed.DrainQueue`
(`internal/embed/drain.go`) and any other callers to use the new flow.

Per-row reclamation on expiry happens automatically through the
`Drain` query's `WHERE leased_until_ns < :now` predicate — no separate
sweep.

## Scope

### Files to modify

- `internal/index/embed_queue_repo.go`
  - Replace the `Drain` implementation. New signature:
    `Drain(workerID string, batchSize int, ttl time.Duration) ([]QueueRow, error)`.
    The query mirrors the spec § *Embed queue lease*:
    `UPDATE embed_queue SET leased_by = :worker, leased_until_ns = :now
    + :ttl, lease_started_at_ns = :now WHERE node_id IN (SELECT node_id
    FROM embed_queue WHERE kind = 'embed' AND (leased_by IS NULL OR
    leased_until_ns < :now) ORDER BY enqueued_at ASC LIMIT :batch)
    RETURNING node_id, enqueued_at, attempts, last_error, kind`.
    Filter on `kind = 'embed'` — Phase 6's `'reindex'` rows are handled
    by a separate worker path.
  - Add `Ack(nodeID, workerID string) error`: `DELETE FROM embed_queue
    WHERE node_id = ? AND leased_by = ?`. The `leased_by` predicate
    guards against acking a row whose lease has expired and been
    re-claimed by another worker — in that case Ack is a no-op (zero
    rows affected; return nil, not an error).
  - Add `Nack(nodeID, workerID string, embedErr error) error`: `UPDATE
    embed_queue SET leased_by = NULL, leased_until_ns = NULL,
    lease_started_at_ns = NULL, attempts = attempts + 1, last_error = ?
    WHERE node_id = ? AND leased_by = ?`. Same lease-guard semantics.
  - Update `Enqueue` and `ReEnqueue` to be explicit that `kind` falls
    back to the column default `'embed'`. (No behavior change; just a
    comment-level clarification.)

- `internal/embed/drain.go`
  - Wherever `Drain` is called (likely once, returning rows), replace
    with the new signature. Pass `index.WorkerID()` and the configured
    TTL.
  - On embed success, call `Ack(nodeID, workerID)`.
  - On embed error, call `Nack(nodeID, workerID, err)`.
  - The existing `ReEnqueue` path (used today for failed nodes inside
    `Drain`'s caller) is replaced by `Nack`. Remove the `ReEnqueue` call
    site here; the method itself stays for the rebuild/reindex flow in
    Phase 6.
  - The `MaxEmbedAttempts` cap (currently 3) is preserved: if `attempts
    + 1` would reach the cap, call a new helper `EmbedQueueRepo.Drop`
    (or inline a `DELETE`) instead of `Nack` to remove the row
    permanently. Add this method.

- TTL value: until T3.2 lands the config plumbing, this task hardcodes
  TTL to `60 * time.Second` at the call site in
  `internal/embed/drain.go`. T3.2 replaces the constant with the
  configured value. **Bridge code, removal target: T3.2.**

### Tests

- Extend `internal/index/embed_queue_repo_test.go`:
  - `Drain` with no rows returns empty.
  - `Drain` with N unleased rows claims `min(N, batchSize)` of them and
    sets `leased_by` / `leased_until_ns`.
  - A second `Drain` call with a different worker ID returns rows that
    do not overlap with the first.
  - `Drain` skips rows whose `leased_until_ns` is in the future and
    `leased_by` is a different worker.
  - `Drain` reclaims a row whose `leased_until_ns` is in the past.
  - `Ack` deletes the row when called with the holding worker ID;
    no-op when called with a stale worker ID (lease moved on).
  - `Nack` increments `attempts`, sets `last_error`, clears lease fields.
  - `Drop` removes a row that's hit the attempts cap.
  - `Drain` filters out `kind = 'reindex'` rows (insert one such row
    directly into the DB; assert it's not returned).

- Update `internal/embed/drain_test.go` (or equivalent) to use the new
  Ack/Nack flow.

## Verification

1. `make build`, `make test`, `make vet`, `make lint` all green.
2. Run a manual two-worker test: in two goroutines (or via the test
   binary), enqueue 100 rows and have both workers Drain in a loop. Each
   row should be processed exactly once.

## Out of Scope

- TTL configuration — T3.2.
- Reindex job draining (`kind = 'reindex'`) — Phase 6.
- Removing the workspace flock — Phase 5.

## Notes for the Implementer

- The `RETURNING` clause needs the same column set as the old `SELECT`
  in Drain — `node_id, enqueued_at, attempts, last_error` — plus `kind`
  for clarity even though this task only ever returns `'embed'`.
- The `(kind, leased_until_ns)` index that makes this claim query
  efficient is added by T2.2 unconditionally. If you find it missing,
  the gap belongs to T2.2 — do not add it here; T3.1 must not change
  the schema (see Phase 3 rule 3).
- The `Drain` method's caller in `internal/embed/drain.go` should
  preserve its existing batch-size handling. Don't change the default
  batch size as part of this task.
- The 60s TTL hardcode is a deliberate bridge. Mark it with a comment:
  `// TTL: bridged constant, replaced in task 3.2`.
