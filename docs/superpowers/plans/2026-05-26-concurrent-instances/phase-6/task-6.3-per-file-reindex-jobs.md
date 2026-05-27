# Task 6.3 — Enqueue per-file reindex jobs during walk

**Phase:** 6
**Spec:** `docs/superpowers/specs/2026-05-26-tusk-concurrent-instances.md`
**Plan:** `docs/superpowers/plans/2026-05-26-concurrent-instances/PLAN.md`
**Prereqs:** T6.2 landed.
**Ships as:** one PR.

## Execution Rules

1. **This task = one PR.**
2. **Build green, full test suite green, lint clean.**
3. **Bridge code:** the in-walker per-file work (parse, upsert node row,
   upsert edges, enqueue embed) **stays** in this task. The walker
   *also* enqueues a `kind = 'reindex'` row per file. Both producers
   write the same effects for now. T6.4 introduces the consumer and
   removes the in-walker work. The bridge is marked with a comment in
   `reindex.go`.
4. **No schema changes.**

## Goal

Make the walker enqueue per-file reindex jobs so Phase 6.4 can have a
queue to drain. In this task, the walker continues doing the work
in-process *and* enqueues the jobs — both paths execute, producing the
same effects. The result is wasted CPU (double-work) but a correct,
shippable intermediate state. T6.4 fixes the waste.

The hash-skip dedup in `embed.DrainQueue` already prevents redundant
embedding work, so the duplication cost is bounded to the per-file
upsert SQL, which is cheap.

## Scope

### Files to modify

- `internal/index/embed_queue_repo.go` — add a helper:
  `EnqueueReindex(path string) error` that inserts a row with
  `node_id` set to a deterministic value derived from `path` (the
  spec leaves this open — the implementer chooses; suggested: use the
  path itself as `node_id` for `kind='reindex'` rows since the
  uniqueness is on path within kind, and document this in a comment).
  Insert with `kind = 'reindex'`. Use `INSERT … ON CONFLICT(node_id)
  DO NOTHING` so a path enqueued by two concurrent walks doesn't error.
- `internal/reindex/reindex.go`
  - In the per-file loop, after the existing parse + upsert work, call
    `EnqueueReindex(parsed.Path)`. Mark the surrounding in-process
    work block with: *"// BRIDGE: in-walker per-file work duplicates
    queue-driven worker path; removed in task 6.4."*

### Open question — node_id for reindex jobs

`embed_queue.node_id` is a primary key on the table. For embed jobs
it's the node id (path without extension). For reindex jobs it's the
file path (with extension) — these collide if a node and its file both
end up in the queue under the same node_id. To avoid collision:

**Option A:** Use `node_id = "reindex:" + path` for reindex rows;
strip the prefix when processing. Visible in `tusk_status` output but
clear about kind.

**Option B:** Drop the PRIMARY KEY constraint on `embed_queue.node_id`
and use `(node_id, kind)` as a composite PK instead. Cleaner, but
schema change → another rebuild. Phase 2 didn't anticipate this.

**Decision:** Option A. Keeps Phase 6 schema-change-free; minor cosmetic
oddity in queue listings is acceptable. Document the convention in the
`EnqueueReindex` helper. The `:` is the separator; downstream
prefix-stripping in T6.4 is naïve (`strings.TrimPrefix(nodeID, "reindex:")`).
Real node IDs are derived from file paths minus extension and do not
start with the literal substring `reindex:` in any current codebase
path, but `EnqueueReindex` should panic in debug builds (or return an
error in release builds) if `path` itself starts with `reindex:` —
preserves the round-trip invariant if conventions ever change.

### Tests

- New test: walker enqueues one `kind = 'reindex'` row per `.md`
  file found.
- New test: walking the same workspace twice in succession does not
  produce duplicate reindex rows (idempotency via `ON CONFLICT DO
  NOTHING`).
- Existing reindex tests pass (the in-walker work still runs).

## Verification

1. `make build`, `make test`, `make vet`, `make lint`, `make fmt` green.
2. Smoke: run `tusk reindex`; inspect `embed_queue`; observe both
   `'embed'` rows (from the in-walker enqueue) and `'reindex'` rows
   (new).

## Out of Scope

- A worker that consumes reindex rows — T6.4.
- Removing the in-walker per-file work — T6.4.

## Notes for the Implementer

- The `kind = 'reindex'` rows just sit in the queue until T6.4 ships.
  This is intentional. If T6.3 ships and T6.4 takes a while, the queue
  grows by one row per file per walk. The next reindex's
  `ON CONFLICT DO NOTHING` keeps them de-duplicated, so the queue
  stays bounded by the number of files in the vault, not by time.
- A unit test should verify that filtering `Drain(kind='embed')`
  (from T3.1) does not return the `'reindex'` rows.
- Update `tusk_status` if it surfaces queue depth — distinguish embed
  vs reindex depth in the output. This is small; include it in this
  task's PR.
