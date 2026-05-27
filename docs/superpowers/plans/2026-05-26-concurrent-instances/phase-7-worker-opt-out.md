# Phase 7 — Worker opt-out and watch disable

**Spec:** `docs/superpowers/specs/2026-05-26-tusk-concurrent-instances.md`
**Plan:** `docs/superpowers/plans/2026-05-26-concurrent-instances/PLAN.md`
**Prereqs:** Phase 3 + Phase 6 complete. Phase 3 ships the embed
worker pool this phase governs; Phase 6 ships the reindex worker pool
and the bridge constant (T6.4) that T7.1 removes. Phases 4-5 are not
required.
**Parallelization:** Sequential. T7.1 → T7.2 → T7.3.

## Inherits From

Phase 3: embed workers exist as goroutines configured by a constant (or
manifest TTL only — no worker-count config yet). The pool size is
hardcoded somewhere in the runtime setup.

Phase 6: a reindex worker pool exists alongside the embed worker pool
(T6.4 starts it with a hardcoded constant marked as bridge code). This
phase's config governs both pools; T7.1 removes the bridge constant.

The watch system (`internal/watch/` and `cmd/tusk/cmd_watch.go`) runs in
every MCP instance unconditionally, producing enqueue events for FS
changes.

## Execution Rules

1. **One task = one PR.** Three tasks, three PRs.
2. **Each PR independently shippable.** Build green, full test suite
   green, lint clean.
3. **No schema changes.**
4. **No bridge code.**

## Goal

Let operators turn an MCP instance into a pure read-server. The mode is
explicit: setting `embed.workers = 0` (manifest) or
`TUSK_EMBED_WORKERS=0` (env) disables embed workers, also disables the
watcher in that instance, and emits a startup `WARN` log explaining the
consequence.

Operators are responsible for ensuring at least one instance (or a
scheduled `tusk reindex`) keeps the index fresh. The MCP server makes
no attempt to detect or coordinate this across instances.

## Tasks

| #     | Title                                          | Prereqs |
|-------|------------------------------------------------|---------|
| 7.1   | Embed-worker config resolution (env + manifest + default) | none (Phase 3) |
| 7.2   | Watch disabled when workers = 0                | T7.1    |
| 7.3   | Startup `WARN` log when workers disabled       | T7.2    |

## Changes Introduced

- **New env var:** `TUSK_EMBED_WORKERS` — integer ≥ 0. T7.1.
- **New manifest key:** `embed.workers` — integer ≥ 0. T7.1.
- **Modified behavior:** when workers = 0, the watcher does not start.
  T7.2.
- **New log:** startup `WARN` line in workers=0 mode. T7.3.
- **Modified docs:** manifest docs and any operator-facing config
  reference. T7.1.

## Acceptance Criteria

After Phase 7 ships:
- Default behavior unchanged: workers run, watcher runs, log is quiet.
- `TUSK_EMBED_WORKERS=0 tusk mcp serve` starts a process that responds
  to queries and mutations but does not run the embed worker pool or
  the watcher.
- A startup log entry at WARN level explains the consequence in
  workers=0 mode (no watcher, no embed drain, operator's responsibility
  to drive indexing elsewhere).
- Env value `TUSK_EMBED_WORKERS=3` overrides manifest `embed.workers
  = 8`; resulting pool has 3 workers.
- Manifest validation rejects `embed.workers = -1`.
- Existing tests pass; new tests cover the config resolution and the
  watch-disable behavior.
