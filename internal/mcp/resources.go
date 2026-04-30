package mcp

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
)

// registerResources registers all MCP resource templates.
func (server *Server) registerResources() {
	// tusk://tasks/{short_id}
	server.addResource("task",
		mcp.NewResourceTemplate(
			"tusk://tasks/{short_id}",
			"Task Detail",
			mcp.WithTemplateDescription("Full task details including tags, relations, and annotations"),
			mcp.WithTemplateMIMEType("application/json"),
		),
		server.handleTaskResource,
	)

	// tusk://projects/{name}
	server.addResource("project",
		mcp.NewResourceTemplate(
			"tusk://projects/{name}",
			"Project Detail",
			mcp.WithTemplateDescription("Project details including settings"),
			mcp.WithTemplateMIMEType("application/json"),
		),
		server.handleProjectResource,
	)

	// tusk://projects/{name}/workflow
	server.addResource("workflow",
		mcp.NewResourceTemplate(
			"tusk://projects/{name}/workflow",
			"Project Workflow",
			mcp.WithTemplateDescription("Workflow statuses and allowed transitions for a project"),
			mcp.WithTemplateMIMEType("application/json"),
		),
		server.handleWorkflowResource,
	)
}

// handleTaskResource serves tusk://tasks/{short_id}.
// Returns the same rich format as tusk_task_get (task + tags + relations + annotations).
func (server *Server) handleTaskResource(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
	shortID := extractURIParam(request.Params.URI, "tusk://tasks/")
	if shortID == "" {
		return nil, &resourceError{msg: "missing short_id in URI"}
	}

	resp, buildErr := server.buildTaskGetResponse(ctx, shortID)

	if buildErr != nil {
		return nil, buildErr
	}

	jsonBytes, marshalErr := json.MarshalIndent(resp, "", "  ")

	if marshalErr != nil {
		return nil, marshalErr
	}

	return []mcp.ResourceContents{
		mcp.TextResourceContents{
			URI:      request.Params.URI,
			MIMEType: "application/json",
			Text:     string(jsonBytes),
		},
	}, nil
}

// handleProjectResource serves tusk://projects/{name}.
func (server *Server) handleProjectResource(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
	name := extractURIParam(request.Params.URI, "tusk://projects/")
	if idx := strings.Index(name, "/"); idx >= 0 {
		name = name[:idx]
	}
	if name == "" {
		return nil, &resourceError{msg: "missing project name in URI"}
	}

	project, projectErr := server.projectSvc.GetByName(ctx, name)

	if projectErr != nil {
		return nil, projectErr
	}

	workflow, workflowErr := server.workflowSvc.GetByID(ctx, project.WorkflowID)

	if workflowErr != nil {
		return nil, workflowErr
	}

	jsonBytes, marshalErr := json.MarshalIndent(server.toProjectResponse(project, workflow.Name), "", "  ")

	if marshalErr != nil {
		return nil, marshalErr
	}

	return []mcp.ResourceContents{
		mcp.TextResourceContents{
			URI:      request.Params.URI,
			MIMEType: "application/json",
			Text:     string(jsonBytes),
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
func (server *Server) handleWorkflowResource(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
	uri := request.Params.URI
	name := extractURIParam(uri, "tusk://projects/")
	if idx := strings.Index(name, "/"); idx >= 0 {
		name = name[:idx]
	}
	if name == "" {
		return nil, &resourceError{msg: "missing project name in URI"}
	}

	project, projectErr := server.projectSvc.GetByName(ctx, name)

	if projectErr != nil {
		return nil, projectErr
	}

	workflow, workflowErr := server.workflowSvc.GetByID(ctx, project.WorkflowID)

	if workflowErr != nil {
		return nil, workflowErr
	}

	statuses, statusesErr := server.workflowSvc.GetStatuses(ctx, workflow.Name)

	if statusesErr != nil {
		return nil, statusesErr
	}

	transitions, transitionsErr := server.workflowSvc.GetTransitions(ctx, workflow.Name)

	if transitionsErr != nil {
		return nil, transitionsErr
	}

	resp := workflowResponse{
		ProjectName: project.Name,
		Workflow:    workflow.Name,
		Statuses:    statuses,
		Transitions: make([]transitionResponse, len(transitions)),
	}
	for index, transition := range transitions {
		resp.Transitions[index] = transitionResponse{From: transition.FromStatus, To: transition.ToStatus}
	}

	jsonBytes, marshalErr := json.MarshalIndent(resp, "", "  ")

	if marshalErr != nil {
		return nil, marshalErr
	}

	return []mcp.ResourceContents{
		mcp.TextResourceContents{
			URI:      request.Params.URI,
			MIMEType: "application/json",
			Text:     string(jsonBytes),
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

func (re *resourceError) Error() string {
	return re.msg
}
