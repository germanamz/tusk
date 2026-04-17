# MCP Field Restrictions — Design

**Initiative:** v0.12 — MCP Field Restrictions
**Date:** 2026-04-17
**Status:** Approved for implementation

## Goal

Give workspace owners configurable, field-level write restrictions on MCP tools so agents cannot modify sensitive fields, and extend the existing tool-level disable list with defaults that lock down workspace-shaping tools. Writes only — read tools are out of scope.

## Summary

Add `[mcp.blocked_fields]`, a TOML table mapping tool names to lists of blocked field names. Enforce at the MCP handler boundary before any service call. Reject blocked calls with a descriptive error; validate entries against a static registry at startup; hot-reload on `tusk_config_set`.

Ship two layers of default policy in `config/default.toml`:

1. `mcp.disabled_tools` blocks whole workspace-shaping tools for agents (config, workflow, project mutation).
2. `mcp.blocked_fields` blocks specific fields on tools that remain enabled.

## Architecture

Enforcement is a single helper, `checkBlocked(toolName, req)`, called as the first line of each mutating MCP handler. It reads `Server.cfg.BlockedFields` per call, so a hot-reload through `tusk_config_set mcp.blocked_fields.*` immediately changes policy without restarting the process. No service-layer changes; no domain-layer changes.

```
Agent tool call
  → mcp-go dispatches to s.handleX
    → checkBlocked(toolName, req)     [new, first line]
       ├── not blocked → proceed to service layer
       └── blocked    → NewToolResultError(...)
```

## Components

### `config.MCPConfig` (modify)

Add:

```go
BlockedFields map[string][]string `mapstructure:"blocked_fields" toml:"blocked_fields" json:"blocked_fields"`
```

### `config/default.toml` (modify)

```toml
[mcp]
disabled_tools = [
  "tusk_config_set",
  "tusk_workflow_create", "tusk_workflow_modify", "tusk_workflow_delete",
  "tusk_project_create",  "tusk_project_modify",  "tusk_project_delete",
]

[mcp.blocked_fields]
tusk_project_modify = ["workflow"]
tusk_project_delete = ["force"]
```

`blocked_fields` defaults fire only when the corresponding tool is opted back in by removing it from `disabled_tools`. Shipping both together documents intent.

### `internal/mcp/field_registry.go` (new)

Static registry: `toolFields map[string]map[string]struct{}`. One entry per MCP tool listing every declared input parameter. Hand-maintained alongside `registerTools`. A short comment at the top of `registerTools` in `server.go` points at this file so adding a new tool prompts a registry update.

### `internal/mcp/blocked.go` (new)

```go
// checkBlocked returns a CallToolResult error if the request supplies any
// field listed in mcp.blocked_fields for the given tool. Otherwise returns nil.
func (s *Server) checkBlocked(toolName string, req mcp.CallToolRequest) *mcp.CallToolResult
```

Uses `req.GetArguments()` to see which keys the caller supplied; absent and nil fields pass. Reports all offending fields in one error message.

### `Server.validateConfig` (modify)

Extend to cover `BlockedFields`:

- Unknown tool name → `blocked_fields: unknown tool %q`.
- Unknown field for known tool → `blocked_fields: tool %q has no field %q`.
- Dotted field name (v0.12) → `blocked_fields: dotted sub-keys not yet supported (%q)`. Parser reserves the shape for future surgical UDA/meta sub-key blocks.
- Errors joined through existing `errors.Join`; all issues surface at once.

### Handler wiring (modify)

Each mutating handler gets one new first line:

```go
if result := s.checkBlocked("tusk_task_modify", request); result != nil {
    return result, nil
}
```

Covered tools (every write-side tool, even those disabled by default):

- `tusk_task_create`, `tusk_task_modify`, `tusk_task_start`, `tusk_task_done`, `tusk_task_delete`, `tusk_task_annotate`, `tusk_task_claim`, `tusk_task_release`, `tusk_task_pop`
- `tusk_task_link`, `tusk_task_unlink`
- `tusk_project_create`, `tusk_project_modify`, `tusk_project_delete`
- `tusk_workflow_create`, `tusk_workflow_modify`, `tusk_workflow_delete`
- `tusk_player_register`
- `tusk_note_add`, `tusk_note_archive`
- `tusk_config_set`

Read-only tools (`*_get`, `*_list`, `*_tree`, `*_next`, `*_available`, `*_show`) are not wired.

### `reloadConfig` (modify)

