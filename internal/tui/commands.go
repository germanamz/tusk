package tui

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/germanamz/tusk/internal/domain"
	"github.com/germanamz/tusk/internal/filter"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

// buildTaskCmds creates the top-level task management commands.
func (a *App) buildTaskCmds() []*cobra.Command {
	addCmd := &cobra.Command{
		Use:   "add [title] [key:value...] [+tag...]",
		Short: "Create a new task",
		Args:  cobra.MinimumNArgs(1),
		RunE:  a.runAdd,
	}
	addCmd.Flags().StringP("description", "d", "", `task description (use @file to read from file, @- for stdin)`)
	addCmd.Flags().StringArrayP("uda", "u", nil, `user-defined attribute (repeatable, format: key=value)`)

	modifyCmd := &cobra.Command{
		Use:   "modify <short_id> [key:value...]",
		Short: "Modify a task",
		Args:  cobra.MinimumNArgs(1),
		RunE:  a.runModify,
	}
	modifyCmd.Flags().StringP("description", "d", "", `new description (use @file to read from file, @- for stdin, "" to clear)`)
	modifyCmd.Flags().StringArrayP("uda", "u", nil, `user-defined attribute (repeatable, format: key=value, key= to clear)`)

	treeCmd := &cobra.Command{
		Use:   "tree [short_id]",
		Short: "Display tasks as a tree hierarchy",
		Long:  "Show all tasks in a tree hierarchy. Optionally specify a short_id to show only that subtree.",
		Args:  cobra.MaximumNArgs(1),
		RunE:  a.runTree,
	}
	treeCmd.Flags().Bool("all", false, "include deleted tasks")

	return []*cobra.Command{
		addCmd,
		{
			Use:   "list [filters...]",
			Short: "List tasks",
			RunE:  a.runList,
		},
		{
			Use:   "info <short_id>",
			Short: "Show task details",
			Args:  cobra.ExactArgs(1),
			RunE:  a.runInfo,
		},
		modifyCmd,
		{
			Use:   "start <short_id>",
			Short: "Transition task to active",
			Args:  cobra.ExactArgs(1),
			RunE:  a.runStart,
		},
		{
			Use:   "done <short_id>",
			Short: "Transition task to completed",
			Args:  cobra.ExactArgs(1),
			RunE:  a.runDone,
		},
		{
			Use:   "delete <short_id>",
			Short: "Transition task to deleted",
			Args:  cobra.ExactArgs(1),
			RunE:  a.runDelete,
		},
		{
			Use:   "annotate <short_id> <message...>",
			Short: "Add a note to a task",
			Args:  cobra.MinimumNArgs(2),
			RunE:  a.runAnnotate,
		},
		treeCmd,
		{
			Use:   "link <short_id> <relation_type> <short_id>",
			Short: "Create a relation between two tasks",
			Long:  `Create a typed relation. Types: blocks, relates_to, duplicates.`,
			Args:  cobra.ExactArgs(3),
			RunE:  a.runLink,
		},
		{
			Use:   "unlink <short_id> <relation_type> <short_id>",
			Short: "Remove a relation between two tasks",
			Long:  `Remove a typed relation. Types: blocks, relates_to, duplicates.`,
			Args:  cobra.ExactArgs(3),
			RunE:  a.runUnlink,
		},
		{
			Use:   "next",
			Short: "Show the highest-urgency actionable task",
			Args:  cobra.NoArgs,
			RunE:  a.runNext,
		},
	}
}

