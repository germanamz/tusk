package mcp

import (
	"context"
	"fmt"

	"github.com/germanamz/tusk/config"
	"github.com/germanamz/tusk/filter"
	"github.com/mark3labs/mcp-go/mcp"
)

// countTasksForProject returns the number of tasks referencing the given
// project name. Mirrors the CLI-side helper so the MCP project_delete
// handler can supply a TaskRefChecker to config.DeleteProject.
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

func applyUrgencyWeights(proj *config.ProjectConfig, weights map[string]float64) error {
	if len(weights) == 0 {
		return nil
	}
	if proj.Settings.Urgency == nil {
		proj.Settings.Urgency = &config.ProjectUrgencyConfig{}
	}
	for k, v := range weights {
		fp := config.UrgencyFieldPtr(proj.Settings.Urgency, k)
		if fp == nil {
			return fmt.Errorf("unknown urgency key %q", k)
		}
		val := v
		*fp = &val
	}
	return nil
}

func applyAutoComplete(proj *config.ProjectConfig, raw map[string]string) {
	if len(raw) == 0 {
		return
	}
	proj.Settings.AutoCompleteParent = &config.AutoCompleteParentConfig{
		TriggerStatus: raw["trigger_status"],
		TargetStatus:  raw["target_status"],
	}
}

func applyAutoRevert(proj *config.ProjectConfig, raw map[string]string) {
	if len(raw) == 0 {
		return
	}
	proj.Settings.AutoRevertParent = &config.AutoRevertParentConfig{
		TriggerStatus: raw["trigger_status"],
		TargetStatus:  raw["target_status"],
	}
}

func (s *Server) handleProjectCreate(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	name, err := req.RequireString("name")
	if err != nil {
		return mcp.NewToolResultError("name is required"), nil
	}
	workflow, err := req.RequireString("workflow")
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

	proj := config.ProjectConfig{Workflow: workflow}
	if err := applyUrgencyWeights(&proj, weights); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	applyAutoComplete(&proj, ac)
	applyAutoRevert(&proj, ar)

	path, err := config.ConfigFilePath(s.loadOpts...)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("resolving config file: %v", err)), nil
	}
	if err := config.CreateProject(path, name, proj); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if err := s.reloadConfig(ctx); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("reloading config: %v", err)), nil
	}
	return toolResultJSON(map[string]any{"ok": true, "name": name, "active_file": path})
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
	args := req.GetArguments()

	mut := config.ProjectMutation{
		UrgencySet:   map[string]float64{},
		UrgencyDelta: map[string]float64{},
	}

	if wf, ok := args["workflow"].(string); ok && wf != "" {
		w := wf
		mut.Workflow = &w
	}

	setWeights, err := parseFloatMap(args["urgency_set"])
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("urgency_set: %v", err)), nil
	}
	for k, v := range setWeights {
		mut.UrgencySet[k] = v
	}

	deltaWeights, err := parseFloatMap(args["urgency_delta"])
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("urgency_delta: %v", err)), nil
	}
	for k, v := range deltaWeights {
		mut.UrgencyDelta[k] = v
	}

	ac, err := parseStringMap(args["auto_complete"])
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("auto_complete: %v", err)), nil
	}
	if len(ac) > 0 {
		mut.AutoCompleteSet = &config.AutoCompleteParentConfig{
			TriggerStatus: ac["trigger_status"],
			TargetStatus:  ac["target_status"],
		}
	}

	ar, err := parseStringMap(args["auto_revert"])
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("auto_revert: %v", err)), nil
	}
	if len(ar) > 0 {
		mut.AutoRevertSet = &config.AutoRevertParentConfig{
			TriggerStatus: ar["trigger_status"],
			TargetStatus:  ar["target_status"],
		}
	}

	path, err := config.ConfigFilePath(s.loadOpts...)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("resolving config file: %v", err)), nil
	}
	if err := config.ModifyProject(path, name, mut); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if err := s.reloadConfig(ctx); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("reloading config: %v", err)), nil
	}
	return toolResultJSON(map[string]any{"ok": true, "name": name, "active_file": path})
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
	force, _ := req.GetArguments()["force"].(bool)

	path, err := config.ConfigFilePath(s.loadOpts...)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("resolving config file: %v", err)), nil
	}

	var checker config.TaskRefChecker
	if s.taskSvc != nil {
		checker = func(projectName string) (int, error) {
			return s.countTasksForProject(ctx, projectName)
		}
	}

	if err := config.DeleteProject(path, name, checker, force); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if err := s.reloadConfig(ctx); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("reloading config: %v", err)), nil
	}
	return toolResultJSON(map[string]any{"ok": true, "name": name, "active_file": path})
}

// HandleProjectDeleteForTest exposes handleProjectDelete for internal tests.
func (s *Server) HandleProjectDeleteForTest(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return s.handleProjectDelete(ctx, req)
}
