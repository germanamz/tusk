# Config-based Projects — Phase 4: Filter & CLI Layer

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Update the filter resolver to work without `ProjectLookup`, update all CLI commands for string project IDs, and simplify project rendering for the new domain struct.

**Architecture:** The filter resolver no longer needs to resolve project names to UUIDs — the project ID IS the name. CLI project commands drop `create` and `modify` (read-only). Task commands use string project IDs directly.

**Tech Stack:** Go, Cobra CLI

**Prerequisite:** Phase 3 (Task domain changes, SQLite updates, TaskService updates) must be complete.

**Design spec:** `docs/superpowers/specs/2026-04-04-config-based-projects-design.md`

---

### Task 1: Simplify filter.Resolver — remove ProjectLookup

**Files:**
- Modify: `internal/filter/resolve.go`

The `project:backend` filter can now map directly to `filter.ProjectID = "backend"` without a database lookup. The `ProjectLookup` interface is removed.

- [ ] **Step 1: Update internal/filter/resolve.go**

Make these changes:

**Remove** the `ProjectLookup` interface:
```go
// DELETE THIS:
type ProjectLookup interface {
	GetByName(ctx context.Context, name string) (*domain.Project, error)
}
```

**Update** the `Resolver` struct to remove the `projectLookup` field:
```go
// Before:
type Resolver struct {
	projectLookup ProjectLookup
	taskLookup    TaskLookup
}

// After:
type Resolver struct {
	taskLookup TaskLookup
}
```

**Update** `NewResolver` to remove the `projectLookup` parameter:
```go
// Before:
func NewResolver(projectLookup ProjectLookup, taskLookup TaskLookup) *Resolver {
	return &Resolver{
		projectLookup: projectLookup,
		taskLookup:    taskLookup,
	}
}

// After:
func NewResolver(taskLookup TaskLookup) *Resolver {
	return &Resolver{
		taskLookup: taskLookup,
	}
}
```

**Update** the `project` case in the `Resolve` method. Replace:
```go
		case "project":
			project, err := r.projectLookup.GetByName(ctx, field.Value)
			if err != nil {
				if errors.Is(err, domain.ErrNotFound) {
					errs = append(errs, fmt.Errorf("project %q not found", field.Value))
				} else {
					errs = append(errs, fmt.Errorf("looking up project %q: %w", field.Value, err))
				}
				continue
			}
			tf.ProjectID = &project.ID
```
with:
```go
		case "project":
			id := field.Value
			tf.ProjectID = &id
```

**Clean up imports**: remove `"errors"` if it's no longer used elsewhere in the file. Check each import — `errors` was only used in the project lookup error handling. The `domain` import may also be removable if it's not used elsewhere (check the `domain.ErrNotFound` reference in the `parent` and `tree` cases — those still use `domain.ErrNotFound`, so keep `domain`).

Actually, looking at the full file: `errors` is used in the `parent` and `tree` cases too (`errors.Is(err, domain.ErrNotFound)`), so keep it. `domain` is used for `domain.TaskFilter` and `domain.ErrNotFound`. Both stay.

- [ ] **Step 2: Verify filter package compiles**

Run: `go build ./internal/filter/...`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/filter/resolve.go
git commit -m "feat(filter): remove ProjectLookup, resolve project filter as direct string"
```

---

### Task 2: Update CLI project commands

**Files:**
- Modify: `internal/tui/project.go`

Remove `tusk project create` and `tusk project modify` subcommands. Keep `tusk project list`.

- [ ] **Step 1: Rewrite internal/tui/project.go**

Replace the entire file contents with:

```go
package tui

import (
	"github.com/spf13/cobra"
)

// buildProjectCmd creates the `tusk project` command group with its subcommands.
func (a *App) buildProjectCmd() *cobra.Command {
	projectCmd := &cobra.Command{
		Use:   "project",
		Short: "Manage projects",
	}

	// tusk project list
	projectCmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List all projects",
		Args:  cobra.NoArgs,
		RunE:  a.runProjectList,
	})

	return projectCmd
}

