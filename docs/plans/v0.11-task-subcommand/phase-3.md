# Phase 3 — Claim and Queue Verbs

## Inherits From

After Phase 2, the implementer will find:

- `(*App).buildTaskCmd()` in `internal/tui/commands.go` registers ten subcommands under `tusk task`: `create`, `list`, `get`, `modify`, `tree`, `start`, `done`, `delete`, `next`, `annotate`.
- `(*App).buildTaskCmds()` bridge-code function returns six flat commands: `link`, `unlink`, `claim`, `release`, `available`, `pop`.
- `internal/tui/moved.go` stub map has ten entries. Removal-stubs are wired via `(*App).registerMovedStubs()` called from `NewApp`.
- E2E tests under `tests/e2e/` reference ten migrated verbs via `{"task", "<verb>", ...}` arg slices; the six claim/queue/relation verbs still use the flat form.
- MCP surface is unchanged from main; `tusk_relation_add` and `tusk_relation_remove` still exist under the `"relation"` visibility group.

## Goal

Migrate the four claim/queue verbs — `claim`, `release`, `available`, `pop` — from the flat `buildTaskCmds()` slice onto the `tusk task` parent. Register hidden stubs for their old flat names. Sweep e2e tests for these four verbs.

## Prerequisites

- Phase 2 complete.

## Relevant Design Context

- Roadmap section: `ROADMAP.md` → `Initiative: tusk task Subcommand Group` → "Story: Claim and queue under `tusk task`".
- Current flat definitions in `internal/tui/commands.go` inside `buildTaskCmds()` (residual six-entry slice from Phase 2): `claim <short_id>`, `release <short_id>`, `available [filters...]`, `pop [filters...]`.
- `pop` and `available` both honor the root persistent `--player` flag via `a.playerID`. Nothing about flag plumbing changes in this phase.

## Tasks

1. **Attach the four claim/queue subcommands to the `tusk task` parent.**
   - Extend `(*App).buildTaskCmd()` in `internal/tui/commands.go` to register:
     | New `Use` | Handler |
     |-----------|---------|
     | `claim <short_id>` | `a.runClaim` |
     | `release <short_id>` | `a.runRelease` |
     | `available [filters...]` | `a.runAvailable` |
     | `pop [filters...]` | `a.runPop` |
   - Preserve `Short`, `Long`, `Args`, and any flags exactly as in the current flat definitions. None of these carries its own flag today; if that has changed between planning and execution, carry the flag forward unchanged.

2. **Remove the four verbs from `buildTaskCmds()`.**
   - After this task, `buildTaskCmds()` returns two commands: `link`, `unlink`. The function remains as bridge code; removal target is still Phase 4.

3. **Extend `registerMovedStubs()` with the four new entries.**
   - Add to the stub map in `internal/tui/moved.go`:
     ```go
     "claim":     "task claim",
     "release":   "task release",
     "available": "task available",
     "pop":       "task pop",
     ```
   - Existing loop handles registration. Verify no duplicate-registration panic — task 2 removed the flat entries before the stubs attach.

4. **Sweep e2e tests for the four claim/queue verbs.**
   - In every `tests/e2e/*_test.go` file, update arg slices whose first element is one of `claim`, `release`, `available`, `pop`: prepend `"task"`. Example: `{"pop", "--player", "agent-1"}` → `{"task", "pop", "--player", "agent-1"}`.
   - Likely high-touch files: `task_queue_test.go`, `player_test.go`. Search before assuming — other files may use these verbs.
   - Update `internal/tui/commands_test.go` for any direct Cobra invocations of these four verbs.

5. **Verify.**
   - Run `make vet`, `make test`, `make test-e2e`. All must pass.
   - Spot-check: `./bin/tusk task pop --player german` behaves identically to the old `./bin/tusk pop --player german`; `./bin/tusk claim abc` prints the hint and exits non-zero.

## User-Visible Behaviors to Preserve

At the end of this phase:

- Everything Phase 1 and Phase 2 preserved still works.
- `tusk task claim <id>`, `tusk task release <id>`, `tusk task available [filters...]`, `tusk task pop [filters...]` behave identically to their flat predecessors, including player-resolution semantics (via the root `--player` persistent flag and `TUSK_PLAYER` env, if applicable), filter parsing, urgency-based ordering, atomic pop-and-claim semantics, and both text and JSON output shapes.
- `tusk claim foo`, `tusk release foo`, `tusk available`, `tusk pop` each print the targeted hint and exit non-zero.
- The two still-flat verbs (`link`, `unlink`) continue to work flat.
- MCP surface unchanged.

## Changes Introduced

- **Modified files:** `internal/tui/commands.go` (subcommand attachment + slice pruning), `internal/tui/moved.go` (stub map extension), `internal/tui/commands_test.go`, `tests/e2e/*_test.go` (verb-scoped sweep).
- **No new files, no new functions, no schema or dependency changes, no MCP changes.**
- **Bridge code state:** `buildTaskCmds()` now returns two commands. Removal target unchanged: **Phase 4**.
