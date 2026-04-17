# Phase 2 — Field Registry and Startup Validation

**Initiative:** MCP Field Restrictions (v0.12)
**Design spec:** `docs/superpowers/specs/2026-04-17-mcp-field-restrictions-design.md`
**Prerequisites:** Phase 1 merged.
**Parallelism:** Must complete before phase 3.

## Goal

Introduce the static MCP tool-field registry and extend `Server.validateConfig` to reject malformed or unknown `mcp.blocked_fields` entries at startup. No runtime enforcement yet — the server simply fails fast on typos.

After this phase, a config with a bad `blocked_fields` entry makes `tusk mcp serve` error on boot with a clear message. Valid configs boot normally; behavior otherwise unchanged.

## Inherits From

**Phase 1 state:**

- `config.MCPConfig.BlockedFields` field exists (`map[string][]string`).
- `config/default.toml` ships populated `disabled_tools` (seven workspace tools) and `[mcp.blocked_fields]` entries for `tusk_project_modify`/`tusk_project_delete`.
- No MCP-layer changes; `Server.cfg.BlockedFields` is populated by `config.Load` but nothing reads it.
- `IsSliceKey("mcp.blocked_fields.<tool>")` and `IsValidKey("mcp.blocked_fields.<tool>")` both return `true` — the `reflect.Map` branches in `config/write.go` handle the single-remaining-part case.

## Tasks

### 1. Create `internal/mcp/field_registry.go`

New file in `internal/mcp/`. Package `mcp`. Hand-maintained, package-private registry:

```go
package mcp

// toolFields enumerates every declared input parameter for each MCP tool
// that accepts writes. Used by validateConfig to reject unknown entries
// in mcp.blocked_fields at startup, and by checkBlocked at runtime
// (introduced in phase 3).
//
// This registry is hand-maintained. Adding, renaming, or removing a
// tool parameter in registerTools (server.go) requires a matching edit
// here. Keep entries in the same order handlers appear in registerTools
// for easy cross-reference.
var toolFields = map[string]map[string]struct{}{
    "tusk_task_create": setOf(
        "title", "description", "priority", "project", "parent",
        "tags", "due", "wait_until", "uda",
    ),
    "tusk_task_modify": setOf(
        "short_id", "version", "title", "description", "priority",
        "project", "parent", "due", "wait_until", "uda",
        "add_tags", "remove_tags",
    ),
    "tusk_task_start":    setOf("short_id", "version", "player_id"),
    "tusk_task_done":     setOf("short_id", "version"),
    "tusk_task_delete":   setOf("short_id", "version"),
    "tusk_task_annotate": setOf("short_id", "body"),
    "tusk_task_link":     setOf("source", "target", "type"),
    "tusk_task_unlink":   setOf("source", "target", "type"),
    "tusk_task_claim":    setOf("short_id", "player_id", "version"),
    "tusk_task_release":  setOf("short_id", "player_id", "version"),
    "tusk_task_pop":      setOf("player_id", "filter"),

    "tusk_project_create": setOf("name", "workflow", "urgency", "auto_complete", "auto_revert"),
    "tusk_project_modify": setOf("name", "version", "workflow", "urgency_set", "urgency_delta", "auto_complete", "auto_revert"),
    "tusk_project_delete": setOf("name", "version", "force"),

    "tusk_workflow_create": setOf("name", "statuses", "transitions"),
    "tusk_workflow_modify": setOf("name", "version", "add_statuses", "set_statuses", "remove_statuses", "add_transitions", "remove_transitions"),
    "tusk_workflow_delete": setOf("name", "version"),

    "tusk_player_register": setOf("player_id"),

    "tusk_note_add":     setOf("player_id", "body", "project", "task", "metadata"),
    "tusk_note_archive": setOf("player_id", "id"),

    "tusk_config_set": setOf("key", "value"),
}

func setOf(keys ...string) map[string]struct{} {
    s := make(map[string]struct{}, len(keys))
    for _, k := range keys {
        s[k] = struct{}{}
    }
    return s
}
```

Cross-check against the corresponding `mcp.NewTool(...)` blocks in `internal/mcp/server.go:registerTools`. If any discrepancy, trust `server.go` and update the registry — do not modify `server.go` here.

### 2. Extend `Server.validateConfig` with `BlockedFields` checks

Edit `internal/mcp/server.go`, `validateConfig` method (around lines 108-179). After the existing `DisabledResourceGroups` loop, before the `if len(errs) > 0` block, append:

```go
for toolName, fields := range s.cfg.BlockedFields {
    registry, known := toolFields[toolName]
    if !known {
        errs = append(errs, fmt.Errorf("blocked_fields: unknown tool %q", toolName))
        continue
    }
    for _, field := range fields {
        if strings.Contains(field, ".") {
            errs = append(errs, fmt.Errorf("blocked_fields: dotted sub-keys not yet supported (%q on tool %q)", field, toolName))
            continue
        }
        if _, ok := registry[field]; !ok {
            errs = append(errs, fmt.Errorf("blocked_fields: tool %q has no field %q", toolName, field))
        }
    }
}
```

Add `"strings"` to the `import` block if not already present (it is not, as of phase 1).

### 3. Add a pointer comment in `registerTools`

At the top of the `registerTools` method in `internal/mcp/server.go` (around line 203), add a single line:

```go
// Keep internal/mcp/field_registry.go in sync when adding, renaming, or
// removing input parameters on any tool below.
```

### 4. Extend `internal/mcp/server_test.go` with validation coverage

Add test cases using the existing `mustNew` helper and `config.MCPConfig` pattern:

- `TestValidateConfig_BlockedFields_UnknownTool` — `BlockedFields: {"tusk_bogus": {"foo"}}` → `New` returns error containing `blocked_fields: unknown tool`.
- `TestValidateConfig_BlockedFields_UnknownField` — `BlockedFields: {"tusk_task_modify": {"bogus"}}` → error containing `has no field "bogus"`.
- `TestValidateConfig_BlockedFields_DottedField` — `BlockedFields: {"tusk_task_modify": {"uda.env"}}` → error containing `dotted sub-keys not yet supported`.
- `TestValidateConfig_BlockedFields_Valid` — `BlockedFields: {"tusk_project_modify": {"workflow"}}` → `New` succeeds.

Use `errors.Is`/`strings.Contains` style consistent with the surrounding `server_test.go` cases for `DisabledTools`.

## User-Visible Behavior Preserved

- All existing MCP handlers still dispatch unchanged — enforcement arrives in phase 3.
- A fresh install (`default.toml` with phase-1 defaults) still starts cleanly; `tusk_project_modify` and `tusk_project_delete` are in `disabled_tools`, so their `blocked_fields` entries are inert but valid, and the new validation passes.
- Any hand-edited config with a typo in `[mcp.blocked_fields]` now refuses to start; previously it was silently ignored. This is an intentional fail-fast.

## Changes Introduced

- **New:** `internal/mcp/field_registry.go` — static registry of tool input fields.
- **Modified:** `internal/mcp/server.go` — `validateConfig` extension, pointer comment in `registerTools`, `strings` import.
- **Modified:** `internal/mcp/server_test.go` — four new validation cases.
- **No bridge code.**
- **No schema migrations, no new dependencies, no new environment variables.**

## Acceptance

- `go build ./...` passes.
- `go test ./internal/mcp/... ./config/...` passes.
- `go vet ./...` passes.
- `tusk mcp serve` still runs against the shipped default config.
