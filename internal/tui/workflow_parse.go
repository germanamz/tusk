package tui

import (
	"fmt"
	"strings"

	"github.com/germanamz/tusk/config"
	"github.com/germanamz/tusk/syntax"
)

// parseStatusValue parses "name(role1,role2)" or "name" into name and roles.
func parseStatusValue(value string) (string, []string) {
	idx := strings.IndexByte(value, '(')
	if idx < 0 {
		return value, nil
	}
	name := value[:idx]
	rolesStr := strings.TrimSuffix(value[idx+1:], ")")
	if rolesStr == "" {
		return name, nil
	}
	return name, strings.Split(rolesStr, ",")
}

// parseTransitions parses "from1:to1,from2:to2" into transition configs.
func parseTransitions(value string) ([]config.WorkflowTransitionConfig, error) {
	pairs := strings.Split(value, ",")
	result := make([]config.WorkflowTransitionConfig, 0, len(pairs))
	for _, pair := range pairs {
		parts := strings.SplitN(pair, ":", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return nil, fmt.Errorf("invalid transition %q: expected from:to", pair)
		}
		result = append(result, config.WorkflowTransitionConfig{From: parts[0], To: parts[1]})
	}
	return result, nil
}

// parseWorkflowCreate parses inline args into a WorkflowConfig.
func parseWorkflowCreate(args []string) (config.WorkflowConfig, error) {
	input := strings.Join(args, " ")
	fs, parseErrs := syntax.ParseFields(input)
	if len(parseErrs) > 0 {
		return config.WorkflowConfig{}, fmt.Errorf("parse error: %s", parseErrs[0].Message)
	}

	wf := config.WorkflowConfig{Statuses: make(map[string]config.StatusConfig)}

	for _, f := range fs.Fields {
		if f.Modifier != 0 {
			return config.WorkflowConfig{}, fmt.Errorf("workflow create does not accept modifier %q on %q", f.Modifier, f.Key)
		}
		if f.Value == "" {
			return config.WorkflowConfig{}, fmt.Errorf("invalid field %q", f.Key)
		}
		switch f.Key {
		case "status":
			name, roles := parseStatusValue(f.Value)
			wf.Statuses[name] = config.StatusConfig{Roles: roles}
		case "transition":
			transitions, err := parseTransitions(f.Value)
			if err != nil {
				return config.WorkflowConfig{}, err
			}
			wf.Transitions = append(wf.Transitions, transitions...)
		default:
			return config.WorkflowConfig{}, fmt.Errorf("unknown field %q (expected 'status' or 'transition')", f.Key)
		}
	}

	if len(wf.Statuses) == 0 {
		return config.WorkflowConfig{}, fmt.Errorf("at least one status is required")
	}
	return wf, nil
}

// parseWorkflowModify parses inline args into a WorkflowMutation.
func parseWorkflowModify(args []string) (config.WorkflowMutation, error) {
	input := strings.Join(args, " ")
	fs, parseErrs := syntax.ParseFields(input)
	if len(parseErrs) > 0 {
		return config.WorkflowMutation{}, fmt.Errorf("parse error: %s", parseErrs[0].Message)
	}

	mut := config.WorkflowMutation{
		SetStatuses: make(map[string]config.StatusConfig),
		AddStatuses: make(map[string]config.StatusConfig),
	}

	for _, f := range fs.Fields {
		if f.Value == "" {
			return config.WorkflowMutation{}, fmt.Errorf("invalid field %q", f.Key)
		}

		switch f.Key {
		case "status":
			name, roles := parseStatusValue(f.Value)
			switch f.Modifier {
			case '+':
				mut.AddStatuses[name] = config.StatusConfig{Roles: roles}
			case '-':
				mut.RemoveStatuses = append(mut.RemoveStatuses, name)
			case 0:
				mut.SetStatuses[name] = config.StatusConfig{Roles: roles}
			default:
				return config.WorkflowMutation{}, fmt.Errorf("unsupported modifier %q on status", f.Modifier)
			}

		case "transition":
			transitions, err := parseTransitions(f.Value)
			if err != nil {
				return config.WorkflowMutation{}, err
			}
			switch f.Modifier {
			case '+':
				mut.AddTransitions = append(mut.AddTransitions, transitions...)
			case '-':
				mut.RemoveTransitions = append(mut.RemoveTransitions, transitions...)
			default:
				return config.WorkflowMutation{}, fmt.Errorf("transition requires + or - modifier (e.g., +transition=from:to)")
			}

		default:
			return config.WorkflowMutation{}, fmt.Errorf("unknown field %q (expected 'status' or 'transition')", f.Key)
		}
	}

	return mut, nil
}
