# Phase 3 — Markdown renderer: dispatch, validation, plumbing, stub

**Initiative:** ROADMAP.md Migration (v0.13)
**Spec:** `docs/superpowers/specs/2026-04-26-roadmap-md-migration-design.md`
**Prerequisites:** Phase 2.

## Inherits From

After Phase 2:

- Projects carry a `Description` field that is fully wired through CLI, MCP, codec, `project show`, and `config show`. The renderer in this phase will read it.
- `tusk task tree --format <text|json>` works exactly as today. The `--format` persistent flag (declared in `internal/tui/app.go:151`) currently accepts only `"text"` and `"json"`; this phase teaches `tusk task tree` (and only `tusk task tree`) to also accept `"markdown"`.
- `domain.Task`, `domain.Annotation`, `domain.Note`, `domain.Tag`, `domain.Workflow`, `domain.Project` all exist with their current shapes. No domain changes in this phase.
- `repository.TagRepository.GetTaskTagsBatch(ctx, taskIDs)` exists.
- `repository.AnnotationRepository` has `GetByTask` (single) and `CountByTasks` (batch counts), but **not** a batch read of full annotations — this phase adds that.
- `repository.NoteRepository.List(ctx, NoteListOptions)` accepts a `ProjectID` (required), `IncludeArchived bool`, and other filters. It returns the full list at once when no `TaskID` filter is set, which is what this phase needs.

## Goal

Wire the markdown format end-to-end at the CLI layer:

- Accept `--format markdown` for `tusk task tree` and `tusk task tree <short_id>`.
- Reject the combination `--format markdown --rollup` with a clear error.
- Reject multi-project trees with a clear error.
- Pre-fetch the inputs the future renderer will need (tags, annotations, notes, project, workflow lookup).
- Emit a **stub** markdown output: only the project H1 + description blockquote (when non-empty) and a single-line `<!-- tusk: markdown rendering body lands in phase 4 -->` placeholder for the body. No tasks rendered yet.

This is intentionally narrow: the CLI flag plumbing, the validation guards, and the input-gathering all land here; the actual title-line + structural rendering arrives in Phase 4. Splitting like this keeps the implementer-agent task count bounded and lets Phase 4 focus exclusively on the renderer body.

## Tasks

### Task 1 — `--format markdown` flag acceptance on `tusk task tree`

`internal/tui/app.go:151` declares `--format` as a persistent flag with help text `output format: "text" or "json"`. Other commands validate `format ∈ {text, json}` per-command.

In `internal/tui/tree.go`:

1. At the top of `runTree`, after the existing `sortMode` validation, add a markdown-aware format validator:
   ```go
   format := strings.ToLower(strings.TrimSpace(a.format))
   if format != "" && format != "text" && format != "json" && format != "markdown" {
       return fmt.Errorf("invalid format %q: tree supports text, json, or markdown", a.format)
   }
   ```
   (Adjust to match the existing validator pattern in this codebase — do not invent a new error helper. Look at `validateSortMode` for the local convention.)
2. If `format == "markdown"`, reject the `--rollup` combination immediately:
   ```go
   if rollup, _ := cmd.Flags().GetBool("rollup"); rollup {
       return fmt.Errorf("--rollup is not supported with --format markdown")
   }
   ```
3. Update the help text on the persistent `--format` flag to read `output format: "text", "json", or "markdown" (markdown is supported only on tree)`. Tweak just the string literal in `internal/tui/app.go`; do not change the flag type or add new flags.

### Task 2 — `AnnotationRepository.GetByTasks` batch method

In `repository/annotation.go`, extend the interface:

```go
type AnnotationRepository interface {
    Create(ctx context.Context, ann *domain.Annotation) error
    GetByTask(ctx context.Context, taskID uuid.UUID) ([]*domain.Annotation, error)
    GetByTasks(ctx context.Context, taskIDs []uuid.UUID) (map[uuid.UUID][]*domain.Annotation, error)  // NEW
    Delete(ctx context.Context, id uuid.UUID) error
    CountByTasks(ctx context.Context, taskIDs []uuid.UUID) (map[uuid.UUID]int, error)
}
```

