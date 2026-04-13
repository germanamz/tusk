# Phase 4 — `tusk_project_create` / `_modify` / `_delete` MCP Tools

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let MCP clients create, modify, and delete projects with the same
validation and task-reference guards as `tusk project create|modify|delete`,
using structured JSON inputs. Each successful mutation writes to the active
config file and calls `s.reloadConfig(ctx)` so the MCP server reflects the
change immediately.

**Architecture:** Three handlers in a new file
`internal/mcp/project_handlers.go` translate structured MCP inputs into
`config.ProjectConfig` and `config.ProjectMutation` values and call
`config.CreateProject`, `config.ModifyProject`, `config.DeleteProject`. The
delete handler reuses the existing `filter.ParseExpr + taskSvc.List`
pattern from `internal/tui/project.go:114-128` to build a `TaskRefChecker`,
so the built-in-`default` / referenced-by-tasks guards from the CLI carry
over unchanged.

**Tech Stack:** Same as prior phases — no new third-party dependencies.

**Prerequisites:** Phases 1, 2, and 3 must be complete.

## Inherits From

- Phases 1–3.
- Pre-existing helpers in `config/project.go`:
  - `CreateProject(path, name, ProjectConfig) error`
  - `ModifyProject(path, name, ProjectMutation) error`
  - `DeleteProject(path, name, TaskRefChecker, force bool) error`
  - `TaskRefChecker` type alias: `func(projectName string) (int, error)`
- `UrgencyFieldPtr(*ProjectUrgencyConfig, key) **float64` — exported
  precisely so non-`config` callers (previously only `internal/tui`) can
  set per-project urgency weights.
- The CLI-side task-reference counter at `internal/tui/project.go:114-128`
  (`countTasksForProject`). This phase adds an equivalent helper inside
  the MCP package instead of reaching across package boundaries.
- The `tusk_project_list` tool already exists and uses the same
  `projectSvc` / `projectRepo`; `reloadConfig` keeps it consistent.

## User-Visible Behavior (must still work)

- Everything from phases 1–3.
- `tusk project create|modify|delete` CLI commands continue to function.
- `tusk_project_list` MCP tool continues to work and, after a successful
  `tusk_project_create`, surfaces the new project without a restart.
- Deleting the built-in `default` project via MCP without `force=true`
  fails with the same error as the CLI.
- Deleting a project that still has task references fails unless
  `force=true`.
- Setting per-project urgency weight overrides via `tusk_project_modify`
  updates the config file and the running urgency engine's per-project
  overrides (those are read from `projectRepo` during scoring).

## Tasks

### Task 1: Register three tool shells and a task-reference counter

**Files:**
- Modify: `internal/mcp/server.go`
- Create: `internal/mcp/project_handlers.go`

- [ ] **Step 1: Extend the `validToolNames` allow-list**

```go
validToolNames := map[string]bool{
	// ... prior entries ...
	"tusk_project_create": true,
	"tusk_project_modify": true,
	"tusk_project_delete": true,
}
```

The `project` group is already registered.

- [ ] **Step 2: Register the three tool definitions**

After the existing `tusk_project_list` registration in `registerTools`, add:

