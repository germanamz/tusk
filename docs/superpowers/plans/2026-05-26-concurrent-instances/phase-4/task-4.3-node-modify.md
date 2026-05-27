# Task 4.3 — Convert `node_modify` to `WriteWithLease`

**Phase:** 4
**Spec:** `docs/superpowers/specs/2026-05-26-tusk-concurrent-instances.md`
**Plan:** `docs/superpowers/plans/2026-05-26-concurrent-instances/PLAN.md`
**Prereqs:** T4.1 landed. Phase 1 already narrowed `node_modify` to
frontmatter-only.
**Ships as:** one PR.

## Execution Rules

1. **This task = one PR.**
2. **Build green, full test suite green, lint clean.**
3. **The workspace flock stays in place.** Phase 5 removes it.
4. **No bridge code.**

## Goal

Route `Service.Modify` (`internal/node/service.go:341`) through
`WriteWithLease`. Modify is the simplest WRITE-BOTH conversion: the
target file exists, the mutation is a frontmatter delta against current
content, and the body passes through untouched (per Phase 1).

## Scope

### Files to modify

- `internal/node/service.go`
  - In `Service.Modify`, replace the direct file read + write block
    (currently around lines 350-394 — read the file, parse, apply
    set/unset, render, write) with a call to `WriteWithLease`. The
    Mutator:
    - Receives the on-disk bytes.
    - Parses frontmatter.
    - Applies `input.SetProps` / `input.UnsetKeys`.
    - Renders new content.
    - If the rendered content equals `current` byte-for-byte (e.g.,
      setting a property to the value it already has), return
      `WriteResult{Action: WriteNoChange}`.
    - Otherwise returns `WriteResult{NewBytes: rendered, NewHash: ...,
      Action: WriteReplace}`.
  - Post-write work — update the node row (properties, edges), enqueue
    for embed — stays the same and runs *after* `WriteWithLease`
    returns successfully. The validation logic
    (workflow/property/ref) runs *inside* the Mutator on the parsed
    after-state, before the helper writes anything. If validation
    fails, the Mutator returns an error; the helper releases the
    lease without writing.

### Tests

- All existing Modify tests continue to pass.
- New test: two concurrent `Service.Modify` calls on the same node
  setting different properties — both land, no property lost.
- New test: Modify with a no-op delta (set property to its current
  value) does not touch the file's mtime and does not enqueue for
  embed (because content_hash didn't change).

## Verification

1. `make build`, `make test`, `make vet`, `make lint`, `make fmt` green.

## Out of Scope

- Removing the flock — Phase 5.
- Any change to the watcher's interaction with Modify writes.

## Notes for the Implementer

- The validation path in current Modify
  (`workflow.Error`, `node.PropertyValidationError`,
  `node.RefValidationError`) must surface through the Mutator. The
  helper returns whatever error the Mutator returns; the handler in
  MCP/CLI then matches on those error types as it does today.
- The no-op detection is important. Without it, every Modify would
  bump mtime and re-enqueue embed work even when nothing changed —
  wasteful and noisy.
- Per the spec § *Optimistic concurrency*, do **not** add any
  content_hash CAS on commit. The lease is the entire concurrency
  protection.
