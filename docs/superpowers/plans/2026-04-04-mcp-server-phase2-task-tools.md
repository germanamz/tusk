# MCP Server Phase 2: Task Tools

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement the 9 task tools — the core of the MCP server. Each tool is a thin adapter: extract params from the MCP request, call the service layer, return JSON.

**Architecture:** All tool handlers are methods on the `Server` struct in `internal/mcp/tools.go`. Tools are registered in `New()` via `s.server.AddTool()`. A shared `toolResultJSON()` helper marshals domain objects to `mcp.CallToolResult` with JSON text content.

**Tech Stack:** Go, `github.com/mark3labs/mcp-go`, existing service layer

**Design Spec:** `docs/superpowers/specs/2026-04-04-mcp-server-core-design.md`

**Depends on:** Phase 1 (foundation) must be completed first.

---

### Task 1: JSON helper and tusk_task_create / tusk_task_get tools

**Files:**
- Modify: `internal/mcp/server.go` (register tools in `New()`)
- Modify: `internal/mcp/tools.go` (add handlers)
- Create: `internal/mcp/tools_test.go`

These are the two foundational tools. `tusk_task_create` is the simplest mutation, and `tusk_task_get` is the richest read (tags + relations + annotations). Getting these right establishes the pattern for all other tools.

- [ ] **Step 1: Write failing test for toolResultJSON helper and tusk_task_create**

Create `internal/mcp/tools_test.go`:

```go
package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestToolResultJSON(t *testing.T) {
	data := map[string]string{"key": "value"}
	result, err := toolResultJSON(data)
	if err != nil {
		t.Fatalf("toolResultJSON() error: %v", err)
	}
	if result == nil {
		t.Fatal("toolResultJSON() returned nil")
	}
	if result.IsError {
		t.Fatal("toolResultJSON() returned error result")
	}
	if len(result.Content) != 1 {
		t.Fatalf("expected 1 content block, got %d", len(result.Content))
	}
	textContent, ok := result.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", result.Content[0])
	}
	var parsed map[string]string
	if err := json.Unmarshal([]byte(textContent.Text), &parsed); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}
	if parsed["key"] != "value" {
		t.Fatalf("expected key=value, got key=%s", parsed["key"])
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:
```bash
go test -v ./internal/mcp/ -run TestToolResultJSON
```
Expected: Compilation error — `toolResultJSON` undefined.

- [ ] **Step 3: Implement toolResultJSON helper and tusk_task_create handler**

Write `internal/mcp/tools.go`:

```go
package mcp

import (
	"context"
	"encoding/json"
	"time"

	"github.com/germanamz/tusk/internal/domain"
	"github.com/germanamz/tusk/internal/service"
	"github.com/google/uuid"
	"github.com/mark3labs/mcp-go/mcp"
)

// toolResultJSON marshals v as indented JSON and wraps it in an MCP tool result.
func toolResultJSON(v any) (*mcp.CallToolResult, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, err
	}
	return mcp.NewToolResultText(string(b)), nil
}

// toolError translates a domain error into an MCP tool-result error.
// context is optional extra info for not-found errors (e.g., "task abc123").
func toolError(err error, context string) *mcp.CallToolResult {
	return mcp.NewToolResultError(mapError(err, context))
}

// taskResponse is the JSON structure returned by task tools.
type taskResponse struct {
	ID             string         `json:"id"`
	ShortID        string         `json:"short_id"`
	ParentID       *string        `json:"parent_id,omitempty"`
	ProjectID      *string        `json:"project_id,omitempty"`
	Title          string         `json:"title"`
	Description    string         `json:"description"`
	Status         string         `json:"status"`
	Priority       int            `json:"priority"`
	Version        int            `json:"version"`
	Tags           []string       `json:"tags"`
	DueAt          *string        `json:"due_at,omitempty"`
	WaitUntil      *string        `json:"wait_until,omitempty"`
	RecurrenceRule *string        `json:"recurrence_rule,omitempty"`
	CreatedAt      string         `json:"created_at"`
	ModifiedAt     string         `json:"modified_at"`
}

