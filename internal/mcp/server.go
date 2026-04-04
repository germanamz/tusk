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
}

// Serve starts the MCP server using stdio transport.
// This blocks until the transport is closed (e.g., stdin EOF).
func (s *Server) Serve() error {
	return server.ServeStdio(s.server)
}

const serverInstructions = `Tusk is a task management system. You can create, list, modify, and transition tasks through workflow statuses. Tasks support parent-child hierarchy, typed relations (blocks, relates_to, duplicates), tags, annotations, and projects. All mutation tools require a version parameter for optimistic locking — fetch the task first to get the current version.`
