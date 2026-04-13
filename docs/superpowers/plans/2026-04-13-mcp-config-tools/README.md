# MCP Config Tools — Plan Overview

**Initiative:** `ROADMAP.md` → v0.9 → *MCP Config Tools*.

**Goal:** Expose Tusk configuration management to AI agents over MCP — matching
the surface of the existing `tusk config`, `tusk workflow`, and `tusk project`
CLI commands. After this initiative, an agent with MCP access can inspect the
effective configuration, mutate scalar config values, and create/modify/delete
workflows and projects, all with the same file-write semantics and validation
rules as the CLI.

## Phase sequencing

Phases must be executed in order. Each phase is independently shippable —
after any phase lands, the binary still builds, the CLI still works, and the
MCP server exposes the tools added so far (or, for phase 1, still exposes only
the existing task/relation/workflow tools).

| # | Phase | Adds |
|---|---|---|
| 1 | `phase-1-plumbing-and-reload.md` | `loadOpts` plumbed to MCP server, hot-reload infra on `inmem.WorkflowRepository` / `inmem.ProjectRepository` / `service.UrgencyEngine`, `Server.reloadConfig` helper (bridge: unused until phase 2) |
| 2 | `phase-2-config-show-set.md` | `tusk_config_show`, `tusk_config_set` MCP tools, new `config` tool group, `storage.*` denylist guard |
| 3 | `phase-3-workflow-tools.md` | `tusk_workflow_create`, `tusk_workflow_modify`, `tusk_workflow_delete` MCP tools with structured JSON params |
| 4 | `phase-4-project-tools.md` | `tusk_project_create`, `tusk_project_modify`, `tusk_project_delete` MCP tools with structured JSON params and task-ref checker |

## Out of scope

- MCP field-level write restrictions (`[mcp.blocked_fields]`) — lives in v0.10
  (`MCP Field Restrictions`), not this initiative.
- CLI DSL parser reuse — MCP tools take structured JSON params, not inline
  `key=value` strings. The tui parsers stay in `internal/tui` and are not moved.
- Hot-reload of `storage.*` settings — changing the database path on a live
  MCP server is undefined; `tusk_config_set` rejects `storage.*` keys.
- Reload of `mcp.disabled_tools` / `mcp.disabled_tool_groups` — the tool
  registry is fixed at process start; disabling a tool via MCP takes effect
  only after restart. Documented in the `tusk_config_set` description.

## Cleanup

After phase 4 is merged and the continuity + post-implementation reviews are
complete, this entire directory is deleted from the repository.
