# Configuration System — Phase 2: MCP Visibility Filtering

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add group annotations to MCP tools and resources, and filter them at registration time based on `MCPConfig` disable lists.

**Architecture:** The MCP `Server` struct gains an `MCPConfig` field and two internal maps (`toolGroups`, `resourceGroups`) that tag each tool/resource with a group name. Before calling `AddTool` or `AddResourceTemplate`, a check function consults the disable lists. Unknown entries in the disable lists produce a warning to stderr.

**Tech Stack:** Go, mcp-go library

**Design Spec:** `docs/superpowers/specs/2026-04-04-configuration-system-design.md`

**Depends on:** Phase 1 (config package must exist so `MCPConfig` type is available)

---

### Task 1: Add MCPConfig to Server and update constructor

**Files:**
- Modify: `internal/mcp/server.go` (lines 1-49)
- Modify: `internal/mcp/server_test.go`
- Modify: `internal/tui/app.go` (line 148 — MCP server creation)

The MCP server constructor currently has this signature (from `internal/mcp/server.go:20-26`):

```go
func New(
    taskSvc *service.TaskService,
    tagSvc *service.TagService,
    relationSvc *service.RelationService,
    projectSvc *service.ProjectService,
    workflowSvc *service.WorkflowService,
    version string,
) *Server
```

We need to add `MCPConfig` as a parameter and store it, along with the group maps.

- [ ] **Step 1: Write a failing test**

Add to `internal/mcp/server_test.go`:

```go
func TestNewServer_WithConfig(t *testing.T) {
	cfg := config.MCPConfig{
		DisabledTools: []string{"tusk_task_delete"},
	}
	s := New(nil, nil, nil, nil, nil, "test", cfg)
	if s == nil {
		t.Fatal("New() returned nil")
	}
	if s.cfg.DisabledTools[0] != "tusk_task_delete" {
		t.Fatal("config not stored on server")
	}
}
```

Add the import for `"github.com/germanamz/tusk/internal/config"` at the top of the test file.

- [ ] **Step 2: Run test to verify it fails**

```bash
go test -v ./internal/mcp/ -run TestNewServer_WithConfig
```

Expected: compilation error — `New` doesn't accept `config.MCPConfig` yet.

- [ ] **Step 3: Update the Server struct and constructor**

In `internal/mcp/server.go`, update the `Server` struct to add new fields:

```go
type Server struct {
	taskSvc        *service.TaskService
	tagSvc         *service.TagService
	relationSvc    *service.RelationService
	projectSvc     *service.ProjectService
	workflowSvc    *service.WorkflowService
	server         *server.MCPServer
	cfg            config.MCPConfig
	toolGroups     map[string]string // tool name → group
	resourceGroups map[string]string // resource URI template → group
}
```

Add the import `"github.com/germanamz/tusk/internal/config"` to the import block.

Update the `New` function signature to accept `cfg config.MCPConfig` as the last parameter:

```go
func New(
	taskSvc *service.TaskService,
	tagSvc *service.TagService,
	relationSvc *service.RelationService,
	projectSvc *service.ProjectService,
	workflowSvc *service.WorkflowService,
	version string,
	cfg config.MCPConfig,
) *Server {
	s := &Server{
		taskSvc:        taskSvc,
		tagSvc:         tagSvc,
		relationSvc:    relationSvc,
		projectSvc:     projectSvc,
		workflowSvc:    workflowSvc,
		cfg:            cfg,
		toolGroups:     make(map[string]string),
		resourceGroups: make(map[string]string),
	}

	s.server = server.NewMCPServer(
		"tusk",
		version,
		server.WithToolCapabilities(false),
		server.WithResourceCapabilities(false, false),
		server.WithRecovery(),
		server.WithInstructions(serverInstructions),
	)

	s.registerTools()
	s.registerResources()

	return s
}
```

- [ ] **Step 4: Fix the existing test**

Update `TestNewServer` in `internal/mcp/server_test.go` to pass the new parameter:

```go
func TestNewServer(t *testing.T) {
	s := New(nil, nil, nil, nil, nil, "test", config.MCPConfig{})
	if s == nil {
		t.Fatal("New() returned nil")
	}
	if s.server == nil {
		t.Fatal("New() did not initialize internal MCP server")
	}
}
```

- [ ] **Step 5: Fix the call site in app.go**

In `internal/tui/app.go`, line 148 currently reads:

```go
mcpServer := tuskmcp.New(taskSvc, tagSvc, relationSvc, projectSvc, a.workflowSvc, vi.Version)
```

