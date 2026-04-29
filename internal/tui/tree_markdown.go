package tui

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/germanamz/tusk/domain"
	"github.com/google/uuid"
)

// markdownInputs bundles everything the markdown renderer needs beyond the
// raw treeNode tree. Constructed once per `tusk task tree --format markdown`
// invocation by gatherMarkdownInputs.
type markdownInputs struct {
	project     *domain.Project
	tagsByTask  map[uuid.UUID][]*domain.Tag
	annsByTask  map[uuid.UUID][]*domain.Annotation
	notesByTask map[uuid.UUID][]*domain.Note // keyed by TaskID; project-level notes go under uuid.Nil
	workflowFor func(*domain.Task) *domain.Workflow
}

// gatherMarkdownInputs assembles the markdown render inputs after the
// tree has been built. Must be called only when the tree is single-project
// (validated by the caller) and tasks is non-empty.
func (app *App) gatherMarkdownInputs(ctx context.Context, tasks []*domain.Task) (*markdownInputs, error) {
	if len(tasks) == 0 {
		return nil, fmt.Errorf("gatherMarkdownInputs: tasks must not be empty")
	}

	projectID := tasks[0].ProjectID
	project, projectErr := app.projectSvc.GetByID(ctx, projectID)

	if projectErr != nil {
		return nil, fmt.Errorf("loading project %v for markdown export: %w", projectID, projectErr)
	}

	taskIDs := make([]uuid.UUID, len(tasks))
	for index, task := range tasks {
		taskIDs[index] = task.ID
	}

	tagsByTask, tagsErr := app.tagSvc.GetTaskTagsBatch(ctx, taskIDs)

	if tagsErr != nil {
		return nil, fmt.Errorf("loading tags for markdown export: %w", tagsErr)
	}

	annsByTask, annotationsErr := app.taskSvc.GetAnnotationsBatch(ctx, project.ID, taskIDs)

	if annotationsErr != nil {
		return nil, fmt.Errorf("loading annotations for markdown export: %w", annotationsErr)
	}

	allNotes, notesErr := app.noteSvc.ListAllForProject(ctx, project.ID)

	if notesErr != nil {
		return nil, fmt.Errorf("loading notes for markdown export: %w", notesErr)
	}

	notesByTask := make(map[uuid.UUID][]*domain.Note)
	for _, note := range allNotes {
		key := uuid.Nil
		if note.TaskID != nil {
			key = *note.TaskID
		}
		notesByTask[key] = append(notesByTask[key], note)
	}

	workflowFor, workflowErr := app.buildWorkflowLookup(ctx, tasks)

	if workflowErr != nil {
		return nil, workflowErr
	}

	return &markdownInputs{
		project:     project,
		tagsByTask:  tagsByTask,
		annsByTask:  annsByTask,
		notesByTask: notesByTask,
		workflowFor: workflowFor,
	}, nil
}

// renderTreeMarkdown writes the markdown export: H1 + project description
// blockquote followed by the recursively-rendered task body. Phase 5 will
// append annotations and notes; Phase 4's output is a strict prefix of that.
func (renderer *Renderer) renderTreeMarkdown(nodes []*treeNode) error {
	if renderer.markdown == nil {
		// Empty workspace OR caller forgot to wire inputs — emit nothing rather
		// than panicking. runTree only sets inputs when the tree is non-empty
		// and single-project.
		return nil
	}
	proj := renderer.markdown.project
	if _, err := fmt.Fprintf(renderer.w, "# %s\n", projectDisplayName(proj.Name)); err != nil {
		return err
	}
	if proj.Description != "" {
		if err := writeBlockquote(renderer.w, proj.Description, ""); err != nil {
			return err
		}
	} else {
		// Trailing blank line between H1 and first task even when no description.
		if _, err := fmt.Fprintln(renderer.w); err != nil {
			return err
		}
	}

	// Project-level notes (TaskID == nil) live under uuid.Nil in the
	// notesByTask map per the gathering convention established in Phase 3.
	// They appear under the H1 description (if any) and before the first task.
	if err := renderNotesBlock(renderer.w, renderer.markdown.notesByTask[uuid.Nil], ""); err != nil {
		return err
	}

	for _, node := range nodes {
		if err := renderer.renderMarkdownNode(node, 0); err != nil {
			return err
		}
	}
	return nil
}