```go
s.addTool("project",
	mcp.NewTool("tusk_project_create",
		mcp.WithDescription("Create a new project bound to a workflow and write it to the active config file."),
		mcp.WithString("name",
			mcp.Required(),
			mcp.Description("Project name (unique within the config file)"),
		),
		mcp.WithString("workflow",
			mcp.Required(),
			mcp.Description("Name of an existing workflow"),
		),
		mcp.WithObject("urgency",
			mcp.Description("Per-project urgency weight overrides (e.g. {\"due_weight\": 10.0}). Keys: priority_weight, due_weight, age_weight, active_weight, blocking_weight, blocked_weight, tags_weight, project_weight, annotations_weight, waiting_weight."),
			mcp.AdditionalProperties(map[string]any{"type": "number"}),
		),
		mcp.WithObject("auto_complete",
			mcp.Description("Parent auto-complete config: {trigger_status, target_status}"),
			mcp.AdditionalProperties(map[string]any{"type": "string"}),
		),
		mcp.WithObject("auto_revert",
			mcp.Description("Parent auto-revert config: {trigger_status, target_status}"),
			mcp.AdditionalProperties(map[string]any{"type": "string"}),
		),
	),
	s.handleProjectCreate,
)

s.addTool("project",
	mcp.NewTool("tusk_project_modify",
		mcp.WithDescription("Modify an existing project. Only fields that are present are changed."),
		mcp.WithString("name",
			mcp.Required(),
			mcp.Description("Project name"),
		),
		mcp.WithString("workflow",
			mcp.Description("New workflow to bind"),
		),
		mcp.WithObject("urgency_set",
			mcp.Description("Absolute per-project urgency overrides — keys as in tusk_project_create.urgency"),
			mcp.AdditionalProperties(map[string]any{"type": "number"}),
		),
		mcp.WithObject("urgency_delta",
			mcp.Description("Delta to apply on top of the effective weight (positive or negative). Cannot overlap with urgency_set keys."),
			mcp.AdditionalProperties(map[string]any{"type": "number"}),
		),
		mcp.WithObject("auto_complete",
			mcp.Description("Replace parent auto-complete config"),
			mcp.AdditionalProperties(map[string]any{"type": "string"}),
		),
		mcp.WithObject("auto_revert",
			mcp.Description("Replace parent auto-revert config"),
			mcp.AdditionalProperties(map[string]any{"type": "string"}),
		),
	),
	s.handleProjectModify,
)

s.addTool("project",
	mcp.NewTool("tusk_project_delete",
		mcp.WithDescription("Delete a project from the active config file. Rejects the built-in 'default' project and any project with task references unless force=true."),
		mcp.WithString("name",
			mcp.Required(),
			mcp.Description("Project name"),
		),
		mcp.WithBoolean("force",
			mcp.Description("Bypass the built-in-default and task-reference guards"),
		),
	),
	s.handleProjectDelete,
)
```

- [ ] **Step 3: Add stub handlers and the task-ref counter**

Create `internal/mcp/project_handlers.go` with stub handlers and a shared
helper that mirrors `internal/tui/project.go:114-128`:

```go
package mcp

import (
	"context"
	"fmt"

	"github.com/germanamz/tusk/filter"
	"github.com/mark3labs/mcp-go/mcp"
)

// countTasksForProject returns the number of tasks referencing the given
// project name. Mirrors the CLI-side helper so the MCP project_delete
// handler can supply a TaskRefChecker to config.DeleteProject.
func (s *Server) countTasksForProject(ctx context.Context, projectName string) (int, error) {
	expr, parseErrs := filter.ParseExpr(fmt.Sprintf("project=%s", projectName))
	if len(parseErrs) > 0 {
		return 0, fmt.Errorf("building filter: %s", filter.FormatErrors(parseErrs))
	}
	resolver := s.newResolver(ctx)
	filterExpr, resolveErrs := resolver.ResolveExpr(ctx, expr)
	if len(resolveErrs) > 0 {
		return 0, resolveErrs[0]
	}
	tasks, err := s.taskSvc.List(ctx, filterExpr)
	if err != nil {
		return 0, err
	}
	return len(tasks), nil
}

func (s *Server) handleProjectCreate(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return mcp.NewToolResultError("not implemented"), nil
}

func (s *Server) handleProjectModify(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return mcp.NewToolResultError("not implemented"), nil
}

func (s *Server) handleProjectDelete(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return mcp.NewToolResultError("not implemented"), nil
}
```

- [ ] **Step 4: Build**

Run: `go build ./... && go vet ./internal/mcp`
Expected: no output.

- [ ] **Step 5: Commit**

```bash
git add internal/mcp/server.go internal/mcp/project_handlers.go
git commit -m "feat(mcp): register project create/modify/delete tool shells"
```

### Task 2: Implement `handleProjectCreate`

**Files:**
- Modify: `internal/mcp/project_handlers.go`
- Test: `internal/mcp/project_handlers_test.go` (new file)

- [ ] **Step 1: Write the failing test**

Create `internal/mcp/project_handlers_test.go`:

```go
func TestHandleProjectCreate_Success(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tusk.toml")
	writeMinimalConfig(t, path)
	srv := newTestServer(t, path)

	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{Arguments: map[string]any{
			"name":     "backend",
			"workflow": "kanban",
			"urgency": map[string]any{
				"due_weight":      15.0,
				"blocking_weight": 20.0,
			},
			"auto_complete": map[string]any{
				"trigger_status": "completed",
				"target_status":  "completed",
			},
		}},
	}
	res, err := srv.HandleProjectCreateForTest(context.Background(), req)
	if err != nil {
		t.Fatalf("HandleProjectCreateForTest: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Content[0].(mcp.TextContent).Text)
	}

	loaded, _ := config.LoadFile(path)
	p, ok := loaded.Projects["backend"]
	if !ok {
		t.Fatalf("backend project not persisted")
	}
	if p.Workflow != "kanban" {
		t.Fatalf("workflow: got %q", p.Workflow)
	}
	if p.Settings.Urgency == nil || p.Settings.Urgency.DueWeight == nil || *p.Settings.Urgency.DueWeight != 15.0 {
		t.Fatalf("due_weight override not persisted: %+v", p.Settings.Urgency)
	}
}

func TestHandleProjectCreate_UnknownWorkflow(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tusk.toml")
	writeMinimalConfig(t, path)
	srv := newTestServer(t, path)

	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{Arguments: map[string]any{
			"name":     "frontend",
			"workflow": "ghost",
		}},
	}
	res, _ := srv.HandleProjectCreateForTest(context.Background(), req)
	if !res.IsError {
		t.Fatalf("expected validation error for unknown workflow")
	}
}
```

- [ ] **Step 2: Run to confirm failure**

Run: `go test ./internal/mcp -run TestHandleProjectCreate -v`
Expected: FAIL.

- [ ] **Step 3: Implement the handler**

Replace the stub in `project_handlers.go`:

```go
import (
	"github.com/germanamz/tusk/config"
)

func parseStringMap(raw any) (map[string]string, error) {
	if raw == nil {
		return nil, nil
	}
	m, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("expected object")
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("key %q: expected string value", k)
		}
		out[k] = s
	}
	return out, nil
}

func parseFloatMap(raw any) (map[string]float64, error) {
	if raw == nil {
		return nil, nil
	}
	m, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("expected object")
	}
	out := make(map[string]float64, len(m))
	for k, v := range m {
		f, ok := v.(float64)
		if !ok {
			return nil, fmt.Errorf("key %q: expected number", k)
		}
		out[k] = f
	}
	return out, nil
}

func applyUrgencyWeights(proj *config.ProjectConfig, weights map[string]float64) error {
	if len(weights) == 0 {
		return nil
	}
	if proj.Settings.Urgency == nil {
		proj.Settings.Urgency = &config.ProjectUrgencyConfig{}
	}
	for k, v := range weights {
		fp := config.UrgencyFieldPtr(proj.Settings.Urgency, k)
		if fp == nil {
			return fmt.Errorf("unknown urgency key %q", k)
		}
		val := v
		*fp = &val
	}
	return nil
}

func applyAutoComplete(proj *config.ProjectConfig, raw map[string]string) {
	if len(raw) == 0 {
		return
	}
	proj.Settings.AutoCompleteParent = &config.AutoCompleteParentConfig{
		TriggerStatus: raw["trigger_status"],
		TargetStatus:  raw["target_status"],
	}
}

func applyAutoRevert(proj *config.ProjectConfig, raw map[string]string) {
	if len(raw) == 0 {
		return
	}
	proj.Settings.AutoRevertParent = &config.AutoRevertParentConfig{
		TriggerStatus: raw["trigger_status"],
		TargetStatus:  raw["target_status"],
	}
}

func (s *Server) handleProjectCreate(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	name, err := req.RequireString("name")
	if err != nil {
		return mcp.NewToolResultError("name is required"), nil
	}
	workflow, err := req.RequireString("workflow")
	if err != nil {
		return mcp.NewToolResultError("workflow is required"), nil
	}
	args := req.GetArguments()

	weights, err := parseFloatMap(args["urgency"])
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("urgency: %v", err)), nil
	}
	ac, err := parseStringMap(args["auto_complete"])
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("auto_complete: %v", err)), nil
	}
	ar, err := parseStringMap(args["auto_revert"])
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("auto_revert: %v", err)), nil
	}

	proj := config.ProjectConfig{Workflow: workflow}
	if err := applyUrgencyWeights(&proj, weights); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	applyAutoComplete(&proj, ac)
	applyAutoRevert(&proj, ar)

	path, err := config.ConfigFilePath(s.loadOpts...)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("resolving config file: %v", err)), nil
	}
	if err := config.CreateProject(path, name, proj); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if err := s.reloadConfig(ctx); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("reloading config: %v", err)), nil
	}
	return toolResultJSON(map[string]any{"ok": true, "name": name, "active_file": path})
}

// HandleProjectCreateForTest exposes handleProjectCreate for internal tests.
func (s *Server) HandleProjectCreateForTest(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return s.handleProjectCreate(ctx, req)
}
```

