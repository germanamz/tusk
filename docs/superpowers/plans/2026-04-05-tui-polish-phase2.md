# TUI Polish Phase 2: Workflow Status Dimming

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extend workflow config with `highlight_statuses` and `dim_statuses` fields, and apply faint rendering to dim-status task rows across `tusk list`, `tusk info`, and `tusk tree`.

**Architecture:** Add two new optional fields to `WorkflowConfig` and `domain.Workflow`. The `Renderer` receives a status-to-display-treatment lookup map built from the workflow config. When rendering task rows, the Renderer checks the task's status and applies `Faint(true)` to the entire row for dim statuses. This requires passing workflow display info to the Renderer at construction time.

**Tech Stack:** Go, `charm.land/lipgloss/v2`, Cobra, Viper

**Design spec:** `docs/superpowers/specs/2026-04-05-tui-polish-design.md`

**Prerequisites:** Phase 1 must be completed. The codebase must have the `Renderer` struct, `Styles`, `NewRenderer()`, `--no-color` flag, and all render functions as `Renderer` methods.

---

## Inherits From

**Phase 1 introduced:**
- `internal/tui/styles.go` — `Renderer` struct with `w`, `format`, `color`, `styles` fields; `Styles` struct with `Priority`, `Dim`, `Header`; `NewRenderer(w, format, color)` constructor; helpers: `styledPriority`, `styledHeader`, `styledLabel`, `paddedLabel`
- All render functions are methods on `*Renderer` (in `render.go` and `tree.go`)
- `App.colorEnabled()` resolves `--no-color` > `NO_COLOR` env > `tui.color` config
- Every command handler creates a `Renderer` via `NewRenderer(cmd.OutOrStdout(), a.format, a.colorEnabled())`
- `tests/e2e/harness.go` sets `NO_COLOR=1`
- `charm.land/lipgloss/v2` is in `go.mod`

---

### Task 1: Add `highlight_statuses` and `dim_statuses` to config and domain

**Files:**
- Modify: `internal/config/config.go` (~line 24, `WorkflowConfig` struct)
- Modify: `internal/config/default.toml` (~line 50, `[workflows.kanban]`)
- Modify: `internal/domain/workflow.go`
- Modify: `internal/inmem/workflow.go` (~line 22, `NewWorkflowRepository`)

- [ ] **Step 1: Write a config validation test for the new fields**

Add to `internal/config/config_test.go`:

```go
func TestLoad_WorkflowStatusDisplay(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.toml")

	t.Run("valid highlight and dim statuses", func(t *testing.T) {
		err := os.WriteFile(cfgFile, []byte(`
[workflows.test]
statuses = ["todo", "doing", "done", "archived"]
transitions = [{ from = "todo", to = "doing" }, { from = "doing", to = "done" }]
highlight_statuses = ["doing"]
dim_statuses = ["done", "archived"]

[projects.default]
workflow = "test"
`), 0o644)
		if err != nil {
			t.Fatal(err)
		}
		cfg, err := config.Load(config.WithSearchPath(dir))
		if err != nil {
			t.Fatalf("Load() error: %v", err)
		}
		wf := cfg.Workflows["test"]
		if len(wf.HighlightStatuses) != 1 || wf.HighlightStatuses[0] != "doing" {
			t.Errorf("HighlightStatuses = %v, want [doing]", wf.HighlightStatuses)
		}
		if len(wf.DimStatuses) != 2 {
			t.Errorf("DimStatuses = %v, want [done archived]", wf.DimStatuses)
		}
	})

	t.Run("dim status not in statuses list", func(t *testing.T) {
		err := os.WriteFile(cfgFile, []byte(`
[workflows.test]
statuses = ["todo", "doing", "done"]
transitions = [{ from = "todo", to = "doing" }]
dim_statuses = ["archived"]

[projects.default]
workflow = "test"
`), 0o644)
		if err != nil {
			t.Fatal(err)
		}
		_, err = config.Load(config.WithSearchPath(dir))
		if err == nil {
			t.Fatal("expected validation error for unknown dim status")
		}
	})

	t.Run("status in both highlight and dim", func(t *testing.T) {
		err := os.WriteFile(cfgFile, []byte(`
