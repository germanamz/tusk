# Phase 4 — Export / Import and E2E

Initiative: v0.13 Sibling Ordering
Design spec: `docs/superpowers/specs/2026-04-23-sibling-ordering-design.md`

## Prerequisites

Phases 1, 2, and 3 must be merged first.

## Inherits From

At the start of this phase:

- `Task.Order *float64` round-trips through the SQLite repo and through the service's create / update / move / resequence paths (Phases 1 + 2).
- `tusk task move`, `tusk task create … order=…`, `tusk task modify … order=…`, `tusk task tree --sort=…`, `tusk_task_move`, and `tusk_task_resequence` are all live (Phase 3).
- The filter grammar recognizes `order=<value>` and `order=<a>..<b>` (Phase 3).
- Every mutation that touches `order` or `parent_id` emits either `task_moved` (for `Move`) or `task_modified` (for `Create` / `Update` / `Resequence`).
- `tusk task get` surfaces `order` in text and JSON output (Phase 3).

JSON export / import, Markdown export / import, and CSV export **do not yet handle `order`** — the exporter emits tasks in whatever the current iteration yields (typically `created_at`), and the importer ignores any `order` key it receives. This phase closes that gap.

## Goal

Round-trip `order` through every data-portability surface, add a black-box E2E scenario that exercises the full feature, and close out the ROADMAP checkboxes. The status-doc and release-note updates are intentionally out of scope for this phase — those land only at milestone completion per project convention.

**User-visible behavior after this phase.**

```
tusk export --format json > ws.json           # tasks carry "order": <float|null>
tusk import --format json --input ws.json     # orders preserved exactly
tusk export --format markdown > plan.md       # siblings emitted in order
tusk import --format markdown --input plan.md # positions derived from document order
tusk export --format csv > tasks.csv          # new "order" column
```

The E2E suite covers move, resequence, cross-parent re-parent, cycle rejection, underflow rejection, order-clear, filter-range, and the JSON / Markdown round-trip.

## Tasks

### Task 4.1 — JSON export and import

Locate the JSON export / import code — either in `internal/portability/`, `internal/tui/export.go`, or a dedicated `service/export.go`. Use `grep -rn '"format".*"json"\|exportJSON\|importJSON' .` to pin it down.

**Export:**

- The task serialization struct (whatever type backs `tusk export --format json`) must include an `Order` field. JSON tag: `order`. Do **not** apply `omitempty` — null must round-trip as `"order": null` to preserve explicit clears.
- If the serialization struct is shared with the task-response shape (`taskJSON` in `internal/tui/render.go`), extend that struct. If it's a dedicated export struct, add the field alongside `Priority` / `Level`.
- Verify the existing golden-file fixtures for the JSON export: if fixtures are checked in under `testdata/` and assert exact JSON shape, update them to include the new field.

**Import:**

- The importer reads the incoming JSON, maps each record onto a `domain.Task`, and writes through `TaskService.Create` (or a dedicated bulk-create path — mirror the existing shape).
- When the record carries `"order": <number>`, set `task.Order = ptrFloat(v)` before the create call; the Phase-2 service logic then writes the literal value instead of auto-defaulting. When the record carries `"order": null`, set `task.Order = nil` and let the service auto-assign (this keeps empty workspaces that were exported before the feature landed importable — the null is treated as "no opinion"). When the key is absent, treat it exactly like null.
- Importing two sibling tasks with the same `order` is allowed; the sort tiebreak handles it. Do **not** validate uniqueness.

**Tests:**

