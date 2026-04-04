# MCP Server Phase 4: Resources & E2E Tests

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add the 3 MCP resource templates and comprehensive E2E tests that exercise the full MCP server over stdio transport.

**Architecture:** Resource handlers go in `internal/mcp/resources.go`. E2E tests launch `tusk mcp serve` as a subprocess, send JSON-RPC requests over stdin, and validate stdout responses.

**Tech Stack:** Go, `github.com/mark3labs/mcp-go`, JSON-RPC 2.0

**Design Spec:** `docs/superpowers/specs/2026-04-04-mcp-server-core-design.md`

**Depends on:** Phases 1-3 must be completed first (all tools registered).

---

### Task 1: Resource templates (tasks, projects, workflows)

**Files:**
- Modify: `internal/mcp/resources.go` (add handlers)
- Modify: `internal/mcp/server.go` (register resources in `New()`)

- [ ] **Step 1: Implement resource handlers**

Write `internal/mcp/resources.go`:

```go
package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/germanamz/tusk/internal/domain"
	"github.com/mark3labs/mcp-go/mcp"
)

// registerResources registers all MCP resource templates.
func (s *Server) registerResources() {
	// tusk://tasks/{short_id}
	s.server.AddResourceTemplate(
		mcp.NewResourceTemplate(
			"tusk://tasks/{short_id}",
			"Task Detail",
			mcp.WithTemplateDescription("Full task details including tags, relations, and annotations"),
			mcp.WithTemplateMIMEType("application/json"),
		),
		s.handleTaskResource,
	)

	// tusk://projects/{name}
	s.server.AddResourceTemplate(
		mcp.NewResourceTemplate(
			"tusk://projects/{name}",
			"Project Detail",
			mcp.WithTemplateDescription("Project details including settings"),
			mcp.WithTemplateMIMEType("application/json"),
		),
		s.handleProjectResource,
	)

	// tusk://projects/{name}/workflow
	s.server.AddResourceTemplate(
		mcp.NewResourceTemplate(
			"tusk://projects/{name}/workflow",
			"Project Workflow",
			mcp.WithTemplateDescription("Workflow statuses and allowed transitions for a project"),
			mcp.WithTemplateMIMEType("application/json"),
		),
		s.handleWorkflowResource,
	)
}

// handleTaskResource serves tusk://tasks/{short_id}.
// Returns the same rich format as tusk_task_get (task + tags + relations + annotations).
func (s *Server) handleTaskResource(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
	// Extract short_id from URI: "tusk://tasks/{short_id}"
	shortID := extractURIParam(request.Params.URI, "tusk://tasks/")
	if shortID == "" {
		return nil, &resourceError{msg: "missing short_id in URI"}
	}

	task, err := s.taskSvc.GetByShortID(ctx, shortID)
	if err != nil {
		return nil, err
	}

	tags, err := s.tagSvc.GetTaskTags(ctx, task.ID)
	if err != nil {
		return nil, err
	}

	annotations, err := s.taskSvc.GetAnnotations(ctx, shortID)
	if err != nil {
		return nil, err
	}

	rels, err := s.relationSvc.GetByTask(ctx, shortID)
	if err != nil {
		return nil, err
	}

	resp := taskGetResponse{
		taskResponse: toTaskResponse(task, tags),
		Annotations:  make([]annotationResponse, len(annotations)),
		Relations:    make([]relationResponse, 0, len(rels)),
	}

	for i, ann := range annotations {
		resp.Annotations[i] = annotationResponse{
			ID:        ann.ID.String(),
			TaskID:    ann.TaskID.String(),
			Body:      ann.Body,
			CreatedAt: ann.CreatedAt.Format(time.RFC3339),
		}
	}

	for _, rel := range rels {
		rr := relationResponse{
			ID:           rel.ID.String(),
			SourceID:     rel.SourceID.String(),
			TargetID:     rel.TargetID.String(),
			RelationType: rel.RelationType,
			CreatedAt:    rel.CreatedAt.Format(time.RFC3339),
		}
		if rel.TargetID == task.ID {
			switch rel.RelationType {
			case "blocks":
				rr.DirectionLabel = "blocked_by"
			case "relates_to":
				rr.DirectionLabel = "related_to"
			case "duplicates":
				rr.DirectionLabel = "duplicated_by"
			}
			if other, lookupErr := s.taskSvc.GetByID(ctx, rel.SourceID); lookupErr == nil {
				rr.RelatedShortID = other.ShortID
				rr.RelatedTitle = other.Title
			}
		} else {
			rr.DirectionLabel = rel.RelationType
			if other, lookupErr := s.taskSvc.GetByID(ctx, rel.TargetID); lookupErr == nil {
				rr.RelatedShortID = other.ShortID
				rr.RelatedTitle = other.Title
			}
		}
		resp.Relations = append(resp.Relations, rr)
	}

	b, err := json.MarshalIndent(resp, "", "  ")
	if err != nil {
		return nil, err
	}

	return []mcp.ResourceContents{
		mcp.TextResourceContents{
			URI:      request.Params.URI,
			MIMEType: "application/json",
			Text:     string(b),
		},
	}, nil
}

// handleProjectResource serves tusk://projects/{name}.
func (s *Server) handleProjectResource(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
	name := extractURIParam(request.Params.URI, "tusk://projects/")
	// Strip trailing /workflow if present (won't happen for this handler, but defensive)
	if idx := strings.Index(name, "/"); idx >= 0 {
		name = name[:idx]
	}
	if name == "" {
		return nil, &resourceError{msg: "missing project name in URI"}
	}

	project, err := s.projectSvc.GetByName(ctx, name)
	if err != nil {
		return nil, err
	}

	b, err := json.MarshalIndent(toProjectResponse(project), "", "  ")
	if err != nil {
		return nil, err
	}

	return []mcp.ResourceContents{
		mcp.TextResourceContents{
			URI:      request.Params.URI,
			MIMEType: "application/json",
			Text:     string(b),
		},
	}, nil
}

// workflowResponse is the JSON structure for the workflow resource.
type workflowResponse struct {
	ProjectName string               `json:"project_name"`
	Workflow    string               `json:"workflow"`
	Statuses    []string             `json:"statuses"`
	Transitions []transitionResponse `json:"transitions"`
}

type transitionResponse struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// handleWorkflowResource serves tusk://projects/{name}/workflow.
func (s *Server) handleWorkflowResource(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
	// Extract name from "tusk://projects/{name}/workflow"
	uri := request.Params.URI
	name := extractURIParam(uri, "tusk://projects/")
	if idx := strings.Index(name, "/"); idx >= 0 {
		name = name[:idx]
	}
	if name == "" {
		return nil, &resourceError{msg: "missing project name in URI"}
	}

	project, err := s.projectSvc.GetByName(ctx, name)
	if err != nil {
		return nil, err
	}

	statuses, err := s.workflowSvc.GetStatuses(ctx, project.ID, project.DefaultWorkflow)
	if err != nil {
		return nil, err
	}

	transitions, err := s.workflowSvc.GetTransitions(ctx, project.ID, project.DefaultWorkflow)
	if err != nil {
		return nil, err
	}

	resp := workflowResponse{
		ProjectName: project.Name,
		Workflow:    project.DefaultWorkflow,
		Statuses:    statuses,
		Transitions: make([]transitionResponse, len(transitions)),
	}
	for i, t := range transitions {
		resp.Transitions[i] = transitionResponse{From: t.FromStatus, To: t.ToStatus}
	}

	b, err := json.MarshalIndent(resp, "", "  ")
	if err != nil {
		return nil, err
	}

	return []mcp.ResourceContents{
		mcp.TextResourceContents{
			URI:      request.Params.URI,
			MIMEType: "application/json",
			Text:     string(b),
		},
	}, nil
}

// extractURIParam extracts the path segment after a known prefix.
// For example, extractURIParam("tusk://tasks/abc123", "tusk://tasks/") returns "abc123".
func extractURIParam(uri, prefix string) string {
	if !strings.HasPrefix(uri, prefix) {
		return ""
	}
	return uri[len(prefix):]
}

// resourceError is a simple error type for resource handler failures.
type resourceError struct {
	msg string
}

func (e *resourceError) Error() string {
	return e.msg
}
```