[workflows.test]
statuses = ["todo", "doing", "done"]
transitions = [{ from = "todo", to = "doing" }]
highlight_statuses = ["doing"]
dim_statuses = ["doing"]

[projects.default]
workflow = "test"
`), 0o644)
		if err != nil {
			t.Fatal(err)
		}
		_, err = config.Load(config.WithSearchPath(dir))
		if err == nil {
			t.Fatal("expected validation error for status in both lists")
		}
	})
}
```

- [ ] **Step 2: Run the test to verify it fails**

```bash
go test -v ./internal/config/ -run TestLoad_WorkflowStatusDisplay
```

Expected: FAIL — `HighlightStatuses` and `DimStatuses` fields don't exist yet.

- [ ] **Step 3: Add fields to `WorkflowConfig` in `config.go`**

In `internal/config/config.go`, modify the `WorkflowConfig` struct:

```go
type WorkflowConfig struct {
	Statuses          []string                   `mapstructure:"statuses"`
	Transitions       []WorkflowTransitionConfig `mapstructure:"transitions"`
	HighlightStatuses []string                   `mapstructure:"highlight_statuses"`
	DimStatuses       []string                   `mapstructure:"dim_statuses"`
}
```

- [ ] **Step 4: Add validation for the new fields**

In `internal/config/config.go`, extend the `validate()` method. After the existing project validation loop, add workflow validation:

```go
for name, wf := range c.Workflows {
	statusSet := make(map[string]bool, len(wf.Statuses))
	for _, s := range wf.Statuses {
		statusSet[s] = true
	}
	for _, s := range wf.HighlightStatuses {
		if !statusSet[s] {
			return fmt.Errorf("workflow %q: highlight_statuses references unknown status %q", name, s)
		}
	}
	dimSet := make(map[string]bool, len(wf.DimStatuses))
	for _, s := range wf.DimStatuses {
		if !statusSet[s] {
			return fmt.Errorf("workflow %q: dim_statuses references unknown status %q", name, s)
		}
		dimSet[s] = true
	}
	for _, s := range wf.HighlightStatuses {
		if dimSet[s] {
			return fmt.Errorf("workflow %q: status %q cannot be in both highlight_statuses and dim_statuses", name, s)
		}
	}
}
```

- [ ] **Step 5: Add fields to `domain.Workflow`**

In `internal/domain/workflow.go`:

```go
type Workflow struct {
	Name              string
	Statuses          []string
	Transitions       []WorkflowTransition
	HighlightStatuses []string
	DimStatuses       []string
}
```

- [ ] **Step 6: Map config fields in `inmem/workflow.go`**

In `internal/inmem/workflow.go`, in `NewWorkflowRepository`, after the existing `copy(wf.Statuses, cfg.Statuses)` and transitions loop, add:

```go
wf.HighlightStatuses = make([]string, len(cfg.HighlightStatuses))
copy(wf.HighlightStatuses, cfg.HighlightStatuses)
wf.DimStatuses = make([]string, len(cfg.DimStatuses))
copy(wf.DimStatuses, cfg.DimStatuses)
```

Also update `copyWorkflow` to copy the new fields:

```go
cp.HighlightStatuses = make([]string, len(wf.HighlightStatuses))
copy(cp.HighlightStatuses, wf.HighlightStatuses)
cp.DimStatuses = make([]string, len(wf.DimStatuses))
copy(cp.DimStatuses, wf.DimStatuses)
```

- [ ] **Step 7: Update `default.toml` with kanban defaults**

In `internal/config/default.toml`, after the `transitions` array in `[workflows.kanban]`, add:

```toml
highlight_statuses = ["active"]
dim_statuses = ["completed", "deleted"]
```

- [ ] **Step 8: Run tests**

```bash
go test -v ./internal/config/ -run TestLoad_WorkflowStatusDisplay && go test ./...
```

Expected: All PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/config/ internal/domain/workflow.go internal/inmem/workflow.go
git commit -m "feat(config): add highlight_statuses and dim_statuses to workflow config

