# Tag Management Phase 3: CLI & E2E Tests

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the `tusk tag` CLI command group (create, list, modify, delete, rename) with text/JSON rendering and comprehensive E2E tests.

**Architecture:** New file `internal/tui/tag.go` for the command group (mirroring `internal/tui/project.go`). New render functions in `internal/tui/render.go`. E2E tests in a new `tests/e2e/tag_management_test.go`. Registration in `internal/tui/app.go`.

**Tech Stack:** Go, Cobra, JSON encoding

**Spec:** `docs/superpowers/specs/2026-04-03-tag-management-design.md`

**Depends on:** Phase 2 (service methods) must be complete.

---

### Task 1: Add tag rendering functions

**Files:**
- Modify: `internal/tui/render.go`

- [ ] **Step 1: Add `tagJSON` type and `toTagJSON` converter**

Open `internal/tui/render.go`. Add the following after the `projectJSON` / `toProjectJSON` block (after line 34):

```go
// tagJSON is the JSON serialization format for a tag.
type tagJSON struct {
	ID    string  `json:"id"`
	Name  string  `json:"name"`
	Color *string `json:"color"`
}

// tagWithUsageJSON is the JSON serialization format for a tag with usage count.
type tagWithUsageJSON struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	Color     *string `json:"color"`
	TaskCount int     `json:"task_count"`
}

func toTagJSON(t *domain.Tag) tagJSON {
	return tagJSON{
		ID:    t.ID.String(),
		Name:  t.Name,
		Color: t.Color,
	}
}

func toTagWithUsageJSON(tw domain.TagWithUsage) tagWithUsageJSON {
	return tagWithUsageJSON{
		ID:        tw.Tag.ID.String(),
		Name:      tw.Tag.Name,
		Color:     tw.Tag.Color,
		TaskCount: tw.TaskCount,
	}
}
```

- [ ] **Step 2: Add `renderTagList` function**

Add after the converter functions:

```go
// renderTagList writes a list of tags to w.
// If showUsage is true, includes the task count column.
func renderTagList(w io.Writer, tags []domain.TagWithUsage, showUsage bool, format string) error {
	if format == "json" {
		items := make([]tagWithUsageJSON, len(tags))
		for i, tw := range tags {
			items[i] = toTagWithUsageJSON(tw)
		}
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(items)
	}

	if len(tags) == 0 {
		return nil
	}

	if showUsage {
		if _, err := fmt.Fprintf(w, "%-20s %-10s %s\n", "NAME", "COLOR", "TASKS"); err != nil {
			return err
		}
		for _, tw := range tags {
			color := "-"
			if tw.Tag.Color != nil {
				color = *tw.Tag.Color
			}
			if _, err := fmt.Fprintf(w, "%-20s %-10s %d\n", tw.Tag.Name, color, tw.TaskCount); err != nil {
				return err
			}
		}
	} else {
		if _, err := fmt.Fprintf(w, "%-20s %s\n", "NAME", "COLOR"); err != nil {
			return err
		}
		for _, tw := range tags {
			color := "-"
			if tw.Tag.Color != nil {
				color = *tw.Tag.Color
			}
			if _, err := fmt.Fprintf(w, "%-20s %s\n", tw.Tag.Name, color); err != nil {
				return err
			}
		}
	}
	return nil
}
```

- [ ] **Step 3: Add `renderTagResult` function**

Add after `renderTagList`:

```go
// renderTagResult writes a single tag mutation result.
func renderTagResult(w io.Writer, action string, tag *domain.Tag, format string) error {
	if format == "json" {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(toTagJSON(tag))
	}
	_, err := fmt.Fprintf(w, "%s tag %s\n", action, tag.Name)
	return err
}
```

- [ ] **Step 4: Verify the build passes**

Run: `go build ./...`
Expected: clean build.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/render.go
git commit -m "feat(tui): add tag rendering functions"
```

---

### Task 2: Build `tusk tag` command group

**Files:**
- Create: `internal/tui/tag.go`
- Modify: `internal/tui/app.go:134`

- [ ] **Step 1: Create `internal/tui/tag.go`**

Create the new file with the command group and all five handlers:

```go
package tui

import (
	"fmt"

	"github.com/germanamz/tusk/internal/domain"
	"github.com/spf13/cobra"
)

