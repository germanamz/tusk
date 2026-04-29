package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/germanamz/tusk/domain"
	"github.com/google/uuid"
)

// RenderWorkflowsTOML renders a slice of domain workflows as a TOML
// fragment matching the shape used in config/default.toml. Output is
// deterministic: workflows are sorted by name, statuses within each
// workflow are sorted by name, and transitions preserve domain order.
func RenderWorkflowsTOML(workflows []*domain.Workflow) string {
	var buf strings.Builder
	buf.WriteString("# workflows (from database — use `tusk workflow` to modify)\n")

	sorted := append([]*domain.Workflow(nil), workflows...)
	sort.Slice(sorted, func(ii, jj int) bool { return sorted[ii].Name < sorted[jj].Name })

	for _, workflow := range sorted {
		statusNames := make([]string, 0, len(workflow.Statuses))
		for name := range workflow.Statuses {
			statusNames = append(statusNames, name)
		}
		sort.Strings(statusNames)
		for _, statusName := range statusNames {
			statusCfg := workflow.Statuses[statusName]
			fmt.Fprintf(&buf, "\n[workflows.%s.statuses.%s]\n", tomlKey(workflow.Name), tomlKey(statusName))
			fmt.Fprintf(&buf, "roles = %s\n", tomlStringArray(rolesToStrings(statusCfg.Roles)))
		}
		for _, transition := range workflow.Transitions {
			fmt.Fprintf(&buf, "\n[[workflows.%s.transitions]]\n", tomlKey(workflow.Name))
			fmt.Fprintf(&buf, "from = %s\n", tomlString(transition.FromStatus))
			fmt.Fprintf(&buf, "to = %s\n", tomlString(transition.ToStatus))
		}
	}
	return buf.String()
}

// RenderProjectsTOML renders a slice of domain projects as a TOML fragment
// matching the shape used in config/default.toml. workflowsByID is used
// to resolve each project's workflow back to its name. Projects whose
// workflow cannot be resolved are rendered with an empty workflow string.
func RenderProjectsTOML(projects []*domain.Project, workflowsByID map[uuid.UUID]*domain.Workflow) string {
	var buf strings.Builder
	buf.WriteString("# projects (from database — use `tusk project` to modify)\n")

	sorted := append([]*domain.Project(nil), projects...)
	sort.Slice(sorted, func(ii, jj int) bool { return sorted[ii].Name < sorted[jj].Name })

	for _, project := range sorted {
		wfName := ""
		if workflow, ok := workflowsByID[project.WorkflowID]; ok && workflow != nil {
			wfName = workflow.Name
		}
		fmt.Fprintf(&buf, "\n[projects.%s]\n", tomlKey(project.Name))
		fmt.Fprintf(&buf, "workflow = %s\n", tomlString(wfName))
		if project.Description != "" {
			fmt.Fprintf(&buf, "description = %s\n", tomlString(project.Description))
		}

		if project.Settings.AutoCompleteParent != nil {
			fmt.Fprintf(&buf, "\n[projects.%s.settings.auto_complete_parent]\n", tomlKey(project.Name))
			fmt.Fprintf(&buf, "trigger_status = %s\n", tomlString(project.Settings.AutoCompleteParent.TriggerStatus))
			fmt.Fprintf(&buf, "target_status = %s\n", tomlString(project.Settings.AutoCompleteParent.TargetStatus))
		}
		if project.Settings.AutoRevertParent != nil {
			fmt.Fprintf(&buf, "\n[projects.%s.settings.auto_revert_parent]\n", tomlKey(project.Name))
			fmt.Fprintf(&buf, "trigger_status = %s\n", tomlString(project.Settings.AutoRevertParent.TriggerStatus))
			fmt.Fprintf(&buf, "target_status = %s\n", tomlString(project.Settings.AutoRevertParent.TargetStatus))
		}
		if urgency := project.Settings.Urgency; urgency != nil {
			fmt.Fprintf(&buf, "\n[projects.%s.settings.urgency]\n", tomlKey(project.Name))
			writeFloatField(&buf, "priority_weight", urgency.PriorityWeight)
			writeFloatField(&buf, "due_weight", urgency.DueWeight)
			writeFloatField(&buf, "age_weight", urgency.AgeWeight)
			writeFloatField(&buf, "active_weight", urgency.ActiveWeight)
			writeFloatField(&buf, "blocking_weight", urgency.BlockingWeight)
			writeFloatField(&buf, "blocked_weight", urgency.BlockedWeight)
			writeFloatField(&buf, "tags_weight", urgency.TagsWeight)
			writeFloatField(&buf, "project_weight", urgency.ProjectWeight)
			writeFloatField(&buf, "annotations_weight", urgency.AnnotationsWeight)
			writeFloatField(&buf, "waiting_weight", urgency.WaitingWeight)
		}
	}
	return buf.String()
}

func writeFloatField(buf *strings.Builder, name string, val *float64) {
	if val == nil {
		return
	}
	fmt.Fprintf(buf, "%s = %s\n", name, formatFloat(*val))
}

func formatFloat(fl float64) string {
	str := fmt.Sprintf("%g", fl)
	if !strings.ContainsAny(str, ".eE") {
		str += ".0"
	}
	return str
}

func rolesToStrings(roles []domain.StatusRole) []string {
	out := make([]string, len(roles))
	for index, role := range roles {
		out[index] = string(role)
	}
	return out
}

func tomlStringArray(values []string) string {
	parts := make([]string, len(values))
	for index, val := range values {
		parts[index] = tomlString(val)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// tomlString quotes a string value using TOML basic-string escaping.
func tomlString(str string) string {
	return fmt.Sprintf("%q", str)
}

// tomlKey renders a table-path key segment. Bare keys (ASCII letters,
// digits, hyphen, underscore) are emitted literally; anything else is
// quoted as a basic string.
func tomlKey(str string) string {
	if str == "" {
		return `""`
	}
	for _, char := range str {
		switch {
		case char >= 'A' && char <= 'Z':
		case char >= 'a' && char <= 'z':
		case char >= '0' && char <= '9':
		case char == '-' || char == '_':
		default:
			return tomlString(str)
		}
	}
	return str
}
