package mcp

import (
	"context"
	"errors"
	"fmt"

	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/service"
	"github.com/google/uuid"
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
	for key, value := range m {
		strVal, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("key %q: expected string value", key)
		}
		out[key] = strVal
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
	for key, value := range m {
		floatVal, ok := value.(float64)
		if !ok {
			return nil, fmt.Errorf("key %q: expected number", key)
		}
		out[key] = floatVal
	}
	return out, nil
}

func urgencyOverrideFromMap(weights map[string]float64) (*domain.UrgencyOverrides, error) {
	if len(weights) == 0 {
		return nil, nil
	}
	overrides := &domain.UrgencyOverrides{}
	for key, val := range weights {
		weight := val
		switch key {
		case "priority_weight":
			overrides.PriorityWeight = &weight
		case "due_weight":
			overrides.DueWeight = &weight
		case "age_weight":
			overrides.AgeWeight = &weight
		case "active_weight":
			overrides.ActiveWeight = &weight
		case "blocking_weight":
			overrides.BlockingWeight = &weight
		case "blocked_weight":
			overrides.BlockedWeight = &weight
		case "tags_weight":
			overrides.TagsWeight = &weight
		case "project_weight":
			overrides.ProjectWeight = &weight
		case "annotations_weight":
			overrides.AnnotationsWeight = &weight
		case "waiting_weight":
			overrides.WaitingWeight = &weight
		default:
			return nil, fmt.Errorf("unknown urgency key %q", key)
		}
	}
	return overrides, nil
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

// parseTaxonomyInput extracts the project taxonomy mutation from the raw
// arguments map. Returns:
//   - (present=false, mutation=nil, nil)       when the key is absent;
//   - (present=true,  mutation={Clear:true},  nil) for an explicit JSON null;
//   - (present=true,  mutation={Value:{}},    nil) for {"ranks": []};
//   - (present=true,  mutation={Value: tax},  nil) for {"ranks": [[...]]} after
//     structural validation;
//   - (present=true,  mutation=nil,           err) on any other shape.
func parseTaxonomyInput(args map[string]any) (bool, *service.TaxonomyMutation, error) {
	raw, present := args["taxonomy"]
	if !present {
		return false, nil, nil
	}
	if raw == nil {
		return true, &service.TaxonomyMutation{Clear: true}, nil
	}
	obj, ok := raw.(map[string]any)
	if !ok {
		return true, nil, fmt.Errorf("taxonomy: expected object")
	}
	ranksRaw, ok := obj["ranks"]
	if !ok {
		return true, nil, fmt.Errorf("taxonomy: missing ranks")
	}

	ranks, ranksErr := parseRanks(ranksRaw)

	if ranksErr != nil {
		return true, nil, fmt.Errorf("taxonomy: %w", ranksErr)
	}

	if len(ranks) == 0 {
		return true, &service.TaxonomyMutation{Value: domain.Taxonomy{}}, nil
	}
	taxonomy := domain.Taxonomy(ranks)

	validateErr := taxonomy.Validate()

	if validateErr != nil {
		return true, nil, fmt.Errorf("taxonomy: %w", validateErr)
	}

	return true, &service.TaxonomyMutation{Value: taxonomy}, nil
}

// parseRanks converts a JSON-decoded ranks array ([]any of []any of string)
// into a [][]string. Any shape mismatch returns a descriptive error.
func parseRanks(raw any) ([][]string, error) {
	arr, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("ranks: expected array")
	}
	out := make([][]string, 0, len(arr))
	for rankIdx, rank := range arr {
		peersArr, ok := rank.([]any)
		if !ok {
			return nil, fmt.Errorf("ranks[%d]: expected array", rankIdx)
		}
		peers := make([]string, 0, len(peersArr))
		for j, peer := range peersArr {
			str, ok := peer.(string)
			if !ok {
				return nil, fmt.Errorf("ranks[%d][%d]: expected string", rankIdx, j)
			}
			peers = append(peers, str)
		}
		out = append(out, peers)
	}
	return out, nil
}

func (server *Server) handleProjectCreate(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if result := server.checkBlocked("tusk_project_create", req); result != nil {
		return result, nil
	}

	name, nameErr := req.RequireString("name")

	if nameErr != nil {
		return mcp.NewToolResultError("name is required"), nil
	}

	workflowName, workflowNameErr := req.RequireString("workflow")

	if workflowNameErr != nil {
		return mcp.NewToolResultError("workflow is required"), nil
	}

	args := req.GetArguments()

	weights, weightsErr := parseFloatMap(args["urgency"])

	if weightsErr != nil {
		return mcp.NewToolResultError(fmt.Sprintf("urgency: %v", weightsErr)), nil
	}

	autoCompleteMap, autoCompleteErr := parseStringMap(args["auto_complete"])

	if autoCompleteErr != nil {
		return mcp.NewToolResultError(fmt.Sprintf("auto_complete: %v", autoCompleteErr)), nil
	}

	autoRevertMap, autoRevertErr := parseStringMap(args["auto_revert"])

	if autoRevertErr != nil {
		return mcp.NewToolResultError(fmt.Sprintf("auto_revert: %v", autoRevertErr)), nil
	}

	urgency, urgencyErr := urgencyOverrideFromMap(weights)

	if urgencyErr != nil {
		return mcp.NewToolResultError(urgencyErr.Error()), nil
	}

	workflow, workflowErr := server.workflowSvc.GetByName(ctx, workflowName)

	if workflowErr != nil {
		return mcp.NewToolResultError(fmt.Sprintf("resolving workflow %q: %v", workflowName, workflowErr)), nil
	}

	settings := domain.ProjectSettings{
		AutoCompleteParent: autoCompleteFromMap(autoCompleteMap),
		AutoRevertParent:   autoRevertFromMap(autoRevertMap),
		Urgency:            urgency,
	}

	desc, _ := args["description"].(string)

	project, createErr := server.projectSvc.Create(ctx, service.CreateProjectInput{
		Name:        name,
		WorkflowID:  workflow.ID,
		Description: desc,
		Settings:    settings,
	})

	if createErr != nil {
		return mcp.NewToolResultError(createErr.Error()), nil
	}

	return toolResultJSON(map[string]any{"ok": true, "project": server.toProjectResponse(project, workflowName)})
}

