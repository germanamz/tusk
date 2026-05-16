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
- `Report` — `Indexed`, `Removed`, `Skipped`, `WorkflowViolations`, `PropertyViolations`, `RefDangling`, `RefAmbiguous`, `RefTypeMismatch`, `RefCycle`.

## Notes

Cross-tree title-based ref resolution (a plan referencing a spec via bare title) requires **two reindex passes**: the first populates the spec table; the second resolves the plan's ref. Single-pass reindex leaves stale `ref_dangling` drift on plans whose target isn't yet in the DB. Symptom encountered during the workspace bootstrap migration. Worth a follow-up to either two-pass internally or topologically sort the walk by node type.