Update it to pass an empty config (Phase 3 will wire the real config):

```go
mcpServer := tuskmcp.New(taskSvc, tagSvc, relationSvc, projectSvc, a.workflowSvc, vi.Version, config.MCPConfig{})
```

Add the import `"github.com/germanamz/tusk/internal/config"` to the import block in `internal/tui/app.go`.

- [ ] **Step 6: Run all tests**

```bash
go test ./internal/mcp/ ./internal/tui/ -v
```

Expected: all PASS

- [ ] **Step 7: Commit**

```bash
git add internal/mcp/server.go internal/mcp/server_test.go internal/tui/app.go
git commit -m "feat(mcp): add MCPConfig parameter to server constructor"
```

---

### Task 2: Add filtering helpers and group-aware tool registration

**Files:**
- Modify: `internal/mcp/server.go` (add helper methods)
- Modify: `internal/mcp/server_test.go` (add filtering tests)

- [ ] **Step 1: Write the filtering tests**

Add to `internal/mcp/server_test.go`:

```go
func TestToolFiltering_DisabledTool(t *testing.T) {
	cfg := config.MCPConfig{
		DisabledTools: []string{"tusk_task_delete"},
	}
	s := New(nil, nil, nil, nil, nil, "test", cfg)

	// tusk_task_delete should be disabled
	if s.isToolEnabled("tusk_task_delete", "task") {
		t.Error("tusk_task_delete should be disabled")
	}
	// Other tools should be enabled
	if !s.isToolEnabled("tusk_task_create", "task") {
		t.Error("tusk_task_create should be enabled")
	}
}

func TestToolFiltering_DisabledGroup(t *testing.T) {
	cfg := config.MCPConfig{
		DisabledToolGroups: []string{"relation"},
	}
	s := New(nil, nil, nil, nil, nil, "test", cfg)

	if s.isToolEnabled("tusk_relation_add", "relation") {
		t.Error("tusk_relation_add should be disabled (group 'relation' disabled)")
	}
	if s.isToolEnabled("tusk_relation_remove", "relation") {
		t.Error("tusk_relation_remove should be disabled (group 'relation' disabled)")
	}
	if !s.isToolEnabled("tusk_task_create", "task") {
		t.Error("tusk_task_create should be enabled (group 'task' not disabled)")
	}
}

func TestResourceFiltering_DisabledResource(t *testing.T) {
	cfg := config.MCPConfig{
		DisabledResources: []string{"tusk://projects/{name}/workflow"},
	}
	s := New(nil, nil, nil, nil, nil, "test", cfg)

	if s.isResourceEnabled("tusk://projects/{name}/workflow", "workflow") {
		t.Error("workflow resource should be disabled")
	}
	if !s.isResourceEnabled("tusk://tasks/{short_id}", "task") {
		t.Error("task resource should be enabled")
	}
}

func TestResourceFiltering_DisabledGroup(t *testing.T) {
	cfg := config.MCPConfig{
		DisabledResourceGroups: []string{"workflow"},
	}
	s := New(nil, nil, nil, nil, nil, "test", cfg)

	if s.isResourceEnabled("tusk://projects/{name}/workflow", "workflow") {
		t.Error("workflow resource should be disabled (group disabled)")
	}
	if !s.isResourceEnabled("tusk://projects/{name}", "project") {
		t.Error("project resource should be enabled")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test -v ./internal/mcp/ -run "TestToolFiltering|TestResourceFiltering"
```

Expected: compilation error — `isToolEnabled` and `isResourceEnabled` don't exist.

- [ ] **Step 3: Implement the filtering helpers**

Add these methods to `internal/mcp/server.go`, after the `New` function and before `registerTools`:

```go
// isToolEnabled returns true if the tool should be registered based on config.
func (s *Server) isToolEnabled(name, group string) bool {
	return !containsStr(s.cfg.DisabledTools, name) &&
		!containsStr(s.cfg.DisabledToolGroups, group)
}

// isResourceEnabled returns true if the resource should be registered based on config.
func (s *Server) isResourceEnabled(uriTemplate, group string) bool {
	return !containsStr(s.cfg.DisabledResources, uriTemplate) &&
		!containsStr(s.cfg.DisabledResourceGroups, group)
}

// containsStr returns true if slice contains the given string.
func containsStr(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: Run filtering tests**

```bash
go test -v ./internal/mcp/ -run "TestToolFiltering|TestResourceFiltering"
```

Expected: all PASS

- [ ] **Step 5: Commit**

```bash
git add internal/mcp/server.go internal/mcp/server_test.go
git commit -m "feat(mcp): add tool and resource filtering helpers"
```

---

### Task 3: Wire filtering into tool and resource registration

**Files:**
- Modify: `internal/mcp/server.go` (`registerTools` and `registerResources` methods)
- Modify: `internal/mcp/server_test.go`

Currently `registerTools()` (in `internal/mcp/server.go:52-314`) calls `s.server.AddTool()` directly for each tool. We need to wrap each registration with a group check.

- [ ] **Step 1: Write an integration test**

This test verifies that disabled tools are actually excluded from the MCP server's tool list. The `mcp-go` library's `MCPServer` type has a `ListTools` method we can call via JSON-RPC. But for a simpler approach, we'll count registered tools by creating servers with and without disabled tools and comparing.

Add to `internal/mcp/server_test.go`:

```go
func TestRegisterTools_FiltersDisabledTools(t *testing.T) {
	// Server with no disabled tools — all 13 tools registered.
	full := New(nil, nil, nil, nil, nil, "test", config.MCPConfig{})

	// Server with "relation" group disabled — should have 11 tools (13 minus 2 relation tools).
	filtered := New(nil, nil, nil, nil, nil, "test", config.MCPConfig{
		DisabledToolGroups: []string{"relation"},
	})

	// Verify by checking the toolGroups map sizes.
	// Full server should have 13 entries, filtered should have 11.
	if len(full.toolGroups) != 13 {
		t.Errorf("full server: expected 13 tools, got %d", len(full.toolGroups))
	}
	if len(filtered.toolGroups) != 11 {
		t.Errorf("filtered server: expected 11 tools (relation group disabled), got %d", len(filtered.toolGroups))
	}
}

func TestRegisterResources_FiltersDisabledResources(t *testing.T) {
	full := New(nil, nil, nil, nil, nil, "test", config.MCPConfig{})
	filtered := New(nil, nil, nil, nil, nil, "test", config.MCPConfig{
		DisabledResourceGroups: []string{"workflow"},
	})

	if len(full.resourceGroups) != 3 {
		t.Errorf("full server: expected 3 resources, got %d", len(full.resourceGroups))
	}
	if len(filtered.resourceGroups) != 2 {
		t.Errorf("filtered server: expected 2 resources (workflow disabled), got %d", len(filtered.resourceGroups))
	}
}
```

- [ ] **Step 2: Run tests to see them fail**

```bash
go test -v ./internal/mcp/ -run "TestRegisterTools_Filters|TestRegisterResources_Filters"
```

Expected: FAIL — `toolGroups` map is empty (nothing populates it yet).

- [ ] **Step 3: Create a helper method for group-aware registration**

Add to `internal/mcp/server.go`, before `registerTools`:

```go
// addTool registers a tool with the MCP server if it's enabled by config.
// It also records the tool's group in the toolGroups map.
func (s *Server) addTool(group string, tool mcp.Tool, handler server.ToolHandlerFunc) {
	name := tool.Name
	if !s.isToolEnabled(name, group) {
		return
	}
	s.toolGroups[name] = group
	s.server.AddTool(tool, handler)
}