New optional fields on WorkflowConfig control status display treatment.
Validated: statuses must exist, no overlap between highlight and dim.
Builtin kanban: active=highlight, completed+deleted=dim."
```

---

### Task 2: Pass status display info to Renderer

**Files:**
- Modify: `internal/tui/styles.go`
- Modify: `internal/tui/app.go`
- Modify: `internal/tui/commands.go`
- Modify: `internal/tui/tree.go`
- Modify: `internal/tui/tag.go`
- Modify: `internal/tui/project.go`
- Modify: `internal/tui/workflow.go`

The Renderer needs to know which statuses are dim so it can apply `Faint(true)`. The simplest approach: pass a `map[string]bool` of dim statuses to `NewRenderer`. Since tasks belong to projects and projects have workflows, the Renderer needs the dim statuses for the relevant workflow. For simplicity, we'll build a merged set of all dim statuses across all workflows — since status names are typically unique across workflows in practice, and the visual treatment is just "faint or not."

- [ ] **Step 1: Update `NewRenderer` signature and `Renderer` struct**

In `internal/tui/styles.go`:

```go
type Renderer struct {
	w           io.Writer
	format      string
	color       bool
	styles      *Styles
	dimStatuses map[string]bool // statuses that should be rendered faint
}

// NewRenderer creates a Renderer. When color is true, styles are initialized.
// dimStatuses is a set of status names that should be rendered faint.
func NewRenderer(w io.Writer, format string, color bool, dimStatuses map[string]bool) *Renderer {
	r := &Renderer{
		w:           w,
		format:      format,
		color:       color,
		dimStatuses: dimStatuses,
	}
	if color {
		r.styles = newStyles()
	}
	return r
}
```

Add a helper method:

```go
// isDimStatus returns true if the given status should be rendered faint.
func (r *Renderer) isDimStatus(status string) bool {
	return r.styles != nil && r.dimStatuses[status]
}
```

- [ ] **Step 2: Add `buildDimStatuses` helper to `App`**

In `internal/tui/app.go`, add a method that collects all dim statuses from all workflow configs:

```go
// buildDimStatuses collects all dim statuses from all workflow configs into a lookup set.
func (a *App) buildDimStatuses() map[string]bool {
	workflows, err := a.workflowSvc.List(context.Background())
	if err != nil {
		return nil
	}
	dim := make(map[string]bool)
	for _, wf := range workflows {
		for _, s := range wf.DimStatuses {
			dim[s] = true
		}
	}
	return dim
}
```

Add `"context"` to the imports in `app.go`.

- [ ] **Step 3: Update all `NewRenderer` call sites to pass dim statuses**

In every command handler that creates a `NewRenderer`, update the call:

**Before:**
```go
r := NewRenderer(cmd.OutOrStdout(), a.format, a.colorEnabled())
```

**After:**
```go
r := NewRenderer(cmd.OutOrStdout(), a.format, a.colorEnabled(), a.buildDimStatuses())
```

This is the same set of files updated in Phase 1 Task 3:
- `commands.go`: all `runX` handlers
- `tag.go`: `runTagList`, `runTagCreate`, `runTagModify`, `runTagDelete`, `runTagRename`
- `project.go`: `runProjectList`
- `workflow.go`: `runWorkflowList`, `runWorkflowInfo`
- `tree.go`: `runTree`

- [ ] **Step 4: Update test file**

Update `internal/tui/styles_test.go` — all `NewRenderer` calls need the new `dimStatuses` parameter. Pass `nil` for existing tests since they don't test dim behavior:

```go
// Update all existing NewRenderer calls:
r := NewRenderer(&bytes.Buffer{}, "text", false, nil)
// and:
r := NewRenderer(&bytes.Buffer{}, "text", true, nil)
```

- [ ] **Step 5: Verify build and tests**

```bash
go build ./... && go test ./...
```

Expected: All pass. No behavioral change yet — dim styling applied in Task 3.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/
git commit -m "refactor(tui): pass dim status set to Renderer

NewRenderer now accepts a dimStatuses map. App.buildDimStatuses()
collects dim statuses from all workflow configs. No visual change yet."
```