// renderMarkdownNode writes a single tree node and recurses into its children.
// depth 0 → "## ", depth 1 → "### ", depth ≥ 2 → bullet ("- [x] " or "- [ ] ")
// indented by 2 spaces per level past depth 2. Description (if any) is emitted
// as a blockquote immediately below the title with the same indent.
func (renderer *Renderer) renderMarkdownNode(node *treeNode, depth int) error {
	workflow := renderer.markdown.workflowFor(node.Task)
	titleLine := formatMarkdownTitleLine(
		node.Task,
		renderer.markdown.tagsByTask[node.Task.ID],
		renderer.hasTaxonomy(node.Task.ProjectID),
		workflow,
	)

	var indent string
	switch depth {
	case 0:
		if _, err := fmt.Fprintf(renderer.w, "## %s\n", titleLine); err != nil {
			return err
		}
	case 1:
		if _, err := fmt.Fprintf(renderer.w, "### %s\n", titleLine); err != nil {
			return err
		}
	default:
		indent = strings.Repeat("  ", depth-2)
		checkbox := " "
		if workflow != nil {
			if statusConfig, ok := workflow.Statuses[node.Task.Status]; ok && statusConfig.HasRole(domain.RoleDone) {
				checkbox = "x"
			}
		}
		if _, err := fmt.Fprintf(renderer.w, "%s- [%s] %s\n", indent, checkbox, titleLine); err != nil {
			return err
		}
	}

	if node.Task.Description != "" {
		if err := writeBlockquote(renderer.w, node.Task.Description, indent); err != nil {
			return err
		}
	}

	// Annotations and notes sit one indent level deeper than the bullet
	// itself (they render as sub-bullets of the task). Phase 4's description
	// blockquote uses the bullet's own indent (`strings.Repeat("  ", depth-2)`),
	// not the +2 sub-content indent used here — the two blocks don't line up
	// at the same column on bullet-rendered tasks. That's intentional: the
	// blockquote is a free-standing markdown construct rather than a list
	// item child.
	subIndent := ""
	if depth >= 2 {
		subIndent = strings.Repeat("  ", depth-2) + "  "
	}
	if err := renderAnnotationsBlock(renderer.w, renderer.markdown.annsByTask[node.Task.ID], subIndent); err != nil {
		return err
	}
	if err := renderNotesBlock(renderer.w, renderer.markdown.notesByTask[node.Task.ID], subIndent); err != nil {
		return err
	}

	for _, child := range node.Children {
		if err := renderer.renderMarkdownNode(child, depth+1); err != nil {
			return err
		}
	}
	return nil
}

// formatMarkdownTitleLine builds the inline-token suffix for a task's title.
// Output shape: "{title} status=… level=… priority=… due=… order=… uda.k=v … +tag …".
// Tokens are emitted only when meaningful per §3.2 of the design spec.
//
// hasTaxonomy controls whether `level=` is emitted (matches the renderer's
// existing taxonomy-resolver pattern).
//
// workflow may be nil (renderer fell back when no workflow was resolvable);
// when nil, status binary classification cannot be performed and the
// status= token is conservatively omitted.
func formatMarkdownTitleLine(task *domain.Task, tags []*domain.Tag, hasTaxonomy bool, workflow *domain.Workflow) string {
	var builder strings.Builder
	builder.WriteString(task.Title)

	// status=<name>: only when non-binary. Binary means: status equals the
	// workflow's initial status name OR carries the done role.
	if workflow != nil && task.Status != "" {
		binary := false
		if statusConfig, ok := workflow.Statuses[task.Status]; ok && statusConfig.HasRole(domain.RoleDone) {
			binary = true
		}
		if !binary {
			if name, ok := workflow.StatusByRole(domain.RoleInitial); ok && name == task.Status {
				binary = true
			}
		}
		if !binary {
			builder.WriteString(" status=")
			builder.WriteString(task.Status)
		}
	}

	if hasTaxonomy && task.Level != nil && *task.Level != "" {
		builder.WriteString(" level=")
		builder.WriteString(*task.Level)
	}

	if task.Priority > 0 {
		builder.WriteString(" priority=")
		builder.WriteString(strconv.Itoa(task.Priority))
	}

	if task.DueAt != nil {
		builder.WriteString(" due=")
		builder.WriteString(task.DueAt.UTC().Format("2006-01-02"))
	}

	if task.Order != nil {
		builder.WriteString(" order=")
		builder.WriteString(strconv.FormatFloat(*task.Order, 'g', 6, 64))
	}

	if len(task.UDA) > 0 {
		keys := make([]string, 0, len(task.UDA))
		for key := range task.UDA {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			builder.WriteString(" uda.")
			builder.WriteString(key)
			builder.WriteString("=")
			builder.WriteString(quoteUDAValue(fmt.Sprintf("%v", task.UDA[key])))
		}
	}

	if len(tags) > 0 {
		names := make([]string, 0, len(tags))
		for _, tag := range tags {
			names = append(names, tag.Name)
		}
		sort.Strings(names)
		for _, name := range names {
			builder.WriteString(" +")
			builder.WriteString(name)
		}
	}

	return builder.String()
}

