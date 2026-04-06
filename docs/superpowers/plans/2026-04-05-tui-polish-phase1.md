# TUI Polish Phase 1: Renderer Struct, NO_COLOR, and Priority Colors

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Introduce the `Renderer` struct, wire up color resolution (`--no-color` / `NO_COLOR` / config), add lipgloss dependency, and color priority values in task output.

**Architecture:** Refactor all free render functions into methods on a new `Renderer` struct in `internal/tui/render.go`. The `Renderer` holds the writer, format, color flag, and precomputed lipgloss `Styles`. All callers in `commands.go`, `tree.go`, `tag.go`, `project.go`, and `workflow.go` are updated to call `Renderer` methods instead of free functions. The `App` struct creates the `Renderer` and resolves color from flag > env > config.

**Tech Stack:** Go, `charm.land/lipgloss/v2`, Cobra

**Design spec:** `docs/superpowers/specs/2026-04-05-tui-polish-design.md`

**Prerequisites:** None — this is the first phase. The codebase is in its current state with no color support.

---

### Task 1: Add lipgloss dependency

**Files:**
- Modify: `go.mod`

- [ ] **Step 1: Add lipgloss v2 dependency**

```bash
cd /Users/germanamz/projects/tusk && go get charm.land/lipgloss/v2@latest
```

- [ ] **Step 2: Tidy modules**

```bash
go mod tidy
```

- [ ] **Step 3: Verify build compiles**

```bash
go build ./...
```

Expected: Clean build, no errors.

- [ ] **Step 4: Commit**

```bash
git add go.mod go.sum
git commit -m "feat(tui): add charm.land/lipgloss/v2 dependency"
```

---

### Task 2: Introduce Renderer struct and Styles

**Files:**
- Create: `internal/tui/styles.go`
- Modify: `internal/tui/render.go`

This task creates the `Renderer` struct and `Styles` type, and converts all free render functions to `Renderer` methods. The `Renderer` is not yet wired into `App` — that happens in Task 3.

- [ ] **Step 1: Create `internal/tui/styles.go` with Renderer and Styles types**

```go
package tui

import (
	"io"

	"charm.land/lipgloss/v2"
)

// Styles holds precomputed lipgloss styles for colored terminal output.
type Styles struct {
	// Priority maps priority int (0-4) to a foreground color style.
	Priority [5]lipgloss.Style
	// Dim is applied to rows whose status is in the workflow's dim_statuses.
	Dim lipgloss.Style
	// Header is used for table column headers (bold).
	Header lipgloss.Style
}

// Renderer encapsulates output formatting and styling for CLI commands.
type Renderer struct {
	w      io.Writer
	format string // "text" or "json"
	color  bool
	styles *Styles // nil when color=false
}

// NewRenderer creates a Renderer. When color is true, styles are initialized.
func NewRenderer(w io.Writer, format string, color bool) *Renderer {
	r := &Renderer{
		w:      w,
		format: format,
		color:  color,
	}
	if color {
		r.styles = newStyles()
	}
	return r
}

// newStyles initializes the default color styles.
func newStyles() *Styles {
	return &Styles{
		Priority: [5]lipgloss.Style{
			lipgloss.NewStyle().Foreground(lipgloss.Color("#666666")), // 0: none
			lipgloss.NewStyle().Foreground(lipgloss.Color("#4488ff")), // 1: low
			lipgloss.NewStyle().Foreground(lipgloss.Color("#ffcc00")), // 2: medium
			lipgloss.NewStyle().Foreground(lipgloss.Color("#ff8800")), // 3: high
			lipgloss.NewStyle().Foreground(lipgloss.Color("#ff4444")), // 4: urgent
		},
		Dim:    lipgloss.NewStyle().Faint(true),
		Header: lipgloss.NewStyle().Bold(true),
	}
}
```

- [ ] **Step 2: Convert all free render functions in `render.go` to Renderer methods**

