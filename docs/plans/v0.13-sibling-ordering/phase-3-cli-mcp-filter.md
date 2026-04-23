# Phase 3 — CLI, MCP, and Filter Surface

Initiative: v0.13 Sibling Ordering
Design spec: `docs/superpowers/specs/2026-04-23-sibling-ordering-design.md`

## Prerequisites

Phases 1 and 2 must be merged first.

## Inherits From

At the start of this phase:

- `Task.Order *float64`, `TaskUpdate.Order **float64`, and the sentinel errors `ErrCyclicParent` / `ErrOrderGapExhausted` all exist (Phase 1).
- `migrations/011_task_order` has backfilled the column and created the index (Phase 1).
- `TaskRepository.NextOrder`, `FirstOrder`, `NeighborOrders` exist and are used by the service (Phase 1 + 2).
- `TaskService.Create` auto-populates `Order` on nil input (Phase 2).
- `TaskService.Move(ctx, MoveRequest) (*domain.Task, error)` and `TaskService.Resequence(ctx, *uuid.UUID, *string) (int, error)` exist, emit events, and are covered by unit tests (Phase 2).
- `EventTaskMoved` / `TaskMovedPayload` are in `domain/event_task.go` and emitted from `TaskService.Move` (Phase 2).
- Absolute `TaskUpdate.Order` writes flow through the existing `task_modified` diff event (Phase 2).
- No CLI command invokes `Move` or `Resequence`; no MCP tool exposes them; no filter recognizes `order=…`.

The hierarchical renderers (`tree`, `list parent=…`, `list tree=…`) already receive tasks in `(order, created_at, id)` order because the sqlite `GetChildren` / `GetDescendants` queries carry the `ORDER BY` clause since Phase 1 — the renderers themselves have not changed.

## Goal

Wire the Phase 2 service capabilities through every user-facing surface: the inline-field parser (so `order=<float>` works in `tusk task create` and `tusk task modify`), a new `tusk task move` cobra command, a `--sort` flag on `list` and `tree`, two MCP tools (`tusk_task_move`, `tusk_task_resequence`), and filter-grammar recognition of `order=<value>` / `order=<a>..<b>`.

**User-visible behavior after this phase.** Humans and agents can reorder siblings:

```
tusk task create "Ship onboarding" project=backend order=2.5
tusk task modify a3f8b2c1 order=4.0
tusk task modify a3f8b2c1 order=
tusk task move a3f8b2c1 --before b7c9d4e2
tusk task move a3f8b2c1 --first --parent root
tusk task move --resequence <parent-id>
tusk task list project=backend order=1..3
tusk task tree --sort=urgency
```

MCP agents get equivalent access through `tusk_task_move` and `tusk_task_resequence`. The event-log and blocked-fields mechanisms behave without further configuration — `task_moved` was already registered in Phase 2; `mcp.blocked_fields.tusk_task_modify = ["order", "parent_id"]` in the existing config file format gives administrators immediate control.

## Tasks

### Task 3.1 — Register `order` in the inline task-field registry

Locate the task-field registry used by `tusk task create` and `tusk task modify`. It is the code path that turns inline tokens like `priority=3`, `project=backend`, `level=story`, `due=today` into a `domain.TaskUpdate`. The Task Level Taxonomy initiative added `level=` through this same mechanism — use the `Level` handling as the template.

Search entry points:
- `grep -rn "priority=" internal/tui/` to find the registry.
- `grep -rn "TaskUpdate" internal/tui/` to find the conversion layer.
- The file is most likely `internal/tui/task.go` or `internal/tui/expand.go`.

Add a new registry entry for `order`:

- **Key name:** `order`.
- **Type:** optional float (`float64`).
- **Create path:** parses `order=<float>` into the `Order *float64` field on the domain task passed to `TaskService.Create`. Reject `order=` (empty) on create — empty makes no sense before a row exists; emit a clear error (`order= requires a numeric value on create`).
- **Modify path:** parses `order=<float>` into `TaskUpdate.Order = doublePtr(ptrFloat(v))`; parses `order=` (empty) into `TaskUpdate.Order = doublePtr(nil)` so the service clears to NULL. Reject non-numeric values with the existing inline-parse error style (use `strconv.ParseFloat` and wrap the error so the user sees the offending token).
- Modifiers `+` / `-` are **not** accepted on `order` in this phase (arithmetic deltas are explicitly deferred — see spec §2). If the user passes `+order=5` or `-order=2`, the registry must return an error: `order does not accept + or - modifiers; use tusk task move to reposition`.

Register `order` in the task-field registry used by the filter parser as well so Task 3.5 has a direct hook — add a single line in whatever map powers the filter-side field lookup (see `filter/` layout).