func (a *App) runProjectList(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	projects, err := a.projectSvc.List(ctx)
	if err != nil {
		return err
	}
	return renderProjectList(cmd.OutOrStdout(), projects, a.format)
}
```

This removes `runProjectCreate`, `runProjectModify`, the `create` subcommand, the `modify` subcommand, and the `service` import.

- [ ] **Step 2: Verify the tui package compiles (may fail due to other files)**

Run: `go build ./internal/tui/...`

This may fail because `commands.go` and `render.go` still use old types. That's expected — Tasks 3 and 4 fix them.

- [ ] **Step 3: Commit**

```bash
git add internal/tui/project.go
git commit -m "feat(tui): remove project create/modify commands, keep list"
```

---

### Task 3: Update CLI task commands for string project IDs

**Files:**
- Modify: `internal/tui/commands.go`
- Modify: `internal/tui/app.go`

Update `runAdd`, `runModify`, and `runInfo` to use string project IDs instead of UUID-based lookups.

- [ ] **Step 1: Update runAdd in commands.go**

Find the project handling block (around lines 49-56):
```go
	// Project
	if f, ok := fs.GetField("project"); ok {
		project, err := a.projectSvc.GetByName(ctx, f.Value)
		if err != nil {
			return fmt.Errorf("project %q not found", f.Value)
		}
		task.ProjectID = &project.ID
	}
```
Replace with:
```go
	// Project
	if f, ok := fs.GetField("project"); ok {
		task.ProjectID = f.Value
	}
```

Project validation (does the project exist?) happens in `TaskService.Create`, so we don't need to look it up here.

- [ ] **Step 2: Update runInfo in commands.go**

Find the project name resolution block (around lines 167-174):
```go
	// Resolve project name for display
	var projectName string
	if task.ProjectID != nil {
		project, err := a.projectSvc.GetByID(ctx, *task.ProjectID)
		if err == nil {
			projectName = project.Name
		}
	}
```
Replace with:
```go
	// Project ID is now the human-readable name — no resolution needed
	projectName := task.ProjectID
```

- [ ] **Step 3: Update runModify in commands.go**

Find the project handling block (around lines 276-284):
```go
	// Project
	if f, ok := fs.GetField("project"); ok {
		project, err := a.projectSvc.GetByName(ctx, f.Value)
		if err != nil {
			return fmt.Errorf("project %q not found", f.Value)
		}
		pid := project.ID
		pp := &pid
		upd.ProjectID = &pp
	}
```
Replace with:
```go
	// Project
	if f, ok := fs.GetField("project"); ok {
		upd.ProjectID = &f.Value
	}
```

Note: `TaskUpdate.ProjectID` is now `*string` (nil = don't change, non-nil = set). We set it to `&f.Value` which is a pointer to the string value.

- [ ] **Step 4: Clean up imports in commands.go**

After these changes, check if the `uuid` import is still needed. It was used in:
- `runList`: `uuid.UUID` for task IDs in batch tag fetch — still needed
- `runModify`: `uuid.UUID` for parent ID handling — still needed

The `uuid` import stays. But `a.projectSvc.GetByName` and `a.projectSvc.GetByID` calls in task commands are removed, so no additional import cleanup is needed.

- [ ] **Step 5: Update app.go — NewResolver call**

In `internal/tui/app.go`, the `NewResolver` call currently passes `projectSvc`:
```go
	a.resolver = filter.NewResolver(projectSvc, taskSvc)
```
Change to (since `NewResolver` no longer takes `ProjectLookup`):
```go
	a.resolver = filter.NewResolver(taskSvc)
```

- [ ] **Step 6: Verify commands.go and app.go compile together**

Run: `go build ./internal/tui/...`
Expected: May still fail due to render.go — fixed in Task 4.

- [ ] **Step 7: Commit**

```bash
git add internal/tui/commands.go internal/tui/app.go
git commit -m "feat(tui): update task commands for string project IDs"
```

---

### Task 4: Update CLI rendering for new Project struct

**Files:**
- Modify: `internal/tui/render.go`

Update `projectJSON`, `toProjectJSON`, `renderProjectList`, `renderProjectResult`, and the task rendering functions that reference `task.ProjectID`.

- [ ] **Step 1: Update projectJSON and toProjectJSON**

Replace the old `projectJSON` struct and conversion function:
```go
type projectJSON struct {
	ID              string                 `json:"id"`
	Name            string                 `json:"name"`
	Description     string                 `json:"description"`
	DefaultWorkflow string                 `json:"default_workflow"`
	Settings        domain.ProjectSettings `json:"settings"`
	Version         int                    `json:"version"`
	CreatedAt       string                 `json:"created_at"`
}

