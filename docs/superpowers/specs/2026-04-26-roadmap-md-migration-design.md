# ROADMAP.md Migration

**Branch:** `feat/roadmap-migration`
**Date:** 2026-04-26
**Status:** design approved, awaiting implementation plan
**Initiative:** v0.13 — Roadmap Self-Host → ROADMAP.md Migration

## Goal

Make `ROADMAP.md` a derived artifact, regenerated from tusk state by `tusk task tree --format markdown`. After cutover, no contributor edits the markdown directly — every change to the roadmap flows through `tusk task` commands. A CI check fails any drift between tusk state and the committed `ROADMAP.md`.

## Non-goals

- No top-level `tusk render` command. Markdown rendering is reachable only through `tusk task tree --format markdown`.
- No markdown *import* path. JSON portability covers round-trip; markdown is export-only.
- No automated parser of the existing `ROADMAP.md`. The source is too inconsistent (multi-paragraph milestone goals, mid-initiative `**Note (v0.13):**` blockquotes, trailing `**Deferred:**` blocks, exit-criteria lines) for a parser to be cheaper than a manual walk. Migration is a one-time manual procedure with a tracking checklist.
- No support for `--rollup` in markdown format. Different audience (terminal triage vs. canonical document); the combination is rejected with a clear CLI error.
- The fields `urgency_overrides`, `recurrence_rule`, `claimed_by`, `claimed_at`, `version`, `created_at`, `modified_at`, `id`, `short_id`, and any future attachment fields are silently dropped from markdown output. Round-trip lives exclusively in JSON.

## Scope

The initiative grows by one prerequisite story (project descriptions) and is otherwise three stories: markdown rendering, manual migration, and cutover.

### 1. Project description field (prerequisite story)

The H1 description block needs a real home, so projects gain a `description` field in lockstep:

- **Migration** — add `description TEXT NOT NULL DEFAULT ''` to the `projects` table. Existing rows take the empty default.
- **Domain** — `Project.Description string`. Empty string is the natural "no description" state, no need for `*string`.
- **Repository** — `sqlite/project_repository.go` reads and writes the new column.
- **Service** — `ProjectService.Create` accepts the field; `ProjectUpdate` adds `Description *string` (`nil` = no change, `*""` = clear, `*"text"` = set).
- **CLI** — `tusk project create <name> description=...` and `tusk project modify <name> description=...` via the existing inline syntax. `description=@./vision.md` works for free because the inline `@` expander already runs on string-typed values. `tusk project show` renders the description below the project name when non-empty.
- **MCP** — `tusk_project_create`, `tusk_project_modify`, the project payload returned by `tusk_project_list`, and the project resource gain a `description` string parameter / field. Empty string on modify clears it. The v0.12 blocked-fields mechanism applies unchanged.
- **Portability codec** — `PortableProject.Description string`. Existing JSON dumps decode cleanly because Go's JSON decoder treats a missing field as the zero value.
- **`config show`** — the read-only `[projects.*]` section gains a `description = "..."` line.

This is a clean, mechanical lift: one migration, no architectural choices.

### 2. Markdown render dialect

Hierarchy maps to markdown by **tree depth**, not by level rank. Level rank is emitted as an inline token for traceability but does not drive structural shape. Keeping shape depth-driven means rank gaps in the configured taxonomy (e.g., a milestone directly parenting a task) render predictably.

| Position | Markdown |
|---|---|
| Project | `# {project display name}` |
| Tree depth 0 (root tasks) | `## {title line}` |
| Tree depth 1 | `### {title line}` |
| Tree depth 2+ | nested bullet, 2 spaces per depth past 2 |

H4–H6 are deliberately not used.

**Project display name.** The H1 is derived from the project's `name` field by splitting on hyphens/underscores and title-casing each token: `tusk-roadmap` → `Tusk Roadmap`, `_default` → `Default`. The project description (introduced by the prerequisite story) renders as a `>` blockquote under the H1, omitted entirely when empty.

