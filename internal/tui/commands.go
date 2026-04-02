package tui

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/germanamz/tusk/internal/domain"
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
	parsed := parseArgs(args)

	if parsed.Title == "" {
		return fmt.Errorf("title is required")
	}

	task := &domain.Task{
		Title: parsed.Title,
	}

	// Project
	if name, ok := parsed.Fields["project"]; ok {
		project, err := a.projectRepo.GetByName(ctx, name)
		if err != nil {
			return fmt.Errorf("project %q not found", name)
		}
		task.ProjectID = &project.ID
	}

	// Priority
	if s, ok := parsed.Fields["priority"]; ok {
		p, err := parsePriority(s)
		if err != nil {
			return err
		}
		task.Priority = p
	}

	// Status (rarely used, defaults to pending in service)
	if s, ok := parsed.Fields["status"]; ok {
		task.Status = s
	}

	// Due date
	if s, ok := parsed.Fields["due"]; ok {
		d, err := parseDate(s)
		if err != nil {
			return err
		}
		task.DueAt = &d
	}

	// Parent
	if shortID, ok := parsed.Fields["parent"]; ok {
		parent, err := a.taskSvc.GetByShortID(ctx, shortID)
		if err != nil {
			return fmt.Errorf("%s", formatError(err, shortID))
		}
		task.ParentID = &parent.ID
	}

	if err := a.taskSvc.Create(ctx, task); err != nil {
		return fmt.Errorf("%s", err)
	}

	// Assign tags if any were specified
	if len(parsed.Tags) > 0 {
		if err := a.tagSvc.AssignToTask(ctx, task.ID, parsed.Tags); err != nil {
			return fmt.Errorf("assigning tags: %w", err)
		}
	}

	return renderMutationResult(cmd.OutOrStdout(), "Created", task, a.format)
}

func (a *App) runList(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	parsed := parseArgs(args)

	filter, err := buildTaskFilter(ctx, parsed, a.projectRepo)
	if err != nil {
		return err
	}

	// Handle parent filter if present
	if shortID, ok := parsed.Fields["parent"]; ok {
		parent, err := a.taskSvc.GetByShortID(ctx, shortID)
		if err != nil {
			return fmt.Errorf("%s", formatError(err, shortID))
		}
		filter.ParentID = &parent.ID
	}

	tasks, err := a.taskSvc.List(ctx, filter)
	if err != nil {
		return err
	}

	// Fetch tags for each task
	taskTags := make(map[string][]*domain.Tag, len(tasks))
	for _, t := range tasks {
		tags, err := a.tagSvc.GetTaskTags(ctx, t.ID)
		if err != nil {
			return fmt.Errorf("loading tags for task %s: %w", t.ShortID, err)
		}
		if len(tags) > 0 {
			taskTags[t.ID.String()] = tags
		}
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

	parsed := parseArgs(args[1:])

	// Auto-fetch current version
	current, err := a.taskSvc.GetByShortID(ctx, shortID)
	if err != nil {
		return fmt.Errorf("%s", formatError(err, shortID))
	}

	upd := domain.TaskUpdate{
		ShortID: shortID,
		Version: current.Version,
	}

	// Title
	if s, ok := parsed.Fields["title"]; ok {
		upd.Title = &s
	}

	// Priority
	if s, ok := parsed.Fields["priority"]; ok {
		p, err := parsePriority(s)
		if err != nil {
			return err
		}
		upd.Priority = &p
	}

	// Status
	if s, ok := parsed.Fields["status"]; ok {
		upd.Status = &s
	}

	// Due date (double pointer: outer nil = don't change, outer non-nil + inner nil = clear)
	if s, ok := parsed.Fields["due"]; ok {
		if s == "" {
			var nilTime *time.Time
			upd.DueAt = &nilTime
		} else {
			d, err := parseDate(s)
			if err != nil {
				return err
			}
			dp := &d
			upd.DueAt = &dp
		}
	}

	// Project
	if name, ok := parsed.Fields["project"]; ok {
		project, err := a.projectRepo.GetByName(ctx, name)
		if err != nil {
			return fmt.Errorf("project %q not found", name)
		}
		pid := project.ID
		pp := &pid
		upd.ProjectID = &pp
	}

	// Parent (double pointer: empty string = clear parent)
	if s, ok := parsed.Fields["parent"]; ok {
		if s == "" {
			var nilUUID *uuid.UUID
			upd.ParentID = &nilUUID
		} else {
			parent, err := a.taskSvc.GetByShortID(ctx, s)
			if err != nil {
				return fmt.Errorf("%s", formatError(err, s))
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
	if len(parsed.Tags) > 0 {
		if err := a.tagSvc.AssignToTask(ctx, updated.ID, parsed.Tags); err != nil {
			return fmt.Errorf("assigning tags: %w", err)
		}
	}

	// Remove excluded tags
	if len(parsed.ExclTags) > 0 {
		if err := a.tagSvc.RemoveFromTask(ctx, updated.ID, parsed.ExclTags); err != nil {
			return fmt.Errorf("removing tags: %w", err)
		}
	}

	return renderMutationResult(cmd.OutOrStdout(), "Modified", updated, a.format)
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

	return renderMutationResult(cmd.OutOrStdout(), "Started", updated, a.format)
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

	return renderMutationResult(cmd.OutOrStdout(), "Completed", updated, a.format)
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

	return renderMutationResult(cmd.OutOrStdout(), "Deleted", updated, a.format)
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

	return renderMutationResult(cmd.OutOrStdout(), "Annotated", task, a.format)
}
