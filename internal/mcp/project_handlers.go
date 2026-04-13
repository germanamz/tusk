package mcp

import (
	"context"
	"fmt"

	"github.com/germanamz/tusk/filter"
	"github.com/mark3labs/mcp-go/mcp"
)

// countTasksForProject returns the number of tasks referencing the given
// project name. Mirrors the CLI-side helper so the MCP project_delete
// handler can supply a TaskRefChecker to config.DeleteProject.
//
//nolint:unused
func (s *Server) countTasksForProject(ctx context.Context, projectName string) (int, error) {
	expr, parseErrs := filter.ParseExpr(fmt.Sprintf("project=%s", projectName))
	if len(parseErrs) > 0 {
		return 0, fmt.Errorf("building filter: %s", filter.FormatErrors(parseErrs))
	}
	resolver := s.newResolver(ctx)
	filterExpr, resolveErrs := resolver.ResolveExpr(ctx, expr)
	if len(resolveErrs) > 0 {
		return 0, resolveErrs[0]
	}
	tasks, err := s.taskSvc.List(ctx, filterExpr)
	if err != nil {
		return 0, err
	}
	return len(tasks), nil
}

func (s *Server) handleProjectCreate(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return mcp.NewToolResultError("not implemented"), nil
}

func (s *Server) handleProjectModify(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return mcp.NewToolResultError("not implemented"), nil
}

func (s *Server) handleProjectDelete(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return mcp.NewToolResultError("not implemented"), nil
}
