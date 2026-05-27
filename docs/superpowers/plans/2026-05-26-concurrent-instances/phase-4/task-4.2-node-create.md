# Task 4.2 — Convert `node_create` to `WriteWithLease`

**Phase:** 4
**Spec:** `docs/superpowers/specs/2026-05-26-tusk-concurrent-instances.md`
**Plan:** `docs/superpowers/plans/2026-05-26-concurrent-instances/PLAN.md`
**Prereqs:** T4.1 landed.
**Ships as:** one PR.

## Execution Rules

1. **This task = one PR.** Other handler conversions (T4.3-4.5) ship
   separately.
2. **Build green, full test suite green, lint clean.**
3. **The workspace flock stays in place.** Both flock and lease protect
   the create; Phase 5 removes the flock.
4. **No bridge code.**

## Goal

Route `Service.Create` (`internal/node/service.go:150`) through
`WriteWithLease` so the file_state row is populated and the lease
coordinates concurrent creates on the same path.

`node_create` is unique among WRITE-BOTH handlers in that the target
file *does not exist yet*. The lease primitive expects a `file_state`
row to claim. The conversion must handle the "no row yet" case.

## Scope

### Files to modify

- `internal/node/service.go`
  - In `Service.Create`, call `WriteWithLease` directly. T4.1's
    helper already lazy-creates a placeholder `file_state` row via
    `INSERT … ON CONFLICT DO NOTHING` before claiming the lease, so
    Create does not pre-insert the row itself.
  - The Mutator:
    - If `current` is non-empty (file exists), return an error — Create
      against an existing path is invalid.
    - Otherwise render the new file content (frontmatter + body from
      CreateInput) and return `WriteResult{NewBytes: rendered,
      NewHash: hashOf(rendered), Action: WriteReplace}`.
  - After successful `WriteWithLease`, perform the existing post-write
    work: upsert node row, edge rows, enqueue for embedding. These
    happen *outside* the lease (the lease's only job is the FS write).

### Files to leave alone (mostly)

- `internal/mcp/tools.go` `registerNodeCreateTool` — no change to MCP
  surface.
- `cmd/tusk/cmd_node_create.go` — no change to CLI surface.

### Tests

- Existing `Service.Create` tests must continue to pass. If a test
  asserts no `file_state` row exists, update it to assert the new row.
- New tests:
  - After Create, `file_state` row exists with the new file's hash.
  - Two concurrent Create calls on the same path: one succeeds, the
    other gets a "file already exists" error (or `ErrBusy` if the
    second arrives before the first commits — both are acceptable;
    document which).
  - Create of a path that already has a tombstoned `file_state` row
    transitions it back to `'live'` correctly.

## Verification

1. `make build`, `make test`, `make vet`, `make lint`, `make fmt` green.
2. Smoke: `tusk node create some/new.md --type note`; inspect
   `.tusk/index.db` to confirm a `file_state` row was added.

## Out of Scope

- Removing the workspace flock — Phase 5.
- Any retry-on-busy logic — propagate `ErrBusy` from the lease.

## Notes for the Implementer

- T4.1's helper handles the placeholder insert (lazy-create). Create
  does not need a special pre-insert path of its own; the helper's
  `INSERT … ON CONFLICT DO NOTHING` covers both fresh paths (Create)
  and pre-existing nodes with no row yet (Modify/Delete on old data).
- The placeholder content_hash is empty string by convention, and
  is overwritten by `WriteWithLease` on successful commit. The Mutator
  treats empty `current` as "file does not exist on disk" — which is
  the only correct condition for Create.
- The Mutator's check that `current` is empty is the moral equivalent
  of `O_CREATE|O_EXCL` for our DB-coordinated world. If two Creates
  race, the second one's Mutator sees the first one's content and
  errors out.
