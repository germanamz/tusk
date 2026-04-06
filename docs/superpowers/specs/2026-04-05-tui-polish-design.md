# TUI Polish — Design Spec

**Initiative:** v0.6 TUI Polish (from ROADMAP.md)
**Date:** 2026-04-05
**Scope:** Color-coded output, tag colors, markdown description rendering

---

## Overview

Add visual polish to tusk's terminal output using the Charm ecosystem (`charm.land/lipgloss/v2` for styling, `charm.land/glamour/v2` for markdown). Introduces a `Renderer` struct that encapsulates all output formatting and styling decisions, replacing the current free-function rendering approach. This positions the codebase for the bubbletea v2 dashboard in v0.8.

## Dependencies

| Package | Version | Purpose |
|---------|---------|---------|
| `charm.land/lipgloss/v2` | v2.0.0+ | Terminal styling (colors, bold, faint) |
| `charm.land/glamour/v2` | v2.0.0+ | Markdown rendering in terminal |

Both use the `charm.land/` module path (not `github.com/charmbracelet/`), ensuring compatibility with `charm.land/bubbletea/v2` when the v0.8 dashboard is implemented.

## 1. Renderer Struct

Refactor the existing free render functions in `internal/tui/render.go` into methods on a `Renderer` struct. This encapsulates output format, writer, and styling config in one place.

```go
type Renderer struct {
    w       io.Writer
    format  string    // "text" or "json"
    color   bool      // resolved from flag > env > config
    styles  *Styles   // nil when color=false
}
```

### Styles

Precomputed lipgloss styles, initialized once when `color=true`:

```go
type Styles struct {
    Priority  [5]lipgloss.Style // indexed by priority int (0-4)
    Dim       lipgloss.Style    // faint text for dim_statuses
    Header    lipgloss.Style    // bold column headers
}
```

All existing `renderX(w io.Writer, data, format string)` free functions become `(r *Renderer) RenderX(data)` methods. The `Renderer` is created once in `App` and passed to command handlers.

### JSON Bypass

When `format=json`, the `Renderer` uses `json.Encoder` directly — no styles are ever applied regardless of `color` setting.

## 2. Color Resolution

Color is determined once at `App` initialization with this precedence:

1. `--no-color` CLI flag (persistent, on root command) → disables
2. `NO_COLOR` environment variable (any value) → disables
3. `tui.color` config field → respects setting
4. Default: enabled

```go
func (a *App) colorEnabled() bool {
    if a.noColor {
        return false
    }
    if _, ok := os.LookupEnv("NO_COLOR"); ok {
        return false
    }
    return a.config.TUI.Color
}
```

A new `--no-color` persistent flag is added to the root Cobra command.

## 3. Priority Colors

Priority values (0-4) map to fixed foreground colors:

| Priority | Symbol | Color |
|----------|--------|-------|
| 4 (urgent) | U | Red (`#ff4444`) |
| 3 (high) | H | Orange (`#ff8800`) |
| 2 (medium) | M | Yellow (`#ffcc00`) |
| 1 (low) | L | Blue (`#4488ff`) |
| 0 (none) | - | Gray (`#666666`) |

**Scope:** Only the priority column/value is colored. The rest of the row stays at its default or status-derived color. These colors are hardcoded — not configurable via config.

## 4. Status Display Treatment

### Workflow Config Extension

Two new optional fields on `WorkflowConfig`:

```go
type WorkflowConfig struct {
    Statuses          []string            `mapstructure:"statuses"`
    Transitions       map[string][]string `mapstructure:"transitions"`
    HighlightStatuses []string            `mapstructure:"highlight_statuses"`
    DimStatuses       []string            `mapstructure:"dim_statuses"`
}
```

Statuses not listed in either field are rendered as **normal** (no special treatment).

### Builtin Kanban Defaults

```toml
[workflows.kanban]
statuses = ["pending", "active", "completed", "deleted"]
highlight_statuses = ["active"]
dim_statuses = ["completed", "deleted"]
```

### Validation Rules

- Statuses in `highlight_statuses` and `dim_statuses` must exist in `statuses`
- A status cannot appear in both lists
- Both fields are optional — omitting means all statuses render as normal

### Rendering Behavior

- **Highlight statuses:** Rendered at normal foreground color (functionally same as normal — the distinction is explicit categorization, not a different style)
- **Normal statuses:** Rendered at default foreground color (implicit — any status not in highlight or dim)
- **Dim statuses:** Entire row rendered with `Faint(true)` — including the priority column, which retains its color but is muted by the terminal's faint rendering

