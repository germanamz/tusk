# Tusk v1 — Kanban Pack Design (Plan 7.c.3 sub-spec)

- **Status:** Draft
- **Date:** 2026-05-07
- **Author:** German Meza
- **Scope:** Ship the built-in `kanban` pack content as data + workflow config — a TOML file at `packs/kanban.toml` in the tusk repo containing one `ticket` node type, two edge types (`parent`, `blocks`), and one workflow behavior instance (`[behaviors.workflow.kanban]`). No engine code. No platform extensions. No `[node-types.project]`. No `assignee`. The kanban pack is self-contained and composes with the tags pack rather than depending on or duplicating it.
- **Successor of:** the brainstorm dialogue captured during Plan 7.c.3 setup.

This document is a sub-spec of `2026-05-05-tusk-v1-rebuild-design.md` (the v1 rebuild design) and a follow-up to `2026-05-07-tusk-v1-7c1-pack-platform-and-ref-design.md` (Plan 7.c.1, which built the pack platform) and `2026-05-07-tusk-v1-7c2-tags-pack-design.md` (Plan 7.c.2, which established the `packs/<name>.toml` repository convention with the tags pack as the first pack content). It addresses ledger item #2 from 7.c.1 §10 (kanban pack content). It **explicitly diverges** from v1 design spec §7.2 in three ways enumerated below; the divergences follow the same "v1 spec is older thinking" pattern that 7.c.1 and 7.c.2 already established.

---

## 1. Goal & Scope

Plan 7.c.3 ships the `kanban` pack as **data + workflow config only**. The user runs `tusk pack add kanban` (or `tusk pack add file://./packs/kanban.toml` for testing); the pack platform from 7.c.1 fetches the file, validates it, and merges its sections into the user's `tusk.toml`. From that point forward, tickets are validated through the existing property validator (Plan 7.b) and the workflow behavior pack (Plan 7) — no new engine code is needed, only new pack content.

### 1.1 In scope

- A new file `packs/kanban.toml` at the tusk repo root containing four sections: `[node-types.ticket]`, `[edge-types.parent]`, `[edge-types.blocks]`, `[behaviors.workflow.kanban]`.
- One end-to-end smoke test that runs `tusk pack add file://./packs/kanban.toml` against the actual pack file and verifies tickets can be created, edges added, and workflow transitions validated end-to-end.

### 1.2 Out of scope (deferred or dropped from v1.c)

The following features either appeared in master spec §7.2 / §8.1 or could plausibly belong to a "kanban" pack but are explicitly **not** in v1.c kanban. Each is a deliberate cut, documented here so future readers don't reintroduce them by accident.

- **`[node-types.project]`.** Master §7.2 listed `project` alongside `ticket`. v1.c kanban treats project as a positional WBS role — a project is just a ticket with children. Users who want a distinct project node type can declare `[node-types.project]` inline in their workspace manifest. Rationale in §4.1 below.
- **`[edge-types.tagged]`.** Master §7.2 listed `tagged` as a kanban edge. v1.c drops it from kanban — composition via `tusk pack add tags` is the supported path. Rationale in §4.2 below.
- **`assignee` property and `[node-types.person]`.** People are an org primitive that belongs to a future `org` or `people` pack, not kanban. Users who want assignees can declare `{ name = "assignee", type = "ref", to = "person" }` inline alongside their own `[node-types.person]`.
- **`--partial` flag for `tusk pack add`.** Considered during the brainstorm as a way to let kanban re-declare `tagged` and skip the duplicate at install time. Dropped in favor of pure composition (kanban omits `tagged` entirely). Acceptable v1.c UX.
- **Auto-transition cascades.** The workflow pack's `auto-complete-parent` and `auto-revert-parent` directives decode but are inactive in v1.c (per behavior-packs spec §4.3). They are omitted from the kanban pack file rather than included as `false` — including them with `false` is visual clutter and including them with `true` emits a stderr "not yet active" notice on every workspace start.

### 1.3 Backward compatibility

