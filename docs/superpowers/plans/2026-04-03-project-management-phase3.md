# Project Management Phase 3: CLI Commands + Wiring

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Wire `ProjectService` into the app, replace `ProjectLookup` interface, and add `tusk project {list,create,modify}` CLI subcommands with text and JSON output.

**Architecture:** The `tui.App` struct drops `ProjectLookup` in favor of `*service.ProjectService`. A new `project.go` file in `internal/tui/` defines the Cobra subcommand group. Rendering functions go in `render.go`. The `cmd/tusk/main.go` creates the `ProjectService` and passes it to `tui.New()`.

**Tech Stack:** Go, Cobra (github.com/spf13/cobra), encoding/json

**Prerequisite:** Phase 2 must be complete (ProjectService with Create, List, GetByName, GetByID, Modify).

---

### Task 1: Wire ProjectService into App and Update Existing Code

**Files:**
- Modify: `internal/tui/app.go`
- Modify: `internal/tui/commands.go`
- Modify: `cmd/tusk/main.go`
- Modify: `internal/tui/commands_test.go`

This task replaces the `ProjectLookup` interface with `*service.ProjectService` throughout the codebase.

- [ ] **Step 1: Update App struct and New function in app.go**

In `internal/tui/app.go`, make these changes:

Remove the `ProjectLookup` interface (lines 21-25):

```go
// ProjectLookup is the subset of project operations the TUI needs.
type ProjectLookup interface {
	GetByID(ctx context.Context, id uuid.UUID) (*domain.Project, error)
	GetByName(ctx context.Context, name string) (*domain.Project, error)
}
```

In the `App` struct, change the `projectRepo` field:

```go
projectRepo ProjectLookup
```

to:

```go
projectSvc *service.ProjectService
```

Update the `New` function signature — change:

```go
func New(taskSvc *service.TaskService, tagSvc *service.TagService, relationSvc *service.RelationService, projectRepo ProjectLookup, vi VersionInfo) *App {
```

to:

```go
func New(taskSvc *service.TaskService, tagSvc *service.TagService, relationSvc *service.RelationService, projectSvc *service.ProjectService, vi VersionInfo) *App {
```

Inside `New`, change:

```go
projectRepo: projectRepo,
```

to:

```go
projectSvc: projectSvc,
```

And change the resolver creation:

```go
a.resolver = filter.NewResolver(projectRepo, taskSvc)
```

to:

```go
a.resolver = filter.NewResolver(projectSvc, taskSvc)
```

This works because `*service.ProjectService` has a `GetByName` method, satisfying the `filter.ProjectLookup` interface (which only requires `GetByName`).

Also update the doc comment on `New`:

```go
// New creates a new App and builds the Cobra command tree.
// taskSvc, tagSvc, and projectSvc may be nil for testing command registration.
```

Remove the unused import of `"github.com/google/uuid"` if it was only used by the `ProjectLookup` interface. Check: the `uuid` import is used in `ProjectLookup`. After removing the interface, if nothing else in `app.go` uses `uuid`, remove the import. (The `uuid` import is NOT used elsewhere in app.go — `context` is used by `ProjectLookup` too, but Cobra commands reference `context` implicitly. Double-check by compiling.)

Remove the `"context"` import only if nothing else in app.go uses it. Looking at app.go, nothing else uses `context` — the command handlers are in `commands.go`. So remove both `"context"` and `"github.com/google/uuid"` imports. Also remove `"github.com/germanamz/tusk/internal/domain"` if unused.

The cleaned up imports for `app.go` should be:

```go
import (
	"fmt"

	"github.com/germanamz/tusk/internal/filter"
	"github.com/germanamz/tusk/internal/service"
	"github.com/spf13/cobra"
)
```

- [ ] **Step 2: Update commands.go to use projectSvc**

In `internal/tui/commands.go`, there are three places that reference `a.projectRepo`:

1. **runAdd** (around line 51) — change:
```go
project, err := a.projectRepo.GetByName(ctx, f.Value)
```
to:
```go
project, err := a.projectSvc.GetByName(ctx, f.Value)
```