In practice, the three tiers reduce to two visual treatments: **dim** and **not dim**. The highlight/normal distinction exists for semantic clarity in config — users explicitly declare which statuses represent active work vs which are just "everything else."

The `Renderer` resolves a task's visual treatment by looking up its status against the workflow's highlight/dim lists at render time. When a row is dimmed, no manual color math is needed — lipgloss's `Faint(true)` is applied to the entire row and the terminal handles brightness reduction.

## 5. Color Application by Command

### `tusk list` (task table)

| Element | Color Treatment |
|---------|----------------|
| Header row | Bold |
| Priority column | Priority color (Section 3) |
| Status column | Dim/normal/highlight per workflow config |
| Full row | Faint when status is in `dim_statuses` |
| Tags (`+name`) | Tag's hex color if set, otherwise inherits row color |

### `tusk info` (detail view)

| Element | Color Treatment |
|---------|----------------|
| Labels ("Status:", "Priority:", etc.) | Bold |
| Priority value | Priority color |
| Status value | Dim/normal/highlight per workflow config |
| Tags | Tag's hex color if set |
| Description | Glamour markdown rendered (Section 7) |
| Other fields | Normal color |

### `tusk tree` (hierarchy view)

| Element | Color Treatment |
|---------|----------------|
| Short ID | Normal |
| Status `[brackets]` | Dim/normal/highlight per workflow config |
| Title | Faint when status is in `dim_statuses` |
| Tags | Tag's hex color if set |

## 6. Tag Colors

### CLI Surface

Tags already have an optional `Color *string` (hex) field in the domain model, stored in the database.

**New flag on existing command:**
- `tusk tag modify <name> --color <hex>` — set tag color (e.g., `--color "#ff4444"`)
- `tusk tag modify <name> --color ""` — clear tag color

**Validation:** Must be a valid 6-digit hex color with `#` prefix (e.g., `#ff4444`).

### Display

Wherever tags appear in text output as `+tagname`:
- If the tag has a hex color set → render tag name in that foreground color via lipgloss
- If no color set → render with default text color (inherits row styling)

### Not Changed

- `tusk tag create` — no `--color` flag (set via modify after creation)
- JSON output — tag color already included as a field
- MCP tools — tag color already exposed in responses

## 7. Markdown Description Rendering

### Scope

Only the `Description` field in `tusk info` text output.

### Implementation

- Use `glamour.RenderWithEnvironmentConfig(description)` for auto-detection of terminal background (dark/light) and word-wrap to terminal width
- When `color=false`: use `glamour.WithStyles(glamour.NoTTYStyleConfig)` for plain text fallback — ASCII formatting for headings/lists/code blocks, no ANSI colors
- When `format=json`: description returned as raw string, no rendering

### Edge Cases

- Empty description → skip section entirely (current behavior preserved)
- Plain text description (no markdown) → glamour renders as-is
- Long descriptions → glamour word-wraps based on terminal width via `golang.org/x/term`

## 8. Config Changes Summary

### Existing (no schema change)

```toml
[tui]
color = true  # already exists, now actually wired up
```

### New Workflow Fields

```toml
[workflows.kanban]
highlight_statuses = ["active"]
dim_statuses = ["completed", "deleted"]
```

### New CLI Flag

```
--no-color    Disable colored output
```

## 9. Files Affected

| File | Change |
|------|--------|
| `go.mod` | Add `charm.land/lipgloss/v2`, `charm.land/glamour/v2` |
| `internal/tui/render.go` | Refactor to `Renderer` struct with methods, add `Styles` |
| `internal/tui/commands.go` | Update callers to use `Renderer` methods |
| `internal/tui/tree.go` | Update to use `Renderer` methods |
| `internal/tui/tag.go` | Add `--color` flag to modify command |
| `internal/tui/project.go` | Update to use `Renderer` methods |
| `internal/tui/workflow.go` | Update to use `Renderer` methods |
| `internal/tui/app.go` | Create `Renderer`, add `--no-color` flag, wire `colorEnabled()` |
| `internal/config/config.go` | Add `HighlightStatuses`, `DimStatuses` to `WorkflowConfig` |
| `internal/config/default.toml` | Add `highlight_statuses`, `dim_statuses` to kanban defaults |
| `cmd/tusk/main.go` | Pass config to `Renderer` creation |
| `tests/e2e/` | Update expectations for any text output format changes |
