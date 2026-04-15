# Phase 4 — Relations, MCP Rename, Finalization

## Inherits From

After Phase 3, the implementer will find:

- `(*App).buildTaskCmd()` in `internal/tui/commands.go` registers fourteen subcommands under `tusk task`: `create`, `list`, `get`, `modify`, `tree`, `start`, `done`, `delete`, `next`, `annotate`, `claim`, `release`, `available`, `pop`.
- `(*App).buildTaskCmds()` bridge-code function still exists, returning two flat commands: `link`, `unlink`.
- `internal/tui/moved.go` stub map has fourteen entries.
- E2E tests reference the fourteen migrated verbs under `{"task", "<verb>", ...}`; `link` and `unlink` still appear as flat arg slices.
- MCP surface is unchanged from main: `tusk_relation_add` and `tusk_relation_remove` tools are registered under the `"relation"` visibility group in `internal/mcp/server.go:412-452`, with handlers in `internal/mcp/tools.go:846-` named `handleRelationAdd` / `handleRelationRemove`. The default-enabled map in `internal/mcp/server.go:117-118` lists both tools; `server.go:136` defines the group set including `"relation"`.
- `config/config_test.go` contains fixture TOML with `disabled_tool_groups = ["relation"]` (line 89 and a check on line 121).

## Goal

Complete the initiative:

1. Migrate the last two task-scoped verbs — `link`, `unlink` — onto the `tusk task` parent.
2. Delete the `buildTaskCmds()` bridge function.
3. Rename the two MCP relation tools to `tusk_task_link` / `tusk_task_unlink`, and rename the MCP visibility group `"relation"` → `"task_relations"`. CLI and MCP stay in lockstep per roadmap Story "Relations under `tusk task`".
4. Finalize user-facing documentation for the v0.11 surface.

## Prerequisites

- Phase 3 complete.

## Relevant Design Context

- Roadmap sections: `ROADMAP.md` → `Initiative: tusk task Subcommand Group` → "Story: Relations under `tusk task`" and "Story: Removal and suggestions for moved commands".
- Current MCP tool registration: `internal/mcp/server.go:412-452` — two `s.addTool("relation", mcp.NewTool("tusk_relation_add", ...))` / `... "tusk_relation_remove" ...` blocks.
- Handler functions: `internal/mcp/tools.go:846` (`handleRelationAdd`), `internal/mcp/tools.go:875` (`handleRelationRemove`). Rename call sites inside `server.go` if any route through a handler map or wrapper — the `addTool` call passes the handler by reference.
- MCP visibility defaults: `internal/mcp/server.go:117-118` and `server.go:136`.
- MCP test fixtures: `internal/mcp/server_test.go:68,72-76,114,171` reference the old tool names and group key.
- E2E fixtures: `tests/e2e/mcp_test.go`, `tests/e2e/mcp_task_queue_test.go` — grep for `tusk_relation_` and `"relation"` before editing.
- Config-test fixture: `config/config_test.go:89,121`.
- Existing v0.11 initiative recap docs land under `docs/status/v0.11-status.md` in prior milestones — add a section there referencing this initiative's completion.

## Tasks

1. **Attach `link` and `unlink` under `tusk task` and delete `buildTaskCmds()`.**
   - Extend `(*App).buildTaskCmd()` in `internal/tui/commands.go` to register:
     | New `Use` | Handler |
     |-----------|---------|
     | `link <short_id> <relation_type> <short_id>` | `a.runLink` |
     | `unlink <short_id> <relation_type> <short_id>` | `a.runUnlink` |
   - Preserve `Short`, `Long`, `Args` exactly as in the current flat definitions.
   - Delete `(*App).buildTaskCmds()` from `internal/tui/commands.go` entirely — the bridge is retired.
   - In `internal/tui/app.go`, remove the `a.root.AddCommand(a.buildTaskCmds()...)` line. After this task, the root-wiring sequence for task commands is exactly: `a.root.AddCommand(a.buildTaskCmd())` followed by `a.registerMovedStubs()`.

2. **Extend `registerMovedStubs()` with `link` and `unlink`, then freeze the map.**
   - Add to the stub map in `internal/tui/moved.go`:
     ```go
     "link":   "task link",
     "unlink": "task unlink",
     ```
   - Final map size: sixteen entries. No further entries will be added in later initiatives.

3. **Rename the two MCP relation tools and their visibility group.**
   - In `internal/mcp/server.go`:
     - Line 117-118: rename map keys `"tusk_relation_add"` → `"tusk_task_link"` and `"tusk_relation_remove"` → `"tusk_task_unlink"`.
     - Line 136: rename group-set entry `"relation"` → `"task_relations"`.
     - Lines 412-452: update both `s.addTool("relation", mcp.NewTool("tusk_relation_add", ...), ...)` calls so the first argument becomes `"task_relations"` and the tool names become `"tusk_task_link"` / `"tusk_task_unlink"` respectively. Update the tool descriptions only if they reference the old names; leave the schemas unchanged — tool rename must not alter request/response shape.
   - In `internal/mcp/tools.go`: rename `handleRelationAdd` → `handleTaskLink` and `handleRelationRemove` → `handleTaskUnlink`. Update the handler-reference call sites inside `server.go`'s `addTool` calls.
   - Search the `internal/mcp/` package for any remaining occurrences of `tusk_relation_` or `"relation"` as a group-string literal; update them. Do **not** touch usage of `relation` as a domain concept (e.g., `relationSvc`, `Relation` types, SQL column names, filter predicates) — that is the service-layer name of the concept and stays the same.

