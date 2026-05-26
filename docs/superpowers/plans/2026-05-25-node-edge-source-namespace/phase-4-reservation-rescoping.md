# Phase 4 — Reservation Rescoping

**Spec:** § *Type-pack reservation model*

**Goal:** Rescope the `subdocument` type-pack's reserved node-types and edge-types so they only apply within `source='markdown'`. Update the `SubUnitConflict` validator so it fires only on within-source collisions. After this phase, a user can declare a node-type called `section` or an edge-type called `contains` in their manifest without error.

## Prerequisites

- Phase 3 complete: edges carry `(kind, source)`; nodes carry `(kind, source)`.

## Tasks

| # | Title | Plan doc |
|---|---|---|
| 4.1 | Rescope `typepacks/subdocument` reservation lists to source-scoped semantics | `phase-4-task-1-rescope-typepack.md` |
| 4.2 | Rescope `SubUnitConflict` validator in `internal/manifest` | `phase-4-task-2-rescope-validator.md` |
| 4.3 | Finishing: integration test confirming user-namespace `section` declarations load cleanly | `phase-4-task-3-finishing.md` |

## Sequencing

Strict order 4.1 → 4.2 → 4.3. Each task is its own PR.

## User-Visible Behavior to Preserve

- Existing manifests that don't declare `section`, `contains`, etc. continue to load identically.
- The subdocument pack's structural behavior (writing `(subunit, markdown, …)` rows) is unchanged.
- Sub-unit sync produces the same edges and nodes as before.

## User-Visible Behavior Newly Enabled

- A manifest declaring `[node-types.section]` or `[edge-types.contains]` in the user namespace loads cleanly. Sub-unit `(subunit, markdown, section)` rows coexist with user `(file, NULL, section)` rows in the index.

## Bridge Code

None. This phase is pure validator/reservation logic with no schema impact.

## Changes Introduced

- `internal/typepacks/subdocument/pack.go` — `ReservedNodeTypes` and `ReservedEdgeTypes` reframed (documentation + structure only — the list values stay the same; only the semantic scope changes via the validator).
- `internal/manifest/subunits.go` (or wherever `SubUnitConflict` is detected) — the comparison logic gains a `source` parameter and fires only when two declarations target the same source.