// buildTagCmd creates the `tusk tag` command group with its subcommands.
func (a *App) buildTagCmd() *cobra.Command {
	tagCmd := &cobra.Command{
		Use:   "tag",
		Short: "Manage tags",
	}

	// tusk tag list
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List all tags",
		Args:  cobra.NoArgs,
		RunE:  a.runTagList,
	}
	listCmd.Flags().String("color", "", `filter by color: "any", "none", or a hex value`)
	listCmd.Flags().Bool("usage", false, "show task count per tag")
	tagCmd.AddCommand(listCmd)

	// tusk tag create <name>
	createCmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a new tag",
		Args:  cobra.ExactArgs(1),
		RunE:  a.runTagCreate,
	}
	createCmd.Flags().String("color", "", "tag color as hex (e.g. #ff0000)")
	tagCmd.AddCommand(createCmd)

	// tusk tag modify <name>
	modifyCmd := &cobra.Command{
		Use:   "modify <name>",
		Short: "Modify a tag",
		Args:  cobra.ExactArgs(1),
		RunE:  a.runTagModify,
	}
	modifyCmd.Flags().String("color", "", `tag color as hex, or "" to clear`)
	tagCmd.AddCommand(modifyCmd)

	// tusk tag delete <name>
	tagCmd.AddCommand(&cobra.Command{
		Use:   "delete <name>",
		Short: "Delete a tag (must not be assigned to any tasks)",
		Args:  cobra.ExactArgs(1),
		RunE:  a.runTagDelete,
	})

	// tusk tag rename <old> <new>
	tagCmd.AddCommand(&cobra.Command{
		Use:   "rename <old> <new>",
		Short: "Rename a tag",
		Args:  cobra.ExactArgs(2),
		RunE:  a.runTagRename,
	})

	return tagCmd
}

func (a *App) runTagList(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	showUsage, _ := cmd.Flags().GetBool("usage")

	tags, err := a.tagSvc.ListWithUsage(ctx)
	if err != nil {
		return err
	}

	// Apply --color filter if provided
	if cmd.Flags().Changed("color") {
		colorFilter, _ := cmd.Flags().GetString("color")
		tags = filterTagsByColor(tags, colorFilter)
	}

	return renderTagList(cmd.OutOrStdout(), tags, showUsage, a.format)
}

// filterTagsByColor filters tags based on the color flag value.
// "any" = tags with a color set, "none" = tags without a color,
// anything else = exact color match.
func filterTagsByColor(tags []domain.TagWithUsage, filter string) []domain.TagWithUsage {
	result := make([]domain.TagWithUsage, 0, len(tags))
	for _, tw := range tags {
		switch filter {
		case "any":
			if tw.Tag.Color != nil {
				result = append(result, tw)
			}
		case "none":
			if tw.Tag.Color == nil {
				result = append(result, tw)
			}
		default:
			if tw.Tag.Color != nil && *tw.Tag.Color == filter {
				result = append(result, tw)
			}
		}
	}
	return result
}

func (a *App) runTagCreate(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	name := args[0]

	var color *string
	if cmd.Flags().Changed("color") {
		c, _ := cmd.Flags().GetString("color")
		color = &c
	}

	tag, err := a.tagSvc.Create(ctx, name, color)
	if err != nil {
		return err
	}
	return renderTagResult(cmd.OutOrStdout(), "Created", tag, a.format)
}

func (a *App) runTagModify(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	name := args[0]

	if !cmd.Flags().Changed("color") {
		return fmt.Errorf("at least one flag must be specified (--color)")
	}

	colorStr, _ := cmd.Flags().GetString("color")
	var color *string
	if colorStr != "" {
		color = &colorStr
	}
	// If colorStr is empty string, color stays nil — this clears the color

	tag, err := a.tagSvc.Modify(ctx, name, color)
	if err != nil {
		return err
	}
	return renderTagResult(cmd.OutOrStdout(), "Modified", tag, a.format)
}

func (a *App) runTagDelete(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	name := args[0]

	if err := a.tagSvc.Delete(ctx, name); err != nil {
		return err
	}

	// For JSON, create a minimal tag to render (we don't have it after deletion)
	if a.format == "json" {
		return renderTagResult(cmd.OutOrStdout(), "Deleted", &domain.Tag{Name: name}, a.format)
	}
	_, err := fmt.Fprintf(cmd.OutOrStdout(), "Deleted tag %s\n", name)
	return err
}

