# Phase 5 — MCP Surface

**Spec:** `docs/superpowers/specs/2026-04-25-subtree-urgency-overrides-design.md`
**Phase size:** 4 tasks

## Prerequisites

Phases 1, 2, 3, 4 must be merged. The CLI path is fully working; the MCP read paths already serialize the new fields via the shared `taskJSON` shape.

## Inherits From

After Phase 4:
- `tusk task modify` accepts the full inline syntax for urgency overrides via a shared `parseUrgencyFields` helper.
- `tusk task get`, `tusk task list`, `tusk task tree` render `urgency_overrides` (sparse) and `effective_urgency_weights` (full 10-weight table) in both text and JSON modes.
- The MCP read path (`tusk_task_get`, `tusk_task_list`, `tusk_task_tree`) already surfaces the new fields in JSON because all tools share the `taskJSON` serializer.
- `domain.ValidateUrgencyOverridesPatch(map[string]any) error` exists and is not yet called from any production path.
- `domain.TaskUpdate.UrgencyMergePatch` accepts the merge-patch shape from any service caller.
- The v0.12 blocked-fields gate (`internal/mcp/field_registry.go::toolFields`, `internal/mcp/blocked.go::checkBlocked`) lists recognized fields per tool.

## Goal

Wire the MCP `tusk_task_modify` tool to accept `urgency_overrides` (RFC 7396 merge patch) and `urgency_overrides_clear` (boolean control flag), validated against the known weight keys and gated by the v0.12 blocked-fields mechanism. Agents reach full parity with the CLI for setting, clearing, and updating overrides — minus arithmetic deltas (by design; agents compute and set).

## Tasks

### Task 5.1 — tusk_task_modify input schema

1. In `internal/mcp/tools.go` (find `registerTools` / the `tusk_task_modify` tool declaration), extend the JSON input schema for the tool:
   ```jsonc
   "urgency_overrides": {
       "type": ["object", "null"],
       "description": "RFC 7396 merge patch over the task's urgency weight overrides. Keys: priority_weight, due_weight, age_weight, active_weight, blocking_weight, blocked_weight, tags_weight, project_weight, annotations_weight, waiting_weight. Each value must be a number (set the key) or JSON null (delete the key from existing overrides). Absent keys are unchanged. Top-level null on this field is rejected — use urgency_overrides_clear: true to drop all overrides in one call."
   },
   "urgency_overrides_clear": {
       "type": "boolean",
       "description": "When true, all task-level urgency overrides are cleared before the urgency_overrides patch is applied. Intended as a one-shot reset before re-patching."
   }
   ```
2. Register both field names in the blocked-fields registry. In `internal/mcp/field_registry.go`, extend the `tusk_task_modify` entry (around line 17):
   ```go
   "tusk_task_modify": setOf(
       "short_id", "version", "title", "description", "priority",
       "project", "parent", "due", "wait_until", "uda",
       "add_tags", "remove_tags",
       "urgency_overrides", "urgency_overrides_clear",
   ),
   ```
   Without this, `validateConfig` will reject any `mcp.blocked_fields.tusk_task_modify` list that contains the new field names at startup, and `checkBlocked` will also not be able to gate them at runtime.

### Task 5.2 — Decode + validate the merge patch in handleTaskModify

1. In the `handleTaskModify` function body (`internal/mcp/tools.go` — same file as the schema registration), after existing field-extraction code and before the `taskSvc.Update(ctx, upd)` call:
2. **Top-level null guard.** Check whether the raw input carried `urgency_overrides` and whether the parsed value is JSON null:
   ```go
   raw, rawOK := req.Arguments["urgency_overrides"]
   if rawOK && raw == nil {
       return mcpError("urgency_overrides: null is not supported; use urgency_overrides_clear: true to drop all overrides")
   }
   ```
   `mcpError` follows the existing error-return convention in the file (look at other handlers in the same file for the established helper name).
