# Declarative Workflows — Phase 3: CLI, MCP, DI Wiring & E2E Tests

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `tusk workflow list` and `tusk workflow info <name>` CLI commands, add `tusk_workflow_list` MCP tool, update DI wiring in `main.go`, update MCP resource handler, and add E2E tests.

**Architecture:** New CLI commands delegate to `WorkflowService`. The MCP tool uses the existing `addTool` pattern. `main.go` swaps the SQLite workflow repo for the in-memory one. The MCP workflow resource handler is updated for the new service signature.

**Tech Stack:** Go, Cobra (CLI), mcp-go (MCP)

**Prerequisites:** Phase 1 and Phase 2 must be complete.

---

### Task 1: DI wiring and MCP resource handler update

Update `main.go` to use in-memory workflow repo and update `NewWorkflowService` call. Update the MCP workflow resource handler for the new service API.

**Files:**
- Modify: `cmd/tusk/main.go:58,63` (workflow repo and service creation)
- Modify: `internal/mcp/resources.go:132-137` (workflow resource handler)
- Modify: `internal/mcp/server.go:88-104` (validate config — add workflow tool group)

- [ ] **Step 1: Update `cmd/tusk/main.go`**

Replace line 58:

```go
	workflowRepo := sqlite.NewWorkflowRepo(db)
```

With:

```go
	workflowRepo := inmem.NewWorkflowRepository(cfg.Workflows)
```

Replace line 63:

```go
	workflowSvc := service.NewWorkflowService(workflowRepo)
```

With:

```go
	workflowSvc := service.NewWorkflowService(workflowRepo, projectRepo)
```

Also remove the `sqlite.NewWorkflowRepo` import usage — the `sqlite` package is still imported for `NewTaskRepo` etc., so the import stays. But `workflowRepo` no longer comes from `sqlite`. Add `"github.com/germanamz/tusk/internal/inmem"` to the import if not already there (it is — line 10 imports it for `inmem.NewProjectRepository`).

- [ ] **Step 2: Update `handleWorkflowResource` in `internal/mcp/resources.go`**

Replace lines 132-137:

```go
	statuses, err := s.workflowSvc.GetStatuses(ctx, project.ID, project.Workflow)
	if err != nil {
		return nil, err
	}

	transitions, err := s.workflowSvc.GetTransitions(ctx, project.ID, project.Workflow)
	if err != nil {
		return nil, err
	}
```

With:

```go
	statuses, err := s.workflowSvc.GetStatuses(ctx, project.Workflow)
	if err != nil {
		return nil, err
	}

	transitions, err := s.workflowSvc.GetTransitions(ctx, project.Workflow)
	if err != nil {
		return nil, err
	}
```

- [ ] **Step 3: Update `validateConfig` in `internal/mcp/server.go`**

Add `"tusk_workflow_list": true` to the `validToolNames` map (around line 101):

```go
	validToolNames := map[string]bool{
		"tusk_task_create":     true,
		"tusk_task_get":        true,
		"tusk_task_list":       true,
		"tusk_task_modify":     true,
		"tusk_task_start":      true,
		"tusk_task_done":       true,
		"tusk_task_delete":     true,
		"tusk_task_annotate":   true,
		"tusk_task_tree":       true,
		"tusk_relation_add":    true,
		"tusk_relation_remove": true,
		"tusk_project_list":    true,
		"tusk_workflow_list":   true,
	}
```

Add `"workflow": true` to the `validToolGroups` map (around line 103):

```go
	validToolGroups := map[string]bool{
		"task": true, "relation": true, "project": true, "workflow": true,
	}
```

- [ ] **Step 4: Verify compilation**

Run: `cd /Users/germanamz/projects/tusk && go build ./...`

Expected: PASS. The full project should compile.

- [ ] **Step 5: Commit**

```bash
git add cmd/tusk/main.go internal/mcp/resources.go internal/mcp/server.go
git commit -m "refactor: wire in-memory workflow repo and update MCP resource handler

Switch main.go from sqlite.WorkflowRepo to inmem.WorkflowRepository.
Update MCP workflow resource handler for new service signatures.
Add workflow tool group to MCP config validation."
```

---

### Task 2: Add `tusk_workflow_list` MCP tool

Register the new MCP tool and implement its handler.

