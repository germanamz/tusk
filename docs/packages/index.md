---
type: package
title: internal/index — SQLite store
import-path: github.com/germanamz/tusk/internal/index
status: stable
---

# internal/index

SQLite-backed index. Owns the schema (nodes, edges, embeddings, embed_queue, workflow_drift, property_drift, meta) and the per-table repos. Opens the DB with `_journal_mode=WAL` and `_busy_timeout=5000` so reindex, watcher, and MCP tool calls can share the file safely.

## Public surface

- `Open(path string) (*Store, error)` — opens or creates the DB.
- `NodeRepo`, `EdgeRepo`, `EmbeddingRepo`, `EmbedQueueRepo`, `DriftLog`, `PropertyDrift`, `MetaRepo` — narrow CRUD facades.
- `RefLookup` semantics live in `internal/node` but bind to `*NodeRepo` in production via `node.NewIndexRefLookup`.

## Notes

WAL + busy_timeout means the watcher can write while a long-running `tusk_query` reads — but reindex's own ordering is what gates correctness, not the DB. See `internal/reindex` for the cross-pass resolution issue.
