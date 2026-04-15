# Phase 1 — Parent Skeleton + CRUD Verbs

## Goal

Introduce the `tusk task` Cobra parent command and migrate the five CRUD verbs — `create`, `list`, `get`, `modify`, `tree` — under it. Old flat names for these verbs become hidden stubs that print a targeted hint. The other eleven task-scoped verbs (lifecycle, claim/queue, relations) remain flat and unchanged in this phase.

## Prerequisites

- Base codebase at main (v0.10 complete).
- No prior phase of this plan required.

## Relevant Design Context

- Roadmap section: `ROADMAP.md` under `v0.11 — CLI Command Grouping` → `Initiative: tusk task Subcommand Group`.
- CLI wiring entry point: `internal/tui/app.go:129` — `a.root.AddCommand(a.buildTaskCmds()...)`.
- Current task command factory: `internal/tui/commands.go:17-128` — `buildTaskCmds()` returns a slice of 16 `*cobra.Command` values.
- Run handlers for the verbs in this phase live in `internal/tui/commands.go`: `runAdd`, `runList`, `runInfo`, `runModify`, `runTree`. Tests reference them in `internal/tui/commands_test.go`.
- Scope table and verb mapping: `ROADMAP.md` — "Story: Task CRUD and lifecycle under `tusk task`".

## Tasks

1. **Add the `buildTaskCmd()` parent factory.**
   - In `internal/tui/commands.go`, add a new function `func (a *App) buildTaskCmd() *cobra.Command`.
   - The returned command uses `Use: "task"`, `Short: "Manage tasks"`, no `Args`, and a `RunE` that calls `cmd.Help()` and returns `nil` — so bare `tusk task` prints usage with exit 0.
   - `Long` field: static text `"Task-scoped commands. Every task CRUD, lifecycle, claim, queue, and relation verb lives under this parent."` Do not attempt to auto-generate the subcommand listing; Cobra's default help renderer already lists registered subcommands.
   - Do not yet populate subcommands inside this function — subcommand attachment happens in task 3 below for this phase's five verbs, and in subsequent phases for the rest.

2. **Wire the parent into the root and keep the flat factory for unmigrated verbs.**
   - In `internal/tui/app.go:129`, replace `a.root.AddCommand(a.buildTaskCmds()...)` with two calls, in order:
     ```go
     a.root.AddCommand(a.buildTaskCmd())
     a.root.AddCommand(a.buildTaskCmds()...)
     ```
   - `buildTaskCmds()` is treated as **bridge code** for this phase — it continues to register the eleven not-yet-migrated verbs at the root. Its removal target is **Phase 4**, at which point the function is deleted entirely.
   - This wiring order means the parent is registered first; stub registration in task 5 uses `a.root.AddCommand` directly and happens after both factories.

3. **Rename `runAdd`→`runCreate` and `runInfo`→`runGet` first.**
   - In `internal/tui/commands.go`, rename the two handler methods on `*App`. The existing call sites inside `buildTaskCmds()` — the `add` and `info` entries in the residual slice — must be updated in the same edit so the codebase still compiles. After this task the flat `add` and `info` commands still exist in `buildTaskCmds()` but their `RunE` fields point at `a.runCreate` and `a.runGet` respectively.
   - In `internal/tui/commands_test.go`, update any direct references to `runAdd`/`runInfo` to use the new names. Leave Cobra invocation arg slices (`{"add", ...}`, `{"info", ...}`) alone at this step — those get their path prefix in task 6.
   - The rename is identifier-only. Function bodies, signatures, and semantics do not change. Running `go build ./...` after this task must succeed.
   - This task is ordered before subcommand wiring so that task 4 can reference the new identifiers directly without a transient compile error.

4. **Move the five CRUD subcommands under the parent and rename `add`→`create`, `info`→`get` at the Cobra `Use` level.**
   - Inside `buildTaskCmd()` (from task 1), build and attach these five subcommands via `parent.AddCommand(...)`. Source each from its current definition in `buildTaskCmds()`:
     | New `Use` | Old `Use` (in `buildTaskCmds`) | Handler |
     |-----------|-------------------------------|---------|
     | `create [title] [key=value...] [+tag...]` | `add [title] [key=value...] [+tag...]` | `a.runCreate` (already renamed in task 3) |
     | `list [filters...]` | `list [filters...]` | `a.runList` |
     | `get <short_id>` | `info <short_id>` | `a.runGet` (already renamed in task 3) |
     | `modify <short_id> [key=value...]` | `modify <short_id> [key=value...]` | `a.runModify` |
     | `tree [short_id]` | `tree [short_id]` | `a.runTree` |
   - Preserve `Short`, `Args`, `Long`, and any flags exactly as they appear today. `create` and `modify` both still carry the `--description`/`-d` and `--uda`/`-u` flags — these flags are eliminated in later v0.11 initiatives, not this one.
   - Remove these five entries from the slice returned by `buildTaskCmds()`. After this task, `buildTaskCmds()` returns eleven commands: `start`, `done`, `delete`, `annotate`, `link`, `unlink`, `next`, `claim`, `release`, `available`, `pop` (matching the order they appear in the current file, minus the five moved).

