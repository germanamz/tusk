package tui

import (
	"context"
	"fmt"
	"io"
	"strings"

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
func (a *App) gatherMarkdownInputs(ctx context.Context, tasks []*domain.Task) (*markdownInputs, error) {
	if len(tasks) == 0 {
		return nil, fmt.Errorf("gatherMarkdownInputs: tasks must not be empty")
	}

	projectID := tasks[0].ProjectID
	project, err := a.projectSvc.GetByID(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("loading project %v for markdown export: %w", projectID, err)
	}

	taskIDs := make([]uuid.UUID, len(tasks))
	for i, t := range tasks {
		taskIDs[i] = t.ID
	}

	tagsByTask, err := a.tagSvc.GetTaskTagsBatch(ctx, taskIDs)
	if err != nil {
		return nil, fmt.Errorf("loading tags for markdown export: %w", err)
	}

	annsByTask, err := a.taskSvc.GetAnnotationsBatch(ctx, project.ID, taskIDs)
	if err != nil {
		return nil, fmt.Errorf("loading annotations for markdown export: %w", err)
	}

	allNotes, err := a.noteSvc.ListAllForProject(ctx, project.ID)
	if err != nil {
		return nil, fmt.Errorf("loading notes for markdown export: %w", err)
	}
	notesByTask := make(map[uuid.UUID][]*domain.Note)
	for _, n := range allNotes {
		key := uuid.Nil
		if n.TaskID != nil {
			key = *n.TaskID
		}
		notesByTask[key] = append(notesByTask[key], n)
	}

	workflowFor, err := a.buildWorkflowLookup(ctx, tasks)
	if err != nil {
		return nil, err
	}

	return &markdownInputs{
		project:     project,
		tagsByTask:  tagsByTask,
		annsByTask:  annsByTask,
		notesByTask: notesByTask,
		workflowFor: workflowFor,
	}, nil
}

// renderTreeMarkdown writes the markdown export. Phase 3 emits only the
// project header + description blockquote and a placeholder comment for the
// body. Phase 4 fills in the title lines, structural shape, and description
// blocks for tasks; Phase 5 adds annotations and notes.
func (r *Renderer) renderTreeMarkdown(_ []*treeNode) error {
	if r.markdown == nil {
		// Empty workspace OR caller forgot to wire inputs — emit nothing rather
		// than panicking. runTree only sets inputs when the tree is non-empty
		// and single-project.
		return nil
	}
	proj := r.markdown.project
	if _, err := fmt.Fprintf(r.w, "# %s\n", projectDisplayName(proj.Name)); err != nil {
		return err
	}
	if proj.Description != "" {
		if err := writeBlockquote(r.w, proj.Description, ""); err != nil {
			return err
		}
	}
	// Phase 4 replaces this placeholder with full body rendering.
	_, err := fmt.Fprintln(r.w, "\n<!-- tusk: markdown body lands in phase 4 -->")
	return err
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
	fields := strings.FieldsFunc(name, func(r rune) bool {
		return r == '-' || r == '_'
	})
	for i, tok := range fields {
		fields[i] = strings.Title(tok) //nolint:staticcheck // ASCII-only per-token use; see comment above.
	}
	return strings.Join(fields, " ")
}

// writeBlockquote writes s as a markdown blockquote with each line prefixed
// by `> `. indent is the per-line indent (used by Phase 4 for nested bullets);
// pass "" for headings. Empty input lines (paragraph separators) are emitted
// as `<indent>>` (no trailing space). A blank line is appended after the
// blockquote so following content sits in its own paragraph.
func writeBlockquote(w io.Writer, s, indent string) error {
	for _, line := range strings.Split(s, "\n") {
		var out string
		if line == "" {
			out = indent + ">\n"
		} else {
			out = indent + "> " + line + "\n"
		}
		if _, err := fmt.Fprint(w, out); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}
	return nil
}
