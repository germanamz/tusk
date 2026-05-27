# Task 7.1 — Embed-worker config resolution

**Phase:** 7
**Spec:** `docs/superpowers/specs/2026-05-26-tusk-concurrent-instances.md`
**Plan:** `docs/superpowers/plans/2026-05-26-concurrent-instances/PLAN.md`
**Prereqs:** Phase 3 + Phase 6 complete (T6.4 introduces the bridge
constant this task removes).
**Ships as:** one PR.

## Execution Rules

1. **This task = one PR.**
2. **Build green, full test suite green, lint clean.**
3. **No bridge code.**
4. **No schema changes.**

## Goal

Make embed-worker pool size configurable via env and manifest. Define
the resolution order. After this task, pool size is configurable but
the watcher still runs unconditionally — T7.2 wires the watch-disable
behavior.

## Scope

### Files to modify

- `internal/manifest/` — add an `[embed]` section to the manifest
  schema with a `workers` integer field. Validation:
  - Must be `>= 0`.
  - Absent → no error, defaults applied at resolution time.
  - Type-coerce per the project's existing manifest conventions
    (likely TOML integers).

- New file `internal/embedconfig/embedconfig.go` (or extend
  `internal/leaseconfig/` from T3.2 with a sibling helper — keep
  parallel structure):
  - `ResolveWorkers(manifestWorkers int) int` resolution:
    1. Read `TUSK_EMBED_WORKERS` env var; if set and parseable as
       non-negative int, return it.
    2. If manifestWorkers > 0 or `(workers field present and value is
       0)`, return manifestWorkers.
    3. Default: `max(1, runtime.NumCPU() / 2)`.
  - Note: `0` is a valid result (meaning opt-out). Both env and
    manifest must be able to express it. Use pointer or presence-tracking
    to distinguish "absent" from "explicitly 0" in the manifest schema.

- `internal/mcp/runtime.go` (or wherever embed/reindex worker pools are
  started — likely after Phase 6) — read the manifest's
  `embed.workers`, pass through `ResolveWorkers`, use the result as
  the pool size. If 0, skip pool creation entirely (no goroutines
  spawned).

  **Removes bridge code from T6.4:** the hardcoded
  `max(1, runtime.NumCPU() / 2)` constant feeding the reindex worker
  pool. Replace it with the `ResolveWorkers` value so the same config
  governs both the embed pool and the reindex pool. Delete the
  `// BRIDGE: hardcoded reindex worker count …` comment. (Phase 6 is
  now a hard prereq of Phase 7, so the bridge is always present when
  this task runs.)

- Manifest docs: document `[embed] workers` with the default and the
  env override. Document that `0` is a valid value meaning "opt out of
  drain in this instance" and warn that some other instance (or
  scheduled reindex) must drain.

### Tests

- Unit tests for `ResolveWorkers`:
  - Env unset, manifest absent → default.
  - Env unset, manifest `workers = 4` → 4.
  - Env unset, manifest `workers = 0` → 0.
  - Env `TUSK_EMBED_WORKERS=8`, manifest `workers = 4` → 8.
  - Env `TUSK_EMBED_WORKERS=0`, manifest `workers = 4` → 0.
  - Env `TUSK_EMBED_WORKERS=garbage` → warning, falls back to manifest
    then default.
- Manifest test: `[embed] workers = -1` → manifest load error.
- Integration test: MCP runtime with `workers = 0` does not spawn the
  embed worker goroutine. Verify by goroutine count or by enqueueing
  a job and asserting it remains in the queue (this is a stronger
  signal — the queue depth stays at 1 indefinitely).

## Verification

1. `make build`, `make test`, `make vet`, `make lint`, `make fmt` green.
2. `make docs` if manifest docs were regenerated.
3. Smoke: `TUSK_EMBED_WORKERS=0 tusk mcp serve --workspace ...`; enqueue
   an embed job; observe it stays in the queue.

## Out of Scope

- Watch disable in workers=0 mode — T7.2.
- Startup WARN log — T7.3.
- Per-instance worker overrides (instance ID → worker count) — out of
  scope for this work stream.

## Notes for the Implementer

- The default `max(1, runtime.NumCPU() / 2)` lands a sensible value
  on small and large machines alike without operator intervention.
- The env-overrides-manifest precedence is convention; do not invert
  it. If both are set, env wins.
- Test isolation: use `t.Setenv` for env-variable tests; never call
  `os.Setenv` directly.
- The watcher continues to run after this task. A workers=0 instance
  with watch on still enqueues work that won't drain. T7.2 fixes this.
  Do not preemptively disable the watcher here — keep the task narrow.
