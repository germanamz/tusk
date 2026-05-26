# Phase 2 — Nodes Table Reshape

**Spec:** § *Nodes table*, § *Index schema bump & rebuild*

**Goal:** Add `kind` and `source` columns to `nodes`, populate them on every insert, drop `parent_id IS NOT NULL` as a row-class discriminator, and tighten the schema with a CHECK constraint and updated indexes. The schema bump happens in two staged sub-bumps within this phase to keep tasks small and shippable; both rebuilds are transparent thanks to Phase 1.

## Prerequisites

- Phase 1 complete: `OpenOrRebuild` is wired through every CLI and MCP entry point.

## Tasks

| # | Title | Plan doc | Bumps `SchemaVersion`? |
|---|---|---|---|
| 2.1 | Add nullable `kind` and `source` columns to `nodes` DDL | `phase-2-task-1-add-nullable-columns.md` | yes |
| 2.2 | Update node writer (file rows) to populate `kind='file', source=NULL` | `phase-2-task-2-file-writer.md` | no |
| 2.3 | Update sub-unit sync to populate `kind='subunit', source='markdown'` | `phase-2-task-3-subunit-sync-writer.md` | no |
| 2.4 | Replace `parent_id IS NOT NULL` reads with `kind='subunit'` across codebase | `phase-2-task-4-replace-parent-id-reads.md` | no |
| 2.5a | Align test writer choice and direct-DDL inserts with row kind; drop tests that become structurally impossible | `phase-2-task-5a-align-writers-and-tests.md` | no |
| 2.5b | Tighten DDL: `NOT NULL kind`, CHECK constraint, replace `nodes_type_idx` with `nodes_kind_type_idx`, rewrite partial UNIQUE on `path` | `phase-2-task-5-tighten-ddl.md` | yes |
| 2.6 | Finishing: full integration test confirming rebuild + behavior preservation | `phase-2-task-6-finishing.md` | no |

## Sequencing

Strict order 2.1 → 2.2 → 2.3 → 2.4 → 2.5a → 2.5b → 2.6. Each task lands as its own PR. The two `SchemaVersion` bumps (2.1 and 2.5b) each trigger transparent rebuilds via `OpenOrRebuild`; mid-phase users still get a single rebuild on upgrade because mismatch detection rebuilds to current regardless of intermediate values.

2.5a was extracted from the original Task 5 after execution surfaced ~15 test failures that the original plan did not anticipate (tests routing sub-unit-shaped rows through `Upsert` and file-shaped rows through `BulkUpsert`; two tests whose seed states the new CHECK makes impossible). Landing the test alignment first keeps every commit green under the lefthook pre-commit `make test` gate.

## User-Visible Behavior to Preserve

- All CLI and MCP commands continue to return the same data.
- `tusk doctor` continues to report sub-unit counts correctly.
- `tusk query` returns the same nodes for the same queries.
- Wikilink resolution unaffected.
- Sub-unit ingestion produces the same `(file, subunit)` shape it does today; only the schema columns differ.

## Bridge Code

Task 2.1 introduces nullable `kind` and `source` columns. Tasks 2.2–2.4 populate them. Task 2.5 promotes `kind` to `NOT NULL` and adds the CHECK constraint. **Removal target:** Task 2.5.

## Changes Introduced

- `internal/index/index.go` — DDL constant gains `kind TEXT`, `source TEXT NULL` (Task 2.1); promotes to `NOT NULL`, adds CHECK, swaps indexes (Task 2.5).
- `internal/index/node_repo.go` — writer accepts and persists `kind`/`source` (Task 2.2); reader queries switch from `parent_id IS NOT NULL` to `kind = 'subunit'` (Task 2.4).
- `internal/subunit/sync.go` — calls the writer with `kind='subunit', source='markdown'` (Task 2.3).
- `internal/manifest/subunits.go` — sub-unit-related queries switch to `kind` (Task 2.4).
- `internal/doctor/doctor.go` — sub-unit count queries switch to `kind` (Task 2.4).
- `internal/node/*.go` — wikilink and edges helpers stop reading `parent_id` as row-class (Task 2.4).
- `internal/index/schema_version.go` — `SchemaVersion` constant bumped twice (Task 2.1, Task 2.5).