Workspaces with no `ticket` node type and no `parent` / `blocks` / workflow-on-ticket configuration are unaffected. Workspaces that previously declared custom `ticket` / `parent` / `blocks` schema or a `[behaviors.workflow.<X>]` block governing tickets may collide with the pack on `tusk pack add kanban`; the 7.c.1 collision-detection path surfaces this and rejects without `--force`.

---

## 2. Pack File

`packs/kanban.toml`:

```toml
# Tusk built-in pack: kanban
#
# Adds a `ticket` node type with workflow-validated status, priority,
# and due-date properties; a WBS `parent` edge for hierarchy; and a
# `blocks` edge for dependency.
#
# A "project" in this pack is just a higher-level ticket — the WBS
# parent edge captures the hierarchy. There is no separate `project`
# node type; if you need one, declare `[node-types.project]` in your
# workspace manifest.
#
# Tagging is intentionally out of scope here. To tag tickets, run
# `tusk pack add tags` (composes cleanly), then either `[[tag/x]]`
# wikilinks in the body or a per-ticket ref opt-in:
#   properties = [{ name = "tags", type = "list-of",
#                   item-type = "ref", to = "tag" }]
#
# Customizing workflow states: edit BOTH the ticket `status` enum
# values AND the [behaviors.workflow.kanban].states list — they must
# stay aligned, since the property validator and the workflow
# validator each enforce their own slice of the contract.

[node-types.ticket]
description = "A unit of work tracked through a workflow"
properties = [
    { name = "status",   type = "enum", values = ["pending", "active", "completed"], required = true },
    { name = "priority", type = "enum", values = ["low", "medium", "high"] },
    { name = "due",      type = "date" },
]

[edge-types.parent]
description = "WBS parent — this ticket is a child of another ticket"
from        = ["ticket"]
to          = ["ticket"]
cardinality = "many-to-one"
ordered     = true
acyclic     = true
inverse     = "children"

[edge-types.blocks]
description = "This ticket blocks another from progressing"
from        = ["ticket"]
to          = ["ticket"]
cardinality = "many-to-many"
ordered     = false
acyclic     = true
inverse     = "blocked-by"

[behaviors.workflow.kanban]
applies-to      = ["ticket"]
status-property = "status"

states = [
    { name = "pending",   initial = true },
    { name = "active",    start = true },
    { name = "completed", terminal = true, done = true },
]

transitions = [
    { from = "pending",   to = "active" },
    { from = "active",    to = "completed" },
    { from = "active",    to = "pending" },
    { from = "completed", to = "pending" },
]
```

About sixty lines including the comment block.

**Notes on the schema choices:**

- `status` is `type = "enum"` with values matching the workflow state names. The property validator rejects unknown values *before* the workflow validator runs, giving an earlier and slightly nicer rejection path for "not even a known state name" cases. The cost is two sources of truth (the enum `values` and the workflow `states[].name`); a user customizing one must remember to update the other. The leading comment calls this out.
- `priority` is an enum without `required = true` — most tickets have a meaningful priority, but the engine doesn't force one. Users can drop the property from their manifest if they don't want it.
- `due` is a plain `date` (no time component) and is optional.
- `parent.ordered = true` so the WBS preserves sibling ordering ("first task, second task" framing). Standard for WBS-style hierarchies.
- `parent.cardinality = "many-to-one"` — a ticket has at most one parent; a parent has many children. `acyclic = true` prevents WBS loops.
- `blocks.cardinality = "many-to-many"` — a ticket can block many tickets and be blocked by many. `acyclic = true` prevents block-loop deadlocks (non-negotiable for dependency edges).
- `inverse = "children"` and `inverse = "blocked-by"` give convenient query handles without the user needing to remember the `<-` operator.
- The workflow instance is named `kanban` (matches the pack name). The instance key shows up in error messages and drift logs; `kanban` is more meaningful than naming it after the type it governs.
- `applies-to = ["ticket"]` with `status-property = "status"` matches the existing workflow-pack contract (behavior-packs spec §4.3).

---

## 3. Repository Layout

After 7.c.3 lands, the `packs/` directory holds:

