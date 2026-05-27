# Task 7.3 — Startup `WARN` log when workers disabled

**Phase:** 7
**Spec:** `docs/superpowers/specs/2026-05-26-tusk-concurrent-instances.md`
**Plan:** `docs/superpowers/plans/2026-05-26-concurrent-instances/PLAN.md`
**Prereqs:** T7.2 landed.
**Ships as:** one PR.

## Execution Rules

1. **This task = one PR.**
2. **Build green, full test suite green, lint clean.**
3. **No bridge code. No schema changes.**

## Goal

When MCP starts with workers = 0, emit a single startup `WARN` log
line explaining the consequence and the operator's responsibility. The
log is the only signal — there is no panic, no startup failure, no
heuristic check whether another instance is draining.

## Scope

### Files to modify

- `internal/mcp/runtime.go` — at the same site where T7.2 gated the
  watcher start, also emit the WARN log. Exact message (per spec
  § *Worker configuration*):

  ```
  WARN embed workers disabled; watch is also disabled in this instance.
       Ensure another instance (or scheduled `tusk reindex`) drives
       indexing for this workspace, otherwise the index will go stale.
  ```

  The log goes through whatever logger the runtime uses (likely the
  `rt.Logger` field set via runtime options). Use the existing
  logging infrastructure — do not introduce a new logger.

### Tests

- Integration test: start MCP with `workers = 0`, capture log output
  via the runtime's logger hook, assert the WARN line is present and
  contains the key phrase "embed workers disabled".
- Integration test: start MCP with `workers > 0`, assert the WARN
  line is absent.

## Verification

1. `make build`, `make test`, `make vet`, `make lint`, `make fmt` green.
2. Smoke: `TUSK_EMBED_WORKERS=0 tusk mcp serve` — observe the WARN line
   on stderr (or wherever logs go).

## Out of Scope

- Periodic re-warning during runtime. The spec specifies a startup-only
  warning. Do not add a watchdog that re-emits warnings.
- Auto-detection of other draining instances. The spec explicitly
  rejects this: it would require cross-process coordination that the
  rest of the design avoids.

## Notes for the Implementer

- One line, one time, at startup. Resist the urge to elaborate. The
  spec text is the canonical wording — copy it.
- If the test framework can't easily capture WARN-level logs, look at
  how other startup log assertions work in the codebase (likely there
  are existing test helpers around the logger interface used by
  `mcp.Runtime`).
- This is the final task of the work stream. After it ships, the
  planning agent runs the post-implementation review (see PLAN.md
  § *Lifecycle*).