2. **runInfo** (around line 170) — change:
```go
project, err := a.projectRepo.GetByID(ctx, *task.ProjectID)
```
to:
```go
project, err := a.projectSvc.GetByID(ctx, *task.ProjectID)
```

3. **runModify** (around line 277) — change:
```go
project, err := a.projectRepo.GetByName(ctx, f.Value)
```
to:
```go
project, err := a.projectSvc.GetByName(ctx, f.Value)
```

- [ ] **Step 3: Update main.go to create ProjectService**

In `cmd/tusk/main.go`, add the `ProjectService` import and creation.

The import block already has `"github.com/germanamz/tusk/internal/service"`. No new import needed.

After the line `projectRepo := sqlite.NewProjectRepo(db)` (around line 50), add:

```go
projectSvc := service.NewProjectService(projectRepo)
```

Then change the `tui.New(...)` call (around line 61) from:

```go
app := tui.New(taskSvc, tagSvc, relationSvc, projectRepo, tui.VersionInfo{
```

to:

```go
app := tui.New(taskSvc, tagSvc, relationSvc, projectSvc, tui.VersionInfo{
```

- [ ] **Step 4: Update commands_test.go**

In `internal/tui/commands_test.go`, the test helper creates the app. Find the line that passes `projectRepo` to `New` (around line 87):

```go
app := New(taskSvc, tagSvc, relationSvc, projectRepo, VersionInfo{})
```

Change to:

```go
projectSvc := service.NewProjectService(projectRepo)
app := New(taskSvc, tagSvc, relationSvc, projectSvc, VersionInfo{})
```

Make sure `service` is imported at the top of the file. It's likely already imported since other services are used.

- [ ] **Step 5: Verify everything compiles and tests pass**

```bash
go build ./... && make test
```

Expected: All tests pass. The wiring change is a straight replacement — `ProjectService` exposes the same `GetByName` and `GetByID` methods that `ProjectLookup` had.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/app.go internal/tui/commands.go internal/tui/commands_test.go cmd/tusk/main.go
git commit -m "refactor: replace ProjectLookup with ProjectService in TUI layer