func (a *App) runTagRename(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	oldName, newName := args[0], args[1]

	if err := a.tagSvc.Rename(ctx, oldName, newName); err != nil {
		return err
	}

	if a.format == "json" {
		return renderTagResult(cmd.OutOrStdout(), "Renamed", &domain.Tag{Name: newName}, a.format)
	}
	_, err := fmt.Fprintf(cmd.OutOrStdout(), "Renamed tag %s to %s\n", oldName, newName)
	return err
}
```

- [ ] **Step 2: Register the tag command in `app.go`**

Open `internal/tui/app.go`. Find line 134 where `buildProjectCmd` is registered:

```go
a.root.AddCommand(a.buildProjectCmd())
```

Add the tag command registration on the next line:

```go
a.root.AddCommand(a.buildTagCmd())
```

- [ ] **Step 3: Verify the build passes**

Run: `go build ./...`
Expected: clean build.

- [ ] **Step 4: Quick manual smoke test**

Run: `go run ./cmd/tusk tag --help`
Expected: shows "Manage tags" with subcommands: create, delete, list, modify, rename.

Run: `go run ./cmd/tusk tag list --help`
Expected: shows "List all tags" with `--color` and `--usage` flags.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/tag.go internal/tui/app.go
git commit -m "feat(tui): add tusk tag command group with create, list, modify, delete, rename"
```

---

### Task 3: E2E tests — CRUD operations

**Files:**
- Create: `tests/e2e/tag_management_test.go`

This file follows the exact same pattern as `tests/e2e/project_test.go` and `tests/e2e/tags_test.go`. Each scenario is a `Scenario` struct with `Steps`, run across the 4-mode matrix via `runScenarios`.

**Important:** The `expandRefs` in `harness.go` currently matches mutation output like "Created task abc123" using `shortIDPattern`. For tag commands, the output format is "Created tag foo" — this does NOT match the regex (`(?:Created|...) task ([0-9a-f]{8,})`). Tag commands don't produce short IDs, so we don't need `$N.short_id` references for tag results. We reference tag names directly since they're deterministic.

However, for the `tag_delete_in_use` scenario we create a task and need its `short_id`. The `$0.short_id` reference works because step 0 is `tusk add` which produces "Created task <short_id>".

- [ ] **Step 1: Create `tests/e2e/tag_management_test.go`**

