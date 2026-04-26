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
	var b strings.Builder
	b.WriteString("# workflows (from database — use `tusk workflow` to modify)\n")

	sorted := append([]*domain.Workflow(nil), workflows...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })

	for _, w := range sorted {
		statusNames := make([]string, 0, len(w.Statuses))
		for name := range w.Statuses {
			statusNames = append(statusNames, name)
		}
		sort.Strings(statusNames)
		for _, sn := range statusNames {
			sc := w.Statuses[sn]
			fmt.Fprintf(&b, "\n[workflows.%s.statuses.%s]\n", tomlKey(w.Name), tomlKey(sn))
			fmt.Fprintf(&b, "roles = %s\n", tomlStringArray(rolesToStrings(sc.Roles)))
		}
		for _, tr := range w.Transitions {
			fmt.Fprintf(&b, "\n[[workflows.%s.transitions]]\n", tomlKey(w.Name))
			fmt.Fprintf(&b, "from = %s\n", tomlString(tr.FromStatus))
			fmt.Fprintf(&b, "to = %s\n", tomlString(tr.ToStatus))
		}
	}
	return b.String()
}

// RenderProjectsTOML renders a slice of domain projects as a TOML fragment
// matching the shape used in config/default.toml. workflowsByID is used
// to resolve each project's workflow back to its name. Projects whose
// workflow cannot be resolved are rendered with an empty workflow string.
func RenderProjectsTOML(projects []*domain.Project, workflowsByID map[uuid.UUID]*domain.Workflow) string {
	var b strings.Builder
	b.WriteString("# projects (from database — use `tusk project` to modify)\n")

	sorted := append([]*domain.Project(nil), projects...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })

	for _, p := range sorted {
		wfName := ""
		if wf, ok := workflowsByID[p.WorkflowID]; ok && wf != nil {
			wfName = wf.Name
		}
		fmt.Fprintf(&b, "\n[projects.%s]\n", tomlKey(p.Name))
		fmt.Fprintf(&b, "workflow = %s\n", tomlString(wfName))
		if p.Description != "" {
			fmt.Fprintf(&b, "description = %s\n", tomlString(p.Description))
		}

		if p.Settings.AutoCompleteParent != nil {
			fmt.Fprintf(&b, "\n[projects.%s.settings.auto_complete_parent]\n", tomlKey(p.Name))
			fmt.Fprintf(&b, "trigger_status = %s\n", tomlString(p.Settings.AutoCompleteParent.TriggerStatus))
			fmt.Fprintf(&b, "target_status = %s\n", tomlString(p.Settings.AutoCompleteParent.TargetStatus))
		}
		if p.Settings.AutoRevertParent != nil {
			fmt.Fprintf(&b, "\n[projects.%s.settings.auto_revert_parent]\n", tomlKey(p.Name))
			fmt.Fprintf(&b, "trigger_status = %s\n", tomlString(p.Settings.AutoRevertParent.TriggerStatus))
			fmt.Fprintf(&b, "target_status = %s\n", tomlString(p.Settings.AutoRevertParent.TargetStatus))
		}
		if u := p.Settings.Urgency; u != nil {
			fmt.Fprintf(&b, "\n[projects.%s.settings.urgency]\n", tomlKey(p.Name))
			writeFloatField(&b, "priority_weight", u.PriorityWeight)
			writeFloatField(&b, "due_weight", u.DueWeight)
			writeFloatField(&b, "age_weight", u.AgeWeight)
			writeFloatField(&b, "active_weight", u.ActiveWeight)
			writeFloatField(&b, "blocking_weight", u.BlockingWeight)
			writeFloatField(&b, "blocked_weight", u.BlockedWeight)
			writeFloatField(&b, "tags_weight", u.TagsWeight)
			writeFloatField(&b, "project_weight", u.ProjectWeight)
			writeFloatField(&b, "annotations_weight", u.AnnotationsWeight)
			writeFloatField(&b, "waiting_weight", u.WaitingWeight)
		}
	}
	return b.String()
}

func writeFloatField(b *strings.Builder, name string, v *float64) {
	if v == nil {
		return
	}
	fmt.Fprintf(b, "%s = %s\n", name, formatFloat(*v))
}

func formatFloat(f float64) string {
	s := fmt.Sprintf("%g", f)
	if !strings.ContainsAny(s, ".eE") {
		s += ".0"
	}
	return s
}

func rolesToStrings(roles []domain.StatusRole) []string {
	out := make([]string, len(roles))
	for i, r := range roles {
		out[i] = string(r)
	}
	return out
}

func tomlStringArray(values []string) string {
	parts := make([]string, len(values))
	for i, v := range values {
		parts[i] = tomlString(v)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// tomlString quotes a string value using TOML basic-string escaping.
func tomlString(s string) string {
	return fmt.Sprintf("%q", s)
}

// tomlKey renders a table-path key segment. Bare keys (ASCII letters,
// digits, hyphen, underscore) are emitted literally; anything else is
// quoted as a basic string.
func tomlKey(s string) string {
	if s == "" {
		return `""`
	}
	for _, r := range s {
		switch {
		case r >= 'A' && r <= 'Z':
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
		case r == '-' || r == '_':
		default:
			return tomlString(s)
		}
	}
	return s
}
