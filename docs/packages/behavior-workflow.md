---
type: package
title: internal/behavior/workflow — workflow Kind
import-path: github.com/germanamz/tusk/internal/behavior/workflow
status: stable
---

# internal/behavior/workflow

The single shipped behavior-pack Kind. Reads `[behaviors.workflow.<name>]` declarations from `tusk.toml`, validates state-machine transitions on node writes, recovers orphan states (a node whose status is no longer in the declared set), and surfaces drift via the `workflow_drift` table.

## Public surface

- `Pack` — implements `behavior.Pack`; reserves the `status` property on its declared node types.
- Node-write validation: rejects illegal transitions; recovers orphans by stamping the initial state and emitting a drift row.
- States: `initial`, `start`, `terminal`, `done` flags drive workflow semantics.

## Notes

`status-property = "<name>"` in the manifest declaration; the engine rejects any `[node-types.<X>].properties` that re-declares the same name (the 7.c.3 lesson). Used by the kanban pack (`pending → active → completed`).

Transition legality and the initial-state-on-create rule are **write-time** policies: they fire on `node create` / `node modify`, where a real "before" status exists. Reindex reads already-persisted state and validates each node against itself (`before == after`), so it flags only a status value that is not a declared state at all — not how the node legally reached its current state. Consequently a markdown file hand-edited straight into a declared-but-illegally-reached state (e.g. `pending → completed`, skipping `active`) is *not* flagged on reindex: reindex has no record of the prior status and cannot reconstruct transition history. `workflow_drift` rows carry the rejection `error_code` and the fully rendered `detail` message so `tusk doctor` reports the real cause.