// formatError translates domain errors into user-friendly messages.
func formatError(err error, shortID string) string {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return fmt.Sprintf("Task not found: %s", shortID)
	case errors.Is(err, domain.ErrConflict):
		return "Version conflict - task was modified by another process"
	case errors.Is(err, domain.ErrInvalidTransition):
		return err.Error()
	case errors.Is(err, domain.ErrCyclicParent):
		return domain.ErrCyclicParent.Error()
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

	// Description
	if cmd.Flags().Changed("description") {
		descVal, _ := cmd.Flags().GetString("description")
		var stdinFile *os.File
		if f, ok := cmd.InOrStdin().(*os.File); ok {
			stdinFile = f
		}
		desc, err := readDescription(descVal, stdinFile)
		if err != nil {
			return err
		}
		task.Description = desc
	}

	// Project
	if f, ok := fs.GetField("project"); ok {
		task.ProjectID = f.Value
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

	// UDA
	if cmd.Flags().Changed("uda") {
		udaVals, _ := cmd.Flags().GetStringArray("uda")
		udaMap, err := parseUDAFlags(udaVals)
		if err != nil {
			return err
		}
		task.UDA = udaMap
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

	r := NewRenderer(cmd.OutOrStdout(), a.format, a.colorEnabled(), nil)
	return r.renderMutationResult("Created", task, tags)
}

func (a *App) runList(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	input := strings.Join(args, " ")
	expr, parseErrs := filter.ParseExpr(input)
	if len(parseErrs) > 0 {
		return fmt.Errorf("filter errors:\n%s", filter.FormatErrors(parseErrs))
	}

	var filterExpr domain.FilterExpr
	if expr != nil {
		var resolveErrs []error
		filterExpr, resolveErrs = a.resolver.ResolveExpr(ctx, expr)
		if len(resolveErrs) > 0 {
			return resolveErrs[0]
		}
	} else {
		// No filter input — apply default statuses
		filterExpr = &domain.TermFilter{TaskFilter: domain.TaskFilter{
			Statuses: []string{"pending", "active"},
		}}
	}

	tasks, err := a.taskSvc.List(ctx, filterExpr)
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

	r := NewRenderer(cmd.OutOrStdout(), a.format, a.colorEnabled(), a.buildDimStatuses())
	return r.renderTaskList(tasks, taskTags)
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

	// Fetch tags
	tags, err := a.tagSvc.GetTaskTags(ctx, task.ID)
	if err != nil {
		return fmt.Errorf("loading tags: %w", err)
	}

	// Fetch and resolve relations
	var resolved []resolvedRelation
	if a.relationSvc != nil {
		rels, relErr := a.relationSvc.GetByTask(ctx, shortID)
		if relErr != nil {
			return fmt.Errorf("loading relations: %w", relErr)
		}
		for _, rel := range rels {
			rr := resolvedRelation{Relation: rel}
			if rel.TargetID == task.ID {
				// This task is the target — show inverse label and source task info
				switch rel.RelationType {
				case "blocks":
					rr.Label = "blocked_by"
				case "relates_to":
					rr.Label = "related_to"
				case "duplicates":
					rr.Label = "duplicated_by"
				}
				if other, lookupErr := a.taskSvc.GetByID(ctx, rel.SourceID); lookupErr == nil {
					rr.RelatedShortID = other.ShortID
					rr.RelatedTitle = other.Title
				}
			} else {
				rr.Label = rel.RelationType
				if other, lookupErr := a.taskSvc.GetByID(ctx, rel.TargetID); lookupErr == nil {
					rr.RelatedShortID = other.ShortID
					rr.RelatedTitle = other.Title
				}
			}
			resolved = append(resolved, rr)
		}
	}

	r := NewRenderer(cmd.OutOrStdout(), a.format, a.colorEnabled(), nil)
	return r.renderTaskInfo(task, annotations, tags, resolved)
}

func (a *App) runNext(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	task, err := a.taskSvc.Next(ctx)
	if err != nil {
		return err
	}

	annotations, err := a.taskSvc.GetAnnotations(ctx, task.ShortID)
	if err != nil {
		return fmt.Errorf("loading annotations: %w", err)
	}

	tags, err := a.tagSvc.GetTaskTags(ctx, task.ID)
	if err != nil {
		return fmt.Errorf("loading tags: %w", err)
	}

	// Fetch and resolve relations (same pattern as runInfo)
	var resolved []resolvedRelation
	if a.relationSvc != nil {
		rels, relErr := a.relationSvc.GetByTask(ctx, task.ShortID)
		if relErr != nil {
			return fmt.Errorf("loading relations: %w", relErr)
		}
		for _, rel := range rels {
			rr := resolvedRelation{Relation: rel}
			if rel.TargetID == task.ID {
				switch rel.RelationType {
				case "blocks":
					rr.Label = "blocked_by"
				case "relates_to":
					rr.Label = "related_to"
				case "duplicates":
					rr.Label = "duplicated_by"
				}
				if other, lookupErr := a.taskSvc.GetByID(ctx, rel.SourceID); lookupErr == nil {
					rr.RelatedShortID = other.ShortID
					rr.RelatedTitle = other.Title
				}
			} else {
				rr.Label = rel.RelationType
				if other, lookupErr := a.taskSvc.GetByID(ctx, rel.TargetID); lookupErr == nil {
					rr.RelatedShortID = other.ShortID
					rr.RelatedTitle = other.Title
				}
			}
			resolved = append(resolved, rr)
		}
	}

	r := NewRenderer(cmd.OutOrStdout(), a.format, a.colorEnabled(), nil)
	return r.renderTaskInfo(task, annotations, tags, resolved)
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

	// Description (double pointer: outer nil = don't change, outer non-nil + inner nil = clear)
	if cmd.Flags().Changed("description") {
		descVal, _ := cmd.Flags().GetString("description")
		var stdinFile *os.File
		if f, ok := cmd.InOrStdin().(*os.File); ok {
			stdinFile = f
		}
		desc, err := readDescription(descVal, stdinFile)
		if err != nil {
			return err
		}
		if desc == "" {
			var nilStr *string
			upd.Description = &nilStr
		} else {
			dp := &desc
			upd.Description = &dp
		}
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
		upd.ProjectID = &f.Value
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

	// UDA
	if cmd.Flags().Changed("uda") {
		udaVals, _ := cmd.Flags().GetStringArray("uda")
		udaMap, err := parseUDAFlags(udaVals)
		if err != nil {
			return err
		}
		upd.UDA = &udaMap
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

	r := NewRenderer(cmd.OutOrStdout(), a.format, a.colorEnabled(), nil)
	return r.renderMutationResult("Modified", updated, modTags)
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

	r := NewRenderer(cmd.OutOrStdout(), a.format, a.colorEnabled(), nil)
	return r.renderMutationResult("Started", updated, nil)
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

	r := NewRenderer(cmd.OutOrStdout(), a.format, a.colorEnabled(), nil)
	return r.renderMutationResult("Completed", updated, nil)
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

	r := NewRenderer(cmd.OutOrStdout(), a.format, a.colorEnabled(), nil)
	return r.renderMutationResult("Deleted", updated, nil)
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

	r := NewRenderer(cmd.OutOrStdout(), a.format, a.colorEnabled(), nil)
	return r.renderMutationResult("Annotated", task, nil)
}

func formatRelationError(err error, sourceShortID, targetShortID string) string {
	switch {
	case errors.Is(err, domain.ErrSourceNotFound):
		return fmt.Sprintf("Source task not found: %s", sourceShortID)
	case errors.Is(err, domain.ErrTargetNotFound):
		return fmt.Sprintf("Target task not found: %s", targetShortID)
	case errors.Is(err, domain.ErrNotFound):
		return fmt.Sprintf("Task not found: %s or %s", sourceShortID, targetShortID)
	case errors.Is(err, domain.ErrCyclicBlock):
		return "relation would create a cycle in blocks graph"
	case errors.Is(err, domain.ErrDuplicateRelation):
		return "relation already exists"
	default:
		return err.Error()
	}
}

func (a *App) runLink(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	sourceShortID := args[0]
	relType := args[1]
	targetShortID := args[2]

	rel, err := a.relationSvc.Add(ctx, sourceShortID, targetShortID, relType)
	if err != nil {
		return fmt.Errorf("%s", formatRelationError(err, sourceShortID, targetShortID))
	}

	r := NewRenderer(cmd.OutOrStdout(), a.format, a.colorEnabled(), nil)
	return r.renderLinkResult(rel, sourceShortID, targetShortID)
}

func (a *App) runUnlink(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	sourceShortID := args[0]
	relType := args[1]
	targetShortID := args[2]

	if err := a.relationSvc.Remove(ctx, sourceShortID, targetShortID, relType); err != nil {
		return fmt.Errorf("%s", formatRelationError(err, sourceShortID, targetShortID))
	}

	r := NewRenderer(cmd.OutOrStdout(), a.format, a.colorEnabled(), nil)
	return r.renderUnlinkResult(sourceShortID, relType, targetShortID)
}
