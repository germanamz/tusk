# Phase 4 — Markdown renderer: title line, structural shape, descriptions

**Initiative:** ROADMAP.md Migration (v0.13)
**Spec:** `docs/superpowers/specs/2026-04-26-roadmap-md-migration-design.md`
**Prerequisites:** Phase 3.

## Inherits From

After Phase 3:

- `tusk task tree --format markdown` is accepted by the CLI and gated behind single-project + no-rollup validation.
- `internal/tui/tree_markdown.go` contains a stub `renderTreeMarkdown` that emits `# {project display name}` + description blockquote + a placeholder line `<!-- tusk: markdown body lands in phase 4 -->`. **This phase removes that placeholder** and replaces it with full task body rendering.
- `markdownInputs` struct exists with `project`, `tagsByTask`, `annsByTask`, `notesByTask`, and `workflowFor` populated by `gatherMarkdownInputs`. **Annotations and notes are still unrendered in this phase** — Phase 5 wires them in.
- `projectDisplayName` and `writeBlockquote` helpers exist with unit tests.
- `Renderer` has an unexported `markdown *markdownInputs` field set by `setMarkdownInputs` immediately after construction.
- `treeNode` and `buildTree` exist in `internal/tui/tree.go` and are the data structure for the recursive walk.

## Goal

Replace the Phase 3 stub with the full body renderer for tasks. After this phase, `tusk task tree --format markdown` produces the complete dialect described in §3.2 of the spec **except** for annotations and notes (which arrive in Phase 5):

- Tree depth maps to H2 / H3 / nested bullets.
- Each task's title line carries `[status=…] [level=…] [priority=…] [due=…] [order=…] [uda.k=v…] [+tag…]` tokens in the documented order.
- Each task's description renders as a blockquote immediately below its title, with multi-paragraph support and bullet-indented nesting for depth ≥ 2.
- Status checkboxes (`[x]` / `[ ]`) are derived from the task's status's role in its workflow (done role → `[x]`; everything else → `[ ]`); delete-role tasks are excluded entirely (matching the existing `--all` / non-`--all` semantics from text/JSON tree).

## Tasks

### Task 1 — `formatMarkdownTitleLine` pure function

In `internal/tui/tree_markdown.go`, add:

```go
// formatMarkdownTitleLine builds the inline-token suffix for a task's title.
// Output shape: "{title} status=… level=… priority=… due=… order=… uda.k=v … +tag …".
// Tokens are emitted only when meaningful per §3.2 of the design spec.
//
// hasTaxonomy controls whether `level=` is emitted (matches the renderer's
// existing taxonomy-resolver pattern).
//
// workflow may be nil (renderer fell back when no workflow was resolvable);
// when nil, status binary classification cannot be performed and the
// status= token is conservatively omitted.
func formatMarkdownTitleLine(t *domain.Task, tags []*domain.Tag, hasTaxonomy bool, workflow *domain.Workflow) string {
    // implementation below
}
```

Implementation rules:

1. Start with `t.Title` verbatim. The renderer does **not** escape markdown-significant characters in titles (consistent with the spec: titles are human-authored and the user is responsible).
2. Append tokens in this fixed order, each separated from the prior content by a single space, only when meaningful:
   - **`status=<name>`** — emitted only when the status is *non-binary*. Binary means: status name equals the workflow's `initial` status name OR the status carries the `done` role. Use `workflow.Statuses[t.Status]` and check `cfg.HasRole(domain.RoleDone)`. Use `workflow.InitialStatusName()` (or whatever the workflow exposes — search `workflow.go` for the helper; if absent, scan `Statuses` for one with `RoleInitial`). When `workflow == nil`, omit the token.
   - **`level=<name>`** — only when `hasTaxonomy && t.Level != nil && *t.Level != ""`.
   - **`priority=<n>`** — only when `t.Priority > 0`. Format as `priority=3`.
   - **`due=<YYYY-MM-DD>`** — only when `t.DueAt != nil`. Format with `t.DueAt.UTC().Format("2006-01-02")` — date only, time component dropped per spec.
   - **`order=<float>`** — only when `t.Order != nil`. Format as `strconv.FormatFloat(*t.Order, 'g', 6, 64)` (up to 6 significant digits, no trailing zeros).
   - **`uda.<key>=<value>`** — iterate `t.UDA` in alphabetical key order. Coerce values via `fmt.Sprintf("%v", v)` (UDA values are `map[string]any` per `domain.Task.UDA`). Pass each coerced value through `quoteUDAValue` (next task) before emitting.
   - **`+tag`** — iterate the `tags` slice sorted alphabetically by `tag.Name`. Always trailing.
3. The function returns just the title-line string, **without** trailing newline. Callers add the newline.

Pure function: no side effects, no I/O, no `Renderer` access.

Tests in `internal/tui/tree_markdown_test.go` — table-driven `TestFormatMarkdownTitleLine`:

- Bare title, no metadata, no taxonomy, workflow with `pending` initial + `completed` done role → just the title (status=, level= absent).
- Title with `status=active` → emits `status=active` (active is non-binary in the kanban workflow shipped with tusk).
- Title in `pending` (initial) → no status token.
- Title in `completed` (done role) → no status token.
- Title with priority=3 → emits `priority=3`.
- Title with priority=0 → no priority token.
- Title with due=2026-04-26 → emits `due=2026-04-26`.
- Title with order=1.25 → emits `order=1.25`.
- Title with UDAs `{"team": "backend", "env": "prod"}` → emits in alphabetical key order: `uda.env=prod uda.team=backend`.
- Title with tags `[ship-blocker, api]` → trailing `+api +ship-blocker` (alphabetical).
- Combined: every token appears in the documented order.
- Taxonomy off + level present → `level=` token absent.
- Workflow nil → `status=` token always absent regardless of status value.

### Task 2 — `quoteUDAValue` helper

In `internal/tui/tree_markdown.go`, add:

```go
// quoteUDAValue returns s wrapped in double quotes when it contains
// whitespace or one of the registered prefix characters from the inline
// syntax lexer ('+', '-', '@'). Empty strings are emitted as `""`.
// Internal double quotes are escaped with `\"` to keep the output
// reparsable by the inline-syntax lexer when the markdown is dogfood-read.
func quoteUDAValue(s string) string {
    // implementation below
}
```

Quoting trigger:

- The string contains any whitespace character (`unicode.IsSpace`).
- The string starts with `+`, `-`, or `@`.
- The string is empty.

Otherwise the value is emitted bare.

When quoting, escape `"` → `\"` and `\` → `\\` in the value. Tests in `internal/tui/tree_markdown_test.go` — table-driven:

- `"plain"` → `plain`.
- `"with space"` → `"with space"`.
- `"+leading-plus"` → `"+leading-plus"`.
- `""` → `""` (empty string forces quoting).
- `"contains \"quote\""` → `"contains \"quote\""`.

### Task 3 — Heading vs. bullet rendering at correct tree depth

In `internal/tui/tree_markdown.go`, add the recursive walk:

```go
func (r *Renderer) renderMarkdownNode(node *treeNode, depth int) error {
    // depth 0 -> "## ", depth 1 -> "### ", depth >= 2 -> bullet
    // implementation below
}
```

Rules:

1. Skip nodes whose status carries the `delete` role (the existing helper `isDeleteRoleTask` in `internal/tui/tree.go:383` and `pruneDeleteRoleNodes` already does this for the text path; mirror the convention rather than re-walk). Actually the cleanest approach is: in the caller (Task 4), prune delete-role nodes from the tree before passing to `renderMarkdownNode` when `--all` is not set. Use `pruneDeleteRoleNodes` directly with the existing `workflowFor` from `markdownInputs`.
2. For depth 0 → write `## {titleLine}\n`.
3. For depth 1 → write `### {titleLine}\n`.
4. For depth ≥ 2 → write `{indent}- [{checkbox}] {titleLine}\n`, where:
   - `indent = strings.Repeat("  ", depth-2)` (2 spaces per depth past 2; depth 2 = no indent on the bullet itself).
   - `checkbox` = `x` when the task's status carries `RoleDone`, else `" "` (single space).
5. The checkbox marker applies to bullet rendering only. **Headings do not get a checkbox prefix** — for headings we rely entirely on the absence of the marker; the `status=` token (or its absence) carries the lifecycle signal. This matches the spec's stated dialect.
6. After the title line, if `node.Task.Description != ""`, call `writeBlockquote(r.w, node.Task.Description, indent)` where `indent` is `""` for headings and `strings.Repeat("  ", depth-2)` for bullets. The helper emits the blockquote and a trailing blank line.
7. Recurse into `node.Children` with `depth+1`.

`titleLine` is built via `formatMarkdownTitleLine(node.Task, r.markdown.tagsByTask[node.Task.ID], r.hasTaxonomy(node.Task.ProjectID), r.markdown.workflowFor(node.Task))`.

### Task 4 — Replace the Phase 3 stub

Rewrite `renderTreeMarkdown` in `internal/tui/tree_markdown.go`:

```go
func (r *Renderer) renderTreeMarkdown(nodes []*treeNode) error {
    if r.markdown == nil {
        return nil
    }
    proj := r.markdown.project
    if _, err := fmt.Fprintf(r.w, "# %s\n", projectDisplayName(proj.Name)); err != nil {
        return err
    }
    if proj.Description != "" {
        if err := writeBlockquote(r.w, proj.Description, ""); err != nil {
            return err
        }
    } else {
        // Trailing blank line between H1 and first task even when no description.
        if _, err := fmt.Fprintln(r.w); err != nil {
            return err
        }
    }
    for _, node := range nodes {
        if err := r.renderMarkdownNode(node, 0); err != nil {
            return err
        }
    }
    return nil
}
```

**Bridge code removed:** the `<!-- tusk: markdown body lands in phase 4 -->` line is gone.

