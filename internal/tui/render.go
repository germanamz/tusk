package tui

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/germanamz/tusk/internal/domain"
)

// projectJSON is the JSON serialization format for a project.
type projectJSON struct {
	ID       string                 `json:"id"`
	Workflow string                 `json:"workflow"`
	Settings domain.ProjectSettings `json:"settings"`
}

func toProjectJSON(p *domain.Project) projectJSON {
	return projectJSON{
		ID:       p.ID,
		Workflow: p.Workflow,
		Settings: p.Settings,
	}
}

// tagJSON is the JSON serialization format for a tag.
type tagJSON struct {
	ID    string  `json:"id"`
	Name  string  `json:"name"`
	Color *string `json:"color"`
}

// tagWithUsageJSON is the JSON serialization format for a tag with usage count.
type tagWithUsageJSON struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	Color     *string `json:"color"`
	TaskCount int     `json:"task_count"`
}

func toTagJSON(t *domain.Tag) tagJSON {
	return tagJSON{
		ID:    t.ID.String(),
		Name:  t.Name,
		Color: t.Color,
	}
}

func toTagWithUsageJSON(tw domain.TagWithUsage) tagWithUsageJSON {
	return tagWithUsageJSON{
		ID:        tw.Tag.ID.String(),
		Name:      tw.Tag.Name,
		Color:     tw.Tag.Color,
		TaskCount: tw.TaskCount,
	}
}

// renderTagList writes a list of tags to w.
// If showUsage is true, includes the task count column.
func renderTagList(w io.Writer, tags []domain.TagWithUsage, showUsage bool, format string) error {
	if format == "json" {
		items := make([]tagWithUsageJSON, len(tags))
		for i, tw := range tags {
			items[i] = toTagWithUsageJSON(tw)
		}
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(items)
	}

	if len(tags) == 0 {
		return nil
	}

	if showUsage {
		if _, err := fmt.Fprintf(w, "%-20s %-10s %s\n", "NAME", "COLOR", "TASKS"); err != nil {
			return err
		}
		for _, tw := range tags {
			color := "-"
			if tw.Tag.Color != nil {
				color = *tw.Tag.Color
			}
			if _, err := fmt.Fprintf(w, "%-20s %-10s %d\n", tw.Tag.Name, color, tw.TaskCount); err != nil {
				return err
			}
		}
	} else {
		if _, err := fmt.Fprintf(w, "%-20s %s\n", "NAME", "COLOR"); err != nil {
			return err
		}
		for _, tw := range tags {
			color := "-"
			if tw.Tag.Color != nil {
				color = *tw.Tag.Color
			}
			if _, err := fmt.Fprintf(w, "%-20s %s\n", tw.Tag.Name, color); err != nil {
				return err
			}
		}
	}
	return nil
}

// renderTagResult writes a single tag mutation result.
func renderTagResult(w io.Writer, action string, tag *domain.Tag, format string) error {
	if format == "json" {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(toTagJSON(tag))
	}
	_, err := fmt.Fprintf(w, "%s tag %s\n", action, tag.Name)
	return err
}

// renderProjectList writes a list of projects to w.
// Text format renders a table; JSON format renders an array.
func renderProjectList(w io.Writer, projects []*domain.Project, format string) error {
	if format == "json" {
		items := make([]projectJSON, len(projects))
		for i, p := range projects {
			items[i] = toProjectJSON(p)
		}
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(items)
	}

	if len(projects) == 0 {
		return nil
	}

	if _, err := fmt.Fprintf(w, "%-20s %-10s %s\n", "ID", "WORKFLOW", "SETTINGS"); err != nil {
		return err
	}
	for _, p := range projects {
		if _, err := fmt.Fprintf(w, "%-20s %-10s %s\n",
			p.ID,
			p.Workflow,
			formatSettingsSummary(p.Settings),
		); err != nil {
			return err
		}
	}
	return nil
}

