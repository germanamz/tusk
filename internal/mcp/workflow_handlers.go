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
	for idx, item := range items {
		obj, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("item %d: expected object", idx)
		}
		name, _ := obj["name"].(string)
		if name == "" {
			return nil, fmt.Errorf("item %d: name is required", idx)
		}
		var roles []string
		if rawRoles, ok := obj["roles"].([]any); ok {
			for jdx, role := range rawRoles {
				roleStr, ok := role.(string)
				if !ok {
					return nil, fmt.Errorf("item %d role %d: expected string", idx, jdx)
				}
				roles = append(roles, roleStr)
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
	for idx, item := range items {
		obj, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("item %d: expected object", idx)
		}
		from, _ := obj["from"].(string)
		to, _ := obj["to"].(string)
		if from == "" || to == "" {
			return nil, fmt.Errorf("item %d: from and to are required", idx)
		}
		out = append(out, transitionSpec{From: from, To: to})
	}
	return out, nil
}

func statusesToDomain(specs []statusSpec) map[string]domain.StatusConfig {
	out := make(map[string]domain.StatusConfig, len(specs))
	for _, spec := range specs {
		roles := make([]domain.StatusRole, len(spec.Roles))
		for idx, role := range spec.Roles {
			roles[idx] = domain.StatusRole(role)
		}
		out[spec.Name] = domain.StatusConfig{Roles: roles}
	}
	return out
}

func transitionsToDomain(specs []transitionSpec) []domain.WorkflowTransition {
	out := make([]domain.WorkflowTransition, 0, len(specs))
	for _, transition := range specs {
		out = append(out, domain.WorkflowTransition{FromStatus: transition.From, ToStatus: transition.To})
	}
	return out
}

func workflowToMap(workflow *domain.Workflow) map[string]any {
	statuses := make([]map[string]any, 0, len(workflow.Statuses))
	for name, statusCfg := range workflow.Statuses {
		roles := make([]string, len(statusCfg.Roles))
		for idx, role := range statusCfg.Roles {
			roles[idx] = string(role)
		}
		statuses = append(statuses, map[string]any{"name": name, "roles": roles})
	}
	transitions := make([]map[string]any, 0, len(workflow.Transitions))
	for _, transition := range workflow.Transitions {
		transitions = append(transitions, map[string]any{"from": transition.FromStatus, "to": transition.ToStatus})
	}
	return map[string]any{
		"id":          workflow.ID.String(),
		"name":        workflow.Name,
		"version":     workflow.Version,
		"statuses":    statuses,
		"transitions": transitions,
	}
}

func (server *Server) handleWorkflowCreate(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if result := server.checkBlocked("tusk_workflow_create", req); result != nil {
		return result, nil
	}

	name, nameErr := req.RequireString("name")

	if nameErr != nil {
		return mcp.NewToolResultError("name is required"), nil
	}

	args := req.GetArguments()

	statuses, statusesErr := parseStatusSpecs(args["statuses"])

	if statusesErr != nil {
		return mcp.NewToolResultError(fmt.Sprintf("statuses: %v", statusesErr)), nil
	}

	if len(statuses) == 0 {
		return mcp.NewToolResultError("statuses is required"), nil
	}

	transitions, transitionsErr := parseTransitionSpecs(args["transitions"])

	if transitionsErr != nil {
		return mcp.NewToolResultError(fmt.Sprintf("transitions: %v", transitionsErr)), nil
	}

	workflow, createErr := server.workflowSvc.Create(ctx, service.CreateWorkflowInput{
		Name:        name,
		Statuses:    statusesToDomain(statuses),
		Transitions: transitionsToDomain(transitions),
	})

	if createErr != nil {
		return mcp.NewToolResultError(createErr.Error()), nil
	}

	return toolResultJSON(map[string]any{"ok": true, "workflow": workflowToMap(workflow)})
}

