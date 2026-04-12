package config

import "fmt"

// CreateWorkflow adds a new workflow to the config file.
// Returns error if the name already exists or validation fails.
func CreateWorkflow(path string, name string, wf WorkflowConfig) error {
	cfg, err := LoadFile(path)
	if err != nil {
		return err
	}
	if cfg.Workflows == nil {
		cfg.Workflows = make(map[string]WorkflowConfig)
	}
	if _, exists := cfg.Workflows[name]; exists {
		return fmt.Errorf("workflow %q already exists", name)
	}
	cfg.Workflows[name] = wf
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}
	return WriteConfig(cfg, path)
}

// DeleteWorkflow removes a workflow from the config file.
// Returns error if the workflow doesn't exist or is referenced by a project.
func DeleteWorkflow(path string, name string) error {
	cfg, err := LoadFile(path)
	if err != nil {
		return err
	}
	if _, exists := cfg.Workflows[name]; !exists {
		return fmt.Errorf("workflow %q: not found", name)
	}
	for projID, proj := range cfg.Projects {
		if proj.Workflow == name {
			return fmt.Errorf("workflow %q is referenced by project %q", name, projID)
		}
	}
	delete(cfg.Workflows, name)
	return WriteConfig(cfg, path)
}

// WorkflowMutation describes changes to apply to an existing workflow.
type WorkflowMutation struct {
	SetStatuses       map[string]StatusConfig
	AddStatuses       map[string]StatusConfig
	RemoveStatuses    []string
	AddTransitions    []WorkflowTransitionConfig
	RemoveTransitions []WorkflowTransitionConfig
}

// ModifyWorkflow applies a mutation to an existing workflow in the config file.
// Returns error if the workflow doesn't exist or the result fails validation.
func ModifyWorkflow(path string, name string, mut WorkflowMutation) error {
	cfg, err := LoadFile(path)
	if err != nil {
		return err
	}
	wf, exists := cfg.Workflows[name]
	if !exists {
		return fmt.Errorf("workflow %q: not found", name)
	}
	if wf.Statuses == nil {
		wf.Statuses = make(map[string]StatusConfig)
	}

	removed := make(map[string]bool, len(mut.RemoveStatuses))
	for _, s := range mut.RemoveStatuses {
		delete(wf.Statuses, s)
		removed[s] = true
	}
	if len(removed) > 0 {
		var kept []WorkflowTransitionConfig
		for _, t := range wf.Transitions {
			if !removed[t.From] && !removed[t.To] {
				kept = append(kept, t)
			}
		}
		wf.Transitions = kept
	}

	for s, sc := range mut.AddStatuses {
		if _, exists := wf.Statuses[s]; exists {
			return fmt.Errorf("status %q already exists in workflow %q", s, name)
		}
		wf.Statuses[s] = sc
	}

	for s, sc := range mut.SetStatuses {
		wf.Statuses[s] = sc
	}

	for _, rm := range mut.RemoveTransitions {
		var kept []WorkflowTransitionConfig
		for _, t := range wf.Transitions {
			if t.From != rm.From || t.To != rm.To {
				kept = append(kept, t)
			}
		}
		wf.Transitions = kept
	}

	wf.Transitions = append(wf.Transitions, mut.AddTransitions...)

	cfg.Workflows[name] = wf
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}
	return WriteConfig(cfg, path)
}
