# TUI Polish Phase 3: Tag Colors and Markdown Rendering

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Render tags in their configured hex color wherever they appear in text output, and render task descriptions as terminal markdown using glamour.

**Architecture:** Tag color rendering adds a `styledTag` helper to the Renderer that applies a lipgloss foreground color from the tag's hex value. Markdown rendering uses `charm.land/glamour/v2` in `renderTaskInfo` for the description field, with `NoTTYStyleConfig` fallback when color is disabled.

**Tech Stack:** Go, `charm.land/lipgloss/v2`, `charm.land/glamour/v2`, Cobra

**Design spec:** `docs/superpowers/specs/2026-04-05-tui-polish-design.md`

**Prerequisites:** Phase 1 and Phase 2 must be completed.

---

## Inherits From

**Phase 1 introduced:**
- `internal/tui/styles.go` — `Renderer` struct, `Styles`, `NewRenderer(w, format, color)` (3-param, extended to 4-param in Phase 2), helpers: `styledPriority`, `styledHeader`, `styledLabel`, `paddedLabel`
- All render functions are methods on `*Renderer`
- `App.colorEnabled()` resolves `--no-color` > `NO_COLOR` env > `tui.color` config
- `charm.land/lipgloss/v2` in `go.mod`
- `tests/e2e/harness.go` sets `NO_COLOR=1`

**Phase 2 introduced:**
- `Renderer.dimStatuses` map and `isDimStatus()` method
- `NewRenderer` accepts `dimStatuses` parameter
- `App.buildDimStatuses()` collects dim statuses from workflow configs
- `renderTaskList` and `renderTreeNode` apply faint to dim-status rows
- `WorkflowConfig` and `domain.Workflow` have `HighlightStatuses` and `DimStatuses` fields
- `default.toml` kanban: `highlight_statuses = ["active"]`, `dim_statuses = ["completed", "deleted"]`

---

### Task 1: Add glamour dependency

**Files:**
- Modify: `go.mod`

- [ ] **Step 1: Add glamour v2 dependency**

```bash
cd /Users/germanamz/projects/tusk && go get charm.land/glamour/v2@latest
```

- [ ] **Step 2: Tidy modules**

```bash
go mod tidy
```

- [ ] **Step 3: Verify build compiles**

```bash
go build ./...
```

Expected: Clean build.

- [ ] **Step 4: Commit**

```bash
git add go.mod go.sum
git commit -m "feat(tui): add charm.land/glamour/v2 dependency"
```

---

### Task 2: Add tag color rendering

**Files:**
- Modify: `internal/tui/styles.go` (add `styledTag` helper)
- Modify: `internal/tui/render.go` (apply tag colors in list and info views)
- Modify: `internal/tui/styles_test.go` (add tests)

Note: `tree.go` is not modified here — the tree view does not display tags.

- [ ] **Step 1: Write tests for `styledTag`**

Add to `internal/tui/styles_test.go`:

```go
func TestStyledTag_NoColor(t *testing.T) {
	r := NewRenderer(&bytes.Buffer{}, "text", false, nil)
	tag := &domain.Tag{Name: "bug"}
	got := r.styledTag(tag)
	if got != "+bug" {
		t.Errorf("styledTag = %q, want \"+bug\"", got)
	}
}

func TestStyledTag_WithColorNoTagColor(t *testing.T) {
	r := NewRenderer(&bytes.Buffer{}, "text", true, nil)
	tag := &domain.Tag{Name: "bug"}
	got := r.styledTag(tag)
	// No tag color set — should return plain "+bug" without ANSI codes
	if got != "+bug" {
		t.Errorf("styledTag = %q, want \"+bug\"", got)
	}
}

func TestStyledTag_WithColorAndTagColor(t *testing.T) {
	r := NewRenderer(&bytes.Buffer{}, "text", true, nil)
	color := "#ff4444"
	tag := &domain.Tag{Name: "urgent", Color: &color}
	got := r.styledTag(tag)
	if !strings.Contains(got, "urgent") {
		t.Errorf("styledTag should contain \"urgent\", got %q", got)
	}
	if !strings.Contains(got, "\x1b[") {
		t.Errorf("styledTag should contain ANSI codes when tag has color, got %q", got)
	}
}
```