// formatSettingsSummary returns a compact text summary of project settings.
func formatSettingsSummary(s domain.ProjectSettings) string {
	var parts []string
	if s.AutoCompleteParent != nil {
		parts = append(parts, "auto-complete:on")
	}
	if s.AutoRevertParent != nil {
		parts = append(parts, "auto-revert:on")
	}
	if len(parts) == 0 {
		return "-"
	}
	return strings.Join(parts, " ")
}

// formatPriority converts a priority int (0-4) to a single display character.
func formatPriority(p int) string {
	switch p {
	case 1:
		return "L"
	case 2:
		return "M"
	case 3:
		return "H"
	case 4:
		return "U"
	default:
		return "-"
	}
}

// formatAge converts a creation time to a human-readable relative age string.
func formatAge(created time.Time) string {
	d := time.Since(created)
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 14*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	case d < 60*24*time.Hour:
		return fmt.Sprintf("%dw", int(d.Hours()/(24*7)))
	case d < 365*24*time.Hour:
		return fmt.Sprintf("%dmo", int(d.Hours()/(24*30)))
	default:
		return fmt.Sprintf("%dy", int(d.Hours()/(24*365)))
	}
}

// taskJSON is the JSON serialization format for a task.
// Field names use snake_case to match the domain model.
type taskJSON struct {
	ID             string         `json:"id"`
	ShortID        string         `json:"short_id"`
	ParentID       *string        `json:"parent_id,omitempty"`
	ProjectID      string         `json:"project_id"`
	Title          string         `json:"title"`
	Description    string         `json:"description"`
	Status         string         `json:"status"`
	Priority       int            `json:"priority"`
	Version        int            `json:"version"`
	Tags           []string       `json:"tags"`
	DueAt          *string        `json:"due_at,omitempty"`
	WaitUntil      *string        `json:"wait_until,omitempty"`
	RecurrenceRule *string        `json:"recurrence_rule,omitempty"`
	UDA            map[string]any `json:"uda,omitempty"`
	CreatedAt      string         `json:"created_at"`
	ModifiedAt     string         `json:"modified_at"`
}

func toTaskJSON(t *domain.Task, tags []*domain.Tag) taskJSON {
	tj := taskJSON{
		ID:          t.ID.String(),
		ShortID:     t.ShortID,
		Title:       t.Title,
		Description: t.Description,
		Status:      t.Status,
		Priority:    t.Priority,
		Version:     t.Version,
		UDA:         t.UDA,
		CreatedAt:   t.CreatedAt.Format(time.RFC3339),
		ModifiedAt:  t.ModifiedAt.Format(time.RFC3339),
	}
	if t.ParentID != nil {
		s := t.ParentID.String()
		tj.ParentID = &s
	}
	tj.ProjectID = t.ProjectID
	if t.DueAt != nil {
		s := t.DueAt.Format(time.RFC3339)
		tj.DueAt = &s
	}
	if t.WaitUntil != nil {
		s := t.WaitUntil.Format(time.RFC3339)
		tj.WaitUntil = &s
	}
	tj.RecurrenceRule = t.RecurrenceRule
	tj.Tags = make([]string, len(tags))
	for i, tg := range tags {
		tj.Tags[i] = tg.Name
	}
	return tj
}

// renderTaskList writes a list of tasks to w in the given format.
// For "text", it renders a fixed-width table. For "json", it renders a JSON array.
// If the list is empty and format is "text", nothing is written.
func renderTaskList(w io.Writer, tasks []*domain.Task, taskTags map[string][]*domain.Tag, format string) error {
	if format == "json" {
		items := make([]taskJSON, len(tasks))
		for i, t := range tasks {
			items[i] = toTaskJSON(t, taskTags[t.ID.String()])
		}
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(items)
	}

	if len(tasks) == 0 {
		return nil
	}

	if _, err := fmt.Fprintf(w, "%-8s %-9s %-4s %-5s %s\n", "ID", "Status", "Pri", "Age", "Title"); err != nil {
		return err
	}
	for _, t := range tasks {
		title := t.Title
		if tags, ok := taskTags[t.ID.String()]; ok && len(tags) > 0 {
			tagStrs := make([]string, len(tags))
			for i, tg := range tags {
				tagStrs[i] = "+" + tg.Name
			}
			title = title + "  " + strings.Join(tagStrs, " ")
		}
		if _, err := fmt.Fprintf(w, "%-8s %-9s %-4s %-5s %s\n",
			t.ShortID,
			t.Status,
			formatPriority(t.Priority),
			formatAge(t.CreatedAt),
			title,
		); err != nil {
			return err
		}
	}
	return nil
}

