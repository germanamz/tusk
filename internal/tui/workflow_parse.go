package tui

import (
	"fmt"
	"strings"

	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/service"
	"github.com/germanamz/tusk/syntax"
)

// parseStatusValue parses "name(role1,role2)" or "name" into name and roles.
func parseStatusValue(value string) (string, []domain.StatusRole) {
	idx := strings.IndexByte(value, '(')
	if idx < 0 {
		return value, nil
	}
	name := value[:idx]
	rolesStr := strings.TrimSuffix(value[idx+1:], ")")
	if rolesStr == "" {
		return name, nil
	}
	parts := strings.Split(rolesStr, ",")
	roles := make([]domain.StatusRole, len(parts))
	for roleIdx, part := range parts {
		roles[roleIdx] = domain.StatusRole(part)
	}
	return name, roles
}

// parseTransitions parses "from1:to1,from2:to2" into domain transitions.
func parseTransitions(value string) ([]domain.WorkflowTransition, error) {
	pairs := strings.Split(value, ",")
	result := make([]domain.WorkflowTransition, 0, len(pairs))
	for _, pair := range pairs {
		parts := strings.SplitN(pair, ":", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return nil, fmt.Errorf("invalid transition %q: expected from:to", pair)
		}
		result = append(result, domain.WorkflowTransition{FromStatus: parts[0], ToStatus: parts[1]})
	}
	return result, nil
}

// parseWorkflowCreate parses inline args into a service.CreateWorkflowInput
// (name is filled in by the caller).
func parseWorkflowCreate(args []string) (service.CreateWorkflowInput, error) {
	input := strings.Join(args, " ")
	fs, parseErrs := syntax.ParseFields(input)
	if len(parseErrs) > 0 {
		return service.CreateWorkflowInput{}, fmt.Errorf("parse error: %s", parseErrs[0].Message)
	}

	out := service.CreateWorkflowInput{
		Statuses: make(map[string]domain.StatusConfig),
	}

	for _, field := range fs.Fields {
		if field.Modifier != 0 {
			return service.CreateWorkflowInput{}, fmt.Errorf("workflow create does not accept modifier %q on %q", field.Modifier, field.Key)
		}
		if field.Value == "" {
			return service.CreateWorkflowInput{}, fmt.Errorf("invalid field %q", field.Key)
		}
		switch field.Key {
		case "status":
			name, roles := parseStatusValue(field.Value)
			out.Statuses[name] = domain.StatusConfig{Roles: roles}
		case "transition":
			transitions, err := parseTransitions(field.Value)
			if err != nil {
				return service.CreateWorkflowInput{}, err
			}
			out.Transitions = append(out.Transitions, transitions...)
		default:
			return service.CreateWorkflowInput{}, fmt.Errorf("unknown field %q (expected 'status' or 'transition')", field.Key)
		}
	}

	if len(out.Statuses) == 0 {
		return service.CreateWorkflowInput{}, fmt.Errorf("at least one status is required")
	}
	return out, nil
}

// parseWorkflowModify parses inline args into a service.ModifyWorkflowInput
// (name and expected version are filled in by the caller).
func parseWorkflowModify(args []string) (service.ModifyWorkflowInput, error) {
	input := strings.Join(args, " ")
	fs, parseErrs := syntax.ParseFields(input)
	if len(parseErrs) > 0 {
		return service.ModifyWorkflowInput{}, fmt.Errorf("parse error: %s", parseErrs[0].Message)
	}

	out := service.ModifyWorkflowInput{
		AddStatuses: make(map[string]domain.StatusConfig),
		SetStatuses: make(map[string]domain.StatusConfig),
	}

	for _, field := range fs.Fields {
		if field.Value == "" {
			return service.ModifyWorkflowInput{}, fmt.Errorf("invalid field %q", field.Key)
		}

		switch field.Key {
		case "status":
			name, roles := parseStatusValue(field.Value)
			switch field.Modifier {
			case '+':
				out.AddStatuses[name] = domain.StatusConfig{Roles: roles}
			case '-':
				out.RemoveStatuses = append(out.RemoveStatuses, name)
			case 0:
				out.SetStatuses[name] = domain.StatusConfig{Roles: roles}
			default:
				return service.ModifyWorkflowInput{}, fmt.Errorf("unsupported modifier %q on status", field.Modifier)
			}

		case "transition":
			transitions, err := parseTransitions(field.Value)
			if err != nil {
				return service.ModifyWorkflowInput{}, err
			}
			switch field.Modifier {
			case '+':
				out.AddTransitions = append(out.AddTransitions, transitions...)
			case '-':
				out.RemoveTransitions = append(out.RemoveTransitions, transitions...)
			default:
				return service.ModifyWorkflowInput{}, fmt.Errorf("transition requires + or - modifier (e.g., +transition=from:to)")
			}

		default:
			return service.ModifyWorkflowInput{}, fmt.Errorf("unknown field %q (expected 'status' or 'transition')", field.Key)
		}
	}

	return out, nil
}