---

### Task 3: Apply dim styling to task rows

**Files:**
- Modify: `internal/tui/render.go`
- Modify: `internal/tui/tree.go`

- [ ] **Step 1: Write test for dim row rendering**

Add to `internal/tui/styles_test.go`:

```go
func TestIsDimStatus(t *testing.T) {
	dim := map[string]bool{"completed": true, "deleted": true}

	t.Run("dim status with color", func(t *testing.T) {
		r := NewRenderer(&bytes.Buffer{}, "text", true, dim)
		if !r.isDimStatus("completed") {
			t.Error("expected completed to be dim")
		}
		if !r.isDimStatus("deleted") {
			t.Error("expected deleted to be dim")
		}
		if r.isDimStatus("active") {
			t.Error("expected active to not be dim")
		}
		if r.isDimStatus("pending") {
			t.Error("expected pending to not be dim")
		}
	})

	t.Run("no color disables dim", func(t *testing.T) {
		r := NewRenderer(&bytes.Buffer{}, "text", false, dim)
		if r.isDimStatus("completed") {
			t.Error("expected dim to be disabled when color is off")
		}
	})

	t.Run("nil dim map", func(t *testing.T) {
		r := NewRenderer(&bytes.Buffer{}, "text", true, nil)
		if r.isDimStatus("completed") {
			t.Error("expected false for nil map")
		}
	})
}
```

- [ ] **Step 2: Run test**

```bash
go test -v ./internal/tui/ -run TestIsDimStatus
```

Expected: PASS.

- [ ] **Step 3: Apply dim styling in `renderTaskList`**

In `internal/tui/render.go`, in the `renderTaskList` method, wrap each row's output with faint styling when the task's status is dim.

In the task row loop, after building the `title` string and before the `fmt.Fprintf` call, add dim wrapping:

```go
for _, t := range tasks {
	title := t.Title
	if tags, ok := taskTags[t.ID.String()]; ok && len(tags) > 0 {
		tagStrs := make([]string, len(tags))
		for i, tg := range tags {
			tagStrs[i] = "+" + tg.Name
		}
		title = title + "  " + strings.Join(tagStrs, " ")
	}

	priStr := r.styledPriority(t.Priority)
	priPad := strings.Repeat(" ", max(0, 4-lipgloss.Width(priStr)))

	line := fmt.Sprintf("%-8s %-9s %s%s %-5s %s",
		t.ShortID,
		t.Status,
		priStr,
		priPad,
		formatAge(t.CreatedAt),
		title,
	)

	if r.isDimStatus(t.Status) {
		line = r.styles.Dim.Render(line)
	}

	if _, err := fmt.Fprintln(r.w, line); err != nil {
		return err
	}
}
```

Note: when the row is dimmed via `Faint(true)`, the priority color inside is also muted by the terminal. This is the desired behavior from the design spec.

- [ ] **Step 4: Apply dim styling in `renderTreeNode`**

In `internal/tui/tree.go`, in `renderTreeNode`:

**Before:**
```go
indent := strings.Repeat("  ", depth)
if _, err := fmt.Fprintf(r.w, "%s%s [%s] %s\n", indent, node.Task.ShortID, node.Task.Status, node.Task.Title); err != nil {
```

**After:**
```go
indent := strings.Repeat("  ", depth)
line := fmt.Sprintf("%s%s [%s] %s", indent, node.Task.ShortID, node.Task.Status, node.Task.Title)
if r.isDimStatus(node.Task.Status) {
	line = r.styles.Dim.Render(line)
}
if _, err := fmt.Fprintln(r.w, line); err != nil {
```

- [ ] **Step 5: Verify build and tests**

```bash
go build ./... && go test ./...
```

Expected: All pass.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/
git commit -m "feat(tui): apply dim styling to tasks with dim workflow statuses