Currently rebuilds only the urgency engine. Extend to swap the full `s.cfg` value atomically so `BlockedFields` (and `DisabledTools`/etc. for consistency) reflects the freshly loaded config. Whole-struct swap under a single assignment keeps read sites lock-free — no per-field torn reads because mutators never hold partial state.

## Data Shape

| Layer                    | Shape                                                                |
|--------------------------|----------------------------------------------------------------------|
| TOML                     | `[mcp.blocked_fields]` table with `tool_name = ["field", ...]`       |
| Go (`MCPConfig`)         | `BlockedFields map[string][]string`                                  |
| `tusk config set` path   | `mcp.blocked_fields.<tool_name>` (slice key, comma-separated values) |
| Runtime lookup           | `s.cfg.BlockedFields[toolName]` per handler call                     |

The existing `IsSliceKey` walks `reflect.Map` on a `map[string][]string`, so `tusk config set mcp.blocked_fields.tusk_task_modify "uda,project"` works through the existing writer. No changes to `config/write.go` expected; confirmed in tests.

## Error Handling

- **Startup**: invalid `blocked_fields` entries collected into a joined error returned from `mcp.New(...)`. The binary refuses to start, matching the existing `validateConfig` pattern for `disabled_tools`.
- **Runtime**: blocked call returns `mcp.NewToolResultError("fields [%s] are blocked by mcp.blocked_fields.%s", fields, toolName)`. Caller (agent) gets one clear message listing every blocked field it supplied.
- **Interaction with `disabled_tools`**: disable wins. A tool in `disabled_tools` never registers a handler, so its `blocked_fields` entry is a no-op at runtime — but startup validation still catches typos in the entry to keep config fixable before a later opt-in.

## Testing

| Test                                                   | Purpose                                                                                      |
|--------------------------------------------------------|----------------------------------------------------------------------------------------------|
| `internal/mcp/blocked_test.go`                         | `checkBlocked` behavior: absent, present-empty, present-nonempty, multiple blocked fields.   |
| `internal/mcp/server_test.go` (additions)              | `validateConfig` rejects unknown tool, unknown field, dotted field; accepts valid combo.     |
| `internal/mcp/handlers_test.go` (additions)            | Modify handler with `BlockedFields` set returns block error and does not call the service.  |
| `internal/mcp/server_test.go` (reload additions)       | `WriteConfig` + `reloadConfig` flips a live `BlockedFields` entry without restart.           |
| `config/config_test.go` (additions)                    | `default.toml` parses; `BlockedFields["tusk_project_modify"]` contains `"workflow"`.         |
| `config/write_test.go` (additions)                     | `IsSliceKey("mcp.blocked_fields.tusk_task_modify")` returns true.                            |
| `tests/e2e` (CLI-level scenario)                       | `tusk config set mcp.blocked_fields.tusk_task_modify priority` persists; config file reflects it. |

E2E through MCP transport is skipped — the harness runs CLI only. Config-layer E2E plus internal MCP tests cover the seam.

## Out of Scope

- Dotted sub-key blocking (`uda.env`, `meta.topic`). The parser rejects dotted names in v0.12 and reserves the shape for a later initiative.
- Read restrictions / response filtering. Roadmap scope is writes.
- `tusk_player_modify` MCP tool. Roadmap illustrated a default with `note_window_size` on player modify, but that tool does not exist yet. Ship the framework now; add the default when the tool ships.
- Wildcards across tools (`*: ["workflow"]`). YAGNI.
- Per-player policy. `blocked_fields` is workspace-wide; per-player restrictions are a later conversation.

## Migration

No schema changes. Users on existing `tusk.toml` files get the new defaults only if they opt in — the embedded defaults apply when a key is absent, but the shipped `default.toml` is a template, not a merge source, so pre-existing local configs keep their current `mcp.disabled_tools`. Release notes call out the behavior change: **fresh installs ship with `tusk_config_set` and all workflow/project mutation tools disabled for MCP**.

## File List

New:

- `internal/mcp/field_registry.go`
- `internal/mcp/blocked.go`
- `internal/mcp/blocked_test.go`

Modified:

- `config/config.go` — `MCPConfig.BlockedFields`
- `config/default.toml` — defaults
- `internal/mcp/server.go` — `validateConfig` extension, `reloadConfig` full-cfg swap, `checkBlocked` calls in mutating handlers, registry pointer comment
- `internal/mcp/server_test.go` — validation + reload coverage
- `internal/mcp/handlers_test.go` — block-at-handler coverage
- `config/config_test.go` — default parse coverage
- `config/write_test.go` — slice-key coverage
- `tests/e2e/...` — config-set scenario
- `ROADMAP.md` — mark checkboxes complete at milestone close
