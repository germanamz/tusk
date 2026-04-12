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
	tokens, lexErrs := syntax.Lex(input)
	if len(lexErrs) > 0 {
		return config.WorkflowConfig{}, fmt.Errorf("parse error: %s", lexErrs[0].Message)
	}

	wf := config.WorkflowConfig{Statuses: make(map[string]config.StatusConfig)}

	for _, tok := range tokens {
		if tok.Type != syntax.TokenField {
			return config.WorkflowConfig{}, fmt.Errorf("unexpected token %q: expected key=value", tok.Value)
		}
		key, value, ok := strings.Cut(tok.Value, "=")
		if !ok || value == "" {
			return config.WorkflowConfig{}, fmt.Errorf("invalid field %q", tok.Value)
		}
		switch key {
		case "status":
			name, roles := parseStatusValue(value)
			wf.Statuses[name] = config.StatusConfig{Roles: roles}
		case "transition":
			transitions, err := parseTransitions(value)
			if err != nil {
				return config.WorkflowConfig{}, err
			}
			wf.Transitions = append(wf.Transitions, transitions...)
		default:
			return config.WorkflowConfig{}, fmt.Errorf("unknown field %q (expected 'status' or 'transition')", key)
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
	tokens, lexErrs := syntax.Lex(input)
	if len(lexErrs) > 0 {
		return config.WorkflowMutation{}, fmt.Errorf("parse error: %s", lexErrs[0].Message)
	}

	mut := config.WorkflowMutation{
		SetStatuses: make(map[string]config.StatusConfig),
		AddStatuses: make(map[string]config.StatusConfig),
	}

	for _, tok := range tokens {
		if tok.Type != syntax.TokenField {
			return config.WorkflowMutation{}, fmt.Errorf("unexpected token %q: expected key=value", tok.Value)
		}

		raw := tok.Value
		modifier := ""
		if len(raw) > 0 && (raw[0] == '+' || raw[0] == '-') {
			modifier = string(raw[0])
			raw = raw[1:]
		}

		key, value, ok := strings.Cut(raw, "=")
		if !ok || value == "" {
			return config.WorkflowMutation{}, fmt.Errorf("invalid field %q", tok.Value)
		}

		switch key {
		case "status":
			name, roles := parseStatusValue(value)
			switch modifier {
			case "+":
				mut.AddStatuses[name] = config.StatusConfig{Roles: roles}
			case "-":
				mut.RemoveStatuses = append(mut.RemoveStatuses, name)
			default:
				mut.SetStatuses[name] = config.StatusConfig{Roles: roles}
			}
		case "transition":
			transitions, err := parseTransitions(value)
			if err != nil {
				return config.WorkflowMutation{}, err
			}
			switch modifier {
			case "+":
				mut.AddTransitions = append(mut.AddTransitions, transitions...)
			case "-":
				mut.RemoveTransitions = append(mut.RemoveTransitions, transitions...)
			default:
				return config.WorkflowMutation{}, fmt.Errorf("transition requires + or - modifier (e.g., +transition=from:to)")
			}
		default:
			return config.WorkflowMutation{}, fmt.Errorf("unknown field %q (expected 'status' or 'transition')", key)
		}
	}

	return mut, nil
}
