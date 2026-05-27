# Task 2.2 — `embed_queue` lease + `kind` columns

**Phase:** 2
**Spec:** `docs/superpowers/specs/2026-05-26-tusk-concurrent-instances.md`
**Plan:** `docs/superpowers/plans/2026-05-26-concurrent-instances/PLAN.md`
**Prereqs:** Phase 1 complete.
**Ships as:** one PR.

## Execution Rules

1. **This task = one PR.**
2. **Build green, tests green, lint clean.**
3. **`EmbedQueueRepo`'s public method signatures stay unchanged** in this
   task. The new columns are added to the table and reflected in
   `QueueRow`, but `Drain` keeps its current select-then-delete shape and
   `Enqueue` / `ReEnqueue` keep their current bodies. Phase 3 (T3.1) is
   the task that flips `Drain` to lease-based.
4. **No bridge code introduced.** The new columns are written by the new
   schema; existing rows are absent (rebuild empties the table).

## Goal

Land the schema delta for `embed_queue`: four new columns (`leased_by`,
`leased_until_ns`, `lease_started_at_ns`, `kind`) per the spec § *Embed
queue lease*. Bump `SchemaVersion` so the rebuild path runs on first open.

The `kind` column carries `'embed'` (default) or `'reindex'`. This task
only writes `'embed'` because no reindex job producer exists yet — that
lands in Phase 6.

## Scope

### Files to modify

- `internal/index/index.go` — locate the `embed_queue` `CREATE TABLE`
  block and add the four columns:
  - `leased_by TEXT`
  - `leased_until_ns INTEGER`
  - `lease_started_at_ns INTEGER`
  - `kind TEXT NOT NULL DEFAULT 'embed'`
  Add an index on `(kind, leased_until_ns)` to make lease-claim queries
  in Phase 3 efficient.

- `internal/index/embed_queue_repo.go`
  - Extend `QueueRow` with `LeasedBy *string`, `LeasedUntilNs *int64`,
    `LeaseStartedAtNs *int64`, `Kind string` fields. Use nullable types
    for the lease columns; `Kind` is non-null with default `'embed'`.
  - Update the `SELECT` in `Drain` to read the new columns into the
    extended `QueueRow`. The select/delete logic itself remains unchanged
    in this task.
  - `Enqueue` and `ReEnqueue` continue to insert without specifying
    `kind`; the column default fills it in. Document this in the function
    comments — Phase 6 adds a separate `EnqueueReindex` helper that sets
    `kind = 'reindex'` explicitly.

- `internal/index/schema_version.go` — bump the `SchemaVersion` constant.
  Suggested: `"2026-05-26-embed-queue-leases"` (any new string works).
  Update the comment.

### Tests

- Extend `internal/index/embed_queue_repo_test.go` (or create if absent):
  - A test that `Enqueue` writes a row with `kind = 'embed'` and NULL
    lease fields.
  - A test that `Drain` returns rows with the new columns populated as
    expected (lease fields nil, kind `'embed'`).
- No new test for lease semantics in this task — the lease primitive
  doesn't exist yet (T2.3).

## Verification

1. `make build`, `make test`, `make vet`, `make lint`, `make fmt` all
   green.
2. The existing `indexopen` rebuild test still passes — a `SchemaVersion`
   mismatch triggers the rebuild and the new columns appear on the
   reconstructed table.

## Out of Scope

- Lease-claim queries (`UPDATE ... RETURNING`) — that's T3.1.
- Worker identity (UUID) — that's T2.3.
- Any change to `embed.DrainQueue` callers
  (`internal/embed/drain.go`) — Phase 3 owns that work.

## Notes for the Implementer

- The spec uses ns-precision Unix timestamps (`*_ns` columns). Match the
  existing convention in `embed_queue_repo.go` which already uses
  `time.Now().UnixNano()` for `enqueued_at`.
- Do **not** add a CHECK constraint to enforce `kind IN ('embed',
  'reindex')`. SQLite CHECK constraints are awkward to evolve; we'll
  rely on the producer side to write valid values.
- The schema-version bump in this task is independent of the one in T2.1.
  If T2.1 lands first, this task's bump string must differ from T2.1's
  string. The implementer should pick the next string at PR time based on
  what's currently in main.
