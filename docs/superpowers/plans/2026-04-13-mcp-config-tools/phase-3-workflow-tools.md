# Phase 3 — `tusk_workflow_create` / `_modify` / `_delete` MCP Tools

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let MCP clients create, modify, and delete workflows with the same
validation semantics as `tusk workflow create|modify|delete`, using structured
JSON inputs instead of the inline CLI DSL. Every successful mutation writes
to the active config file and calls `s.reloadConfig(ctx)` so the running
server immediately reflects the new workflow definitions.

**Architecture:** Three handlers in a new file
`internal/mcp/workflow_handlers.go` translate structured MCP inputs into the
existing `config.WorkflowConfig` / `config.WorkflowMutation` types and call
`config.CreateWorkflow`, `config.ModifyWorkflow`, `config.DeleteWorkflow`.
No parser reuse from `internal/tui` — MCP input shape is JSON objects and
arrays, not `status=pending(initial)` DSL fragments.

**Tech Stack:** `github.com/mark3labs/mcp-go/mcp` (already in use), existing
`config` package helpers, no new third-party dependencies.

**Prerequisites:** **Phase 1** and **Phase 2** must be complete.
Specifically this phase relies on:

- `mcp.Server.loadOpts` and `Server.reloadConfig(ctx)` (from phase 1).
- The `config` tool group allow-list entry in
  `validateConfig` (added in phase 2 — the new workflow tools live in the
  existing `workflow` group, not the `config` group, but the `validateConfig`
  pattern from phase 2 is the reference).

## Inherits From

- Phases 1 and 2.
- Pre-existing helpers in `config/workflow.go`:
  - `CreateWorkflow(path, name, WorkflowConfig) error`
  - `ModifyWorkflow(path, name, WorkflowMutation) error`
  - `DeleteWorkflow(path, name) error`
- `validToolNames` / `validToolGroups` maps in `internal/mcp/server.go`; the
  `workflow` group already exists (used by `tusk_workflow_list`).
- `ConfigFilePath(opts...)` for resolving the write target.
- `serverInstructions` string (phase 2 already extended it once).

## User-Visible Behavior (must still work)

- All behavior from phases 1 and 2.
- `tusk workflow create|modify|delete` CLI commands continue to function.
- `tusk_workflow_list` MCP tool continues to return the full list, and after
  a successful `tusk_workflow_create`, a subsequent call to
  `tusk_workflow_list` on the same connection includes the new workflow —
  without a process restart.
- Attempting to delete a workflow referenced by a project fails with the
  same error text `config.DeleteWorkflow` already produces.
- Validation errors (missing `initial` role, missing transition
  `initial→start`, etc.) are surfaced to the MCP caller with the same
  `config.Validate` messages the CLI emits.

## Tasks

### Task 1: Extend the tool allow-list and register three tool shells

**Files:**
- Modify: `internal/mcp/server.go`

- [ ] **Step 1: Add the new tool names to `validateConfig`**

Edit the `validToolNames` map inside `validateConfig`:

```go
validToolNames := map[string]bool{
	// ... existing entries from phases 0–2 ...
	"tusk_workflow_create": true,
	"tusk_workflow_modify": true,
	"tusk_workflow_delete": true,
}
```

The `workflow` group is already in `validToolGroups`; no change there.

- [ ] **Step 2: Register the three tools inside `registerTools`**

After the existing `tusk_workflow_list` registration, add:

