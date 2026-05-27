# Phase 4 — WRITE-BOTH handlers via `file_state` leases

**Spec:** `docs/superpowers/specs/2026-05-26-tusk-concurrent-instances.md`
**Plan:** `docs/superpowers/plans/2026-05-26-concurrent-instances/PLAN.md`
**Prereqs:** Phase 2 + Phase 3 complete.
**Parallelization:** T4.1 is required first. T4.2 through T4.5 may run in
parallel after T4.1.

## Inherits From

Phase 2 left in place:
- `file_state` table.
- `FileStateRepo` with CRUD.
- `FileStateRepo.Claim` and `Release` primitives.
- `index.WorkerID()` returning a stable per-process UUID.

Phase 3 left in place:
- `internal/leaseconfig` resolver (`Resolve(manifestTTL int) time.Duration`)
  that reads `TUSK_LEASE_TTL_SECONDS` from env, falls back to manifest
  `lease.ttl_seconds`, then to 60s. Handlers in this phase pass the
  resolved value through to `WriteWithLease` and to direct
  `FileStateRepo.Claim` calls (T4.4). One TTL value applies to both
  `file_state` and `embed_queue` leases per spec § *Lease TTL*.

The workspace flock is still acquired by MCP startup
(`internal/mcp/runtime.go:96-107`) and by CLI mutation commands
(`cmd/tusk/cmd_node_modify.go:73` and siblings). This phase adds the lease
on top of the flock; both protect writes simultaneously, which is
redundant but safe. Phase 5 removes the flock.

The CLI helper `withWorkspaceLock` (referenced in
`cmd/tusk/cmd_node_modify.go:73`) continues to wrap every mutation. The
lease is acquired *inside* that wrapping in this phase.

## Execution Rules

1. **One task = one PR.** Five tasks, five PRs.
2. **Each PR independently shippable.** Build green, full Go test suite
   green, lint clean.
3. **No schema changes in this phase.**
4. **Bridge code:** the workspace flock around each mutation is retained
   throughout this phase. It is **not** bridge code — Phase 5 removes it
   as a distinct deliberate step, not because Phase 4 stubbed it.
5. **`.tusk/staging/` directory** is created lazily on first write by the
   helper in T4.1. The directory is not created in Phase 2 because no
   code consumes it there.

## Goal

Make every WRITE-BOTH handler use the `file_state` lease for FS
coordination, with atomic `.tusk/staging/<uuid>` temp + `os.Rename`.
After this phase, two CLI mutations to the same file (or a CLI mutation
+ MCP mutation) coordinate via the lease — even though concurrent MCP
instances are still flock-blocked from existing at all (Phase 5 unlocks
that).

The write flow per the spec § *Write flow*:

```
1. claim lease on file_state[path]      (auto-cleans stale temp)
2. read file from disk                  (lease guarantees freshness)
3. apply mutation                       (frontmatter delta, move, delete)
4. set pending_temp_path / pending_hash
5. write new content to .tusk/staging/<uuid>
6. os.Rename(temp, target)
7. commit: clear lease, set new content_hash + mtime + size
8. update nodes / edges / embed_queue in normal transactions
```

T4.1 builds the helper that wraps steps 1-7 (and the auto-cleanup at
claim time). T4.2-4.5 thread each handler's mutation logic through it.

## Tasks

| #     | Title                                  | Prereqs |
|-------|----------------------------------------|---------|
| 4.1   | `node.WriteWithLease` helper           | none (Phase 2) |
| 4.2   | Convert `node_create` to `WriteWithLease` | T4.1 |
| 4.3   | Convert `node_modify` to `WriteWithLease` | T4.1 |
| 4.4   | Convert `node_move` (`node.Rename`) to `WriteWithLease` | T4.1 |
| 4.5   | Convert `node_delete` (`node.Delete`) to `WriteWithLease` | T4.1 |

T4.2 through T4.5 are independent of one another and may proceed in
parallel after T4.1 lands. Each touches a different handler in the same
files but different functions; merge conflicts at PR time will be
trivial if any.

## Changes Introduced

- **New helper:** `node.WriteWithLease` (T4.1), composing lease claim +
  staged write + rename + commit.
- **Modified handlers:** `Service.Create`, `Service.Modify`, package-level
  `Delete`, `Rename` — all routed through `WriteWithLease`. Surface
  contracts unchanged from the caller perspective; the difference is
  invisible to MCP/CLI callers as long as the workspace flock still
  protects them.
- **New filesystem path:** `<workspace-root>/.tusk/staging/<uuid>` for
  in-flight temp files. Created lazily by `WriteWithLease`.
- **No schema changes. No env vars. No new manifest keys.**
- **No bridge code introduced.** The workspace flock that's still around
  is not bridge code per the definition; it's removed by Phase 5 as its
  own deliberate change.

## Acceptance Criteria

After Phase 4 ships:
- All four mutation paths (create, modify, move, delete) populate
  `file_state` rows. After a fresh workspace boot + a few mutations,
  `SELECT path, content_hash, leased_by FROM file_state` shows the
  affected paths with their current hashes and NULL lease fields.
- Modify and Delete succeed against **pre-existing nodes** whose
  `file_state` row has not yet been populated. The `WriteWithLease`
  helper lazy-creates the placeholder row inside the same path so the
  `Claim` immediately afterwards finds something to lock. A test
  asserts this: insert a node row + on-disk file directly (no
  `file_state` row), then call `Service.Modify` against it; the
  modify must succeed and the row must exist afterwards.
- A test that calls `Service.Modify` on the same file from two
  goroutines (within one process) shows correct serialization: both
  modifications land, in some order, no data lost.
- A test that simulates a crashed writer by setting
  `pending_temp_path` and writing a stub temp file, then calling
  `Claim` from a new worker, confirms the temp is unlinked and
  `pending_temp_path` cleared.
- All existing handler tests continue to pass unchanged.
- `.tusk/staging/` exists on disk after the first write; it remains
  empty after the next successful write (rename moved the file out).