func toTaskResponse(t *domain.Task, tags []*domain.Tag) taskResponse {
	r := taskResponse{
		ID:          t.ID.String(),
		ShortID:     t.ShortID,
		Title:       t.Title,
		Description: t.Description,
		Status:      t.Status,
		Priority:    t.Priority,
		Version:     t.Version,
		CreatedAt:   t.CreatedAt.Format(time.RFC3339),
		ModifiedAt:  t.ModifiedAt.Format(time.RFC3339),
	}
	if t.ParentID != nil {
		s := t.ParentID.String()
		r.ParentID = &s
	}
	if t.ProjectID != nil {
		s := t.ProjectID.String()
		r.ProjectID = &s
	}
	if t.DueAt != nil {
		s := t.DueAt.Format(time.RFC3339)
		r.DueAt = &s
	}
	if t.WaitUntil != nil {
		s := t.WaitUntil.Format(time.RFC3339)
		r.WaitUntil = &s
	}
	r.RecurrenceRule = t.RecurrenceRule
	r.Tags = make([]string, len(tags))
	for i, tg := range tags {
		r.Tags[i] = tg.Name
	}
	return r
}

// handleTaskCreate handles the tusk_task_create tool.
func (s *Server) handleTaskCreate(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	title, err := request.RequireString("title")
	if err != nil {
		return mcp.NewToolResultError("title is required"), nil
	}

	task := &domain.Task{
		Title: title,
	}

	// Optional: description
	if desc, err := request.RequireString("description"); err == nil {
		task.Description = desc
	}

	// Optional: priority
	if p, err := request.RequireFloat("priority"); err == nil {
		task.Priority = int(p)
	}

	// Optional: project (by name)
	if projectName, err := request.RequireString("project"); err == nil {
		project, lookupErr := s.projectSvc.GetByName(ctx, projectName)
		if lookupErr != nil {
			return toolError(lookupErr, "project "+projectName), nil
		}
		task.ProjectID = &project.ID
	}

	// Optional: parent (by short_id)
	if parentShortID, err := request.RequireString("parent"); err == nil {
		parent, lookupErr := s.taskSvc.GetByShortID(ctx, parentShortID)
		if lookupErr != nil {
			return toolError(lookupErr, "parent task "+parentShortID), nil
		}
		task.ParentID = &parent.ID
	}

	// Optional: due (ISO 8601)
	if dueStr, err := request.RequireString("due"); err == nil {
		d, parseErr := time.Parse(time.RFC3339, dueStr)
		if parseErr != nil {
			return mcp.NewToolResultError("invalid due date format, expected ISO 8601 (RFC3339)"), nil
		}
		task.DueAt = &d
	}

	// Optional: wait_until (ISO 8601)
	if waitStr, err := request.RequireString("wait_until"); err == nil {
		w, parseErr := time.Parse(time.RFC3339, waitStr)
		if parseErr != nil {
			return mcp.NewToolResultError("invalid wait_until format, expected ISO 8601 (RFC3339)"), nil
		}
		task.WaitUntil = &w
	}

	if err := s.taskSvc.Create(ctx, task); err != nil {
		return toolError(err, ""), nil
	}

	// Assign tags if provided
	tags := request.GetStringSlice("tags", nil)
	if len(tags) > 0 {
		if err := s.tagSvc.AssignToTask(ctx, task.ID, tags); err != nil {
			return toolError(err, ""), nil
		}
	}

	// Fetch tags for response
	taskTags, err := s.tagSvc.GetTaskTags(ctx, task.ID)
	if err != nil {
		return nil, err
	}

	return toolResultJSON(toTaskResponse(task, taskTags))
}
```

- [ ] **Step 4: Register tusk_task_create in New()**

In `internal/mcp/server.go`, add a call inside `New()` after the `server.NewMCPServer(...)` block, before the `return s`:

```go
s.registerTools()
```

Then add the method to `internal/mcp/server.go`:

```go
// registerTools registers all MCP tool definitions and their handlers.
func (s *Server) registerTools() {
	s.server.AddTool(
		mcp.NewTool("tusk_task_create",
			mcp.WithDescription("Create a new task"),
			mcp.WithString("title",
				mcp.Required(),
				mcp.Description("Task title"),
			),
			mcp.WithString("description",
				mcp.Description("Task description"),
			),
			mcp.WithNumber("priority",
				mcp.Description("Priority level: 0=none, 1=low, 2=medium, 3=high, 4=urgent"),
			),
			mcp.WithString("project",
				mcp.Description("Project name (uses default project if omitted)"),
			),
			mcp.WithString("parent",
				mcp.Description("Parent task short_id for creating subtasks"),
			),
			mcp.WithArray("tags",
				mcp.Description("Tags to assign to the task"),
				mcp.WithStringItems(),
			),
			mcp.WithString("due",
				mcp.Description("Due date in ISO 8601 / RFC3339 format"),
			),
			mcp.WithString("wait_until",
				mcp.Description("Hide task until this ISO 8601 / RFC3339 date"),
			),
		),
		s.handleTaskCreate,
	)
}
```

- [ ] **Step 5: Run the helper test to verify it passes**

Run:
```bash
go test -v ./internal/mcp/ -run TestToolResultJSON
```
Expected: PASS.

- [ ] **Step 6: Verify project compiles**

Run:
```bash
make build
```
Expected: Compiles successfully.

- [ ] **Step 7: Implement tusk_task_get handler**

Add to `internal/mcp/tools.go`:

```go
// annotationResponse is the JSON structure for annotations within task get.
type annotationResponse struct {
	ID        string `json:"id"`
	TaskID    string `json:"task_id"`
	Body      string `json:"body"`
	CreatedAt string `json:"created_at"`
}