// quoteUDAValue returns s wrapped in double quotes when it contains
// whitespace or one of the registered prefix characters from the inline
// syntax lexer ('+', '-', '@'). Empty strings are emitted as `""`.
// Internal double quotes are escaped with `\"` and backslashes with `\\`
// to keep the output reparsable by the inline-syntax lexer when the
// markdown is dogfood-read.
func quoteUDAValue(str string) string {
	needsQuote := str == ""
	if !needsQuote {
		switch str[0] {
		case '+', '-', '@':
			needsQuote = true
		}
	}
	if !needsQuote {
		for _, char := range str {
			if unicode.IsSpace(char) {
				needsQuote = true
				break
			}
		}
	}
	if !needsQuote {
		return str
	}
	var builder strings.Builder
	builder.Grow(len(str) + 2)
	builder.WriteByte('"')
	for _, char := range str {
		switch char {
		case '\\':
			builder.WriteString(`\\`)
		case '"':
			builder.WriteString(`\"`)
		default:
			builder.WriteRune(char)
		}
	}
	builder.WriteByte('"')
	return builder.String()
}

// projectDisplayName converts a kebab/snake-case project name into a
// title-cased display string ("tusk-roadmap" -> "Tusk Roadmap"). The name is
// split on '-' and '_'; empty tokens (from leading or trailing separators
// like "_default") are dropped, then each remaining token is title-cased
// using strings.Title. Per-token use of strings.Title is the documented
// codebase convention — strings.Title is deprecated for full-string use, but
// remains correct for single ASCII words and avoids pulling in
// golang.org/x/text/cases for what is otherwise a one-line conversion.
func projectDisplayName(name string) string {
	fields := strings.FieldsFunc(name, func(char rune) bool {
		return char == '-' || char == '_'
	})
	for i, tok := range fields {
		fields[i] = strings.Title(tok) //nolint:staticcheck // ASCII-only per-token use; see comment above.
	}
	return strings.Join(fields, " ")
}

// renderAnnotationsBlock writes the labeled `**Annotations:**` list for a
// task. indent is "" for heading-rendered tasks (depth 0/1) and the
// sub-content indent for bullet-rendered tasks (depth >= 2 — see
// renderMarkdownNode for how that indent is computed). Emits nothing when
// anns is empty.
//
// Heading variant ends with a trailing blank line so the next block sits in
// its own paragraph. Bullet variant emits no trailing blank — the next
// sibling sub-bullet or child handles its own spacing.
//
// Annotations are written in chronological order (created_at ascending);
// the slice is sorted defensively in case the upstream batch loader did not
// guarantee order.
func renderAnnotationsBlock(writer io.Writer, anns []*domain.Annotation, indent string) error {
	if len(anns) == 0 {
		return nil
	}
	sorted := make([]*domain.Annotation, len(anns))
	copy(sorted, anns)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].CreatedAt.Before(sorted[j].CreatedAt)
	})

	if indent == "" {
		if _, err := fmt.Fprint(writer, "**Annotations:**\n"); err != nil {
			return err
		}
		for _, annotation := range sorted {
			body := strings.TrimRightFunc(annotation.Body, unicode.IsSpace)
			if _, err := fmt.Fprintf(writer, "- %s: %s\n", annotation.CreatedAt.UTC().Format("2006-01-02"), body); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(writer); err != nil {
			return err
		}
		return nil
	}

	if _, err := fmt.Fprintf(writer, "%s- **Annotations:**\n", indent); err != nil {
		return err
	}
	for _, annotation := range sorted {
		body := strings.TrimRightFunc(annotation.Body, unicode.IsSpace)
		if _, err := fmt.Fprintf(writer, "%s  - %s: %s\n", indent, annotation.CreatedAt.UTC().Format("2006-01-02"), body); err != nil {
			return err
		}
	}
	return nil
}

