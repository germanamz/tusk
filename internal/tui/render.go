package tui

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/service"
	"github.com/google/uuid"
)

// projectJSON is the JSON serialization format for a project.
type projectJSON struct {
	ID       string                 `json:"id"`
	Workflow string                 `json:"workflow"`
	Settings domain.ProjectSettings `json:"settings"`
}

func toProjectJSON(p *domain.Project, workflowName string) projectJSON {
	return projectJSON{
		ID:       p.Name,
		Workflow: workflowName,
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
func (r *Renderer) renderTagList(tags []domain.TagWithUsage, showUsage bool) error {
	if r.format == "json" {
		items := make([]tagWithUsageJSON, len(tags))
		for i, tw := range tags {
			items[i] = toTagWithUsageJSON(tw)
		}
		enc := json.NewEncoder(r.w)
		enc.SetIndent("", "  ")
		return enc.Encode(items)
	}

	if len(tags) == 0 {
		return nil
	}

	if showUsage {
		nameH := r.styledHeader("NAME")
		colorH := r.styledHeader("COLOR")
		tasksH := r.styledHeader("TASKS")
		if _, err := fmt.Fprintf(r.w, "%s%s %s%s %s\n",
			nameH, strings.Repeat(" ", max(0, 20-lipgloss.Width(nameH))),
			colorH, strings.Repeat(" ", max(0, 10-lipgloss.Width(colorH))),
			tasksH,
		); err != nil {
			return err
		}
		for _, tw := range tags {
			color := "-"
			if tw.Tag.Color != nil {
				color = *tw.Tag.Color
			}
			if _, err := fmt.Fprintf(r.w, "%-20s %-10s %d\n", tw.Tag.Name, color, tw.TaskCount); err != nil {
				return err
			}
		}
	} else {
		nameH := r.styledHeader("NAME")
		colorH := r.styledHeader("COLOR")
		if _, err := fmt.Fprintf(r.w, "%s%s %s\n",
			nameH, strings.Repeat(" ", max(0, 20-lipgloss.Width(nameH))),
			colorH,
		); err != nil {
			return err
		}
		for _, tw := range tags {
			color := "-"
			if tw.Tag.Color != nil {
				color = *tw.Tag.Color
			}
			if _, err := fmt.Fprintf(r.w, "%-20s %s\n", tw.Tag.Name, color); err != nil {
				return err
			}
		}
	}
	return nil
}

// renderTagResult writes a single tag mutation result.
func (r *Renderer) renderTagResult(action string, tag *domain.Tag) error {
	if r.format == "json" {
		enc := json.NewEncoder(r.w)
		enc.SetIndent("", "  ")
		return enc.Encode(toTagJSON(tag))
	}
	_, err := fmt.Fprintf(r.w, "%s tag %s\n", action, tag.Name)
	return err
}

// renderTagRenameResult writes a tag rename confirmation (text) or tag JSON.
func (r *Renderer) renderTagRenameResult(oldName string, tag *domain.Tag) error {
	if r.format == "json" {
		enc := json.NewEncoder(r.w)
		enc.SetIndent("", "  ")
		return enc.Encode(toTagJSON(tag))
	}
	_, err := fmt.Fprintf(r.w, "Renamed tag %s to %s\n", oldName, tag.Name)
	return err
}

// renderProjectList writes a list of projects to w.
// Text format renders a table; JSON format renders an array.
// workflowNames maps workflow UUIDs to their display names.
func (r *Renderer) renderProjectList(projects []*domain.Project, workflowNames map[uuid.UUID]string) error {
	if r.format == "json" {
		items := make([]projectJSON, len(projects))
		for i, p := range projects {
			items[i] = toProjectJSON(p, workflowNames[p.WorkflowID])
		}
		enc := json.NewEncoder(r.w)
		enc.SetIndent("", "  ")
		return enc.Encode(items)
	}

	if len(projects) == 0 {
		return nil
	}

	idH := r.styledHeader("ID")
	wfH := r.styledHeader("WORKFLOW")
	settH := r.styledHeader("SETTINGS")
	if _, err := fmt.Fprintf(r.w, "%s%s %s%s %s\n",
		idH, strings.Repeat(" ", max(0, 20-lipgloss.Width(idH))),
		wfH, strings.Repeat(" ", max(0, 10-lipgloss.Width(wfH))),
		settH,
	); err != nil {
		return err
	}
	for _, p := range projects {
		if _, err := fmt.Fprintf(r.w, "%-20s %-10s %s\n",
			p.Name,
			workflowNames[p.WorkflowID],
			formatSettingsSummary(p.Settings),
		); err != nil {
			return err
		}
	}
	return nil
}

// projectShowJSON is the JSON payload for `tusk project show`.
type projectShowJSON struct {
	projectJSON
	EffectiveTaxonomy effectiveTaxonomyShowJSON `json:"effective_taxonomy"`
}

type effectiveTaxonomyShowJSON struct {
	Ranks  [][]string `json:"ranks"`
	Source string     `json:"source"`
}

// renderProjectShow writes a single project's detailed view to w.
// Text renders Workflow, Settings, and an effective Taxonomy block.
// JSON extends projectJSON with an effective_taxonomy object.
func (r *Renderer) renderProjectShow(p *domain.Project, workflowName string, taxonomy domain.Taxonomy, source service.TaxonomySource) error {
	if r.format == "json" {
		ranks := [][]string(taxonomy)
		if ranks == nil {
			ranks = [][]string{}
		}
		payload := projectShowJSON{
			projectJSON: toProjectJSON(p, workflowName),
			EffectiveTaxonomy: effectiveTaxonomyShowJSON{
				Ranks:  ranks,
				Source: taxonomySourceLabel(source),
			},
		}
		enc := json.NewEncoder(r.w)
		enc.SetIndent("", "  ")
		return enc.Encode(payload)
	}

	if _, err := fmt.Fprintf(r.w, "%s %s\n", r.paddedLabel("Name:", 12), p.Name); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(r.w, "%s %s\n", r.paddedLabel("Workflow:", 12), workflowName); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(r.w, "%s %s\n", r.paddedLabel("Settings:", 12), formatSettingsSummary(p.Settings)); err != nil {
		return err
	}
	// Taxonomy block. Provenance source dictates whether we emit a
	// `source:` line, a disabled placeholder, or nothing.
	switch source {
	case service.TaxonomySourceProjectOverride:
		if taxonomy.IsEmpty() {
			if _, err := fmt.Fprintf(r.w, "%s (disabled; project opted out)\n", r.paddedLabel("Taxonomy:", 12)); err != nil {
				return err
			}
			return nil
		}
		if _, err := fmt.Fprintf(r.w, "%s %s\n", r.paddedLabel("Taxonomy:", 12), FormatTaxonomyInline(taxonomy)); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(r.w, "  source: project override\n"); err != nil {
			return err
		}
	case service.TaxonomySourceWorkspace:
		if _, err := fmt.Fprintf(r.w, "%s %s\n", r.paddedLabel("Taxonomy:", 12), FormatTaxonomyInline(taxonomy)); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(r.w, "  source: workspace default\n"); err != nil {
			return err
		}
	default:
		if _, err := fmt.Fprintf(r.w, "%s (none)\n", r.paddedLabel("Taxonomy:", 12)); err != nil {
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

// formatUrgency formats an urgency score for display.
func formatUrgency(u float64) string {
	if u == 0 {
		return "  0"
	}
	return fmt.Sprintf("%.1f", u)
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
	Level          *string        `json:"level,omitempty"`
	Status         string         `json:"status"`
	Priority       int            `json:"priority"`
	Order          *float64       `json:"order,omitempty"`
	Version        int            `json:"version"`
	Tags           []string       `json:"tags"`
	DueAt          *string        `json:"due_at,omitempty"`
	WaitUntil      *string        `json:"wait_until,omitempty"`
	RecurrenceRule *string        `json:"recurrence_rule,omitempty"`
	UDA            map[string]any `json:"uda,omitempty"`
	ClaimedBy      *string        `json:"claimed_by,omitempty"`
	ClaimedAt      *string        `json:"claimed_at,omitempty"`
	CreatedAt      string         `json:"created_at"`
	ModifiedAt     string         `json:"modified_at"`
	Urgency        float64        `json:"urgency"`
}

func (r *Renderer) toTaskJSON(t *domain.Task, tags []*domain.Tag) taskJSON {
	tj := taskJSON{
		ID:          t.ID.String(),
		ShortID:     t.ShortID,
		Title:       t.Title,
		Description: t.Description,
		Status:      t.Status,
		Priority:    t.Priority,
		Order:       t.Order,
		Version:     t.Version,
		UDA:         t.UDA,
		CreatedAt:   t.CreatedAt.Format(time.RFC3339),
		ModifiedAt:  t.ModifiedAt.Format(time.RFC3339),
		Urgency:     t.Urgency,
	}
	if t.ParentID != nil {
		s := t.ParentID.String()
		tj.ParentID = &s
	}
	tj.ProjectID = r.projectName(t.ProjectID)
	if t.DueAt != nil {
		s := t.DueAt.Format(time.RFC3339)
		tj.DueAt = &s
	}
	if t.WaitUntil != nil {
		s := t.WaitUntil.Format(time.RFC3339)
		tj.WaitUntil = &s
	}
	tj.RecurrenceRule = t.RecurrenceRule
	tj.Level = t.Level
	if t.ClaimedBy != nil {
		tj.ClaimedBy = t.ClaimedBy
	}
	if t.ClaimedAt != nil {
		s := t.ClaimedAt.Format(time.RFC3339)
		tj.ClaimedAt = &s
	}
	tj.Tags = make([]string, len(tags))
	for i, tg := range tags {
		tj.Tags[i] = tg.Name
	}
	return tj
}

// renderTaskList writes a list of tasks to w in the given format.
// For "text", it renders a fixed-width table. For "json", it renders a JSON array.
// If the list is empty and format is "text", nothing is written.
func (r *Renderer) renderTaskList(tasks []*domain.Task, taskTags map[string][]*domain.Tag) error {
	if r.format == "json" {
		items := make([]taskJSON, len(tasks))
		for i, t := range tasks {
			items[i] = r.toTaskJSON(t, taskTags[t.ID.String()])
		}
		enc := json.NewEncoder(r.w)
		enc.SetIndent("", "  ")
		return enc.Encode(items)
	}

	if len(tasks) == 0 {
		return nil
	}

	idH := r.styledHeader("ID")
	statusH := r.styledHeader("Status")
	priH := r.styledHeader("Pri")
	ageH := r.styledHeader("Age")
	urgH := r.styledHeader("Urg")
	titleH := r.styledHeader("Title")
	if _, err := fmt.Fprintf(r.w, "%s%s %s%s %s%s %s%s %s%s %s\n",
		idH, strings.Repeat(" ", max(0, 8-lipgloss.Width(idH))),
		statusH, strings.Repeat(" ", max(0, 9-lipgloss.Width(statusH))),
		priH, strings.Repeat(" ", max(0, 4-lipgloss.Width(priH))),
		ageH, strings.Repeat(" ", max(0, 5-lipgloss.Width(ageH))),
		urgH, strings.Repeat(" ", max(0, 6-lipgloss.Width(urgH))),
		titleH,
	); err != nil {
		return err
	}
	for _, t := range tasks {
		title := t.Title
		if tags, ok := taskTags[t.ID.String()]; ok && len(tags) > 0 {
			tagStrs := make([]string, len(tags))
			for i, tg := range tags {
				tagStrs[i] = r.styledTag(tg)
			}
			title = title + "  " + strings.Join(tagStrs, " ")
		}
		priStr := r.styledPriority(t.Priority)
		priPad := strings.Repeat(" ", max(0, 4-lipgloss.Width(priStr)))
		line := fmt.Sprintf("%-8s %-9s %s%s %-5s %-6s %s",
			t.ShortID,
			t.Status,
			priStr,
			priPad,
			formatAge(t.CreatedAt),
			formatUrgency(t.Urgency),
			title,
		)
		if r.isDimStatus(t.Status) {
			line = r.styles.Dim.Render(line)
		}
		if _, err := fmt.Fprintln(r.w, line); err != nil {
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
func (r *Renderer) renderTaskInfo(task *domain.Task, annotations []*domain.Annotation, tags []*domain.Tag, relations []resolvedRelation) error {
	if r.format == "json" {
		info := taskInfoJSON{taskJSON: r.toTaskJSON(task, tags)}
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
		enc := json.NewEncoder(r.w)
		enc.SetIndent("", "  ")
		return enc.Encode(info)
	}

	if _, err := fmt.Fprintf(r.w, "%s %s\n", r.paddedLabel("ID:", 13), task.ShortID); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(r.w, "%s %s\n", r.paddedLabel("Title:", 13), task.Title); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(r.w, "%s %s\n", r.paddedLabel("Status:", 13), task.Status); err != nil {
		return err
	}
	priName := formatPriorityName(task.Priority)
	if r.styles != nil {
		idx := task.Priority
		if idx < 0 || idx > 4 {
			idx = 0
		}
		priName = r.styles.Priority[idx].Render(priName)
	}
	if _, err := fmt.Fprintf(r.w, "%s %s\n", r.paddedLabel("Priority:", 13), priName); err != nil {
		return err
	}
	if task.Order != nil {
		if _, err := fmt.Fprintf(r.w, "%s %s\n", r.paddedLabel("Order:", 13), strconv.FormatFloat(*task.Order, 'f', -1, 64)); err != nil {
			return err
		}
	}
	if r.hasTaxonomy(task.ProjectID) {
		level := "—"
		if task.Level != nil && *task.Level != "" {
			level = *task.Level
		}
		if _, err := fmt.Fprintf(r.w, "%s %s\n", r.paddedLabel("Level:", 13), level); err != nil {
			return err
		}
	}

	if len(tags) > 0 {
		tagStrs := make([]string, len(tags))
		for i, tg := range tags {
			tagStrs[i] = r.styledTag(tg)
		}
		if _, err := fmt.Fprintf(r.w, "%s %s\n", r.paddedLabel("Tags:", 13), strings.Join(tagStrs, " ")); err != nil {
			return err
		}
	}

	if task.Description != "" {
		if _, err := fmt.Fprintln(r.w, r.paddedLabel("Description:", 13)); err != nil {
			return err
		}
		if _, err := fmt.Fprintln(r.w); err != nil {
			return err
		}
		rendered, err := r.renderMarkdown(task.Description)
		if err != nil {
			rendered = task.Description
		}
		if _, err := fmt.Fprintln(r.w, rendered); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(r.w, "%s %s\n", r.paddedLabel("Project:", 13), r.projectName(task.ProjectID)); err != nil {
		return err
	}
	if task.ParentID != nil {
		if _, err := fmt.Fprintf(r.w, "%s %s\n", r.paddedLabel("Parent:", 13), task.ParentID.String()); err != nil {
			return err
		}
	}
	if task.DueAt != nil {
		if _, err := fmt.Fprintf(r.w, "%s %s\n", r.paddedLabel("Due:", 13), task.DueAt.Format("2006-01-02")); err != nil {
			return err
		}
	}
	if task.WaitUntil != nil {
		if _, err := fmt.Fprintf(r.w, "%s %s\n", r.paddedLabel("Wait:", 13), task.WaitUntil.Format("2006-01-02 15:04:05")); err != nil {
			return err
		}
	}
	if task.RecurrenceRule != nil {
		if _, err := fmt.Fprintf(r.w, "%s %s\n", r.paddedLabel("Recurrence:", 13), *task.RecurrenceRule); err != nil {
			return err
		}
	}
	if task.ClaimedBy != nil {
		if _, err := fmt.Fprintf(r.w, "%s %s\n", r.paddedLabel("Claimed By:", 13), *task.ClaimedBy); err != nil {
			return err
		}
	}
	if task.ClaimedAt != nil {
		if _, err := fmt.Fprintf(r.w, "%s %s\n", r.paddedLabel("Claimed At:", 13), task.ClaimedAt.Format("2006-01-02 15:04:05")); err != nil {
			return err
		}
	}
	if len(task.UDA) > 0 {
		if _, err := fmt.Fprintln(r.w, r.styledLabel("UDA:")); err != nil {
			return err
		}
		if err := r.renderUDASection(task.UDA); err != nil {
			return err
		}
	}

	if _, err := fmt.Fprintf(r.w, "%s %s\n", r.paddedLabel("Created:", 13), task.CreatedAt.Format("2006-01-02 15:04:05")); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(r.w, "%s %s\n", r.paddedLabel("Modified:", 13), task.ModifiedAt.Format("2006-01-02 15:04:05")); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(r.w, "%s %d\n", r.paddedLabel("Version:", 13), task.Version); err != nil {
		return err
	}

	if len(annotations) > 0 {
		if _, err := fmt.Fprintln(r.w); err != nil {
			return err
		}
		if _, err := fmt.Fprintln(r.w, r.styledLabel("Annotations:")); err != nil {
			return err
		}
		for _, ann := range annotations {
			if _, err := fmt.Fprintf(r.w, "  %s - %s\n", ann.CreatedAt.Format("2006-01-02 15:04"), ann.Body); err != nil {
				return err
			}
		}
	}

	if len(relations) > 0 {
		if _, err := fmt.Fprintln(r.w); err != nil {
			return err
		}
		if _, err := fmt.Fprintln(r.w, r.styledLabel("Relations:")); err != nil {
			return err
		}
		for _, rr := range relations {
			if _, err := fmt.Fprintf(r.w, "  %-14s %-8s  %s\n", rr.Label, rr.RelatedShortID, rr.RelatedTitle); err != nil {
				return err
			}
		}
	}

	return nil
}

// renderUDASection writes UDA key-value pairs as an indented block.
// Keys are sorted alphabetically. Single-line values appear inline;
// multi-line values appear indented below the key.
func (r *Renderer) renderUDASection(uda map[string]any) error {
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
			if _, err := fmt.Fprintf(r.w, "  %s:\n", k); err != nil {
				return err
			}
			for _, line := range strings.Split(v, "\n") {
				if _, err := fmt.Fprintf(r.w, "    %s\n", line); err != nil {
					return err
				}
			}
		} else {
			// Single-line: inline after key with aligned padding
			if _, err := fmt.Fprintf(r.w, "  %-*s  %s\n", maxKeyLen+1, k+":", v); err != nil {
				return err
			}
		}
	}
	return nil
}

// levelCheckJSON is the JSON payload returned by `tusk task level-check`.
// Task mirrors the existing taskJSON shape; each record carries the violating
// reason, the resolved taxonomy, and the provenance source.
type levelCheckJSON struct {
	Task        taskJSON      `json:"task"`
	Reason      string        `json:"reason"`
	ParentLevel string        `json:"parent_level,omitempty"`
	Taxonomy    taxonomyRanks `json:"taxonomy"`
	Source      string        `json:"source"`
}

// taxonomyRanks wraps a taxonomy's rank groups in the `{"ranks": [...]}`
// shape used elsewhere in MCP-facing JSON.
type taxonomyRanks struct {
	Ranks [][]string `json:"ranks"`
}

// renderLevelCheck writes a retroactive-violation report.
// Text: one line per violation, sorted by project / short_id. JSON: array of
// structured records including the task, reason, taxonomy, and source.
func (r *Renderer) renderLevelCheck(violations []service.LevelViolation) error {
	if r.format == "json" {
		items := make([]levelCheckJSON, len(violations))
		for i, v := range violations {
			ranks := [][]string(v.Taxonomy)
			if ranks == nil {
				ranks = [][]string{}
			}
			items[i] = levelCheckJSON{
				Task:        r.toTaskJSON(v.Task, nil),
				Reason:      v.Err.Reason,
				ParentLevel: v.Err.ParentLevel,
				Taxonomy:    taxonomyRanks{Ranks: ranks},
				Source:      taxonomySourceLabel(v.Source),
			}
		}
		enc := json.NewEncoder(r.w)
		enc.SetIndent("", "  ")
		return enc.Encode(items)
	}

	for _, v := range violations {
		level := "—"
		if v.Task.Level != nil && *v.Task.Level != "" {
			level = *v.Task.Level
		}
		reason := v.Err.Reason
		if r.styles != nil {
			reason = r.styles.Priority[4].Render(reason)
		}
		projectName := r.projectName(v.Task.ProjectID)
		parent := "—"
		if v.Err.ParentLevel != "" {
			parent = v.Err.ParentLevel
		}
		if _, err := fmt.Fprintf(r.w, "%-8s  %-20s  %-12s  %-22s  (parent: %s)\n",
			v.Task.ShortID, projectName, level, reason, parent,
		); err != nil {
			return err
		}
	}
	return nil
}

// taxonomySourceLabel maps service.TaxonomySource to the short identifier
// used in MCP/JSON responses. Centralised here so the level-check renderer
// and the MCP project response stay aligned.
func taxonomySourceLabel(src service.TaxonomySource) string {
	switch src {
	case service.TaxonomySourceProjectOverride:
		return "project_override"
	case service.TaxonomySourceWorkspace:
		return "workspace_default"
	default:
		return "none"
	}
}

// renderMutationResult writes a one-line confirmation (text) or full task JSON.
// action is a past-tense verb like "Created", "Modified", "Started", etc.
func (r *Renderer) renderMutationResult(action string, task *domain.Task, tags []*domain.Tag) error {
	if r.format == "json" {
		enc := json.NewEncoder(r.w)
		enc.SetIndent("", "  ")
		return enc.Encode(r.toTaskJSON(task, tags))
	}
	_, err := fmt.Fprintf(r.w, "%s task %s\n", action, task.ShortID)
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

// renderUnlinkResult writes an unlink confirmation (text) or empty JSON object.
func (r *Renderer) renderUnlinkResult(sourceShortID, relType, targetShortID string) error {
	if r.format == "json" {
		_, err := fmt.Fprintln(r.w, "{}")
		return err
	}
	_, err := fmt.Fprintf(r.w, "Unlinked %s %s %s\n", sourceShortID, relType, targetShortID)
	return err
}

// renderLinkResult writes a link confirmation (text) or full relation JSON.
func (r *Renderer) renderLinkResult(rel *domain.Relation, sourceShortID, targetShortID string) error {
	if r.format == "json" {
		enc := json.NewEncoder(r.w)
		enc.SetIndent("", "  ")
		return enc.Encode(relationJSON{
			ID:           rel.ID.String(),
			SourceID:     rel.SourceID.String(),
			TargetID:     rel.TargetID.String(),
			RelationType: rel.RelationType,
			CreatedAt:    rel.CreatedAt.Format(time.RFC3339),
		})
	}
	_, err := fmt.Fprintf(r.w, "Linked %s %s %s\n", sourceShortID, rel.RelationType, targetShortID)
	return err
}

// statusJSON is the JSON serialization format for a workflow status with roles.
type statusJSON struct {
	Name  string   `json:"name"`
	Roles []string `json:"roles,omitempty"`
}

// workflowJSON is the JSON serialization format for a workflow.
type workflowJSON struct {
	Name        string              `json:"name"`
	Statuses    []statusJSON        `json:"statuses"`
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
	names := wf.StatusNames()
	statuses := make([]statusJSON, len(names))
	for i, name := range names {
		sc := wf.Statuses[name]
		roles := make([]string, len(sc.Roles))
		for j, r := range sc.Roles {
			roles[j] = string(r)
		}
		statuses[i] = statusJSON{Name: name, Roles: roles}
	}
	return workflowJSON{
		Name:        wf.Name,
		Statuses:    statuses,
		Transitions: transitions,
	}
}

// renderWorkflowList writes a list of workflows to w.
// workflowProjects maps workflow name to referencing project IDs.
func (r *Renderer) renderWorkflowList(workflows []*domain.Workflow, workflowProjects map[string][]string) error {
	if r.format == "json" {
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
		enc := json.NewEncoder(r.w)
		enc.SetIndent("", "  ")
		return enc.Encode(items)
	}

	if len(workflows) == 0 {
		return nil
	}

	nameH := r.styledHeader("NAME")
	statusesH := r.styledHeader("STATUSES")
	if _, err := fmt.Fprintf(r.w, "%s%s %s\n",
		nameH, strings.Repeat(" ", max(0, 20-lipgloss.Width(nameH))),
		statusesH,
	); err != nil {
		return err
	}
	for _, wf := range workflows {
		if _, err := fmt.Fprintf(r.w, "%-20s %s\n", wf.Name, strings.Join(wf.StatusNames(), ", ")); err != nil {
			return err
		}
	}
	return nil
}

// renderWorkflowInfo writes a detailed workflow view to w.
func (r *Renderer) renderWorkflowInfo(wf *domain.Workflow, projectIDs []string) error {
	if r.format == "json" {
		if projectIDs == nil {
			projectIDs = []string{}
		}
		info := workflowInfoJSON{
			workflowJSON: toWorkflowJSON(wf),
			Projects:     projectIDs,
		}
		enc := json.NewEncoder(r.w)
		enc.SetIndent("", "  ")
		return enc.Encode(info)
	}

	if _, err := fmt.Fprintf(r.w, "%s %s\n", r.paddedLabel("Workflow:", 13), wf.Name); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(r.w, "%s %s\n", r.paddedLabel("Statuses:", 13), strings.Join(wf.StatusNames(), ", ")); err != nil {
		return err
	}
	// Show roles per status
	for _, name := range wf.StatusNames() {
		sc := wf.Statuses[name]
		if len(sc.Roles) > 0 {
			roles := make([]string, len(sc.Roles))
			for i, r := range sc.Roles {
				roles[i] = string(r)
			}
			if _, err := fmt.Fprintf(r.w, "  %-15s %s\n", name+":", strings.Join(roles, ", ")); err != nil {
				return err
			}
		}
	}

	if len(wf.Transitions) > 0 {
		if _, err := fmt.Fprintln(r.w, r.styledLabel("Transitions:")); err != nil {
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
			if _, err := fmt.Fprintf(r.w, fmtStr, t.FromStatus, t.ToStatus); err != nil {
				return err
			}
		}
	}

	if len(projectIDs) > 0 {
		if _, err := fmt.Fprintf(r.w, "%s %s\n", r.paddedLabel("Projects:", 13), strings.Join(projectIDs, ", ")); err != nil {
			return err
		}
	}

	return nil
}

// renderWorkflowMutation writes a workflow mutation confirmation.
func (r *Renderer) renderWorkflowMutation(action string, name string) error {
	if r.format == "json" {
		enc := json.NewEncoder(r.w)
		enc.SetIndent("", "  ")
		return enc.Encode(map[string]string{
			"action":   strings.ToLower(action),
			"workflow": name,
		})
	}
	_, err := fmt.Fprintf(r.w, "%s workflow %s\n", action, name)
	return err
}

// renderProjectMutation writes a project mutation confirmation.
func (r *Renderer) renderProjectMutation(action string, name string) error {
	if r.format == "json" {
		enc := json.NewEncoder(r.w)
		enc.SetIndent("", "  ")
		return enc.Encode(map[string]string{
			"action":  strings.ToLower(action),
			"project": name,
		})
	}
	_, err := fmt.Fprintf(r.w, "%s project %s\n", action, name)
	return err
}

// playerJSON is the JSON serialization format for a player.
type playerJSON struct {
	ID             string `json:"id"`
	Type           string `json:"type"`
	NoteWindowSize *int   `json:"note_window_size,omitempty"`
	RegisteredAt   string `json:"registered_at"`
	LastSeenAt     string `json:"last_seen_at"`
}

func toPlayerJSON(p *domain.Player) playerJSON {
	return playerJSON{
		ID:             p.ID,
		Type:           p.Type,
		NoteWindowSize: p.NoteWindowSize,
		RegisteredAt:   p.RegisteredAt.Format(time.RFC3339),
		LastSeenAt:     p.LastSeenAt.Format(time.RFC3339),
	}
}

// renderPlayerResult writes a player mutation result.
func (r *Renderer) renderPlayerResult(action string, player *domain.Player) error {
	if r.format == "json" {
		enc := json.NewEncoder(r.w)
		enc.SetIndent("", "  ")
		return enc.Encode(toPlayerJSON(player))
	}
	_, err := fmt.Fprintf(r.w, "%s player %s (type: %s)\n", action, player.ID, player.Type)
	if err != nil {
		return err
	}
	if player.NoteWindowSize != nil {
		_, err = fmt.Fprintf(r.w, "  note_window_size: %d\n", *player.NoteWindowSize)
	}
	return err
}
