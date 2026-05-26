# Phase 6 — Cleanup (embeddings DDL fix + dead migration removal)

**Spec:** § *What the schema bump removes* (extended with the embeddings DDL correction below).

**Goal:** Close out the schema reshape by (1) correcting the embeddings table uniqueness shape that the now-dead `migrateEmbeddingsPrimaryKey` was ratcheting toward, and (2) deleting that migration plus the other dead migration functions in `internal/index/index.go`. Together these leave the DDL constant in `internal/index/index.go` as the single authoritative source of the on-disk schema.

The two tasks are themed together because they share a root cause: the dead migration produced a wrong-by-construction shape on the embeddings table (`UNIQUE(node_id)` collapsing all chunks of a multi-chunk node into a single row), and removing the migration is only safe if the corrected DDL is what every future index inherits. Task 1 fixes the DDL; Task 2 deletes the migration.

## Prerequisites

- Phase 5 complete: full feature is live; rebuild model is the only path for incompatible indexes.

## Tasks

| # | Title | Plan doc | Bumps `SchemaVersion`? |
|---|---|---|---|
| 6.1 | Fix embeddings uniqueness shape to `UNIQUE(node_id, chunk_idx)` so the `embeddingsMatch` hash-skip can fire | `phase-6-task-1-fix-embeddings-uniqueness.md` | yes |
| 6.2 | Remove dead P2 sub-unit migrations and embeddings UNIQUE-tightening from `internal/index/index.go` | `phase-6-task-2-remove-legacy-migrations.md` | no |

## Sequencing

Strict order 6.1 → 6.2. Each task lands as its own PR.

Task 6.1 must land first because Task 6.2 deletes `migrateEmbeddingsPrimaryKey`; once deleted, no in-place migration exists to bridge between the old (`UNIQUE(node_id)`) and the corrected (`UNIQUE(node_id, chunk_idx)`) shapes. With Task 6.1 ahead of it, the corrected DDL is what every fresh index gets; pre-existing indexes are dropped by `OpenOrRebuild` per the `SchemaVersion` bump in 6.1.

This phase intentionally contains only 2 tasks (below the 4–6 task guideline) because rule 5 of the phase-planning rules exempts cleanup phases. The two tasks are tightly coupled — the DDL fix and the migration removal are two halves of one cleanup intent — and splitting either further would fragment a coherent change.

After Task 6.2 merges, the planning agent runs the post-implementation review and removes the entire `docs/superpowers/plans/2026-05-25-node-edge-source-namespace/` directory as a final commit (per the phase-planning rules).

## User-Visible Behavior to Preserve

- Same as Phase 5; the embeddings shape change is invisible to all CLI, MCP, and query consumers — only internal storage layout differs.
- `tusk reindex` becomes correctly idempotent on unchanged content (including multi-chunk nodes), restoring the property that commit `0b6f11a` claimed but did not fully achieve under `UNIQUE(node_id)`.
- The MCP watcher's repeated reindex passes on fs events become near-no-ops once the hash-skip fires, which silently removes the runaway re-embed load against Ollama. No watcher code is changed in this phase; the deferred per-file partial reindex (v1-design "Plan 8") remains a separate future effort.

## Changes Introduced

- `internal/index/index.go` — embeddings DDL changes from column-level `node_id UNIQUE` to table-level `UNIQUE(node_id, chunk_idx)` (Task 6.1); the four dead `migrate*` functions and exclusive helpers are removed (Task 6.2).
- `internal/index/embedding_repo.go` — `Upsert` ON CONFLICT target switches to `(node_id, chunk_idx)` (Task 6.1).
- `internal/index/schema_version.go` — `SchemaVersion` bumped once more in Task 6.1 to trigger transparent rebuild for existing indexes.
- `internal/embed/drain_test.go` — `TestDrainQueue_PersistsAllChunksForMultiChunkNode` updated to assert one row per chunk; a new multi-chunk variant of `TestDrainQueue_SkipsEmbedWhenContentUnchanged` is added (Task 6.1).
- `internal/index/embedding_repo_test.go` — `TestEmbeddingRepo_Stats_Aggregates` rewritten to reflect the corrected storage shape (Task 6.1).
