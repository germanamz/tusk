package mcp

import (
	"context"
	"errors"
	"fmt"

	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/service"
	"github.com/mark3labs/mcp-go/mcp"
)

func parseStringMap(raw any) (map[string]string, error) {
	if raw == nil {
		return nil, nil
	}
	m, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("expected object")
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("key %q: expected string value", k)
		}
		out[k] = s
	}
	return out, nil
}

func parseFloatMap(raw any) (map[string]float64, error) {
	if raw == nil {
		return nil, nil
	}
	m, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("expected object")
	}
	out := make(map[string]float64, len(m))
	for k, v := range m {
		f, ok := v.(float64)
		if !ok {
			return nil, fmt.Errorf("key %q: expected number", k)
		}
		out[k] = f
	}
	return out, nil
}

func urgencyOverrideFromMap(weights map[string]float64) (*domain.UrgencyOverrides, error) {
	if len(weights) == 0 {
		return nil, nil
	}
	o := &domain.UrgencyOverrides{}
	for k, v := range weights {
		val := v
		switch k {
		case "priority_weight":
			o.PriorityWeight = &val
		case "due_weight":
			o.DueWeight = &val
		case "age_weight":
			o.AgeWeight = &val
		case "active_weight":
			o.ActiveWeight = &val
		case "blocking_weight":
			o.BlockingWeight = &val
		case "blocked_weight":
			o.BlockedWeight = &val
		case "tags_weight":
			o.TagsWeight = &val
		case "project_weight":
			o.ProjectWeight = &val
		case "annotations_weight":
			o.AnnotationsWeight = &val
		case "waiting_weight":
			o.WaitingWeight = &val
		default:
			return nil, fmt.Errorf("unknown urgency key %q", k)
		}
	}
	return o, nil
}

func autoCompleteFromMap(raw map[string]string) *domain.AutoCompleteConfig {
	if len(raw) == 0 {
		return nil
	}
	return &domain.AutoCompleteConfig{
		TriggerStatus: raw["trigger_status"],
		TargetStatus:  raw["target_status"],
	}
}

func autoRevertFromMap(raw map[string]string) *domain.AutoRevertConfig {
	if len(raw) == 0 {
		return nil
	}
	return &domain.AutoRevertConfig{
		TriggerStatus: raw["trigger_status"],
		TargetStatus:  raw["target_status"],
	}
}

func projectToMap(p *domain.Project) map[string]any {
	return map[string]any{
		"id":       p.ID.String(),
		"name":     p.Name,
		"version":  p.Version,
		"workflow": p.WorkflowID.String(),
	}
}

func (s *Server) handleProjectCreate(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	name, err := req.RequireString("name")
	if err != nil {
		return mcp.NewToolResultError("name is required"), nil
	}
	workflowName, err := req.RequireString("workflow")
	if err != nil {
		return mcp.NewToolResultError("workflow is required"), nil
	}
	args := req.GetArguments()

	weights, err := parseFloatMap(args["urgency"])
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("urgency: %v", err)), nil
	}
	ac, err := parseStringMap(args["auto_complete"])
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("auto_complete: %v", err)), nil
	}
	ar, err := parseStringMap(args["auto_revert"])
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("auto_revert: %v", err)), nil
	}
	urgency, err := urgencyOverrideFromMap(weights)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	wf, err := s.workflowSvc.GetByName(ctx, workflowName)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("resolving workflow %q: %v", workflowName, err)), nil
	}

	settings := domain.ProjectSettings{
		AutoCompleteParent: autoCompleteFromMap(ac),
		AutoRevertParent:   autoRevertFromMap(ar),
		Urgency:            urgency,
	}

	s.configMu.Lock()
	defer s.configMu.Unlock()
	p, err := s.projectSvc.Create(ctx, service.CreateProjectInput{
		Name:       name,
		WorkflowID: wf.ID,
		Settings:   settings,
	})
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return toolResultJSON(map[string]any{"ok": true, "project": projectToMap(p)})
}

// HandleProjectCreateForTest exposes handleProjectCreate for internal tests.
func (s *Server) HandleProjectCreateForTest(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return s.handleProjectCreate(ctx, req)
}

func (s *Server) handleProjectModify(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	name, err := req.RequireString("name")
	if err != nil {
		return mcp.NewToolResultError("name is required"), nil
	}
	versionF, err := req.RequireFloat("version")
	if err != nil {
		return mcp.NewToolResultError("version is required"), nil
	}
	args := req.GetArguments()

	input := service.ModifyProjectInput{
		Name:            name,
		ExpectedVersion: int(versionF),
		Urgency: service.UrgencyMutation{
			Set:   map[string]float64{},
			Delta: map[string]float64{},
		},
	}

	if wfName, ok := args["workflow"].(string); ok && wfName != "" {
		wf, err := s.workflowSvc.GetByName(ctx, wfName)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("resolving workflow %q: %v", wfName, err)), nil
		}
		id := wf.ID
		input.WorkflowID = &id
	}

	setWeights, err := parseFloatMap(args["urgency_set"])
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("urgency_set: %v", err)), nil
	}
	for k, v := range setWeights {
		input.Urgency.Set[k] = v
	}

	deltaWeights, err := parseFloatMap(args["urgency_delta"])
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("urgency_delta: %v", err)), nil
	}
	for k, v := range deltaWeights {
		input.Urgency.Delta[k] = v
	}

	ac, err := parseStringMap(args["auto_complete"])
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("auto_complete: %v", err)), nil
	}
	input.AutoComplete = autoCompleteFromMap(ac)

	ar, err := parseStringMap(args["auto_revert"])
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("auto_revert: %v", err)), nil
	}
	input.AutoRevert = autoRevertFromMap(ar)

	s.configMu.Lock()
	defer s.configMu.Unlock()
	p, err := s.projectSvc.Modify(ctx, input)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return toolResultJSON(map[string]any{"ok": true, "project": projectToMap(p)})
}

// HandleProjectModifyForTest exposes handleProjectModify for internal tests.
func (s *Server) HandleProjectModifyForTest(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return s.handleProjectModify(ctx, req)
}

func (s *Server) handleProjectDelete(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	name, err := req.RequireString("name")
	if err != nil {
		return mcp.NewToolResultError("name is required"), nil
	}
	versionF, err := req.RequireFloat("version")
	if err != nil {
		return mcp.NewToolResultError("version is required"), nil
	}
	force, _ := req.GetArguments()["force"].(bool)

	p, err := s.projectSvc.GetByName(ctx, name)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return mcp.NewToolResultError(fmt.Sprintf("project %q: not found", name)), nil
		}
		return mcp.NewToolResultError(err.Error()), nil
	}

	s.configMu.Lock()
	defer s.configMu.Unlock()
	if err := s.projectSvc.Delete(ctx, p.ID, int(versionF), force); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return toolResultJSON(map[string]any{"ok": true, "name": name})
}

// HandleProjectDeleteForTest exposes handleProjectDelete for internal tests.
func (s *Server) HandleProjectDeleteForTest(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return s.handleProjectDelete(ctx, req)
}