In `sqlite/annotation.go`, implement `GetByTasks`. Mirror `GetTaskTagsBatch` in `sqlite/tag.go:190` for shape: build an `IN (?, ?, …)` clause from the slice, query, scan into the map keyed by `task_id`. Return `(map[uuid.UUID][]*domain.Annotation{}, nil)` for empty input — never nil.

In `repository/repository_test.go`, the existing stub test type implements the interface; add a stub `GetByTasks` that returns an empty map. (Search for `stubAnnotationRepo` and add the method alongside the existing ones.)

In `sqlite/annotation_test.go`, add a test `TestAnnotationRepo_GetByTasks_Batch` that:

1. Creates two tasks with two annotations each.
2. Calls `GetByTasks([taskID1, taskID2])` and asserts both keys are present with two annotations each, ordered ascending by `created_at`.
3. Calls `GetByTasks([])` and asserts an empty (non-nil) map.
4. Calls `GetByTasks([nonExistentTaskID])` and asserts an empty map (not an error, no key present).

### Task 3 — `markdownInputs` struct + input-gathering helper

Create `internal/tui/tree_markdown.go` (new file). Add the struct and a single helper, both unexported:

```go
package tui

import (
    "context"

    "github.com/germanamz/tusk/domain"
    "github.com/google/uuid"
)

// markdownInputs bundles everything the markdown renderer needs beyond the
// raw treeNode tree. Constructed once per `tusk task tree --format markdown`
// invocation by gatherMarkdownInputs.
type markdownInputs struct {
    project     *domain.Project
    tagsByTask  map[uuid.UUID][]*domain.Tag
    annsByTask  map[uuid.UUID][]*domain.Annotation
    notesByTask map[uuid.UUID][]*domain.Note  // keyed by TaskID; project-level notes go under uuid.Nil
    workflowFor func(*domain.Task) *domain.Workflow
}

// gatherMarkdownInputs assembles the markdown render inputs after the
// tree has been built. Must be called only when the tree is single-project
// (validated by the caller) and tasks is non-nil.
func (a *App) gatherMarkdownInputs(ctx context.Context, tasks []*domain.Task) (*markdownInputs, error) {
    // implementation below
}
```

Implementation:

1. Determine the project: read `tasks[0].ProjectID` (the single-project guard runs before this — see Task 4). Resolve via `a.projectSvc.GetByID(ctx, projectID)`.
2. Build the `taskIDs []uuid.UUID` slice from the tree's tasks.
3. Fetch tags: `a.tagRepo.GetTaskTagsBatch(ctx, taskIDs)` (the existing field on `App`; if the App struct does not expose `tagRepo`, look at how `tagSvc` is wired and add the missing repo accessor as a thin pass-through method on the service rather than reaching past the service layer).
4. Fetch annotations via the new `a.annotationRepo.GetByTasks(ctx, taskIDs)` (or the equivalent service method — match the wiring style already used by other tree-fetch helpers).
5. Fetch notes:
   ```go
   opts := repository.NoteListOptions{ProjectID: project.ID, IncludeArchived: false}
   allNotes, err := a.noteRepo.List(ctx, opts)
   ```
   Then group by `TaskID`: notes with `TaskID == nil` go under `uuid.Nil` in the map; notes with a concrete `TaskID` go under that key.
6. Reuse `a.buildWorkflowLookup(ctx, tasks)` (existing in `internal/tui/tree.go:311`) for `workflowFor`.
7. Return the populated struct.

If `App` exposes any of these dependencies via a service rather than a repository today, route through the service. Look at how `runTree` already accesses `taskSvc`, `projectSvc`, `workflowSvc` — match that style for consistency. (For example, `noteSvc.ListForExport` may not exist; if so, prefer adding a small thin service method that wraps `noteRepo.List` over reaching directly into the repo from the TUI layer.)

