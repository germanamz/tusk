---
type: package
title: internal/reindex — full reindex pipeline
import-path: github.com/germanamz/tusk/internal/reindex
status: stable
---

# internal/reindex

Walks the workspace tree, parses every markdown file, validates against the manifest, runs behavior hooks, resolves refs, and upserts nodes + edges + drift rows in one pass. Powers `tusk reindex` and the `tusk_reindex` MCP tool.

## Public surface

- `Run(ctx, Config) (*Report, error)` — single entry point.
- `Config` — repos, manifest, behaviors, optional drainer hookup.
- `Report` — `Indexed`, `Removed`, `Skipped`, `WorkflowViolations`, `PropertyViolations`, `RefDangling`, `RefAmbiguous`, `RefTypeMismatch`, `RefCycle`, `RefHealed`.
- `HealRefDrift(ctx, WorkerConfig) (HealReport, error)` — re-resolves recorded ref drift after a sweep; the MCP drainer calls it after productive ticks.

## Notes

Ref resolution runs per file against the live index, so a file processed before its target has a node row records `ref_dangling` drift instead of an edge. The **ref-drift heal pass** makes this converge: after the sweep's drain, `Run` re-enqueues the file behind every ref-kind drift row and drains once more — by then every live file has a node row, so refs that dangled only for ordering reasons (fresh index) or because their target was created after the referencing file was last indexed resolve, write their edges, and clear their drift. Genuinely broken refs re-record drift and stay in the report. Async walks instead enqueue the drifted files for the background drainer, which heals after any tick that indexed something. Report ref counters reflect the post-heal end state of the pass.