**Important note on WorkflowService.GetTransitions:** The workflow resource needs transition data. Check whether `WorkflowService` exposes a `GetTransitions` method. Currently it only has `IsTransitionAllowed` and `GetStatuses`. You will need to add a `GetTransitions` method to `WorkflowService`:

Add to `internal/service/workflow.go`:

```go
// GetTransitions returns all allowed transitions for the workflow
// identified by projectID and workflowName.
func (s *WorkflowService) GetTransitions(ctx context.Context, projectID uuid.UUID, workflowName string) ([]*domain.WorkflowTransition, error) {
	wf, err := s.workflowRepo.GetByProjectAndName(ctx, projectID, workflowName)
	if err != nil {
		return nil, fmt.Errorf("loading workflow %q: %w", workflowName, err)
	}
	return s.workflowRepo.GetTransitions(ctx, wf.ID)
}
```

Also, the `Server` struct needs a `workflowSvc` field. Update `internal/mcp/server.go`:

In the `Server` struct, add:
```go
workflowSvc *service.WorkflowService
```

Update the `New()` function signature to accept it:
```go
func New(
	taskSvc *service.TaskService,
	tagSvc *service.TagService,
	relationSvc *service.RelationService,
	projectSvc *service.ProjectService,
	workflowSvc *service.WorkflowService,
) *Server {
```

