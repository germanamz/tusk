package tui

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/germanamz/tusk/internal/domain"
	"github.com/germanamz/tusk/internal/filter"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

// formatError translates domain errors into user-friendly messages.
func formatError(err error, shortID string) string {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return fmt.Sprintf("Task not found: %s", shortID)
	case errors.Is(err, domain.ErrConflict):
		return "Version conflict - task was modified by another process"
	case errors.Is(err, domain.ErrInvalidTransition):
		return err.Error()
	default:
		return err.Error()
	}
}

func (a *App) runAdd(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	input := strings.Join(args, " ")
	fs, parseErrs := filter.Parse(input)
	if len(parseErrs) > 0 {
		return fmt.Errorf("%s", filter.FormatErrors(parseErrs))
	}

	title := fs.Title()
	if title == "" {
		return fmt.Errorf("title is required")
	}

	task := &domain.Task{
		Title: title,
	}

	// Project
	if f, ok := fs.GetField("project"); ok {
		project, err := a.projectRepo.GetByName(ctx, f.Value)
		if err != nil {
			return fmt.Errorf("project %q not found", f.Value)
		}
		task.ProjectID = &project.ID
	}

	// Priority
	if f, ok := fs.GetField("priority"); ok {
		p, err := filter.ParsePriorityValue(f.Value)
		if err != nil {
			return err
		}
		task.Priority = p
	}

	// Status (rarely used, defaults to pending in service)
	if f, ok := fs.GetField("status"); ok {
		task.Status = f.Value
	}

	// Due date
	if f, ok := fs.GetField("due"); ok {
		d, err := filter.ParseDateValue(f.Value)
		if err != nil {
			return err
		}
		task.DueAt = &d
	}

	// Parent
	if f, ok := fs.GetField("parent"); ok {
		parent, err := a.taskSvc.GetByShortID(ctx, f.Value)
		if err != nil {
			return fmt.Errorf("%s", formatError(err, f.Value))
		}
		task.ParentID = &parent.ID
	}

	if err := a.taskSvc.Create(ctx, task); err != nil {
		return fmt.Errorf("%s", err)
	}

	// Assign tags if any were specified
	incTags := fs.IncludeTags()
	if len(incTags) > 0 {
		if err := a.tagSvc.AssignToTask(ctx, task.ID, incTags); err != nil {
			return fmt.Errorf("assigning tags: %w", err)
		}
	}

	// Fetch tags for output
	tags, err := a.tagSvc.GetTaskTags(ctx, task.ID)
	if err != nil {
		return fmt.Errorf("loading tags: %w", err)
	}

	return renderMutationResult(cmd.OutOrStdout(), "Created", task, tags, a.format)
}

func (a *App) runList(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	input := strings.Join(args, " ")
	fs, parseErrs := filter.Parse(input)
	if len(parseErrs) > 0 {
		return fmt.Errorf("%s", filter.FormatErrors(parseErrs))
	}

	tf, resolveErrs := a.resolver.Resolve(ctx, fs)
	if len(resolveErrs) > 0 {
		msgs := make([]string, len(resolveErrs))
		for i, e := range resolveErrs {
			msgs[i] = e.Error()
		}
		return fmt.Errorf("%s", strings.Join(msgs, "\n"))
	}

	tasks, err := a.taskSvc.List(ctx, *tf)
	if err != nil {
		return err
	}

	// Fetch tags for all tasks in one query
	taskIDs := make([]uuid.UUID, len(tasks))
	for i, t := range tasks {
		taskIDs[i] = t.ID
	}
	tagsByTaskID, err := a.tagSvc.GetTaskTagsBatch(ctx, taskIDs)
	if err != nil {
		return fmt.Errorf("loading tags: %w", err)
	}

	// Convert uuid.UUID keys to string keys for the render layer
	taskTags := make(map[string][]*domain.Tag, len(tagsByTaskID))
	for id, tags := range tagsByTaskID {
		taskTags[id.String()] = tags
	}

	return renderTaskList(cmd.OutOrStdout(), tasks, taskTags, a.format)
}

func (a *App) runInfo(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	shortID := args[0]

	task, err := a.taskSvc.GetByShortID(ctx, shortID)
	if err != nil {
		return fmt.Errorf("%s", formatError(err, shortID))
	}

	annotations, err := a.taskSvc.GetAnnotations(ctx, shortID)
	if err != nil {
		return fmt.Errorf("loading annotations: %w", err)
	}

	// Resolve project name for display
	var projectName string
	if task.ProjectID != nil {
		project, err := a.projectRepo.GetByID(ctx, *task.ProjectID)
		if err == nil {
			projectName = project.Name
		}
	}

	// Fetch tags
	tags, err := a.tagSvc.GetTaskTags(ctx, task.ID)
	if err != nil {
		return fmt.Errorf("loading tags: %w", err)
	}

	return renderTaskInfo(cmd.OutOrStdout(), task, annotations, tags, projectName, a.format)
}

