---
type: package
title: internal/index — SQLite store
import-path: github.com/germanamz/tusk/internal/index
status: stable
---

# internal/index

SQLite-backed index. Owns the schema (nodes, edges, path_refs, embeddings, node_embeddings, embed_queue, file_state, skipped_files, workflow_drift, property_drift, meta) and the per-table repos. Sub-unit `nodes` rows carry a structural-address id (`<fileID>#S1.2P3`) and a `content_hash`; vectors are content-addressed — `embeddings` is keyed by `(content_hash, model)` and `node_embeddings` maps each node-chunk to its shared vector. Opens the DB with `_journal_mode=WAL` and `_busy_timeout=5000` so reindex, watcher, and MCP tool calls can share the file safely.

## Public surface

- `Open(path string) (*Store, error)` — opens or creates the DB.
- `RemoveArtifacts(dbPath string) ([]string, error)` — deletes the DB file together with its `-wal`/`-shm` sidecars (absent files are not an error); returns the paths removed. Used by `internal/reset` and by `OpenOrRebuild`'s schema-mismatch rebuild so both drop the full artifact set rather than orphaning sidecars.
- `NodeRepo`, `EdgeRepo`, `EmbeddingRepo`, `EmbedQueueRepo`, `FileStateRepo`, `DriftLog`, `PropertyDrift`, `MetaRepo` — narrow CRUD facades.
- `FileStateRepo.RecordSkip` / `ClearSkip` / `ListSkips` — the `skipped_files` table: one row per file reindex acked without indexing because of a fault, with the error text. The methods live on `FileStateRepo` because the table is path-keyed and follows the file lifecycle, and `FileStates` is already wired into every reindex config.
- `EdgeRepo.ReplacePathRefs` / `PathRefsFrom` / `PathRefsTo` / `AllPathRefs` — the `path_refs` table: one row per (page, paths edge type, path, line), keyed to the page's node row with `ON DELETE CASCADE`. The methods live on `EdgeRepo` because a path ref is an edge whose target is a path, and every pipeline that rewrites a page's edges already holds that handle.
- `SectionSpans(*sql.DB, fileID)` / `InnermostSection(spans, line)` — a file's numbered section rows, and the deepest one holding a line. `tusk query --include paths`, `tusk doctor` and `tusk claude refs` use them to name the section a path mention sits in.
- `RefLookup` semantics live in `internal/node` but bind to `*NodeRepo` in production via `node.NewIndexRefLookup`.

## Notes

The package registers a deterministic `regexp(pattern, value)` SQL function at init, so every connection the `sqlite` driver opens has it. SQLite reserves the `X REGEXP Y` operator but ships no implementation, and calls this function for it. It backs the filter's `path`/`id` glob patterns (see [`internal/pathglob`](pathglob.md)). The implementation is Go's RE2, which runs in linear time, behind a small bounded cache of compiled patterns that is safe to share across connections. A NULL argument yields NULL, and a pattern that doesn't compile is an SQL error.

WAL + busy_timeout means the watcher can write while a long-running `tusk_query` reads — but reindex's own ordering is what gates correctness, not the DB. See `internal/reindex` for the cross-pass resolution issue.

`Open` applies a small set of idempotent migrations after the bootstrap schema — dropping the dead `manifest_snapshot`/`warnings` tables and the unused `idx_file_state_lease` index, and adding the nullable `nodes.start_line` / `nodes.end_line` columns (a sub-unit's file line range) to indexes that predate them. `ADD COLUMN` keeps every row and vector; the new columns stay NULL until reindex renumbers the rows (see `internal/reindex`). A new table added as `CREATE TABLE IF NOT EXISTS` (like `skipped_files` or `path_refs`) reaches existing indexes on their next `Open` with no `SchemaVersion` bump. Incompatible on-disk schemas are not migrated in place: `OpenOrRebuild` drops and rebuilds the DB from the authoritative `CREATE TABLE` DDL, keyed on `SchemaVersion`. The `edges` table carries no `ordinal` column — sibling ordering is derived from the source node's `OrderedBy` property at query time (see `internal/manifest`).