And assign it:
```go
s := &Server{
	taskSvc:     taskSvc,
	tagSvc:      tagSvc,
	relationSvc: relationSvc,
	projectSvc:  projectSvc,
	workflowSvc: workflowSvc,
}
```

Update the `tusk mcp serve` command in `internal/tui/app.go` to pass `workflowSvc`. The `App` struct doesn't currently hold `workflowSvc`, so you need to either:
- Add `workflowSvc` as a field on `App`, OR
- Accept it in the `mcp serve` command closure

The simplest approach is to add it to `App`. In `internal/tui/app.go`:

Add `workflowSvc *service.WorkflowService` to the `App` struct.

Update the `New()` function signature to accept it:
```go
func New(taskSvc *service.TaskService, tagSvc *service.TagService, relationSvc *service.RelationService, projectSvc *service.ProjectService, workflowSvc *service.WorkflowService, vi VersionInfo) *App {
```

Assign it: `a.workflowSvc = workflowSvc`

Update the `mcp serve` command to pass it:
```go
mcpServer := tuskmcp.New(taskSvc, tagSvc, relationSvc, projectSvc, a.workflowSvc)
```

Update `cmd/tusk/main.go` to pass `workflowSvc` to `tui.New()`:
```go
app := tui.New(taskSvc, tagSvc, relationSvc, projectSvc, workflowSvc, tui.VersionInfo{...})
```

Update the test in `internal/mcp/server_test.go`:
```go
s := New(nil, nil, nil, nil, nil)
```

- [ ] **Step 2: Register resources in New()**

In `internal/mcp/server.go`, add `s.registerResources()` after `s.registerTools()`:

```go
s.registerTools()
s.registerResources()
```

- [ ] **Step 3: Verify it compiles and tests pass**

Run:
```bash
make build && make test
```
Expected: Compiles and all tests pass. The `TestNewServer` test should be updated to pass the extra `nil` for `workflowSvc`.

- [ ] **Step 4: Commit**

```bash
git add internal/mcp/resources.go internal/mcp/server.go internal/mcp/server_test.go internal/service/workflow.go internal/tui/app.go cmd/tusk/main.go
git commit -m "feat(mcp): add resource templates for tasks, projects, and workflows"
```

---

### Task 2: E2E test harness for MCP server

**Files:**
- Create: `tests/e2e/mcp_test.go`

This test file exercises the MCP server end-to-end by launching `tusk mcp serve` as a subprocess, sending JSON-RPC 2.0 requests via stdin, and reading JSON-RPC responses from stdout.

- [ ] **Step 1: Write the MCP E2E test helper and first test**

Create `tests/e2e/mcp_test.go`:

```go
package e2e

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// mcpEnv manages an MCP server subprocess for E2E testing.
type mcpEnv struct {
	t      *testing.T
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
	nextID int
}

// newMCPEnv starts a `tusk mcp serve` subprocess with a fresh temp DB.
func newMCPEnv(t *testing.T, binPath string) *mcpEnv {
	t.Helper()
	tmpFile, err := os.CreateTemp(t.TempDir(), "tusk-mcp-e2e-*.db")
	if err != nil {
		t.Fatalf("creating temp db: %v", err)
	}
	_ = tmpFile.Close()

	cmd := exec.Command(binPath, "--db", tmpFile.Name(), "mcp", "serve")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("stdin pipe: %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		t.Fatalf("starting mcp server: %v", err)
	}

	t.Cleanup(func() {
		_ = stdin.Close()
		_ = cmd.Wait()
	})

	env := &mcpEnv{
		t:      t,
		cmd:    cmd,
		stdin:  stdin,
		stdout: bufio.NewReader(stdout),
		nextID: 1,
	}

	// Send initialize request
	env.initialize()

	return env
}

// jsonRPCRequest is a JSON-RPC 2.0 request.
type jsonRPCRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// jsonRPCResponse is a JSON-RPC 2.0 response.
type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   json.RawMessage `json:"error,omitempty"`
}

// send sends a JSON-RPC request and reads the response.
func (e *mcpEnv) send(method string, params any) jsonRPCResponse {
	e.t.Helper()

	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      e.nextID,
		Method:  method,
		Params:  params,
	}
	e.nextID++

	b, err := json.Marshal(req)
	if err != nil {
		e.t.Fatalf("marshaling request: %v", err)
	}

	// Write request followed by newline (JSON-RPC over stdio uses newline-delimited JSON)
	if _, err := fmt.Fprintf(e.stdin, "%s\n", b); err != nil {
		e.t.Fatalf("writing request: %v", err)
	}

	// Read response line
	line, err := e.stdout.ReadString('\n')
	if err != nil {
		e.t.Fatalf("reading response: %v", err)
	}

	var resp jsonRPCResponse
	if err := json.Unmarshal([]byte(line), &resp); err != nil {
		e.t.Fatalf("unmarshaling response: %v\nraw: %s", err, line)
	}

	return resp
}

// initialize sends the MCP initialize handshake.
func (e *mcpEnv) initialize() {
	e.t.Helper()
	resp := e.send("initialize", map[string]any{
		"protocolVersion": "2025-03-26",
		"capabilities":    map[string]any{},
		"clientInfo": map[string]any{
			"name":    "tusk-e2e-test",
			"version": "1.0.0",
		},
	})
	if resp.Error != nil {
		e.t.Fatalf("initialize failed: %s", resp.Error)
	}

	// Send initialized notification (no response expected for notifications)
	notif := jsonRPCRequest{
		JSONRPC: "2.0",
		Method:  "notifications/initialized",
	}
	b, _ := json.Marshal(notif)
	if _, err := fmt.Fprintf(e.stdin, "%s\n", b); err != nil {
		e.t.Fatalf("writing initialized notification: %v", err)
	}
}

// callTool sends a tools/call request and returns the parsed result.
func (e *mcpEnv) callTool(name string, args map[string]any) map[string]any {
	e.t.Helper()
	resp := e.send("tools/call", map[string]any{
		"name":      name,
		"arguments": args,
	})
	if resp.Error != nil {
		e.t.Fatalf("tool %s returned error: %s", name, resp.Error)
	}

	// Parse the result to extract the text content
	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		e.t.Fatalf("parsing tool result: %v", err)
	}
	if result.IsError {
		e.t.Fatalf("tool %s returned isError=true: %s", name, result.Content[0].Text)
	}

	// Parse the JSON text content
	var parsed map[string]any
	if err := json.Unmarshal([]byte(result.Content[0].Text), &parsed); err != nil {
		// Might be an array — try returning nil and let caller handle
		e.t.Fatalf("parsing tool JSON content: %v\nraw: %s", err, result.Content[0].Text)
	}
	return parsed
}

// callToolRaw sends a tools/call request and returns the raw text content.
func (e *mcpEnv) callToolRaw(name string, args map[string]any) string {
	e.t.Helper()
	resp := e.send("tools/call", map[string]any{
		"name":      name,
		"arguments": args,
	})
	if resp.Error != nil {
		e.t.Fatalf("tool %s returned error: %s", name, resp.Error)
	}

	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		e.t.Fatalf("parsing tool result: %v", err)
	}
	return result.Content[0].Text
}

// callToolExpectError sends a tools/call request and expects an isError=true result.
// Returns the error message text.
func (e *mcpEnv) callToolExpectError(name string, args map[string]any) string {
	e.t.Helper()
	resp := e.send("tools/call", map[string]any{
		"name":      name,
		"arguments": args,
	})
	if resp.Error != nil {
		e.t.Fatalf("tool %s returned protocol error (expected tool error): %s", name, resp.Error)
	}

	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		e.t.Fatalf("parsing tool result: %v", err)
	}
	if !result.IsError {
		e.t.Fatalf("expected isError=true, got false. content: %s", result.Content[0].Text)
	}
	return result.Content[0].Text
}

func TestMCPTaskLifecycle(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}
	env := newMCPEnv(t, binPath)

	// Create a task
	created := env.callTool("tusk_task_create", map[string]any{
		"title":    "MCP test task",
		"priority": 3,
		"tags":     []string{"mcp", "test"},
	})

	shortID, ok := created["short_id"].(string)
	if !ok || shortID == "" {
		t.Fatal("created task missing short_id")
	}
	if created["title"] != "MCP test task" {
		t.Fatalf("expected title 'MCP test task', got %v", created["title"])
	}
	if created["status"] != "pending" {
		t.Fatalf("expected status 'pending', got %v", created["status"])
	}

	// Get the task (rich response)
	fetched := env.callTool("tusk_task_get", map[string]any{
		"short_id": shortID,
	})
	if fetched["title"] != "MCP test task" {
		t.Fatalf("get: expected title 'MCP test task', got %v", fetched["title"])
	}
	tags, _ := fetched["tags"].([]any)
	if len(tags) != 2 {
		t.Fatalf("get: expected 2 tags, got %d", len(tags))
	}

	// Start the task
	version := created["version"].(float64)
	started := env.callTool("tusk_task_start", map[string]any{
		"short_id": shortID,
		"version":  version,
	})
	if started["status"] != "active" {
		t.Fatalf("expected status 'active', got %v", started["status"])
	}

	// Complete the task
	version = started["version"].(float64)
	completed := env.callTool("tusk_task_done", map[string]any{
		"short_id": shortID,
		"version":  version,
	})
	if completed["status"] != "completed" {
		t.Fatalf("expected status 'completed', got %v", completed["status"])
	}

	// List tasks
	listRaw := env.callToolRaw("tusk_task_list", map[string]any{
		"status": []string{"completed"},
	})
	var listed []map[string]any
	if err := json.Unmarshal([]byte(listRaw), &listed); err != nil {
		t.Fatalf("parsing list result: %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("expected 1 completed task, got %d", len(listed))
	}
	if listed[0]["short_id"] != shortID {
		t.Fatalf("listed task short_id mismatch")
	}
}

func TestMCPTaskModify(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}
	env := newMCPEnv(t, binPath)

	created := env.callTool("tusk_task_create", map[string]any{
		"title": "Original title",
	})
	shortID := created["short_id"].(string)
	version := created["version"].(float64)

	modified := env.callTool("tusk_task_modify", map[string]any{
		"short_id": shortID,
		"version":  version,
		"title":    "Updated title",
		"priority": 2,
		"add_tags": []string{"urgent"},
	})
	if modified["title"] != "Updated title" {
		t.Fatalf("expected 'Updated title', got %v", modified["title"])
	}
	if modified["priority"].(float64) != 2 {
		t.Fatalf("expected priority 2, got %v", modified["priority"])
	}
}

func TestMCPRelations(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}
	env := newMCPEnv(t, binPath)

	task1 := env.callTool("tusk_task_create", map[string]any{"title": "Blocker"})
	task2 := env.callTool("tusk_task_create", map[string]any{"title": "Blocked"})

	shortID1 := task1["short_id"].(string)
	shortID2 := task2["short_id"].(string)

	// Add relation
	rel := env.callTool("tusk_relation_add", map[string]any{
		"source": shortID1,
		"target": shortID2,
		"type":   "blocks",
	})
	if rel["relation_type"] != "blocks" {
		t.Fatalf("expected relation_type 'blocks', got %v", rel["relation_type"])
	}

	// Verify relation in task get
	fetched := env.callTool("tusk_task_get", map[string]any{"short_id": shortID2})
	relations, _ := fetched["relations"].([]any)
	if len(relations) != 1 {
		t.Fatalf("expected 1 relation, got %d", len(relations))
	}

	// Remove relation
	env.callToolRaw("tusk_relation_remove", map[string]any{
		"source": shortID1,
		"target": shortID2,
		"type":   "blocks",
	})

	// Verify relation removed
	fetched2 := env.callTool("tusk_task_get", map[string]any{"short_id": shortID2})
	relations2, _ := fetched2["relations"].([]any)
	if len(relations2) != 0 {
		t.Fatalf("expected 0 relations after remove, got %d", len(relations2))
	}
}

func TestMCPProjects(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}
	env := newMCPEnv(t, binPath)

	// List projects (should have _default)
	listRaw := env.callToolRaw("tusk_project_list", map[string]any{})
	var projects []map[string]any
	if err := json.Unmarshal([]byte(listRaw), &projects); err != nil {
		t.Fatalf("parsing project list: %v", err)
	}
	found := false
	for _, p := range projects {
		if p["name"] == "_default" {
			found = true
		}
	}
	if !found {
		t.Fatal("_default project not found in list")
	}

	// Create a project
	created := env.callTool("tusk_project_create", map[string]any{
		"name":        "test-project",
		"description": "A test project",
	})
	if created["name"] != "test-project" {
		t.Fatalf("expected name 'test-project', got %v", created["name"])
	}
}

func TestMCPErrorHandling(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}
	env := newMCPEnv(t, binPath)

	// Not found
	errMsg := env.callToolExpectError("tusk_task_get", map[string]any{
		"short_id": "nonexistent",
	})
	if !strings.Contains(errMsg, "not found") {
		t.Fatalf("expected 'not found' error, got: %s", errMsg)
	}

	// Version conflict
	created := env.callTool("tusk_task_create", map[string]any{"title": "Conflict test"})
	shortID := created["short_id"].(string)

	errMsg = env.callToolExpectError("tusk_task_start", map[string]any{
		"short_id": shortID,
		"version":  999,
	})
	if !strings.Contains(errMsg, "version conflict") {
		t.Fatalf("expected 'version conflict' error, got: %s", errMsg)
	}

	// Cyclic blocks
	task1 := env.callTool("tusk_task_create", map[string]any{"title": "A"})
	task2 := env.callTool("tusk_task_create", map[string]any{"title": "B"})
	sid1 := task1["short_id"].(string)
	sid2 := task2["short_id"].(string)

	env.callTool("tusk_relation_add", map[string]any{
		"source": sid1, "target": sid2, "type": "blocks",
	})
	errMsg = env.callToolExpectError("tusk_relation_add", map[string]any{
		"source": sid2, "target": sid1, "type": "blocks",
	})
	if !strings.Contains(errMsg, "cycle") {
		t.Fatalf("expected 'cycle' error, got: %s", errMsg)
	}
}

func TestMCPAnnotations(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}
	env := newMCPEnv(t, binPath)

	created := env.callTool("tusk_task_create", map[string]any{"title": "Annotate me"})
	shortID := created["short_id"].(string)

	ann := env.callTool("tusk_task_annotate", map[string]any{
		"short_id": shortID,
		"body":     "This is a note",
	})
	if ann["body"] != "This is a note" {
		t.Fatalf("expected annotation body 'This is a note', got %v", ann["body"])
	}

	// Verify annotation appears in task get
	fetched := env.callTool("tusk_task_get", map[string]any{"short_id": shortID})
	annotations, _ := fetched["annotations"].([]any)
	if len(annotations) != 1 {
		t.Fatalf("expected 1 annotation, got %d", len(annotations))
	}
}

func TestMCPTree(t *testing.T) {
	if binPath == "" {
		t.Skip("binary not built")
	}
	env := newMCPEnv(t, binPath)

	parent := env.callTool("tusk_task_create", map[string]any{"title": "Parent"})
	parentSID := parent["short_id"].(string)

	env.callTool("tusk_task_create", map[string]any{
		"title":  "Child 1",
		"parent": parentSID,
	})
	env.callTool("tusk_task_create", map[string]any{
		"title":  "Child 2",
		"parent": parentSID,
	})

	treeRaw := env.callToolRaw("tusk_task_tree", map[string]any{
		"short_id": parentSID,
	})
	var tree []map[string]any
	if err := json.Unmarshal([]byte(treeRaw), &tree); err != nil {
		t.Fatalf("parsing tree: %v", err)
	}
	if len(tree) != 1 {
		t.Fatalf("expected 1 root node, got %d", len(tree))
	}
	children, _ := tree[0]["children"].([]any)
	if len(children) != 2 {
		t.Fatalf("expected 2 children, got %d", len(children))
	}
}
```

