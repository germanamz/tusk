package tui

import (
	"encoding/json"
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

	// Tags not yet supported
	if len(parsed.Tags) > 0 || len(parsed.ExclTags) > 0 {
		return fmt.Errorf("tags not yet supported")
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

	return renderTaskList(cmd.OutOrStdout(), tasks, a.format)
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

	return renderTaskInfo(cmd.OutOrStdout(), task, annotations, a.format)
}

func (a *App) runModify(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	shortID := args[0]

	// Tags not yet supported
	parsed := parseArgs(args[1:])
	if len(parsed.Tags) > 0 || len(parsed.ExclTags) > 0 {
		return fmt.Errorf("tags not yet supported")
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

	return renderMutationResult(cmd.OutOrStdout(), "Modified", updated, a.format)
}

func (a *App) runStart(cmd *cobra.Command, args []string) error {
	return fmt.Errorf("not implemented")
}

func (a *App) runDone(cmd *cobra.Command, args []string) error {
	return fmt.Errorf("not implemented")
}

func (a *App) runDelete(cmd *cobra.Command, args []string) error {
	return fmt.Errorf("not implemented")
}

func (a *App) runAnnotate(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	shortID := args[0]
	body := strings.Join(args[1:], " ")

	ann, err := a.taskSvc.Annotate(ctx, shortID, body)
	if err != nil {
		return fmt.Errorf("%s", formatError(err, shortID))
	}

	if a.format == "json" {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(map[string]string{
			"id":         ann.ID.String(),
			"task_id":    ann.TaskID.String(),
			"body":       ann.Body,
			"created_at": ann.CreatedAt.Format(time.RFC3339),
		})
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Annotated task %s\n", shortID)
	return nil
}