func (server *Server) handleWorkflowModify(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if result := server.checkBlocked("tusk_workflow_modify", req); result != nil {
		return result, nil
	}

	name, nameErr := req.RequireString("name")

	if nameErr != nil {
		return mcp.NewToolResultError("name is required"), nil
	}

	versionF, versionErr := req.RequireFloat("version")

	if versionErr != nil {
		return mcp.NewToolResultError("version is required"), nil
	}

	args := req.GetArguments()

	addStatuses, addStatusesErr := parseStatusSpecs(args["add_statuses"])

	if addStatusesErr != nil {
		return mcp.NewToolResultError(fmt.Sprintf("add_statuses: %v", addStatusesErr)), nil
	}

	setStatuses, setStatusesErr := parseStatusSpecs(args["set_statuses"])

	if setStatusesErr != nil {
		return mcp.NewToolResultError(fmt.Sprintf("set_statuses: %v", setStatusesErr)), nil
	}

	addTrans, addTransErr := parseTransitionSpecs(args["add_transitions"])

	if addTransErr != nil {
		return mcp.NewToolResultError(fmt.Sprintf("add_transitions: %v", addTransErr)), nil
	}

	removeTrans, removeTransErr := parseTransitionSpecs(args["remove_transitions"])

	if removeTransErr != nil {
		return mcp.NewToolResultError(fmt.Sprintf("remove_transitions: %v", removeTransErr)), nil
	}

	var removeStatuses []string
	if raw, ok := args["remove_statuses"].([]any); ok {
		for idx, item := range raw {
			str, ok := item.(string)
			if !ok {
				return mcp.NewToolResultError(fmt.Sprintf("remove_statuses[%d]: expected string", idx)), nil
			}
			removeStatuses = append(removeStatuses, str)
		}
	}

	workflow, modifyErr := server.workflowSvc.Modify(ctx, service.ModifyWorkflowInput{
		Name:              name,
		ExpectedVersion:   int(versionF),
		AddStatuses:       statusesToDomain(addStatuses),
		SetStatuses:       statusesToDomain(setStatuses),
		RemoveStatuses:    removeStatuses,
		AddTransitions:    transitionsToDomain(addTrans),
		RemoveTransitions: transitionsToDomain(removeTrans),
	})

	if modifyErr != nil {
		return mcp.NewToolResultError(modifyErr.Error()), nil
	}

	return toolResultJSON(map[string]any{"ok": true, "workflow": workflowToMap(workflow)})
}

// HandleWorkflowModifyForTest exposes handleWorkflowModify for internal tests.
func (server *Server) HandleWorkflowModifyForTest(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return server.handleWorkflowModify(ctx, req)
}

func (server *Server) handleWorkflowDelete(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if result := server.checkBlocked("tusk_workflow_delete", req); result != nil {
		return result, nil
	}

	name, nameErr := req.RequireString("name")

	if nameErr != nil {
		return mcp.NewToolResultError("name is required"), nil
	}

	versionF, versionErr := req.RequireFloat("version")

	if versionErr != nil {
		return mcp.NewToolResultError("version is required"), nil
	}

	workflow, getErr := server.workflowSvc.GetByName(ctx, name)

	if getErr != nil {
		if errors.Is(getErr, domain.ErrNotFound) {
			return mcp.NewToolResultError(fmt.Sprintf("workflow %q: not found", name)), nil
		}
		return mcp.NewToolResultError(getErr.Error()), nil
	}

	deleteErr := server.workflowSvc.Delete(ctx, workflow.ID, int(versionF))

	if deleteErr != nil {
		return mcp.NewToolResultError(deleteErr.Error()), nil
	}

	return toolResultJSON(map[string]any{"ok": true, "name": name})
}

// HandleWorkflowDeleteForTest exposes handleWorkflowDelete for internal tests.
func (server *Server) HandleWorkflowDeleteForTest(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return server.handleWorkflowDelete(ctx, req)
}

// HandleWorkflowCreateForTest exposes handleWorkflowCreate for internal tests.
func (server *Server) HandleWorkflowCreateForTest(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return server.handleWorkflowCreate(ctx, req)
}