In `internal/tui/render.go`, change every free function to a method on `*Renderer`. The function signatures change as follows — the `w io.Writer` and `format string` parameters are removed since they come from the Renderer fields:

**Before (example):**
```go
func renderTaskList(w io.Writer, tasks []*domain.Task, taskTags map[string][]*domain.Tag, format string) error {
```

**After:**
```go
func (r *Renderer) renderTaskList(tasks []*domain.Task, taskTags map[string][]*domain.Tag) error {
```

Apply this transformation to every render function in `render.go`. Replace all references to `w` with `r.w` and all references to `format` with `r.format` inside each function body. The full list of functions to convert:

| Old signature | New signature |
|---|---|
| `renderTagList(w, tags, showUsage, format)` | `(r *Renderer) renderTagList(tags, showUsage)` |
| `renderTagResult(w, action, tag, format)` | `(r *Renderer) renderTagResult(action, tag)` |
| `renderProjectList(w, projects, format)` | `(r *Renderer) renderProjectList(projects)` |
| `renderTaskList(w, tasks, taskTags, format)` | `(r *Renderer) renderTaskList(tasks, taskTags)` |
| `renderTaskInfo(w, task, annotations, tags, relations, format)` | `(r *Renderer) renderTaskInfo(task, annotations, tags, relations)` |
| `renderUDASection(w, uda)` | `(r *Renderer) renderUDASection(uda)` |
| `renderMutationResult(w, action, task, tags, format)` | `(r *Renderer) renderMutationResult(action, task, tags)` |
| `renderLinkResult(w, rel, sourceShortID, targetShortID, format)` | `(r *Renderer) renderLinkResult(rel, sourceShortID, targetShortID)` |
| `renderWorkflowList(w, workflows, workflowProjects, format)` | `(r *Renderer) renderWorkflowList(workflows, workflowProjects)` |
| `renderWorkflowInfo(w, wf, projectIDs, format)` | `(r *Renderer) renderWorkflowInfo(wf, projectIDs)` |

Also convert `renderTree` and `renderTreeNode` in `tree.go`:

| Old signature | New signature |
|---|---|
| `renderTree(w, nodes, format)` | `(r *Renderer) renderTree(nodes)` |
| `renderTreeNode(w, node, depth)` | `(r *Renderer) renderTreeNode(node, depth)` |

Inside each function body:
- Replace `w` → `r.w`
- Replace `format` → `r.format`
- No other logic changes yet — styling is added in Task 4.

- [ ] **Step 3: Verify build compiles**

```bash
go build ./...
```

Expected: Build FAILS because callers in `commands.go`, `tree.go`, `tag.go`, `project.go`, `workflow.go` still call the old free functions. This is expected — Task 3 fixes them.

- [ ] **Step 4: Commit (WIP — will compile after Task 3)**

```bash
git add internal/tui/styles.go internal/tui/render.go internal/tui/tree.go
git commit -m "refactor(tui): introduce Renderer struct and convert render functions to methods

WIP: callers updated in next task"
```

---

### Task 3: Wire Renderer into App and update all callers

**Files:**
- Modify: `internal/tui/app.go`
- Modify: `internal/tui/commands.go`
- Modify: `internal/tui/tag.go`
- Modify: `internal/tui/project.go`
- Modify: `internal/tui/workflow.go`
- Modify: `internal/tui/tree.go`

- [ ] **Step 1: Add `--no-color` flag and `Renderer` creation to `App`**

In `internal/tui/app.go`:

1. Add `noColor bool` field to the `App` struct.
2. Add `"os"` to the imports.
3. Add a `colorEnabled()` method:

```go
// colorEnabled resolves whether color output is active.
// Precedence: --no-color flag > NO_COLOR env > tui.color config.
func (a *App) colorEnabled() bool {
	if a.noColor {
		return false
	}
	if _, ok := os.LookupEnv("NO_COLOR"); ok {
		return false
	}
	return a.tuiCfg.Color
}
```

4. In the `New()` function, after the line that creates the root command and before adding commands, add the `--no-color` persistent flag:

```go
a.root.PersistentFlags().BoolVar(&a.noColor, "no-color", false, "disable colored output")
```

- [ ] **Step 2: Update all callers to create a Renderer and call methods**

In each command handler, replace the old free function call with a `Renderer` method call. The pattern is:

**Before:**
```go
return renderTaskList(cmd.OutOrStdout(), tasks, taskTags, a.format)
```

**After:**
```go
r := NewRenderer(cmd.OutOrStdout(), a.format, a.colorEnabled())
return r.renderTaskList(tasks, taskTags)
```

Apply this pattern to every call site. The full list of call sites to update:

**`commands.go`:**
- `runAdd` (~line 213): `renderMutationResult(cmd.OutOrStdout(), "Created", ...)` → `NewRenderer(cmd.OutOrStdout(), a.format, a.colorEnabled()).renderMutationResult("Created", ...)`
- `runList` (~line 261): `renderTaskList(cmd.OutOrStdout(), ...)` → same pattern
- `runInfo` (~line 319): `renderTaskInfo(cmd.OutOrStdout(), ...)` → same pattern
- `runModify` (~line 456): `renderMutationResult(cmd.OutOrStdout(), "Modified", ...)` → same pattern
- `runStart` (~line 473): `renderMutationResult(cmd.OutOrStdout(), "Started", ...)` → same pattern
- `runDone` (~line 490): `renderMutationResult(cmd.OutOrStdout(), "Completed", ...)` → same pattern
- `runDelete` (~line 507): `renderMutationResult(cmd.OutOrStdout(), "Deleted", ...)` → same pattern
- `runAnnotate` (~line 525): `renderMutationResult(cmd.OutOrStdout(), "Annotated", ...)` → same pattern
- `runLink` (~line 556): `renderLinkResult(cmd.OutOrStdout(), ...)` → same pattern
- `runUnlink` (~line 571): outputs directly via Fprintf — leave as-is (no render function involved)

**`tag.go`:**
- `runTagList` (~line 82): `renderTagList(cmd.OutOrStdout(), ...)` → same pattern
- `runTagCreate` (~line 123): `renderTagResult(cmd.OutOrStdout(), ...)` → same pattern
- `runTagModify` (~line 145): `renderTagResult(cmd.OutOrStdout(), ...)` → same pattern
- `runTagDelete` (~line 156): `renderTagResult(cmd.OutOrStdout(), ...)` → same pattern
- `runTagRename` (~line 169): `renderTagResult(cmd.OutOrStdout(), ...)` → same pattern

**`project.go`:**
- `runProjectList` (~line 31): `renderProjectList(cmd.OutOrStdout(), ...)` → same pattern

**`workflow.go`:**
- `runWorkflowList` (~line 50): `renderWorkflowList(cmd.OutOrStdout(), ...)` → same pattern
- `runWorkflowInfo` (~line 61): `renderWorkflowInfo(cmd.OutOrStdout(), ...)` → same pattern

**`tree.go`:**
- `runTree` (~line 190): `renderTree(cmd.OutOrStdout(), ...)` → same pattern
- The `runTree` function also has a direct `fmt.Fprintln(cmd.ErrOrStderr(), "No tasks.")` for empty results — leave that as-is.

- [ ] **Step 3: Verify build compiles**

```bash
go build ./...
```

Expected: Clean build.

- [ ] **Step 4: Run all tests**

```bash
go test ./...
```

Expected: All tests pass. No behavioral changes yet — the Renderer just wraps the same logic.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/
git commit -m "refactor(tui): wire Renderer into App with --no-color flag

