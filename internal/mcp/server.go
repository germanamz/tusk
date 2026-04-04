package mcp

import (
	"github.com/germanamz/tusk/internal/service"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// Server wraps an MCP server that exposes tusk capabilities as tools and resources.
type Server struct {
	taskSvc     *service.TaskService
	tagSvc      *service.TagService
	relationSvc *service.RelationService
	projectSvc  *service.ProjectService
	server      *server.MCPServer
}

// New creates a new MCP Server and registers all tools and resources.
func New(
	taskSvc *service.TaskService,
	tagSvc *service.TagService,
	relationSvc *service.RelationService,
	projectSvc *service.ProjectService,
	version string,
) *Server {
	s := &Server{
		taskSvc:     taskSvc,
		tagSvc:      tagSvc,
		relationSvc: relationSvc,
		projectSvc:  projectSvc,
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

	return s
}

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
}

// Serve starts the MCP server using stdio transport.
// This blocks until the transport is closed (e.g., stdin EOF).
func (s *Server) Serve() error {
	return server.ServeStdio(s.server)
}

const serverInstructions = `Tusk is a task management system. You can create, list, modify, and transition tasks through workflow statuses. Tasks support parent-child hierarchy, typed relations (blocks, relates_to, duplicates), tags, annotations, and projects. All mutation tools require a version parameter for optimistic locking — fetch the task first to get the current version.`
