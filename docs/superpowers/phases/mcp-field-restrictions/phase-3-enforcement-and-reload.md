# Phase 3 — Runtime Enforcement and Hot Reload

**Initiative:** MCP Field Restrictions (v0.12)
**Design spec:** `docs/superpowers/specs/2026-04-17-mcp-field-restrictions-design.md`
**Prerequisites:** Phases 1 and 2 merged.
**Parallelism:** Terminal phase — no parallelism possible.

## Goal

Turn the inert config surface into enforced policy. Add `checkBlocked`, wire it into every mutating MCP handler, extend `reloadConfig` to swap the full `s.cfg` so `BlockedFields` updates apply without restart, and ship the tests that prove all of it end-to-end.

After this phase, the feature is complete. Roadmap checkboxes flip and the v0.12 milestone is closer to closure.

## Inherits From

**Phase 1 state:**

- `config.MCPConfig.BlockedFields` exists; defaults shipped in `default.toml`.

**Phase 2 state:**

- `internal/mcp/field_registry.go` exists with `toolFields` map and `setOf` helper.
- `validateConfig` rejects unknown tools, unknown fields, and dotted field names for `BlockedFields`.
- `registerTools` carries a sync-pointer comment.
- Server still does not check `BlockedFields` on any request — every mutating handler runs unchanged.

## Tasks

### 1. Create `internal/mcp/blocked.go` and its unit tests

New file `internal/mcp/blocked.go`:

```go
package mcp

import (
    "fmt"
    "sort"
    "strings"

    "github.com/mark3labs/mcp-go/mcp"
)

// checkBlocked returns a tool-result error when the request supplies any
// field listed in s.cfg.BlockedFields[toolName]. Absent or nil values
// pass. Returns nil when nothing is blocked.
//
// Field presence is determined from req.GetArguments(): a key present in
// the arguments map with a non-nil value is considered "supplied".
func (s *Server) checkBlocked(toolName string, req mcp.CallToolRequest) *mcp.CallToolResult {
    blocked := s.cfg.BlockedFields[toolName]
    if len(blocked) == 0 {
        return nil
    }
    args := req.GetArguments()
    if len(args) == 0 {
        return nil
    }
    var hit []string
    for _, field := range blocked {
        v, ok := args[field]
        if !ok || v == nil {
            continue
        }
        hit = append(hit, field)
    }
    if len(hit) == 0 {
        return nil
    }
    sort.Strings(hit)
    return mcp.NewToolResultError(fmt.Sprintf(
        "fields [%s] are blocked by mcp.blocked_fields.%s",
        strings.Join(hit, ", "), toolName,
    ))
}
```

New file `internal/mcp/blocked_test.go` covering (use `config.MCPConfig{BlockedFields: ...}` and the existing `mustNew` helper):

- Field absent from arguments → `checkBlocked` returns `nil`.
- Field present with `nil` value → returns `nil`.
- Field present with non-nil value → returns an error result; message contains the field name and the tool name.
- Multiple blocked fields supplied → error lists all of them, sorted.
- `BlockedFields` empty for this tool → returns `nil` even with arguments.

Build the `mcp.CallToolRequest` via the public constructor used elsewhere in the package tests (grep `CallToolRequest{` in existing test files for the exact pattern).

### 2. Wire `checkBlocked` into every mutating handler

Touch each of the handlers below. As the very first statement of each function (before any `RequireString`/`RequireFloat` calls), add:

```go
if result := s.checkBlocked("<tool_name>", request); result != nil {
    return result, nil
}
```

Use the exact tool name as registered. Handlers to wire (21 total):