All render functions now called as Renderer methods. Color resolution
added: --no-color flag > NO_COLOR env > tui.color config. No visual
changes yet — styling applied in next task."
```

---

### Task 4: Add priority colors to task list output

**Files:**
- Modify: `internal/tui/styles.go` (add helper method)
- Modify: `internal/tui/render.go` (apply priority styling)

- [ ] **Step 1: Add a `styledPriority` helper to `styles.go`**

```go
// styledPriority returns the priority symbol with color applied if styles are active.
func (r *Renderer) styledPriority(priority int) string {
	sym := formatPriority(priority)
	if r.styles == nil {
		return sym
	}
	idx := priority
	if idx < 0 || idx > 4 {
		idx = 0
	}
	return r.styles.Priority[idx].Render(sym)
}

// styledHeader returns text with bold styling if styles are active.
func (r *Renderer) styledHeader(text string) string {
	if r.styles == nil {
		return text
	}
	return r.styles.Header.Render(text)
}
```

- [ ] **Step 2: Apply priority color in `renderTaskList`**

In `internal/tui/render.go`, in the `renderTaskList` method, change the text-format header and row rendering.

**Header — before:**
```go
if _, err := fmt.Fprintf(r.w, "%-8s %-9s %-4s %-5s %s\n", "ID", "Status", "Pri", "Age", "Title"); err != nil {
```

**Header — after:**
```go
if _, err := fmt.Fprintf(r.w, "%-8s %-9s %-4s %-5s %s\n",
	r.styledHeader("ID"),
	r.styledHeader("Status"),
	r.styledHeader("Pri"),
	r.styledHeader("Age"),
	r.styledHeader("Title"),
); err != nil {
```

**Row priority — before:**
```go
if _, err := fmt.Fprintf(r.w, "%-8s %-9s %-4s %-5s %s\n",
	t.ShortID,
	t.Status,
	formatPriority(t.Priority),
	formatAge(t.CreatedAt),
	title,
); err != nil {
```

**Row priority — after:**
```go
if _, err := fmt.Fprintf(r.w, "%-8s %-9s %-4s %-5s %s\n",
	t.ShortID,
	t.Status,
	r.styledPriority(t.Priority),
	formatAge(t.CreatedAt),
	title,
); err != nil {
```

**Note on column alignment:** When lipgloss adds ANSI escape codes, the `%-4s` format specifier counts bytes including invisible escape characters. This means columns will misalign. To fix this, use lipgloss's `lipgloss.Width()` function to compute visible width, and manually pad:

```go
priStr := r.styledPriority(t.Priority)
priPad := strings.Repeat(" ", max(0, 4-lipgloss.Width(priStr)))
```

Then use string concatenation instead of `fmt.Fprintf` format width for the priority column:

```go
if _, err := fmt.Fprintf(r.w, "%-8s %-9s %s%s %-5s %s\n",
	t.ShortID,
	t.Status,
	priStr,
	priPad,
	formatAge(t.CreatedAt),
	title,
); err != nil {
```

Apply the same padding approach for header columns that use `styledHeader`.

- [ ] **Step 3: Apply priority color in `renderTaskInfo`**

In `renderTaskInfo`, find the priority line:

**Before:**
```go
if _, err := fmt.Fprintf(r.w, "%-13s %s\n", "Priority:", formatPriorityName(task.Priority)); err != nil {
```

**After:**
```go
priName := formatPriorityName(task.Priority)
if r.styles != nil {
	idx := task.Priority
	if idx < 0 || idx > 4 {
		idx = 0
	}
	priName = r.styles.Priority[idx].Render(priName)
}
if _, err := fmt.Fprintf(r.w, "%-13s %s\n", "Priority:", priName); err != nil {
```

Also apply `styledHeader`-style bold to the labels ("ID:", "Title:", "Status:", etc.) — but only if you want to. Per the design spec, labels in info view are bold. Apply the same pattern: wrap each label string with a helper. However, since info labels use `%-13s` padding, the same ANSI-width issue applies. Use `lipgloss.Width()` for correct padding:

```go
func (r *Renderer) styledLabel(text string) string {
	if r.styles == nil {
		return text
	}
	return r.styles.Header.Render(text)
}

func (r *Renderer) paddedLabel(text string, width int) string {
	styled := r.styledLabel(text)
	pad := max(0, width-lipgloss.Width(styled))
	return styled + strings.Repeat(" ", pad)
}
```

Then replace all `fmt.Fprintf(r.w, "%-13s %s\n", "Label:", value)` calls with:
```go
fmt.Fprintf(r.w, "%s %s\n", r.paddedLabel("Label:", 13), value)
```

- [ ] **Step 4: Verify build compiles and tests pass**

```bash
go build ./... && go test ./...
```

Expected: All pass. E2E tests run with `--format text` and `--format json`. The text format tests may need adjustment if they do exact string matching on output — check for failures. If e2e tests compare exact text output, the ANSI codes in colored mode could cause mismatches. Since e2e tests set `NO_COLOR` environment variable... actually they don't. Check if e2e tests set `NO_COLOR`:

```bash
grep -r "NO_COLOR" tests/e2e/
```

If they don't, you need to set `NO_COLOR=1` in the e2e harness to prevent color codes from breaking text assertions. Add this to the `Run()` method in `tests/e2e/harness.go`:

```go
env = append(env, "NO_COLOR=1")
```

This ensures all e2e tests run without color, which preserves existing text assertions.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/ tests/e2e/
git commit -m "feat(tui): add priority colors to task list and info output

Priority values colored: red (urgent), orange (high), yellow (medium),
blue (low), gray (none). Table headers rendered bold. ANSI-aware column
padding via lipgloss.Width(). E2E harness sets NO_COLOR to preserve
text assertions."
```

---

### Task 5: Add unit tests for Renderer and color resolution

**Files:**
- Create: `internal/tui/styles_test.go`

- [ ] **Step 1: Write tests for `styledPriority` with color on and off**

```go
package tui

import (
	"bytes"
	"strings"
	"testing"
)

func TestStyledPriority_NoColor(t *testing.T) {
	r := NewRenderer(&bytes.Buffer{}, "text", false)
	tests := []struct {
		priority int
		want     string
	}{
		{0, "-"},
		{1, "L"},
		{2, "M"},
		{3, "H"},
		{4, "U"},
	}
	for _, tt := range tests {
		got := r.styledPriority(tt.priority)
		if got != tt.want {
			t.Errorf("styledPriority(%d) = %q, want %q", tt.priority, got, tt.want)
		}
	}
}

func TestStyledPriority_WithColor(t *testing.T) {
	r := NewRenderer(&bytes.Buffer{}, "text", true)
	tests := []struct {
		priority int
		wantText string // the visible text inside the ANSI codes
	}{
		{0, "-"},
		{1, "L"},
		{2, "M"},
		{3, "H"},
		{4, "U"},
	}
	for _, tt := range tests {
		got := r.styledPriority(tt.priority)
		// With color enabled, output should contain ANSI escape sequences
		if !strings.Contains(got, tt.wantText) {
			t.Errorf("styledPriority(%d) = %q, should contain %q", tt.priority, got, tt.wantText)
		}
		// Should contain escape character
		if !strings.Contains(got, "\x1b[") {
			t.Errorf("styledPriority(%d) = %q, should contain ANSI escape codes", tt.priority, got)
		}
	}
}

func TestStyledHeader_NoColor(t *testing.T) {
	r := NewRenderer(&bytes.Buffer{}, "text", false)
	got := r.styledHeader("Title")
	if got != "Title" {
		t.Errorf("styledHeader(\"Title\") = %q, want \"Title\"", got)
	}
}

func TestStyledHeader_WithColor(t *testing.T) {
	r := NewRenderer(&bytes.Buffer{}, "text", true)
	got := r.styledHeader("Title")
	if !strings.Contains(got, "Title") {
		t.Errorf("styledHeader should contain \"Title\", got %q", got)
	}
	if !strings.Contains(got, "\x1b[") {
		t.Errorf("styledHeader should contain ANSI codes, got %q", got)
	}
}
```

- [ ] **Step 2: Run the tests**

```bash
go test -v ./internal/tui/ -run TestStyled
```

Expected: All PASS.

- [ ] **Step 3: Write test for `colorEnabled` on App**

In the same file or a new `internal/tui/app_test.go` if one doesn't exist:

```go
func TestColorEnabled(t *testing.T) {
	tests := []struct {
		name    string
		noColor bool
		envSet  bool
		cfgColor bool
		want    bool
	}{
		{"defaults to config true", false, false, true, true},
		{"defaults to config false", false, false, false, false},
		{"no-color flag overrides config", true, false, true, false},
		{"NO_COLOR env overrides config", false, true, true, false},
		{"flag takes precedence over env", true, true, true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := &App{
				noColor: tt.noColor,
				tuiCfg:  config.TUIConfig{Color: tt.cfgColor},
			}
			if tt.envSet {
				t.Setenv("NO_COLOR", "1")
			}
			if got := a.colorEnabled(); got != tt.want {
				t.Errorf("colorEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}
```

Add `"github.com/germanamz/tusk/internal/config"` to the test imports.

- [ ] **Step 4: Run all tests**

```bash
go test -v ./internal/tui/ -run TestColor
```

Expected: All PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/styles_test.go
git commit -m "test(tui): add unit tests for Renderer styles and color resolution"
```

---

### Task 6: Verify end-to-end and run full test suite

**Files:**
- No new files

- [ ] **Step 1: Run full test suite**

```bash
make test
```

Expected: All tests pass.

- [ ] **Step 2: Run tests with race detector**

```bash
make test-race
```

Expected: No race conditions.

- [ ] **Step 3: Run linter**

```bash
make lint
```

Expected: No lint errors.

- [ ] **Step 4: Manual smoke test (optional but recommended)**

```bash
# Build and test colored output
make build
./bin/tusk add "Test priority colors" --priority 4
./bin/tusk add "Medium priority" --priority 2
./bin/tusk add "Low priority" --priority 1
./bin/tusk list

# Verify NO_COLOR works
NO_COLOR=1 ./bin/tusk list

# Verify --no-color works
./bin/tusk list --no-color

# Verify JSON is unaffected
./bin/tusk list --format json
```

---

## Changes Introduced

**New files:**
- `internal/tui/styles.go` — `Renderer` struct, `Styles` struct, `NewRenderer()`, `newStyles()`, helper methods (`styledPriority`, `styledHeader`, `styledLabel`, `paddedLabel`)
- `internal/tui/styles_test.go` — Unit tests for Renderer and color resolution

**Modified files:**
- `go.mod` / `go.sum` — Added `charm.land/lipgloss/v2`
- `internal/tui/render.go` — All free render functions converted to `*Renderer` methods
- `internal/tui/tree.go` — `renderTree` and `renderTreeNode` converted to `*Renderer` methods
- `internal/tui/app.go` — Added `noColor` field, `colorEnabled()` method, `--no-color` persistent flag
- `internal/tui/commands.go` — All callers create `NewRenderer()` and call methods
- `internal/tui/tag.go` — Callers updated to use `Renderer`
- `internal/tui/project.go` — Callers updated to use `Renderer`
- `internal/tui/workflow.go` — Callers updated to use `Renderer`
- `tests/e2e/harness.go` — Added `NO_COLOR=1` to test environment

**New dependencies:**
- `charm.land/lipgloss/v2` (and its transitive dependencies)

**New CLI flags:**
- `--no-color` (persistent, on root command)

**User-visible behavior preserved:**
- All commands produce identical text output when `NO_COLOR=1` is set or `--no-color` is passed
- JSON output is completely unchanged
- All existing e2e tests pass (NO_COLOR set in harness)

**New user-visible behavior:**
- Priority column in `tusk list` is colored (red/orange/yellow/blue/gray) when color is enabled
- Priority value in `tusk info` is colored when color is enabled
- Table headers in `tusk list` are bold when color is enabled
- Labels in `tusk info` are bold when color is enabled

**Bridge code:** None.