- [ ] **Step 2: Run E2E tests**

Run:
```bash
make build && go test -v ./tests/e2e/ -run TestMCP -count=1
```
Expected: All MCP E2E tests pass. If any fail, debug by checking the raw JSON-RPC exchange (stderr output from the subprocess shows server-side errors).

**Note:** The `binPath` variable is set in `tests/e2e/main_test.go` via `TestMain`. The binary is already built by the existing E2E harness infrastructure — no changes needed to `main_test.go`.

- [ ] **Step 3: Run the full test suite to verify no regressions**

Run:
```bash
make test
```
Expected: All existing tests plus new MCP E2E tests pass.

- [ ] **Step 4: Commit**

```bash
git add tests/e2e/mcp_test.go
git commit -m "test(e2e): add MCP server E2E tests"
```

---

### Task 3: Update ROADMAP.md and clean up

**Files:**
- Modify: `ROADMAP.md` (mark MCP Server Core stories as done)

- [ ] **Step 1: Mark the completed stories in ROADMAP.md**

In `ROADMAP.md`, under `## v0.3 — MCP Server`, change the `MCP Server Core` initiative items from `- [ ]` to `- [x]`:

```markdown
### Initiative: MCP Server Core

> stdio-transport MCP server mapping tools to service methods.

- [x] **Story: MCP server with stdio transport**
  - [x] Server setup and lifecycle management
  - [x] stdio transport implementation
  - [x] Tool registration framework

- [x] **Story: Task tools**
  - [x] `tusk_task_create` — TaskService.Create
  - [x] `tusk_task_list` — TaskService.List
  - [x] `tusk_task_get` — TaskService.GetByShortID
  - [x] `tusk_task_modify` — TaskService.Update
  - [x] `tusk_task_start` — TaskService.Start
  - [x] `tusk_task_done` — TaskService.Complete
  - [x] `tusk_task_delete` — TaskService.Delete
  - [x] `tusk_task_annotate` — TaskService.Annotate
  - [x] `tusk_task_tree` — TaskService.Tree

- [x] **Story: Relation & project tools**
  - [x] `tusk_relation_add` — RelationService.Add
  - [x] `tusk_relation_remove` — RelationService.Remove
  - [x] `tusk_project_list` — ProjectService.List
  - [x] `tusk_project_create` — ProjectService.Create
```

Also mark the MCP Resources and MCP Concurrency initiatives if they were completed in this implementation. Based on the design, resources are done but concurrency (version passing) was built into the tools, so mark those too:

```markdown
### Initiative: MCP Resources

> Expose tasks, projects, and workflows as readable resources.

- [x] **Story: MCP resource definitions**
  - [x] `tusk://tasks/{short_id}` resource
  - [x] `tusk://projects/{name}` resource
  - [x] `tusk://projects/{name}/workflow` resource

### Initiative: MCP Concurrency

> End-to-end optimistic locking through MCP tool I/O.

- [x] **Story: Version passing**
  - [x] Include `version` in all task tool responses
  - [x] Accept `version` in modify/start/done/delete tool inputs
  - [x] Return ErrConflict on version mismatch
```

- [ ] **Step 2: Run linter**

Run:
```bash
make lint
```
Expected: No new lint issues.

- [ ] **Step 3: Commit**

```bash
git add ROADMAP.md
git commit -m "docs: mark v0.3 MCP Server initiative as complete"
```