func toProjectJSON(p *domain.Project) projectJSON {
	return projectJSON{
		ID:              p.ID.String(),
		Name:            p.Name,
		Description:     p.Description,
		DefaultWorkflow: p.DefaultWorkflow,
		Settings:        p.Settings,
		Version:         p.Version,
		CreatedAt:       p.CreatedAt.Format(time.RFC3339),
	}
}
```
with:
```go
type projectJSON struct {
	ID       string                 `json:"id"`
	Workflow string                 `json:"workflow"`
	Settings domain.ProjectSettings `json:"settings"`
}

func toProjectJSON(p *domain.Project) projectJSON {
	return projectJSON{
		ID:       p.ID,
		Workflow: p.Workflow,
		Settings: p.Settings,
	}
}
```

- [ ] **Step 2: Update renderProjectList**

Replace:
```go
func renderProjectList(w io.Writer, projects []*domain.Project, format string) error {
	if format == "json" {
		items := make([]projectJSON, len(projects))
		for i, p := range projects {
			items[i] = toProjectJSON(p)
		}
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(items)
	}

	if len(projects) == 0 {
		return nil
	}

	if _, err := fmt.Fprintf(w, "%-20s %-30s %-10s %s\n", "NAME", "DESCRIPTION", "WORKFLOW", "SETTINGS"); err != nil {
		return err
	}
	for _, p := range projects {
		if _, err := fmt.Fprintf(w, "%-20s %-30s %-10s %s\n",
			p.Name,
			truncate(p.Description, 30),
			p.DefaultWorkflow,
			formatSettingsSummary(p.Settings),
		); err != nil {
			return err
		}
	}
	return nil
}
```
with:
```go
func renderProjectList(w io.Writer, projects []*domain.Project, format string) error {
	if format == "json" {
		items := make([]projectJSON, len(projects))
		for i, p := range projects {
			items[i] = toProjectJSON(p)
		}
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(items)
	}

	if len(projects) == 0 {
		return nil
	}

	if _, err := fmt.Fprintf(w, "%-20s %-10s %s\n", "ID", "WORKFLOW", "SETTINGS"); err != nil {
		return err
	}
	for _, p := range projects {
		if _, err := fmt.Fprintf(w, "%-20s %-10s %s\n",
			p.ID,
			p.Workflow,
			formatSettingsSummary(p.Settings),
		); err != nil {
			return err
		}
	}
	return nil
}
```

- [ ] **Step 3: Remove renderProjectResult**

The `renderProjectResult` function was used by `runProjectCreate` and `runProjectModify`, which no longer exist. Delete it:

```go
// DELETE THIS FUNCTION:
func renderProjectResult(w io.Writer, action string, project *domain.Project, format string) error {
	// ...
}
```

- [ ] **Step 4: Update toTaskJSON for string ProjectID**

In `toTaskJSON`, find:
```go
	if t.ProjectID != nil {
		s := t.ProjectID.String()
		tj.ProjectID = &s
	}
```
Replace with:
```go
	if t.ProjectID != "" {
		tj.ProjectID = &t.ProjectID
	}
```

Also update the `taskJSON` struct — `ProjectID` field stays as `*string` in the JSON (it's omitted when empty).

- [ ] **Step 5: Update renderTaskInfo for string ProjectID**

In `renderTaskInfo`, find:
```go
	if task.ProjectID != nil {
		projectDisplay := task.ProjectID.String()
		if projectName != "" {
			projectDisplay = fmt.Sprintf("%s (%s)", projectName, task.ProjectID.String())
		}
		if _, err := fmt.Fprintf(w, "%-13s %s\n", "Project:", projectDisplay); err != nil {
			return err
		}
	}
```
Replace with:
```go
	if task.ProjectID != "" {
		if _, err := fmt.Fprintf(w, "%-13s %s\n", "Project:", task.ProjectID); err != nil {
			return err
		}
	}
```

The project ID is now human-readable — no separate name resolution needed.

- [ ] **Step 6: Clean up unused imports in render.go**

After these changes, check if `time` is still imported. It's used in `taskJSON` field formatting (CreatedAt, ModifiedAt, DueAt, WaitUntil) and `formatAge`. Keep it.

- [ ] **Step 7: Verify the tui package compiles**

Run: `go build ./internal/tui/...`
Expected: PASS (all tui files now use the new types)

- [ ] **Step 8: Commit**

```bash
git add internal/tui/render.go
git commit -m "feat(tui): update rendering for config-driven projects"
```
