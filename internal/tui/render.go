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
	ID          string                 `json:"id"`
	Workflow    string                 `json:"workflow"`
	Description string                 `json:"description"`
	Settings    domain.ProjectSettings `json:"settings"`
}

func toProjectJSON(project *domain.Project, workflowName string) projectJSON {
	return projectJSON{
		ID:          project.Name,
		Workflow:    workflowName,
		Description: project.Description,
		Settings:    project.Settings,
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

func toTagJSON(tag *domain.Tag) tagJSON {
	return tagJSON{
		ID:    tag.ID.String(),
		Name:  tag.Name,
		Color: tag.Color,
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
func (renderer *Renderer) renderTagList(tags []domain.TagWithUsage, showUsage bool) error {
	if renderer.format == "json" {
		items := make([]tagWithUsageJSON, len(tags))
		for index, tw := range tags {
			items[index] = toTagWithUsageJSON(tw)
		}
		enc := json.NewEncoder(renderer.w)
		enc.SetIndent("", "  ")
		return enc.Encode(items)
	}

	if len(tags) == 0 {
		return nil
	}

	if showUsage {
		nameH := renderer.styledHeader("NAME")
		colorH := renderer.styledHeader("COLOR")
		tasksH := renderer.styledHeader("TASKS")
		if _, err := fmt.Fprintf(renderer.w, "%s%s %s%s %s\n",
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
			if _, err := fmt.Fprintf(renderer.w, "%-20s %-10s %d\n", tw.Tag.Name, color, tw.TaskCount); err != nil {
				return err
			}
		}
	} else {
		nameH := renderer.styledHeader("NAME")
		colorH := renderer.styledHeader("COLOR")
		if _, err := fmt.Fprintf(renderer.w, "%s%s %s\n",
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
			if _, err := fmt.Fprintf(renderer.w, "%-20s %s\n", tw.Tag.Name, color); err != nil {
				return err
			}
		}
	}
	return nil
}

// renderTagResult writes a single tag mutation result.
func (renderer *Renderer) renderTagResult(action string, tag *domain.Tag) error {
	if renderer.format == "json" {
		enc := json.NewEncoder(renderer.w)
		enc.SetIndent("", "  ")
		return enc.Encode(toTagJSON(tag))
	}
	_, err := fmt.Fprintf(renderer.w, "%s tag %s\n", action, tag.Name)
	return err
}

// renderTagRenameResult writes a tag rename confirmation (text) or tag JSON.
func (renderer *Renderer) renderTagRenameResult(oldName string, tag *domain.Tag) error {
	if renderer.format == "json" {
		enc := json.NewEncoder(renderer.w)
		enc.SetIndent("", "  ")
		return enc.Encode(toTagJSON(tag))
	}
	_, err := fmt.Fprintf(renderer.w, "Renamed tag %s to %s\n", oldName, tag.Name)
	return err
}

// renderProjectList writes a list of projects to w.
// Text format renders a table; JSON format renders an array.
// workflowNames maps workflow UUIDs to their display names.
func (renderer *Renderer) renderProjectList(projects []*domain.Project, workflowNames map[uuid.UUID]string) error {
	if renderer.format == "json" {
		items := make([]projectJSON, len(projects))
		for index, project := range projects {
			items[index] = toProjectJSON(project, workflowNames[project.WorkflowID])
		}
		enc := json.NewEncoder(renderer.w)
		enc.SetIndent("", "  ")
		return enc.Encode(items)
	}

	if len(projects) == 0 {
		return nil
	}

	idH := renderer.styledHeader("ID")
	wfH := renderer.styledHeader("WORKFLOW")
	settH := renderer.styledHeader("SETTINGS")
	if _, err := fmt.Fprintf(renderer.w, "%s%s %s%s %s\n",
		idH, strings.Repeat(" ", max(0, 20-lipgloss.Width(idH))),
		wfH, strings.Repeat(" ", max(0, 10-lipgloss.Width(wfH))),
		settH,
	); err != nil {
		return err
	}
	for _, project := range projects {
		if _, err := fmt.Fprintf(renderer.w, "%-20s %-10s %s\n",
			project.Name,
			workflowNames[project.WorkflowID],
			formatSettingsSummary(project.Settings),
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
func (renderer *Renderer) renderProjectShow(proj *domain.Project, workflowName string, taxonomy domain.Taxonomy, source service.TaxonomySource) error {
	if renderer.format == "json" {
		ranks := [][]string(taxonomy)
		if ranks == nil {
			ranks = [][]string{}
		}
		payload := projectShowJSON{
			projectJSON: toProjectJSON(proj, workflowName),
			EffectiveTaxonomy: effectiveTaxonomyShowJSON{
				Ranks:  ranks,
				Source: taxonomySourceLabel(source),
			},
		}
		enc := json.NewEncoder(renderer.w)
		enc.SetIndent("", "  ")
		return enc.Encode(payload)
	}

	if _, err := fmt.Fprintf(renderer.w, "%s %s\n", renderer.paddedLabel("Name:", 12), proj.Name); err != nil {
		return err
	}
	if proj.Description != "" {
		if _, err := fmt.Fprintln(renderer.w, renderer.paddedLabel("Description:", 12)); err != nil {
			return err
		}
		for _, line := range strings.Split(proj.Description, "\n") {
			if _, err := fmt.Fprintf(renderer.w, "  %s\n", line); err != nil {
				return err
			}
		}
	}
	if _, err := fmt.Fprintf(renderer.w, "%s %s\n", renderer.paddedLabel("Workflow:", 12), workflowName); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(renderer.w, "%s %s\n", renderer.paddedLabel("Settings:", 12), formatSettingsSummary(proj.Settings)); err != nil {
		return err
	}
	// Taxonomy block. Provenance source dictates whether we emit a
	// `source:` line, a disabled placeholder, or nothing.
	switch source {
	case service.TaxonomySourceProjectOverride:
		if taxonomy.IsEmpty() {
			if _, err := fmt.Fprintf(renderer.w, "%s (disabled; project opted out)\n", renderer.paddedLabel("Taxonomy:", 12)); err != nil {
				return err
			}
			return nil
		}
		if _, err := fmt.Fprintf(renderer.w, "%s %s\n", renderer.paddedLabel("Taxonomy:", 12), FormatTaxonomyInline(taxonomy)); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(renderer.w, "  source: project override\n"); err != nil {
			return err
		}
	case service.TaxonomySourceWorkspace:
		if _, err := fmt.Fprintf(renderer.w, "%s %s\n", renderer.paddedLabel("Taxonomy:", 12), FormatTaxonomyInline(taxonomy)); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(renderer.w, "  source: workspace default\n"); err != nil {
			return err
		}
	default:
		if _, err := fmt.Fprintf(renderer.w, "%s (none)\n", renderer.paddedLabel("Taxonomy:", 12)); err != nil {
			return err
		}
	}
	return nil
}

// formatSettingsSummary returns a compact text summary of project settings.
func formatSettingsSummary(settings domain.ProjectSettings) string {
	var parts []string
	if settings.AutoCompleteParent != nil {
		parts = append(parts, "auto-complete:on")
	}
	if settings.AutoRevertParent != nil {
		parts = append(parts, "auto-revert:on")
	}
	if len(parts) == 0 {
		return "-"
	}
	return strings.Join(parts, " ")
}

// formatPriority converts a priority int (0-4) to a single display character.
func formatPriority(priority int) string {
	switch priority {
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
func formatUrgency(urgency float64) string {
	if urgency == 0 {
		return "  0"
	}
	return fmt.Sprintf("%.1f", urgency)
}

// formatAge converts a creation time to a human-readable relative age string.
func formatAge(created time.Time) string {
	duration := time.Since(created)
	switch {
	case duration < time.Hour:
		return fmt.Sprintf("%dm", int(duration.Minutes()))
	case duration < 24*time.Hour:
		return fmt.Sprintf("%dh", int(duration.Hours()))
	case duration < 14*24*time.Hour:
		return fmt.Sprintf("%dd", int(duration.Hours()/24))
	case duration < 60*24*time.Hour:
		return fmt.Sprintf("%dw", int(duration.Hours()/(24*7)))
	case duration < 365*24*time.Hour:
		return fmt.Sprintf("%dmo", int(duration.Hours()/(24*30)))
	default:
		return fmt.Sprintf("%dy", int(duration.Hours()/(24*365)))
	}
}

// urgencyOverridesJSON is the sparse per-task self overrides; only keys
// explicitly set on the task appear.
type urgencyOverridesJSON struct {
	PriorityWeight    *float64 `json:"priority_weight,omitempty"`
	DueWeight         *float64 `json:"due_weight,omitempty"`
	AgeWeight         *float64 `json:"age_weight,omitempty"`
	ActiveWeight      *float64 `json:"active_weight,omitempty"`
	BlockingWeight    *float64 `json:"blocking_weight,omitempty"`
	BlockedWeight     *float64 `json:"blocked_weight,omitempty"`
	TagsWeight        *float64 `json:"tags_weight,omitempty"`
	ProjectWeight     *float64 `json:"project_weight,omitempty"`
	AnnotationsWeight *float64 `json:"annotations_weight,omitempty"`
	WaitingWeight     *float64 `json:"waiting_weight,omitempty"`
}

// urgencyWeightsJSON is the full 10-weight resolved table; all fields
// always present when emitted.
type urgencyWeightsJSON struct {
	PriorityWeight    float64 `json:"priority_weight"`
	DueWeight         float64 `json:"due_weight"`
	AgeWeight         float64 `json:"age_weight"`
	ActiveWeight      float64 `json:"active_weight"`
	BlockingWeight    float64 `json:"blocking_weight"`
	BlockedWeight     float64 `json:"blocked_weight"`
	TagsWeight        float64 `json:"tags_weight"`
	ProjectWeight     float64 `json:"project_weight"`
	AnnotationsWeight float64 `json:"annotations_weight"`
	WaitingWeight     float64 `json:"waiting_weight"`
}

// rollupJSON is the JSON serialization format for a Rollup of descendants.
// Used by `tusk task tree --rollup` (per-node) and `tusk task summary` (per
// block and totals). status_counts is always a non-nil slice so it encodes
// as `[]` (not `null`) when empty.
type rollupJSON struct {
	Done         int               `json:"done"`
	Total        int               `json:"total"`
	Percent      float64           `json:"percent"`
	StatusCounts []statusCountJSON `json:"status_counts"`
}

// statusCountJSON is one entry in a rollup's status breakdown.
type statusCountJSON struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// toRollupJSON converts a domain.Rollup to its wire form. status_counts is
// always a non-nil slice so it encodes as `[]` (not `null`) when empty.
func toRollupJSON(roll domain.Rollup) rollupJSON {
	counts := make([]statusCountJSON, 0, len(roll.StatusCounts))
	for _, statusCount := range roll.StatusCounts {
		counts = append(counts, statusCountJSON{Name: statusCount.Name, Count: statusCount.Count})
	}
	return rollupJSON{
		Done:         roll.Done,
		Total:        roll.Total,
		Percent:      roll.Percent,
		StatusCounts: counts,
	}
}

// taskJSON is the JSON serialization format for a task.
// Field names use snake_case to match the domain model.
type taskJSON struct {
	ID                      string                `json:"id"`
	ShortID                 string                `json:"short_id"`
	ParentID                *string               `json:"parent_id,omitempty"`
	ProjectID               string                `json:"project_id"`
	Title                   string                `json:"title"`
	Description             string                `json:"description"`
	Level                   *string               `json:"level,omitempty"`
	Status                  string                `json:"status"`
	Priority                int                   `json:"priority"`
	Order                   *float64              `json:"order,omitempty"`
	Version                 int                   `json:"version"`
	Tags                    []string              `json:"tags"`
	DueAt                   *string               `json:"due_at,omitempty"`
	WaitUntil               *string               `json:"wait_until,omitempty"`
	RecurrenceRule          *string               `json:"recurrence_rule,omitempty"`
	UDA                     map[string]any        `json:"uda,omitempty"`
	ClaimedBy               *string               `json:"claimed_by,omitempty"`
	ClaimedAt               *string               `json:"claimed_at,omitempty"`
	CreatedAt               string                `json:"created_at"`
	ModifiedAt              string                `json:"modified_at"`
	Urgency                 float64               `json:"urgency"`
	UrgencyOverrides        *urgencyOverridesJSON `json:"urgency_overrides,omitempty"`
	EffectiveUrgencyWeights *urgencyWeightsJSON   `json:"effective_urgency_weights,omitempty"`
}

func (renderer *Renderer) toTaskJSON(task *domain.Task, tags []*domain.Tag) taskJSON {
	tj := taskJSON{
		ID:          task.ID.String(),
		ShortID:     task.ShortID,
		Title:       task.Title,
		Description: task.Description,
		Status:      task.Status,
		Priority:    task.Priority,
		Order:       task.Order,
		Version:     task.Version,
		UDA:         task.UDA,
		CreatedAt:   task.CreatedAt.Format(time.RFC3339),
		ModifiedAt:  task.ModifiedAt.Format(time.RFC3339),
		Urgency:     task.Urgency,
	}
	if task.ParentID != nil {
		str := task.ParentID.String()
		tj.ParentID = &str
	}
	tj.ProjectID = renderer.projectName(task.ProjectID)
	if task.DueAt != nil {
		str := task.DueAt.Format(time.RFC3339)
		tj.DueAt = &str
	}
	if task.WaitUntil != nil {
		str := task.WaitUntil.Format(time.RFC3339)
		tj.WaitUntil = &str
	}
	tj.RecurrenceRule = task.RecurrenceRule
	tj.Level = task.Level
	if task.ClaimedBy != nil {
		tj.ClaimedBy = task.ClaimedBy
	}
	if task.ClaimedAt != nil {
		str := task.ClaimedAt.Format(time.RFC3339)
		tj.ClaimedAt = &str
	}
	tj.Tags = make([]string, len(tags))
	for index, tag := range tags {
		tj.Tags[index] = tag.Name
	}
	if task.UrgencyOverrides != nil {
		tj.UrgencyOverrides = toUrgencyOverridesJSON(task.UrgencyOverrides)
	}
	if task.EffectiveWeights != nil {
		tj.EffectiveUrgencyWeights = toUrgencyWeightsJSON(*task.EffectiveWeights)
	}
	return tj
}

func toUrgencyOverridesJSON(overrides *domain.UrgencyOverrides) *urgencyOverridesJSON {
	return &urgencyOverridesJSON{
		PriorityWeight:    overrides.PriorityWeight,
		DueWeight:         overrides.DueWeight,
		AgeWeight:         overrides.AgeWeight,
		ActiveWeight:      overrides.ActiveWeight,
		BlockingWeight:    overrides.BlockingWeight,
		BlockedWeight:     overrides.BlockedWeight,
		TagsWeight:        overrides.TagsWeight,
		ProjectWeight:     overrides.ProjectWeight,
		AnnotationsWeight: overrides.AnnotationsWeight,
		WaitingWeight:     overrides.WaitingWeight,
	}
}

func toUrgencyWeightsJSON(weights domain.ResolvedUrgencyWeights) *urgencyWeightsJSON {
	return &urgencyWeightsJSON{
		PriorityWeight:    weights.PriorityWeight,
		DueWeight:         weights.DueWeight,
		AgeWeight:         weights.AgeWeight,
		ActiveWeight:      weights.ActiveWeight,
		BlockingWeight:    weights.BlockingWeight,
		BlockedWeight:     weights.BlockedWeight,
		TagsWeight:        weights.TagsWeight,
		ProjectWeight:     weights.ProjectWeight,
		AnnotationsWeight: weights.AnnotationsWeight,
		WaitingWeight:     weights.WaitingWeight,
	}
}

// renderTaskList writes a list of tasks to w in the given format.
// For "text", it renders a fixed-width table. For "json", it renders a JSON array.
// If the list is empty and format is "text", nothing is written.
func (renderer *Renderer) renderTaskList(tasks []*domain.Task, taskTags map[string][]*domain.Tag) error {
	if renderer.format == "json" {
		items := make([]taskJSON, len(tasks))
		for index, task := range tasks {
			items[index] = renderer.toTaskJSON(task, taskTags[task.ID.String()])
		}
		enc := json.NewEncoder(renderer.w)
		enc.SetIndent("", "  ")
		return enc.Encode(items)
	}

	if len(tasks) == 0 {
		return nil
	}

	idH := renderer.styledHeader("ID")
	statusH := renderer.styledHeader("Status")
	priH := renderer.styledHeader("Pri")
	ageH := renderer.styledHeader("Age")
	urgH := renderer.styledHeader("Urg")
	titleH := renderer.styledHeader("Title")
	if _, err := fmt.Fprintf(renderer.w, "%s%s %s%s %s%s %s%s %s%s %s\n",
		idH, strings.Repeat(" ", max(0, 8-lipgloss.Width(idH))),
		statusH, strings.Repeat(" ", max(0, 9-lipgloss.Width(statusH))),
		priH, strings.Repeat(" ", max(0, 4-lipgloss.Width(priH))),
		ageH, strings.Repeat(" ", max(0, 5-lipgloss.Width(ageH))),
		urgH, strings.Repeat(" ", max(0, 6-lipgloss.Width(urgH))),
		titleH,
	); err != nil {
		return err
	}
	for _, task := range tasks {
		title := task.Title
		if tags, ok := taskTags[task.ID.String()]; ok && len(tags) > 0 {
			tagStrs := make([]string, len(tags))
			for index, tag := range tags {
				tagStrs[index] = renderer.styledTag(tag)
			}
			title = title + "  " + strings.Join(tagStrs, " ")
		}
		priStr := renderer.styledPriority(task.Priority)
		priPad := strings.Repeat(" ", max(0, 4-lipgloss.Width(priStr)))
		line := fmt.Sprintf("%-8s %-9s %s%s %-5s %-6s %s",
			task.ShortID,
			task.Status,
			priStr,
			priPad,
			formatAge(task.CreatedAt),
			formatUrgency(task.Urgency),
			title,
		)
		if renderer.isDimStatus(task.Status) {
			line = renderer.styles.Dim.Render(line)
		}
		if _, err := fmt.Fprintln(renderer.w, line); err != nil {
			return err
		}
	}
	return nil
}

// formatPriorityName converts a priority int to a full name for the info view.
func formatPriorityName(priority int) string {
	switch priority {
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
func (renderer *Renderer) renderTaskInfo(task *domain.Task, annotations []*domain.Annotation, tags []*domain.Tag, relations []resolvedRelation) error {
	if renderer.format == "json" {
		info := taskInfoJSON{taskJSON: renderer.toTaskJSON(task, tags)}
		for _, annotation := range annotations {
			info.Annotations = append(info.Annotations, annotationJSON{
				ID:        annotation.ID.String(),
				TaskID:    annotation.TaskID.String(),
				Body:      annotation.Body,
				CreatedAt: annotation.CreatedAt.Format(time.RFC3339),
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
		enc := json.NewEncoder(renderer.w)
		enc.SetIndent("", "  ")
		return enc.Encode(info)
	}

	if _, err := fmt.Fprintf(renderer.w, "%s %s\n", renderer.paddedLabel("ID:", 13), task.ShortID); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(renderer.w, "%s %s\n", renderer.paddedLabel("Title:", 13), task.Title); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(renderer.w, "%s %s\n", renderer.paddedLabel("Status:", 13), task.Status); err != nil {
		return err
	}
	priName := formatPriorityName(task.Priority)
	if renderer.styles != nil {
		idx := task.Priority
		if idx < 0 || idx > 4 {
			idx = 0
		}
		priName = renderer.styles.Priority[idx].Render(priName)
	}
	if _, err := fmt.Fprintf(renderer.w, "%s %s\n", renderer.paddedLabel("Priority:", 13), priName); err != nil {
		return err
	}
	if task.Order != nil {
		if _, err := fmt.Fprintf(renderer.w, "%s %s\n", renderer.paddedLabel("Order:", 13), strconv.FormatFloat(*task.Order, 'f', -1, 64)); err != nil {
			return err
		}
	}
	if renderer.hasTaxonomy(task.ProjectID) {
		level := "—"
		if task.Level != nil && *task.Level != "" {
			level = *task.Level
		}
		if _, err := fmt.Fprintf(renderer.w, "%s %s\n", renderer.paddedLabel("Level:", 13), level); err != nil {
			return err
		}
	}

	if len(tags) > 0 {
		tagStrs := make([]string, len(tags))
		for index, tag := range tags {
			tagStrs[index] = renderer.styledTag(tag)
		}
		if _, err := fmt.Fprintf(renderer.w, "%s %s\n", renderer.paddedLabel("Tags:", 13), strings.Join(tagStrs, " ")); err != nil {
			return err
		}
	}

	if task.Description != "" {
		if _, err := fmt.Fprintln(renderer.w, renderer.paddedLabel("Description:", 13)); err != nil {
			return err
		}

		if _, err := fmt.Fprintln(renderer.w); err != nil {
			return err
		}

		rendered, markdownErr := renderer.renderMarkdown(task.Description)

		if markdownErr != nil {
			rendered = task.Description
		}

		if _, err := fmt.Fprintln(renderer.w, rendered); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(renderer.w, "%s %s\n", renderer.paddedLabel("Project:", 13), renderer.projectName(task.ProjectID)); err != nil {
		return err
	}
	if task.ParentID != nil {
		if _, err := fmt.Fprintf(renderer.w, "%s %s\n", renderer.paddedLabel("Parent:", 13), task.ParentID.String()); err != nil {
			return err
		}
	}
	if task.DueAt != nil {
		if _, err := fmt.Fprintf(renderer.w, "%s %s\n", renderer.paddedLabel("Due:", 13), task.DueAt.Format("2006-01-02")); err != nil {
			return err
		}
	}
	if task.WaitUntil != nil {
		if _, err := fmt.Fprintf(renderer.w, "%s %s\n", renderer.paddedLabel("Wait:", 13), task.WaitUntil.Format("2006-01-02 15:04:05")); err != nil {
			return err
		}
	}
	if task.RecurrenceRule != nil {
		if _, err := fmt.Fprintf(renderer.w, "%s %s\n", renderer.paddedLabel("Recurrence:", 13), *task.RecurrenceRule); err != nil {
			return err
		}
	}
	if task.ClaimedBy != nil {
		if _, err := fmt.Fprintf(renderer.w, "%s %s\n", renderer.paddedLabel("Claimed By:", 13), *task.ClaimedBy); err != nil {
			return err
		}
	}
	if task.ClaimedAt != nil {
		if _, err := fmt.Fprintf(renderer.w, "%s %s\n", renderer.paddedLabel("Claimed At:", 13), task.ClaimedAt.Format("2006-01-02 15:04:05")); err != nil {
			return err
		}
	}
	if len(task.UDA) > 0 {
		if _, err := fmt.Fprintln(renderer.w, renderer.styledLabel("UDA:")); err != nil {
			return err
		}
		if err := renderer.renderUDASection(task.UDA); err != nil {
			return err
		}
	}

	if _, err := fmt.Fprintf(renderer.w, "%s %s\n", renderer.paddedLabel("Created:", 13), task.CreatedAt.Format("2006-01-02 15:04:05")); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(renderer.w, "%s %s\n", renderer.paddedLabel("Modified:", 13), task.ModifiedAt.Format("2006-01-02 15:04:05")); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(renderer.w, "%s %d\n", renderer.paddedLabel("Version:", 13), task.Version); err != nil {
		return err
	}

	if task.UrgencyOverrides != nil {
		if _, err := fmt.Fprintln(renderer.w); err != nil {
			return err
		}
		if _, err := fmt.Fprintln(renderer.w, renderer.styledLabel("Urgency Overrides:")); err != nil {
			return err
		}
		if err := renderer.renderSparseUrgencyOverrides(task.UrgencyOverrides); err != nil {
			return err
		}
	}
	if task.EffectiveWeights != nil {
		if _, err := fmt.Fprintln(renderer.w); err != nil {
			return err
		}
		if _, err := fmt.Fprintln(renderer.w, renderer.styledLabel("Effective Urgency Weights:")); err != nil {
			return err
		}
		if err := renderer.renderResolvedUrgencyWeights(*task.EffectiveWeights); err != nil {
			return err
		}
	}

	if len(annotations) > 0 {
		if _, err := fmt.Fprintln(renderer.w); err != nil {
			return err
		}
		if _, err := fmt.Fprintln(renderer.w, renderer.styledLabel("Annotations:")); err != nil {
			return err
		}
		for _, annotation := range annotations {
			if _, err := fmt.Fprintf(renderer.w, "  %s - %s\n", annotation.CreatedAt.Format("2006-01-02 15:04"), annotation.Body); err != nil {
				return err
			}
		}
	}

	if len(relations) > 0 {
		if _, err := fmt.Fprintln(renderer.w); err != nil {
			return err
		}
		if _, err := fmt.Fprintln(renderer.w, renderer.styledLabel("Relations:")); err != nil {
			return err
		}
		for _, rr := range relations {
			if _, err := fmt.Fprintf(renderer.w, "  %-14s %-8s  %s\n", rr.Label, rr.RelatedShortID, rr.RelatedTitle); err != nil {
				return err
			}
		}
	}

	return nil
}

// renderSparseUrgencyOverrides writes the non-nil keys of a per-task urgency
// override block in the canonical key order. Skips nil pointers.
func (renderer *Renderer) renderSparseUrgencyOverrides(overrides *domain.UrgencyOverrides) error {
	for _, key := range domain.ValidUrgencyWeightKeys {
		fieldPtr := domain.UrgencyOverrideFieldPtr(overrides, key)
		if fieldPtr == nil || *fieldPtr == nil {
			continue
		}
		value := strconv.FormatFloat(**fieldPtr, 'f', -1, 64)
		if _, err := fmt.Fprintf(renderer.w, "  %-18s %s\n", key, value); err != nil {
			return err
		}
	}
	return nil
}

// renderResolvedUrgencyWeights writes all 10 keys of a fully-resolved weight
// table in the canonical key order.
func (renderer *Renderer) renderResolvedUrgencyWeights(weights domain.ResolvedUrgencyWeights) error {
	for _, key := range domain.ValidUrgencyWeightKeys {
		value := strconv.FormatFloat(resolvedWeightByKey(weights, key), 'f', -1, 64)
		if _, err := fmt.Fprintf(renderer.w, "  %-18s %s\n", key, value); err != nil {
			return err
		}
	}
	return nil
}

func resolvedWeightByKey(weights domain.ResolvedUrgencyWeights, key string) float64 {
	switch key {
	case "priority_weight":
		return weights.PriorityWeight
	case "due_weight":
		return weights.DueWeight
	case "age_weight":
		return weights.AgeWeight
	case "active_weight":
		return weights.ActiveWeight
	case "blocking_weight":
		return weights.BlockingWeight
	case "blocked_weight":
		return weights.BlockedWeight
	case "tags_weight":
		return weights.TagsWeight
	case "project_weight":
		return weights.ProjectWeight
	case "annotations_weight":
		return weights.AnnotationsWeight
	case "waiting_weight":
		return weights.WaitingWeight
	}
	return 0
}

// renderUDASection writes UDA key-value pairs as an indented block.
// Keys are sorted alphabetically. Single-line values appear inline;
// multi-line values appear indented below the key.
func (renderer *Renderer) renderUDASection(uda map[string]any) error {
	keys := make([]string, 0, len(uda))
	for key := range uda {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	// Calculate max key length for alignment
	maxKeyLen := 0
	for _, key := range keys {
		if len(key) > maxKeyLen {
			maxKeyLen = len(key)
		}
	}

	for _, key := range keys {
		value := fmt.Sprintf("%v", uda[key])
		if strings.Contains(value, "\n") {
			// Multi-line: key on its own line, value indented below
			if _, err := fmt.Fprintf(renderer.w, "  %s:\n", key); err != nil {
				return err
			}
			for _, line := range strings.Split(value, "\n") {
				if _, err := fmt.Fprintf(renderer.w, "    %s\n", line); err != nil {
					return err
				}
			}
		} else {
			// Single-line: inline after key with aligned padding
			if _, err := fmt.Fprintf(renderer.w, "  %-*s  %s\n", maxKeyLen+1, key+":", value); err != nil {
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
func (renderer *Renderer) renderLevelCheck(violations []service.LevelViolation) error {
	if renderer.format == "json" {
		items := make([]levelCheckJSON, len(violations))
		for index, violation := range violations {
			ranks := [][]string(violation.Taxonomy)
			if ranks == nil {
				ranks = [][]string{}
			}
			items[index] = levelCheckJSON{
				Task:        renderer.toTaskJSON(violation.Task, nil),
				Reason:      violation.Err.Reason,
				ParentLevel: violation.Err.ParentLevel,
				Taxonomy:    taxonomyRanks{Ranks: ranks},
				Source:      taxonomySourceLabel(violation.Source),
			}
		}
		enc := json.NewEncoder(renderer.w)
		enc.SetIndent("", "  ")
		return enc.Encode(items)
	}

	for _, violation := range violations {
		level := "—"
		if violation.Task.Level != nil && *violation.Task.Level != "" {
			level = *violation.Task.Level
		}
		reason := violation.Err.Reason
		if renderer.styles != nil {
			reason = renderer.styles.Priority[4].Render(reason)
		}
		projectName := renderer.projectName(violation.Task.ProjectID)
		parent := "—"
		if violation.Err.ParentLevel != "" {
			parent = violation.Err.ParentLevel
		}
		if _, err := fmt.Fprintf(renderer.w, "%-8s  %-20s  %-12s  %-22s  (parent: %s)\n",
			violation.Task.ShortID, projectName, level, reason, parent,
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
func (renderer *Renderer) renderMutationResult(action string, task *domain.Task, tags []*domain.Tag) error {
	if renderer.format == "json" {
		enc := json.NewEncoder(renderer.w)
		enc.SetIndent("", "  ")
		return enc.Encode(renderer.toTaskJSON(task, tags))
	}
	_, err := fmt.Fprintf(renderer.w, "%s task %s\n", action, task.ShortID)
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
func (renderer *Renderer) renderUnlinkResult(sourceShortID, relType, targetShortID string) error {
	if renderer.format == "json" {
		_, err := fmt.Fprintln(renderer.w, "{}")
		return err
	}
	_, err := fmt.Fprintf(renderer.w, "Unlinked %s %s %s\n", sourceShortID, relType, targetShortID)
	return err
}

// renderLinkResult writes a link confirmation (text) or full relation JSON.
func (renderer *Renderer) renderLinkResult(rel *domain.Relation, sourceShortID, targetShortID string) error {
	if renderer.format == "json" {
		enc := json.NewEncoder(renderer.w)
		enc.SetIndent("", "  ")
		return enc.Encode(relationJSON{
			ID:           rel.ID.String(),
			SourceID:     rel.SourceID.String(),
			TargetID:     rel.TargetID.String(),
			RelationType: rel.RelationType,
			CreatedAt:    rel.CreatedAt.Format(time.RFC3339),
		})
	}
	_, err := fmt.Fprintf(renderer.w, "Linked %s %s %s\n", sourceShortID, rel.RelationType, targetShortID)
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

func toWorkflowJSON(workflow *domain.Workflow) workflowJSON {
	transitions := make([]workflowTransJSON, len(workflow.Transitions))
	for index, trans := range workflow.Transitions {
		transitions[index] = workflowTransJSON{From: trans.FromStatus, To: trans.ToStatus}
	}
	names := workflow.StatusNames()
	statuses := make([]statusJSON, len(names))
	for index, name := range names {
		statusConfig := workflow.Statuses[name]
		roles := make([]string, len(statusConfig.Roles))
		for subindex, role := range statusConfig.Roles {
			roles[subindex] = string(role)
		}
		statuses[index] = statusJSON{Name: name, Roles: roles}
	}
	return workflowJSON{
		Name:        workflow.Name,
		Statuses:    statuses,
		Transitions: transitions,
	}
}

// renderWorkflowList writes a list of workflows to w.
// workflowProjects maps workflow name to referencing project IDs.
func (renderer *Renderer) renderWorkflowList(workflows []*domain.Workflow, workflowProjects map[string][]string) error {
	if renderer.format == "json" {
		items := make([]workflowInfoJSON, len(workflows))
		for index, workflow := range workflows {
			projectIDs := workflowProjects[workflow.Name]
			if projectIDs == nil {
				projectIDs = []string{}
			}
			items[index] = workflowInfoJSON{
				workflowJSON: toWorkflowJSON(workflow),
				Projects:     projectIDs,
			}
		}
		enc := json.NewEncoder(renderer.w)
		enc.SetIndent("", "  ")
		return enc.Encode(items)
	}

	if len(workflows) == 0 {
		return nil
	}

	nameH := renderer.styledHeader("NAME")
	statusesH := renderer.styledHeader("STATUSES")
	if _, err := fmt.Fprintf(renderer.w, "%s%s %s\n",
		nameH, strings.Repeat(" ", max(0, 20-lipgloss.Width(nameH))),
		statusesH,
	); err != nil {
		return err
	}
	for _, workflow := range workflows {
		if _, err := fmt.Fprintf(renderer.w, "%-20s %s\n", workflow.Name, strings.Join(workflow.StatusNames(), ", ")); err != nil {
			return err
		}
	}
	return nil
}

// renderWorkflowInfo writes a detailed workflow view to w.
func (renderer *Renderer) renderWorkflowInfo(workflow *domain.Workflow, projectIDs []string) error {
	if renderer.format == "json" {
		if projectIDs == nil {
			projectIDs = []string{}
		}
		info := workflowInfoJSON{
			workflowJSON: toWorkflowJSON(workflow),
			Projects:     projectIDs,
		}
		enc := json.NewEncoder(renderer.w)
		enc.SetIndent("", "  ")
		return enc.Encode(info)
	}

	if _, err := fmt.Fprintf(renderer.w, "%s %s\n", renderer.paddedLabel("Workflow:", 13), workflow.Name); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(renderer.w, "%s %s\n", renderer.paddedLabel("Statuses:", 13), strings.Join(workflow.StatusNames(), ", ")); err != nil {
		return err
	}
	// Show roles per status
	for _, name := range workflow.StatusNames() {
		statusConfig := workflow.Statuses[name]
		if len(statusConfig.Roles) > 0 {
			roles := make([]string, len(statusConfig.Roles))
			for index, role := range statusConfig.Roles {
				roles[index] = string(role)
			}
			if _, err := fmt.Fprintf(renderer.w, "  %-15s %s\n", name+":", strings.Join(roles, ", ")); err != nil {
				return err
			}
		}
	}

	if len(workflow.Transitions) > 0 {
		if _, err := fmt.Fprintln(renderer.w, renderer.styledLabel("Transitions:")); err != nil {
			return err
		}
		maxLen := 0
		for _, trans := range workflow.Transitions {
			if len(trans.FromStatus) > maxLen {
				maxLen = len(trans.FromStatus)
			}
		}
		fmtStr := fmt.Sprintf("  %%-%ds -> %%s\n", maxLen)
		for _, trans := range workflow.Transitions {
			if _, err := fmt.Fprintf(renderer.w, fmtStr, trans.FromStatus, trans.ToStatus); err != nil {
				return err
			}
		}
	}

	if len(projectIDs) > 0 {
		if _, err := fmt.Fprintf(renderer.w, "%s %s\n", renderer.paddedLabel("Projects:", 13), strings.Join(projectIDs, ", ")); err != nil {
			return err
		}
	}

	return nil
}

// renderWorkflowMutation writes a workflow mutation confirmation.
func (renderer *Renderer) renderWorkflowMutation(action string, name string) error {
	if renderer.format == "json" {
		enc := json.NewEncoder(renderer.w)
		enc.SetIndent("", "  ")
		return enc.Encode(map[string]string{
			"action":   strings.ToLower(action),
			"workflow": name,
		})
	}
	_, err := fmt.Fprintf(renderer.w, "%s workflow %s\n", action, name)
	return err
}

// renderProjectMutation writes a project mutation confirmation.
func (renderer *Renderer) renderProjectMutation(action string, name string) error {
	if renderer.format == "json" {
		enc := json.NewEncoder(renderer.w)
		enc.SetIndent("", "  ")
		return enc.Encode(map[string]string{
			"action":  strings.ToLower(action),
			"project": name,
		})
	}
	_, err := fmt.Fprintf(renderer.w, "%s project %s\n", action, name)
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

func toPlayerJSON(player *domain.Player) playerJSON {
	return playerJSON{
		ID:             player.ID,
		Type:           player.Type,
		NoteWindowSize: player.NoteWindowSize,
		RegisteredAt:   player.RegisteredAt.Format(time.RFC3339),
		LastSeenAt:     player.LastSeenAt.Format(time.RFC3339),
	}
}

// renderPlayerResult writes a player mutation result.
func (renderer *Renderer) renderPlayerResult(action string, player *domain.Player) error {
	if renderer.format == "json" {
		enc := json.NewEncoder(renderer.w)
		enc.SetIndent("", "  ")
		return enc.Encode(toPlayerJSON(player))
	}
	_, err := fmt.Fprintf(renderer.w, "%s player %s (type: %s)\n", action, player.ID, player.Type)

	if err != nil {
		return err
	}

	if player.NoteWindowSize != nil {
		_, err = fmt.Fprintf(renderer.w, "  note_window_size: %d\n", *player.NoteWindowSize)
	}
	return err
}