- [ ] **Step 4: Verify the tests pass**

Run: `go test ./internal/mcp -run TestHandleProjectCreate -v -race`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/mcp/project_handlers.go internal/mcp/project_handlers_test.go
git commit -m "feat(mcp): implement tusk_project_create"
```

### Task 3: Implement `handleProjectModify`

**Files:**
- Modify: `internal/mcp/project_handlers.go`
- Modify: `internal/mcp/project_handlers_test.go`

- [ ] **Step 1: Write the failing test**

Append:

```go
func TestHandleProjectModify_SetAndDelta(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tusk.toml")
	writeMinimalConfig(t, path)
	if err := config.CreateProject(path, "backend", config.ProjectConfig{Workflow: "kanban"}); err != nil {
		t.Fatalf("seed project: %v", err)
	}

	srv := newTestServer(t, path)

	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{Arguments: map[string]any{
			"name": "backend",
			"urgency_set": map[string]any{
				"blocking_weight": 25.0,
			},
			"urgency_delta": map[string]any{
				"due_weight": 3.0,
			},
		}},
	}
	res, err := srv.HandleProjectModifyForTest(context.Background(), req)
	if err != nil {
		t.Fatalf("HandleProjectModifyForTest: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Content[0].(mcp.TextContent).Text)
	}

	loaded, _ := config.LoadFile(path)
	p := loaded.Projects["backend"]
	if p.Settings.Urgency == nil || p.Settings.Urgency.BlockingWeight == nil || *p.Settings.Urgency.BlockingWeight != 25.0 {
		t.Fatalf("blocking_weight set failed: %+v", p.Settings.Urgency)
	}
	if p.Settings.Urgency.DueWeight == nil || *p.Settings.Urgency.DueWeight == 0 {
		t.Fatalf("due_weight delta failed: %+v", p.Settings.Urgency)
	}
}

func TestHandleProjectModify_SetDeltaConflict(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tusk.toml")
	writeMinimalConfig(t, path)
	if err := config.CreateProject(path, "backend", config.ProjectConfig{Workflow: "kanban"}); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	srv := newTestServer(t, path)

	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{Arguments: map[string]any{
			"name": "backend",
			"urgency_set":   map[string]any{"due_weight": 10.0},
			"urgency_delta": map[string]any{"due_weight": 2.0},
		}},
	}
	res, _ := srv.HandleProjectModifyForTest(context.Background(), req)
	if !res.IsError {
		t.Fatalf("expected conflict error")
	}
}
```

- [ ] **Step 2: Run to confirm failure**

Run: `go test ./internal/mcp -run TestHandleProjectModify -v`
Expected: FAIL.

- [ ] **Step 3: Implement the handler**

Append to `project_handlers.go`:

```go
func (s *Server) handleProjectModify(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	name, err := req.RequireString("name")
	if err != nil {
		return mcp.NewToolResultError("name is required"), nil
	}
	args := req.GetArguments()

	mut := config.ProjectMutation{
		UrgencySet:   map[string]float64{},
		UrgencyDelta: map[string]float64{},
	}

	if wf, ok := args["workflow"].(string); ok && wf != "" {
		w := wf
		mut.Workflow = &w
	}

	setWeights, err := parseFloatMap(args["urgency_set"])
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("urgency_set: %v", err)), nil
	}
	for k, v := range setWeights {
		mut.UrgencySet[k] = v
	}

	deltaWeights, err := parseFloatMap(args["urgency_delta"])
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("urgency_delta: %v", err)), nil
	}
	for k, v := range deltaWeights {
		mut.UrgencyDelta[k] = v
	}

	ac, err := parseStringMap(args["auto_complete"])
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("auto_complete: %v", err)), nil
	}
	if len(ac) > 0 {
		mut.AutoCompleteSet = &config.AutoCompleteParentConfig{
			TriggerStatus: ac["trigger_status"],
			TargetStatus:  ac["target_status"],
		}
	}

	ar, err := parseStringMap(args["auto_revert"])
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("auto_revert: %v", err)), nil
	}
	if len(ar) > 0 {
		mut.AutoRevertSet = &config.AutoRevertParentConfig{
			TriggerStatus: ar["trigger_status"],
			TargetStatus:  ar["target_status"],
		}
	}

	path, err := config.ConfigFilePath(s.loadOpts...)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("resolving config file: %v", err)), nil
	}
	if err := config.ModifyProject(path, name, mut); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if err := s.reloadConfig(ctx); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("reloading config: %v", err)), nil
	}
	return toolResultJSON(map[string]any{"ok": true, "name": name, "active_file": path})
}

