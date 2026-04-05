# Declarative Workflows — Phase 4: CLI Commands, MCP Tool & E2E Tests

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `tusk workflow list` and `tusk workflow info <name>` CLI commands, add `tusk_workflow_list` MCP tool, update MCP config validation, and add E2E tests.

**Architecture:** New CLI commands delegate to `WorkflowService`. MCP tool uses the existing `addTool` pattern. E2E tests use the existing harness with `runScenarios`.

**Tech Stack:** Go, Cobra (CLI), mcp-go (MCP)

**Prerequisites:** Phase 3 must be complete (final WorkflowService API in place, all tests green).

---

### Task 1: Add `tusk_workflow_list` MCP tool

Register the new tool and implement its handler. Update MCP config validation to recognize the new tool and group.

**Files:**
- Modify: `internal/mcp/server.go` (tool registration + config validation)
- Modify: `internal/mcp/tools.go` (handler + response type)

- [ ] **Step 1: Add tool registration in `internal/mcp/server.go`**

Add at the end of `registerTools`, just before the closing brace (after the `tusk_task_tree` block):

```go
	s.addTool("workflow",
		mcp.NewTool("tusk_workflow_list",
			mcp.WithDescription("List all workflows with their statuses, transitions, and referencing projects"),
		),
		s.handleWorkflowList,
	)
```

- [ ] **Step 2: Update `validateConfig` in `internal/mcp/server.go`**

Add `"tusk_workflow_list": true` to `validToolNames` and `"workflow": true` to `validToolGroups`:

```go
	validToolNames := map[string]bool{
		...
		"tusk_project_list":    true,
		"tusk_workflow_list":   true,
	}
	validToolGroups := map[string]bool{
		"task": true, "relation": true, "project": true, "workflow": true,
	}
```

- [ ] **Step 3: Add handler and response type in `internal/mcp/tools.go`**

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

Note: `transitionResponse` is already defined in `resources.go`.

- [ ] **Step 4: Verify compilation**

Run: `cd /Users/germanamz/projects/tusk && go build ./internal/mcp/...`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/mcp/server.go internal/mcp/tools.go
git commit -m "feat(mcp): add tusk_workflow_list tool

Lists all workflows with statuses, transitions, and referencing
projects. Registered in the 'workflow' tool group."
```

---

### Task 2: Add CLI workflow commands

Add `tusk workflow list` and `tusk workflow info <name>` commands with text and JSON output.

**Files:**
- Create: `internal/tui/workflow.go`
- Modify: `internal/tui/render.go` (add workflow rendering)
- Modify: `internal/tui/app.go` (register command)

- [ ] **Step 1: Add workflow rendering to `internal/tui/render.go`**

Add at the end of the file:

```go
// workflowJSON is the JSON serialization format for a workflow.
type workflowJSON struct {
	Name        string              `json:"name"`
	Statuses    []string            `json:"statuses"`
	Transitions []workflowTransJSON `json:"transitions"`
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
		if projectIDs == nil {
			projectIDs = []string{}
		}
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

- [ ] **Step 3: Register command in `internal/tui/app.go`**

After line 142 (`a.root.AddCommand(a.buildProjectCmd())`), add:

```go
	a.root.AddCommand(a.buildWorkflowCmd())
```

- [ ] **Step 4: Build and smoke test**

Run:

```bash
cd /Users/germanamz/projects/tusk && go build -o bin/tusk ./cmd/tusk/
bin/tusk workflow list
bin/tusk workflow info kanban
```

Expected: both commands produce output with kanban workflow data.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/workflow.go internal/tui/render.go internal/tui/app.go
git commit -m "feat(cli): add tusk workflow list and workflow info commands

Config-driven workflow commands showing statuses, transitions, and
referencing projects. Supports text and JSON output formats."
```

---

### Task 3: E2E tests

Add end-to-end tests for the new CLI commands using the existing harness.

**Files:**
- Create: `tests/e2e/workflow_test.go`

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
								if !ok || len(statuses) != 4 {
									t.Fatalf("expected 4 statuses, got %v", m["statuses"])
								}
								transitions, ok := m["transitions"].([]any)
								if !ok || len(transitions) != 6 {
									t.Fatalf("expected 6 transitions, got %v", m["transitions"])
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

- [ ] **Step 3: Run full test suite with race detector**

Run: `cd /Users/germanamz/projects/tusk && make test-race`

Expected: all PASS.

- [ ] **Step 4: Commit**

```bash
git add tests/e2e/workflow_test.go
git commit -m "test(e2e): add workflow CLI end-to-end tests

Cover workflow list, workflow info, and nonexistent workflow error
across all 4 harness combinations."
```

---

### Task 4: Update ROADMAP

Mark the Declarative Workflows initiative as complete.

**Files:**
- Modify: `ROADMAP.md`

- [ ] **Step 1: Mark stories complete in `ROADMAP.md`**

In the `### Initiative: Declarative Workflows` section (lines 239-266), change all `- [ ]` to `- [x]` for the four stories and all their sub-tasks.

- [ ] **Step 2: Commit**

```bash
git add ROADMAP.md
git commit -m "docs: mark Declarative Workflows initiative complete in ROADMAP"
```