// formatPriorityName converts a priority int to a full name for the info view.
func formatPriorityName(p int) string {
	switch p {
	case 1:
		return "low"
	case 2:
		return "medium"
	case 3:
		return "high"
	case 4:
		return "urgent"
	default:
		return "none"
	}
}

// annotationJSON is the JSON serialization format for an annotation.
type annotationJSON struct {
	ID        string `json:"id"`
	TaskID    string `json:"task_id"`
	Body      string `json:"body"`
	CreatedAt string `json:"created_at"`
}

// taskInfoJSON wraps a task with its annotations for the info JSON output.
type taskInfoJSON struct {
	taskJSON
	Annotations []annotationJSON `json:"annotations,omitempty"`
	Relations   []relationJSON   `json:"relations,omitempty"`
}

// renderTaskInfo writes a single task's detail view to w.
// For "text", it renders key-value pairs with optional annotations.
// For "json", it renders the task as a JSON object including annotations.
func renderTaskInfo(w io.Writer, task *domain.Task, annotations []*domain.Annotation, tags []*domain.Tag, relations []resolvedRelation, format string) error {
	if format == "json" {
		info := taskInfoJSON{taskJSON: toTaskJSON(task, tags)}
		for _, ann := range annotations {
			info.Annotations = append(info.Annotations, annotationJSON{
				ID:        ann.ID.String(),
				TaskID:    ann.TaskID.String(),
				Body:      ann.Body,
				CreatedAt: ann.CreatedAt.Format(time.RFC3339),
			})
		}
		for _, rr := range relations {
			info.Relations = append(info.Relations, relationJSON{
				ID:             rr.Relation.ID.String(),
				SourceID:       rr.Relation.SourceID.String(),
				TargetID:       rr.Relation.TargetID.String(),
				RelationType:   rr.Relation.RelationType,
				RelatedShortID: rr.RelatedShortID,
				RelatedTitle:   rr.RelatedTitle,
				DirectionLabel: rr.Label,
				CreatedAt:      rr.Relation.CreatedAt.Format(time.RFC3339),
			})
		}
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(info)
	}

	if _, err := fmt.Fprintf(w, "%-13s %s\n", "ID:", task.ShortID); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "%-13s %s\n", "Title:", task.Title); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "%-13s %s\n", "Status:", task.Status); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "%-13s %s\n", "Priority:", formatPriorityName(task.Priority)); err != nil {
		return err
	}

	if len(tags) > 0 {
		tagStrs := make([]string, len(tags))
		for i, tg := range tags {
			tagStrs[i] = "+" + tg.Name
		}
		if _, err := fmt.Fprintf(w, "%-13s %s\n", "Tags:", strings.Join(tagStrs, " ")); err != nil {
			return err
		}
	}

	if task.Description != "" {
		if _, err := fmt.Fprintln(w, "Description:"); err != nil {
			return err
		}
		if _, err := fmt.Fprintln(w); err != nil {
			return err
		}
		if _, err := fmt.Fprintln(w, task.Description); err != nil {
			return err
		}
	}
	if task.ProjectID != "" {
		if _, err := fmt.Fprintf(w, "%-13s %s\n", "Project:", task.ProjectID); err != nil {
			return err
		}
	}
	if task.ParentID != nil {
		if _, err := fmt.Fprintf(w, "%-13s %s\n", "Parent:", task.ParentID.String()); err != nil {
			return err
		}
	}
	if task.DueAt != nil {
		if _, err := fmt.Fprintf(w, "%-13s %s\n", "Due:", task.DueAt.Format("2006-01-02")); err != nil {
			return err
		}
	}
	if task.WaitUntil != nil {
		if _, err := fmt.Fprintf(w, "%-13s %s\n", "Wait:", task.WaitUntil.Format("2006-01-02 15:04:05")); err != nil {
			return err
		}
	}
	if task.RecurrenceRule != nil {
		if _, err := fmt.Fprintf(w, "%-13s %s\n", "Recurrence:", *task.RecurrenceRule); err != nil {
			return err
		}
	}
	if len(task.UDA) > 0 {
		if _, err := fmt.Fprintln(w, "UDA:"); err != nil {
			return err
		}
		if err := renderUDASection(w, task.UDA); err != nil {
			return err
		}
	}

	if _, err := fmt.Fprintf(w, "%-13s %s\n", "Created:", task.CreatedAt.Format("2006-01-02 15:04:05")); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "%-13s %s\n", "Modified:", task.ModifiedAt.Format("2006-01-02 15:04:05")); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "%-13s %d\n", "Version:", task.Version); err != nil {
		return err
	}

	if len(annotations) > 0 {
		if _, err := fmt.Fprintln(w); err != nil {
			return err
		}
		if _, err := fmt.Fprintln(w, "Annotations:"); err != nil {
			return err
		}
		for _, ann := range annotations {
			if _, err := fmt.Fprintf(w, "  %s - %s\n", ann.CreatedAt.Format("2006-01-02 15:04"), ann.Body); err != nil {
				return err
			}
		}
	}

	if len(relations) > 0 {
		if _, err := fmt.Fprintln(w); err != nil {
			return err
		}
		if _, err := fmt.Fprintln(w, "Relations:"); err != nil {
			return err
		}
		for _, rr := range relations {
			if _, err := fmt.Fprintf(w, "  %-14s %-8s  %s\n", rr.Label, rr.RelatedShortID, rr.RelatedTitle); err != nil {
				return err
			}
		}
	}

	return nil
}

