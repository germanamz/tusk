# Phase 5 — Reference-Resolution Grammar

**Spec:** § *Reference resolution rule*, § *Naming conventions*, § *Reference resolution in code*

**Goal:** Land the `<source>:<type>` parser and its consumers. The grammar accepts three forms (`type` = union, `:type` = user namespace, `source:type` = scoped to one source). Walker, query layer, and MCP boundary all consume parsed `EdgeRef`/`NodeRef` values instead of raw strings.

## Prerequisites

- Phase 4 complete: user-namespace and pack-namespace declarations coexist.

## Tasks

| # | Title | Plan doc |
|---|---|---|
| 5.1 | `EdgeRef`, `NodeRef` types + `Scope` enum + parser (`internal/typeref`) | `phase-5-task-1-ref-types-and-parser.md` |
| 5.2 | `NeighborsByEdgeRefs` in `internal/index/edge_repo.go` (alongside existing `NeighborsByEdgeTypes`) | `phase-5-task-2-neighbors-by-edge-refs.md` |
| 5.3 | `internal/graphexpand` walker uses `EdgeRefs` | `phase-5-task-3-graphexpand-walker.md` |
| 5.4 | Query layer (`internal/query`) parses incoming type names into refs | `phase-5-task-4-query-layer.md` |
| 5.5 | MCP boundary parses qualified names; remove deprecated `NeighborsByEdgeTypes` | `phase-5-task-5-mcp-boundary.md` |
| 5.6 | Finishing: end-to-end test exercising all three reference forms | `phase-5-task-6-finishing.md` |

## Sequencing

Strict order 5.1 → 5.2 → 5.3 → 5.4 → 5.5 → 5.6. Each task is its own PR.

## User-Visible Behavior to Preserve

- Bare-name type references (current behavior) continue to work via union semantics.
- `tusk_context` MCP calls return the same neighbor sets for the same arguments.
- `tusk query` returns the same nodes for the same filters.
- `query.graph-expansion.edge-types` configs that list bare names continue to work identically.

## User-Visible Behavior Newly Enabled

- Users may write qualified type references: `markdown:contains`, `:contains`, etc.
- Same grammar across manifest config, query filters, MCP arguments, and CLI.

## Bridge Code

Task 5.2 introduces `NeighborsByEdgeRefs` alongside the existing `NeighborsByEdgeTypes`. Task 5.3 switches the walker over. Task 5.5 removes `NeighborsByEdgeTypes`. **Removal target:** Task 5.5.

## Changes Introduced

- New package `internal/typeref` — `EdgeRef`, `NodeRef`, `Scope` enum, `Parse(string) (Ref, error)`.
- `internal/index/edge_repo.go` — `NeighborsByEdgeRefs` (Task 5.2); `NeighborsByEdgeTypes` deleted (Task 5.5).
- `internal/graphexpand/walk.go` — `Walker.EdgeTypes []string` becomes `Walker.EdgeRefs []typeref.EdgeRef` (Task 5.3).
- `internal/query/query_service.go` and `internal/query/expand.go` — incoming type names parsed via `typeref.Parse` (Task 5.4).
- `internal/mcp/runtime.go` — MCP tool arguments parsed via `typeref.Parse` (Task 5.5).
