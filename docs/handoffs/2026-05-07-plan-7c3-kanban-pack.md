---
type: handoff
title: Handoff 2026-05-07 — Plan 7.c.3 setup
session-date: "2026-05-07"
---

# Tusk v1 Rebuild — Session Handoff (Plan 7.c.3)

**Status:** Plans 1a → 7.c.2 shipped. v1 at `76956ab`. PR #351 (v1 → main, the cascade endpoint) still open from prior session, awaiting merge. Plan 7.c.3 (kanban pack content) is next.

## Open PRs

- https://github.com/germanamz/tusk/pull/351 — v1 → main, cascade endpoint, awaiting merge.

(No 7.c.3 stack member yet. Plans 1b through 7.c.2 all merged into v1.)

## Completed plans (most recent)

| Plan | Branch | Doc |
|---|---|---|
| 7 | merged (#358) | `docs/superpowers/plans/2026-05-06-tusk-v1-7-behavior-packs.md` |
| 7.b | merged (#359) | `docs/superpowers/plans/2026-05-07-tusk-v1-7b-node-types.md` |
| 7.c.1 | merged (#360) | `docs/superpowers/plans/2026-05-07-tusk-v1-7c1-pack-platform-and-ref.md` |
| 7.c.2 | merged (#361) | `docs/superpowers/plans/2026-05-07-tusk-v1-7c2-tags-pack.md` |

## v1 capability surface (after 7.c.2)

- Filesystem-as-source-of-truth markdown vault
- Node + edge graph indexed in SQLite (WAL + busy_timeout)
- TaskWarrior-flavored filter grammar with multi-hop edge traversal
- Semantic retrieval over Ollama (whole-document chunks, pure-Go cosine)
- MCP server (stdio + SSE) with full 1:1 tool surface mirroring CLI verbs
- Behavior-pack engine: 8 hook slots, recovery-aware dispatch, in-tree registration; one shipped Kind (workflow)
- Workflow pack: declarative state-machine validation, orphan-state recovery, drift surface
- `[node-types]` manifest section + property validation (string/int/float/bool/date/datetime/enum/markdown/list-of), structured rejections, drift-via-doctor for undeclared properties
- **`ref` property type:** declarable as `{ type = "ref", to = "<node-type>" }` and `list-of(ref)`. Auto-generates one edge type per ref property; collision-rejects against explicit `[edge-types.X]`. Frontmatter resolves as wikilink (node-ID lookup) or bare title (within `to` type). Validator integrates with Service.Create/Modify/reindex; doctor surfaces 4 new issue kinds (ref_dangling, ref_ambiguous, ref_type_mismatch, ref_cycle) via the property_drift table.
- **`tusk pack add <name|url>`:** URL-fetch (HTTP/HTTPS/file://), no cache, 30s timeout, 1 MiB cap, 3-redirect cap. Hard-error collision detection with `--force` splice override. Atomic write under workspace lock. Built-in name aliases: `kanban`, `vault`, `tags`. Engine has zero notion of packs — pure templating mechanism.
- **Tags pack** (data only): universal `tag` node type, `tagged` edge type (`from = ["*"]`, many-to-many). Lives at `packs/tags.toml` in the repo; reachable via `tusk pack add tags` after v1→main cascade.

## v1 design spec divergences shipped during 7.c.1 / 7.c.2

Important to remember when reading the master spec (`docs/superpowers/specs/2026-05-05-tusk-v1-rebuild-design.md`):

- §6 `tags:` frontmatter shorthand, auto-creation of missing tag nodes, path templating (`tags/<name>` configurable), manifest gate flag, "empty tag bodies" doctor surface — **all dropped from v1.c**. Users wanting the bare-string-array UX opt in per node-type via the 7.c.1 ref pattern.
- §7.1 layer 1 `[type-packs.<name>.<sub>]` override sections — **dropped**. Pack content materializes verbatim into standard sections.
- §7.1 `[workspace] type-packs = [...]` activation list — **dropped**. Engine has no runtime notion of packs.
- §10 filter shorthand `+tag` / `-tag` — **dropped from v1.c entirely**.
- §11.2 type-pack ergonomic shortcuts (`tusk ticket open`, `tusk note new`, etc.) — **deferred from v1.c**. Future plan (post-v1.c) revisits as a manifest config rather than inference.

## Next up: Plan 7.c.3 — Kanban pack (data + behavior)

Spec references:

- v1 design `docs/superpowers/specs/2026-05-05-tusk-v1-rebuild-design.md` §7.2 (kanban pack contents), §8.1 (workflow behavior shape)
- 7.c.1 sub-spec `docs/superpowers/specs/2026-05-07-tusk-v1-7c1-pack-platform-and-ref-design.md` §10 ledger #2
- 7.c.2 sub-spec `docs/superpowers/specs/2026-05-07-tusk-v1-7c2-tags-pack-design.md` §7 ledger entry

### Scope

- `packs/kanban.toml` containing:
  - `[node-types.ticket]` (probably with `summary`, `priority`, `due`, `status`, etc. — design call)
  - `[node-types.project]` (design call on properties)
  - `[edge-types.parent]` (one-to-many, acyclic)
  - `[edge-types.blocks]` (many-to-many, acyclic)
  - `[edge-types.tagged]` — **design tension here** (collides with tags pack's edge type)
  - `[behaviors.workflow.kanban]` — declarative state machine for tickets
- One end-to-end smoke test in `cmd/tusk/cmd_pack_add_test.go`.

### Open design tension to resolve in 7.c.3 brainstorm

The kanban pack wants `tagged` edges on tickets. The tags pack already declares `[edge-types.tagged]`. Three viable answers:

1. Kanban depends on tags being added first; if user runs `tusk pack add kanban` against a workspace that doesn't have tags, the manifest is incomplete (queries against `tagged` won't find the edge type) — fail loudly via the existing manifest validator (edge type referenced but not declared, if such a rule exists), or ship kanban without `tagged`-related declarations.
2. Kanban re-declares `[edge-types.tagged]` itself; collides with tags pack's declaration when both are added — user forced to use `--force` (loses one definition) or pick one pack. Bad UX.
3. Kanban omits any `tagged`-related semantics entirely; users opt in via the ref pattern on `[node-types.ticket]` (`{ name = "tags", type = "list-of", item-type = "ref", to = "tag" }`). Means kanban doesn't ship tag UX out of the box, but composes cleanly with whatever tag mechanism the user picks.

Likely answer is (3) — pure composition. But brainstorm to confirm.

### Other 7.c.3 design calls

- Ticket properties: which subset of {summary, priority, due, status, assignee, due-date, ...}? `assignee` would naturally be a `ref` to person — but person isn't declared anywhere. Either drop assignee from the v1.c kanban ticket schema, or have kanban declare `[node-types.person]` itself, or defer assignee to user opt-in.
- Workflow states: spec §8.1 sketch was pending → active → completed with one auto-revert transition. Reaffirm or adjust.
- Status property name: spec says `status`. Plan 7's workflow pack already takes a `status-property` config field, so this is just a config value.

### Process

Plan 7.c.3 needs a sub-spec brainstorm first (`superpowers:brainstorming`), then `superpowers:writing-plans`, then dispatch implementer subagent. Same pattern as 7.c.1 / 7.c.2.

## Conventions established (Plans 1b–7.c.2)

- Branching: stacked PRs off `v1`. Each plan = `feat/plan-N` branched from current v1 tip.
- Bundling: ~5–6 logical bundles per plan, one implementer subagent per bundle (general-purpose, sonnet). Smaller plans (like 7.c.2) ship as one bundle. Spec compliance verified informally with `make test` + spot-checks rather than dispatching separate reviewer subagents.
- No pauses between bundles — user reviews in the PR.
- Docs style: specs and plans avoid full function bodies. Type sketches, TOML examples, SQL DDL, JSON envelopes are fine. Tests in plan docs are shown in full (they're the precise behavioral spec); production code is described as behavior + signature + key invariants.
- Style rules: linter-enforced via lefthook pre-commit. Minimum 2-character identifiers, blank lines around err guards, named errors on shadow. Never `--no-verify`.
- Stale LSP diagnostics: `<new-diagnostics>` blocks frequently fire after subagent commits land — they show errors that aren't real. Verify with `go test ./... && make lint` before reacting.
- PR titles: conventional commit prefix (`feat(v1):`, `chore:`, etc.). The pr-title workflow enforces this.
- Plan/spec doc commits: spec lands as its own `docs(spec):` commit; plan as `docs(plan):` — both before any implementation commits on the branch.
- No Claude attribution in commits or PRs (no Co-Authored-By, no "Generated with Claude Code").

## Lessons from 7.c.1 / 7.c.2 worth keeping in mind

1. **Architectural shifts during brainstorm are the norm.** 7.c.1's brainstorm dropped the `[workspace] type-packs = [...]` activation list and `[type-packs.X.<sub>]` override sections that the master v1 spec had described. The result was simpler. Don't be afraid to flag that the v1 spec is older thinking and the right move is different.
2. **`ResolveRefs` fallback path (Bundle 4 of 7.c.1)** — when reindex's `ResolveEdges` (Plan 2) moves ref values from `Properties` to `Edges` before `ResolveRefs` runs, the resolver falls back to reading from `Edges`. Service.Create avoids this by ordering `ResolveRefs` before `ResolveEdges`. Two code paths with different value-source assumptions — code-path-dependent smell, may benefit from re-ordering reindex's pipeline rather than carrying the fallback. Worth a follow-up cleanup whenever someone is touching reindex.
3. **Plan docs containing CLI smoke tests need verified flag shapes** (7.c.2 lesson). The 7.c.2 plan wrote `tusk node create --type tag --title auth` (no `--path`) and `tusk edge add tagged tag/auth tag/security` (positional) in the plan; both forms don't match the actual CLI (`--path` is required, edge-add uses `--type`/`--source`/`--target` flags). The implementer corrected inline. For 7.c.3/7.c.4, either grep the actual CLI flag definitions before writing smoke tests in the plan, or instruct the implementer to treat smoke-test argument shapes as their own decision.
4. **Plan-spec drift caught at implementation time.** Plan 7.b Bundle 1 caught `title` (a reserved property name) being used as a sample property declaration in test fixtures. Implementer flagged and fixed it. Implementer subagents should be encouraged to flag (not silently change) plan/spec disagreements; they're often real bugs.
5. **MCP structured warnings via text-parsing remains a v1 expediency.** Plans 7 and 7.b both used stderr text-line parsing instead of a structured warning channel. 7.c.1's MCP envelope work used `errors.As(*node.RefValidationError)` for hard errors (structured) but warnings would still need promotion. Carry forward.
6. **Auto-cleanup test helpers.** `cmd/tusk/cmd_test_helpers_test.go`'s `chdir(test, dir)` registers `t.Cleanup` internally and returns nothing. Past plan docs sometimes wrote `previous := chdir(...); defer previous()` — wrong shape. Cross-check helper signatures before writing test code in plans.

## Where to start the new session

> "Pick up the Tusk v1 rebuild — Plan 7.c.3 (kanban pack content + workflow behavior on tickets) is next. Run a sub-spec brainstorm first (`superpowers:brainstorming`) to nail down: ticket schema (which properties), the `tagged`/tags-pack composition tension (recommended answer: kanban omits `tagged` entirely; users compose), workflow states (reaffirm pending→active→completed), and whether `assignee` ships in the v1.c ticket schema. Land the sub-spec at `docs/superpowers/specs/<date>-tusk-v1-7c3-kanban-pack-design.md`, then write the plan `docs/superpowers/plans/<date>-tusk-v1-7c3-kanban-pack.md`, then dispatch one implementer subagent (probably one bundle since this is data-only like 7.c.2 unless the workflow config drives runtime work). Same pattern as 7.c.2."

## Sync first

```bash
git fetch origin && git checkout v1 && git pull
```

Current v1 tip is `76956ab feat(v1): plan 7.c.2 — tags pack (#361)`. No local cleanup needed — `feat/plan-7c2` was already deleted in the previous session.
