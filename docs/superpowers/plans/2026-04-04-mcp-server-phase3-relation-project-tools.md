# MCP Server Phase 3: Relation & Project Tools

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add the 4 remaining tools — 2 relation tools and 2 project tools — completing the full tool set.

**Architecture:** Same pattern as Phase 2 — handler methods on `Server`, registered in `registerTools()`. All handlers are in `internal/mcp/tools.go`.

**Tech Stack:** Go, `github.com/mark3labs/mcp-go`, existing service layer

**Design Spec:** `docs/superpowers/specs/2026-04-04-mcp-server-core-design.md`

**Depends on:** Phase 1 (foundation) and Phase 2 (task tools) must be completed first.

---

### Task 1: tusk_relation_add and tusk_relation_remove tools

**Files:**
- Modify: `internal/mcp/tools.go` (add handlers)
- Modify: `internal/mcp/server.go` (register tools)

- [ ] **Step 1: Implement handleRelationAdd and handleRelationRemove**

Add to `internal/mcp/tools.go`:

```go
// relationAddResponse is the JSON structure returned by the relation add tool.
type relationAddResponse struct {
	ID           string `json:"id"`
	SourceID     string `json:"source_id"`
	TargetID     string `json:"target_id"`
	RelationType string `json:"relation_type"`
	CreatedAt    string `json:"created_at"`
}

// handleRelationAdd handles the tusk_relation_add tool.
func (s *Server) handleRelationAdd(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	source, err := request.RequireString("source")
	if err != nil {
		return mcp.NewToolResultError("source is required"), nil
	}
	target, err := request.RequireString("target")
	if err != nil {
		return mcp.NewToolResultError("target is required"), nil
	}
	relType, err := request.RequireString("type")
	if err != nil {
		return mcp.NewToolResultError("type is required"), nil
	}

	rel, err := s.relationSvc.Add(ctx, source, target, relType)
	if err != nil {
		return toolError(err, ""), nil
	}

	return toolResultJSON(relationAddResponse{
		ID:           rel.ID.String(),
		SourceID:     rel.SourceID.String(),
		TargetID:     rel.TargetID.String(),
		RelationType: rel.RelationType,
		CreatedAt:    rel.CreatedAt.Format(time.RFC3339),
	})
}

// handleRelationRemove handles the tusk_relation_remove tool.
func (s *Server) handleRelationRemove(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	source, err := request.RequireString("source")
	if err != nil {
		return mcp.NewToolResultError("source is required"), nil
	}
	target, err := request.RequireString("target")
	if err != nil {
		return mcp.NewToolResultError("target is required"), nil
	}
	relType, err := request.RequireString("type")
	if err != nil {
		return mcp.NewToolResultError("type is required"), nil
	}

	if err := s.relationSvc.Remove(ctx, source, target, relType); err != nil {
		return toolError(err, ""), nil
	}

	return mcp.NewToolResultText("relation removed"), nil
}
```

- [ ] **Step 2: Register both tools in registerTools()**

Add to `registerTools()` in `internal/mcp/server.go`:

```go
s.server.AddTool(
	mcp.NewTool("tusk_relation_add",
		mcp.WithDescription("Create a typed relation between two tasks"),
		mcp.WithString("source",
			mcp.Required(),
			mcp.Description("Source task short_id"),
		),
		mcp.WithString("target",
			mcp.Required(),
			mcp.Description("Target task short_id"),
		),
		mcp.WithString("type",
			mcp.Required(),
			mcp.Description("Relation type"),
			mcp.Enum("blocks", "relates_to", "duplicates"),
		),
	),
	s.handleRelationAdd,
)

s.server.AddTool(
	mcp.NewTool("tusk_relation_remove",
		mcp.WithDescription("Remove a relation between two tasks"),
		mcp.WithString("source",
			mcp.Required(),
			mcp.Description("Source task short_id"),
		),
		mcp.WithString("target",
			mcp.Required(),
			mcp.Description("Target task short_id"),
		),
		mcp.WithString("type",
			mcp.Required(),
			mcp.Description("Relation type"),
			mcp.Enum("blocks", "relates_to", "duplicates"),
		),
	),
	s.handleRelationRemove,
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
git commit -m "feat(mcp): add relation add and remove tools"
```

---

### Task 2: tusk_project_list and tusk_project_create tools

**Files:**
- Modify: `internal/mcp/tools.go` (add handlers)
- Modify: `internal/mcp/server.go` (register tools)

- [ ] **Step 1: Implement handleProjectList and handleProjectCreate**

Add to `internal/mcp/tools.go`:

```go
// projectResponse is the JSON structure returned by project tools.
type projectResponse struct {
	ID              string                 `json:"id"`
	Name            string                 `json:"name"`
	Description     string                 `json:"description"`
	DefaultWorkflow string                 `json:"default_workflow"`
	Settings        domain.ProjectSettings `json:"settings"`
	Version         int                    `json:"version"`
	CreatedAt       string                 `json:"created_at"`
}

func toProjectResponse(p *domain.Project) projectResponse {
	return projectResponse{
		ID:              p.ID.String(),
		Name:            p.Name,
		Description:     p.Description,
		DefaultWorkflow: p.DefaultWorkflow,
		Settings:        p.Settings,
		Version:         p.Version,
		CreatedAt:       p.CreatedAt.Format(time.RFC3339),
	}
}

// handleProjectList handles the tusk_project_list tool.
func (s *Server) handleProjectList(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	projects, err := s.projectSvc.List(ctx)
	if err != nil {
		return nil, err
	}

	results := make([]projectResponse, len(projects))
	for i, p := range projects {
		results[i] = toProjectResponse(p)
	}

	return toolResultJSON(results)
}

// handleProjectCreate handles the tusk_project_create tool.
func (s *Server) handleProjectCreate(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	name, err := request.RequireString("name")
	if err != nil {
		return mcp.NewToolResultError("name is required"), nil
	}

	var description string
	if desc, err := request.RequireString("description"); err == nil {
		description = desc
	}

	project, err := s.projectSvc.Create(ctx, name, description)
	if err != nil {
		return toolError(err, "project "+name), nil
	}

	return toolResultJSON(toProjectResponse(project))
}
```

- [ ] **Step 2: Register both tools in registerTools()**

Add to `registerTools()` in `internal/mcp/server.go`:

```go
s.server.AddTool(
	mcp.NewTool("tusk_project_list",
		mcp.WithDescription("List all projects"),
	),
	s.handleProjectList,
)

s.server.AddTool(
	mcp.NewTool("tusk_project_create",
		mcp.WithDescription("Create a new project"),
		mcp.WithString("name",
			mcp.Required(),
			mcp.Description("Project name (must be unique)"),
		),
		mcp.WithString("description",
			mcp.Description("Project description"),
		),
	),
	s.handleProjectCreate,
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
git commit -m "feat(mcp): add project list and create tools"
```