3. **Parse `urgency_overrides_clear`.** Extract as an optional bool; default false if absent. Reject any non-boolean type with a clear error.
4. **Parse `urgency_overrides` patch.** If `raw` is a non-nil `map[string]any` (or the Go type delivered by the MCP JSON decoder — match the pattern already used by `uda` parsing in the same handler):
   - Call `domain.ValidateUrgencyOverridesPatch(rawMap)`. On error, return the validator's message as the MCP error response verbatim.
   - Decode to `*domain.UrgencyOverridesPatch`:
     ```go
     patch := &domain.UrgencyOverridesPatch{
         Clear: make(map[string]bool),
         Set:   make(map[string]float64),
     }
     for key, value := range rawMap {
         if value == nil {
             patch.Clear[key] = true
             continue
         }
         // Validator guarantees the value is a numeric type; coerce to float64.
         switch v := value.(type) {
         case float64: patch.Set[key] = v
         case float32: patch.Set[key] = float64(v)
         case int:     patch.Set[key] = float64(v)
         case int64:   patch.Set[key] = float64(v)
         default:
             return mcpError(fmt.Sprintf("urgency_overrides: unexpected numeric type %T for key %q", v, key))
         }
     }
     ```
5. **Apply the clear flag.** If `urgency_overrides_clear == true`:
   - Ensure `patch` exists (allocate if only the clear flag was provided with no merge patch).
   - Set `patch.ClearAll = true`.
6. **Normalize empty patches.** If `patch` exists but `!patch.ClearAll && len(patch.Clear) == 0 && len(patch.Set) == 0`, leave `upd.UrgencyMergePatch = nil` — avoids forcing the service into a no-op code path. An empty JSON object `{}` from the agent is a no-op, consistent with RFC 7396 semantics.
7. **Assign.** Otherwise `upd.UrgencyMergePatch = patch`. Never set `upd.UrgencyDelta` (MCP has no arithmetic-delta form). Never set `upd.UrgencyOverrides` (no full-replace input on MCP — that path is reserved for JSON import tooling).

### Task 5.3 — Blocked-fields gate verification

1. The blocked-fields registration in Task 5.1 ensures `validateConfig` accepts the new names in a `mcp.blocked_fields.tusk_task_modify` list. The runtime gate in `internal/mcp/blocked.go::checkBlocked` checks only whether the caller's request includes a blocked field — no per-field code is needed; the field names must match.
2. Add a per-field check inside `handleTaskModify`: before any decoding of urgency inputs, let the existing blocked-fields machinery run. The convention is already present for other fields; match it:
   - Inspect `req.Arguments` for the presence of `urgency_overrides` and `urgency_overrides_clear`.
   - Pass both names to the existing `checkBlocked` call (or whatever interception point the handler uses for other fields).
   - On reject, return the standard blocked-field error message (see `blocked.go:41` for the format string `"fields [%s] are blocked by mcp.blocked_fields.%s"`).
3. Verify in `internal/mcp/blocked_test.go` that `TestValidateConfig_BlockedFields_UnknownField` (around line 240) does not break — the new field names should pass validation.
4. Add a dedicated unit test in the same file: `TestBlockedFields_BlocksUrgencyOverrides` — config lists `urgency_overrides`; simulated call that includes the field; asserts blocked error.

### Task 5.4 — MCP E2E

