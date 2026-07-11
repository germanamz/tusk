---
type: package
title: internal/doctor — drift surface
import-path: github.com/germanamz/tusk/internal/doctor
status: stable
---

# internal/doctor

Reads workspace health from the index — workflow drift, property drift, ref-resolution drift, dangling edges, embed queue depth and retries, and the resolved graph-expansion config — and emits a single human-readable summary. Backs both `tusk doctor` and the `tusk_doctor` MCP tool.

## Public surface

- `Run(workspace, repos…) (Report, error)` — synchronous health check.
- `Migrate(Config) (MigrationReport, error)` — auto-migrates any legacy `__cli__` / `__mcp__` edge rows left over from pre-frontmatter `tusk edge add` / `tusk_edge_add` calls back into the source node's frontmatter. Runs by default ahead of `Run`; `tusk doctor --no-migrate` (and the equivalent MCP flag) skips it for a diagnostic-only pass. A row whose edge type is no longer declared in the manifest cannot be written to frontmatter, so it is reported as skipped and left in the index rather than aborting the whole report — doctor never dies on the drift it exists to diagnose.
- `Issue` shape — `Kind`, `NodeID`, `Message`.
- Issue kinds include: `dangling-edge`, `embed-retry`, `workflow-violation`, `undeclared-property`, `type-mismatch`, `required-missing`, `enum-violation`, `ref_dangling`, `ref_ambiguous`, `ref_type_mismatch`, `ref_cycle`, `embed-large-chunk`, `embed-no-chunks`, `embedding-drift`, `legacy-cli-edge`, `legacy-mcp-edge`, `sub-units-disabled-dirty`, `graph-expansion-unknown-edge`, `graph-expansion-invalid-edge`, `graph-expansion-no-edges`, `graph-expansion-weight-zero`.

## Notes

- **Orphaned drift is never reported.** Workflow / property drift is only written while validating a live node, so a row whose node was deleted or renamed away is an orphan with no repair path. `Run` skips such rows at read time (a node-existence filter), and `reindex.Run` sweeps them from the drift tables (`DeleteOrphans`) so they do not accumulate. Together these stop doctor from pointing at ghost node ids forever.
- **Graph-expansion edge-types are validated with the query path's grammar** (`typeref.Parse`), so doctor agrees with what a real `--semantic` query does: a malformed entry (e.g. `Refs`) is flagged `graph-expansion-invalid-edge` (it hard-fails every semantic query), a well-formed scoped ref like `:references` whose type is declared is not flagged, and an undeclared-but-well-formed name stays `graph-expansion-unknown-edge` (the walker silently skips it). `enabled=true` with an empty `edge-types` list is a `graph-expansion-no-edges` no-op.
- **`embed-retry`** surfaces embed-queue rows that have failed at least once (`attempts > 0`) with their attempt count and last error, so a persistently failing embedder is visible rather than hidden behind the queue-depth counter.