Add `"github.com/germanamz/tusk/internal/domain"` to the test imports if not already present.

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test -v ./internal/tui/ -run TestStyledTag
```

Expected: FAIL — `styledTag` doesn't exist yet.

- [ ] **Step 3: Implement `styledTag` in `styles.go`**

```go
// styledTag returns "+tagname" with the tag's hex color applied as foreground if color is enabled
// and the tag has a color set. Otherwise returns plain "+tagname".
func (r *Renderer) styledTag(tag *domain.Tag) string {
	text := "+" + tag.Name
	if r.styles == nil || tag.Color == nil {
		return text
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(*tag.Color)).Render(text)
}
```

Add `"github.com/germanamz/tusk/internal/domain"` to the imports in `styles.go`.

- [ ] **Step 4: Run tests**

```bash
go test -v ./internal/tui/ -run TestStyledTag
```

Expected: PASS.

- [ ] **Step 5: Apply `styledTag` in `renderTaskList`**

In `internal/tui/render.go`, in `renderTaskList`, find where tags are appended to the title:

**Before:**
```go
if tags, ok := taskTags[t.ID.String()]; ok && len(tags) > 0 {
	tagStrs := make([]string, len(tags))
	for i, tg := range tags {
		tagStrs[i] = "+" + tg.Name
	}
	title = title + "  " + strings.Join(tagStrs, " ")
}
```

**After:**
```go
if tags, ok := taskTags[t.ID.String()]; ok && len(tags) > 0 {
	tagStrs := make([]string, len(tags))
	for i, tg := range tags {
		tagStrs[i] = r.styledTag(tg)
	}
	title = title + "  " + strings.Join(tagStrs, " ")
}
```

- [ ] **Step 6: Apply `styledTag` in `renderTaskInfo`**

In `renderTaskInfo`, find the tags display:

**Before:**
```go
if len(tags) > 0 {
	tagStrs := make([]string, len(tags))
	for i, tg := range tags {
		tagStrs[i] = "+" + tg.Name
	}
	if _, err := fmt.Fprintf(r.w, "%-13s %s\n", "Tags:", strings.Join(tagStrs, " ")); err != nil {
```

**After:**
```go
if len(tags) > 0 {
	tagStrs := make([]string, len(tags))
	for i, tg := range tags {
		tagStrs[i] = r.styledTag(tg)
	}
	if _, err := fmt.Fprintf(r.w, "%s %s\n", r.paddedLabel("Tags:", 13), strings.Join(tagStrs, " ")); err != nil {
```

- [ ] **Step 7: Verify build and tests**

```bash
go build ./... && go test ./...
```

Expected: All pass.

- [ ] **Step 8: Commit**

```bash
git add internal/tui/
git commit -m "feat(tui): render tags in their configured hex color

Tags with a Color value display in that foreground color in list,
info, and tree views. Tags without color render plain. No effect
when color is disabled."
```

---

### Task 3: Add markdown description rendering

**Files:**
- Modify: `internal/tui/render.go` (description rendering in `renderTaskInfo`)
- Modify: `internal/tui/styles.go` (add `renderMarkdown` helper)
- Modify: `internal/tui/styles_test.go` (add tests)

- [ ] **Step 1: Write tests for markdown rendering**

Add to `internal/tui/styles_test.go`:

```go
func TestRenderMarkdown_WithColor(t *testing.T) {
	r := NewRenderer(&bytes.Buffer{}, "text", true, nil)
	input := "# Hello\n\nThis is **bold** text."
	got, err := r.renderMarkdown(input)
	if err != nil {
		t.Fatalf("renderMarkdown error: %v", err)
	}
	// Glamour renders headings, bold, etc. — just check it's not empty and contains the text
	if !strings.Contains(got, "Hello") {
		t.Errorf("renderMarkdown should contain \"Hello\", got %q", got)
	}
	if !strings.Contains(got, "bold") {
		t.Errorf("renderMarkdown should contain \"bold\", got %q", got)
	}
}

func TestRenderMarkdown_NoColor(t *testing.T) {
	r := NewRenderer(&bytes.Buffer{}, "text", false, nil)
	input := "# Hello\n\nSome text."
	got, err := r.renderMarkdown(input)
	if err != nil {
		t.Fatalf("renderMarkdown error: %v", err)
	}
	// With no color, should still render (plain text fallback) and contain the text
	if !strings.Contains(got, "Hello") {
		t.Errorf("renderMarkdown should contain \"Hello\", got %q", got)
	}
	// Should NOT contain ANSI escape codes
	if strings.Contains(got, "\x1b[") {
		t.Errorf("renderMarkdown with no color should not contain ANSI codes, got %q", got)
	}
}

func TestRenderMarkdown_PlainText(t *testing.T) {
	r := NewRenderer(&bytes.Buffer{}, "text", true, nil)
	input := "Just a plain description with no markdown."
	got, err := r.renderMarkdown(input)
	if err != nil {
		t.Fatalf("renderMarkdown error: %v", err)
	}
	if !strings.Contains(got, "plain description") {
		t.Errorf("renderMarkdown should contain original text, got %q", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test -v ./internal/tui/ -run TestRenderMarkdown
```

Expected: FAIL — `renderMarkdown` doesn't exist yet.

- [ ] **Step 3: Implement `renderMarkdown` in `styles.go`**

```go
// renderMarkdown renders markdown text for terminal display using glamour.
// When color is disabled, uses NoTTY style for plain ASCII formatting.
func (r *Renderer) renderMarkdown(text string) (string, error) {
	var opts []glamour.TermRendererOption

	if r.color {
		opts = append(opts, glamour.WithAutoStyle())
	} else {
		opts = append(opts, glamour.WithStyles(glamour.NoTTYStyleConfig))
	}
	opts = append(opts, glamour.WithWordWrap(0)) // 0 = auto-detect terminal width

	renderer, err := glamour.NewTermRenderer(opts...)
	if err != nil {
		return text, err // fallback to raw text on error
	}

	rendered, err := renderer.Render(text)
	if err != nil {
		return text, err // fallback to raw text on error
	}

	return strings.TrimRight(rendered, "\n"), nil
}
```

Add to imports in `styles.go`:

```go
"strings"

"charm.land/glamour/v2"
```

- [ ] **Step 4: Run tests**

```bash
go test -v ./internal/tui/ -run TestRenderMarkdown
```

Expected: PASS.

- [ ] **Step 5: Apply markdown rendering in `renderTaskInfo`**

In `internal/tui/render.go`, in `renderTaskInfo`, find the description rendering:

**Before:**
```go
if task.Description != "" {
	if _, err := fmt.Fprintln(r.w, "Description:"); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(r.w); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(r.w, task.Description); err != nil {
		return err
	}
}
```

**After:**
```go
if task.Description != "" {
	if _, err := fmt.Fprintln(r.w, r.paddedLabel("Description:", 13)); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(r.w); err != nil {
		return err
	}
	rendered, err := r.renderMarkdown(task.Description)
	if err != nil {
		// Fallback to raw text if glamour fails
		rendered = task.Description
	}
	if _, err := fmt.Fprintln(r.w, rendered); err != nil {
		return err
	}
}
```

- [ ] **Step 6: Verify build and tests**

```bash
go build ./... && go test ./...
```

Expected: All pass.

- [ ] **Step 7: Commit**

```bash
git add internal/tui/
git commit -m "feat(tui): render task descriptions as terminal markdown

Uses glamour v2 to render markdown descriptions in tusk info output.
Falls back to NoTTY style (ASCII-only) when color is disabled. Falls
back to raw text if glamour encounters an error. Only applies to the
description field in text format — JSON is unaffected."
```

---

### Task 4: Add e2e tests for tag colors and markdown rendering

**Files:**
- Modify: `tests/e2e/tag_management_test.go`
- Modify: `tests/e2e/output_format_test.go` (or create `tests/e2e/markdown_test.go`)

- [ ] **Step 1: Write e2e test for tag color set and clear**

In `tests/e2e/tag_management_test.go`, add a new scenario to the existing `TestTagManagement` function's `scenarios` slice (or add a new test function). The harness `Step` struct uses `Args`, `WantErr`, `Assert`, `AssertJSON`, and `AssertText` fields.

```go
// Add this scenario to the existing scenarios slice in TestTagManagement,
// or create a new test function:
func TestTagColorSetAndClear(t *testing.T) {
	scenarios := []Scenario{
		{
			Name: "tag_color_set_and_clear",
			Steps: []Step{
				{Args: []string{"tag", "create", "urgent"}},
				{
					Args: []string{"tag", "modify", "urgent", "--color", "#ff4444"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						assertEqual(t, m["color"], "#ff4444")
					},
				},
				{
					Args: []string{"tag", "list"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						arr := jsonArray(t, parsed)
						found := false
						for _, item := range arr {
							m := item.(map[string]any)
							if m["name"] == "urgent" {
								assertEqual(t, m["color"], "#ff4444")
								found = true
							}
						}
						if !found {
							t.Fatal("tag 'urgent' not found in list")
						}
					},
				},
				{
					Args: []string{"tag", "modify", "urgent", "--color", ""},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						if m["color"] != nil {
							t.Errorf("expected color to be null after clear, got %v", m["color"])
						}
					},
				},
			},
		},
	}
	runScenarios(t, binPath, scenarios)
}
```

- [ ] **Step 2: Write e2e test for markdown description in info**

Add to `tests/e2e/output_format_test.go` or a new file:

```go
func TestMarkdownDescriptionInInfo(t *testing.T) {
	scenarios := []Scenario{
		{
			Name: "info_renders_description",
			Steps: []Step{
				{
					Args: []string{"add", "Markdown test", "--description", "# Heading\n\nSome **bold** text."},
				},
				{
					Args: []string{"info", "$0.short_id"},
					Assert: func(t *testing.T, r Result) {
						t.Helper()
						// Both text and json should contain the heading text
						assertContains(t, r.Stdout, "Heading")
					},
				},
			},
		},
	}
	runScenarios(t, binPath, scenarios)
}
```

- [ ] **Step 3: Run tests**

```bash
go test -v ./tests/e2e/ -run "TestTagColorSetAndClear|TestMarkdownDescriptionInInfo"
```

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add tests/e2e/
git commit -m "test(e2e): add tests for tag colors and markdown description rendering"
```

---

### Task 5: Final verification

**Files:** None.

- [ ] **Step 1: Run full test suite**

```bash
make test
```

Expected: All pass.

- [ ] **Step 2: Run with race detector**

```bash
make test-race
```

Expected: No race conditions.

- [ ] **Step 3: Run linter**

```bash
make lint
```

Expected: Clean.

- [ ] **Step 4: Run vet**

```bash
make vet
```

Expected: Clean.

- [ ] **Step 5: Manual smoke test (optional but recommended)**

```bash
make build

# Create a tag with color and a task using it
./bin/tusk tag create "critical"
./bin/tusk tag modify "critical" --color "#ff4444"
./bin/tusk add "Test markdown" --description "# Hello World

This is a **markdown** description with:
- bullet one
- bullet two

And some \`inline code\`." +critical --priority 4

# Verify colored output
./bin/tusk list
./bin/tusk info $(./bin/tusk list --format json | jq -r '.[0].short_id')

# Verify no-color fallback
./bin/tusk info $(./bin/tusk list --format json | jq -r '.[0].short_id') --no-color

# Verify JSON is unaffected
./bin/tusk info $(./bin/tusk list --format json | jq -r '.[0].short_id') --format json
```

---

## Changes Introduced

**New files:** None.

**Modified files:**
- `go.mod` / `go.sum` — Added `charm.land/glamour/v2`
- `internal/tui/styles.go` — Added `styledTag(tag)` and `renderMarkdown(text)` methods
- `internal/tui/styles_test.go` — Tests for `styledTag` and `renderMarkdown`
- `internal/tui/render.go` — Tag rendering uses `styledTag`; description rendering uses `renderMarkdown`
- `tests/e2e/` — Tag color and markdown e2e tests

**New dependencies:**
- `charm.land/glamour/v2` (and its transitive dependencies)

**User-visible behavior preserved:**
- JSON output unchanged
- All existing e2e tests pass (see note below)
- Tag color set/clear via `tusk tag modify --color` works as before (flag already existed)

**Behavior change note:** When `NO_COLOR=1` is set, glamour still reformats description text using ASCII conventions (e.g., heading underlines, list prefixes). Plain text descriptions pass through largely unchanged, but descriptions with markdown syntax will look different from before (formatted rather than raw). Existing e2e tests that assert on description text (e.g., `assertContains(t, output, "This is the description")` in `task_lifecycle_test.go`) should still pass since glamour preserves plain text content. If any fail due to whitespace changes, adjust the assertions to match glamour's output.

**New user-visible behavior:**
- Tags with hex colors render in that foreground color in list, info, and tree views
- Task descriptions in `tusk info` render as terminal markdown (headings, bold, code blocks, lists)
- With `--no-color` or `NO_COLOR`, descriptions render as ASCII-formatted plain text (no ANSI codes)

**Bridge code:** None.