// renderNotesBlock writes the labeled `**Notes:**` list for a task or for the
// project-level scope. indent semantics match renderAnnotationsBlock. Each
// note renders as `- {YYYY-MM-DD} ({player_id}[, {key}={val}…]): body`,
// sorted chronologically by created_at. Multi-line bodies put line one inline
// after the colon and indent subsequent lines under the bullet text by an
// additional 2 spaces (no leading bullet marker).
//
// Metadata pairs are alphabetical by key. Values are coerced via
// fmt.Sprintf("%v", v); strings containing whitespace or a registered prefix
// character are quoted via quoteUDAValue so the output remains reparsable by
// the inline-syntax lexer.
func renderNotesBlock(writer io.Writer, notes []*domain.Note, indent string) error {
	if len(notes) == 0 {
		return nil
	}
	sorted := make([]*domain.Note, len(notes))
	copy(sorted, notes)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].CreatedAt.Before(sorted[j].CreatedAt)
	})

	heading := indent == ""
	var bulletPrefix, contPrefix string
	if heading {
		bulletPrefix = "- "
		contPrefix = "  "
	} else {
		bulletPrefix = indent + "  - "
		contPrefix = indent + "    "
	}

	if heading {
		if _, err := fmt.Fprint(writer, "**Notes:**\n"); err != nil {
			return err
		}
	} else {
		if _, err := fmt.Fprintf(writer, "%s- **Notes:**\n", indent); err != nil {
			return err
		}
	}

	for _, note := range sorted {
		header := fmt.Sprintf("%s%s (%s%s)", bulletPrefix,
			note.CreatedAt.UTC().Format("2006-01-02"),
			note.PlayerID,
			formatNoteMetadata(note.Metadata),
		)
		body := strings.TrimRightFunc(note.Body, unicode.IsSpace)
		lines := strings.Split(body, "\n")
		if _, err := fmt.Fprintf(writer, "%s: %s\n", header, lines[0]); err != nil {
			return err
		}
		for _, line := range lines[1:] {
			if _, err := fmt.Fprintf(writer, "%s%s\n", contPrefix, line); err != nil {
				return err
			}
		}
	}
	if heading {
		if _, err := fmt.Fprintln(writer); err != nil {
			return err
		}
	}
	return nil
}

// formatNoteMetadata returns ", k=v, k2=v2" for non-empty metadata (with a
// leading ", ") or "" for empty. Keys are alphabetized; values are coerced
// via fmt.Sprintf and quoted with quoteUDAValue when ambiguous.
func formatNoteMetadata(meta map[string]any) string {
	if len(meta) == 0 {
		return ""
	}
	keys := make([]string, 0, len(meta))
	for key := range meta {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var builder strings.Builder
	for _, key := range keys {
		builder.WriteString(", ")
		builder.WriteString(key)
		builder.WriteByte('=')
		builder.WriteString(quoteUDAValue(fmt.Sprintf("%v", meta[key])))
	}
	return builder.String()
}

// writeBlockquote writes s as a markdown blockquote with each line prefixed
// by `> `. indent is the per-line indent (used by Phase 4 for nested bullets);
// pass "" for headings. Empty input lines (paragraph separators) are emitted
// as `<indent>>` (no trailing space). A blank line is appended after the
// blockquote so following content sits in its own paragraph.
func writeBlockquote(writer io.Writer, text, indent string) error {
	for _, line := range strings.Split(text, "\n") {
		var out string
		if line == "" {
			out = indent + ">\n"
		} else {
			out = indent + "> " + line + "\n"
		}
		if _, err := fmt.Fprint(writer, out); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(writer); err != nil {
		return err
	}
	return nil
}