1. Create `tests/e2e/mcp_urgency_overrides_test.go` using the existing MCP harness (see `tests/e2e/mcp_task_queue_test.go` for the current pattern — it drives the MCP transport and asserts JSON responses).
2. Scenarios to cover:
   - `set_multiple_keys` — call `tusk_task_modify` with `urgency_overrides: {priority_weight: 5.0, blocking_weight: 20.0}` on a task with no existing overrides; follow with `tusk_task_get`; assert `urgency_overrides` has exactly those two keys.
   - `null_clears_single_key` — precondition: task has `{priority_weight: 5, blocking_weight: 20, due_weight: 3}`. Call `urgency_overrides: {due_weight: null}`; assert only `due_weight` removed; others intact.
   - `empty_patch_no_op` — call `urgency_overrides: {}`; assert state unchanged; version still bumped only if normalization decides to bump (spec note: an empty merge patch should NOT bump version, matching the normalization in Task 5.2).
   - `top_level_null_rejected` — call `urgency_overrides: null`; assert error message mentions `urgency_overrides_clear`.
   - `clear_all_then_set` — task has three keys; call `urgency_overrides_clear: true, urgency_overrides: {priority_weight: 5}`; assert final state has only `priority_weight: 5` (clear-all ran first).
   - `unknown_key_rejected` — call `urgency_overrides: {bogus_key: 1}`; assert error names `bogus_key` and lists valid keys.
   - `non_numeric_rejected` — call `urgency_overrides: {priority_weight: "high"}`; assert error names `priority_weight` and mentions "must be a number or null".
   - `blocked_fields_gate` — run the MCP server with `mcp.blocked_fields.tusk_task_modify = ["urgency_overrides"]`; call with an urgency patch; assert blocked error; assert task state unchanged.
   - `effective_weights_on_read` — after setting an override via MCP, call `tusk_task_get`; assert the JSON response carries `effective_urgency_weights` with the full 10-weight resolved table.
   - `task_tree_carries_fields` — place an override on a parent; call `tusk_task_tree` for the subtree; assert every descendant carries `effective_urgency_weights` reflecting the inheritance. (Note: this may depend on the subtree handler routing through `List` with a `RootID` filter. If `handleTaskTree`'s subtree branch still calls `GetDescendants` raw — the latent issue tracked in the hardening initiative — this test may surface zero urgency scores. **If it does, document the finding in the implementer's closing summary and mark the E2E case as pending the hardening story; do NOT fix the hardening bug as part of this phase.**)

## User-visible behaviors that must still work after this phase

- All existing MCP tools (`tusk_task_create`, `tusk_task_get`, `tusk_task_list`, `tusk_task_tree`, `tusk_task_delete`, `tusk_task_start`, `tusk_task_done`, `tusk_task_move`, `tusk_task_annotate`, `tusk_task_link`, `tusk_task_unlink`, `tusk_task_claim`, `tusk_task_release`, `tusk_task_pop`) continue to accept their existing inputs and produce identical outputs.
- Calls to `tusk_task_modify` that do not include `urgency_overrides` or `urgency_overrides_clear` produce byte-identical results to pre-Phase-5.
- The v0.12 blocked-fields mechanism continues to gate every other field as before. A blocked-fields configuration listing `urgency_overrides` must reject the call before any state change; omitting the field from the list leaves the new path fully functional.
- Resource reads (`tusk://tasks/{short_id}`) carry the new JSON fields, since Phase 4 already wired them into `taskJSON`.
- `make test` and `make test-race` pass.

## Bridge code

None.

## Changes Introduced

- **New files:**
  - `tests/e2e/mcp_urgency_overrides_test.go`
- **Modified files:**
  - `internal/mcp/server.go` — `tusk_task_modify` JSON input schema additions (`urgency_overrides` object and `urgency_overrides_clear` boolean) registered alongside the other `WithObject`/`WithBoolean` declarations.
  - `internal/mcp/tools.go` — `handleTaskModify` decode and validation logic for the new fields.
  - `internal/mcp/field_registry.go` — adds `urgency_overrides` and `urgency_overrides_clear` to the `tusk_task_modify` field set.
  - `internal/mcp/blocked.go` (if per-field inspection needs a small edit to expose the new field names to the gate) — match the existing convention for other fields; typically no code change if the gate walks `req.Arguments` directly.
  - `internal/mcp/blocked_test.go` — `TestBlockedFields_BlocksUrgencyOverrides`.
- **Modified interfaces:** `tusk_task_modify` gains two input fields (additive; absent = no-op).
- **No new schema migrations, environment variables, or dependencies.**