```go
s.addTool("workflow",
	mcp.NewTool("tusk_workflow_create",
		mcp.WithDescription("Create a new workflow and write it to the active config file. Fails if the name already exists."),
		mcp.WithString("name",
			mcp.Required(),
			mcp.Description("Workflow name (must be unique within the config file)"),
		),
		mcp.WithArray("statuses",
			mcp.Required(),
			mcp.Description("Ordered list of statuses. Each item is {name: string, roles: string[]}. Roles: initial, start, terminal, done, delete, highlight, dim."),
			mcp.WithObjectItems(map[string]any{
				"name":  map[string]any{"type": "string"},
				"roles": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			}),
		),
		mcp.WithArray("transitions",
			mcp.Required(),
			mcp.Description("Allowed transitions. Each item is {from: string, to: string}."),
			mcp.WithObjectItems(map[string]any{
				"from": map[string]any{"type": "string"},
				"to":   map[string]any{"type": "string"},
			}),
		),
	),
	s.handleWorkflowCreate,
)

s.addTool("workflow",
	mcp.NewTool("tusk_workflow_modify",
		mcp.WithDescription("Modify an existing workflow: add, remove, or update statuses and transitions."),
		mcp.WithString("name",
			mcp.Required(),
			mcp.Description("Workflow name"),
		),
		mcp.WithArray("add_statuses",
			mcp.Description("Statuses to add (must not already exist). Items: {name, roles[]}."),
			mcp.WithObjectItems(map[string]any{
				"name":  map[string]any{"type": "string"},
				"roles": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			}),
		),
		mcp.WithArray("set_statuses",
			mcp.Description("Statuses to update in place (replaces roles). Items: {name, roles[]}."),
			mcp.WithObjectItems(map[string]any{
				"name":  map[string]any{"type": "string"},
				"roles": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			}),
		),
		mcp.WithArray("remove_statuses",
			mcp.Description("Status names to remove. Any transitions touching these are removed too."),
			mcp.WithStringItems(),
		),
		mcp.WithArray("add_transitions",
			mcp.Description("Transitions to add. Items: {from, to}."),
			mcp.WithObjectItems(map[string]any{
				"from": map[string]any{"type": "string"},
				"to":   map[string]any{"type": "string"},
			}),
		),
		mcp.WithArray("remove_transitions",
			mcp.Description("Transitions to remove. Items: {from, to}."),
			mcp.WithObjectItems(map[string]any{
				"from": map[string]any{"type": "string"},
				"to":   map[string]any{"type": "string"},
			}),
		),
	),
	s.handleWorkflowModify,
)

s.addTool("workflow",
	mcp.NewTool("tusk_workflow_delete",
		mcp.WithDescription("Delete a workflow from the active config file. Fails if any project references it."),
		mcp.WithString("name",
			mcp.Required(),
			mcp.Description("Workflow name"),
		),
	),
	s.handleWorkflowDelete,
)
```

Add stub handlers in a new file `internal/mcp/workflow_handlers.go` so the
package still compiles:

```go
package mcp

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
)

func (s *Server) handleWorkflowCreate(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return mcp.NewToolResultError("not implemented"), nil
}

func (s *Server) handleWorkflowModify(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return mcp.NewToolResultError("not implemented"), nil
}

func (s *Server) handleWorkflowDelete(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return mcp.NewToolResultError("not implemented"), nil
}
```

- [ ] **Step 3: Build**

Run: `go build ./... && go vet ./internal/mcp`
Expected: no output.

- [ ] **Step 4: Commit**

```bash
git add internal/mcp/server.go internal/mcp/workflow_handlers.go
git commit -m "feat(mcp): register workflow create/modify/delete tool shells"
```

### Task 2: Implement `handleWorkflowCreate`

**Files:**
- Modify: `internal/mcp/workflow_handlers.go`
- Test: `internal/mcp/workflow_handlers_test.go` (new file)

- [ ] **Step 1: Write the failing tests**

Create `internal/mcp/workflow_handlers_test.go`. Reuse the `newTestServer`
and `writeMinimalConfig` helpers from phase 2 — either by moving them into
a shared `helpers_test.go` file now or by calling them directly (they live
in the same `mcp_test` package already).

