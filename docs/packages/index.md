---
type: package
title: internal/index — SQLite store
import-path: github.com/germanamz/tusk/internal/index
status: stable
---

# internal/index

SQLite-backed index. Owns the schema (nodes, edges, embeddings, node_embeddings, embed_queue, workflow_drift, property_drift, meta) and the per-table repos. Sub-unit `nodes` rows carry a structural-address id (`<fileID>#S1.2P3`) and a `content_hash`; vectors are content-addressed — `embeddings` is keyed by `(content_hash, model)` and `node_embeddings` maps each node-chunk to its shared vector. Opens the DB with `_journal_mode=WAL` and `_busy_timeout=5000` so reindex, watcher, and MCP tool calls can share the file safely.

## Public surface

- `Open(path string) (*Store, error)` — opens or creates the DB.
- `RemoveArtifacts(dbPath string) ([]string, error)` — deletes the DB file together with its `-wal`/`-shm` sidecars (absent files are not an error); returns the paths removed. Used by `internal/reset` and by `OpenOrRebuild`'s schema-mismatch rebuild so both drop the full artifact set rather than orphaning sidecars.
- `NodeRepo`, `EdgeRepo`, `EmbeddingRepo`, `EmbedQueueRepo`, `DriftLog`, `PropertyDrift`, `MetaRepo` — narrow CRUD facades.
- `RefLookup` semantics live in `internal/node` but bind to `*NodeRepo` in production via `node.NewIndexRefLookup`.

## Notes

WAL + busy_timeout means the watcher can write while a long-running `tusk_query` reads — but reindex's own ordering is what gates correctness, not the DB. See `internal/reindex` for the cross-pass resolution issue.

`Open` applies a small set of idempotent migrations after the bootstrap schema — dropping the dead `manifest_snapshot`/`warnings` tables and the unused `idx_file_state_lease` index. Incompatible on-disk schemas are not migrated in place: `OpenOrRebuild` drops and rebuilds the DB from the authoritative `CREATE TABLE` DDL, keyed on `SchemaVersion`. The `edges` table carries no `ordinal` column — sibling ordering is derived from the source node's `OrderedBy` property at query time (see `internal/manifest`).
