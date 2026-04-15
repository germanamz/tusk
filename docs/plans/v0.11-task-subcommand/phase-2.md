# Phase 2 — Lifecycle Verbs

## Inherits From

After Phase 1, the implementer will find:

- A `(*App).buildTaskCmd()` factory in `internal/tui/commands.go` that returns a `*cobra.Command` with `Use: "task"` and five registered subcommands: `create`, `list`, `get`, `modify`, `tree`.
- `(*App).runAdd` has been renamed to `(*App).runCreate`; `(*App).runInfo` has been renamed to `(*App).runGet`. All other run-handler names (`runStart`, `runDone`, `runDelete`, `runNext`, `runAnnotate`, `runLink`, `runUnlink`, `runClaim`, `runRelease`, `runAvailable`, `runPop`, `runList`, `runModify`, `runTree`) are unchanged.
- `internal/tui/app.go` registers the parent via `a.root.AddCommand(a.buildTaskCmd())`, then calls `a.root.AddCommand(a.buildTaskCmds()...)`, then calls `a.registerMovedStubs()`.
- `(*App).buildTaskCmds()` still exists as bridge code, returning eleven flat commands: `start`, `done`, `delete`, `annotate`, `link`, `unlink`, `next`, `claim`, `release`, `available`, `pop`. This function is scheduled for removal in Phase 4.
- `internal/tui/moved.go` defines `(*App).registerMovedStubs()` with a map containing five entries (`add`, `info`, `list`, `modify`, `tree`) and a stub-registration loop.
- E2E tests under `tests/e2e/` reference the five migrated verbs via `{"task", "<verb>", ...}` arg slices; all other verbs still use the flat form.

## Goal

Migrate the five lifecycle verbs — `start`, `done`, `delete`, `next`, `annotate` — from the flat `buildTaskCmds()` slice onto the `tusk task` parent. Register hidden stubs for the old flat names. Sweep e2e tests for these five verbs only.

## Prerequisites

- Phase 1 complete.

## Relevant Design Context

- Same roadmap section as Phase 1: `ROADMAP.md` → `Initiative: tusk task Subcommand Group` → "Story: Task CRUD and lifecycle under `tusk task`".
- Current flat definitions for these verbs: `internal/tui/commands.go` inside `buildTaskCmds()`, in the residual eleven-entry slice left over from Phase 1. Entries carry `Use: "start <short_id>"`, `"done <short_id>"`, `"delete <short_id>"`, `"next"`, `"annotate <short_id> <message...>"`.
- `annotate` positional-body semantics: positional body runs through inline-syntax parsing in a later v0.11 initiative. This phase changes invocation path only; do not touch how the body is parsed.

## Tasks

1. **Attach the five lifecycle subcommands to the `tusk task` parent.**
   - Extend `(*App).buildTaskCmd()` in `internal/tui/commands.go` to register five additional subcommands via the parent's `AddCommand`. Source each from its current definition in `buildTaskCmds()`:
     | New `Use` | Handler |
     |-----------|---------|
     | `start <short_id>` | `a.runStart` |
     | `done <short_id>` | `a.runDone` |
     | `delete <short_id>` | `a.runDelete` |
     | `next` | `a.runNext` |
     | `annotate <short_id> <message...>` | `a.runAnnotate` |
   - Preserve `Short`, `Long`, `Args`, and any flags exactly. None of these verbs carries flags today; double-check `annotate` against the current source — if any flag has been added between planning and execution, carry it over verbatim.
   - Ordering inside `buildTaskCmd()`: append the lifecycle verbs after the Phase 1 CRUD verbs. The exact registration order only affects `tusk task --help` rendering, not runtime behavior.

2. **Remove the five lifecycle verbs from `buildTaskCmds()`.**
   - After this task, `buildTaskCmds()` returns six commands: `link`, `unlink`, `claim`, `release`, `available`, `pop` (in whatever residual order matches the current source).
   - The function remains as bridge code; do not delete it yet. Its removal target is still Phase 4.

3. **Extend `registerMovedStubs()` with the five new entries.**
   - In `internal/tui/moved.go`, add these entries to the stub map:
     ```go
     "start":    "task start",
     "done":     "task done",
     "delete":   "task delete",
     "next":     "task next",
     "annotate": "task annotate",
     ```
   - The existing stub-registration loop handles them automatically — no new code paths.
   - Verify at runtime (or via a unit test in `commands_test.go`) that no Cobra "command already registered" panic occurs. It must not, because task 2 removed the flat registrations before the stubs go in.

4. **Sweep e2e tests for the five lifecycle verbs.**
   - In every `tests/e2e/*_test.go` file, update arg slices whose first element is one of `start`, `done`, `delete`, `next`, `annotate`: prepend `"task"`. Example: `{"start", id}` → `{"task", "start", id}`.
   - Do not touch arg slices for any other verb. Phase 1-migrated verbs are already updated; Phase 3 and 4 verbs are still flat.
   - Likely high-touch files based on the verb set: `task_lifecycle_test.go`, `annotations_test.go`, `task_queue_test.go`, `error_handling_test.go`, `propagation_test.go`, `output_format_test.go`, `hierarchy_test.go`, `urgency_test.go`. Other e2e files may also contain these verbs — search before assuming.
   - Update `internal/tui/commands_test.go` for any direct Cobra invocations of these five verbs.

5. **Verify.**
   - Run `make vet`, `make test`, `make test-e2e`. All must pass.
   - Manually sanity-check: `./bin/tusk task start --help` prints usage; `./bin/tusk start foo` prints `unknown command "start"; did you mean 'tusk task start'?` and exits non-zero.

## User-Visible Behaviors to Preserve

At the end of this phase:

- Everything Phase 1 preserved still works: `tusk task create/list/get/modify/tree`, and hint messages for their old flat names.
- `tusk task start <id>`, `tusk task done <id>`, `tusk task delete <id>`, `tusk task next`, `tusk task annotate <id> <message>` behave identically to the flat predecessors, including all status-transition semantics, urgency recomputation, claim interactions, and output rendering in both text and JSON.
- `tusk start foo`, `tusk done foo`, `tusk delete foo`, `tusk next`, `tusk annotate foo bar` each print the targeted hint and exit non-zero.
- The six still-flat verbs (`link`, `unlink`, `claim`, `release`, `available`, `pop`) continue to work flat.
- MCP surface unchanged.

## Changes Introduced

- **Modified files:** `internal/tui/commands.go` (subcommand attachment + slice pruning), `internal/tui/moved.go` (stub map extension), `internal/tui/commands_test.go`, `tests/e2e/*_test.go` (verb-scoped sweep).
- **No new files, no new functions, no schema or dependency changes, no MCP changes.**
- **Bridge code state:** `buildTaskCmds()` now returns six commands. Removal target unchanged: **Phase 4**.