// relationResponse is the JSON structure for relations within task get.
type relationResponse struct {
	ID             string `json:"id"`
	SourceID       string `json:"source_id"`
	TargetID       string `json:"target_id"`
	RelationType   string `json:"relation_type"`
	RelatedShortID string `json:"related_short_id"`
	RelatedTitle   string `json:"related_title"`
	DirectionLabel string `json:"direction_label"`
	CreatedAt      string `json:"created_at"`
}

// taskGetResponse extends taskResponse with annotations and relations.
type taskGetResponse struct {
	taskResponse
	Annotations []annotationResponse `json:"annotations"`
	Relations   []relationResponse   `json:"relations"`
}

// handleTaskGet handles the tusk_task_get tool. Returns the full task with
// tags, relations, and annotations.
func (s *Server) handleTaskGet(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	shortID, err := request.RequireString("short_id")
	if err != nil {
		return mcp.NewToolResultError("short_id is required"), nil
	}

	task, err := s.taskSvc.GetByShortID(ctx, shortID)
	if err != nil {
		return toolError(err, "task "+shortID), nil
	}

	// Fetch tags
	tags, err := s.tagSvc.GetTaskTags(ctx, task.ID)
	if err != nil {
		return nil, err
	}

	// Fetch annotations
	annotations, err := s.taskSvc.GetAnnotations(ctx, shortID)
	if err != nil {
		return nil, err
	}

	// Fetch and resolve relations
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

	return toolResultJSON(resp)
}
```

- [ ] **Step 8: Register tusk_task_get in registerTools()**

Add to `registerTools()` in `internal/mcp/server.go`:

```go
s.server.AddTool(
	mcp.NewTool("tusk_task_get",
		mcp.WithDescription("Get a task with full details including tags, relations, and annotations"),
		mcp.WithString("short_id",
			mcp.Required(),
			mcp.Description("Task short ID (8+ hex characters)"),
		),
	),
	s.handleTaskGet,
)
```

- [ ] **Step 9: Verify project compiles and tests pass**

Run:
```bash
make build && make test
```
Expected: Compiles and all tests pass.

- [ ] **Step 10: Commit**

```bash
git add internal/mcp/tools.go internal/mcp/tools_test.go internal/mcp/server.go
git commit -m "feat(mcp): add task create and get tools with JSON helper"
```

---

### Task 2: tusk_task_list and tusk_task_modify tools

**Files:**
- Modify: `internal/mcp/tools.go` (add handlers)
- Modify: `internal/mcp/server.go` (register tools)

- [ ] **Step 1: Implement handleTaskList**

Add to `internal/mcp/tools.go`:

```go
// handleTaskList handles the tusk_task_list tool.
func (s *Server) handleTaskList(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	filter := domain.TaskFilter{}

	// Optional: status (string array)
	if statuses := request.GetStringSlice("status", nil); len(statuses) > 0 {
		filter.Statuses = statuses
	}

	// Optional: priority range
	if pMin, err := request.RequireFloat("priority_min"); err == nil {
		v := int(pMin)
		filter.PriorityMin = &v
	}
	if pMax, err := request.RequireFloat("priority_max"); err == nil {
		v := int(pMax)
		filter.PriorityMax = &v
	}

	// Optional: project (by name → ID)
	if projectName, err := request.RequireString("project"); err == nil {
		project, lookupErr := s.projectSvc.GetByName(ctx, projectName)
		if lookupErr != nil {
			return toolError(lookupErr, "project "+projectName), nil
		}
		filter.ProjectID = &project.ID
	}

	// Optional: tags include/exclude
	if tags := request.GetStringSlice("tags", nil); len(tags) > 0 {
		filter.Tags = tags
	}
	if exTags := request.GetStringSlice("exclude_tags", nil); len(exTags) > 0 {
		filter.ExcludeTags = exTags
	}

	// Optional: due date range
	if after, err := request.RequireString("due_after"); err == nil {
		d, parseErr := time.Parse(time.RFC3339, after)
		if parseErr != nil {
			return mcp.NewToolResultError("invalid due_after format, expected ISO 8601 (RFC3339)"), nil
		}
		filter.DueAfter = &d
	}
	if before, err := request.RequireString("due_before"); err == nil {
		d, parseErr := time.Parse(time.RFC3339, before)
		if parseErr != nil {
			return mcp.NewToolResultError("invalid due_before format, expected ISO 8601 (RFC3339)"), nil
		}
		filter.DueBefore = &d
	}

	// Optional: parent (direct children)
	if parentShortID, err := request.RequireString("parent"); err == nil {
		parent, lookupErr := s.taskSvc.GetByShortID(ctx, parentShortID)
		if lookupErr != nil {
			return toolError(lookupErr, "parent task "+parentShortID), nil
		}
		filter.ParentID = &parent.ID
	}

	// Optional: root (all descendants)
	if rootShortID, err := request.RequireString("root"); err == nil {
		root, lookupErr := s.taskSvc.GetByShortID(ctx, rootShortID)
		if lookupErr != nil {
			return toolError(lookupErr, "root task "+rootShortID), nil
		}
		filter.RootID = &root.ID
	}

	tasks, err := s.taskSvc.List(ctx, filter)
	if err != nil {
		return nil, err
	}

	// Batch-fetch tags for all tasks
	taskIDs := make([]uuid.UUID, len(tasks))
	for i, t := range tasks {
		taskIDs[i] = t.ID
	}
	tagsByTask, err := s.tagSvc.GetTaskTagsBatch(ctx, taskIDs)
	if err != nil {
		return nil, err
	}

	results := make([]taskResponse, len(tasks))
	for i, t := range tasks {
		results[i] = toTaskResponse(t, tagsByTask[t.ID])
	}

	return toolResultJSON(results)
}
```

- [ ] **Step 2: Register tusk_task_list in registerTools()**

Add to `registerTools()` in `internal/mcp/server.go`:

```go
s.server.AddTool(
	mcp.NewTool("tusk_task_list",
		mcp.WithDescription("List tasks with optional filters"),
		mcp.WithArray("status",
			mcp.Description("Filter by status (e.g. [\"pending\", \"active\"])"),
			mcp.WithStringItems(),
		),
		mcp.WithNumber("priority_min",
			mcp.Description("Minimum priority (0-4)"),
		),
		mcp.WithNumber("priority_max",
			mcp.Description("Maximum priority (0-4)"),
		),
		mcp.WithString("project",
			mcp.Description("Filter by project name"),
		),
		mcp.WithArray("tags",
			mcp.Description("Include tasks with these tags"),
			mcp.WithStringItems(),
		),
		mcp.WithArray("exclude_tags",
			mcp.Description("Exclude tasks with these tags"),
			mcp.WithStringItems(),
		),
		mcp.WithString("due_after",
			mcp.Description("Tasks due after this ISO 8601 date"),
		),
		mcp.WithString("due_before",
			mcp.Description("Tasks due before this ISO 8601 date"),
		),
		mcp.WithString("parent",
			mcp.Description("List direct children of this task (short_id)"),
		),
		mcp.WithString("root",
			mcp.Description("List all descendants of this task (short_id)"),
		),
	),
	s.handleTaskList,
)
```

- [ ] **Step 3: Verify it compiles**

Run:
```bash
make build
```
Expected: Compiles successfully.

- [ ] **Step 4: Implement handleTaskModify**

Add to `internal/mcp/tools.go`:

```go
// handleTaskModify handles the tusk_task_modify tool.
func (s *Server) handleTaskModify(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	shortID, err := request.RequireString("short_id")
	if err != nil {
		return mcp.NewToolResultError("short_id is required"), nil
	}

	version, err := request.RequireFloat("version")
	if err != nil {
		return mcp.NewToolResultError("version is required"), nil
	}

	upd := domain.TaskUpdate{
		ShortID: shortID,
		Version: int(version),
	}

	// Optional fields
	if title, err := request.RequireString("title"); err == nil {
		upd.Title = &title
	}
	if desc, err := request.RequireString("description"); err == nil {
		upd.Description = &desc
	}
	if p, err := request.RequireFloat("priority"); err == nil {
		v := int(p)
		upd.Priority = &v
	}

	// Optional: project (by name)
	if projectName, err := request.RequireString("project"); err == nil {
		project, lookupErr := s.projectSvc.GetByName(ctx, projectName)
		if lookupErr != nil {
			return toolError(lookupErr, "project "+projectName), nil
		}
		pid := project.ID
		pp := &pid
		upd.ProjectID = &pp
	}

	// Optional: parent (by short_id, empty string clears parent)
	if parentShortID, err := request.RequireString("parent"); err == nil {
		if parentShortID == "" {
			var nilUUID *uuid.UUID
			upd.ParentID = &nilUUID
		} else {
			parent, lookupErr := s.taskSvc.GetByShortID(ctx, parentShortID)
			if lookupErr != nil {
				return toolError(lookupErr, "parent task "+parentShortID), nil
			}
			pid := parent.ID
			pp := &pid
			upd.ParentID = &pp
		}
	}

	// Optional: due (ISO 8601, empty string clears)
	if dueStr, err := request.RequireString("due"); err == nil {
		if dueStr == "" {
			var nilTime *time.Time
			upd.DueAt = &nilTime
		} else {
			d, parseErr := time.Parse(time.RFC3339, dueStr)
			if parseErr != nil {
				return mcp.NewToolResultError("invalid due date format, expected ISO 8601 (RFC3339)"), nil
			}
			dp := &d
			upd.DueAt = &dp
		}
	}

	// Optional: wait_until (ISO 8601, empty string clears)
	if waitStr, err := request.RequireString("wait_until"); err == nil {
		if waitStr == "" {
			var nilTime *time.Time
			upd.WaitUntil = &nilTime
		} else {
			w, parseErr := time.Parse(time.RFC3339, waitStr)
			if parseErr != nil {
				return mcp.NewToolResultError("invalid wait_until format, expected ISO 8601 (RFC3339)"), nil
			}
			wp := &w
			upd.WaitUntil = &wp
		}
	}

	updated, err := s.taskSvc.Update(ctx, upd)
	if err != nil {
		return toolError(err, "task "+shortID), nil
	}

	// Handle tag changes
	addTags := request.GetStringSlice("add_tags", nil)
	if len(addTags) > 0 {
		if err := s.tagSvc.AssignToTask(ctx, updated.ID, addTags); err != nil {
			return toolError(err, ""), nil
		}
	}
	removeTags := request.GetStringSlice("remove_tags", nil)
	if len(removeTags) > 0 {
		if err := s.tagSvc.RemoveFromTask(ctx, updated.ID, removeTags); err != nil {
			return toolError(err, ""), nil
		}
	}

	// Fetch tags for response
	taskTags, err := s.tagSvc.GetTaskTags(ctx, updated.ID)
	if err != nil {
		return nil, err
	}

	return toolResultJSON(toTaskResponse(updated, taskTags))
}
```

- [ ] **Step 5: Register tusk_task_modify in registerTools()**

Add to `registerTools()` in `internal/mcp/server.go`:

```go
s.server.AddTool(
	mcp.NewTool("tusk_task_modify",
		mcp.WithDescription("Modify task fields. Requires version for optimistic locking."),
		mcp.WithString("short_id",
			mcp.Required(),
			mcp.Description("Task short ID"),
		),
		mcp.WithNumber("version",
			mcp.Required(),
			mcp.Description("Current task version (for optimistic locking)"),
		),
		mcp.WithString("title",
			mcp.Description("New title"),
		),
		mcp.WithString("description",
			mcp.Description("New description"),
		),
		mcp.WithNumber("priority",
			mcp.Description("New priority (0-4)"),
		),
		mcp.WithString("project",
			mcp.Description("Move to project (by name)"),
		),
		mcp.WithString("parent",
			mcp.Description("Set parent task (short_id). Empty string clears parent."),
		),
		mcp.WithString("due",
			mcp.Description("Due date (ISO 8601). Empty string clears."),
		),
		mcp.WithString("wait_until",
			mcp.Description("Wait until date (ISO 8601). Empty string clears."),
		),
		mcp.WithArray("add_tags",
			mcp.Description("Tags to add"),
			mcp.WithStringItems(),
		),
		mcp.WithArray("remove_tags",
			mcp.Description("Tags to remove"),
			mcp.WithStringItems(),
		),
	),
	s.handleTaskModify,
)
```

- [ ] **Step 6: Verify it compiles and tests pass**

Run:
```bash
make build && make test
```
Expected: Compiles and all tests pass.

- [ ] **Step 7: Commit**

```bash
git add internal/mcp/tools.go internal/mcp/server.go
git commit -m "feat(mcp): add task list and modify tools"
```

---

### Task 3: tusk_task_start / tusk_task_done / tusk_task_delete / tusk_task_annotate tools

**Files:**
- Modify: `internal/mcp/tools.go` (add handlers)
- Modify: `internal/mcp/server.go` (register tools)

These four tools are simple — each extracts 1-2 params and calls a single service method.

- [ ] **Step 1: Implement the four handlers**

Add to `internal/mcp/tools.go`:

```go
// handleTaskStart handles the tusk_task_start tool.
func (s *Server) handleTaskStart(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	shortID, err := request.RequireString("short_id")
	if err != nil {
		return mcp.NewToolResultError("short_id is required"), nil
	}
	version, err := request.RequireFloat("version")
	if err != nil {
		return mcp.NewToolResultError("version is required"), nil
	}

	updated, err := s.taskSvc.Start(ctx, shortID, int(version))
	if err != nil {
		return toolError(err, "task "+shortID), nil
	}

	return toolResultJSON(toTaskResponse(updated, nil))
}