**Files:**
- Modify: `internal/mcp/server.go` (add tool registration in `registerTools`)
- Modify: `internal/mcp/tools.go` (add handler and response types)

- [ ] **Step 1: Add tool registration in `internal/mcp/server.go`**

Add the following at the end of the `registerTools` method, just before the closing brace (after the `tusk_task_tree` block around line 411):

```go
	s.addTool("workflow",
		mcp.NewTool("tusk_workflow_list",
			mcp.WithDescription("List all workflows with their statuses, transitions, and referencing projects"),
		),
		s.handleWorkflowList,
	)
```

- [ ] **Step 2: Add handler and response types in `internal/mcp/tools.go`**

Add at the end of the file:

```go
// workflowListResponse is the JSON structure returned by the workflow list tool.
type workflowListResponse struct {
	Name        string               `json:"name"`
	Statuses    []string             `json:"statuses"`
	Transitions []transitionResponse `json:"transitions"`
	Projects    []string             `json:"projects"`
}

// handleWorkflowList handles the tusk_workflow_list tool.
func (s *Server) handleWorkflowList(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	workflows, err := s.workflowSvc.List(ctx)
	if err != nil {
		return nil, err
	}

	results := make([]workflowListResponse, len(workflows))
	for i, wf := range workflows {
		// Get projects that reference this workflow
		_, projectIDs, err := s.workflowSvc.GetWorkflowWithProjects(ctx, wf.Name)
		if err != nil {
			return nil, err
		}

		transitions := make([]transitionResponse, len(wf.Transitions))
		for j, t := range wf.Transitions {
			transitions[j] = transitionResponse{From: t.FromStatus, To: t.ToStatus}
		}

		if projectIDs == nil {
			projectIDs = []string{}
		}
		results[i] = workflowListResponse{
			Name:        wf.Name,
			Statuses:    wf.Statuses,
			Transitions: transitions,
			Projects:    projectIDs,
		}
	}

	return toolResultJSON(results)
}
```

Note: `transitionResponse` is already defined in `resources.go` (lines 111-114). Reuse it here.

- [ ] **Step 3: Verify compilation**

Run: `cd /Users/germanamz/projects/tusk && go build ./internal/mcp/...`

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/mcp/server.go internal/mcp/tools.go
git commit -m "feat(mcp): add tusk_workflow_list tool

Lists all workflows with statuses, transitions, and referencing
projects. Registered in the 'workflow' tool group."
```

---

### Task 3: Add CLI workflow commands

Add `tusk workflow list` and `tusk workflow info <name>` commands.

**Files:**
- Create: `internal/tui/workflow.go`
- Modify: `internal/tui/render.go` (add workflow rendering functions)
- Modify: `internal/tui/app.go:142` (register workflow command)

- [ ] **Step 1: Add workflow rendering to `internal/tui/render.go`**

Add at the end of the file:

```go
// workflowJSON is the JSON serialization format for a workflow.
type workflowJSON struct {
	Name        string               `json:"name"`
	Statuses    []string             `json:"statuses"`
	Transitions []workflowTransJSON  `json:"transitions"`
}

// workflowTransJSON is the JSON serialization format for a workflow transition.
type workflowTransJSON struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// workflowInfoJSON extends workflowJSON with referencing projects.
type workflowInfoJSON struct {
	workflowJSON
	Projects []string `json:"projects"`
}

func toWorkflowJSON(wf *domain.Workflow) workflowJSON {
	transitions := make([]workflowTransJSON, len(wf.Transitions))
	for i, t := range wf.Transitions {
		transitions[i] = workflowTransJSON{From: t.FromStatus, To: t.ToStatus}
	}
	return workflowJSON{
		Name:        wf.Name,
		Statuses:    wf.Statuses,
		Transitions: transitions,
	}
}

// renderWorkflowList writes a list of workflows to w.
func renderWorkflowList(w io.Writer, workflows []*domain.Workflow, format string) error {
	if format == "json" {
		items := make([]workflowJSON, len(workflows))
		for i, wf := range workflows {
			items[i] = toWorkflowJSON(wf)
		}
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(items)
	}

	if len(workflows) == 0 {
		return nil
	}

	if _, err := fmt.Fprintf(w, "%-20s %s\n", "NAME", "STATUSES"); err != nil {
		return err
	}
	for _, wf := range workflows {
		if _, err := fmt.Fprintf(w, "%-20s %s\n",
			wf.Name,
			strings.Join(wf.Statuses, ", "),
		); err != nil {
			return err
		}
	}
	return nil
}

