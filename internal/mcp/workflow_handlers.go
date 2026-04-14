package mcp

import (
	"context"
	"errors"
	"fmt"

	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/service"
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

func statusesToDomain(specs []statusSpec) map[string]domain.StatusConfig {
	out := make(map[string]domain.StatusConfig, len(specs))
	for _, s := range specs {
		roles := make([]domain.StatusRole, len(s.Roles))
		for i, r := range s.Roles {
			roles[i] = domain.StatusRole(r)
		}
		out[s.Name] = domain.StatusConfig{Roles: roles}
	}
	return out
}

func transitionsToDomain(specs []transitionSpec) []domain.WorkflowTransition {
	out := make([]domain.WorkflowTransition, 0, len(specs))
	for _, t := range specs {
		out = append(out, domain.WorkflowTransition{FromStatus: t.From, ToStatus: t.To})
	}
	return out
}

func workflowToMap(w *domain.Workflow) map[string]any {
	statuses := make([]map[string]any, 0, len(w.Statuses))
	for name, sc := range w.Statuses {
		roles := make([]string, len(sc.Roles))
		for i, r := range sc.Roles {
			roles[i] = string(r)
		}
		statuses = append(statuses, map[string]any{"name": name, "roles": roles})
	}
	transitions := make([]map[string]any, 0, len(w.Transitions))
	for _, t := range w.Transitions {
		transitions = append(transitions, map[string]any{"from": t.FromStatus, "to": t.ToStatus})
	}
	return map[string]any{
		"id":          w.ID.String(),
		"name":        w.Name,
		"version":     w.Version,
		"statuses":    statuses,
		"transitions": transitions,
	}
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

	wf, err := s.workflowSvc.Create(ctx, service.CreateWorkflowInput{
		Name:        name,
		Statuses:    statusesToDomain(statuses),
		Transitions: transitionsToDomain(transitions),
	})
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return toolResultJSON(map[string]any{"ok": true, "workflow": workflowToMap(wf)})
}

func (s *Server) handleWorkflowModify(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	name, err := req.RequireString("name")
	if err != nil {
		return mcp.NewToolResultError("name is required"), nil
	}
	versionF, err := req.RequireFloat("version")
	if err != nil {
		return mcp.NewToolResultError("version is required"), nil
	}
	args := req.GetArguments()

	addStatuses, err := parseStatusSpecs(args["add_statuses"])
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("add_statuses: %v", err)), nil
	}
	setStatuses, err := parseStatusSpecs(args["set_statuses"])
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("set_statuses: %v", err)), nil
	}
	addTrans, err := parseTransitionSpecs(args["add_transitions"])
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("add_transitions: %v", err)), nil
	}
	removeTrans, err := parseTransitionSpecs(args["remove_transitions"])
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("remove_transitions: %v", err)), nil
	}

	var removeStatuses []string
	if raw, ok := args["remove_statuses"].([]any); ok {
		for i, r := range raw {
			str, ok := r.(string)
			if !ok {
				return mcp.NewToolResultError(fmt.Sprintf("remove_statuses[%d]: expected string", i)), nil
			}
			removeStatuses = append(removeStatuses, str)
		}
	}

	wf, err := s.workflowSvc.Modify(ctx, service.ModifyWorkflowInput{
		Name:              name,
		ExpectedVersion:   int(versionF),
		AddStatuses:       statusesToDomain(addStatuses),
		SetStatuses:       statusesToDomain(setStatuses),
		RemoveStatuses:    removeStatuses,
		AddTransitions:    transitionsToDomain(addTrans),
		RemoveTransitions: transitionsToDomain(removeTrans),
	})
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return toolResultJSON(map[string]any{"ok": true, "workflow": workflowToMap(wf)})
}

// HandleWorkflowModifyForTest exposes handleWorkflowModify for internal tests.
func (s *Server) HandleWorkflowModifyForTest(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return s.handleWorkflowModify(ctx, req)
}

func (s *Server) handleWorkflowDelete(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	name, err := req.RequireString("name")
	if err != nil {
		return mcp.NewToolResultError("name is required"), nil
	}
	versionF, err := req.RequireFloat("version")
	if err != nil {
		return mcp.NewToolResultError("version is required"), nil
	}

	wf, err := s.workflowSvc.GetByName(ctx, name)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return mcp.NewToolResultError(fmt.Sprintf("workflow %q: not found", name)), nil
		}
		return mcp.NewToolResultError(err.Error()), nil
	}

	if err := s.workflowSvc.Delete(ctx, wf.ID, int(versionF)); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return toolResultJSON(map[string]any{"ok": true, "name": name})
}

// HandleWorkflowDeleteForTest exposes handleWorkflowDelete for internal tests.
func (s *Server) HandleWorkflowDeleteForTest(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return s.handleWorkflowDelete(ctx, req)
}

// HandleWorkflowCreateForTest exposes handleWorkflowCreate for internal tests.
func (s *Server) HandleWorkflowCreateForTest(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return s.handleWorkflowCreate(ctx, req)
}