// handleTaskDone handles the tusk_task_done tool.
func (s *Server) handleTaskDone(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	shortID, err := request.RequireString("short_id")
	if err != nil {
		return mcp.NewToolResultError("short_id is required"), nil
	}
	version, err := request.RequireFloat("version")
	if err != nil {
		return mcp.NewToolResultError("version is required"), nil
	}

	updated, err := s.taskSvc.Complete(ctx, shortID, int(version))
	if err != nil {
		return toolError(err, "task "+shortID), nil
	}

	return toolResultJSON(toTaskResponse(updated, nil))
}

// handleTaskDelete handles the tusk_task_delete tool.
func (s *Server) handleTaskDelete(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	shortID, err := request.RequireString("short_id")
	if err != nil {
		return mcp.NewToolResultError("short_id is required"), nil
	}
	version, err := request.RequireFloat("version")
	if err != nil {
		return mcp.NewToolResultError("version is required"), nil
	}

	updated, err := s.taskSvc.Delete(ctx, shortID, int(version))
	if err != nil {
		return toolError(err, "task "+shortID), nil
	}

	return toolResultJSON(toTaskResponse(updated, nil))
}

// handleTaskAnnotate handles the tusk_task_annotate tool.
func (s *Server) handleTaskAnnotate(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	shortID, err := request.RequireString("short_id")
	if err != nil {
		return mcp.NewToolResultError("short_id is required"), nil
	}
	body, err := request.RequireString("body")
	if err != nil {
		return mcp.NewToolResultError("body is required"), nil
	}

	ann, err := s.taskSvc.Annotate(ctx, shortID, body)
	if err != nil {
		return toolError(err, "task "+shortID), nil
	}

	return toolResultJSON(annotationResponse{
		ID:        ann.ID.String(),
		TaskID:    ann.TaskID.String(),
		Body:      ann.Body,
		CreatedAt: ann.CreatedAt.Format(time.RFC3339),
	})
}
```

- [ ] **Step 2: Register all four tools in registerTools()**

Add to `registerTools()` in `internal/mcp/server.go`:

```go
s.server.AddTool(
	mcp.NewTool("tusk_task_start",
		mcp.WithDescription("Transition a task to active status"),
		mcp.WithString("short_id",
			mcp.Required(),
			mcp.Description("Task short ID"),
		),
		mcp.WithNumber("version",
			mcp.Required(),
			mcp.Description("Current task version (for optimistic locking)"),
		),
	),
	s.handleTaskStart,
)

