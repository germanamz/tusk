# Node & Edge Source Namespace — Implementation Index

> **For agentic workers:** Each phase is implemented by a separate agent. Phase docs are the implementer's primary directive; this index exists for the planning agent and reviewers to see the whole shape. Task docs (one per task) provide step-by-step TDD breakdowns; the implementer follows their phase doc and consults the task docs for execution detail.

**Spec:** `docs/superpowers/specs/2026-05-25-node-edge-source-namespace-design.md`

**Goal:** Reshape the SQLite index so `nodes` and `edges` carry explicit `(kind, source, type)` columns; scope built-in type-pack reservations to their owning source; ship the schema as an incompatible index bump rebuilt from source files; preserve all user-facing behavior.

**Strategy:** One schema bump for the whole feature, sequenced so each phase compiles cleanly and ships independently. Phase 1 builds the rebuild infrastructure with no schema change. Phases 2 and 3 add `kind`/`source` to nodes and edges respectively (each is one atomic PR for the DDL+writer pair, plus follow-up PRs for reader updates and tests). Phase 4 rescopes the reservation model. Phase 5 lands the reference-resolution grammar (`<source>:<type>` parsing). A final cleanup phase removes dead migration code.

---

## Phase Sequence

| Phase | Title | Plan doc | Tasks (incl. finishing) | Ships |
|---|---|---|---|---|
| 1 | Index rebuild infrastructure | `phase-1-rebuild-infrastructure.md` | 6 | `schema_version` key in `meta`, `ErrSchemaIncompatible` sentinel, `OpenOrRebuild` helper, CLI/MCP wired through it |
| 2 | Nodes table reshape | `phase-2-nodes-reshape.md` | 6 | `nodes.kind` and `nodes.source` columns, CHECK constraint, composite index, writers populate, readers stop reading `parent_id` for row-class |
| 3 | Edges table reshape | `phase-3-edges-reshape.md` | 6 | `edges.kind` and `edges.source` columns, CHECK constraint, new UNIQUE shape, `edges_source_type_idx` and `edges_kind_idx`, every edge writer populates |
| 4 | Reservation rescoping | `phase-4-reservation-rescoping.md` | 3 | `subdocument` typepack reservations scoped to `source='markdown'`; `SubUnitConflict` validator fires only on within-source collisions |
| 5 | Reference-resolution grammar | `phase-5-reference-resolution.md` | 7 | `<source>:<type>` parser, `EdgeRef`/`NodeRef` types, `NeighborsByEdgeRefs`, walker and filter compiler updated, filter grammar accepts qualified edge-type idents, MCP boundary parses the notation |
| 6 | Cleanup | `phase-6-cleanup.md` | 2 | Embeddings DDL fixed to `UNIQUE(node_id, chunk_idx)` so the hash-skip can fire; dead legacy-migration code removed from `internal/index/index.go` |

**Totals:** 30 tasks across 6 phases; each phase's "finishing" task carries the finishing PR. Phase 5 carries 7 tasks because the original task 5.4 was split (5.4 covers node-type literal compilation in `internal/filter`; 5.4a was added when implementing 5.4 revealed that qualified edge-type identifier syntax requires distinct lexer/parser/validator work).

## PR Structure

- **One PR per task** (28 task PRs total across all phases).
- Each phase's last task is the finishing task — its PR ties the phase together with the end-to-end integration test and the phase-summary commit message.
- Phase 6 is the final cleanup PR.

Phases must merge in numeric order. Within a phase, tasks must merge in numeric order — each task's PR builds on the previous one within the same phase.

## Schema-Version Sequencing

The `schema_version` constant in `internal/index` is bumped three times across this feature:

- **End of Phase 2** — when nodes gain `kind`/`source` and the DDL becomes incompatible with prior 1.x indexes.
- **End of Phase 3** — when edges gain `kind`/`source`.
- **Phase 6 Task 1** — when the embeddings table moves to `UNIQUE(node_id, chunk_idx)` so the per-chunk hash-skip can fire.

Each bump triggers `OpenOrRebuild` to rebuild from source files on first run after upgrade. Users who upgrade across the whole feature experience exactly one rebuild because mismatch detection doesn't care how many times the version was bumped; it just rebuilds to the current value.

## Per-Phase Prerequisites

- Phase 1 depends only on the base codebase.
- Phase 2 depends on Phase 1 (needs `OpenOrRebuild` for the rebuild path to work).
- Phase 3 depends on Phase 2 (the manifest-aware writers in Phase 3 assume the nodes schema is current).
- Phase 4 depends on Phase 3 (rescoping the validator makes sense only after both schemas carry `source`).
- Phase 5 depends on Phase 4 (the parser surfaces user-namespace `<source>:<type>` references that the validator must accept).
- Phase 6 depends on Phase 5 (cleanup happens after all reshaping is complete).

No phases run in parallel; the sequence is strict.

## Per-Task Plan Files

Each task in each phase has its own dedicated plan file at:

```
docs/superpowers/plans/2026-05-25-node-edge-source-namespace/
  phase-1-rebuild-infrastructure.md
  phase-1-task-1-schema-version-key.md
  phase-1-task-2-incompatible-sentinel.md
  phase-1-task-3-open-or-rebuild-helper.md
  phase-1-task-4-wire-cli.md
  phase-1-task-5-wire-mcp.md
  phase-2-nodes-reshape.md
  phase-2-task-1-add-nullable-columns.md
  phase-2-task-2-file-writer.md
  phase-2-task-3-subunit-sync-writer.md
  phase-2-task-4-replace-parent-id-reads.md
  phase-2-task-5a-align-writers-and-tests.md
  phase-2-task-5-tighten-ddl.md
  phase-2-task-6-finishing.md
  ...
  phase-6-cleanup.md
  phase-6-task-1-fix-embeddings-uniqueness.md
  phase-6-task-2-remove-legacy-migrations.md
```

Task plan docs are bite-sized TDD breakdowns (write failing test → minimal implementation → run → commit). The phase plan doc is the implementer's primary directive; task docs are the execution detail.

## Cleanup at End

After all six phases land and the implementation review confirms the system works end-to-end, the planning agent removes the plan docs from `docs/superpowers/plans/2026-05-25-node-edge-source-namespace/` as a final commit. The spec remains.