func (a *App) runModify(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	shortID := args[0]

	input := strings.Join(args[1:], " ")
	fs, parseErrs := filter.Parse(input)
	if len(parseErrs) > 0 {
		return fmt.Errorf("%s", filter.FormatErrors(parseErrs))
	}

	// Auto-fetch current version
	current, err := a.taskSvc.GetByShortID(ctx, shortID)
	if err != nil {
		return fmt.Errorf("%s", formatError(err, shortID))
	}

	upd := domain.TaskUpdate{
		ShortID: shortID,
		Version: current.Version,
	}

	// Title from free text (if any)
	if title := fs.Title(); title != "" {
		upd.Title = &title
	}

	// Priority
	if f, ok := fs.GetField("priority"); ok {
		p, err := filter.ParsePriorityValue(f.Value)
		if err != nil {
			return err
		}
		upd.Priority = &p
	}

	// Status
	if f, ok := fs.GetField("status"); ok {
		v := f.Value
		upd.Status = &v
	}

	// Due date (double pointer: outer nil = don't change, outer non-nil + inner nil = clear)
	if f, ok := fs.GetField("due"); ok {
		if f.Value == "" {
			var nilTime *time.Time
			upd.DueAt = &nilTime
		} else {
			d, err := filter.ParseDateValue(f.Value)
			if err != nil {
				return err
			}
			dp := &d
			upd.DueAt = &dp
		}
	}

	// Project
	if f, ok := fs.GetField("project"); ok {
		project, err := a.projectRepo.GetByName(ctx, f.Value)
		if err != nil {
			return fmt.Errorf("project %q not found", f.Value)
		}
		pid := project.ID
		pp := &pid
		upd.ProjectID = &pp
	}

	// Parent (double pointer: empty string = clear parent)
	if f, ok := fs.GetField("parent"); ok {
		if f.Value == "" {
			var nilUUID *uuid.UUID
			upd.ParentID = &nilUUID
		} else {
			parent, err := a.taskSvc.GetByShortID(ctx, f.Value)
			if err != nil {
				return fmt.Errorf("%s", formatError(err, f.Value))
			}
			pid := parent.ID
			pp := &pid
			upd.ParentID = &pp
		}
	}

	updated, err := a.taskSvc.Update(ctx, upd)
	if err != nil {
		return fmt.Errorf("%s", formatError(err, shortID))
	}

	// Add new tags
	incTags := fs.IncludeTags()
	if len(incTags) > 0 {
		if err := a.tagSvc.AssignToTask(ctx, updated.ID, incTags); err != nil {
			return fmt.Errorf("assigning tags: %w", err)
		}
	}

	// Remove excluded tags
	excTags := fs.ExcludeTags()
	if len(excTags) > 0 {
		if err := a.tagSvc.RemoveFromTask(ctx, updated.ID, excTags); err != nil {
			return fmt.Errorf("removing tags: %w", err)
		}
	}

	// Fetch tags for output
	modTags, err := a.tagSvc.GetTaskTags(ctx, updated.ID)
	if err != nil {
		return fmt.Errorf("loading tags: %w", err)
	}

	return renderMutationResult(cmd.OutOrStdout(), "Modified", updated, modTags, a.format)
}

func (a *App) runStart(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	shortID := args[0]

	current, err := a.taskSvc.GetByShortID(ctx, shortID)
	if err != nil {
		return fmt.Errorf("%s", formatError(err, shortID))
	}

	updated, err := a.taskSvc.Start(ctx, shortID, current.Version)
	if err != nil {
		return fmt.Errorf("%s", formatError(err, shortID))
	}

	return renderMutationResult(cmd.OutOrStdout(), "Started", updated, nil, a.format)
}

func (a *App) runDone(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	shortID := args[0]

	current, err := a.taskSvc.GetByShortID(ctx, shortID)
	if err != nil {
		return fmt.Errorf("%s", formatError(err, shortID))
	}

	updated, err := a.taskSvc.Complete(ctx, shortID, current.Version)
	if err != nil {
		return fmt.Errorf("%s", formatError(err, shortID))
	}

	return renderMutationResult(cmd.OutOrStdout(), "Completed", updated, nil, a.format)
}

func (a *App) runDelete(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	shortID := args[0]

	current, err := a.taskSvc.GetByShortID(ctx, shortID)
	if err != nil {
		return fmt.Errorf("%s", formatError(err, shortID))
	}

	updated, err := a.taskSvc.Delete(ctx, shortID, current.Version)
	if err != nil {
		return fmt.Errorf("%s", formatError(err, shortID))
	}

	return renderMutationResult(cmd.OutOrStdout(), "Deleted", updated, nil, a.format)
}

func (a *App) runAnnotate(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	shortID := args[0]
	body := strings.Join(args[1:], " ")

	_, err := a.taskSvc.Annotate(ctx, shortID, body)
	if err != nil {
		return fmt.Errorf("%s", formatError(err, shortID))
	}

	task, err := a.taskSvc.GetByShortID(ctx, shortID)
	if err != nil {
		return fmt.Errorf("%s", formatError(err, shortID))
	}

	return renderMutationResult(cmd.OutOrStdout(), "Annotated", task, nil, a.format)
}
