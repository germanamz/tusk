# Phase 1 — Index Rebuild Infrastructure

**Spec:** `docs/superpowers/specs/2026-05-25-node-edge-source-namespace-design.md` § *Index schema bump & rebuild*

**Goal:** Build the machinery that detects an incompatible on-disk index and rebuilds it from source files. No schema change in this phase; the rebuild path is exercised only when a developer manually seeds a different `schema_version`. By the end of the phase, every CLI command and the MCP runtime open the index through a single helper that handles mismatch transparently.

**Prerequisites:** None beyond the base codebase.

**Ships:** `meta.schema_version` key, `ErrSchemaIncompatible` sentinel, `index.OpenOrRebuild` helper (or sibling-package equivalent), CLI and MCP entry points wired through it.

## Tasks

| # | Title | Plan doc |
|---|---|---|
| 1.1 | Add `schema_version` constant + `meta` read/write | `phase-1-task-1-schema-version-key.md` |
| 1.2 | Add `ErrSchemaIncompatible` sentinel; `index.Open` returns it on mismatch | `phase-1-task-2-incompatible-sentinel.md` |
| 1.3 | Add `OpenOrRebuild` helper (new sibling package) | `phase-1-task-3-open-or-rebuild-helper.md` |
| 1.4 | Wire every CLI command through `OpenOrRebuild` | `phase-1-task-4-wire-cli.md` |
| 1.5 | Wire MCP runtime through `OpenOrRebuild` | `phase-1-task-5-wire-mcp.md` |

## Sequencing

Tasks run in order: 1.1 → 1.2 → 1.3 → 1.4 → 1.5. Each lands as its own PR.

A **finishing PR** wraps the phase with an end-to-end test that:
- Opens a populated index, hand-edits `meta.schema_version` to a different value, re-opens via `OpenOrRebuild`, asserts the file was deleted and recreated, and asserts `reindex.Run` repopulated it from the workspace.

## User-Visible Behavior to Preserve

- Every CLI command continues to open the index successfully when the version matches (today's path).
- `tusk doctor`, `tusk query`, `tusk reindex`, `tusk mcp serve`, and the watcher all behave identically when no mismatch is present.
- No new flags or environment variables are introduced.

## Bridge Code

None. This phase introduces only additive plumbing; the rebuild path is dormant until Phase 2 bumps the schema version.

## Changes Introduced

- `internal/index/schema_version.go` — new file holding the `SchemaVersion` constant and the `meta` key name.
- `internal/index/meta_repo.go` — no API change; existing `Get`/`Set` are reused.
- `internal/index/index.go` — `Open` reads `meta.schema_version` after migrations run and returns `ErrSchemaIncompatible` if it does not match the constant.
- `internal/index/errors.go` (new) — declares `ErrSchemaIncompatible` and its `SchemaVersionError` type carrying observed and expected versions.
- New package `internal/workspace/indexopen` (chosen to avoid an `index` ↔ `reindex` cycle) — exposes `OpenOrRebuild(ws workspace.Workspace, reindexCfg reindex.Config) (*index.Index, error)`. Catches `ErrSchemaIncompatible`, deletes the file at `ws.IndexPath`, re-`Open`s, and runs `reindex.Run`.
- Every `cmd/tusk/cmd_*.go` file currently calling `index.Open(ws.IndexPath)` — replaced with the new helper.
- `internal/mcp/runtime.go` — same replacement.
- One integration test in `internal/workspace/indexopen/indexopen_test.go` covering the full rebuild flow.