// renderWorkflowInfo writes a detailed workflow view to w.
func renderWorkflowInfo(w io.Writer, wf *domain.Workflow, projectIDs []string, format string) error {
	if format == "json" {
		info := workflowInfoJSON{
			workflowJSON: toWorkflowJSON(wf),
			Projects:     projectIDs,
		}
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(info)
	}

	if _, err := fmt.Fprintf(w, "%-13s %s\n", "Workflow:", wf.Name); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "%-13s %s\n", "Statuses:", strings.Join(wf.Statuses, ", ")); err != nil {
		return err
	}

	if len(wf.Transitions) > 0 {
		if _, err := fmt.Fprintln(w, "Transitions:"); err != nil {
			return err
		}
		// Find max from-status length for alignment
		maxLen := 0
		for _, t := range wf.Transitions {
			if len(t.FromStatus) > maxLen {
				maxLen = len(t.FromStatus)
			}
		}
		fmtStr := fmt.Sprintf("  %%-%ds -> %%s\n", maxLen)
		for _, t := range wf.Transitions {
			if _, err := fmt.Fprintf(w, fmtStr, t.FromStatus, t.ToStatus); err != nil {
				return err
			}
		}
	}

	if len(projectIDs) > 0 {
		if _, err := fmt.Fprintf(w, "%-13s %s\n", "Projects:", strings.Join(projectIDs, ", ")); err != nil {
			return err
		}
	}

	return nil
}
```

- [ ] **Step 2: Create `internal/tui/workflow.go`**

```go
package tui

import (
	"fmt"

	"github.com/spf13/cobra"
)

// buildWorkflowCmd creates the `tusk workflow` command group.
// Workflows are config-driven — only list and info are available.
func (a *App) buildWorkflowCmd() *cobra.Command {
	workflowCmd := &cobra.Command{
		Use:   "workflow",
		Short: "Manage workflows",
	}

	workflowCmd.AddCommand(
		&cobra.Command{
			Use:   "list",
			Short: "List all workflows",
			Args:  cobra.NoArgs,
			RunE:  a.runWorkflowList,
		},
		&cobra.Command{
			Use:   "info <name>",
			Short: "Show workflow details",
			Args:  cobra.ExactArgs(1),
			RunE:  a.runWorkflowInfo,
		},
	)

	return workflowCmd
}

func (a *App) runWorkflowList(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	workflows, err := a.workflowSvc.List(ctx)
	if err != nil {
		return err
	}
	return renderWorkflowList(cmd.OutOrStdout(), workflows, a.format)
}

func (a *App) runWorkflowInfo(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	name := args[0]

	wf, projectIDs, err := a.workflowSvc.GetWorkflowWithProjects(ctx, name)
	if err != nil {
		return fmt.Errorf("workflow %q not found", name)
	}
	return renderWorkflowInfo(cmd.OutOrStdout(), wf, projectIDs, a.format)
}
```

- [ ] **Step 3: Register workflow command in `internal/tui/app.go`**

After line 142 (`a.root.AddCommand(a.buildProjectCmd())`), add:

```go
	a.root.AddCommand(a.buildWorkflowCmd())
```

- [ ] **Step 4: Verify compilation and run binary**

Run:

```bash
cd /Users/germanamz/projects/tusk && go build -o bin/tusk ./cmd/tusk/
bin/tusk workflow list
bin/tusk workflow info kanban
```

Expected: `workflow list` shows the kanban workflow. `workflow info kanban` shows detailed view with statuses, transitions, and projects.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/workflow.go internal/tui/render.go internal/tui/app.go
git commit -m "feat(cli): add tusk workflow list and workflow info commands

Config-driven workflow commands showing statuses, transitions, and
referencing projects. Supports text and JSON output formats."
```

---

### Task 4: E2E tests and ROADMAP update

Add E2E tests for the new CLI commands and update ROADMAP to mark stories complete.