```go
func TestHandleWorkflowCreate_Success(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tusk.toml")
	writeMinimalConfig(t, path)
	srv := newTestServer(t, path)

	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{Arguments: map[string]any{
			"name": "sprint",
			"statuses": []any{
				map[string]any{"name": "todo", "roles": []any{"initial"}},
				map[string]any{"name": "doing", "roles": []any{"start", "highlight"}},
				map[string]any{"name": "done", "roles": []any{"terminal", "done"}},
				map[string]any{"name": "dropped", "roles": []any{"terminal", "delete", "dim"}},
			},
			"transitions": []any{
				map[string]any{"from": "todo", "to": "doing"},
				map[string]any{"from": "doing", "to": "done"},
				map[string]any{"from": "doing", "to": "dropped"},
			},
		}},
	}
	res, err := srv.HandleWorkflowCreateForTest(context.Background(), req)
	if err != nil {
		t.Fatalf("HandleWorkflowCreateForTest: %v", err)
	}
	if res.IsError {
		text := res.Content[0].(mcp.TextContent).Text
		t.Fatalf("unexpected error: %s", text)
	}

	loaded, err := config.LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	wf, ok := loaded.Workflows["sprint"]
	if !ok {
		t.Fatalf("workflow sprint not persisted")
	}
	if len(wf.Statuses) != 4 || len(wf.Transitions) != 3 {
		t.Fatalf("unexpected shape: %+v", wf)
	}

	// Hot reload worked — server-side workflow repo now contains it.
	wfs, err := srv.WorkflowRepoForTest().List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	var found bool
	for _, w := range wfs {
		if w.Name == "sprint" {
			found = true
		}
	}
	if !found {
		t.Fatalf("workflow sprint not reloaded into repo: %+v", wfs)
	}
}

func TestHandleWorkflowCreate_ValidationError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tusk.toml")
	writeMinimalConfig(t, path)
	srv := newTestServer(t, path)

	// Missing `initial` role — should be rejected by Config.Validate.
	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{Arguments: map[string]any{
			"name": "broken",
			"statuses": []any{
				map[string]any{"name": "doing", "roles": []any{"start"}},
				map[string]any{"name": "done", "roles": []any{"terminal", "done"}},
				map[string]any{"name": "dropped", "roles": []any{"terminal", "delete"}},
			},
			"transitions": []any{
				map[string]any{"from": "doing", "to": "done"},
			},
		}},
	}
	res, _ := srv.HandleWorkflowCreateForTest(context.Background(), req)
	if !res.IsError {
		t.Fatalf("expected validation error")
	}
}
```

Add a helper on the server:

```go
// WorkflowRepoForTest exposes the workflow repo handle for internal tests.
func (s *Server) WorkflowRepoForTest() *inmem.WorkflowRepository { return s.workflowRepo }
```

Place it alongside the existing `*ForTest` shims in `server.go` or in a
dedicated `testexports.go` file; pick whichever the current code already
prefers.

- [ ] **Step 2: Run to confirm failure**

Run: `go test ./internal/mcp -run TestHandleWorkflowCreate -v`
Expected: FAIL — stub returns "not implemented".

- [ ] **Step 3: Implement the handler**

In `internal/mcp/workflow_handlers.go`:

```go
package mcp

import (
	"context"
	"fmt"

	"github.com/germanamz/tusk/config"
	"github.com/mark3labs/mcp-go/mcp"
)

type statusSpec struct {
	Name  string   `json:"name"`
	Roles []string `json:"roles"`
}

type transitionSpec struct {
	From string `json:"from"`
	To   string `json:"to"`
}

func parseStatusSpecs(raw any) ([]statusSpec, error) {
	if raw == nil {
		return nil, nil
	}
	items, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("expected array")
	}
	out := make([]statusSpec, 0, len(items))
	for i, item := range items {
		obj, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("item %d: expected object", i)
		}
		name, _ := obj["name"].(string)
		if name == "" {
			return nil, fmt.Errorf("item %d: name is required", i)
		}
		var roles []string
		if rawRoles, ok := obj["roles"].([]any); ok {
			for j, r := range rawRoles {
				s, ok := r.(string)
				if !ok {
					return nil, fmt.Errorf("item %d role %d: expected string", i, j)
				}
				roles = append(roles, s)
			}
		}
		out = append(out, statusSpec{Name: name, Roles: roles})
	}
	return out, nil
}

func parseTransitionSpecs(raw any) ([]transitionSpec, error) {
	if raw == nil {
		return nil, nil
	}
	items, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("expected array")
	}
	out := make([]transitionSpec, 0, len(items))
	for i, item := range items {
		obj, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("item %d: expected object", i)
		}
		from, _ := obj["from"].(string)
		to, _ := obj["to"].(string)
		if from == "" || to == "" {
			return nil, fmt.Errorf("item %d: from and to are required", i)
		}
		out = append(out, transitionSpec{From: from, To: to})
	}
	return out, nil
}

func statusesToConfig(specs []statusSpec) map[string]config.StatusConfig {
	out := make(map[string]config.StatusConfig, len(specs))
	for _, s := range specs {
		out[s.Name] = config.StatusConfig{Roles: append([]string(nil), s.Roles...)}
	}
	return out
}

func transitionsToConfig(specs []transitionSpec) []config.WorkflowTransitionConfig {
	out := make([]config.WorkflowTransitionConfig, 0, len(specs))
	for _, t := range specs {
		out = append(out, config.WorkflowTransitionConfig{From: t.From, To: t.To})
	}
	return out
}

func (s *Server) handleWorkflowCreate(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	name, err := req.RequireString("name")
	if err != nil {
		return mcp.NewToolResultError("name is required"), nil
	}
	args := req.GetArguments()

	statuses, err := parseStatusSpecs(args["statuses"])
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("statuses: %v", err)), nil
	}
	if len(statuses) == 0 {
		return mcp.NewToolResultError("statuses is required"), nil
	}
	transitions, err := parseTransitionSpecs(args["transitions"])
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("transitions: %v", err)), nil
	}

	wf := config.WorkflowConfig{
		Statuses:    statusesToConfig(statuses),
		Transitions: transitionsToConfig(transitions),
	}

	path, err := config.ConfigFilePath(s.loadOpts...)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("resolving config file: %v", err)), nil
	}
	if err := config.CreateWorkflow(path, name, wf); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if err := s.reloadConfig(ctx); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("reloading config: %v", err)), nil
	}

	return toolResultJSON(map[string]any{"ok": true, "name": name, "active_file": path})
}

// HandleWorkflowCreateForTest exposes handleWorkflowCreate for internal tests.
func (s *Server) HandleWorkflowCreateForTest(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return s.handleWorkflowCreate(ctx, req)
}
```

- [ ] **Step 4: Verify tests pass**

Run: `go test ./internal/mcp -run TestHandleWorkflowCreate -v -race`
Expected: PASS on both tests.

- [ ] **Step 5: Commit**

```bash
git add internal/mcp/workflow_handlers.go internal/mcp/workflow_handlers_test.go internal/mcp/server.go
git commit -m "feat(mcp): implement tusk_workflow_create"
```

### Task 3: Implement `handleWorkflowModify`

**Files:**
- Modify: `internal/mcp/workflow_handlers.go`
- Modify: `internal/mcp/workflow_handlers_test.go`

- [ ] **Step 1: Write the failing test**

Append:

```go
func TestHandleWorkflowModify_AddAndRemove(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tusk.toml")
	writeMinimalConfig(t, path)
	srv := newTestServer(t, path)

	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{Arguments: map[string]any{
			"name": "kanban",
			"add_statuses": []any{
				map[string]any{"name": "in_review", "roles": []any{}},
			},
			"add_transitions": []any{
				map[string]any{"from": "active", "to": "in_review"},
				map[string]any{"from": "in_review", "to": "completed"},
			},
		}},
	}
	res, err := srv.HandleWorkflowModifyForTest(context.Background(), req)
	if err != nil {
		t.Fatalf("HandleWorkflowModifyForTest: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Content[0].(mcp.TextContent).Text)
	}

	loaded, err := config.LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if _, ok := loaded.Workflows["kanban"].Statuses["in_review"]; !ok {
		t.Fatalf("in_review not added")
	}
}
```