| File | Function | Tool name |
|---|---|---|
| `internal/mcp/tools.go` | `handleTaskCreate` | `tusk_task_create` |
| `internal/mcp/tools.go` | `handleTaskModify` | `tusk_task_modify` |
| `internal/mcp/tools.go` | `handleTaskStart` | `tusk_task_start` |
| `internal/mcp/tools.go` | `handleTaskDone` | `tusk_task_done` |
| `internal/mcp/tools.go` | `handleTaskDelete` | `tusk_task_delete` |
| `internal/mcp/tools.go` | `handleTaskAnnotate` | `tusk_task_annotate` |
| `internal/mcp/tools.go` | `handleTaskLink` | `tusk_task_link` |
| `internal/mcp/tools.go` | `handleTaskUnlink` | `tusk_task_unlink` |
| `internal/mcp/tools.go` | `handleTaskClaim` | `tusk_task_claim` |
| `internal/mcp/tools.go` | `handleTaskRelease` | `tusk_task_release` |
| `internal/mcp/tools.go` | `handleTaskPop` | `tusk_task_pop` |
| `internal/mcp/project_handlers.go` | `handleProjectCreate` | `tusk_project_create` |
| `internal/mcp/project_handlers.go` | `handleProjectModify` | `tusk_project_modify` |
| `internal/mcp/project_handlers.go` | `handleProjectDelete` | `tusk_project_delete` |
| `internal/mcp/workflow_handlers.go` | `handleWorkflowCreate` | `tusk_workflow_create` |
| `internal/mcp/workflow_handlers.go` | `handleWorkflowModify` | `tusk_workflow_modify` |
| `internal/mcp/workflow_handlers.go` | `handleWorkflowDelete` | `tusk_workflow_delete` |
| `internal/mcp/tools.go` | `handlePlayerRegister` | `tusk_player_register` |
| `internal/mcp/note_handlers.go` | `handleNoteAdd` | `tusk_note_add` |
| `internal/mcp/note_handlers.go` | `handleNoteArchive` | `tusk_note_archive` |
| `internal/mcp/config_handlers.go` | `handleConfigSet` | `tusk_config_set` |

Do **not** wire read handlers: `handleTaskGet`, `handleTaskList`, `handleTaskTree`, `handleTaskNext`, `handleTaskAvailable`, `handleProjectList`, `handleWorkflowList`, `handleNoteList`, `handleConfigShow`. The registry does not include them either.

If any handler name above has been renamed since the spec was written, prefer the current name — grep by the `s.addTool("...", mcp.NewTool("<tool_name>", ...)` binding to find the handler reference.

### 3. Make `reloadConfig` swap the full `s.cfg`

In `internal/mcp/server.go`, `reloadConfig` method (around lines 860-879). Today it only updates the urgency engine. Extend it to also replace `s.cfg`:

```go
func (s *Server) reloadConfig(ctx context.Context) error {
    cfg, err := config.Load(s.loadOpts...)
    if err != nil {
        return fmt.Errorf("reloading config: %w", err)
    }
    s.urgencyEngine.Reload(service.UrgencyWeights{ /* unchanged */ })
    s.cfg = cfg.MCP   // whole-struct swap; reads after this see the new map
    _ = ctx
    return nil
}
```

Keep the urgency `Reload` call unchanged. The swap is a single assignment, so readers in `checkBlocked` either see the old or the new `BlockedFields` map — never a torn intermediate.

Update the comment block above `reloadConfig` to note that `mcp.blocked_fields` now hot-reloads, unlike `mcp.disabled_*` (which still requires restart because handler registration is frozen at boot).

### 4. Handler-level block test

Add to `internal/mcp/handlers_test.go` (or `project_handlers_test.go`, whichever fits the surrounding style):

`TestHandleProjectModify_BlockedFieldRejected`:

- Construct a server with `config.MCPConfig{BlockedFields: {"tusk_project_modify": {"workflow"}}}`, using a stub `ProjectService` that records whether `Update` was called.
- Build a `CallToolRequest` with `name=foo`, `version=1`, `workflow=kanban`.
- Expect the handler to return an error result containing `mcp.blocked_fields.tusk_project_modify` and leave the stub untouched.

Also add a negative case: same server config, request omits `workflow` → handler proceeds (stub `Update` gets called; the test can stop at the first service call).

### 5. Reload integration test

