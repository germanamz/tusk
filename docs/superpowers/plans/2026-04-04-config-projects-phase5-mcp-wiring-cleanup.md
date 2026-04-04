# Config-based Projects — Phase 5: MCP, DI Wiring & Cleanup

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Update MCP tools and resources for string project IDs, remove `tusk_project_create` tool, update `cmd/tusk/main.go` DI wiring to use the in-memory project repo, and update E2E tests. After this phase the entire codebase compiles, all tests pass, and the project layer is fully config-driven.

**Architecture:** MCP tools switch from UUID-based project lookups to string IDs. The DI wiring in `main.go` replaces `sqlite.NewProjectRepo(db)` with `inmem.NewProjectRepository(cfg.Projects)`. E2E tests adapt to the new schema and CLI behavior.

**Tech Stack:** Go, MCP (github.com/mark3labs/mcp-go)

**Prerequisite:** Phase 4 (filter and CLI updates) must be complete.

**Design spec:** `docs/superpowers/specs/2026-04-04-config-based-projects-design.md`

---

### Task 1: Update MCP tools for string project IDs

**Files:**
- Modify: `internal/mcp/tools.go`

Update task tool handlers to use string project IDs and remove `handleProjectCreate`.

- [ ] **Step 1: Update handleTaskCreate**

Find the project handling block (around lines 104-111):
```go
	// Optional: project (by name)
	if projectName, err := request.RequireString("project"); err == nil {
		project, lookupErr := s.projectSvc.GetByName(ctx, projectName)
		if lookupErr != nil {
			return toolError(lookupErr, "project "+projectName), nil
		}
		task.ProjectID = &project.ID
	}
```
Replace with:
```go
	// Optional: project (by ID, which is the human-readable name)
	if projectID, err := request.RequireString("project"); err == nil {
		task.ProjectID = projectID
	}
```

Project validation happens in `TaskService.Create`.

- [ ] **Step 2: Update handleTaskList**

Find the project handling block (around lines 296-303):
```go
	// Optional: project (by name → ID)
	if projectName, err := request.RequireString("project"); err == nil {
		project, lookupErr := s.projectSvc.GetByName(ctx, projectName)
		if lookupErr != nil {
			return toolError(lookupErr, "project "+projectName), nil
		}
		filter.ProjectID = &project.ID
	}
```
Replace with:
```go
	// Optional: project (by ID)
	if projectID, err := request.RequireString("project"); err == nil {
		filter.ProjectID = &projectID
	}
```

- [ ] **Step 3: Update handleTaskModify**

Find the project handling block (around lines 399-407):
```go
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
```
Replace with:
```go
	// Optional: project (by ID)
	if projectID, err := request.RequireString("project"); err == nil {
		upd.ProjectID = &projectID
	}
```

- [ ] **Step 4: Update toTaskResponse for string ProjectID**

Find (around lines 63-66):
```go
	if t.ProjectID != nil {
		s := t.ProjectID.String()
		r.ProjectID = &s
	}
```
Replace with:
```go
	if t.ProjectID != "" {
		r.ProjectID = &t.ProjectID
	}
```

- [ ] **Step 5: Update toTreeNodeResponse for string ProjectID**

Find (around lines 582-585):
```go
	if task.ProjectID != nil {
		s := task.ProjectID.String()
		r.ProjectID = &s
	}
```
Replace with:
```go
	if task.ProjectID != "" {
		r.ProjectID = &task.ProjectID
	}
```

- [ ] **Step 6: Update projectResponse struct and toProjectResponse**

Replace:
```go
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
```
with:
```go
type projectResponse struct {
	ID       string                 `json:"id"`
	Workflow string                 `json:"workflow"`
	Settings domain.ProjectSettings `json:"settings"`
}

func toProjectResponse(p *domain.Project) projectResponse {
	return projectResponse{
		ID:       p.ID,
		Workflow: p.Workflow,
		Settings: p.Settings,
	}
}
```

- [ ] **Step 7: Delete handleProjectCreate**

Remove the entire `handleProjectCreate` function (around lines 747-764):
```go
// DELETE THIS:
func (s *Server) handleProjectCreate(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// ...
}
```

- [ ] **Step 8: Clean up imports in tools.go**

After removing `handleProjectCreate` and the UUID-based project lookups, check if these imports are still needed:
- `"time"` — still used for task timestamps. Keep.
- `"github.com/google/uuid"` — check if any UUID usage remains. It was used in `buildTreeResponse` (`uuid.UUID` type in map keys). Keep if so.

- [ ] **Step 9: Commit**

```bash
git add internal/mcp/tools.go
git commit -m "feat(mcp): update tools for string project IDs, remove project_create"
```

---

### Task 2: Update MCP server.go and resources.go

**Files:**
- Modify: `internal/mcp/server.go`
- Modify: `internal/mcp/resources.go`

Remove `tusk_project_create` tool registration and update resource handlers.

- [ ] **Step 1: Remove tusk_project_create from server.go registerTools**

Find and delete this block (around lines 401-413):
```go
	s.addTool("project",
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

- [ ] **Step 2: Remove tusk_project_create from validateConfig**

In the `validateConfig` method, remove `"tusk_project_create": true` from the `validToolNames` map:
```go
	validToolNames := map[string]bool{
		// ... keep all others ...
		"tusk_project_list":    true,
		// DELETE: "tusk_project_create":  true,
	}