### Task 4 — Single-project validation + dispatch wiring in `runTree`

In `runTree` (after `sortTasks`, before `buildTree`):

1. Skip the validation when `format != "markdown"`.
2. When `format == "markdown"`:
   - If `tasks` is empty, fall through to the normal flow (the renderer emits an empty-result branch in Task 5).
   - Walk `tasks` and collect the distinct `ProjectID` values. If more than one is present, return:
     ```
     fmt.Errorf("--format markdown requires a single project; pass project=<name> or run on a single-project workspace")
     ```
   - If exactly one is present, call `a.gatherMarkdownInputs(ctx, tasks)` and stash the result in a local variable.

Then, in the existing `r := a.newRenderer(...)` block, add a third branch on `r.format`:

In `internal/tui/tree.go`'s `renderTree` method, change the dispatch:

```go
func (r *Renderer) renderTree(nodes []*treeNode) error {
    if r.format == "json" {
        // existing JSON path — unchanged
    }
    if r.format == "markdown" {
        return r.renderTreeMarkdown(nodes)
    }
    // existing text path — unchanged
}
```

`renderTreeMarkdown` is the stub from Task 5. It needs the markdown inputs, but the existing `Renderer` struct has no field for them. To avoid mutating the struct's public surface, store the inputs on the `Renderer` via a new unexported setter:

```go
// in styles.go or tree_markdown.go (unexported)
type markdownState struct {
    inputs *markdownInputs
}

func (r *Renderer) setMarkdownInputs(in *markdownInputs) {
    r.markdown = in
}
```

Add a new unexported field `markdown *markdownInputs` on `Renderer`. `runTree` calls `r.setMarkdownInputs(inputs)` immediately after `r := a.newRenderer(...)` (only when format is markdown). The setter is a no-op when called with nil.

### Task 5 — Stub `renderTreeMarkdown`

In `internal/tui/tree_markdown.go`, add the renderer entry point:

```go
// renderTreeMarkdown writes the markdown export. Phase 3 emits only the
// project header + description blockquote and a placeholder comment for the
// body. Phase 4 fills in the title lines, structural shape, and description
// blocks for tasks; Phase 5 adds annotations and notes.
func (r *Renderer) renderTreeMarkdown(nodes []*treeNode) error {
    if r.markdown == nil {
        // Empty workspace OR caller forgot to wire inputs — emit nothing rather
        // than panicking. runTree only sets inputs when the tree is non-empty
        // and single-project.
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
    }
    // Phase 4 replaces this placeholder with full body rendering.
    _, err := fmt.Fprintln(r.w, "\n<!-- tusk: markdown body lands in phase 4 -->")
    return err
}

// projectDisplayName converts a kebab/snake-case project name into a
// title-cased display string ("tusk-roadmap" -> "Tusk Roadmap").
func projectDisplayName(name string) string { /* implementation below */ }

// writeBlockquote writes s as a markdown blockquote with each line prefixed
// by `> `. indent is the per-line indent (used by Phase 4 for nested bullets);
// pass "" for headings. A blank line separates the blockquote from following
// content.
func writeBlockquote(w io.Writer, s, indent string) error { /* implementation below */ }
```

Both helpers are minimal in this phase but must already implement the shape they will hold long-term:

- `projectDisplayName` — split on `-` and `_`, drop leading/trailing empty tokens (so `_default` → `Default`), title-case each token, join with single spaces. Use `strings.Title` only after splitting (it is deprecated for full strings; per-token use is fine and consistent with the codebase). Document the chosen approach in a comment.
- `writeBlockquote` — split `s` on `\n`. For each line, write `<indent>> <line>\n`. For empty lines (paragraph separators), write `<indent>>\n`. Append a trailing blank line before returning.

Add unit tests in `internal/tui/tree_markdown_test.go`:

- `TestProjectDisplayName` — table of `("tusk-roadmap", "Tusk Roadmap")`, `("_default", "Default")`, `("a", "A")`, `("multi-word_project", "Multi Word Project")`.
- `TestWriteBlockquote` — single line, multi-paragraph (uses blank-line separator), with indent `"  "`.

**Bridge code (Phase 4 removes):** the `<!-- tusk: markdown body lands in phase 4 -->` placeholder line is the only piece of bridge code in this phase. It is tagged with the comment exactly as written. Phase 4 replaces this with the full task body rendering.

### Task 6 — Plumb tests + e2e smoke

Unit-level coverage for the validation guards:

In `internal/tui/tree_test.go` (or create `tree_markdown_test.go` if cleaner):

- `TestRunTree_MarkdownRejectsRollup` — sets `--format markdown` and `--rollup`, expects an error containing `--rollup is not supported with --format markdown`.
- `TestRunTree_MarkdownRejectsMultiProject` — fixtures two projects each with one task, expects an error containing `requires a single project`.
- `TestRunTree_MarkdownEmptyWorkspace` — runs against an empty workspace, expects empty output (no error).
- `TestRunTree_MarkdownSingleProject_StubOutput` — fixtures one project with description and one task; asserts output contains exactly the H1, the description blockquote, and the placeholder comment. The task body is **not** rendered yet — assert it is absent.

E2E:

In `tests/e2e/`, add `tree_markdown_test.go` with one scenario:

- Create a project `roadmap` with a description (using the Phase 2 `description=` parser).
- Create one root task in that project.
- Run `tusk task tree --format markdown`.
- Assert output starts with `# Roadmap` and contains the description text and the placeholder `<!-- tusk: markdown body lands in phase 4 -->`.

Use the existing harness conventions; one scenario keeps the test count down and exercises the full CLI flag plumbing.

## User-visible behaviors (acceptance criteria)

- All Phase 2 acceptance criteria still hold.
- `tusk task tree --format markdown` is accepted on the CLI; other commands continue to reject markdown format.
- `tusk task tree --format markdown --rollup` errors with `--rollup is not supported with --format markdown`. Exit non-zero.
- `tusk task tree --format markdown` against a workspace whose tasks span more than one project errors with `--format markdown requires a single project; pass project=<name> or run on a single-project workspace`.
- `tusk task tree project=<name> --format markdown` (or any equivalently project-scoped invocation) emits exactly the project H1, the description blockquote (when non-empty), and the placeholder comment. **No task content** is in the output yet — that arrives in Phase 4.
- `tusk task tree --format markdown` against an empty workspace emits nothing and exits zero.
- `make test` and `make test-race` pass.

## Bridge code introduced

| Bridge | Where | Removed in |
|---|---|---|
| Placeholder `<!-- tusk: markdown body lands in phase 4 -->` line in stub renderer | `internal/tui/tree_markdown.go:renderTreeMarkdown` | Phase 4 |

## Changes Introduced

- **New files:**
  - `internal/tui/tree_markdown.go` (struct + stub renderer + helpers)
  - `internal/tui/tree_markdown_test.go` (unit tests)
  - `tests/e2e/tree_markdown_test.go` (e2e smoke)
- **Modified files:**
  - `internal/tui/tree.go` (markdown format validator, rollup reject, single-project guard, dispatch in `renderTree`)
  - `internal/tui/app.go` (flag help text)
  - `internal/tui/styles.go` (new unexported `markdown` field on `Renderer`, plus `setMarkdownInputs`)
  - `internal/tui/tree_test.go` (new validation tests)
  - `repository/annotation.go` (new `GetByTasks` method on the interface)
  - `repository/repository_test.go` (stub repo gets `GetByTasks`)
  - `sqlite/annotation.go` (new `GetByTasks` implementation)
  - `sqlite/annotation_test.go` (batch test)
- **No schema migrations.**
- **No new dependencies.**
- **Public API additions:** `repository.AnnotationRepository.GetByTasks`. No new public types in `internal/tui` (the new struct + helpers are unexported).
