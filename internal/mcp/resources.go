package mcp

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
)

// registerResources registers all MCP resource templates.
func (s *Server) registerResources() {
	// tusk://tasks/{short_id}
	s.addResource("task",
		mcp.NewResourceTemplate(
			"tusk://tasks/{short_id}",
			"Task Detail",
			mcp.WithTemplateDescription("Full task details including tags, relations, and annotations"),
			mcp.WithTemplateMIMEType("application/json"),
		),
		s.handleTaskResource,
	)

	// tusk://projects/{name}
	s.addResource("project",
		mcp.NewResourceTemplate(
			"tusk://projects/{name}",
			"Project Detail",
			mcp.WithTemplateDescription("Project details including settings"),
			mcp.WithTemplateMIMEType("application/json"),
		),
		s.handleProjectResource,
	)

	// tusk://projects/{name}/workflow
	s.addResource("workflow",
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
	shortID := extractURIParam(request.Params.URI, "tusk://tasks/")
	if shortID == "" {
		return nil, &resourceError{msg: "missing short_id in URI"}
	}

	resp, err := s.buildTaskGetResponse(ctx, shortID)
	if err != nil {
		return nil, err
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

	statuses, err := s.workflowSvc.GetStatuses(ctx, project.Workflow)
	if err != nil {
		return nil, err
	}

	transitions, err := s.workflowSvc.GetTransitions(ctx, project.Workflow)
	if err != nil {
		return nil, err
	}

	resp := workflowResponse{
		ProjectName: project.Name,
		Workflow:    project.Workflow,
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
