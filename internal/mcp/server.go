package mcp

import (
	"github.com/germanamz/tusk/internal/service"
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

	return s
}

// Serve starts the MCP server using stdio transport.
// This blocks until the transport is closed (e.g., stdin EOF).
func (s *Server) Serve() error {
	return server.ServeStdio(s.server)
}

const serverInstructions = `Tusk is a task management system. You can create, list, modify, and transition tasks through workflow statuses. Tasks support parent-child hierarchy, typed relations (blocks, relates_to, duplicates), tags, annotations, and projects. All mutation tools require a version parameter for optimistic locking — fetch the task first to get the current version.`