**Files:**
- Create: `tests/e2e/workflow_test.go`
- Modify: `ROADMAP.md` (mark Declarative Workflows stories as done)

- [ ] **Step 1: Create `tests/e2e/workflow_test.go`**

```go
package e2e

import (
	"testing"
)

func TestWorkflowCommands(t *testing.T) {
	scenarios := []Scenario{
		{
			Name: "workflow_list_default",
			Steps: []Step{
				{
					Args: []string{"workflow", "list"},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "kanban")
						assertContains(t, output, "pending")
					},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						arr := jsonArray(t, parsed)
						if len(arr) < 1 {
							t.Fatal("expected at least 1 workflow")
						}
						found := false
						for _, item := range arr {
							m := item.(map[string]any)
							if m["name"] == "kanban" {
								found = true
								statuses, ok := m["statuses"].([]any)
								if !ok {
									t.Fatal("expected statuses array")
								}
								if len(statuses) != 4 {
									t.Fatalf("expected 4 statuses, got %d", len(statuses))
								}
								transitions, ok := m["transitions"].([]any)
								if !ok {
									t.Fatal("expected transitions array")
								}
								if len(transitions) != 6 {
									t.Fatalf("expected 6 transitions, got %d", len(transitions))
								}
								break
							}
						}
						if !found {
							t.Fatal("expected kanban workflow in list")
						}
					},
				},
			},
		},
		{
			Name: "workflow_info_kanban",
			Steps: []Step{
				{
					Args: []string{"workflow", "info", "kanban"},
					AssertText: func(t *testing.T, output string) {
						t.Helper()
						assertContains(t, output, "kanban")
						assertContains(t, output, "pending")
						assertContains(t, output, "active")
						assertContains(t, output, "completed")
						assertContains(t, output, "deleted")
						assertContains(t, output, "->")
						assertContains(t, output, "default")
					},
					AssertJSON: func(t *testing.T, parsed any) {
						t.Helper()
						m := parsed.(map[string]any)
						if m["name"] != "kanban" {
							t.Fatalf("expected name 'kanban', got %v", m["name"])
						}
						statuses, ok := m["statuses"].([]any)
						if !ok || len(statuses) != 4 {
							t.Fatalf("expected 4 statuses, got %v", m["statuses"])
						}
						transitions, ok := m["transitions"].([]any)
						if !ok || len(transitions) != 6 {
							t.Fatalf("expected 6 transitions, got %v", m["transitions"])
						}
						projects, ok := m["projects"].([]any)
						if !ok || len(projects) < 1 {
							t.Fatalf("expected at least 1 project, got %v", m["projects"])
						}
					},
				},
			},
		},
		{
			Name: "workflow_info_nonexistent",
			Steps: []Step{
				{
					Args:    []string{"workflow", "info", "nonexistent"},
					WantErr: true,
					Assert: func(t *testing.T, r Result) {
						t.Helper()
						combined := r.Stdout + r.Stderr
						assertContains(t, combined, "not found")
					},
				},
			},
		},
	}

	runScenarios(t, binPath, scenarios)
}
```

- [ ] **Step 2: Build and run E2E tests**

Run:

```bash
cd /Users/germanamz/projects/tusk && make build && go test ./tests/e2e/ -run TestWorkflowCommands -v
```

Expected: all scenarios PASS across all 4 combinations (flag/env x text/json).

- [ ] **Step 3: Run the full test suite**

Run: `cd /Users/germanamz/projects/tusk && make test`

Expected: all tests PASS (unit + e2e).

- [ ] **Step 4: Update ROADMAP.md**

In `ROADMAP.md`, under `## v0.4 — Configuration & Customization`, find the `### Initiative: Declarative Workflows` section (lines 239-266) and mark all stories and tasks as complete by changing `- [ ]` to `- [x]`.

- [ ] **Step 5: Commit**

```bash
git add tests/e2e/workflow_test.go ROADMAP.md
git commit -m "test(e2e): add workflow CLI tests and mark initiative complete

Add E2E tests for tusk workflow list and tusk workflow info.
Mark Declarative Workflows initiative complete in ROADMAP.md."
```

- [ ] **Step 6: Final verification**

Run:

```bash
cd /Users/germanamz/projects/tusk && make test-race
```

Expected: all tests PASS with race detector enabled.
