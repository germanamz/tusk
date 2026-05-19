---
type: package
title: internal/doctor — drift surface
import-path: github.com/germanamz/tusk/internal/doctor
status: stable
---

# internal/doctor

Reads workspace health from the index — workflow drift, property drift, ref-resolution drift, dangling edges, embed queue depth — and emits a single human-readable summary. Backs both `tusk doctor` and the `tusk_doctor` MCP tool.

## Public surface

- `Run(workspace, repos…) (Report, error)` — synchronous health check.
- `Migrate(Config) (MigrationReport, error)` — auto-migrates any legacy `__cli__` / `__mcp__` edge rows left over from pre-frontmatter `tusk edge add` / `tusk_edge_add` calls back into the source node's frontmatter. Runs by default ahead of `Run`; `tusk doctor --no-migrate` (and the equivalent MCP flag) skips it for a diagnostic-only pass.
- `Issue` shape — `Kind`, `NodeID`, `Property`, `Reason`.
- Issue kinds: `dangling-edge`, `type-mismatch`, `undeclared-property`, `ref_dangling`, `ref_ambiguous`, `ref_type_mismatch`, `ref_cycle`, `workflow-drift`, `embed-queue-stuck`.

## Notes

MCP equivalent currently parses doctor's stderr text-line output for structured warnings — v1 expediency carried as a residual since Plan 7. A structured warning channel would clean this up.
