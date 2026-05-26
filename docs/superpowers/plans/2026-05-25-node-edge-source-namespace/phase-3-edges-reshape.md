# Phase 3 — Edges Table Reshape

**Spec:** § *Edges table*, § *Index schema bump & rebuild*

**Goal:** Add `kind` and `source` columns to `edges`, populate them on every insert (three writer paths: `direct`, `derived`, `structural`), tighten the schema with a CHECK constraint, swap the UNIQUE constraint to include `source`, and add origin/source indexes.

## Prerequisites

- Phase 2 complete: `nodes` reshape is done; the rebuild path is exercised.

## Tasks

| # | Title | Plan doc | Bumps `SchemaVersion`? |
|---|---|---|---|
| 3.1 | Add nullable `kind`/`source` columns to `edges` DDL | `phase-3-task-1-add-nullable-columns.md` | yes |
| 3.2 | Update `synthesizeRefEdgeTypes` writer path → `(derived, NULL)` | `phase-3-task-2-ref-derived-writer.md` | no |
| 3.3 | Update frontmatter property-value ingest → `(direct, NULL)` | `phase-3-task-3-direct-writer.md` | no |
| 3.4 | Update sub-unit sync `contains`/`contained-by` writer → `(structural, markdown)` | `phase-3-task-4-structural-writer.md` | no |
| 3.5 | Tighten DDL: `NOT NULL kind`, CHECK constraint, UNIQUE includes `source`, new indexes (`edges_source_type_idx`, `edges_kind_idx`) | `phase-3-task-5-tighten-ddl.md` | yes |
| 3.6 | Finishing: end-to-end test confirming all three writer paths land in the rebuilt index with correct values | `phase-3-task-6-finishing.md` | no |

## Sequencing

Strict order 3.1 → 3.2 → 3.3 → 3.4 → 3.5 → 3.6. Each task is its own PR.

## User-Visible Behavior to Preserve

- All edges in the graph after rebuild match the edges in the graph before, modulo the new `kind`/`source` columns.
- `tusk doctor` and `tusk query` return the same edge-related results.
- Graph expansion (`tusk_context`) returns the same neighbor sets.
- Wikilink-resolved edges, `references`-derived edges, and `contains`/`contained-by` structural edges all continue to exist; only the schema columns differ.

## Bridge Code

Task 3.1 introduces nullable `kind`/`source` columns. Tasks 3.2–3.4 populate them. Task 3.5 tightens to `NOT NULL` + CHECK + new UNIQUE shape. **Removal target:** Task 3.5.

## Changes Introduced

- `internal/index/index.go` — `edges` DDL gains `kind TEXT`, `source TEXT NULL` (Task 3.1); promotes `kind` to `NOT NULL`, adds CHECK, swaps UNIQUE, adds two new indexes (Task 3.5).
- `internal/index/edge_repo.go` — insert and update statements accept and persist `kind`/`source` (Tasks 3.2, 3.3, 3.4 use these via their callers).
- `internal/manifest/loader.go` — `synthesizeRefEdgeTypes` produces edge values with `kind='derived'` (Task 3.2).
- `internal/node/edges.go` or wherever frontmatter property-value ingest writes edges — sets `kind='direct'` (Task 3.3).
- `internal/subunit/sync.go` — `rewriteContains` and any other `contains`/`contained-by` writer sets `kind='structural', source='markdown'` (Task 3.4).
- `internal/index/schema_version.go` — bumped twice (Task 3.1, Task 3.5).