```
packs/
  tags.toml          # 7.c.2 (shipped)
  kanban.toml        # 7.c.3 (this plan)
  vault.toml         # 7.c.4 (future)
```

The 7.c.1 alias map (`internal/typepacks/aliases.go`) is unchanged — it already points `kanban` at `https://raw.githubusercontent.com/germanamz/tusk/main/packs/kanban.toml`. The URL becomes live as soon as `packs/kanban.toml` lands on `main` (after the v1→main cascade). During 7.c.3's PR review, the URL returns 404; the smoke test uses `file://` against the repo-local path.

No `packs/README.md` ships in 7.c.3 — same call as 7.c.2. The per-pack TOML comment block is the per-pack documentation; this design doc plus the alias map are the global reference. A future plan may add a `packs/README.md` if/when the directory grows enough to merit it.

---

## 4. Divergences from Master Spec §7.2 / §8.1

Three explicit divergences from the master v1 rebuild design. Each is a deliberate "v1 spec is older thinking" cut following the same pattern as 7.c.1's drop of Layer 1 overrides and 7.c.2's drop of the `tags:` frontmatter shorthand.

### 4.1 No `[node-types.project]`

Master spec §7.2 listed `project` as a kanban node type alongside `ticket`. v1.c drops it.

A "project" in v1.c kanban is positional, not nominal — it's a ticket with children, identified by its position at the top of a WBS subtree rather than by its node type. The `parent` edge captures the hierarchy; queries like "give me all the top-level tickets" reduce to "tickets with no `parent` edge." Users who want a distinct `project` type can declare `[node-types.project]` inline in their workspace manifest after `tusk pack add kanban`.

Rationale: a separate project node type would need its own properties (which?), its own workflow (or none?), and its own edge interactions (does `parent` flow ticket → project? is there a separate `member-of` edge? both?). All of those are decisions a v1.c kanban pack would have to make on the user's behalf, and they're decisions a workspace owner can make better with knowledge of their actual workflow. Positional projects via `parent` are the lowest-opinion default.

### 4.2 No `[edge-types.tagged]`

Master spec §7.2 listed `tagged` as a kanban edge. v1.c drops it from the kanban pack.

The tags pack (7.c.2) already declares `[edge-types.tagged]`. If the kanban pack also declared `tagged`, the two would collide on a workspace that adds both packs — the user would face a hard error from the 7.c.1 collision-detection path and would have to resolve via `--force` (which silently drops one definition). Three alternatives were considered during the brainstorm:

1. **Pure composition (chosen).** Kanban omits `tagged` entirely. Users who want tags on tickets run `tusk pack add tags` separately. Order doesn't matter — both packs are independent.
2. **Hard dependency.** Kanban references `tagged` without declaring it; the engine fails loudly at manifest load if `tagged` isn't already there. Forces an ordered install and pays the tags-pack cost on every kanban user. Rejected: too restrictive.
3. **Re-declare with a `--partial` install flag.** Kanban declares `tagged` itself; a new `--partial` flag on `tusk pack add` skips duplicate sections. More complex, requires a platform extension, and has subtle UX issues around shape mismatches. Rejected: not worth the complexity for v1.c.

Pure composition keeps the engine's "zero notion of packs" invariant intact and matches how 7.c.2 framed the tags pack as a composable building block. The trade is that out-of-the-box kanban has no tagging UX until the user adds the tags pack — but the pack's leading comment block makes this discoverable.

### 4.3 No `assignee` property and no `[node-types.person]`

Master spec §7.2 didn't actually list these, but they were on the menu during the brainstorm. v1.c kanban drops both.

People are an org primitive that belongs in a future `org` or `people` pack, not in kanban. Bundling a `person` node type with kanban would couple "a unit of work" to "an organization model" in a way that's awkward for solo users (who don't need a person concept) and incomplete for team users (who need much more than a single property — roles, teams, emails, status, etc.). Users who want assignees on tickets in v1.c can declare `[node-types.person]` and `{ name = "assignee", type = "ref", to = "person" }` inline in their workspace manifest.

