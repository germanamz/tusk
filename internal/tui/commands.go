package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/filter"
	"github.com/germanamz/tusk/syntax"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

// buildTaskCmd creates the `tusk task` parent command. Every task-scoped
// verb — CRUD, lifecycle, claim, queue, and relation — lives under this parent.
func (a *App) buildTaskCmd() *cobra.Command {
	parent := &cobra.Command{
		Use:   "task",
		Short: "Manage tasks",
		Long: `Create, view, modify, and coordinate tasks.

CRUD & Viewing:
  create      Create a new task
  list        List tasks with filters
  get         Show task details
  modify      Modify a task
  tree        Display task hierarchy

Lifecycle:
  start       Transition to active
  done        Transition to completed
  delete      Soft-delete (transition to deleted)
  next        Highest-urgency actionable task

Coordination:
  claim       Claim a task for a player
  release     Release a task claim
  available   List unclaimed, actionable, unblocked tasks
  pop         Atomically claim and start the top task

Annotations:
  annotate    Add a timestamped note

Relations:
  link        Create a typed relation (blocks, relates_to, duplicates)
  unlink      Remove a typed relation

Entity fields on create/modify flow through inline key=value syntax:
  title, description, level, project, priority, due, parent, status, uda.<key>
  Tags: +tag / -tag    File expansion: field=@./path, field=@-, @@escape`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	createCmd := &cobra.Command{
		Use:   "create [title] [key=value...] [+tag...]",
		Short: "Create a new task",
		Long: `Create a new task. The first positional argument is the task title (can also
be set via title=... for file-loading via @).

Accepted fields:
  project=<name>       Project to assign
  priority=0..4        Priority level (0=none, 4=urgent)
  due=<date>           Due date (absolute or relative)
  parent=<short_id>    Parent task for subtask creation
  description=<text>   Task description
  level=<name>         Task level (requires a taxonomy in config)
  status=<status>      Initial status (defaults to pending)

UDA fields:
  uda.<key>=<value>    Set arbitrary user-defined metadata

Tags:
  +tag                 Add a tag

File expansion:
  description=@./spec.md               Load file content
  description=@-                       Read from stdin
  description="see @./f for details"   Mid-string expansion
  title=@./title.txt                   Title from file
  @@                                   Escape literal @`,
		Example: `  # Basic task with priority and tag
  tusk task create "Implement auth middleware" project=backend priority=3 +api

  # Task with UDA metadata
  tusk task create "Deploy monitoring" uda.env=prod uda.region=eu

  # Description from file
  tusk task create "Design spec" description=@./spec.md

  # Title and description from files
  tusk task create title=@./title.txt description=@./body.md

  # Subtask
  tusk task create "Subtask" parent=a3f8b2c1`,
		Args: cobra.MinimumNArgs(1),
		RunE: a.runCreate,
	}

	modifyCmd := &cobra.Command{
		Use:   "modify <short_id> [key=value...]",
		Short: "Modify a task",
		Long: `Modify an existing task. The first positional argument is the task short ID.
Remaining arguments are field assignments and tag operations.

Field clearing: description= (empty value) clears the field.
Same for level=.

UDA operations:
  uda.key=value    Set a UDA key
  uda.key=         Delete a UDA key

Tags:
  +tag             Add a tag
  -tag             Remove a tag (use -- before -tag to avoid flag parsing)

File expansion works on description=, title=, and any string field:
  field=@./path    Load from file
  field=@-         Read from stdin
  @@               Escape literal @`,
		Example: `  # Update priority and add tag
  tusk task modify a3f8b2c1 priority=4 +urgent

  # Set UDA, change project
  tusk task modify a3f8b2c1 uda.team=backend project=backend

  # Clear a UDA key
  tusk task modify a3f8b2c1 uda.env=

  # Load description from file
  tusk task modify a3f8b2c1 description=@./updated-spec.md

  # Remove a tag (use -- to prevent flag parsing)
  tusk task modify a3f8b2c1 -- -obsolete`,
		Args: cobra.MinimumNArgs(1),
		RunE: a.runModify,
	}

	treeCmd := &cobra.Command{
		Use:   "tree [short_id]",
		Short: "Display tasks as a tree hierarchy",
		Long:  "Show all tasks in a tree hierarchy. Optionally specify a short_id to show only that subtree.",
		Example: `  # Full task tree
  tusk task tree

  # Subtree from a specific task
  tusk task tree a3f8b2c1

  # Include deleted tasks
  tusk task tree --all`,
		Args: cobra.MaximumNArgs(1),
		RunE: a.runTree,
	}
	treeCmd.Flags().Bool("all", false, "include deleted tasks")

	levelCheckCmd := &cobra.Command{
		Use:   "level-check [filters...]",
		Short: "Report tasks whose level violates their project taxonomy",
		Long: `Scan tasks against their project's effective taxonomy and list every task
whose state conflicts with it. Scans every status by default (including
terminal tasks) so retroactive violations surface. Accepts the same inline
filter syntax as tusk task list to narrow the scan.

Exit code is 0 when the workspace is clean and 1 when any violations exist.`,
		Example: `  # Scan the whole workspace
  tusk task level-check

  # Narrow to a single project
  tusk task level-check project=backend

  # JSON report for machine consumption
  tusk task level-check --format json`,
		RunE: a.runLevelCheck,
	}

	parent.AddCommand(
		createCmd,
		&cobra.Command{
			Use:   "list [filters...]",
			Short: "List tasks",
			Long: `List tasks sorted by urgency. Defaults to status=pending,active when no status
filter is given. Accepts the full filter syntax: field=value, +tag, -tag,
ranges (priority=2..4), relative dates (due=today..friday), boolean operators
(AND, OR, NOT), and parenthesized grouping.`,
			Example: `  # All pending and active tasks (default)
  tusk task list

  # Filter by project and tag
  tusk task list project=backend +api

  # Priority range
  tusk task list priority=2..4

  # Boolean filter
  tusk task list "(status=active AND +urgent) OR priority=4"

  # UDA filter
  tusk task list uda.env=prod`,
			RunE: a.runList,
		},
		&cobra.Command{
			Use:   "get <short_id>",
			Short: "Show task details",
			Long: `Show full details for a single task including status, priority, project, due
date, tags, UDAs, annotations, relations, claim state, and urgency score.`,
			Args: cobra.ExactArgs(1),
			RunE: a.runGet,
		},
		modifyCmd,
		treeCmd,
		levelCheckCmd,
		&cobra.Command{
			Use:   "start <short_id>",
			Short: "Transition task to active",
			Long: `Transition a task to active status. Auto-claims the task for the current player
if unclaimed. Rejects if claimed by another player. Use --player to identify
yourself.`,
			Args: cobra.ExactArgs(1),
			RunE: a.runStart,
		},
		&cobra.Command{
			Use:   "done <short_id>",
			Short: "Transition task to completed",
			Long: `Transition a task to completed status. The task must be in a status that has a
valid transition to the workflow's "done" status.`,
			Args: cobra.ExactArgs(1),
			RunE: a.runDone,
		},
		&cobra.Command{
			Use:   "delete <short_id>",
			Short: "Transition task to deleted",
			Long: `Soft-delete a task by transitioning it to the workflow's "delete" status. The
task remains in the database for history; it is excluded from default list views.`,
			Args: cobra.ExactArgs(1),
			RunE: a.runDelete,
		},
		&cobra.Command{
			Use:   "next",
			Short: "Show the highest-urgency actionable task",
			Long: `Show the single highest-urgency actionable task. "Actionable" means the task is
in a non-terminal status, is not blocked by other tasks, and is not waiting.`,
			Args: cobra.NoArgs,
			RunE: a.runNext,
		},
		&cobra.Command{
			Use:   "annotate <short_id> <message...>",
			Short: "Add a note to a task",
			Long: `Add a timestamped note to a task. The annotation body supports @file expansion:
use @./path.md to load content from a file, @- to read from stdin, or @@ to
escape a literal @.`,
			Example: `  # Inline annotation
  tusk task annotate a3f8b2c1 "Blocked by upstream API changes"

  # Annotation from file
  tusk task annotate a3f8b2c1 @./investigation.md

  # Annotation from stdin
  echo "piped content" | tusk task annotate a3f8b2c1 @-`,
			Args: cobra.MinimumNArgs(2),
			RunE: a.runAnnotate,
		},
		&cobra.Command{
			Use:   "claim <short_id>",
			Short: "Claim a task for the current player",
			Long: `Claim a task for the current player, signaling intent and preventing other
players from starting it. Requires --player to identify the claimant.`,
			Args: cobra.ExactArgs(1),
			RunE: a.runClaim,
		},
		&cobra.Command{
			Use:   "release <short_id>",
			Short: "Release a task claim",
			Long: `Release a task claim. Only the current claimant can release. Requires --player
to verify identity.`,
			Args: cobra.ExactArgs(1),
			RunE: a.runRelease,
		},
		&cobra.Command{
			Use:   "available [filters...]",
			Short: "List unclaimed, actionable, unblocked tasks",
			Long: `List tasks that are unclaimed, in a non-terminal status, and not blocked by
other tasks. Accepts filters to narrow results (e.g., project, tags, priority).`,
			Example: `  # All available tasks
  tusk task available --player agent-1

  # Available tasks in a specific project
  tusk task available project=backend --player agent-1`,
			RunE: a.runAvailable,
		},
		&cobra.Command{
			Use:   "pop [filters...]",
			Short: "Claim and start the highest-urgency available task",
			Long: `Atomically find the highest-urgency available task, claim it for the calling
player, and transition it to active. Replaces a list-filter-pick-claim sequence
with one command, eliminating race conditions. Requires --player.`,
			Example: `  # Pop highest-urgency available task
  tusk task pop --player agent-1

  # Pop from a specific project
  tusk task pop project=backend --player agent-1`,
			RunE: a.runPop,
		},
		&cobra.Command{
			Use:   "link <short_id> <relation_type> <short_id>",
			Short: "Create a relation between two tasks",
			Long:  `Create a typed relation. Types: blocks, relates_to, duplicates.`,
			Example: `  tusk task link a3f8b2c1 blocks b7c9d4e2
  tusk task link a3f8b2c1 relates_to c8d2e5f3
  tusk task link a3f8b2c1 duplicates d9e3f6a4`,
			Args: cobra.ExactArgs(3),
			RunE: a.runLink,
		},
		&cobra.Command{
			Use:     "unlink <short_id> <relation_type> <short_id>",
			Short:   "Remove a relation between two tasks",
			Long:    `Remove a typed relation. Types: blocks, relates_to, duplicates.`,
			Example: `  tusk task unlink a3f8b2c1 blocks b7c9d4e2`,
			Args:    cobra.ExactArgs(3),
			RunE:    a.runUnlink,
		},
	)

	return parent
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
	case errors.Is(err, domain.ErrNoAvailableTasks):
		return "No available tasks"
	case errors.Is(err, domain.ErrTaxonomyViolation):
		if msg, ok := formatTaxonomyError(err, ""); ok {
			return msg
		}
		return err.Error()
	default:
		return err.Error()
	}
}

