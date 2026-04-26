# Phase 5 — Markdown renderer: annotations and notes

**Initiative:** ROADMAP.md Migration (v0.13)
**Spec:** `docs/superpowers/specs/2026-04-26-roadmap-md-migration-design.md`
**Prerequisites:** Phase 4.

## Inherits From

After Phase 4:

- `tusk task tree project=<name> --format markdown` emits a complete markdown document with H1 + project description + nested H2/H3/bullet structure, status checkboxes, and inline tokens (`status=`, `level=`, `priority=`, `due=`, `order=`, `uda.*=`, `+tag`).
- `markdownInputs.annsByTask` and `markdownInputs.notesByTask` are populated by `gatherMarkdownInputs` (Phase 3) but **not yet consumed** by the renderer.
- `internal/tui/tree_markdown.go` has the renderer skeleton with `renderMarkdownNode` writing title + description; this phase appends annotation and note lists at the correct render positions.
- Golden test fixture at `internal/tui/testdata/tree_markdown_basic.golden.md` exists with no annotations or notes.

## Goal

Render annotations and notes as labeled child lists. After this phase, the renderer is feature-complete per §3.2 of the design spec.

Two scopes:

- **Per-task annotations and notes** — appear as `**Annotations:**` and `**Notes:**` labeled lists under each task, after its description (when present), before its children.
- **Project-level notes** — notes with `task_id IS NULL` appear under the H1's project description, before the first task. No project-level annotations exist in the schema.

## Tasks

### Task 1 — Per-task annotation block

In `internal/tui/tree_markdown.go`, add:

```go
// renderAnnotationsBlock writes the labeled `**Annotations:**` list for a
// task. Indent applies to both the label and each list item, used for
// bullet-rendered tasks (depth >= 2). Heading-rendered tasks pass "".
// Emits nothing (no label, no blank lines) when anns is empty.
func renderAnnotationsBlock(w io.Writer, anns []*domain.Annotation, indent string) error {
    // implementation below
}
```

Output shape (heading-level, indent=""):

```
**Annotations:**
- 2026-04-15: Blocked by upstream API changes
- 2026-04-20: Resolved upstream

```

Bullet-level (indent="  "):

```
  - **Annotations:**
    - 2026-04-15: Blocked by upstream API changes
    - 2026-04-20: Resolved upstream
```

Format details:

- **Heading-level (`indent == ""`):** the label is on its own line, followed by one bullet per annotation with `- {YYYY-MM-DD}: {body}`. Trailing blank line after the last bullet.
- **Bullet-level (`indent != ""`):** the label becomes a sub-bullet `{indent}- **Annotations:**`, and each annotation is a deeper sub-bullet `{indent}  - {YYYY-MM-DD}: {body}`. No trailing blank line — the next sibling or child handles its own spacing.
- Date format: `ann.CreatedAt.UTC().Format("2006-01-02")`.
- Body: trim trailing whitespace; do not escape markdown-significant characters (consistent with title handling).
- Annotations are emitted in chronological order — the input slice is already sorted ascending by `created_at` (the new `GetByTasks` method preserves order; if the implementer notices this is not guaranteed, sort here).

Tests in `internal/tui/tree_markdown_test.go`:

- `TestRenderAnnotationsBlock_Empty` — empty slice, no output.
- `TestRenderAnnotationsBlock_Heading` — three annotations at indent="".
- `TestRenderAnnotationsBlock_Bullet` — same three at indent="  ".

### Task 2 — Per-task note block

In `internal/tui/tree_markdown.go`, add:

```go
// renderNotesBlock writes the labeled `**Notes:**` list for a task or for
// the project-level scope. Indent applies to both the label and each item.
// Emits nothing when notes is empty.
func renderNotesBlock(w io.Writer, notes []*domain.Note, indent string) error {
    // implementation below
}
```

Format per note (single bullet item):

```
- {YYYY-MM-DD} ({player_id}[, {meta_key}={meta_val}…]): {body}
```

- Date format: `note.CreatedAt.UTC().Format("2006-01-02")`.
- `(player_id)` — always emitted; helps multi-player roadmaps.
- Optional metadata: when `note.Metadata` is non-empty, append `, ` plus alphabetical-key `key=value` pairs inside the parentheses. Coerce values via `fmt.Sprintf("%v", v)` (Note metadata is `map[string]any`). Apply the same `quoteUDAValue` helper from Phase 4 when a value contains whitespace or a registered prefix character.
- Body: trim trailing whitespace.
- Multi-line bodies: the first line goes inline after the colon; subsequent lines are indented under the bullet by an additional 2 spaces, no leading bullet marker.

Bullet-level (indent="  "):

```
  - **Notes:**
    - {YYYY-MM-DD} (player[, k=v…]): body
```

Notes are emitted chronologically by `created_at` ascending. The Phase 3 `gatherMarkdownInputs` queries with `IncludeArchived: false` — archived notes never reach the renderer.

Tests:

- `TestRenderNotesBlock_Empty`
- `TestRenderNotesBlock_HeadingMinimal` — one note, no metadata.
- `TestRenderNotesBlock_HeadingWithMetadata` — one note with two metadata keys (sorted alphabetically).
- `TestRenderNotesBlock_Multiline` — note with `\n` in body; assert the second line is indented under the bullet.
- `TestRenderNotesBlock_Bullet` — same as heading variants but with indent="  ".
- `TestRenderNotesBlock_QuotedMetadata` — metadata value contains whitespace; assert it is double-quoted.

### Task 3 — Wire annotations and notes into `renderMarkdownNode`

In `internal/tui/tree_markdown.go`, update `renderMarkdownNode` to call the two new helpers immediately after the description block (and before recursing into children):

