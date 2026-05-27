# Phase 2 — Schema and lease primitives

**Spec:** `docs/superpowers/specs/2026-05-26-tusk-concurrent-instances.md`
**Plan:** `docs/superpowers/plans/2026-05-26-concurrent-instances/PLAN.md`
**Prereqs:** Phase 1 complete (`body` removed from `node_modify`).
**Parallelization:** T2.1 and T2.2 may run in parallel.
T2.3 requires T2.1 only (it adds `FileStateRepo.Claim`/`Release` and
`WorkerID()`; the analogous embed_queue claim lands in T3.1). T2.3 may
run in parallel with T2.2.

## Inherits From

Phase 1 narrowed `tusk_node_modify` to frontmatter mutations only:
- `node.ModifyInput` no longer carries a `Body` field.
- The MCP `tusk_node_modify` tool no longer accepts a `body` parameter.
- `Service.Modify` no longer rewrites file bodies.

Phase 2 builds on the base codebase otherwise — SQLite WAL is already on
(`internal/index/index.go:144`), `embed_queue` already exists
(`internal/index/embed_queue_repo.go`), the workspace flock is still in
place at MCP startup (`internal/mcp/runtime.go:96-107`). Nothing in this
phase removes or relies on removing the flock; concurrent instances are
still blocked when this phase ships.

## Execution Rules

Apply to every task in this phase:

1. **One task = one PR.** Each of the four tasks ships independently.
2. **Each PR must be independently shippable.** Build green, full Go test
   suite green (lefthook pre-commit enforces), `make lint` clean.
3. **Multiple rebuilds during this phase are accepted.** Tasks 2.1 and 2.2
   both bump the index schema version (`internal/index/schema_version.go`)
   and trigger a drop-and-rebuild on first open after upgrade via the
   existing `OpenOrRebuild` flow
   (`internal/workspace/indexopen/indexopen.go:32`). Users who pull each
   PR pay one rebuild per schema-changing task. This is the deliberate
   cost of independent shipability; see `PLAN.md` § *Phase 2* for the
   reasoning.
4. **Bridge code is permitted only when explicitly named** in a task doc,
   and only with a removal target identified (no removal targets are
   expected in this phase — every artifact introduced lives on past the
   work stream).
5. **No handler or runtime change** in this phase. The new table, columns,
   and helpers stay unconsumed until Phase 3 (embed queue) and Phase 4
   (handlers) wire them up. Build green even though the new types are
   referenced by no production code.

## Goal

Land the data structures and primitives the rest of the work stream
depends on, with no behavioral change to user-facing surfaces:

- The `file_state` table (per-file coordination row: hash, mtime, size,
  lease columns, `pending_temp_path`, `pending_hash`, `last_seen_gen`,
  `updated_at_ns`) — schema from spec § *`file_state` — per-file
  coordination table*.
- Lease and `kind` columns on the existing `embed_queue` table — schema
  from spec § *Embed queue lease*. The `kind` column is added now (not in
  Phase 6) so reindex jobs in Phase 6 do not require their own schema
  bump.
- The lease primitive in Go — `Claim`, `Release`, stale-temp cleanup, plus
  a stable worker identity (UUID generated at MCP startup). These live in
  a new package or alongside the existing repo code; the task doc names
  the location.

The schema version mechanism already exists
(`internal/index/schema_version.go` + `OpenOrRebuild`); each
schema-changing task simply bumps the `SchemaVersion` string and the
existing rebuild path takes over.

## Tasks

| #     | Title                                              | Prereqs        |
|-------|----------------------------------------------------|----------------|
| 2.1   | `file_state` table and `FileStateRepo` CRUD        | none (Phase 1) |
| 2.2   | `embed_queue` lease + `kind` columns               | none (Phase 1) |
| 2.3   | Lease primitive (`Claim` / `Release` / cleanup) + worker identity | T2.1 |

T2.1 and T2.2 are independent of each other and may proceed in parallel.

## Changes Introduced

- **New table:** `file_state` (T2.1).
- **Modified table:** `embed_queue` gains `leased_by`, `leased_until_ns`,
  `lease_started_at_ns`, `kind` (T2.2).
- **New code:** `FileStateRepo` (T2.1); new columns on `EmbedQueueRepo`
  schema, but the row type and Drain signature remain unchanged (T2.2);
  lease primitive and worker identity helper (T2.3).
- **Schema bumps:** two during the phase (T2.1, T2.2), each via
  `SchemaVersion` string change.
- **No env vars, no manifest keys, no CLI flags** in this phase.
- **No bridge code beyond what each task names.**

## Acceptance Criteria

After Phase 2 ships:
- An upgrade from the prior schema triggers a single drop-and-rebuild on
  first open. The rebuilt `.tusk/` contains the new `file_state` table
  (empty initially) and the new `embed_queue` columns.
- All existing handlers (`node_create`, `node_modify`, `node_move`,
  `node_delete`, `edge_add`, `edge_remove`, queries, watch, reindex,
  doctor) behave identically to before. None of them consume the new
  primitives yet.
- `embed_queue` continues to drain via its current path
  (`Drain` returns oldest rows and deletes them in one transaction). The
  new columns are inert (`leased_by` always NULL, `kind` always `'embed'`).
- The lease primitive exists in code but is unused outside of its own
  tests. A unit test for the primitive verifies the claim / release /
  stale-temp-cleanup path against a temporary SQLite DB.
- Full Go test suite, lint, vet all green.