// projectNameForTask resolves the human-readable project name for a task's
// ProjectID, falling back to the stringified UUID when the lookup fails.
// Used in handlers that want to include the project in TaxonomyError
// messages without exposing raw UUIDs to users.
func (a *App) projectNameForTask(ctx context.Context, projectID uuid.UUID) string {
	if a.projectSvc == nil {
		return projectID.String()
	}
	p, err := a.projectSvc.GetByID(ctx, projectID)
	if err != nil {
		return projectID.String()
	}
	return p.Name
}

func (a *App) runCreate(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	input := strings.Join(args, " ")
	fs, parseErrs := filter.Parse(input)
	if len(parseErrs) > 0 {
		return fmt.Errorf("%s", filter.FormatErrors(parseErrs))
	}

	var stdinFile *os.File
	if f, ok := cmd.InOrStdin().(*os.File); ok {
		stdinFile = f
	}
	state := &expandState{}

	var rawTitle string
	if f, ok := fs.GetField("title"); ok {
		rawTitle = f.Value
	} else {
		rawTitle = fs.Title()
	}
	if rawTitle == "" {
		return fmt.Errorf("title is required")
	}
	expandedTitle, err := a.expandRefsWithState(rawTitle, stdinFile, state)
	if err != nil {
		return err
	}

	task := &domain.Task{
		Title: expandedTitle,
	}

	if f, ok := fs.GetField("description"); ok {
		expandedDesc, err := a.expandRefsWithState(f.Value, stdinFile, state)
		if err != nil {
			return err
		}
		task.Description = expandedDesc
	}

	// Level (inline taxonomy assignment)
	if f, ok := fs.GetField("level"); ok {
		if f.Modifier != 0 {
			return fmt.Errorf("modifier %q not supported on level", string(f.Modifier))
		}
		if f.Value == "" {
			return fmt.Errorf("level= on create requires a value; use modify to clear")
		}
		v := f.Value
		task.Level = &v
	}

	// Project
	if f, ok := fs.GetField("project"); ok {
		resolved, err := a.taskSvc.ResolveProjectName(cmd.Context(), f.Value)
		if err != nil {
			return fmt.Errorf("resolving project %q: %w", f.Value, err)
		}
		task.ProjectID = resolved
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

	if err := validateKnownFields(fs); err != nil {
		return err
	}
	udaMap, err := collectUDAs(fs)
	if err != nil {
		return err
	}
	if udaMap != nil {
		task.UDA = udaMap
	}

	if err := a.taskSvc.Create(ctx, task); err != nil {
		if errors.Is(err, domain.ErrTaxonomyViolation) {
			if msg, ok := formatTaxonomyError(err, a.projectNameForTask(ctx, task.ProjectID)); ok {
				return fmt.Errorf("%s", msg)
			}
		}
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

	r := a.newRenderer(cmd.Context(), cmd.OutOrStdout(), nil)
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

	r := a.newRenderer(cmd.Context(), cmd.OutOrStdout(), a.buildDimStatuses())
	return r.renderTaskList(tasks, taskTags)
}

func (a *App) runLevelCheck(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	input := strings.Join(args, " ")
	expr, parseErrs := filter.ParseExpr(input)
	if len(parseErrs) > 0 {
		return fmt.Errorf("filter errors:\n%s", filter.FormatErrors(parseErrs))
	}

	var filterExpr domain.FilterExpr
	if expr != nil {
		var resolveErrs []error
		filterExpr, resolveErrs = a.resolver.ResolveExprAllStatuses(ctx, expr)
		if len(resolveErrs) > 0 {
			return resolveErrs[0]
		}
	}

	violations, err := a.taskSvc.LevelCheck(ctx, filterExpr)
	if err != nil {
		return err
	}

	r := a.newRenderer(cmd.Context(), cmd.OutOrStdout(), nil)
	if err := r.renderLevelCheck(violations); err != nil {
		return err
	}
	if len(violations) > 0 {
		return ErrLevelViolations
	}
	return nil
}

// ErrLevelViolations is returned by `tusk task level-check` when the scan
// found any violating tasks. The CLI entry point (cmd/tusk/main.go) detects
// this sentinel and exits with status 1 without printing a redundant error
// line — the renderer has already listed the violations.
var ErrLevelViolations = errors.New("taxonomy violations detected")

func (a *App) runGet(cmd *cobra.Command, args []string) error {
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

	r := a.newRenderer(cmd.Context(), cmd.OutOrStdout(), nil)
	return r.renderTaskInfo(task, annotations, tags, resolved)
}

func (a *App) runNext(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	task, err := a.taskSvc.Next(ctx)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return fmt.Errorf("no actionable tasks")
		}
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

	// Fetch and resolve relations (same pattern as runGet)
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

	r := a.newRenderer(cmd.Context(), cmd.OutOrStdout(), nil)
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

	var stdinFile *os.File
	if f, ok := cmd.InOrStdin().(*os.File); ok {
		stdinFile = f
	}
	state := &expandState{}

	// Title: inline field wins over free text; both pass through the expander.
	if f, ok := fs.GetField("title"); ok {
		expanded, err := a.expandRefsWithState(f.Value, stdinFile, state)
		if err != nil {
			return err
		}
		upd.Title = &expanded
	} else if title := fs.Title(); title != "" {
		expanded, err := a.expandRefsWithState(title, stdinFile, state)
		if err != nil {
			return err
		}
		upd.Title = &expanded
	}

	// Description (double pointer: outer nil = don't change, outer non-nil + inner nil = clear)
	if f, ok := fs.GetField("description"); ok {
		if f.Value == "" {
			var nilStr *string
			upd.Description = &nilStr
		} else {
			expanded, err := a.expandRefsWithState(f.Value, stdinFile, state)
			if err != nil {
				return err
			}
			dp := &expanded
			upd.Description = &dp
		}
	}

	// Level (double pointer: empty value clears the level)
	if f, ok := fs.GetField("level"); ok {
		if f.Modifier != 0 {
			return fmt.Errorf("modifier %q not supported on level", string(f.Modifier))
		}
		if f.Value == "" {
			var nilStr *string
			upd.Level = &nilStr
		} else {
			v := f.Value
			lp := &v
			upd.Level = &lp
		}
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
		resolved, err := a.taskSvc.ResolveProjectName(ctx, f.Value)
		if err != nil {
			return fmt.Errorf("resolving project %q: %w", f.Value, err)
		}
		upd.ProjectID = &resolved
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

	if err := validateKnownFields(fs); err != nil {
		return err
	}
	udaMap, err := collectUDAs(fs)
	if err != nil {
		return err
	}
	if udaMap != nil {
		upd.UDA = &udaMap
	}

	updated, err := a.taskSvc.Update(ctx, upd)
	if err != nil {
		if errors.Is(err, domain.ErrTaxonomyViolation) {
			projectID := current.ProjectID
			if upd.ProjectID != nil {
				projectID = *upd.ProjectID
			}
			if msg, ok := formatTaxonomyError(err, a.projectNameForTask(ctx, projectID)); ok {
				return fmt.Errorf("%s", msg)
			}
		}
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

	r := a.newRenderer(cmd.Context(), cmd.OutOrStdout(), nil)
	return r.renderMutationResult("Modified", updated, modTags)
}

func (a *App) runStart(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	shortID := args[0]

	// Auto-register player if --player is set
	if a.playerID != "" {
		if err := a.ensurePlayer(ctx); err != nil {
			return err
		}
	}

	current, err := a.taskSvc.GetByShortID(ctx, shortID)
	if err != nil {
		return fmt.Errorf("%s", formatError(err, shortID))
	}

	updated, err := a.taskSvc.Start(ctx, shortID, current.Version, a.playerID)
	if err != nil {
		if errors.Is(err, domain.ErrTaskClaimed) {
			return fmt.Errorf("%s", formatClaimError(err, shortID))
		}
		return fmt.Errorf("%s", formatError(err, shortID))
	}

	r := a.newRenderer(cmd.Context(), cmd.OutOrStdout(), nil)
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

	r := a.newRenderer(cmd.Context(), cmd.OutOrStdout(), nil)
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

	r := a.newRenderer(cmd.Context(), cmd.OutOrStdout(), nil)
	return r.renderMutationResult("Deleted", updated, nil)
}

func (a *App) runAnnotate(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	shortID := args[0]
	body := strings.Join(args[1:], " ")

	var stdinFile *os.File
	if f, ok := cmd.InOrStdin().(*os.File); ok {
		stdinFile = f
	}
	expandedBody, err := a.expandRefs(body, stdinFile)
	if err != nil {
		return err
	}

	_, err = a.taskSvc.Annotate(ctx, shortID, expandedBody)
	if err != nil {
		return fmt.Errorf("%s", formatError(err, shortID))
	}

	task, err := a.taskSvc.GetByShortID(ctx, shortID)
	if err != nil {
		return fmt.Errorf("%s", formatError(err, shortID))
	}

	r := a.newRenderer(cmd.Context(), cmd.OutOrStdout(), nil)
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

	r := a.newRenderer(cmd.Context(), cmd.OutOrStdout(), nil)
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

	r := a.newRenderer(cmd.Context(), cmd.OutOrStdout(), nil)
	return r.renderUnlinkResult(sourceShortID, relType, targetShortID)
}

func (a *App) runClaim(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	shortID := args[0]

	if a.playerID == "" {
		return fmt.Errorf("--player flag is required for claim")
	}

	// Auto-register player if not already registered
	if err := a.ensurePlayer(ctx); err != nil {
		return err
	}

	current, err := a.taskSvc.GetByShortID(ctx, shortID)
	if err != nil {
		return fmt.Errorf("%s", formatError(err, shortID))
	}

	updated, err := a.taskSvc.Claim(ctx, shortID, a.playerID, current.Version)
	if err != nil {
		return fmt.Errorf("%s", formatClaimError(err, shortID))
	}

	r := a.newRenderer(cmd.Context(), cmd.OutOrStdout(), nil)
	return r.renderMutationResult("Claimed", updated, nil)
}

func (a *App) runRelease(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	shortID := args[0]

	if a.playerID == "" {
		return fmt.Errorf("--player flag is required for release")
	}

	current, err := a.taskSvc.GetByShortID(ctx, shortID)
	if err != nil {
		return fmt.Errorf("%s", formatError(err, shortID))
	}

	updated, err := a.taskSvc.Release(ctx, shortID, a.playerID, current.Version)
	if err != nil {
		return fmt.Errorf("%s", formatClaimError(err, shortID))
	}

	r := a.newRenderer(cmd.Context(), cmd.OutOrStdout(), nil)
	return r.renderMutationResult("Released", updated, nil)
}

// ensurePlayer auto-registers the current --player as "human" if not yet registered.
func (a *App) ensurePlayer(ctx context.Context) error {
	if a.playerSvc == nil || a.playerID == "" {
		return nil
	}
	_, err := a.playerSvc.GetByID(ctx, a.playerID)
	if err == nil {
		return nil // already registered
	}
	if errors.Is(err, domain.ErrNotFound) {
		_, regErr := a.playerSvc.Register(ctx, a.playerID, "human")
		if regErr != nil && !errors.Is(regErr, domain.ErrConflict) {
			return fmt.Errorf("auto-registering player: %w", regErr)
		}
		return nil
	}
	return fmt.Errorf("checking player: %w", err)
}

func formatClaimError(err error, shortID string) string {
	switch {
	case errors.Is(err, domain.ErrTaskClaimed):
		return fmt.Sprintf("Task %s is already claimed by another player", shortID)
	case errors.Is(err, domain.ErrNotFound):
		return fmt.Sprintf("Task not found: %s", shortID)
	case errors.Is(err, domain.ErrConflict):
		return "Version conflict - task was modified by another process"
	default:
		return err.Error()
	}
}

func (a *App) runAvailable(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	if a.playerID == "" {
		return fmt.Errorf("--player flag is required for available")
	}

	if err := a.ensurePlayer(ctx); err != nil {
		return err
	}

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
	}

	tasks, err := a.taskSvc.Available(ctx, filterExpr)
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

	taskTags := make(map[string][]*domain.Tag, len(tagsByTaskID))
	for id, tags := range tagsByTaskID {
		taskTags[id.String()] = tags
	}

	r := a.newRenderer(cmd.Context(), cmd.OutOrStdout(), a.buildDimStatuses())
	return r.renderTaskList(tasks, taskTags)
}

func (a *App) runPop(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	if a.playerID == "" {
		return fmt.Errorf("--player flag is required for pop")
	}

	if err := a.ensurePlayer(ctx); err != nil {
		return err
	}

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
	}

	task, err := a.taskSvc.Pop(ctx, a.playerID, filterExpr)
	if err != nil {
		return fmt.Errorf("%s", formatError(err, ""))
	}

	// Load tags for the task
	tags, err := a.tagSvc.GetTaskTags(ctx, task.ID)
	if err != nil {
		return fmt.Errorf("loading tags: %w", err)
	}

	annotations, err := a.taskSvc.GetAnnotations(ctx, task.ShortID)
	if err != nil {
		return fmt.Errorf("loading annotations: %w", err)
	}

	// Fetch and resolve relations
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

	r := a.newRenderer(cmd.Context(), cmd.OutOrStdout(), nil)
	return r.renderTaskInfo(task, annotations, tags, resolved)
}

// buildPlayerCmd creates the `tusk player` subcommand group.
func (a *App) buildPlayerCmd() *cobra.Command {
	registerCmd := &cobra.Command{
		Use:   "register <id>",
		Short: "Register a new player",
		Args:  cobra.ExactArgs(1),
		RunE:  a.runPlayerRegister,
	}
	registerCmd.Flags().String("type", "agent", `player type: "human" or "agent"`)

	modifyCmd := &cobra.Command{
		Use:   "modify <id> [fields...]",
		Short: "Modify a player's settings",
		Long: `Update a player's configurable fields.

Supported fields:
  note-window-size=<N>   per-player override for the notes trailing window
  note-window-size=      clear the override (fall back to project/global default)

Examples:
  tusk player modify agent-1 note-window-size=50
  tusk player modify agent-1 note-window-size=`,
		Args: cobra.MinimumNArgs(2),
		RunE: a.runPlayerModify,
	}

	playerCmd := &cobra.Command{
		Use:   "player",
		Short: "Player management",
	}
	playerCmd.AddCommand(registerCmd)
	playerCmd.AddCommand(modifyCmd)
	return playerCmd
}

func (a *App) runPlayerRegister(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	id := args[0]
	playerType, _ := cmd.Flags().GetString("type")

	player, err := a.playerSvc.Register(ctx, id, playerType)
	if err != nil {
		return fmt.Errorf("%s", err)
	}

	r := a.newRenderer(cmd.Context(), cmd.OutOrStdout(), nil)
	return r.renderPlayerResult("Registered", player)
}

func (a *App) runPlayerModify(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	id := args[0]
	if id == "" {
		return fmt.Errorf("player ID must not be empty")
	}

	fs, parseErrs := syntax.ParseFields(strings.Join(args[1:], " "))
	if len(parseErrs) > 0 {
		return fmt.Errorf("%s", filter.FormatErrors(parseErrs))
	}

	var (
		sawNoteWindow bool
		newSize       *int
	)

	for _, f := range fs.Fields {
		switch f.Key {
		case "note-window-size":
			if f.Modifier != 0 {
				return fmt.Errorf("note-window-size does not accept %q prefix", string(f.Modifier))
			}
			sawNoteWindow = true
			if f.Value == "" {
				newSize = nil
				continue
			}
			n, parseErr := strconv.Atoi(f.Value)
			if parseErr != nil {
				return fmt.Errorf("note-window-size must be an integer, got %q", f.Value)
			}
			if n <= 0 {
				return fmt.Errorf("note-window-size must be positive, got %d", n)
			}
			v := n
			newSize = &v
		default:
			return fmt.Errorf("unknown field %q on player modify", f.Key)
		}
	}

	if !sawNoteWindow {
		return fmt.Errorf("no modifiable fields supplied")
	}

	player, err := a.playerSvc.SetNoteWindowSize(ctx, id, newSize)
	if err != nil {
		return fmt.Errorf("%s", err)
	}

	r := a.newRenderer(cmd.Context(), cmd.OutOrStdout(), nil)
	return r.renderPlayerResult("Updated", player)
}