// HandleProjectCreateForTest exposes handleProjectCreate for internal tests.
func (server *Server) HandleProjectCreateForTest(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return server.handleProjectCreate(ctx, req)
}

func (server *Server) handleProjectModify(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if result := server.checkBlocked("tusk_project_modify", req); result != nil {
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

	input := service.ModifyProjectInput{
		Name:            name,
		ExpectedVersion: int(versionF),
		Urgency: service.UrgencyMutation{
			Set:   map[string]float64{},
			Delta: map[string]float64{},
		},
	}

	if wfName, ok := args["workflow"].(string); ok && wfName != "" {
		workflow, workflowErr := server.workflowSvc.GetByName(ctx, wfName)

		if workflowErr != nil {
			return mcp.NewToolResultError(fmt.Sprintf("resolving workflow %q: %v", wfName, workflowErr)), nil
		}

		workflowID := workflow.ID
		input.WorkflowID = &workflowID
	}

	if rawDesc, present := args["description"]; present {
		descStr, ok := rawDesc.(string)
		if !ok {
			return mcp.NewToolResultError("description: expected string"), nil
		}
		var inner *string
		if descStr != "" {
			str := descStr
			inner = &str
		}
		input.Description = &inner
	}

	setWeights, setWeightsErr := parseFloatMap(args["urgency_set"])

	if setWeightsErr != nil {
		return mcp.NewToolResultError(fmt.Sprintf("urgency_set: %v", setWeightsErr)), nil
	}

	for key, val := range setWeights {
		input.Urgency.Set[key] = val
	}

	deltaWeights, deltaWeightsErr := parseFloatMap(args["urgency_delta"])

	if deltaWeightsErr != nil {
		return mcp.NewToolResultError(fmt.Sprintf("urgency_delta: %v", deltaWeightsErr)), nil
	}

	for key, val := range deltaWeights {
		input.Urgency.Delta[key] = val
	}

	autoCompleteMap, autoCompleteErr := parseStringMap(args["auto_complete"])

	if autoCompleteErr != nil {
		return mcp.NewToolResultError(fmt.Sprintf("auto_complete: %v", autoCompleteErr)), nil
	}

	input.AutoComplete = autoCompleteFromMap(autoCompleteMap)

	autoRevertMap, autoRevertErr := parseStringMap(args["auto_revert"])

	if autoRevertErr != nil {
		return mcp.NewToolResultError(fmt.Sprintf("auto_revert: %v", autoRevertErr)), nil
	}

	input.AutoRevert = autoRevertFromMap(autoRevertMap)

	_, taxMutation, taxMutationErr := parseTaxonomyInput(args)

	if taxMutationErr != nil {
		return mcp.NewToolResultError(taxMutationErr.Error()), nil
	}

	input.Taxonomy = taxMutation

	project, modifyErr := server.projectSvc.Modify(ctx, input)

	if modifyErr != nil {
		return mcp.NewToolResultError(modifyErr.Error()), nil
	}

	resolvedWorkflowName, resolveErr := server.resolveWorkflowName(ctx, project.WorkflowID)

	if resolveErr != nil {
		return mcp.NewToolResultError(resolveErr.Error()), nil
	}

	return toolResultJSON(map[string]any{"ok": true, "project": server.toProjectResponse(project, resolvedWorkflowName)})
}

// resolveWorkflowName looks up the workflow name for the given id, returning
// an empty string when the workflow has been deleted out from under the
// project. Used so project responses can surface the workflow's human name
// without each handler duplicating the lookup.
func (server *Server) resolveWorkflowName(ctx context.Context, id uuid.UUID) (string, error) {
	workflow, workflowErr := server.workflowSvc.GetByID(ctx, id)

	if workflowErr != nil {
		if errors.Is(workflowErr, domain.ErrNotFound) {
			return "", nil
		}
		return "", workflowErr
	}

	return workflow.Name, nil
}

// HandleProjectModifyForTest exposes handleProjectModify for internal tests.
func (server *Server) HandleProjectModifyForTest(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return server.handleProjectModify(ctx, req)
}

func (server *Server) handleProjectDelete(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if result := server.checkBlocked("tusk_project_delete", req); result != nil {
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

	force, _ := req.GetArguments()["force"].(bool)

	project, lookupErr := server.projectSvc.GetByName(ctx, name)

	if lookupErr != nil {
		if errors.Is(lookupErr, domain.ErrNotFound) {
			return mcp.NewToolResultError(fmt.Sprintf("project %q: not found", name)), nil
		}
		return mcp.NewToolResultError(lookupErr.Error()), nil
	}

	if deleteErr := server.projectSvc.Delete(ctx, project.ID, int(versionF), force); deleteErr != nil {
		return mcp.NewToolResultError(deleteErr.Error()), nil
	}
	return toolResultJSON(map[string]any{"ok": true, "name": name})
}

// HandleProjectDeleteForTest exposes handleProjectDelete for internal tests.
func (server *Server) HandleProjectDeleteForTest(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return server.handleProjectDelete(ctx, req)
}