- Extend the existing JSON round-trip test (search `grep -rn "round-trip\|Roundtrip\|RoundTrip" .`) with a fixture that includes three siblings with orders `1.5, 2.5, 3.75`. Export → re-import into an empty workspace → re-export; assert byte-for-byte equality of the two exports (or compare the deserialized struct trees, if that's the existing style).
- Add a case asserting `"order": null` imports as "no opinion" (the imported task receives an auto-default).

Acceptance: `go test ./internal/portability/... ./internal/tui/... -run Export\|Import\|Roundtrip` passes (adjust path to match the actual location).

### Task 4.2 — Markdown export and import

Same locator approach. Markdown export emits a bulleted tree; import parses bullets back into tasks.

**Export:**

- Walk each sibling group in `(order ASC NULLS LAST, created_at ASC, id ASC)` order — Phase 1 already guarantees this at the repo layer. The exporter just needs to preserve the order it receives; no new sort logic required. Verify by grep'ing the exporter for any `sort.Slice` on tasks and confirming it either already respects this order or is dropped in favor of the repo order.
- Do **not** emit the raw `order` float in the markdown body. Document position is the carrier. The export header (if any) records exporter version and timestamp as before.

**Import:**

- As the parser walks bullets, it assigns `order = 1.0, 2.0, 3.0, ...` to each bullet within its parent group. Set `task.Order = ptrFloat(sequence)` on the in-memory task before handing it to the create path. The Phase-2 service writes it verbatim.
- If the same author hand-edits the markdown to duplicate a bullet, the importer assigns sequential integers — no dedup, no special handling.

**Tests:**

- Round-trip: hand-write a three-level markdown tree as a test fixture, import it, re-export, assert byte-identical output (modulo header timestamps).
- Export-only round-trip: seed a workspace with non-integer orders (`1.0, 1.5, 2.0`), export, re-import into an empty workspace, verify the resulting tree has dense integers `1.0, 2.0, 3.0` (this is the documented Markdown-import behavior — float precision is lost through the markdown carrier).

Acceptance: `go test ./... -run Markdown` passes.

### Task 4.3 — CSV export `order` column

CSV is export-only. Locate the CSV writer (likely `internal/portability/csv.go` or similar — `grep -rn "csv.NewWriter" .`).

- Add an `order` column to the header tuple, placed after `priority` to match the JSON / display order.
- Emit the numeric float for non-null orders, empty string for NULL.
- No import path.

Tests: extend the CSV exporter test with a fixture row carrying `order = 2.5` and another with `order = nil`; assert the correct cells.

Acceptance: `go test ./... -run CSV` passes.

### Task 4.4 — E2E scenario `sibling_ordering`

Add a scenario in `tests/e2e/` (file name follows the existing convention — `task_*_test.go` or a dedicated `sibling_ordering_test.go`). The E2E harness runs every scenario in the 4-way matrix (2 DB modes × 2 output formats); scenario steps reference prior results via `$0.short_id` syntax.

Required steps:

1. Create a parent, then three children under it in a known order. Assert the children's persisted orders are `1.0, 2.0, 3.0`.
2. `tusk task tree parent=$0.short_id` shows the children in `1, 2, 3` order (both text and JSON outputs).
3. `tusk task move $2.short_id --before $1.short_id` (move child #3 before child #2). Assert resulting tree order: `child1, child3, child2`. JSON output's `order` values reflect a midpoint for `child3`.
4. `tusk task move $1.short_id --first` (child #2 jumps to the head). Assert resulting order: `child2, child1, child3`.
5. Create a second parent `other`. `tusk task move $1.short_id --after <child-of-other>`: assert the moved child now lives under `other`, at a position immediately after the referenced target.
6. `tusk task modify $3.short_id order=` clears order. Assert the task sinks to the end of its sibling group.
7. `tusk task move --resequence <parent-short-id>`: assert every child's order is rewritten to a dense integer sequence and verify exit status.
8. Cycle rejection: try `tusk task move <parent> --after <grandchild>`. Assert non-zero exit, error message mentions "parent cycle".
9. Underflow rejection: seed two tasks via `order=` absolute writes — first at `1.0`, second at `math.Nextafter(1.0, 2.0)` (seed directly via `tusk task modify`). Try `tusk task move <new-task> --before <second>`. Assert non-zero exit, error message mentions "no float64 midpoint" and includes a `--resequence` hint containing the parent short ID.
10. Filter range: create tasks spanning orders `1..5`, run `tusk task list order=2..4`; assert the returned set is exactly the matching rows.
11. `tusk task tree --sort urgency` on the seeded tree returns a different, urgency-ordered shape than the default.
12. JSON round-trip: `tusk export --format json` → import into a second temp workspace → `tusk export --format json` from the second workspace. Assert the second export matches the first.
13. Markdown round-trip (document-position-only): export to markdown, import into a third temp workspace, export again, assert output stability.

Keep the scenario resilient to non-deterministic timestamps: compare on `(parent_id, order, title)` tuples rather than full-row equality where needed, using the harness's existing tolerance helpers.

Acceptance: `go test -v ./tests/e2e -run SiblingOrdering` passes in the full 4-way matrix.

### Task 4.5 — ROADMAP ticks

Edit `ROADMAP.md`. Under `### Initiative: Sibling Ordering`, tick every checkbox that the full four-phase sequence has now closed. The initiative has three stories:

- **Story: Order field and sort policy** — all five bullets.
- **Story: `tusk task move` command** — all five bullets (including `--resequence` and `tusk_task_move` MCP tool).
- **Story: Order in export / import** — both bullets.

Do **not** touch the story headers' own checkboxes unless every bullet underneath is now complete (the project convention is "tick the story header once everything under it is shipped" — follow the pattern used for Task Level Taxonomy closure).

Do **not** create `docs/status/v0.13-status.md` or `docs/releases/v0.13.md`. Those are milestone-complete deliverables, not per-initiative. This phase closes the Sibling Ordering initiative; the v0.13 status + release docs land with the milestone in a later cleanup pass.

Acceptance: `git diff ROADMAP.md` shows exclusively checkbox flips under the Sibling Ordering initiative, no other edits.

## Preserved User-Visible Behavior

Everything preserved at the end of Phase 3, plus:

- `tusk export --format json` now emits `order` for every task; existing consumers that tolerate unknown fields keep working.
- `tusk import --format json` accepts workspaces with or without `order` keys.
- `tusk export --format markdown` emits siblings in order; existing markdown consumers that read bullets positionally keep working.
- `tusk import --format markdown` assigns order from document position.
- `tusk export --format csv` gains an `order` column; downstream spreadsheet users see one new column at a known position.

## Changes Introduced

**New files:**
- `tests/e2e/sibling_ordering_test.go` (or whatever file name the existing E2E convention prefers).

**Modified files:**
- JSON portability (task-serialization struct) — add `Order *float64` field.
- Markdown portability (exporter + importer) — sibling-group order preservation + doc-position import.
- CSV portability (exporter only) — add `order` column.
- `ROADMAP.md` — tick three stories under Sibling Ordering.
- Any relevant test-fixture golden files for JSON / CSV export.

**Bridge code:** none.

**Dependencies added:** none.

**Behavioral changes outside this initiative:** none.