Wire ProjectService through App struct, update all command handlers
and main.go to use the service instead of direct repo access."
```

---

### Task 2: Add Project Rendering Functions

**Files:**
- Modify: `internal/tui/render.go`

- [ ] **Step 1: Add projectJSON type and rendering functions**

Append to `internal/tui/render.go`:

```go
// projectJSON is the JSON serialization format for a project.
type projectJSON struct {
	ID              string                      `json:"id"`
	Name            string                      `json:"name"`
	Description     string                      `json:"description"`
	DefaultWorkflow string                      `json:"default_workflow"`
	Settings        domain.ProjectSettings      `json:"settings"`
	Version         int                         `json:"version"`
	CreatedAt       string                      `json:"created_at"`
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

// renderProjectList writes a list of projects to w.
// Text format renders a table; JSON format renders an array.
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

// renderProjectResult writes a single project mutation result.
// Text format shows key-value pairs; JSON format shows the full object.
func renderProjectResult(w io.Writer, action string, project *domain.Project, format string) error {
	if format == "json" {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(toProjectJSON(project))
	}
	_, err := fmt.Fprintf(w, "%s project %s\n", action, project.Name)
	return err
}

// formatSettingsSummary returns a compact text summary of project settings.
func formatSettingsSummary(s domain.ProjectSettings) string {
	var parts []string
	if s.AutoCompleteParent != nil {
		parts = append(parts, "auto-complete:on")
	}
	if s.AutoRevertParent != nil {
		parts = append(parts, "auto-revert:on")
	}
	if len(parts) == 0 {
		return "-"
	}
	return strings.Join(parts, " ")
}

// truncate shortens a string to maxLen characters, adding "..." if truncated.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}
```

- [ ] **Step 2: Verify it compiles**

```bash
go build ./internal/tui/...
```

Expected: Success.

- [ ] **Step 3: Commit**

```bash
git add internal/tui/render.go
git commit -m "feat: add project rendering functions for text and JSON output"
```

---

### Task 3: Add Project CLI Subcommands

**Files:**
- Create: `internal/tui/project.go`
- Modify: `internal/tui/app.go` (register the subcommand)

- [ ] **Step 1: Create the project subcommand file**

Create `internal/tui/project.go`:

```go
package tui

import (
	"fmt"
	"strings"

	"github.com/germanamz/tusk/internal/service"
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

	// tusk project create <name>
	createCmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a new project",
		Args:  cobra.ExactArgs(1),
		RunE:  a.runProjectCreate,
	}
	createCmd.Flags().StringP("description", "d", "", "project description")
	projectCmd.AddCommand(createCmd)

	// tusk project modify <name>
	modifyCmd := &cobra.Command{
		Use:   "modify <name>",
		Short: "Modify a project",
		Args:  cobra.ExactArgs(1),
		RunE:  a.runProjectModify,
	}
	modifyCmd.Flags().StringP("description", "d", "", "new description")
	modifyCmd.Flags().StringArray("set", nil, "set a settings value (dot-path=value)")
	modifyCmd.Flags().StringArray("unset", nil, "unset a settings key (dot-path)")
	projectCmd.AddCommand(modifyCmd)

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

func (a *App) runProjectCreate(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	name := args[0]
	description, _ := cmd.Flags().GetString("description")

	project, err := a.projectSvc.Create(ctx, name, description)
	if err != nil {
		return err
	}
	return renderProjectResult(cmd.OutOrStdout(), "Created", project, a.format)
}

func (a *App) runProjectModify(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	name := args[0]

	opts := service.ModifyOptions{}

	if cmd.Flags().Changed("description") {
		desc, _ := cmd.Flags().GetString("description")
		opts.Description = &desc
	}

	setFlags, _ := cmd.Flags().GetStringArray("set")
	if len(setFlags) > 0 {
		opts.Sets = make(map[string]string, len(setFlags))
		for _, s := range setFlags {
			parts := strings.SplitN(s, "=", 2)
			if len(parts) != 2 {
				return fmt.Errorf("invalid --set format %q: expected key=value", s)
			}
			opts.Sets[parts[0]] = parts[1]
		}
	}

	unsetFlags, _ := cmd.Flags().GetStringArray("unset")
	if len(unsetFlags) > 0 {
		opts.Unsets = unsetFlags
	}

	project, err := a.projectSvc.Modify(ctx, name, opts)
	if err != nil {
		return err
	}
	return renderProjectResult(cmd.OutOrStdout(), "Modified", project, a.format)
}
```

- [ ] **Step 2: Register the project command in app.go**

In `internal/tui/app.go`, inside the `New` function, add the project command to the root. Add it right before the `return a` statement (around line 143):

After the existing `a.root.AddCommand(...)` block that adds all the task commands, add:

```go
a.root.AddCommand(a.buildProjectCmd())
```

So the end of the `New` function looks like:

```go
	a.root.AddCommand(
		// ... existing commands ...
	)

	a.root.AddCommand(a.buildProjectCmd())

	return a
}
```

- [ ] **Step 3: Verify it compiles**

```bash
go build ./...
```

Expected: Success.

- [ ] **Step 4: Manual smoke test**

Build and test the commands:

```bash
make build
./bin/tusk project list
./bin/tusk project create testproj -d "Test project"
./bin/tusk project list
./bin/tusk project modify testproj --set auto_complete_parent.trigger_status=completed --set auto_complete_parent.target_status=completed
./bin/tusk project list --format json
```

Expected: Commands work, output is formatted correctly.

- [ ] **Step 5: Run the full test suite**

```bash
make test
```

Expected: All tests pass.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/project.go internal/tui/app.go
git commit -m "feat: add tusk project {list,create,modify} CLI subcommands

First noun-verb subcommand group. Supports --description, --set
dot-path=value, and --unset dot-path flags for project modification."
```