- [ ] **Step 2: Run to confirm failure**

Run: `go test ./internal/mcp -run TestHandleWorkflowModify -v`
Expected: FAIL.

- [ ] **Step 3: Implement the handler**

Append to `workflow_handlers.go`:

```go
func (s *Server) handleWorkflowModify(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	name, err := req.RequireString("name")
	if err != nil {
		return mcp.NewToolResultError("name is required"), nil
	}
	args := req.GetArguments()

	addStatuses, err := parseStatusSpecs(args["add_statuses"])
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("add_statuses: %v", err)), nil
	}
	setStatuses, err := parseStatusSpecs(args["set_statuses"])
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("set_statuses: %v", err)), nil
	}
	addTrans, err := parseTransitionSpecs(args["add_transitions"])
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("add_transitions: %v", err)), nil
	}
	removeTrans, err := parseTransitionSpecs(args["remove_transitions"])
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("remove_transitions: %v", err)), nil
	}

	var removeStatuses []string
	if raw, ok := args["remove_statuses"].([]any); ok {
		for i, r := range raw {
			s, ok := r.(string)
			if !ok {
				return mcp.NewToolResultError(fmt.Sprintf("remove_statuses[%d]: expected string", i)), nil
			}
			removeStatuses = append(removeStatuses, s)
		}
	}

	mut := config.WorkflowMutation{
		AddStatuses:       statusesToConfig(addStatuses),
		SetStatuses:       statusesToConfig(setStatuses),
		RemoveStatuses:    removeStatuses,
		AddTransitions:    transitionsToConfig(addTrans),
		RemoveTransitions: transitionsToConfig(removeTrans),
	}

	path, err := config.ConfigFilePath(s.loadOpts...)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("resolving config file: %v", err)), nil
	}
	if err := config.ModifyWorkflow(path, name, mut); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if err := s.reloadConfig(ctx); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("reloading config: %v", err)), nil
	}

	return toolResultJSON(map[string]any{"ok": true, "name": name, "active_file": path})
}

// HandleWorkflowModifyForTest exposes handleWorkflowModify for internal tests.
func (s *Server) HandleWorkflowModifyForTest(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return s.handleWorkflowModify(ctx, req)
}
```

- [ ] **Step 4: Verify the test passes**

Run: `go test ./internal/mcp -run TestHandleWorkflowModify -v -race`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/mcp/workflow_handlers.go internal/mcp/workflow_handlers_test.go
git commit -m "feat(mcp): implement tusk_workflow_modify"
```

### Task 4: Implement `handleWorkflowDelete`

**Files:**
- Modify: `internal/mcp/workflow_handlers.go`
- Modify: `internal/mcp/workflow_handlers_test.go`

- [ ] **Step 1: Write the failing test**

Append:

```go
func TestHandleWorkflowDelete_Success(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tusk.toml")
	writeMinimalConfig(t, path)

	// Seed an extra workflow via the config helper so we can delete it.
	if err := config.CreateWorkflow(path, "sprint", config.WorkflowConfig{
		Statuses: map[string]config.StatusConfig{
			"todo":  {Roles: []string{"initial"}},
			"doing": {Roles: []string{"start"}},
			"done":  {Roles: []string{"terminal", "done"}},
			"drop":  {Roles: []string{"terminal", "delete"}},
		},
		Transitions: []config.WorkflowTransitionConfig{{From: "todo", To: "doing"}},
	}); err != nil {
		t.Fatalf("seeding sprint: %v", err)
	}

	srv := newTestServer(t, path)

	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{Arguments: map[string]any{"name": "sprint"}},
	}
	res, err := srv.HandleWorkflowDeleteForTest(context.Background(), req)
	if err != nil {
		t.Fatalf("HandleWorkflowDeleteForTest: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Content[0].(mcp.TextContent).Text)
	}

	loaded, _ := config.LoadFile(path)
	if _, ok := loaded.Workflows["sprint"]; ok {
		t.Fatalf("sprint still present after delete")
	}
}