4. **Update MCP and config tests for the rename.**
   - `internal/mcp/server_test.go:68,72-76,114,171`: replace `"relation"` group-string literal with `"task_relations"`; replace `tusk_relation_add` / `tusk_relation_remove` with `tusk_task_link` / `tusk_task_unlink`.
   - `config/config_test.go:89,121`: same rename for the TOML fixture (`disabled_tool_groups = ["task_relations"]`) and its assertion.
   - `tests/e2e/mcp_test.go`, `tests/e2e/mcp_task_queue_test.go`: grep for `tusk_relation_` and any `"relation"` group references; rename. Leave domain `relation_type` argument values (`blocks`, `relates_to`, `duplicates`) alone — those are MCP input data, not tool-identity strings.
   - `internal/mcp/server.go:237` contains the phrase "tags, relations, and annotations" in a tool description — that is prose, not an identifier; leave it alone.

5. **Sweep e2e tests for the CLI `link`/`unlink` verbs.**
   - In every `tests/e2e/*_test.go` file, update arg slices whose first element is `link` or `unlink`: prepend `"task"`. Example: `{"link", a, "blocks", b}` → `{"task", "link", a, "blocks", b}`.
   - Likely high-touch file: `relations_test.go`. Search before assuming.
   - Update `internal/tui/commands_test.go` for any direct Cobra invocations of `link` / `unlink`.

6. **Documentation and release notes.**
   - `docs/configuration.md`: find every reference to `tusk_relation_add`, `tusk_relation_remove`, and the `"relation"` MCP visibility group; update to the new names. If the doc has a default-groups listing, update it.
   - `docs/status/v0.11-status.md`: if the file does not yet exist, create it with a single `## Initiative: tusk task Subcommand Group` section summarizing that every task-scoped verb now lives under `tusk task`, `runAdd`/`runInfo` have been renamed to `runCreate`/`runGet`, MCP tools `tusk_relation_add`/`tusk_relation_remove` have been renamed to `tusk_task_link`/`tusk_task_unlink`, and the MCP visibility group `"relation"` has been renamed to `"task_relations"`. If the file already exists from a peer v0.11 initiative, append the section in place.
   - Do **not** back-edit `docs/status/v0.3-status.md` or `docs/releases/v0.3.md` — historical docs stay historical.
   - Do **not** attempt shell completion regeneration in this phase unless a `tusk completion` subcommand already exists and is reachable from `make`. If not, add a single sentence to the v0.11 status doc noting that consumers should regenerate completions after upgrade.
   - **PRODUCT.md check:** `PRODUCT.md` already describes the `tusk task` surface as the current shape (see lines 158-236 of `PRODUCT.md`), so no edits are required there. Verify by grepping for `tusk add `, `tusk info `, `tusk link ` as bare flat invocations — there should be none. If any are found, update them to the `tusk task` form.
   - Run `make vet`, `make test`, `make test-e2e`. All must pass.

## User-Visible Behaviors to Preserve

At the end of this phase:

- Every behavior preserved by Phases 1-3 still works.
- `tusk task link <a> <type> <b>` and `tusk task unlink <a> <type> <b>` behave identically to the flat predecessors, including cycle detection on `blocks`, inverse-relation derivation, and both text and JSON output shapes.
- `tusk link a blocks b` and `tusk unlink a blocks b` print the targeted hint and exit non-zero.
- All sixteen migrated verbs are reachable via `tusk task <verb>` and print hints when invoked via their old flat path.
- `tusk task` bare invocation prints usage and exits 0.
- `tusk task --help` lists every subcommand with its `Short` description.
- MCP clients calling `tusk_task_link` and `tusk_task_unlink` receive identical request/response shapes to what `tusk_relation_add` / `tusk_relation_remove` returned on main, including error paths. MCP clients still calling the old tool names receive the server's standard "unknown tool" error — there is no backward-compat alias.
- MCP config files that declared `disabled_tool_groups = ["relation"]` must be updated to `"task_relations"`; this is documented in the v0.11 status note as a breaking change.
- Every non-relation MCP tool (`tusk_task_*`, `tusk_project_*`, `tusk_workflow_*`, `tusk_player_*`, `tusk_config_*`) is unaffected.
- The `buildTaskCmds` symbol no longer exists in the codebase.

## Changes Introduced

- **Deleted functions:** `(*App).buildTaskCmds` (bridge retired).
- **Renamed functions:** `handleRelationAdd` → `handleTaskLink`, `handleRelationRemove` → `handleTaskUnlink` in `internal/mcp/tools.go`.
- **Renamed MCP tool identifiers (breaking):** `tusk_relation_add` → `tusk_task_link`, `tusk_relation_remove` → `tusk_task_unlink`.
- **Renamed MCP visibility group (breaking):** `"relation"` → `"task_relations"`.
- **Modified files:** `internal/tui/commands.go`, `internal/tui/app.go`, `internal/tui/moved.go`, `internal/tui/commands_test.go`, `internal/mcp/server.go`, `internal/mcp/tools.go`, `internal/mcp/server_test.go`, `config/config_test.go`, `tests/e2e/mcp_test.go`, `tests/e2e/mcp_task_queue_test.go`, `tests/e2e/relations_test.go` (and any other e2e file that invokes `link`/`unlink` flat), `docs/configuration.md`, `docs/status/v0.11-status.md` (created or extended).
- **No new dependencies, no schema migrations, no new env vars.**
- **Bridge code cleared:** `buildTaskCmds()` deleted. No bridge code from this initiative remains in the codebase after this phase.
