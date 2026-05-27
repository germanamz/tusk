# Task 7.2 — Watch disabled when workers = 0

**Phase:** 7
**Spec:** `docs/superpowers/specs/2026-05-26-tusk-concurrent-instances.md`
**Plan:** `docs/superpowers/plans/2026-05-26-concurrent-instances/PLAN.md`
**Prereqs:** T7.1 landed.
**Ships as:** one PR.

## Execution Rules

1. **This task = one PR.**
2. **Build green, full test suite green, lint clean.**
3. **No bridge code. No schema changes.**

## Goal

When the resolved embed-worker count is 0, do not start the watcher
either. The instance becomes a pure read-server: it answers queries,
accepts mutations through normal handlers (which still enqueue, the
queue just doesn't drain in this instance), but does not observe FS
changes.

The reasoning (spec § *Worker configuration*): an instance that
cannot drain the queue should not be producing work for it either. If
the operator hasn't arranged for another draining instance (or a
scheduled reindex), the queue grows without bound — the watcher would
just accelerate that growth.

## Scope

### Files to modify

- `internal/mcp/runtime.go` — at startup, after resolving worker count
  (T7.1), branch:
  - If `workers > 0`: start the watcher as today.
  - If `workers == 0`: skip the watcher's goroutine and its registration
    with the FS notify subsystem.

- `internal/watch/` — likely no change needed; the gate is at the
  runtime layer, not inside the watcher itself. Confirm during
  implementation.

- `cmd/tusk/cmd_watch.go` — this is the CLI `tusk watch` command, used
  by humans to run a watcher process standalone. Different code path
  from the MCP-embedded watcher. The CLI command should also honor the
  same config: if `embed.workers = 0` (or env equivalent), `tusk
  watch` exits immediately with an explanatory error. Rationale: a
  standalone `tusk watch` exists to drive embeds; if there's no drain,
  the command is pointless.

### Tests

- Integration test: MCP runtime with `workers = 0` does not start
  watcher goroutine (assert via goroutine count or by writing to a
  vault file and checking no enqueue happens for that change).
- Integration test: MCP runtime with `workers > 0` starts watcher as
  before (existing tests cover this).
- CLI test: `tusk watch` with `TUSK_EMBED_WORKERS=0` exits with a
  non-zero status and the explanatory message.

## Verification

1. `make build`, `make test`, `make vet`, `make lint`, `make fmt` green.
2. Smoke: `TUSK_EMBED_WORKERS=0 tusk mcp serve`; modify a file in the
   vault from outside the process; confirm no enqueue (queue depth
   via `tusk_status` or direct DB inspection).

## Out of Scope

- The startup WARN log — T7.3.

## Notes for the Implementer

- The MCP runtime's watcher and the CLI `tusk watch` likely share
  underlying machinery in `internal/watch/`. The gate is at the
  startup site of each — do not push the gate into `internal/watch/`
  itself, since the watcher should still be usable as a library by
  any future caller that wants to subscribe regardless of worker
  state.
- Reindex worker pool (from Phase 6) is also gated by the same
  workers=0 config — when workers are off, neither embed nor reindex
  pools run. Document this in code comments at the gate site.
- The CLI `tusk watch` exit message should suggest the alternative:
  *"Watch requires embed workers (TUSK_EMBED_WORKERS > 0). To run a
  watcher-only instance, ensure another tusk instance is draining
  this workspace's index."*