5. **Register hidden stub commands for the five migrated flat verbs.**
   - Add a new file `internal/tui/moved.go` exporting `func (a *App) registerMovedStubs()`.
   - The function defines a `map[string]string` whose keys are old flat verb names and values are the new `tusk <path>` invocation suffix (without the leading `tusk `). Phase 1 entries:
     ```go
     {"add": "task create", "info": "task get"}
     ```
     Plus three self-mapped entries for verbs whose name did not change but whose location did:
     ```go
     {"list": "task list", "modify": "task modify", "tree": "task tree"}
     ```
   - For each entry, call `a.root.AddCommand` with a `*cobra.Command` that has:
     - `Use` = the old flat verb name
     - `Hidden: true`
     - `DisableFlagParsing: true` (so users passing flags/args to the old name don't trip Cobra's flag validator before the hint fires)
     - `RunE` returning `fmt.Errorf("unknown command %q; did you mean 'tusk %s'?", old, newPath)`
   - Call `a.registerMovedStubs()` from `NewApp` in `internal/tui/app.go`, immediately after the existing `a.root.AddCommand(a.buildTaskCmds()...)` line. Subsequent phases will extend the map; the call site stays the same.
   - **Important — no duplicate registrations:** the stubs for `add`, `info`, `list`, `modify`, and `tree` must not collide with any still-flat registration of the same name. Task 4's slice-pruning step removes all five from `buildTaskCmds()` before this task registers the stubs, so the order must be task 4 → task 5. If task 5 is somehow applied first, Cobra will panic on duplicate command registration at `NewApp` time.

6. **Sweep e2e tests and unit tests for the five migrated verbs.**
   - Update argument slices in every file under `tests/e2e/` that currently invokes any of `add`, `list`, `info`, `modify`, `tree` as the first CLI argument. Each such slice must gain a leading `"task"` element and, for `add`/`info`, rename the verb to `create`/`get`. Files to review (from `grep` of `tests/e2e/`): `propagation_test.go`, `urgency_test.go`, `uda_test.go`, `task_queue_test.go`, `task_lifecycle_test.go`, `player_test.go`, `output_format_test.go`, `hierarchy_test.go`, `filtering_test.go`, `error_handling_test.go`, `tag_management_test.go`, `relations_test.go`, `tags_test.go`, `annotations_test.go`. Not every file touches these five verbs — only update call sites that do.
   - Leave every other verb (`start`, `done`, `delete`, `annotate`, `link`, `unlink`, `next`, `claim`, `release`, `available`, `pop`) unchanged in e2e tests. Those migrate in later phases.
   - Update `internal/tui/commands_test.go` in the same sweep for any direct Cobra invocations of the five verbs, in addition to the identifier rename already done in task 3.
   - Verify with `make vet`, `make test`, and `make test-e2e`. All three must pass before the phase is complete.

## User-Visible Behaviors to Preserve

At the end of this phase:

- `tusk task` prints usage text with exit 0.
- `tusk task create "title" project=backend +api priority=3` creates a task identically to the previous `tusk add` invocation, including all flags (`--description`, `--uda`, positional args, key=value inline fields).
- `tusk task list [filters...]`, `tusk task get <short_id>`, `tusk task modify <short_id> ...`, and `tusk task tree [short_id]` behave identically to their flat predecessors.
- `tusk add foo` prints: `unknown command "add"; did you mean 'tusk task create'?` and exits non-zero. Same shape for `info`, `list`, `modify`, `tree`.
- Every not-yet-migrated verb (`tusk start`, `tusk done`, `tusk delete`, `tusk annotate`, `tusk link`, `tusk unlink`, `tusk next`, `tusk claim`, `tusk release`, `tusk available`, `tusk pop`) continues to work flat, unchanged.
- All MCP tools are unaffected in this phase.
- Output rendering (text and JSON) is byte-for-byte identical for migrated verbs.

## Changes Introduced

- **New files:** `internal/tui/moved.go` (stub registration).
- **New functions:** `(*App).buildTaskCmd`, `(*App).registerMovedStubs`.
- **Renamed functions:** `(*App).runAdd` → `(*App).runCreate`, `(*App).runInfo` → `(*App).runGet`.
- **Modified functions:** `(*App).buildTaskCmds` — return slice shrinks from 16 to 11 entries. This is **bridge code**; removal target: **Phase 4**.
- **Modified files:** `internal/tui/app.go` (parent wiring + stub call), `internal/tui/commands.go` (new factory + rename + slice pruning), `internal/tui/commands_test.go` (rename + invocation updates), `tests/e2e/*_test.go` (14 files, verb-scoped sweep).
- **No schema migrations, no new dependencies, no new env vars, no MCP changes.**
- **Bridge code:** `buildTaskCmds()` continues to register the eleven unmigrated verbs flat. Phases 2, 3, and 4 each shrink this function further; Phase 4 deletes it.