```go
// after description block has been written

indent := ""
if depth >= 2 {
    indent = strings.Repeat("  ", depth-2) + "  "
    // NOTE: bullet content sits one level deeper than the bullet itself.
    // The bullet line uses `strings.Repeat("  ", depth-2)`; sub-content
    // sits at +2 spaces.
}

if err := renderAnnotationsBlock(r.w, r.markdown.annsByTask[node.Task.ID], indent); err != nil {
    return err
}
if err := renderNotesBlock(r.w, r.markdown.notesByTask[node.Task.ID], indent); err != nil {
    return err
}

for _, child := range node.Children {
    if err := r.renderMarkdownNode(child, depth+1); err != nil {
        return err
    }
}
```

The exact indent expression depends on whether the description-blockquote indent already chosen by Phase 4 matches what we want for annotations/notes. Verify the Phase 4 implementation: `writeBlockquote` was called with the bullet's own indent (`strings.Repeat("  ", depth-2)`), but the blockquote itself starts at one indent level deeper (`> ` is the prefix, not `  > `). For annotations/notes we want the same "one level deeper than the bullet" pattern. Confirm by reading the actual Phase 4 code and adjust the indent constant accordingly. If Phase 4 used a different indent convention, document the choice in a comment so the renderer reads consistently.

### Task 4 — Project-level notes block

In `internal/tui/tree_markdown.go`, update `renderTreeMarkdown` to render a project-level Notes block immediately after the H1 description and before iterating roots:

```go
// after description blockquote is written
//
// Project-level notes have TaskID == nil and are stored under uuid.Nil
// in markdownInputs.notesByTask (Phase 3 grouping convention).
projNotes := r.markdown.notesByTask[uuid.Nil]
if len(projNotes) > 0 {
    if err := renderNotesBlock(r.w, projNotes, ""); err != nil {
        return err
    }
}

for _, node := range nodes {
    if err := r.renderMarkdownNode(node, 0); err != nil {
        return err
    }
}
```

Project-level notes are rendered at heading-level indent (`""`).

### Task 5 — Update the golden fixture and regenerate

1. Update `internal/tui/testdata/tree_markdown_basic.golden.md`:
   - Add a project-level note in the fixture's `markdownInputs.notesByTask[uuid.Nil]`.
   - Add 1–2 annotations and 1–2 notes on the milestone (heading-rendered).
   - Add 1 annotation and 1 note on the deepest leaf task (bullet-rendered).
   - Regenerate the golden file (using whichever update mechanism Phase 4 chose) and review the diff carefully — verify indentation, blank lines, and ordering match the rules.
2. Add `TestRenderTreeMarkdown_NotesAndAnnotations`:
   - Same fixture as the golden but exercised through unit assertions on a smaller subset (sentinel-based, not golden) so a future tweak to the golden file does not silently break the more focused assertion.
3. Add `TestRenderTreeMarkdown_ProjectLevelNotes`:
   - Fixture with no per-task notes, one project-level note.
   - Assert the project-level `**Notes:**` block appears between the H1 description and the first task.

### Task 6 — E2E coverage and v0.13 status doc update

E2E:

In `tests/e2e/tree_markdown_test.go`, extend the Phase 4 scenario:

- After creating the project + milestone + initiative + stories + tasks, add:
  - `tusk task annotate <milestone-id> "Initial scope ratified"`
  - `tusk note add "caching strategy notes" project=<project-name> --player german` (project-level note).
  - `tusk note add "retry needed" --task <leaf-task-id> --player german`.
- Run `tusk task tree project=<name> --format markdown`.
- Assert the output contains `**Annotations:**`, `**Notes:**`, the milestone annotation body, both note bodies.

Status doc:

Add a new section to `docs/status/v0.13-status.md` (create the file if it does not yet exist — check the directory; existing pattern is `v0.X-status.md` per finished initiative chunk). The section briefly recaps the renderer features — one paragraph naming the supported tokens and any deferrals. This matches the existing per-version recap convention.

If `docs/status/v0.13-status.md` already exists (because earlier v0.13 initiatives have been recapped), append a "ROADMAP.md Migration — markdown rendering complete" subsection. If it does not exist, create it from the structure of `docs/status/v0.12-status.md`.

## User-visible behaviors (acceptance criteria)

- All Phase 4 acceptance criteria still hold.
- `tusk task tree project=<name> --format markdown` now renders annotations and notes inline.
- Per-task annotations appear as `**Annotations:**` labeled lists at the appropriate indent (heading-level for H2/H3 tasks, sub-bullet for depth ≥ 2 tasks).
- Per-task notes appear as `**Notes:**` labeled lists with `(player_id[, key=value…])` annotations.
- Archived notes are never rendered.
- Project-level notes (`task_id IS NULL`) appear as a `**Notes:**` block under the H1 description, before the first task.
- Output is deterministic: annotations and notes are always emitted in `created_at` ascending order; metadata pairs are alphabetical by key.
- `make test` and `make test-race` pass.

## Bridge code introduced

None.

## Changes Introduced

- **Modified files:**
  - `internal/tui/tree_markdown.go` (`renderAnnotationsBlock`, `renderNotesBlock`, `renderMarkdownNode` updates, `renderTreeMarkdown` project-level notes block)
  - `internal/tui/tree_markdown_test.go` (annotation/note unit tests + new integration test)
  - `internal/tui/testdata/tree_markdown_basic.golden.md` (regenerated to include annotations/notes)
  - `tests/e2e/tree_markdown_test.go` (extended scenario)
  - `docs/status/v0.13-status.md` (new or extended recap)
- **No new files** in production code (golden fixture is the only "data" file and it already exists).
- **No new dependencies.**
- **No schema migrations.**
- **Public API additions:** none.