Tasks in dim_statuses (e.g. completed, deleted) render with faint text
in list and tree views. Priority colors are muted by terminal's faint
rendering. No effect when color is disabled."
```

---

### Task 4: Add e2e test for workflow status display config

**Files:**
- Modify: `tests/e2e/workflow_test.go`

- [ ] **Step 1: Write e2e test verifying custom workflow with highlight/dim statuses loads correctly**

In `tests/e2e/workflow_test.go`, add a test function. The harness `Scenario` struct does not support a `Config` field, so use the manual approach (same pattern as `tests/e2e/propagation_test.go`): create an `Env`, call `env.withConfig(...)`, then run steps manually.

```go
func TestWorkflowStatusDisplay(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}

	configContent := `
[workflows.custom]
statuses = ["backlog", "in_progress", "review", "done", "archived"]
transitions = [
  { from = "backlog", to = "in_progress" },
  { from = "in_progress", to = "review" },
  { from = "review", to = "done" },
]
highlight_statuses = ["in_progress", "review"]
dim_statuses = ["done", "archived"]

[projects.default]
workflow = "custom"
`

	for _, dbMode := range []string{"flag", "env"} {
		t.Run(dbMode, func(t *testing.T) {
			t.Parallel()
			env := newEnv(t, binPath, dbMode, "json")
			env.withConfig(configContent)

			// Create a task — verifies config loads without error
			r := env.Run("add", "Test task")
			if r.Err != nil {
				t.Fatalf("add failed: %v\nstderr: %s", r.Err, r.Stderr)
			}

			// List tasks — verifies task is returned
			r = env.Run("list")
			if r.Err != nil {
				t.Fatalf("list failed: %v\nstderr: %s", r.Err, r.Stderr)
			}
			assertContains(t, r.Stdout, "Test task")
		})
	}
}
```

Note: since e2e tests run with `NO_COLOR=1` (set in harness), we cannot assert on ANSI codes. This test verifies the config loads without error and tasks can be created/listed with the new workflow fields — a correctness check, not a visual check.

- [ ] **Step 2: Run the test**

```bash
go test -v ./tests/e2e/ -run TestWorkflowStatusDisplay
```

Expected: PASS.

- [ ] **Step 3: Run full test suite**

```bash
make test && make lint
```

Expected: All pass.

- [ ] **Step 4: Commit**

```bash
git add tests/e2e/
git commit -m "test(e2e): add workflow status display config test

Verifies custom workflows with highlight_statuses and dim_statuses
load correctly and tasks can be created and listed."
```

---

## Changes Introduced

**New files:** None.

**Modified files:**
- `internal/config/config.go` — `WorkflowConfig` gains `HighlightStatuses` and `DimStatuses` fields; `validate()` extended with status display validation
- `internal/config/config_test.go` — Tests for new validation rules
- `internal/config/default.toml` — Kanban workflow gets `highlight_statuses = ["active"]`, `dim_statuses = ["completed", "deleted"]`
- `internal/domain/workflow.go` — `Workflow` gains `HighlightStatuses` and `DimStatuses` fields
- `internal/inmem/workflow.go` — `NewWorkflowRepository` and `copyWorkflow` map/copy new fields
- `internal/tui/styles.go` — `Renderer` gains `dimStatuses` field; `NewRenderer` accepts `dimStatuses` param; `isDimStatus()` method added
- `internal/tui/app.go` — `buildDimStatuses()` method added
- `internal/tui/render.go` — `renderTaskList` applies faint to dim-status rows
- `internal/tui/tree.go` — `renderTreeNode` applies faint to dim-status nodes
- `internal/tui/commands.go`, `tag.go`, `project.go`, `workflow.go` — Updated `NewRenderer` calls with `dimStatuses` param
- `internal/tui/styles_test.go` — Updated `NewRenderer` calls, added `isDimStatus` tests
- `tests/e2e/workflow_test.go` — Added config validation e2e test

**Config changes:**
- New optional fields: `workflows.<name>.highlight_statuses`, `workflows.<name>.dim_statuses`

**User-visible behavior preserved:**
- All existing commands produce identical output when `NO_COLOR=1` is set
- JSON output unchanged
- All existing e2e tests pass

**New user-visible behavior:**
- Tasks with dim statuses (e.g., completed, deleted) render with faint/muted text in `tusk list` and `tusk tree` when color is enabled
- Priority colors on dim rows are also muted

**Bridge code:** None.