**Single-project requirement.** Markdown render is project-scoped: the rendered tree must belong to exactly one project. Three valid invocations:

- `tusk task tree project=<name> --format markdown` — explicit project filter.
- `tusk task tree <short_id> --format markdown` — subtree mode; the root's project drives the H1.
- `tusk task tree --format markdown` when the workspace contains tasks from a single project — implicit single-project workspace.

Multi-project tree fails fast: `markdown format requires a single project; pass project=<name> or run on a single-project workspace`.

#### Title line — inline tokens

Tokens follow the title in this fixed order, separated by single spaces:

```
{title} [status=…] [level=…] [priority=…] [due=…] [order=…] [uda.k=v…] [+tag…]
```

- **`status=<name>`** — emitted only when the status is *non-binary* (anything other than the workflow's `initial` status or a `done`-role status). Binary states use the checkbox alone: pending → `[ ]`, completed → `[x]`. Active, in-review, blocked, etc. → `status=active`/`status=in-review`/`status=blocked` and `[ ]`. Delete-role tasks are not emitted at all (matches the default `tusk task tree` behavior; `--all` includes them).
- **`level=<name>`** — emitted only when the project's effective taxonomy is non-empty.
- **`priority=<n>`** — emitted when priority > 0.
- **`due=<YYYY-MM-DD>`** — date only. Time-of-day on due dates is round-trip lossy in markdown view; preserved through JSON portability.
- **`order=<float>`** — up to 6 significant digits; only when non-nil.
- **`uda.<key>=<value>`** — every UDA, sorted by key. Values containing whitespace or one of the registered prefix characters (`+`, `-`, `@`) are double-quoted (`uda.note="see specs"`).
- **`+tag`** — every tag, sorted alphabetically, always trailing.

Headings carry the token line on the same line as the heading text. Bullets ditto.

#### Description block

Per "Shape 1": a fenced blockquote immediately below the title, separated by a blank line:

```markdown
## v0.13 — Roadmap Self-Host level=milestone

> Make tusk usable as the source of truth for its own roadmap.
>
> The milestone combines the foundational capabilities…
```

Multi-paragraph descriptions use a blank `>` line as a paragraph separator. Markdown content nested inside a description (lists, code blocks, inline links) survives because `>` blockquotes accept arbitrary nested markdown. For bullet-rendered tasks, the description sits indented under the bullet:

```markdown
- [ ] Define event types level=task
  > Cover task lifecycle, claims, relations.
```

A trailing blank line precedes the next entity (heading, bullet, or end of parent).

#### Annotations and notes

Both render as labeled child lists under their parent task. Annotations are immutable + chronological. Notes are emitted with author + select metadata, all players, non-archived only:

```markdown
### Initiative: Event Log level=initiative

> Append-only event table…

**Annotations:**
- 2026-04-15: Blocked by upstream API changes
- 2026-04-20: Resolved upstream

**Notes:**
- 2026-04-22 (german): caching strategy won't work
- 2026-04-23 (agent-1, topic=auth): retry logic needed
```

Same labeled-list shape on bullet tasks, indented under the bullet.

**Project-level notes** (notes with `task_id IS NULL`) render as a project-scoped `**Notes:**` block immediately under the H1 description, before the first task. Same chronological ordering, same per-line `(player_id[, key=value…])` annotation. Project-level annotations do not exist in the current schema, so no equivalent block.

#### Determinism guarantees

- Sibling order: by `order` ascending, then `created_at` ascending (existing tree sort).
- Tag order: alphabetical by tag name.
- UDA order: alphabetical by key.
- Annotation order: chronological (`created_at` ascending).
- Note order: chronological (`created_at` ascending), all players, non-archived only.

#### Flag interactions

- `--all` — include delete-role tasks. Same semantics as text/JSON.
- `--rollup` — **rejected** with `--format markdown`. CLI error: `rollup is not supported with --format markdown`.
- `--sort` — applies before rendering. Default for tree views is `order`, which gives deterministic markdown for cutover.
- `tusk task tree <short_id> --format markdown` — subtree mode. The subtree's root renders as H2 (depth 0 in the rendered tree); no project H1, no other roots. Useful for "regenerate just this milestone" workflows post-cutover.

### 3. Renderer architecture

Single new file: `internal/tui/tree_markdown.go`. One existing file touched: `internal/tui/tree.go`. No changes to `Renderer` struct.

#### Dispatch

`renderTree` (in `tree.go`) gains a third branch:

```go
switch r.format {
case "json":     // existing
case "markdown":
    return r.renderTreeMarkdown(ctx, nodes, inputs)
default:         // text — existing
}
```

The text and JSON code paths are unchanged.

#### Inputs gathered in `runTree`

When `a.format == "markdown"`, `runTree` fetches three things the existing path does not supply:

- **Tags per task** — `tagRepo.GetTaskTagsBatch(ctx, taskIDs)` already exists; one round-trip, no N+1.
- **Annotations per task** — `AnnotationRepository` today has only `GetByTask` (single) and `CountByTasks` (batch counts). Add `GetByTasks(ctx, taskIDs) (map[uuid.UUID][]*domain.Annotation, error)` mirroring the tag-batch shape.
- **Notes** — `NoteRepository.List` with `NoteListOptions{ProjectID: p, IncludeArchived: false}` returns every non-archived note for the project across all players in one call; group by `TaskID` in Go. Project-level notes (`TaskID == nil`) are folded into a project-level "Notes" block emitted under the H1 description.
- **Project description** — already on `*domain.Project` after the prerequisite story.

These get bundled into a `markdownInputs` struct passed to the renderer:

```go
type markdownInputs struct {
    project     *domain.Project
    tagsByTask  map[uuid.UUID][]*domain.Tag
    annsByTask  map[uuid.UUID][]*domain.Annotation
    notesByTask map[uuid.UUID][]*domain.Note
    workflowFor func(*domain.Task) *domain.Workflow
}
```

`workflowFor` is the existing `buildWorkflowLookup` helper used by `--rollup` — it resolves a task's governing workflow, which the renderer needs to classify a status as initial / done-role / non-binary.

#### Renderer file: `tree_markdown.go`

Three layers, all unexported:

1. **`renderTreeMarkdown(ctx, nodes []*treeNode, inputs *markdownInputs) error`** — entry. Writes the project H1 + description, then walks roots.
2. **`renderMarkdownNode(node *treeNode, depth int, inputs *markdownInputs) error`** — recursive walk. Dispatches on `depth` to heading vs. bullet rendering; emits description, annotations, notes; recurses.
3. **`formatMarkdownTitleLine(task, tags, hasTaxonomy, workflow) string`** — pure function: builds the `{title} [status=…] [level=…] …` line. No I/O. Pure shape makes this trivial to test in isolation.

Helper: `quoteUDAValue(s) string` for double-quoting UDA values containing whitespace or a registered prefix character.

#### Validation

- `runTree` rejects `--format markdown --rollup` early with a clear error.
- `--format markdown` is accepted on `tusk task tree` and `tusk task tree <short_id>`. Other commands continue to accept only `text|json` per their existing format validators.

### 4. Manual migration

The original spec called for `scripts/migrate-roadmap/main.go`. Discarded — the source has too many one-off shapes for a parser to be cheaper than a manual walk. Replaced by a tracking checklist.

#### Tracking doc

**Path:** `docs/status/v0.13-roadmap-migration.md`. Sits alongside the per-version status recaps.

**Shape:** mirrors `ROADMAP.md` structure as a flat-but-indented checklist. Each line is one migratable item; checked items optionally carry the resulting tusk short_id for navigation.

```markdown
# v0.13 — ROADMAP.md Migration Tracker

Tracks manual migration of ROADMAP.md into the `tusk-roadmap` project.
Once every box is checked and `tusk task tree --format markdown` matches
the source, this file (and the original ROADMAP.md) get replaced by the
regenerated output. Removed at cutover.

Taxonomy: `milestone:initiative:story:(task,spike)`.

## v0.1 — Foundation
- [x] Milestone → `m1a2b3c4`
- [x] Initiative: Core Domain & Storage → `i7d8e9f0`
  - [x] Story: Domain model → `s1234567`
    - [x] Define core types → `t11111111`
    ...

## v0.13 — Roadmap Self-Host
- [x] Milestone → `mabcdef01`
- [x] Initiative: Event Log → `i01020304`
  ...
- [ ] Initiative: ROADMAP.md Migration
  - [ ] Story: Markdown rendering
  - [ ] Story: Manual migration  ← in progress
  - [ ] Story: Cutover
```

#### Project setup (one-time, before migration starts)

```bash
tusk project create tusk-roadmap workflow=kanban description=@./tmp/vision.md
tusk project modify tusk-roadmap taxonomy.levels=milestone:initiative:story:(task,spike)
```

The taxonomy is recorded in the tracker doc preamble so the convention is visible to anyone reviewing the migration commits.

#### Workflow

1. Pre-create the tracker doc with one bullet per source heading/bullet, all unchecked.
2. For each item in document order:
   - Run `tusk task create "..." level=... project=tusk-roadmap parent=...` (with `description=@./tmp.md` when there's prose).
   - For items already complete in the source (`[x]`), follow with `tusk task done <short_id>`.
   - Tick the box in the tracker, append `→ <short_id>`.
3. Migration commits land in small, reviewable batches (one per milestone is reasonable).

### 5. Cutover

Cutover is a documented procedure, not a script:

1. **Tracker complete.** Every box in `docs/status/v0.13-roadmap-migration.md` is checked.
2. **Generate the regenerated file:**
   ```bash
   tusk task tree project=tusk-roadmap --format markdown > ROADMAP.regen.md
   ```
3. **Diff sanity check.** Compare `ROADMAP.regen.md` against `ROADMAP.md`. Expected differences:
   - Heading text identical (modulo new `level=` token suffixes).
   - Description prose identical (modulo `>` blockquote prefixing).
   - Bullet ordering identical (migration writes `order` to match document position).
   - Status checkboxes identical.
   Any unexpected delta gets fixed in tusk state (`tusk task modify`), not the markdown.
4. **Replace the source.** `mv ROADMAP.regen.md ROADMAP.md`, commit.
5. **Update contributor docs.** `CONTRIBUTING.md` (or wherever roadmap edits are documented) gets a "ROADMAP.md is generated" block: edits go through `tusk task` commands, regeneration via `make roadmap`.
6. **Add `make roadmap` target:**
   ```makefile
   roadmap:
       tusk task tree project=tusk-roadmap --format markdown > ROADMAP.md
   ```
7. **Delete the tracker doc** in the same commit.
8. **Add a CI check.** `make roadmap && git diff --exit-code ROADMAP.md` runs on every PR. Drift becomes a CI failure pointing at "you edited the markdown by hand, regenerate."

The CI check is the durable enforcement that replaces the discarded migration script's "guard against drift" role.

### 6. Testing

#### Unit tests — `internal/tui/tree_markdown_test.go`

- `formatMarkdownTitleLine` — table-driven across every token combination: status binary/non-binary, level present/absent, priority 0–4, due date, order, multiple UDAs (sort + quoting), multiple tags (sort), taxonomy on/off.
- Description blockquote formatter — single line, multi-paragraph (blank `>` separator), embedded markdown (list inside description), depth-indented for bullets.
- Annotation/notes formatter — empty, single, multiple, archived-excluded, multi-player.

#### Renderer integration tests

- Small fixture tree (one project, one milestone, one initiative, two stories, three tasks) with seeded annotations, notes, tags, UDAs. Compare full output against a golden file at `internal/tui/testdata/tree_markdown_basic.golden.md`.
- Empty workspace — `# {project name}` with empty description block (or no block at all when description is empty), no tasks.
- Taxonomy-disabled project — `level=` tokens absent on every line.
- Subtree render (`tusk task tree <short_id> --format markdown`) — depth offset starts at the subtree root, no project H1.

#### E2E — `tests/e2e/tree_markdown_test.go`

One scenario that creates a small fixture via CLI, runs `tusk task tree --format markdown`, and checks the output for sentinels (specific titles, tokens, status checkboxes). Black-box, sentinel-based — the harness already runs in two DB modes; full byte-exact assertions belong in unit tests.

#### Cutover sanity check (one-off, contributor-side)

After manual migration, `tusk task tree --format markdown | diff - ROADMAP.md` should produce only expected differences. Iterative — fix tusk state, regen, diff again. Once cutover lands, the `make roadmap && git diff --exit-code` CI step makes drift detection permanent.

## Decisions log

| Decision | Choice | Why |
|---|---|---|
| Migration approach | Manual with tracking doc | Source is too inconsistent for a parser to be cheaper than a manual walk; original script story discarded. |
| Renderer location | `internal/tui/tree_markdown.go` (extend `Renderer` dispatch) | Smallest diff; matches existing `text`/`json` pattern; no MCP/library consumer requested. |
| Render shape mapping | Tree depth → markdown structure (H1/H2/H3/bullets) | Predictable under taxonomy rank gaps; level-by-rank only drives the optional `level=` token. |
| Description placement | Blockquote (`>`) directly below title at every level | Uniform rule, parses unambiguously, supports multi-paragraph + nested markdown. |
| Product Vision | Project description (new field) | Single source of truth on the `tusk-roadmap` project; no synthetic root task; no duplication with PRODUCT.md. |
| Trailing source blockquotes | Folded into the parent's description | Strict dialect; "after the children" is a hand-author convention that doesn't need to round-trip. |
| `--rollup` interaction | Rejected with markdown format | Different audiences (terminal triage vs. canonical doc). Loud error beats cluttered output. |
| Project schema change | Add `Project.Description` as part of this initiative | Cheap; honest; needed for Product Vision; useful long-term. |

## Risks and edge cases

- **Multi-paragraph descriptions** — handled by blank `>` separator. Code blocks: emit verbatim with each line `>`-prefixed; renders fine in GitHub markdown.
- **UDA values with newlines** — extremely rare. v0.13 rejects them at render time with a typed error; user moves the content to `description`. Add quoting only if it shows up during migration.
- **Title containing `>` or `[`** — markdown-significant but human-authored. The renderer does not escape; user is responsible. Documented in a comment on the renderer.
- **Empty taxonomy on the project** — `level=` tokens disappear; structural shape works because it's depth-driven.
- **`tusk task tree --format markdown` against a project with no taxonomy** — works, with `level=` tokens omitted. Same applies to `_default`.
- **First-pass round-trip mismatch at cutover** — likely. Mitigation: iterative fix-tusk-state-then-regen-then-diff loop; the tracker doc keeps source of truth visible until the diff is clean.
- **Notes from many players cluttering output** — observed risk. v0.13 default is `all-players, non-archived`. If post-migration output is too noisy, add `--notes={none,own,all}` flag in a follow-up.
- **`_default` project after migration** — likely empty of roadmap tasks. Renders as `# Default` H1 with no description block. Acceptable.
- **Tree rendered via `tusk task tree --format markdown` mid-migration** — produces a partial roadmap. Useful for diff-checking progress; tracker doc is the source of "what's done" until cutover.
- **Empty result set** — `tusk task tree project=<name> --format markdown` against a project with zero tasks emits the H1 + description block (when present) and nothing else. With no project filter and zero tasks workspace-wide, emits nothing (single-project rule has nothing to anchor on; not an error).

## Out of scope / deferred

- Markdown import path — explicitly rejected by the v0.13 spec. JSON portability covers round-trip.
- A top-level `tusk render` command — explicitly rejected.
- `--notes` filter on the markdown renderer — deferred until a real noise problem surfaces.
- Project description editing through the `tusk_project_modify` MCP tool — included in the prerequisite story; no follow-up needed.
- CSV export — separately deferred under the Data Portability initiative.