Tests in `internal/tui/task_test.go` (or the nearest existing parser test file):
- Create with `order=2.5` sets the persisted order to `2.5`.
- Modify with `order=4.0` writes absolute `4.0`.
- Modify with `order=` clears to NULL.
- Modify with `+order=1` fails with the documented error.
- Create with `order=` fails with the documented error.
- Modify with `order=notanumber` fails with a parse error naming the offending token.

Acceptance: `go test ./internal/tui/...` passes. `tusk task create "x" order=2.5` and `tusk task modify <id> order=` both work end-to-end when run against a temp DB.

### Task 3.2 — `tusk task move` cobra command

Add `internal/tui/task_move.go` (new file). Wire the command in `internal/tui/task.go` (or wherever `tusk task` subcommands are registered — search `grep -rn "task.*AddCommand" internal/tui/`).

Command shape:

```
tusk task move <id> --before <target>
tusk task move <id> --after  <target>
tusk task move <id> --first [--parent <id>|--parent root]
tusk task move <id> --last  [--parent <id>|--parent root]
tusk task move --resequence <parent-id>
```

Implementation:

- Flags on the cobra command:
  - `--before string` — target short ID / UUID.
  - `--after string` — target short ID / UUID.
  - `--first bool`.
  - `--last bool`.
  - `--parent string` — accepts a short ID, a UUID, or the literal `root`.
  - `--resequence string` — parent short ID / UUID (empty = positional-less variant).
  - `--version int` — optional; when omitted, load the task once and use its current version.
  - Standard `--output` / `--no-color` / `--player` flags inherited from the parent command.
- `PreRunE` enforces:
  - Exactly one of `--before`, `--after`, `--first`, `--last`, `--resequence` is set. Error otherwise.
  - `--parent` is allowed only with `--first` or `--last`.
  - `--resequence` rejects every other ordering flag.
  - Positional `<id>` is required except when `--resequence` is used (which takes its parent through the flag, not a positional — keep the CLI shape consistent with existing admin commands like `tusk workflow modify`).
- `RunE`:
  - Resolve `<id>` → `uuid.UUID` via the existing short-ID resolver (search `resolveShortID` or similar in `internal/tui/`).
  - For `--before` / `--after`, resolve the target similarly.
  - Build a `service.MoveRequest`:
    - `TaskID`, `Version` (from flag or from a pre-read),
    - `Position` per flag,
    - `TargetID` if applicable,
    - `ParentID` per the `--parent` flag: unset → `nil`, `--parent root` → `doublePtr((*uuid.UUID)(nil))`, `--parent <id>` → `doublePtr(&resolved)`,
    - `ActorID` from the shared player-id resolver.
  - Call `taskService.Move(ctx, req)`.
  - For `--resequence`:
    - Resolve the parent (`root` → nil).
    - Call `taskService.Resequence(ctx, parentID, actorID)`.
  - Render the result. For Move, reuse the existing `renderTask` helper (JSON or text). For Resequence, text output: `resequenced N tasks under parent <short-id|root>`; JSON output: `{"rewritten": N, "parent_id": "<id>|null"}`.
- Error mapping:
  - `domain.ErrNotFound` → exit 1 with `task not found: <id>`.
  - `domain.ErrConflict` → exit 1 with `version conflict (task changed since read; pass --version after re-reading)`.
  - `domain.ErrCyclicParent` → exit 1 with `cannot move: would create a parent cycle`.
  - `domain.ErrOrderGapExhausted` → exit 1 printing the service's wrapped message (which already includes `parent <short-id>` and the resequence command hint).

Tests in `internal/tui/task_move_test.go`:
- Flag mutual-exclusion errors (before + after, before + first, before + resequence, parent without first/last).
- Successful `--before` execution via the test harness's in-memory service.
- `--first --parent root` re-parents to root.
- `--resequence` path writes the expected count.
- Error passthrough for each domain error.

Acceptance: `go test ./internal/tui/... -run Move` passes.

### Task 3.3 — `--sort` flag on `tusk task list` and `tusk task tree`

Edit the cobra commands for `list` and `tree`. Add a persistent flag (or per-command flag — match whichever pattern the existing `list`/`tree` commands use):

```
--sort order|urgency|created|priority|due
```

Defaults:
- `tree` → `order`.
- `list` (including `list parent=…`, `list tree=…`, `next`, `available`) → `urgency`.