```go
package e2e

import "testing"

func TestTagManagement(t *testing.T) {
	scenarios := []Scenario{
		{
			Name: "tag_create",
			Steps: []Step{
				{
					Args: []string{"tag", "create", "foo"},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Created tag foo")
					},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						assertEqual(t, m["name"], "foo")
					},
				},
				// Duplicate should fail
				{
					Args:    []string{"tag", "create", "foo"},
					WantErr: true,
				},
			},
		},
		{
			Name: "tag_create_with_color",
			Steps: []Step{
				{
					Args: []string{"tag", "create", "colored", "--color", "#ff0000"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						assertEqual(t, m["name"], "colored")
						assertEqual(t, m["color"], "#ff0000")
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Created tag colored")
					},
				},
			},
		},
		{
			Name: "tag_list",
			Steps: []Step{
				{Args: []string{"tag", "create", "alpha"}},
				{Args: []string{"tag", "create", "beta"}},
				{
					Args: []string{"tag", "list"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						arr := jsonArray(t, parsed)
						if len(arr) != 2 {
							t.Fatalf("expected 2 tags, got %d", len(arr))
						}
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "alpha")
						assertContains(t, output, "beta")
					},
				},
			},
		},
		{
			Name: "tag_list_with_usage",
			Steps: []Step{
				{Args: []string{"tag", "create", "tracked"}},
				{Args: []string{"add", "Task one", "+tracked"}},
				{
					Args: []string{"tag", "list", "--usage"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						arr := jsonArray(t, parsed)
						if len(arr) != 1 {
							t.Fatalf("expected 1 tag, got %d", len(arr))
						}
						m := arr[0].(map[string]any)
						assertEqual(t, m["name"], "tracked")
						assertEqual(t, m["task_count"], float64(1))
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "tracked")
						assertContains(t, output, "TASKS")
					},
				},
			},
		},
		{
			Name: "tag_list_filter_color",
			Steps: []Step{
				{Args: []string{"tag", "create", "red", "--color", "#ff0000"}},
				{Args: []string{"tag", "create", "plain"}},
				// Filter: only colored tags
				{
					Args: []string{"tag", "list", "--color", "any"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						arr := jsonArray(t, parsed)
						if len(arr) != 1 {
							t.Fatalf("expected 1 colored tag, got %d", len(arr))
						}
						assertEqual(t, arr[0].(map[string]any)["name"], "red")
					},
				},
				// Filter: only uncolored tags
				{
					Args: []string{"tag", "list", "--color", "none"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						arr := jsonArray(t, parsed)
						if len(arr) != 1 {
							t.Fatalf("expected 1 uncolored tag, got %d", len(arr))
						}
						assertEqual(t, arr[0].(map[string]any)["name"], "plain")
					},
				},
			},
		},
		{
			Name: "tag_modify_color",
			Steps: []Step{
				{Args: []string{"tag", "create", "tocolor"}},
				{
					Args: []string{"tag", "modify", "tocolor", "--color", "#00ff00"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						assertEqual(t, m["name"], "tocolor")
						assertEqual(t, m["color"], "#00ff00")
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Modified tag tocolor")
					},
				},
			},
		},
		{
			Name: "tag_modify_clear_color",
			Steps: []Step{
				{Args: []string{"tag", "create", "clearme", "--color", "#aabbcc"}},
				{
					Args: []string{"tag", "modify", "clearme", "--color", ""},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						assertEqual(t, m["name"], "clearme")
						if m["color"] != nil {
							t.Fatalf("expected nil color after clearing, got %v", m["color"])
						}
					},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Modified tag clearme")
					},
				},
			},
		},
		{
			Name: "tag_modify_no_flags",
			Steps: []Step{
				{Args: []string{"tag", "create", "noflag"}},
				{
					Args:    []string{"tag", "modify", "noflag"},
					WantErr: true,
				},
			},
		},
		{
			Name: "tag_rename",
			Steps: []Step{
				{Args: []string{"tag", "create", "oldname"}},
				{
					Args: []string{"tag", "rename", "oldname", "newname"},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Renamed tag oldname to newname")
					},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						assertEqual(t, m["name"], "newname")
					},
				},
				// Verify old name is gone and new name exists
				{
					Args: []string{"tag", "list"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						arr := jsonArray(t, parsed)
						if len(arr) != 1 {
							t.Fatalf("expected 1 tag, got %d", len(arr))
						}
						assertEqual(t, arr[0].(map[string]any)["name"], "newname")
					},
				},
			},
		},
		{
			Name: "tag_rename_conflict",
			Steps: []Step{
				{Args: []string{"tag", "create", "aaa"}},
				{Args: []string{"tag", "create", "bbb"}},
				{
					Args:    []string{"tag", "rename", "aaa", "bbb"},
					WantErr: true,
				},
			},
		},
		{
			Name: "tag_delete",
			Steps: []Step{
				{Args: []string{"tag", "create", "temp"}},
				{
					Args: []string{"tag", "delete", "temp"},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "Deleted tag temp")
					},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						assertEqual(t, m["name"], "temp")
					},
				},
				// Verify it's gone
				{
					Args: []string{"tag", "list"},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						arr := jsonArray(t, parsed)
						if len(arr) != 0 {
							t.Fatalf("expected 0 tags after delete, got %d", len(arr))
						}
					},
				},
			},
		},
		{
			Name: "tag_delete_in_use",
			Steps: []Step{
				{Args: []string{"add", "My task", "+busy"}},
				{
					Args:    []string{"tag", "delete", "busy"},
					WantErr: true,
				},
			},
		},
	}

	runScenarios(t, binPath, scenarios)
}
```

- [ ] **Step 2: Build the binary and run E2E tests**

Run: `make build && go test -v ./tests/e2e/ -run TestTagManagement -timeout 60s`
Expected: all 12 scenarios pass across the 4-mode matrix (48 test cases total).

- [ ] **Step 3: Run full test suite to check for regressions**

Run: `make test`
Expected: all tests pass (unit + E2E).

- [ ] **Step 4: Commit**

```bash
git add tests/e2e/tag_management_test.go
git commit -m "test(e2e): add tag management E2E tests"
```

---

### Task 4: Update ROADMAP.md

**Files:**
- Modify: `ROADMAP.md:139-144`

- [ ] **Step 1: Mark the Tag Management stories as done**

Open `ROADMAP.md`. Find the Tag Management initiative (around lines 138-144). Change the checkboxes from `[ ]` to `[x]`:

```markdown
### Initiative: Tag Management

> Dedicated tag subcommand for CRUD operations.

- [x] **Story: `tusk tag` subcommand**
  - [x] `tusk tag create <name>` — create a tag
  - [x] `tusk tag list` — list all tags
  - [x] `tusk tag delete <name>` — delete a tag
  - [x] `tusk tag rename <old> <new>` — rename a tag
```

- [ ] **Step 2: Run full test suite one final time**

Run: `make test`
Expected: all tests pass.

- [ ] **Step 3: Commit**

```bash
git add ROADMAP.md
git commit -m "chore: mark tag management initiative as done"
```
