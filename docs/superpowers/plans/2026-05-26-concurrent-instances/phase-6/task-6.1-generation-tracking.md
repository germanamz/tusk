# Task 6.1 — `reindex_gen` counter + per-file `last_seen_gen` updates

**Phase:** 6
**Spec:** `docs/superpowers/specs/2026-05-26-tusk-concurrent-instances.md`
**Plan:** `docs/superpowers/plans/2026-05-26-concurrent-instances/PLAN.md`
**Prereqs:** Phase 4 + Phase 5 complete.
**Ships as:** one PR.

## Execution Rules

1. **This task = one PR.**
2. **Build green, full test suite green, lint clean.**
3. **The existing in-memory `seenPaths` reap stays in place** for this
   task. T6.2 replaces it. Build green throughout.
4. **No schema changes.** `file_state.last_seen_gen` landed in Phase 2.
   The `meta` table already exists in the base codebase (see
   `internal/index/meta_repo.go`); T6.1 only adds a new key
   (`reindex_gen`) to it.

## Goal

Introduce a monotonic reindex generation counter and have the walker
write `last_seen_gen` on every `file_state` row it touches. This sets up
T6.2's generation-based reap without altering reap behavior yet.

## Scope

### Files to modify

- `internal/reindex/reindex.go`
  - At the start of `Run`, atomically bump `reindex_gen` in `MetaRepo`:
    read the current value (parse as int64, treat missing/empty as 0),
    add 1, store. Wrap the read+write in a single transaction so
    concurrent walks each get a distinct generation. Capture the new
    value in a local variable for use during the walk.
  - In the per-file parse loop (around line 407 where `seenPaths` is
    written today), also Upsert the `file_state` row for that path
    with:
    - `content_hash` from the parsed file
    - `mtime_ns`, `size` from `os.Stat`
    - `state = 'live'`
    - `last_seen_gen = <current walk's gen>`
    - `updated_at_ns = now`
  - Use `INSERT … ON CONFLICT(path) DO UPDATE SET …` so existing rows
    refresh their `last_seen_gen` and rows for newly-discovered files
    appear.
  - Keep the existing `seenPaths[parsed.Path] = struct{}{}` line — the
    reap still uses it. T6.2 removes it.

- `internal/index/meta_repo.go` — no change required if `MetaRepo`
  already supports atomic increment-style operations. If it only has
  Get/Set, the implementer adds a helper:
  `Incr(key string) (int64, error)` that does the read+write under a
  transaction. Document the helper.

### Files to leave alone

- Reap logic (`internal/reindex/reindex.go:417-439`) — stays as-is for
  this task.
- Handler code — no change.

### Tests

- Existing reindex tests continue to pass.
- New test: invoke `Run` twice; assert `reindex_gen` is 2 after the
  second call (1 after the first).
- New test: after `Run`, every node's `file_state` row has
  `last_seen_gen` equal to the walk's generation.
- New test: two concurrent `Run` invocations (via goroutines in a test)
  produce two distinct generations and both succeed.

## Verification

1. `make build`, `make test`, `make vet`, `make lint`, `make fmt` green.

## Out of Scope

- Replacing the in-memory `seenPaths` reap — T6.2.
- Enqueueing reindex jobs — T6.3.
- Worker drain of reindex jobs — T6.4.

## Notes for the Implementer

- The `reindex_gen` is a single global counter per workspace, not
  per-process. Concurrent walks each get their own value via the
  atomic increment.
- `int64` is more than enough range — even a walk per second for
  thousands of years won't overflow.
- The `MetaRepo.Get` for `reindex_gen` on a fresh workspace returns
  empty; treat that as 0 and bump to 1.
- The `file_state` upsert pattern here is the same one Phase 4
  handlers introduced. Reuse the same `FileStateRepo.Upsert` method —
  do not introduce a parallel method.