If a future `org` pack lands, this v1.c-era inline opt-in becomes the migration target — users with custom assignee declarations would either keep them or run `tusk pack add org` and let the pack take over.

---

## 5. User UX Patterns

After `tusk pack add kanban`, the supported authoring shapes:

### 5.1 Create a ticket via CLI

```bash
tusk node create --type ticket --path tickets/auth-migration.md \
    --prop status=pending --prop priority=high --prop due=2026-06-15
```

The property validator checks `status ∈ {pending, active, completed}`, `priority ∈ {low, medium, high}`, and `due` parses as ISO date. The workflow validator then runs (`status-property = "status"` matches; before-empty and after-pending is the legal initial transition).

### 5.2 Build the WBS hierarchy

```bash
tusk edge add --type parent --source tickets/auth-jwt.md --target tickets/auth-migration.md
```

The `auth-jwt` ticket becomes a child of `auth-migration`. Cycle detection runs (parent is acyclic). Sibling order is preserved on the parent (ordered=true).

Frontmatter alternative — children can declare their parent inline via wikilink, picked up by Plan 2's frontmatter-edge mechanism:

```yaml
---
type: ticket
title: Implement JWT signing
status: pending
priority: high
parent: "[[tickets/auth-migration]]"
---
```

### 5.3 Block dependency

```bash
tusk edge add --type blocks --source tickets/db-migration.md --target tickets/auth-jwt.md
```

The `db-migration` ticket blocks `auth-jwt` from progressing. Cycle detection runs (blocks is acyclic).

### 5.4 Workflow transitions via modify

```bash
tusk node modify tickets/auth-jwt.md --prop status=active
```

The workflow validator checks `(pending, active)` is in the transition table. ✓ The file is written. Re-running with `status=completed` succeeds (`(active, completed)` is legal). Trying `status=completed` directly from `pending` fails with a structured error listing valid targets (`["active"]`).

### 5.5 Composing with the tags pack

```bash
tusk pack add tags     # adds [node-types.tag] + [edge-types.tagged]
tusk pack add kanban   # adds ticket + parent + blocks + workflow
```

Order doesn't matter — both packs are independent. After both are added, four ways to apply tags to tickets exist:

1. Body wikilink: `[[tag/auth]]` in the ticket's markdown body emits a `tagged` edge.
2. Frontmatter wikilink array: `tagged: ["[[tag/auth]]"]` in the ticket's frontmatter.
3. Direct edge: `tusk edge add --type tagged --source tickets/auth-jwt.md --target tag/auth.md`.
4. Per-node-type ref opt-in: edit `[node-types.ticket]` in the workspace manifest to add `{ name = "tags", type = "list-of", item-type = "ref", to = "tag" }`, then write `tags: [auth, security]` in ticket frontmatter. (Note: pattern 4 emits edges under edge type `tags`, not `tagged` — see 7.c.2 §6 residual #2.)

Editing the manifest directly is the supported customization path in v1.c — there are no Layer 1 overrides.

---

## 6. Testing Strategy

One end-to-end smoke test, added to `cmd/tusk/` as a new test file `cmd_pack_kanban_smoke_test.go` (or as an additional case in the existing `cmd_pack_add_test.go` — implementer's call). The test exercises the kanban pack against a real workspace using the actual `packs/kanban.toml` file:

```
1. tusk init --name test
2. tusk pack add file://<repo>/packs/kanban.toml
   - verify tusk.toml gained [node-types.ticket],
     [edge-types.parent], [edge-types.blocks],
     [behaviors.workflow.kanban]

3. tusk node create --type ticket --path tickets/parent.md \
       --prop status=pending --prop priority=high
   - verify tickets/parent.md exists with the expected frontmatter

4. tusk node create --type ticket --path tickets/child.md --prop status=pending
   - same shape

5. tusk edge add --type parent --source tickets/child.md \
       --target tickets/parent.md
   - verify edge row exists in the index

6. tusk node modify tickets/parent.md --prop status=active
   - workflow validator accepts (pending → active)

7. tusk node modify tickets/parent.md --prop status=completed
   - workflow validator accepts (active → completed)

8. (negative) tusk node modify tickets/child.md --prop status=completed
   - workflow validator rejects (pending → completed not in table)
   - assert non-zero exit code; stderr/structured error surfaces
     ValidTargets including "active"
```

This is dual-duty validation: it validates the pack platform end-to-end against a real fixture, validates the kanban pack content decodes and merges cleanly under the manifest validator, and exercises both the property validator (via `priority` enum and `status` enum) and the workflow validator (via the `status` transitions, including a negative case).

No new unit tests beyond this. Like the tags pack, the kanban pack file has no logic to unit-test; it's data + workflow config. The existing `internal/typepacks` and workflow behavior-pack test suites cover the underlying mechanism against synthetic fixtures.

**CLI flag verification.** The 7.c.2 plan-doc lesson (handoff entry #3) caught wrong CLI flag shapes in the smoke-test pseudocode. To avoid recurrence, the implementer (or the planning step) will grep `cmd/tusk/cmd_node_create.go`, `cmd/tusk/cmd_node_modify.go`, and `cmd/tusk/cmd_edge_add.go` for the actual flag definitions before writing the smoke test code, and will treat any flag shapes shown in this design doc or in the plan doc as the implementer's call rather than a fixed contract.

---

## 7. Open Questions / Residuals

1. **Workflow ↔ enum drift cost.** The `status` property is `type = "enum"` with values mirroring `[behaviors.workflow.kanban].states[].name`. Customizing the workflow states means editing both sections — they must stay aligned. The pack's leading comment block calls this out. If real usage shows pain, a future plan can introduce a "string status with workflow-driven validation" mode (drop the property-layer enum) or a manifest-level "use workflow states for this enum" reference (single source of truth). Acceptable for v1.c.

2. **No `[node-types.project]` divergence with master spec §7.2.** The v1 design spec listed `project` as a kanban node type. v1.c drops it in favor of WBS-positional projects. The master spec is not edited by this plan; readers cross-reference via §4.1 above and the ledger entry in §8 below. If the divergence proves confusing, a future `docs(spec): supersede v1 §7.2 project paragraph` commit can land separately — same handling as the §6 tags-shorthand divergence from 7.c.2.

3. **No assignee / person.** People are deferred to a future `org` or `people` pack. Users who want assignees on tickets opt in inline via the ref pattern. Not a v1.c blocker; revisit when an `org`/`people` pack is on the roadmap.

4. **`packs/README.md` still not shipped.** Same call as 7.c.2. Per-pack TOML comment blocks plus this design doc plus the alias map serve as documentation. Add a directory README when 7.c.4 lands or when `packs/` grows enough to warrant it.

5. **No MCP surface for `tusk pack add`.** Carries forward unchanged from 7.c.1 §10 ledger #10. Kanban pack inherits that policy: workspace-config commands stay CLI-only.

6. **No auto-cascades.** The workflow pack's `auto-complete-parent` and `auto-revert-parent` directives are inactive in v1.c. The kanban pack omits them entirely rather than including them as `false` (visual clutter) or `true` (emits a stderr warning per behavior-packs spec §4.3). When the directives are activated in a future plan, kanban can be amended to include them.

---

## 8. Plan 7.c+ Ledger Updates

Plan 7.c.1's §10 ledger items are updated:

| # | Item | Status |
|---|---|---|
| 2 | Kanban pack content (`ticket` node type, `parent`/`blocks` edges, workflow on tickets) | **Shipped in 7.c.3** |
| (new) | Master spec §7.2 — `project` node type, `tagged` edge on kanban, implied `assignee`/`person` | Dropped from v1.c. Project is positional via `parent` edge; tags compose via the tags pack; people deferred to a future `org` pack. |

All other 7.c.1 / 7.c.2 ledger items carry forward unchanged.

The 7.c series remaining:

- **7.c.4** — vault pack content (`note`, `meeting`, `decision` node types; `references`, `relates-to` edges).