func TestHandleWorkflowDelete_ReferencedByProject(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tusk.toml")
	writeMinimalConfig(t, path)
	srv := newTestServer(t, path)

	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{Arguments: map[string]any{"name": "kanban"}},
	}
	res, _ := srv.HandleWorkflowDeleteForTest(context.Background(), req)
	if !res.IsError {
		t.Fatalf("expected error deleting referenced workflow")
	}
}
```

- [ ] **Step 2: Run to confirm failure**

Run: `go test ./internal/mcp -run TestHandleWorkflowDelete -v`
Expected: FAIL.

- [ ] **Step 3: Implement the handler**

Append to `workflow_handlers.go`:

```go
func (s *Server) handleWorkflowDelete(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	name, err := req.RequireString("name")
	if err != nil {
		return mcp.NewToolResultError("name is required"), nil
	}
	path, err := config.ConfigFilePath(s.loadOpts...)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("resolving config file: %v", err)), nil
	}
	if err := config.DeleteWorkflow(path, name); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if err := s.reloadConfig(ctx); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("reloading config: %v", err)), nil
	}
	return toolResultJSON(map[string]any{"ok": true, "name": name, "active_file": path})
}

// HandleWorkflowDeleteForTest exposes handleWorkflowDelete for internal tests.
func (s *Server) HandleWorkflowDeleteForTest(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return s.handleWorkflowDelete(ctx, req)
}
```

- [ ] **Step 4: Verify the tests pass**

Run: `go test ./internal/mcp -run TestHandleWorkflowDelete -v -race`
Expected: PASS on both tests.

- [ ] **Step 5: Commit**

```bash
git add internal/mcp/workflow_handlers.go internal/mcp/workflow_handlers_test.go
git commit -m "feat(mcp): implement tusk_workflow_delete"
```

### Task 5: Update `serverInstructions` to mention workflow tools

**Files:**
- Modify: `internal/mcp/server.go`

- [ ] **Step 1: Append to the instructions string**

```go
const serverInstructions = `Tusk is a task management system. You can create, list, modify, and transition tasks through workflow statuses. Tasks support parent-child hierarchy, typed relations (blocks, relates_to, duplicates), tags, annotations, and projects. All mutation tools require a version parameter for optimistic locking — fetch the task first to get the current version. You can also inspect the active configuration via tusk_config_show and modify scalar config values via tusk_config_set (storage.* keys are read-only over MCP). Workflows can be created, modified, and deleted via tusk_workflow_create, tusk_workflow_modify, and tusk_workflow_delete using structured JSON inputs (statuses and transitions as arrays of objects).`
```

- [ ] **Step 2: Run the full package tests**

Run: `go test ./internal/mcp -v -race`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/mcp/server.go
git commit -m "docs(mcp): advertise workflow tools in server instructions"
```

## Changes Introduced

- **New files:**
  - `internal/mcp/workflow_handlers.go` — three handlers plus
    `parseStatusSpecs`, `parseTransitionSpecs`, `statusesToConfig`,
    `transitionsToConfig`, and `*ForTest` shims.
  - `internal/mcp/workflow_handlers_test.go` — create (success +
    validation), modify (add/remove), delete (success + referenced).

- **Modified files:**
  - `internal/mcp/server.go` — `validateConfig` allow-list extended with
    `tusk_workflow_create/modify/delete`. `registerTools` wires the three
    new tool definitions. `serverInstructions` updated. `WorkflowRepoForTest`
    accessor added.

- **Modified interfaces:** none beyond Phase 1's signature change.

- **New environment variables / schema migrations / dependencies:** none.

- **User-visible behavior after this phase:**
  - MCP clients can create, modify, and delete workflows; changes
    immediately take effect server-side via `reloadConfig`.
  - `tusk_workflow_list` sees newly created workflows within the same MCP
    session.
  - Validation and reference-guard errors from `config.Validate` and
    `config.DeleteWorkflow` surface back to the MCP caller verbatim.

- **Bridge code:** none introduced in this phase. No bridge code removed.