The caller (`runTree` in `internal/tui/tree.go`) must prune delete-role nodes before calling `renderTree` when `--all` is unset. The text path already does this via `pruneDeleteRoleNodes` only in `--rollup` mode; markdown needs the same prune unconditionally (since markdown doesn't honor `--rollup`). Add:

```go
if format == "markdown" {
    if !showAll {
        // Markdown render needs delete-role nodes pruned the same way the
        // default text view excludes them. The `--all` flag already restricts
        // the fetch when not set, but the workflow-driven delete role check
        // is still necessary for any custom workflows that mark non-default
        // statuses with delete role.
        nodes = pruneDeleteRoleNodes(nodes, mdInputs.workflowFor)
    }
}
```

(Place this where the existing `--rollup` prune lives — search for `pruneDeleteRoleNodes` in `tree.go`.)

### Task 5 — Golden-file integration test

Create `internal/tui/testdata/tree_markdown_basic.golden.md` with the expected output for a small fixture (1 milestone with description, 1 initiative with description, 2 stories with description, 3 tasks with no description). Use realistic tokens so the golden file proves the title-line formatter works in context.

In `internal/tui/tree_markdown_test.go`, add `TestRenderTreeMarkdown_GoldenBasic`:

1. Build a fixture in-memory: project with description, taxonomy `[[milestone],[initiative],[story],[task,spike]]`, kanban workflow, 6 tasks with the right `Level`, `Status`, `Priority`, `DueAt`, `Order`, `UDA`, and tag assignments to exercise every token.
2. Construct `markdownInputs` directly (no DB needed).
3. Call `r.renderTreeMarkdown(nodes)`.
4. Read `testdata/tree_markdown_basic.golden.md` and compare. On mismatch, print the diff.
5. Update-mode: support `-update` flag (or a `TUSK_UPDATE_GOLDEN=1` env var — match whichever convention the existing testdata files use; if no convention exists, document the chosen one in a comment at the top of the file).

Add additional smaller golden cases as plain string assertions (not separate files):

- `TestRenderMarkdownNode_Bullet_WithDescription` — depth 2 task with multi-paragraph description, asserts the description is `>`-prefixed and indented.
- `TestRenderTreeMarkdown_TaxonomyDisabled` — same fixture, taxonomy off, asserts `level=` tokens are absent.
- `TestRenderTreeMarkdown_EmptyDescription` — project with empty description, asserts no blockquote under H1.
- `TestRenderTreeMarkdown_DeleteRoleTaskExcluded` — fixture has a `deleted`-status task; asserts it does not appear in output.

### Task 6 — E2E expansion

Extend the existing `tests/e2e/tree_markdown_test.go` (added in Phase 3):

1. Replace the Phase 3 single scenario with a richer fixture that creates:
   - One milestone (description + status=pending).
   - One initiative as child of the milestone (description).
   - Two stories under the initiative — one `[x]` (completed), one `[ ]` (active, with `status=active` token).
   - Two leaf tasks under each story.
2. Run `tusk task tree project=<name> --format markdown`.
3. Assert sentinels:
   - Output starts with `# {Display Name}` + description line.
   - Contains `## ` for the milestone heading and `### ` for the initiative heading.
   - Contains `- [x] ` for the completed story title.
   - Contains `- [ ] ` for the active story title with `status=active`.
   - Does **not** contain the Phase 3 placeholder comment.

The harness's two-mode execution (DB config × output format) covers both DB modes; we don't add format-mode multiplication.

## User-visible behaviors (acceptance criteria)

- All Phase 3 acceptance criteria still hold (`--rollup` reject, single-project requirement, empty workspace).
- `tusk task tree project=<name> --format markdown` emits a fully-formed markdown document with H1 + project description + nested H2/H3/bullet structure, status checkboxes, and inline tokens per §3.2 of the design spec.
- The Phase 3 placeholder comment is gone from the output.
- Delete-role tasks are excluded by default; `--all` includes them.
- Title-line tokens appear in the documented order, deterministically.
- `--rollup` still rejected.
- Multi-project tree still rejected.
- `make test` and `make test-race` pass.

## Bridge code introduced

None. This phase removes the Phase 3 placeholder bridge and introduces no new ones. Annotations and notes are silently unrendered (the spec says they are part of the renderer); their rendering arrives in Phase 5. This is acceptable because:

- Annotations and notes are not part of the title line or structural shape.
- The Phase 4 output is a strict subset of the Phase 5 output — every byte produced by Phase 4 will still appear in the Phase 5 output, with annotations/notes appended as labeled lists at their proper positions.
- This is *omission-without-bridge* rather than *placeholder-bridge*: there is no stub line, no marker comment, and no compile-time hook that Phase 5 must remove. Phase 5 simply *adds* lines below existing ones.

## Changes Introduced

- **New files:**
  - `internal/tui/testdata/tree_markdown_basic.golden.md`
- **Modified files:**
  - `internal/tui/tree_markdown.go` (`formatMarkdownTitleLine`, `quoteUDAValue`, `renderMarkdownNode`, full `renderTreeMarkdown`)
  - `internal/tui/tree_markdown_test.go` (extensive table-driven + golden-file tests)
  - `internal/tui/tree.go` (delete-role prune for markdown when `--all` is unset)
  - `tests/e2e/tree_markdown_test.go` (richer scenario)
- **No new dependencies.**
- **No schema migrations.**
- **Public API additions:** none (everything new is unexported in `internal/tui`).
