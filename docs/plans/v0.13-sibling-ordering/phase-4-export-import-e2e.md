# Phase 4 — E2E and ROADMAP Closure

Initiative: v0.13 Sibling Ordering
Design spec: `docs/superpowers/specs/2026-04-23-sibling-ordering-design.md`

## Scope Change — 2026-04-24

The original Phase 4 plan bundled "JSON / Markdown / CSV order round-trip" with the E2E scenario. That assumed data-portability export/import commands already existed. They do not — ROADMAP tracks them under a separate **Data Portability** initiative that has not yet started. The order-in-export/import bullets have therefore been moved into the three Data Portability stories (JSON, Markdown, CSV) and a pointer note left behind in the Sibling Ordering initiative. See the ROADMAP commit that accompanies this phase.

Phase 4 is now E2E + ROADMAP closure only.

## Prerequisites

Phases 1, 2, and 3 merged.

## Inherits From

At the start of this phase:

- `Task.Order *float64` round-trips through the SQLite repo and the service's create / update / move / resequence paths (Phases 1 + 2).
- `tusk task move`, `tusk task create … order=…`, `tusk task modify … order=…`, `tusk task tree --sort=…`, `tusk_task_move`, and `tusk_task_resequence` are live (Phase 3).
- Filter grammar recognizes `order=<value>` and `order=<a>..<b>` (Phase 3).
- Mutations touching `order` or `parent_id` emit `task_moved` (for `Move`) or `task_modified` (for `Create` / `Update` / `Resequence`).
- `tusk task get` / JSON output surface `order` (Phase 3).

## Goal

Ship a black-box E2E scenario that exercises every user-visible piece of the Sibling Ordering feature built in Phases 1-3, then close out the two ROADMAP stories the feature actually delivers.

The JSON / Markdown / CSV round-trip steps that appeared in the pre-rewrite Phase 4 plan are intentionally **not** included — they belong to the Data Portability initiative. When that initiative lands, its phases will include their own round-trip tests that exercise `order` as part of the broader export/import suite.

## Tasks

### Task 4.1 — E2E scenario `sibling_ordering`

Add `tests/e2e/sibling_ordering_test.go` following the existing `task_*_test.go` convention. The E2E harness runs every scenario in the 4-way matrix (2 DB config modes × 2 output formats); scenario steps reference prior results via `$N.short_id` syntax.

Required steps:

1. **Seed.** Create a parent, then three children under it in a known order. Assert each child's persisted order through `tusk task get` — children receive `1.0, 2.0, 3.0`.
2. **Default tree order.** `tusk task tree parent=$0.short_id` shows the children in `1, 2, 3` order.
3. **Move before.** `tusk task move $3.short_id --before $2.short_id` (move child #3 before child #2). Assert resulting tree order: `child1, child3, child2`. The moved child's `order` is now a midpoint between `1.0` and `2.0`.
4. **Move first.** `tusk task move $2.short_id --first` (child #2 jumps to the head). Assert resulting order: `child2, child1, child3`.
5. **Cross-parent reparent.** Create a second parent `other`, seed it with one child. Then `tusk task move <moved-child> --after <other-child>`: assert the moved child now has `parent_id` = `other` and sits immediately after the referenced target.
6. **Order clear.** `tusk task modify $N.short_id order=` (empty value) clears order. Assert the task's `order` is null and it sinks to the end of its sibling group under the `(order ASC NULLS LAST, created_at ASC, id ASC)` sort.
7. **Resequence.** `tusk task move --resequence <parent-short-id>`: assert every child's order is rewritten to a dense integer sequence (`1.0, 2.0, 3.0, …`) and exit status is zero.
8. **Cycle rejection.** Attempt `tusk task move <parent> --after <grandchild>`. Assert non-zero exit; stderr mentions "cycle".
9. **Underflow rejection.** Seed two sibling tasks with orders `1.0` and `math.Nextafter(1.0, 2.0)` via `tusk task modify … order=…`. Create a third task and attempt `tusk task move <third> --before <second>`. Assert non-zero exit; stderr mentions "no float64 midpoint" or equivalent and includes a `--resequence` hint.
10. **Filter range.** Seed siblings spanning orders `1..5`, run `tusk task list order=2..4` (or the closest filter syntax supported); assert the returned set is exactly the matching rows.
11. **Urgency sort override.** `tusk task tree --sort urgency` returns a different, urgency-ordered shape than the default `order` sort.

Keep the scenario resilient to non-deterministic timestamps: compare on `(parent_id, order, title)` tuples rather than full-row equality where needed, using the harness's existing tolerance helpers. Where the exact error-message wording is a stability risk, match on a substring.

**Acceptance:** `go test -v ./tests/e2e -run SiblingOrdering` passes in the full 4-way matrix.

### Task 4.2 — ROADMAP ticks

Edit `ROADMAP.md`. Under `### Initiative: Sibling Ordering`:

- **Story: Order field and sort policy** — tick every bullet (all six are delivered by Phases 1-3).
- **Story: `tusk task move` command** — tick every bullet (all five are delivered by Phases 2-3).

Do **not** tick the story headers themselves unless the project convention is "tick the header when every bullet under it is shipped." If it is, tick them too.

Leave the scope-change note (`> **Note (v0.13):** …`) and the new order-related bullets under Data Portability unchecked. Those stories are not part of this initiative.

Do **not** create `docs/status/v0.13-status.md` or `docs/releases/v0.13.md`. Those are milestone-complete deliverables that land when v0.13 as a whole closes.

**Acceptance:** `git diff ROADMAP.md` shows exclusively checkbox flips under the Sibling Ordering initiative's two stories, no other edits.

## Preserved User-Visible Behavior

Everything preserved at the end of Phase 3. No new user-visible behavior in this phase — it is test + documentation coverage only.

## Changes Introduced

**New files:**
- `tests/e2e/sibling_ordering_test.go`

**Modified files:**
- `ROADMAP.md` — tick two stories under Sibling Ordering.

**Bridge code:** none.
**Dependencies added:** none.
**Behavioral changes outside this initiative:** none.

## Deferred to Data Portability initiative

When the Data Portability initiative is executed, its plan must:

- Add `Order *float64` to whatever task-serialization struct backs JSON export.
- Make Markdown export emit siblings in the repo's existing `(order ASC NULLS LAST, created_at ASC, id ASC)` order; Markdown import assigns dense-integer order from document position.
- Add an `order` column to CSV export (after `priority`, empty cell for NULL).
- Include JSON + Markdown round-trip tests (these were steps 12-13 in the pre-rewrite plan).

These requirements are captured in the Data Portability initiative bullets added by the Phase 4 ROADMAP move.