```

- [ ] **Step 3: Update resources.go — handleProjectResource**

Change `GetByName` to `GetByID`:
```go
// Before:
	project, err := s.projectSvc.GetByName(ctx, name)

// After:
	project, err := s.projectSvc.GetByID(ctx, name)
```

- [ ] **Step 4: Update resources.go — handleWorkflowResource**

Change `GetByName` to `GetByID` and update the workflow field reference:
```go
// Before:
	project, err := s.projectSvc.GetByName(ctx, name)
	if err != nil {
		return nil, err
	}

	statuses, err := s.workflowSvc.GetStatuses(ctx, project.ID, project.DefaultWorkflow)
	if err != nil {
		return nil, err
	}

	transitions, err := s.workflowSvc.GetTransitions(ctx, project.ID, project.DefaultWorkflow)

// After:
	project, err := s.projectSvc.GetByID(ctx, name)
	if err != nil {
		return nil, err
	}

	statuses, err := s.workflowSvc.GetStatuses(ctx, project.ID, project.Workflow)
	if err != nil {
		return nil, err
	}

	transitions, err := s.workflowSvc.GetTransitions(ctx, project.ID, project.Workflow)
```

Also update the response construction:
```go
// Before:
	resp := workflowResponse{
		ProjectName: project.Name,
		Workflow:    project.DefaultWorkflow,
		// ...

// After:
	resp := workflowResponse{
		ProjectName: project.ID,
		Workflow:    project.Workflow,
		// ...
```

- [ ] **Step 5: Verify the mcp package compiles**

Run: `go build ./internal/mcp/...`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/mcp/server.go internal/mcp/resources.go
git commit -m "feat(mcp): remove project_create tool, update resources for string IDs"
```

---

### Task 3: Update DI wiring in main.go

**Files:**
- Modify: `cmd/tusk/main.go`

Replace `sqlite.NewProjectRepo(db)` with `inmem.NewProjectRepository(cfg.Projects)`. Add the `inmem` import.

- [ ] **Step 1: Update cmd/tusk/main.go**

**Add import:**
```go
	"github.com/germanamz/tusk/internal/inmem"
```

**Replace** the project repo creation (around line 56):
```go
// Before:
	projectRepo := sqlite.NewProjectRepo(db)

// After:
	projectRepo := inmem.NewProjectRepository(cfg.Projects)
```

The rest of the wiring stays the same — `projectRepo` is injected into `service.NewProjectService(projectRepo)` and `service.NewTaskService(taskRepo, annotationRepo, projectRepo, workflowSvc, store)`.

- [ ] **Step 2: Verify the binary compiles**

Run: `go build ./cmd/tusk/...`
Expected: PASS

- [ ] **Step 3: Run unit tests**

Run: `make test`

Note: Some tests may fail due to test fixtures that reference the old schema or project types. E2E tests are updated in Task 4. Unit tests in `internal/service/` may need fixture updates if they create tasks with UUID project IDs.

- [ ] **Step 4: Commit**

```bash
git add cmd/tusk/main.go
git commit -m "feat: wire in-memory project repo in DI setup"
```

---

### Task 4: Update E2E tests

**Files:**
- Modify: `tests/e2e/project_test.go`
- Modify: `tests/e2e/harness.go` (if it references project setup)
- Modify: `tests/e2e/propagation_test.go` (if it creates projects)
- Modify: other E2E test files as needed

E2E tests run against the actual binary. Since projects are now config-driven, tests that used `tusk project create` must be updated.

- [ ] **Step 1: Examine current E2E test files**

Read the following files to understand what needs changing:
- `tests/e2e/project_test.go` — tests for project commands
- `tests/e2e/propagation_test.go` — tests for auto-complete/revert (these create projects with settings)
- `tests/e2e/harness.go` — test harness setup

- [ ] **Step 2: Update project_test.go**

Remove tests for `tusk project create` and `tusk project modify`. Keep tests for `tusk project list`. Add a test that verifies the default project appears in the list.

The exact changes depend on the current test content. The test should verify:
- `tusk project list` shows the `default` project with `kanban` workflow
- Projects defined in a test config file appear in the list

For tests that need custom projects, the test harness needs to write a custom `config.toml` to a temp directory and pass it to the tusk binary (via `TUSK_` environment variables or a custom config path).

- [ ] **Step 3: Update propagation_test.go**

Propagation tests need projects with `auto_complete_parent` settings. Since projects are now config-driven, these tests must:
1. Write a test `config.toml` with a project that has the required settings
2. Use the test harness to point at this config
3. Create tasks in that project

If the test harness doesn't support custom config, update it to accept a config path or write a config file in the temp directory.

- [ ] **Step 4: Update harness.go if needed**

The test harness may need to:
- Accept a config directory parameter
- Write a default test config to the temp directory
- Set `--db` to point at a temp database (it likely already does this)

Check if the harness creates a database and runs migrations — with the new schema (no projects table), the default `_default` project seed is gone. Tasks will default to `project_id = "default"`.

- [ ] **Step 5: Run all E2E tests**

Run: `make test-e2e`
Expected: All PASS

- [ ] **Step 6: Run the full test suite**

Run: `make test`
Expected: All PASS

- [ ] **Step 7: Run vet and lint**

Run: `make vet && make lint`
Expected: All PASS

- [ ] **Step 8: Commit**

```bash
git add tests/e2e/
git commit -m "test(e2e): update tests for config-driven projects"
```
