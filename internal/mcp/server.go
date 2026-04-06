package mcp

import (
	"errors"
	"fmt"
	"slices"

	"github.com/germanamz/tusk/internal/config"
	"github.com/germanamz/tusk/internal/service"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// Server wraps an MCP server that exposes tusk capabilities as tools and resources.
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

// New creates a new MCP Server and registers all tools and resources.
// Returns an error if the config contains unknown disable list entries.
func New(
	taskSvc *service.TaskService,
	tagSvc *service.TagService,
	relationSvc *service.RelationService,
	projectSvc *service.ProjectService,
	workflowSvc *service.WorkflowService,
	version string,
	cfg config.MCPConfig,
) (*Server, error) {
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

	if err := s.validateConfig(); err != nil {
		return nil, err
	}

	return s, nil
}

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
	return slices.Contains(slice, s)
}

// validateConfig returns an error if any disable list entry does not match
// a known tool, resource, or group.
func (s *Server) validateConfig() error {
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
		"tusk_task_next":       true,
		"tusk_relation_add":    true,
		"tusk_relation_remove": true,
		"tusk_project_list":    true,
		"tusk_workflow_list":   true,
	}
	validToolGroups := map[string]bool{
		"task": true, "relation": true, "project": true, "workflow": true,
	}
	validResourceURIs := map[string]bool{
		"tusk://tasks/{short_id}":         true,
		"tusk://projects/{name}":          true,
		"tusk://projects/{name}/workflow": true,
	}
	validResourceGroups := map[string]bool{
		"task": true, "project": true, "workflow": true,
	}

	var errs []error
	for _, name := range s.cfg.DisabledTools {
		if !validToolNames[name] {
			errs = append(errs, fmt.Errorf("disabled_tools: unknown tool %q", name))
		}
	}
	for _, group := range s.cfg.DisabledToolGroups {
		if !validToolGroups[group] {
			errs = append(errs, fmt.Errorf("disabled_tool_groups: unknown group %q", group))
		}
	}
	for _, uri := range s.cfg.DisabledResources {
		if !validResourceURIs[uri] {
			errs = append(errs, fmt.Errorf("disabled_resources: unknown resource %q", uri))
		}
	}
	for _, group := range s.cfg.DisabledResourceGroups {
		if !validResourceGroups[group] {
			errs = append(errs, fmt.Errorf("disabled_resource_groups: unknown group %q", group))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("invalid mcp config: %w", errors.Join(errs...))
	}
	return nil
}

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

// registerTools registers all MCP tool definitions and their handlers.
func (s *Server) registerTools() {
	s.addTool("task",
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
			mcp.WithObject("uda",
				mcp.Description("User-defined attributes as key-value pairs (all values must be strings)"),
				mcp.AdditionalProperties(map[string]any{"type": "string"}),
			),
		),
		s.handleTaskCreate,
	)

	s.addTool("task",
		mcp.NewTool("tusk_task_get",
			mcp.WithDescription("Get a task with full details including tags, relations, and annotations"),
			mcp.WithString("short_id",
				mcp.Required(),
				mcp.Description("Task short ID (8+ hex characters)"),
			),
		),
		s.handleTaskGet,
	)

	s.addTool("task",
		mcp.NewTool("tusk_task_list",
			mcp.WithDescription("List tasks with optional filters"),
			mcp.WithString("filter",
				mcp.Description("Filter expression with AND/OR/NOT/parentheses support (e.g. 'status:active OR +urgent'). When provided, other filter parameters are ignored."),
			),
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
			mcp.WithString("title",
				mcp.Description("Filter tasks whose title contains this substring (case-insensitive)"),
			),
			mcp.WithString("description",
				mcp.Description("Filter tasks whose description contains this substring (case-insensitive)"),
			),
		),
		s.handleTaskList,
	)

	s.addTool("task",
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
			mcp.WithObject("uda",
				mcp.Description("UDA key-value pairs to merge. Empty string value removes the key."),
				mcp.AdditionalProperties(map[string]any{"type": "string"}),
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

	s.addTool("task",
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

	s.addTool("task",
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

	s.addTool("task",
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

	s.addTool("task",
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

	s.addTool("relation",
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

	s.addTool("relation",
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

	s.addTool("project",
		mcp.NewTool("tusk_project_list",
			mcp.WithDescription("List all projects"),
		),
		s.handleProjectList,
	)

	s.addTool("task",
		mcp.NewTool("tusk_task_tree",
			mcp.WithDescription("Get tasks as a nested tree hierarchy"),
			mcp.WithString("short_id",
				mcp.Description("Root task short_id (omit for full tree)"),
			),
			mcp.WithBoolean("include_deleted",
				mcp.Description("Include deleted tasks in the tree"),
			),
		),
		s.handleTaskTree,
	)

	s.addTool("task", mcp.NewTool(
		"tusk_task_next",
		mcp.WithDescription("Get the highest-urgency actionable task (not waiting, not blocked)"),
	), s.handleTaskNext)

	s.addTool("workflow",
		mcp.NewTool("tusk_workflow_list",
			mcp.WithDescription("List all workflows with their statuses, transitions, and referencing projects"),
		),
		s.handleWorkflowList,
	)
}

// Serve starts the MCP server using stdio transport.
// This blocks until the transport is closed (e.g., stdin EOF).
func (s *Server) Serve() error {
	return server.ServeStdio(s.server)
}

const serverInstructions = `Tusk is a task management system. You can create, list, modify, and transition tasks through workflow statuses. Tasks support parent-child hierarchy, typed relations (blocks, relates_to, duplicates), tags, annotations, and projects. All mutation tools require a version parameter for optimistic locking — fetch the task first to get the current version.`