s.server.AddTool(
	mcp.NewTool("tusk_task_done",
		mcp.WithDescription("Transition a task to completed status"),
		mcp.WithString("short_id",
			mcp.Required(),
			mcp.Description("Task short ID"),
		),
		mcp.WithNumber("version",
			mcp.Required(),
			mcp.Description("Current task version (for optimistic locking)"),
		),
	),
	s.handleTaskDone,
)

s.server.AddTool(
	mcp.NewTool("tusk_task_delete",
		mcp.WithDescription("Soft-delete a task (transitions to deleted status)"),
		mcp.WithString("short_id",
			mcp.Required(),
			mcp.Description("Task short ID"),
		),
		mcp.WithNumber("version",
			mcp.Required(),
			mcp.Description("Current task version (for optimistic locking)"),
		),
	),
	s.handleTaskDelete,
)

s.server.AddTool(
	mcp.NewTool("tusk_task_annotate",
		mcp.WithDescription("Add an annotation (note) to a task"),
		mcp.WithString("short_id",
			mcp.Required(),
			mcp.Description("Task short ID"),
		),
		mcp.WithString("body",
			mcp.Required(),
			mcp.Description("Annotation text"),
		),
	),
	s.handleTaskAnnotate,
)
```

- [ ] **Step 3: Verify it compiles and tests pass**

Run:
```bash
make build && make test
```
Expected: Compiles and all tests pass.

- [ ] **Step 4: Commit**

```bash
git add internal/mcp/tools.go internal/mcp/server.go
git commit -m "feat(mcp): add task start, done, delete, and annotate tools"
```

---

### Task 4: tusk_task_tree tool

**Files:**
- Modify: `internal/mcp/tools.go` (add handler)
- Modify: `internal/mcp/server.go` (register tool)

The tree tool reuses the same tree-building logic as the TUI. The `buildTree` and `treeNodeJSON`/`toTreeNodeJSON` functions are in `internal/tui/tree.go` and are unexported. Rather than exporting them or creating a shared package, we re-implement the tree-build in the MCP layer — it's ~30 lines and avoids coupling MCP to TUI internals.

- [ ] **Step 1: Implement handleTaskTree**

Add to `internal/mcp/tools.go`:

```go
// treeNodeResponse is the nested JSON structure for the tree tool.
type treeNodeResponse struct {
	ID             string             `json:"id"`
	ShortID        string             `json:"short_id"`
	ParentID       *string            `json:"parent_id,omitempty"`
	ProjectID      *string            `json:"project_id,omitempty"`
	Title          string             `json:"title"`
	Description    string             `json:"description"`
	Status         string             `json:"status"`
	Priority       int                `json:"priority"`
	Version        int                `json:"version"`
	DueAt          *string            `json:"due_at,omitempty"`
	WaitUntil      *string            `json:"wait_until,omitempty"`
	RecurrenceRule *string            `json:"recurrence_rule,omitempty"`
	CreatedAt      string             `json:"created_at"`
	ModifiedAt     string             `json:"modified_at"`
	Children       []treeNodeResponse `json:"children"`
}