Renderer wiring:
- The service continues to return tasks in the order produced by the repo query. The CLI renderer (`internal/tui/render.go` or the per-command render path) applies the chosen sort as the last step before emitting text or JSON.
- `--sort order` on `list` applies an in-memory sort on `(order ASC NULLS LAST, created_at ASC, id ASC)` via `sort.SliceStable`.
- `--sort urgency` continues to route through `UrgencyEngine.ScoreAndSort`.
- `--sort created` sorts by `created_at ASC`.
- `--sort priority` sorts by `(priority DESC, urgency DESC)`.
- `--sort due` sorts by `(due_at ASC NULLS LAST, urgency DESC)`.
- For `tree`, `--sort order` is the default and is satisfied by the SQL `ORDER BY` already in place; a non-default `--sort` triggers an in-memory re-sort of the flat task slice **before** `buildTree` is called (the tree builder preserves input order within each sibling group). Ensure `buildTree` still produces the expected nested shape under any choice — its only requirement is "parents appear before children" in the input, which the SQL recursive CTE preserves.

Tests in `internal/tui/tree_test.go` and a new `internal/tui/list_sort_test.go`:
- Tree with mixed `order` renders in ascending `order` by default.
- Tree with `--sort=urgency` renders descending urgency within each sibling group.
- List with `--sort=order` applies the alternative sort.
- List default is urgency.

Acceptance: `go test ./internal/tui/...` passes. Manual smoke: `tusk task tree --sort urgency` on a seeded DB produces the expected shape.

### Task 3.4 — `tusk_task_move` and `tusk_task_resequence` MCP tools

Add two new MCP tools next to the existing `tusk_task_*` tools (search `grep -rn "tusk_task_create\|tusk_task_modify" internal/mcp/`).

**`tusk_task_move`** JSON Schema (inputs):

```jsonc
{
  "type": "object",
  "properties": {
    "task_id":   { "type": "string", "description": "Task short ID or UUID." },
    "position":  { "type": "string", "enum": ["before", "after", "first", "last"] },
    "target_id": { "type": "string", "description": "Required when position is 'before' or 'after'." },
    "parent_id": { "type": ["string", "null"], "description": "First/last only. Absent = keep current parent; null = move to root; string = move under that parent." },
    "version":   { "type": "integer" },
    "player_id": { "type": "string" }
  },
  "required": ["task_id", "position", "version"]
}
```

Semantic validation inside the tool handler (before calling the service):

- `position in ["before","after"]` requires `target_id` to be present; `parent_id` must be absent.
- `position in ["first","last"]` requires `target_id` to be absent.
- `parent_id` presence handling (requires distinguishing "key absent" from "key present with JSON `null`" — a plain pointer field in a struct loses this distinction, so the handler must decode arguments into `map[string]json.RawMessage` first):
  - Key absent from the incoming `arguments` map → `MoveRequest.ParentID = nil`.
  - Key present with value `null` (`raw` is exactly `json.RawMessage("null")`) → `MoveRequest.ParentID = doublePtr((*uuid.UUID)(nil))`.
  - Key present with a non-empty string → unmarshal to string, resolve through the short-ID / UUID helper, then `doublePtr(&resolved)`.
  - Key present with an empty string or any other JSON type → return an MCP input validation error.

The handler threads `player_id` into the auto-registration flow (same pattern as every other `tusk_task_*` tool) and sets `MoveRequest.ActorID` accordingly.

Response: the moved task in the standard MCP task-response shape (same shape returned by `tusk_task_modify`). Include the fresh `version` and `order`.

**`tusk_task_resequence`** JSON Schema:

```jsonc
{
  "type": "object",
  "properties": {
    "parent_id": { "type": ["string", "null"], "description": "Short ID / UUID of the parent; null for root." },
    "player_id": { "type": "string" }
  },
  "required": ["parent_id"]
}
```

Response: `{ "rewritten": <int>, "parent_id": "<input>" }`. Resolve `parent_id` null / string via the same helper as Move.