// renderUDASection writes UDA key-value pairs as an indented block.
// Keys are sorted alphabetically. Single-line values appear inline;
// multi-line values appear indented below the key.
func renderUDASection(w io.Writer, uda map[string]any) error {
	keys := make([]string, 0, len(uda))
	for k := range uda {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// Calculate max key length for alignment
	maxKeyLen := 0
	for _, k := range keys {
		if len(k) > maxKeyLen {
			maxKeyLen = len(k)
		}
	}

	for _, k := range keys {
		v := fmt.Sprintf("%v", uda[k])
		if strings.Contains(v, "\n") {
			// Multi-line: key on its own line, value indented below
			if _, err := fmt.Fprintf(w, "  %s:\n", k); err != nil {
				return err
			}
			for _, line := range strings.Split(v, "\n") {
				if _, err := fmt.Fprintf(w, "    %s\n", line); err != nil {
					return err
				}
			}
		} else {
			// Single-line: inline after key with aligned padding
			if _, err := fmt.Fprintf(w, "  %-*s  %s\n", maxKeyLen+1, k+":", v); err != nil {
				return err
			}
		}
	}
	return nil
}

// renderMutationResult writes a one-line confirmation (text) or full task JSON.
// action is a past-tense verb like "Created", "Modified", "Started", etc.
func renderMutationResult(w io.Writer, action string, task *domain.Task, tags []*domain.Tag, format string) error {
	if format == "json" {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(toTaskJSON(task, tags))
	}
	_, err := fmt.Fprintf(w, "%s task %s\n", action, task.ShortID)
	return err
}

// resolvedRelation holds a relation with its resolved display info.
type resolvedRelation struct {
	Relation       *domain.Relation
	RelatedShortID string // short ID of the other task
	RelatedTitle   string // title of the other task
	Label          string // display label (e.g. "blocks", "blocked_by")
}

// relationJSON is the JSON serialization format for a relation.
type relationJSON struct {
	ID             string `json:"id"`
	SourceID       string `json:"source_id"`
	TargetID       string `json:"target_id"`
	RelationType   string `json:"relation_type"`
	RelatedShortID string `json:"related_short_id"`
	RelatedTitle   string `json:"related_title"`
	DirectionLabel string `json:"direction_label"`
	CreatedAt      string `json:"created_at"`
}

// renderLinkResult writes a link confirmation (text) or full relation JSON.
func renderLinkResult(w io.Writer, rel *domain.Relation, sourceShortID, targetShortID, format string) error {
	if format == "json" {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(relationJSON{
			ID:           rel.ID.String(),
			SourceID:     rel.SourceID.String(),
			TargetID:     rel.TargetID.String(),
			RelationType: rel.RelationType,
			CreatedAt:    rel.CreatedAt.Format(time.RFC3339),
		})
	}
	_, err := fmt.Fprintf(w, "Linked %s %s %s\n", sourceShortID, rel.RelationType, targetShortID)
	return err
}

// workflowJSON is the JSON serialization format for a workflow.
type workflowJSON struct {
	Name        string              `json:"name"`
	Statuses    []string            `json:"statuses"`
	Transitions []workflowTransJSON `json:"transitions"`
}

// workflowTransJSON is the JSON serialization format for a workflow transition.
type workflowTransJSON struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// workflowInfoJSON extends workflowJSON with referencing projects.
type workflowInfoJSON struct {
	workflowJSON
	Projects []string `json:"projects"`
}

func toWorkflowJSON(wf *domain.Workflow) workflowJSON {
	transitions := make([]workflowTransJSON, len(wf.Transitions))
	for i, t := range wf.Transitions {
		transitions[i] = workflowTransJSON{From: t.FromStatus, To: t.ToStatus}
	}
	return workflowJSON{
		Name:        wf.Name,
		Statuses:    wf.Statuses,
		Transitions: transitions,
	}
}

// renderWorkflowList writes a list of workflows to w.
// workflowProjects maps workflow name to referencing project IDs.
func renderWorkflowList(w io.Writer, workflows []*domain.Workflow, workflowProjects map[string][]string, format string) error {
	if format == "json" {
		items := make([]workflowInfoJSON, len(workflows))
		for i, wf := range workflows {
			projectIDs := workflowProjects[wf.Name]
			if projectIDs == nil {
				projectIDs = []string{}
			}
			items[i] = workflowInfoJSON{
				workflowJSON: toWorkflowJSON(wf),
				Projects:     projectIDs,
			}
		}
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(items)
	}

	if len(workflows) == 0 {
		return nil
	}

	if _, err := fmt.Fprintf(w, "%-20s %s\n", "NAME", "STATUSES"); err != nil {
		return err
	}
	for _, wf := range workflows {
		if _, err := fmt.Fprintf(w, "%-20s %s\n", wf.Name, strings.Join(wf.Statuses, ", ")); err != nil {
			return err
		}
	}
	return nil
}

// renderWorkflowInfo writes a detailed workflow view to w.
func renderWorkflowInfo(w io.Writer, wf *domain.Workflow, projectIDs []string, format string) error {
	if format == "json" {
		if projectIDs == nil {
			projectIDs = []string{}
		}
		info := workflowInfoJSON{
			workflowJSON: toWorkflowJSON(wf),
			Projects:     projectIDs,
		}
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(info)
	}

	if _, err := fmt.Fprintf(w, "%-13s %s\n", "Workflow:", wf.Name); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "%-13s %s\n", "Statuses:", strings.Join(wf.Statuses, ", ")); err != nil {
		return err
	}

	if len(wf.Transitions) > 0 {
		if _, err := fmt.Fprintln(w, "Transitions:"); err != nil {
			return err
		}
		maxLen := 0
		for _, t := range wf.Transitions {
			if len(t.FromStatus) > maxLen {
				maxLen = len(t.FromStatus)
			}
		}
		fmtStr := fmt.Sprintf("  %%-%ds -> %%s\n", maxLen)
		for _, t := range wf.Transitions {
			if _, err := fmt.Fprintf(w, fmtStr, t.FromStatus, t.ToStatus); err != nil {
				return err
			}
		}
	}

	if len(projectIDs) > 0 {
		if _, err := fmt.Fprintf(w, "%-13s %s\n", "Projects:", strings.Join(projectIDs, ", ")); err != nil {
			return err
		}
	}

	return nil
}
