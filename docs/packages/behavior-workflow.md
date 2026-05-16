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