// addResource registers a resource template if it's enabled by config.
func (s *Server) addResource(group string, tmpl mcp.ResourceTemplate, handler server.ResourceTemplateHandlerFunc) {
	uri := tmpl.URITemplate.Raw()
	if !s.isResourceEnabled(uri, group) {
		return
	}
	s.resourceGroups[uri] = group
	s.server.AddResourceTemplate(tmpl, handler)
}
```

**Note on `tmpl.URITemplate.Raw()`:** The `mcp-go` library's `ResourceTemplate` has a `URITemplate` field. Check the actual field name — it may be a `string` directly or a structured type. Look at the import and the existing `registerResources` code in `internal/mcp/resources.go:14` where `mcp.NewResourceTemplate("tusk://tasks/{short_id}", ...)` is called. The first argument is a string URI template. The `ResourceTemplate` struct likely stores it. You may need to inspect the `mcp-go` library to find the correct field. If `URITemplate` is already a string, use it directly:

```go
uri := string(tmpl.URITemplate)
```

If it's a structured type with a `String()` or `Raw()` method, use that instead. Adjust accordingly.

- [ ] **Step 4: Replace all `s.server.AddTool` calls with `s.addTool`**

In `internal/mcp/server.go`, the `registerTools` method currently has 13 `s.server.AddTool(...)` calls. Replace each one with `s.addTool(group, ...)` using these group assignments:

| Call | Group |
|------|-------|
| `tusk_task_create` | `"task"` |
| `tusk_task_get` | `"task"` |
| `tusk_task_list` | `"task"` |
| `tusk_task_modify` | `"task"` |
| `tusk_task_start` | `"task"` |
| `tusk_task_done` | `"task"` |
| `tusk_task_delete` | `"task"` |
| `tusk_task_annotate` | `"task"` |
| `tusk_task_tree` | `"task"` |
| `tusk_relation_add` | `"relation"` |
| `tusk_relation_remove` | `"relation"` |
| `tusk_project_list` | `"project"` |
| `tusk_project_create` | `"project"` |

For example, the first call changes from:

```go
s.server.AddTool(
    mcp.NewTool("tusk_task_create", ...),
    s.handleTaskCreate,
)
```

To:

```go
s.addTool("task",
    mcp.NewTool("tusk_task_create", ...),
    s.handleTaskCreate,
)
```

Repeat for all 13 tools. The tool definition arguments (the `mcp.NewTool(...)` call and its options) stay exactly the same — only the outer `s.server.AddTool(` changes to `s.addTool("group",`.

- [ ] **Step 5: Replace all `s.server.AddResourceTemplate` calls with `s.addResource`**

In `internal/mcp/resources.go`, the `registerResources` method has 3 `s.server.AddResourceTemplate(...)` calls. Replace each:

| Resource | Group |
|----------|-------|
| `tusk://tasks/{short_id}` | `"task"` |
| `tusk://projects/{name}` | `"project"` |
| `tusk://projects/{name}/workflow` | `"workflow"` |

For example:

```go
s.server.AddResourceTemplate(
    mcp.NewResourceTemplate("tusk://tasks/{short_id}", ...),
    s.handleTaskResource,
)
```

Becomes:

```go
s.addResource("task",
    mcp.NewResourceTemplate("tusk://tasks/{short_id}", ...),
    s.handleTaskResource,
)
```

- [ ] **Step 6: Run the integration tests**

```bash
go test -v ./internal/mcp/ -run "TestRegisterTools_Filters|TestRegisterResources_Filters"
```

Expected: PASS

- [ ] **Step 7: Run all MCP tests to catch regressions**

```bash
go test -v ./internal/mcp/
```

Expected: all PASS

- [ ] **Step 8: Commit**

```bash
git add internal/mcp/server.go internal/mcp/resources.go internal/mcp/server_test.go
git commit -m "feat(mcp): filter tools and resources by config disable lists"
```

---

### Task 4: Add validation warnings for unknown disable entries

**Files:**
- Modify: `internal/mcp/server.go`
- Modify: `internal/mcp/server_test.go`

When a user puts a typo in their config (e.g., `disabled_tools = ["tusk_tak_delete"]`), they should see a warning. We'll validate after registration and write warnings to stderr.

- [ ] **Step 1: Write the test**

Add to `internal/mcp/server_test.go`:

```go
func TestValidation_UnknownEntries(t *testing.T) {
	// Capture stderr output.
	var buf strings.Builder

	cfg := config.MCPConfig{
		DisabledTools:          []string{"tusk_nonexistent_tool"},
		DisabledToolGroups:     []string{"nonexistent_group"},
		DisabledResources:      []string{"tusk://nonexistent/resource"},
		DisabledResourceGroups: []string{"nonexistent_res_group"},
	}

	s := &Server{
		cfg:            cfg,
		toolGroups:     map[string]string{"tusk_task_create": "task"},
		resourceGroups: map[string]string{"tusk://tasks/{short_id}": "task"},
	}

	s.validateConfig(&buf)

	output := buf.String()
	if !strings.Contains(output, "tusk_nonexistent_tool") {
		t.Errorf("expected warning about unknown tool, got: %s", output)
	}
	if !strings.Contains(output, "nonexistent_group") {
		t.Errorf("expected warning about unknown tool group, got: %s", output)
	}
	if !strings.Contains(output, "tusk://nonexistent/resource") {
		t.Errorf("expected warning about unknown resource, got: %s", output)
	}
	if !strings.Contains(output, "nonexistent_res_group") {
		t.Errorf("expected warning about unknown resource group, got: %s", output)
	}
}

func TestValidation_NoWarningsForValidEntries(t *testing.T) {
	var buf strings.Builder

	cfg := config.MCPConfig{
		DisabledToolGroups:     []string{"relation"},
		DisabledResourceGroups: []string{"workflow"},
	}

	s := &Server{
		cfg: cfg,
		toolGroups: map[string]string{
			"tusk_task_create":   "task",
			"tusk_relation_add":  "relation",
		},
		resourceGroups: map[string]string{
			"tusk://tasks/{short_id}":              "task",
			"tusk://projects/{name}/workflow":       "workflow",
		},
	}

	s.validateConfig(&buf)

	if buf.Len() != 0 {
		t.Errorf("expected no warnings, got: %s", buf.String())
	}
}
```

Add `"strings"` to the import block if not already present.

- [ ] **Step 2: Run tests to see them fail**

```bash
go test -v ./internal/mcp/ -run "TestValidation"
```

Expected: compilation error — `validateConfig` doesn't exist.

- [ ] **Step 3: Implement validateConfig**

Add to `internal/mcp/server.go`:

```go
// validateConfig warns about disable list entries that don't match any
// registered tool or resource. Writes warnings to w (typically os.Stderr).
func (s *Server) validateConfig(w io.Writer) {
	// Collect known tool names and groups.
	knownTools := make(map[string]bool, len(s.toolGroups))
	knownToolGroups := make(map[string]bool)
	for name, group := range s.toolGroups {
		knownTools[name] = true
		knownToolGroups[group] = true
	}

	// Collect known resource URIs and groups.
	knownResources := make(map[string]bool, len(s.resourceGroups))
	knownResourceGroups := make(map[string]bool)
	for uri, group := range s.resourceGroups {
		knownResources[uri] = true
		knownResourceGroups[group] = true
	}

	// Also consider disabled entries as "known" groups/names —
	// a disabled tool won't appear in toolGroups, so we need to
	// check against the full set of valid names.
	// We hardcode the valid sets here.
	validToolNames := map[string]bool{
		"tusk_task_create":   true,
		"tusk_task_get":      true,
		"tusk_task_list":     true,
		"tusk_task_modify":   true,
		"tusk_task_start":    true,
		"tusk_task_done":     true,
		"tusk_task_delete":   true,
		"tusk_task_annotate": true,
		"tusk_task_tree":     true,
		"tusk_relation_add":    true,
		"tusk_relation_remove": true,
		"tusk_project_list":   true,
		"tusk_project_create": true,
	}
	validToolGroups := map[string]bool{
		"task": true, "relation": true, "project": true,
	}
	validResourceURIs := map[string]bool{
		"tusk://tasks/{short_id}":          true,
		"tusk://projects/{name}":           true,
		"tusk://projects/{name}/workflow":  true,
	}
	validResourceGroups := map[string]bool{
		"task": true, "project": true, "workflow": true,
	}

	for _, name := range s.cfg.DisabledTools {
		if !validToolNames[name] {
			fmt.Fprintf(w, "tusk: config warning: disabled_tools contains unknown tool %q\n", name)
		}
	}
	for _, group := range s.cfg.DisabledToolGroups {
		if !validToolGroups[group] {
			fmt.Fprintf(w, "tusk: config warning: disabled_tool_groups contains unknown group %q\n", group)
		}
	}
	for _, uri := range s.cfg.DisabledResources {
		if !validResourceURIs[uri] {
			fmt.Fprintf(w, "tusk: config warning: disabled_resources contains unknown resource %q\n", uri)
		}
	}
	for _, group := range s.cfg.DisabledResourceGroups {
		if !validResourceGroups[group] {
			fmt.Fprintf(w, "tusk: config warning: disabled_resource_groups contains unknown group %q\n", group)
		}
	}
}
```

Add `"io"` and `"fmt"` to the import block if not already present.

- [ ] **Step 4: Call validateConfig from the constructor**

In the `New` function in `internal/mcp/server.go`, add this line after `s.registerResources()`:

```go
s.registerTools()
s.registerResources()
s.validateConfig(os.Stderr)
```

Add `"os"` to the import block.

- [ ] **Step 5: Run all MCP tests**

```bash
go test -v ./internal/mcp/
```

Expected: all PASS

- [ ] **Step 6: Run E2E tests to verify no regressions**

```bash
go test -v ./tests/e2e/ -run TestMCP -timeout 60s
```

Expected: all PASS

- [ ] **Step 7: Commit**

```bash
git add internal/mcp/server.go internal/mcp/server_test.go
git commit -m "feat(mcp): validate config disable lists and warn on unknown entries"
```