// HandleProjectModifyForTest exposes handleProjectModify for internal tests.
func (s *Server) HandleProjectModifyForTest(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return s.handleProjectModify(ctx, req)
}
```

- [ ] **Step 4: Verify the tests pass**

Run: `go test ./internal/mcp -run TestHandleProjectModify -v -race`
Expected: PASS on both tests.

- [ ] **Step 5: Commit**

```bash
git add internal/mcp/project_handlers.go internal/mcp/project_handlers_test.go
git commit -m "feat(mcp): implement tusk_project_modify"
```

### Task 4: Implement `handleProjectDelete`

**Files:**
- Modify: `internal/mcp/project_handlers.go`
- Modify: `internal/mcp/project_handlers_test.go`

- [ ] **Step 1: Write the failing test**

Append:

```go
func TestHandleProjectDelete_Success(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tusk.toml")
	writeMinimalConfig(t, path)
	if err := config.CreateProject(path, "backend", config.ProjectConfig{Workflow: "kanban"}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	srv := newTestServer(t, path)

	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{Arguments: map[string]any{"name": "backend"}},
	}
	res, err := srv.HandleProjectDeleteForTest(context.Background(), req)
	if err != nil {
		t.Fatalf("HandleProjectDeleteForTest: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Content[0].(mcp.TextContent).Text)
	}
	loaded, _ := config.LoadFile(path)
	if _, ok := loaded.Projects["backend"]; ok {
		t.Fatalf("backend still present after delete")
	}
}

func TestHandleProjectDelete_DefaultGuarded(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tusk.toml")
	writeMinimalConfig(t, path)
	srv := newTestServer(t, path)

	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{Arguments: map[string]any{"name": "default"}},
	}
	res, _ := srv.HandleProjectDeleteForTest(context.Background(), req)
	if !res.IsError {
		t.Fatalf("expected guard error for built-in default project")
	}
}
```

Note: the task-reference guard requires a functioning `taskSvc`, which the
bare `newTestServer` helper from phase 2 does not provide. For the
reference-by-tasks scenario, extend `newTestServer` in a follow-up task or
add a comment in this test file noting it is covered end-to-end by the
existing CLI e2e suite. The two tests above exercise the code path that
actually differs from the CLI (MCP argument parsing + reload).

- [ ] **Step 2: Run to confirm failure**

Run: `go test ./internal/mcp -run TestHandleProjectDelete -v`
Expected: FAIL.

- [ ] **Step 3: Implement the handler**

Append to `project_handlers.go`:

```go
func (s *Server) handleProjectDelete(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	name, err := req.RequireString("name")
	if err != nil {
		return mcp.NewToolResultError("name is required"), nil
	}
	force, _ := req.GetArguments()["force"].(bool)

	path, err := config.ConfigFilePath(s.loadOpts...)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("resolving config file: %v", err)), nil
	}

	var checker config.TaskRefChecker
	if s.taskSvc != nil {
		checker = func(projectName string) (int, error) {
			return s.countTasksForProject(ctx, projectName)
		}
	}

	if err := config.DeleteProject(path, name, checker, force); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if err := s.reloadConfig(ctx); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("reloading config: %v", err)), nil
	}
	return toolResultJSON(map[string]any{"ok": true, "name": name, "active_file": path})
}

