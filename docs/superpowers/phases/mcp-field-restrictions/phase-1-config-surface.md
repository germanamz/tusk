# Phase 1 — Config Surface

**Initiative:** MCP Field Restrictions (v0.12)
**Design spec:** `docs/superpowers/specs/2026-04-17-mcp-field-restrictions-design.md`
**Prerequisites:** None beyond current `main`.
**Parallelism:** Must complete before phases 2 and 3.

## Goal

Introduce the configuration surface — the `BlockedFields` field on `MCPConfig` and the `default.toml` defaults — without any runtime behavior change. After this phase, config parses and the field is reachable, but nothing enforces it.

The MCP server continues to behave exactly as before. This phase is inert by design; it exists so later phases can focus purely on registry/validation and enforcement.

## Inherits From

Base `main` at or after commit `d6510c8` (spec committed). No prior phase.

## Tasks

### 1. Add `BlockedFields` to `config.MCPConfig`

Edit `config/config.go`, `MCPConfig` struct (around line 73-78). Add one field after `DisabledResources`:

```go
BlockedFields map[string][]string `mapstructure:"blocked_fields" toml:"blocked_fields" json:"blocked_fields"`
```

No helper functions, no validation here. Just the field.

### 2. Update `config/default.toml`

In the existing `[mcp]` section, replace the current `disabled_tools = []` with the seven workspace-shaping tool names, and append a new `[mcp.blocked_fields]` table below the existing MCP keys. Final state of the `[mcp]` section:

```toml
[mcp]
# Disable MCP tool groups: ["task", "task_relations", "project", "workflow", "player", "config"]
disabled_tool_groups = []

# Workspace-shaping tools are blocked for MCP agents by default. Remove
# entries to opt in; combine with [mcp.blocked_fields] for field-level policy.
disabled_tools = [
  "tusk_config_set",
  "tusk_workflow_create",
  "tusk_workflow_modify",
  "tusk_workflow_delete",
  "tusk_project_create",
  "tusk_project_modify",
  "tusk_project_delete",
]

# Disable MCP resource groups: ["task", "project", "workflow"]
disabled_resource_groups = []

# Disable individual MCP resources by URI template, e.g.:
# disabled_resources = ["tusk://projects/{name}/workflow"]
disabled_resources = []

# Field-level write restrictions. Keys are MCP tool names, values are the
# input fields an agent may not supply. Enforced only when the tool is
# enabled (i.e., not listed in disabled_tools above).
[mcp.blocked_fields]
tusk_project_modify = ["workflow"]
tusk_project_delete = ["force"]
```

Keep the leading `[storage]`/`[urgency]`/`[tui]` sections untouched.

### 3. Fix map-leaf handling in `isSliceKeyPath` and `isValidKeyPath`

`config/write.go` contains both `isSliceKeyPath` (around line 130) and `isValidKeyPath` (around line 177). Each has a `reflect.Map` branch that returns `false` when `len(parts) == 1`. That is correct for nested maps like `map[string]struct{...}` (where the map key is followed by further struct-field traversal), but wrong for `map[string][]string` where the map key is the final part and the value is the leaf.

Replace the `reflect.Map` branch in `isSliceKeyPath` with:

```go
case reflect.Map:
    if len(parts) == 0 {
        return false
    }
    if len(parts) == 1 {
        elem := t.Elem()
        for elem.Kind() == reflect.Ptr {
            elem = elem.Elem()
        }
        return elem.Kind() == reflect.Slice
    }
    return isSliceKeyPath(t.Elem(), parts[1:])
```

Replace the `reflect.Map` branch in `isValidKeyPath` with:

```go
case reflect.Map:
    if len(parts) == 0 {
        return false
    }
    if len(parts) == 1 {
        elem := t.Elem()
        for elem.Kind() == reflect.Ptr {
            elem = elem.Elem()
        }
        return elem.Kind() != reflect.Struct
    }
    return isValidKeyPath(t.Elem(), parts[1:])
```

No other call sites in the Config type currently reach the map branch (no top-level maps existed before phase 1), so the change is purely additive. Existing `TestIsSliceKey` / `TestIsValidKey` cases must still pass.

### 4. Extend `config/config_test.go` with default-parse coverage

Locate the existing test that loads the embedded default (grep for `default.toml` or `defaultConfig`). Add assertions:

- `cfg.MCP.DisabledTools` contains exactly the seven tool names from Task 2, in the listed order.
- `cfg.MCP.BlockedFields["tusk_project_modify"]` equals `[]string{"workflow"}`.
- `cfg.MCP.BlockedFields["tusk_project_delete"]` equals `[]string{"force"}`.

If no such defaults test exists, add one named `TestDefaultConfigBlockedFields`.

### 5. Extend `config/write_test.go` with slice-key and valid-key coverage

Add cases to `TestIsSliceKey` (around line 164):

- `"mcp.blocked_fields.tusk_project_modify"` → `true`
- `"mcp.blocked_fields.tusk_task_modify"` → `true` (any map key accepted)
- `"mcp.blocked_fields"` → `false` (the map itself is not a slice)

Add cases to `TestIsValidKey` (around line 188):

- `"mcp.blocked_fields.tusk_project_modify"` → `true`
- `"mcp.blocked_fields"` → `false`

All new cases must pass after Task 3's fix.

## User-Visible Behavior Preserved

- `tusk mcp serve` with a fresh `default.toml` still registers every task/note tool.
- Existing user configs (without `disabled_tools` set) now inherit the new default list — a behavior change documented in the release notes but **not** in this phase's artifacts (release notes land at milestone close per project convention).
- No tool call rejected for field reasons (feature is inert).

## Changes Introduced

- **Modified:** `config/config.go` — new `BlockedFields` field on `MCPConfig`.
- **Modified:** `config/default.toml` — expanded `disabled_tools`, new `[mcp.blocked_fields]` table.
- **Modified:** `config/write.go` — `reflect.Map` branches in `isSliceKeyPath` and `isValidKeyPath` now recognize a map key as a terminal path when the map value is a leaf.
- **Modified:** `config/config_test.go` — default-parse assertions.
- **Modified:** `config/write_test.go` — slice-key and valid-key cases.
- **No new files.**
- **No bridge code.**
- **No schema migrations, no new dependencies, no new environment variables.**

## Acceptance

- `go build ./...` passes.
- `go test ./config/...` passes (or one targeted `t.Skip` with the documented pointer).
- `go vet ./...` passes.
- MCP layer compiles untouched — `Server.cfg.BlockedFields` is reachable but unread.