Extend `internal/mcp/server_test.go`. New test `TestReloadConfig_BlockedFieldsHotSwap`. Sequence:

1. `tmpDir := t.TempDir()`; `path := filepath.Join(tmpDir, "tusk.toml")`.
2. Build an initial `config.Config` with empty `BlockedFields` and write it via `config.WriteConfig(&cfg, path)`.
3. Construct the server with `loadOpts := []config.Option{config.WithExplicitFile(path)}`. Pass the same slice into `New(...)` — the server stores it on `s.loadOpts`, so later reloads read from the same temp file.
4. Invoke `checkBlocked(<tool>, req)` (or the full handler) with a supplied field — expect no block.
5. Rewrite `path` with `BlockedFields: {"<tool>": {"<field>"}}` via a second `config.WriteConfig`.
6. Call `s.ReloadConfigForTest(ctx)`.
7. Invoke `checkBlocked` with the same request — expect the block error; error message must contain `mcp.blocked_fields.<tool>`.

Uses only package-internal access (`s.cfg` write, `checkBlocked` call) — the test file is already in `package mcp`.

### 6. E2E scenario + ROADMAP tick

**E2E:** Add a scenario in `tests/e2e/` (`config_test.go` if one exists, else `mcp_config_test.go`):

- `tusk config set mcp.blocked_fields.tusk_task_modify priority` — succeeds.
- `tusk config show --output json` — output contains `blocked_fields` with the expected entry.

The scenario proves the dot-path writer, slice-key detection, and TOML round-trip work end-to-end. Skip actual MCP transport testing — out of scope for the harness.

**ROADMAP:** In `ROADMAP.md`, flip the three checkboxes under `Initiative: MCP Field Restrictions` (lines ~960-963) from `[ ]` to `[x]`. Do **not** create `docs/status/v0.12-status.md` or `docs/releases/v0.12.md` here — those land at milestone close per project convention.

## User-Visible Behavior Preserved

After this phase, these continue to work exactly as before:

- Any MCP call that does **not** supply a blocked field completes normally.
- `tusk mcp serve` boots on the shipped `default.toml`; all disabled tools stay disabled; all enabled tools respond.
- `tusk_config_set` for scalar keys (`urgency.due_weight`, `tui.color`, etc.) still writes and hot-reloads urgency weights.
- Optimistic locking errors, not-found errors, and workflow-transition errors from the service layer surface unchanged.

And these are newly enforced:

- Any MCP call that supplies a field listed in `BlockedFields[toolName]` returns a tool-result error naming the blocked fields and the config key.
- `tusk_config_set mcp.blocked_fields.<tool> <csv>` takes effect on the next tool call without a server restart.

## Changes Introduced

- **New:** `internal/mcp/blocked.go` — `checkBlocked` helper.
- **New:** `internal/mcp/blocked_test.go` — unit coverage for `checkBlocked`.
- **Modified:** `internal/mcp/tools.go`, `project_handlers.go`, `workflow_handlers.go`, `note_handlers.go`, `config_handlers.go` — one-line block check prepended to each of the 21 mutating handlers.
- **Modified:** `internal/mcp/server.go` — full-cfg swap in `reloadConfig` plus doc comment update.
- **Modified:** `internal/mcp/handlers_test.go` (or `project_handlers_test.go`) — handler-level block test.
- **Modified:** `internal/mcp/server_test.go` — hot-reload integration test.
- **Modified:** `tests/e2e/...` — config-set + show scenario.
- **Modified:** `ROADMAP.md` — initiative checkboxes ticked.
- **No bridge code.**
- **No schema migrations, no new dependencies, no new environment variables.**

## Acceptance

- `make test` passes (unit + e2e).
- `make test-race` passes.
- `make vet` and `make lint` pass.
- `tusk mcp serve` boots on shipped defaults; a hand-crafted call to `tusk_project_modify` supplying `workflow=` returns the block error; the same call without `workflow` reaches the service layer.
- `tusk config set mcp.blocked_fields.tusk_task_modify priority` persists and is visible in `tusk config show`.