func toTreeNodeResponse(task *domain.Task) treeNodeResponse {
	r := treeNodeResponse{
		ID:          task.ID.String(),
		ShortID:     task.ShortID,
		Title:       task.Title,
		Description: task.Description,
		Status:      task.Status,
		Priority:    task.Priority,
		Version:     task.Version,
		CreatedAt:   task.CreatedAt.Format(time.RFC3339),
		ModifiedAt:  task.ModifiedAt.Format(time.RFC3339),
		Children:    []treeNodeResponse{},
	}
	if task.ParentID != nil {
		s := task.ParentID.String()
		r.ParentID = &s
	}
	if task.ProjectID != nil {
		s := task.ProjectID.String()
		r.ProjectID = &s
	}
	if task.DueAt != nil {
		s := task.DueAt.Format(time.RFC3339)
		r.DueAt = &s
	}
	if task.WaitUntil != nil {
		s := task.WaitUntil.Format(time.RFC3339)
		r.WaitUntil = &s
	}
	r.RecurrenceRule = task.RecurrenceRule
	return r
}

// buildTreeResponse constructs a nested tree from a flat task list.
// If rootID is non-nil, only that task is the root. Otherwise, tasks without
// a parent (or whose parent is not in the set) become roots.
func buildTreeResponse(tasks []*domain.Task, rootID *uuid.UUID) []treeNodeResponse {
	type node struct {
		resp     treeNodeResponse
		children []*node
	}

	byID := make(map[uuid.UUID]*node, len(tasks))
	for _, t := range tasks {
		n := &node{resp: toTreeNodeResponse(t)}
		byID[t.ID] = n
	}

	var roots []*node
	for _, t := range tasks {
		n := byID[t.ID]
		if rootID != nil && t.ID == *rootID {
			roots = append(roots, n)
			continue
		}
		if t.ParentID != nil {
			if parent, ok := byID[*t.ParentID]; ok {
				parent.children = append(parent.children, n)
				continue
			}
		}
		if rootID == nil {
			roots = append(roots, n)
		}
	}

	var flatten func(n *node) treeNodeResponse
	flatten = func(n *node) treeNodeResponse {
		r := n.resp
		r.Children = make([]treeNodeResponse, len(n.children))
		for i, child := range n.children {
			r.Children[i] = flatten(child)
		}
		return r
	}

	result := make([]treeNodeResponse, len(roots))
	for i, root := range roots {
		result[i] = flatten(root)
	}
	return result
}

