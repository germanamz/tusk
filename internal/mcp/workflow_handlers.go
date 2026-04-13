package mcp

import (
	"context"
	"fmt"

	"github.com/germanamz/tusk/config"
	"github.com/mark3labs/mcp-go/mcp"
)

type statusSpec struct {
	Name  string   `json:"name"`
	Roles []string `json:"roles"`
}

type transitionSpec struct {
	From string `json:"from"`
	To   string `json:"to"`
}

func parseStatusSpecs(raw any) ([]statusSpec, error) {
	if raw == nil {
		return nil, nil
	}
	items, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("expected array")
	}
	out := make([]statusSpec, 0, len(items))
	for i, item := range items {
		obj, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("item %d: expected object", i)
		}
		name, _ := obj["name"].(string)
		if name == "" {
			return nil, fmt.Errorf("item %d: name is required", i)
		}
		var roles []string
		if rawRoles, ok := obj["roles"].([]any); ok {
			for j, r := range rawRoles {
				s, ok := r.(string)
				if !ok {
					return nil, fmt.Errorf("item %d role %d: expected string", i, j)
				}
				roles = append(roles, s)
			}
		}
		out = append(out, statusSpec{Name: name, Roles: roles})
	}
	return out, nil
}

func parseTransitionSpecs(raw any) ([]transitionSpec, error) {
	if raw == nil {
		return nil, nil
	}
	items, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("expected array")
	}
	out := make([]transitionSpec, 0, len(items))
	for i, item := range items {
		obj, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("item %d: expected object", i)
		}
		from, _ := obj["from"].(string)
		to, _ := obj["to"].(string)
		if from == "" || to == "" {
			return nil, fmt.Errorf("item %d: from and to are required", i)
		}
		out = append(out, transitionSpec{From: from, To: to})
	}
	return out, nil
}

func statusesToConfig(specs []statusSpec) map[string]config.StatusConfig {
	out := make(map[string]config.StatusConfig, len(specs))
	for _, s := range specs {
		out[s.Name] = config.StatusConfig{Roles: append([]string(nil), s.Roles...)}
	}
	return out
}

func transitionsToConfig(specs []transitionSpec) []config.WorkflowTransitionConfig {
	out := make([]config.WorkflowTransitionConfig, 0, len(specs))
	for _, t := range specs {
		out = append(out, config.WorkflowTransitionConfig{From: t.From, To: t.To})
	}
	return out
}

func (s *Server) handleWorkflowCreate(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	name, err := req.RequireString("name")
	if err != nil {
		return mcp.NewToolResultError("name is required"), nil
	}
	args := req.GetArguments()

	statuses, err := parseStatusSpecs(args["statuses"])
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("statuses: %v", err)), nil
	}
	if len(statuses) == 0 {
		return mcp.NewToolResultError("statuses is required"), nil
	}
	transitions, err := parseTransitionSpecs(args["transitions"])
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("transitions: %v", err)), nil
	}

	wf := config.WorkflowConfig{
		Statuses:    statusesToConfig(statuses),
		Transitions: transitionsToConfig(transitions),
	}

	path, err := config.ConfigFilePath(s.loadOpts...)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("resolving config file: %v", err)), nil
	}
	if err := config.CreateWorkflow(path, name, wf); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if err := s.reloadConfig(ctx); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("reloading config: %v", err)), nil
	}

	return toolResultJSON(map[string]any{"ok": true, "name": name, "active_file": path})
}

func (s *Server) handleWorkflowModify(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return mcp.NewToolResultError("not implemented"), nil
}

func (s *Server) handleWorkflowDelete(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return mcp.NewToolResultError("not implemented"), nil
}

// HandleWorkflowCreateForTest exposes handleWorkflowCreate for internal tests.
func (s *Server) HandleWorkflowCreateForTest(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return s.handleWorkflowCreate(ctx, req)
}