Error mapping to MCP error objects follows the existing convention for Move-related errors (`ErrNotFound` → `resource_not_found`, `ErrConflict` → `version_conflict`, `ErrCyclicParent` → `invalid_request`, `ErrOrderGapExhausted` → `invalid_state` with the service's wrapped message).

**Blocked-fields + visibility:**
- No new config knob. The existing `mcp.blocked_fields.tusk_task_move` list in `tusk.toml` is honored automatically because the new tool walks the same filter code path as `tusk_task_modify`. Document that admins can add `"parent_id"` or `"target_id"` there. If the existing blocked-fields mechanism is tied exclusively to the `Task` struct's field names (not MCP tool input names), add an explicit pre-service check in the `tusk_task_move` handler that rejects the tool call when any of its declared inputs appears in the blocked list.
- Admin can hide the entire tool via `mcp.visibility.tools` — no code change needed.

Tests in `internal/mcp/` (file name matches the existing per-tool test convention):
- Successful `before` / `after` / `first` / `last` calls round-trip.
- Absent / null / string `parent_id` map to the three tristate values.
- `position=before` without `target_id` returns an input validation error.
- `position=first` with `target_id` returns an input validation error.
- Version conflict mapping.
- Cycle rejection mapping.
- Resequence happy path.

Acceptance: `go test ./internal/mcp/...` passes.

### Task 3.5 — Filter grammar: `order=<value>` and `order=<a>..<b>`

Locate the filter field registry. It is the layer that maps `priority=2..4`, `project=backend`, `level=story` into the internal `FilterExpr`. Task Level Taxonomy added `level=` — mirror that diff.

Search:
- `grep -rn "level=\|priority=" filter/`
- `filter/lexer.go`, `filter/parser.go`, `filter/resolver.go` are the likely touch points.

Implementation:

- In the field registry, add `order`:
  - Type: `float64`.
  - Accepts exact match (`order=2.5`), range (`order=2..5`), and empty-value match (`order=` → `IS NULL`).
  - Rejects `,`-separated sets (no meaningful use case; keeps the grammar tight).
  - Rejects `+` / `-` modifiers (filter contexts don't use them on numeric fields).
- In the resolver, map the `order` field to the SQL column `"order"` (quoted). Reuse the existing numeric comparison / range translation; follow the `priority` code path, not the `project` code path (project resolves via join — order does not).
- In `buildFilterExpr` (or equivalent SQL builder in the sqlite repo), ensure the `"order"` column name is always quoted. This may already be in place from Task 1.3 / Phase 1's optional registry entry; if not, add it now.

Tests:
- `filter/lexer_test.go` — `order=2.5`, `order=2..5`, `order=` lex into expected tokens.
- `filter/parser_test.go` — resulting AST nodes carry the right field / predicate.
- `filter/resolver_test.go` (or the sqlite filter integration test) — each grammar maps to the expected SQL predicate and returns the expected rows against a seeded DB.

Acceptance: `go test ./filter/... ./sqlite/... -run Order` passes.

### Task 3.6 — Render `order` in `tusk task get` text output

The existing `tusk task get <id>` text renderer prints one labeled line per field (title, status, priority, project, level, …). Add a line for `order` directly below `priority` so users can read the current value and decide whether to `tusk task move` or set absolute. JSON output already exposes `order` via the task-serialization shape — no change there.

- Render as `order: <value>` when `task.Order != nil`.
- When `task.Order == nil`, omit the line entirely (mirrors how an absent `level` is rendered).

Tests: extend the relevant `task_get` test in `internal/tui/task_test.go` or `internal/tui/render_test.go`.

Acceptance: `go test ./internal/tui/... -run "Get|Render"` passes.

## Preserved User-Visible Behavior

Everything preserved at the end of Phase 2, plus:

- `tusk task create` now accepts inline `order=<float>` without warning.
- `tusk task modify` accepts `order=<float>` (set) and `order=` (clear).
- `tusk task tree` default shape is still ordered; `--sort` is additive.
- `tusk task list` / `next` / `available` / `pop` default to urgency sort — ensure no accidental default swap.
- Existing MCP tools (`tusk_task_create`, `tusk_task_modify`, `tusk_task_get`, …) still work; they also surface `order` in the task response.
- Filter grammar continues to accept every prior field; `order=…` is additive.
- `tusk_task_move` and `tusk_task_resequence` honor the blocked-fields / visibility config; admins can hide them.

## Changes Introduced

**New files:**
- `internal/tui/task_move.go`
- `internal/tui/task_move_test.go`
- `internal/tui/list_sort_test.go` (or additions to an existing list test file)
- `internal/mcp/task_move.go` (or equivalent; match the existing per-tool file naming)
- `internal/mcp/task_move_test.go`
- `internal/mcp/task_resequence.go`
- `internal/mcp/task_resequence_test.go`

**Modified files:**
- `internal/tui/task.go` — register `move` subcommand, register `order` in the inline field registry.
- `internal/tui/list.go` / `internal/tui/tree.go` — `--sort` flag + renderer-side sort selection.
- `internal/tui/render.go` — `order` line in `task get` text output.
- `internal/mcp/server.go` (or wherever tools are registered) — register the two new tools.
- `filter/lexer.go` + `filter/parser.go` + `filter/resolver.go` — `order` field, range support, `IS NULL` case.
- `sqlite/task.go` — `"order"` filter column mapping, if not already in place from Phase 1.

**New MCP tools:** `tusk_task_move`, `tusk_task_resequence`.

**Bridge code:** none.

**Dependencies added:** none.