// handleTaskTree handles the tusk_task_tree tool.
func (s *Server) handleTaskTree(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var tasks []*domain.Task
	var rootID *uuid.UUID

	if shortID, err := request.RequireString("short_id"); err == nil {
		// Subtree mode
		root, lookupErr := s.taskSvc.GetByShortID(ctx, shortID)
		if lookupErr != nil {
			return toolError(lookupErr, "task "+shortID), nil
		}
		descendants, err := s.taskSvc.GetDescendants(ctx, root.ID)
		if err != nil {
			return nil, err
		}
		tasks = append([]*domain.Task{root}, descendants...)
		rootID = &root.ID
	} else {
		// Full tree mode
		filter := domain.TaskFilter{
			Statuses: []string{"pending", "active", "completed"},
		}
		// Check include_deleted flag
		if val, err := request.RequireString("include_deleted"); err == nil && val == "true" {
			filter = domain.TaskFilter{}
		}
		var listErr error
		tasks, listErr = s.taskSvc.List(ctx, filter)
		if listErr != nil {
			return nil, listErr
		}
	}

	tree := buildTreeResponse(tasks, rootID)
	return toolResultJSON(tree)
}
```

- [ ] **Step 2: Register tusk_task_tree in registerTools()**

Add to `registerTools()` in `internal/mcp/server.go`:

```go
s.server.AddTool(
	mcp.NewTool("tusk_task_tree",
		mcp.WithDescription("Get tasks as a nested tree hierarchy"),
		mcp.WithString("short_id",
			mcp.Description("Root task short_id (omit for full tree)"),
		),
		mcp.WithString("include_deleted",
			mcp.Description("Set to \"true\" to include deleted tasks"),
		),
	),
	s.handleTaskTree,
)
```

- [ ] **Step 3: Verify it compiles and tests pass**

Run:
```bash
make build && make test
```
Expected: Compiles and all tests pass.

- [ ] **Step 4: Commit**

```bash
git add internal/mcp/tools.go internal/mcp/server.go
git commit -m "feat(mcp): add task tree tool"
```