// HandleProjectDeleteForTest exposes handleProjectDelete for internal tests.
func (s *Server) HandleProjectDeleteForTest(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return s.handleProjectDelete(ctx, req)
}
```

- [ ] **Step 4: Verify the tests pass**

Run: `go test ./internal/mcp -run TestHandleProjectDelete -v -race`
Expected: PASS on both tests.

- [ ] **Step 5: Commit**

```bash
git add internal/mcp/project_handlers.go internal/mcp/project_handlers_test.go
git commit -m "feat(mcp): implement tusk_project_delete"
```

### Task 5: Update `serverInstructions` and tick the ROADMAP story

**Files:**
- Modify: `internal/mcp/server.go`
- Modify: `ROADMAP.md`

- [ ] **Step 1: Extend the instructions string**

```go
const serverInstructions = `Tusk is a task management system. You can create, list, modify, and transition tasks through workflow statuses. Tasks support parent-child hierarchy, typed relations (blocks, relates_to, duplicates), tags, annotations, and projects. All mutation tools require a version parameter for optimistic locking — fetch the task first to get the current version. You can also inspect the active configuration via tusk_config_show and modify scalar config values via tusk_config_set (storage.* keys are read-only over MCP). Workflows can be created, modified, and deleted via tusk_workflow_create, tusk_workflow_modify, and tusk_workflow_delete using structured JSON inputs. Projects can be created, modified, and deleted via tusk_project_create, tusk_project_modify, and tusk_project_delete — deletion honors the built-in-default and referencing-tasks guards (pass force=true to bypass).`
```

- [ ] **Step 2: Mark the ROADMAP story complete**

Edit `ROADMAP.md`, find the `Initiative: MCP Config Tools` section, and
tick every checkbox:

```markdown
### Initiative: MCP Config Tools

> Expose configuration management to AI agents via MCP tools.

- [x] **Story: Config MCP tools**
  - [x] `tusk_config_show` — read effective configuration
  - [x] `tusk_config_set` — set a config value
  - [x] `tusk_workflow_create` / `tusk_workflow_modify` / `tusk_workflow_delete` — workflow management
  - [x] `tusk_project_create` / `tusk_project_modify` / `tusk_project_delete` — project management
```

- [ ] **Step 3: Run the full test suite**

Run:

```bash
go test ./... -race
go vet ./...
```

Expected: all PASS, no vet warnings.

- [ ] **Step 4: Commit**

```bash
git add internal/mcp/server.go ROADMAP.md
git commit -m "docs(mcp): advertise project tools and tick MCP Config Tools story"
```

## Changes Introduced

- **New files:**
  - `internal/mcp/project_handlers.go` — three handlers, shared helpers
    (`parseStringMap`, `parseFloatMap`, `applyUrgencyWeights`,
    `applyAutoComplete`, `applyAutoRevert`), `countTasksForProject`, and
    `*ForTest` shims.
  - `internal/mcp/project_handlers_test.go` — create (success + unknown
    workflow), modify (set/delta + conflict), delete (success + default
    guard).

- **Modified files:**
  - `internal/mcp/server.go` — `validateConfig` allow-list extended with
    `tusk_project_create/modify/delete`; `registerTools` wires the three
    new tools; `serverInstructions` updated.
  - `ROADMAP.md` — MCP Config Tools initiative ticked.

- **Modified interfaces:** none.

- **New environment variables / schema migrations / dependencies:** none.

- **User-visible behavior after this phase:**
  - MCP clients can manage projects end-to-end, with the same guards as
    the CLI. Hot-reload ensures `tusk_project_list`, task creation, and
    urgency scoring immediately see the new or updated projects.
  - `tusk project create|modify|delete` CLI behavior unchanged.
  - The `MCP Config Tools` roadmap initiative is fully delivered.

- **Bridge code:** none introduced; none outstanding. Phase 1's
  `reloadConfig` helper is now called from all five mutating MCP tools
  (`tusk_config_set`, `tusk_workflow_create/modify/delete` from phase 3,
  plus the three tools added in this phase).
