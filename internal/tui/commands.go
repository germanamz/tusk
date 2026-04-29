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
func (app *App) buildTaskCmd() *cobra.Command {
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
		RunE: app.runCreate,
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
		RunE: app.runModify,
	}

	treeCmd := &cobra.Command{
		Use:   "tree [short_id]",
		Short: "Display tasks as a tree hierarchy",
		Long: `Show all tasks in a tree hierarchy. Optionally specify a short_id to show
only that subtree. Siblings render in ascending sibling-order by default;
pass --sort to switch. Pass --rollup to annotate every branch node with a
[done/total done, %] progress badge and a (status: count, ...) breakdown
of its descendants; in JSON mode every node carries a rollup field.`,
		Example: `  # Full task tree
  tusk task tree

  # Subtree from a specific task
  tusk task tree a3f8b2c1

  # Include deleted tasks
  tusk task tree --all

  # Re-sort siblings by urgency
  tusk task tree --sort urgency

  # Show progress rollup on every branch node
  tusk task tree --rollup`,
		Args: cobra.MaximumNArgs(1),
		RunE: app.runTree,
	}
	treeCmd.Flags().Bool("all", false, "include deleted tasks")
	treeCmd.Flags().String("sort", "order", "sibling sort key: order|urgency|created|priority|due")
	treeCmd.Flags().Bool("rollup", false, "annotate branch nodes with descendant rollup stats")

	summaryCmd := &cobra.Command{
		Use:   "summary [<short_id> | filter...]",
		Short: "Summarize task progress with descendant rollups",
		Long: `Summarize task progress as a rollup of descendants by status.

With a short_id, summarize that task's subtree.
With filter terms, one block per matching task. The filter restricts both
which tasks become blocks and which descendants are counted, unless
--full is passed (in which case the filter only selects blocks).
With no arguments, summarize each root task plus a totals line.`,
		Example: `  # Single subtree
  tusk task summary a3f8b2c1

  # All root tasks (workspace-wide)
  tusk task summary

  # One block per story; counts limited to story-level descendants
  tusk task summary level=story

  # One block per initiative; counts include the full subtree under each
  tusk task summary --full level=initiative`,
		Args: cobra.ArbitraryArgs,
		RunE: app.runSummary,
	}
	summaryCmd.Flags().Bool("full", false, "with a filter, count the full subtree under each block (otherwise the filter restricts descendant counting too)")

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
		RunE: app.runLevelCheck,
	}

	listCmd := &cobra.Command{
		Use:   "list [filters...]",
		Short: "List tasks",
		Long: `List tasks sorted by urgency. Defaults to status=pending,active when no status
filter is given. Accepts the full filter syntax: field=value, +tag, -tag,
ranges (priority=2..4), relative dates (due=today..friday), boolean operators
(AND, OR, NOT), and parenthesized grouping. Pass --sort to switch the sort
key.`,
		Example: `  # All pending and active tasks (default)
  tusk task list

  # Filter by project and tag
  tusk task list project=backend +api

  # Priority range
  tusk task list priority=2..4

  # Boolean filter
  tusk task list "(status=active AND +urgent) OR priority=4"

  # UDA filter
  tusk task list uda.env=prod

  # Siblings under a parent, in sibling-order
  tusk task list parent=a3f8b2c1 --sort order`,
		RunE: app.runList,
	}
	listCmd.Flags().String("sort", "urgency", "sort key: order|urgency|created|priority|due")

	parent.AddCommand(
		createCmd,
		listCmd,
		&cobra.Command{
			Use:   "get <short_id>",
			Short: "Show task details",
			Long: `Show full details for a single task including status, priority, project, due
date, tags, UDAs, annotations, relations, claim state, and urgency score.`,
			Args: cobra.ExactArgs(1),
			RunE: app.runGet,
		},
		modifyCmd,
		treeCmd,
		summaryCmd,
		levelCheckCmd,
		&cobra.Command{
			Use:   "start <short_id>",
			Short: "Transition task to active",
			Long: `Transition a task to active status. Auto-claims the task for the current player
if unclaimed. Rejects if claimed by another player. Use --player to identify
yourself.`,
			Args: cobra.ExactArgs(1),
			RunE: app.runStart,
		},
		&cobra.Command{
			Use:   "done <short_id>",
			Short: "Transition task to completed",
			Long: `Transition a task to completed status. The task must be in a status that has a
valid transition to the workflow's "done" status.`,
			Args: cobra.ExactArgs(1),
			RunE: app.runDone,
		},
		&cobra.Command{
			Use:   "delete <short_id>",
			Short: "Transition task to deleted",
			Long: `Soft-delete a task by transitioning it to the workflow's "delete" status. The
task remains in the database for history; it is excluded from default list views.`,
			Args: cobra.ExactArgs(1),
			RunE: app.runDelete,
		},
		&cobra.Command{
			Use:   "next",
			Short: "Show the highest-urgency actionable task",
			Long: `Show the single highest-urgency actionable task. "Actionable" means the task is
in a non-terminal status, is not blocked by other tasks, and is not waiting.`,
			Args: cobra.NoArgs,
			RunE: app.runNext,
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
			RunE: app.runAnnotate,
		},
		&cobra.Command{
			Use:   "claim <short_id>",
			Short: "Claim a task for the current player",
			Long: `Claim a task for the current player, signaling intent and preventing other
players from starting it. Requires --player to identify the claimant.`,
			Args: cobra.ExactArgs(1),
			RunE: app.runClaim,
		},
		&cobra.Command{
			Use:   "release <short_id>",
			Short: "Release a task claim",
			Long: `Release a task claim. Only the current claimant can release. Requires --player
to verify identity.`,
			Args: cobra.ExactArgs(1),
			RunE: app.runRelease,
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
			RunE: app.runAvailable,
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
			RunE: app.runPop,
		},
		&cobra.Command{
			Use:   "link <short_id> <relation_type> <short_id>",
			Short: "Create a relation between two tasks",
			Long:  `Create a typed relation. Types: blocks, relates_to, duplicates.`,
			Example: `  tusk task link a3f8b2c1 blocks b7c9d4e2
  tusk task link a3f8b2c1 relates_to c8d2e5f3
  tusk task link a3f8b2c1 duplicates d9e3f6a4`,
			Args: cobra.ExactArgs(3),
			RunE: app.runLink,
		},
		&cobra.Command{
			Use:     "unlink <short_id> <relation_type> <short_id>",
			Short:   "Remove a relation between two tasks",
			Long:    `Remove a typed relation. Types: blocks, relates_to, duplicates.`,
			Example: `  tusk task unlink a3f8b2c1 blocks b7c9d4e2`,
			Args:    cobra.ExactArgs(3),
			RunE:    app.runUnlink,
		},
		app.buildTaskMoveCmd(),
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
func (app *App) projectNameForTask(ctx context.Context, projectID uuid.UUID) string {
	if app.projectSvc == nil {
		return projectID.String()
	}
	project, err := app.projectSvc.GetByID(ctx, projectID)

	if err != nil {
		return projectID.String()
	}

	return project.Name
}

func (app *App) runCreate(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	input := strings.Join(args, " ")
	fs, parseErrs := filter.Parse(input)
	if len(parseErrs) > 0 {
		return fmt.Errorf("%s", filter.FormatErrors(parseErrs))
	}

	var stdinFile *os.File
	if file, ok := cmd.InOrStdin().(*os.File); ok {
		stdinFile = file
	}
	state := &expandState{}

	var rawTitle string
	if field, ok := fs.GetField("title"); ok {
		rawTitle = field.Value
	} else {
		rawTitle = fs.Title()
	}
	if rawTitle == "" {
		return fmt.Errorf("title is required")
	}
	expandedTitle, expandTitleErr := app.expandRefsWithState(rawTitle, stdinFile, state)

	if expandTitleErr != nil {
		return expandTitleErr
	}

	task := &domain.Task{
		Title: expandedTitle,
	}

	if field, ok := fs.GetField("description"); ok {
		expandedDesc, expandDescErr := app.expandRefsWithState(field.Value, stdinFile, state)

		if expandDescErr != nil {
			return expandDescErr
		}

		task.Description = expandedDesc
	}

	// Level (inline taxonomy assignment)
	if field, ok := fs.GetField("level"); ok {
		if field.Modifier != 0 {
			return fmt.Errorf("modifier %q not supported on level", string(field.Modifier))
		}
		if field.Value == "" {
			return fmt.Errorf("level= on create requires a value; use modify to clear")
		}
		levelVal := field.Value
		task.Level = &levelVal
	}

	// Project
	if field, ok := fs.GetField("project"); ok {
		resolved, resolveErr := app.taskSvc.ResolveProjectName(cmd.Context(), field.Value)

		if resolveErr != nil {
			return fmt.Errorf("resolving project %q: %w", field.Value, resolveErr)
		}

		task.ProjectID = resolved
	}

	// Priority
	if field, ok := fs.GetField("priority"); ok {
		priority, priorityErr := filter.ParsePriorityValue(field.Value)

		if priorityErr != nil {
			return priorityErr
		}

		task.Priority = priority
	}

	// Order
	if field, ok := fs.GetField("order"); ok {
		if field.Modifier != 0 {
			return fmt.Errorf("order does not accept + or - modifiers; use tusk task move to reposition")
		}
		if field.Value == "" {
			return fmt.Errorf("order= requires a numeric value on create")
		}
		orderVal, orderErr := strconv.ParseFloat(field.Value, 64)

		if orderErr != nil {
			return fmt.Errorf("invalid order value %q: %w", field.Value, orderErr)
		}

		task.Order = &orderVal
	}

	// Status (rarely used, defaults to pending in service)
	if field, ok := fs.GetField("status"); ok {
		task.Status = field.Value
	}

	// Due date
	if field, ok := fs.GetField("due"); ok {
		dueDate, dueDateErr := filter.ParseDateValue(field.Value)

		if dueDateErr != nil {
			return dueDateErr
		}

		task.DueAt = &dueDate
	}

	// Parent
	if field, ok := fs.GetField("parent"); ok {
		parentTask, parentErr := app.taskSvc.GetByShortID(ctx, field.Value)

		if parentErr != nil {
			return fmt.Errorf("%s", formatError(parentErr, field.Value))
		}

		task.ParentID = &parentTask.ID
	}

	if validateErr := validateKnownFields(fs); validateErr != nil {
		return validateErr
	}
	udaMap, udaErr := collectUDAs(fs)

	if udaErr != nil {
		return udaErr
	}

	if udaMap != nil {
		task.UDA = udaMap
	}

	if createErr := app.taskSvc.Create(ctx, task); createErr != nil {
		if errors.Is(createErr, domain.ErrTaxonomyViolation) {
			if msg, ok := formatTaxonomyError(createErr, app.projectNameForTask(ctx, task.ProjectID)); ok {
				return fmt.Errorf("%s", msg)
			}
		}
		return fmt.Errorf("%s", createErr)
	}

	// Assign tags if any were specified
	incTags := fs.IncludeTags()
	if len(incTags) > 0 {
		if assignErr := app.tagSvc.AssignToTask(ctx, task.ID, incTags); assignErr != nil {
			return fmt.Errorf("assigning tags: %w", assignErr)
		}
	}

	// Fetch tags for output
	tags, tagsErr := app.tagSvc.GetTaskTags(ctx, task.ID)

	if tagsErr != nil {
		return fmt.Errorf("loading tags: %w", tagsErr)
	}

	renderer := app.newRenderer(cmd.Context(), cmd.OutOrStdout(), nil)
	return renderer.renderMutationResult("Created", task, tags)
}

func (app *App) runList(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	sortMode, _ := cmd.Flags().GetString("sort")
	if err := validateSortMode(sortMode); err != nil {
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
		filterExpr, resolveErrs = app.resolver.ResolveExpr(ctx, expr)
		if len(resolveErrs) > 0 {
			return resolveErrs[0]
		}
	} else {
		// No filter input — apply default statuses
		filterExpr = &domain.TermFilter{TaskFilter: domain.TaskFilter{
			Statuses: []string{"pending", "active"},
		}}
	}

	tasks, listErr := app.taskSvc.List(ctx, filterExpr)

	if listErr != nil {
		return listErr
	}

	// Service returns tasks sorted by urgency; re-sort when the user asked
	// for a different key. Urgency is already applied, so "urgency" is a
	// no-op (sortTasks runs anyway so tie-breaking stays stable).
	sortTasks(tasks, sortMode)

	// Fetch tags for all tasks in one query
	taskIDs := make([]uuid.UUID, len(tasks))
	for index, task := range tasks {
		taskIDs[index] = task.ID
	}
	tagsByTaskID, tagsErr := app.tagSvc.GetTaskTagsBatch(ctx, taskIDs)

	if tagsErr != nil {
		return fmt.Errorf("loading tags: %w", tagsErr)
	}

	// Convert uuid.UUID keys to string keys for the render layer
	taskTags := make(map[string][]*domain.Tag, len(tagsByTaskID))
	for taskID, tags := range tagsByTaskID {
		taskTags[taskID.String()] = tags
	}

	renderer := app.newRenderer(cmd.Context(), cmd.OutOrStdout(), app.buildDimStatuses())
	return renderer.renderTaskList(tasks, taskTags)
}

func (app *App) runLevelCheck(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	input := strings.Join(args, " ")
	expr, parseErrs := filter.ParseExpr(input)
	if len(parseErrs) > 0 {
		return fmt.Errorf("filter errors:\n%s", filter.FormatErrors(parseErrs))
	}

	var filterExpr domain.FilterExpr
	if expr != nil {
		var resolveErrs []error
		filterExpr, resolveErrs = app.resolver.ResolveExprAllStatuses(ctx, expr)
		if len(resolveErrs) > 0 {
			return resolveErrs[0]
		}
	}

	violations, checkErr := app.taskSvc.LevelCheck(ctx, filterExpr)

	if checkErr != nil {
		return checkErr
	}

	renderer := app.newRenderer(cmd.Context(), cmd.OutOrStdout(), nil)
	if renderErr := renderer.renderLevelCheck(violations); renderErr != nil {
		return renderErr
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

func (app *App) runGet(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	shortID := args[0]

	task, taskErr := app.taskSvc.GetByShortID(ctx, shortID)

	if taskErr != nil {
		return fmt.Errorf("%s", formatError(taskErr, shortID))
	}

	annotations, annotationsErr := app.taskSvc.GetAnnotations(ctx, shortID)

	if annotationsErr != nil {
		return fmt.Errorf("loading annotations: %w", annotationsErr)
	}

	// Fetch tags
	tags, tagsErr := app.tagSvc.GetTaskTags(ctx, task.ID)

	if tagsErr != nil {
		return fmt.Errorf("loading tags: %w", tagsErr)
	}

	// Fetch and resolve relations
	var resolved []resolvedRelation
	if app.relationSvc != nil {
		rels, relErr := app.relationSvc.GetByTask(ctx, shortID)

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
				if other, lookupErr := app.taskSvc.GetByID(ctx, rel.SourceID); lookupErr == nil {
					rr.RelatedShortID = other.ShortID
					rr.RelatedTitle = other.Title
				}
			} else {
				rr.Label = rel.RelationType
				if other, lookupErr := app.taskSvc.GetByID(ctx, rel.TargetID); lookupErr == nil {
					rr.RelatedShortID = other.ShortID
					rr.RelatedTitle = other.Title
				}
			}
			resolved = append(resolved, rr)
		}
	}

	renderer := app.newRenderer(cmd.Context(), cmd.OutOrStdout(), nil)
	return renderer.renderTaskInfo(task, annotations, tags, resolved)
}

func (app *App) runNext(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	task, taskErr := app.taskSvc.Next(ctx)

	if taskErr != nil {
		if errors.Is(taskErr, domain.ErrNotFound) {
			return fmt.Errorf("no actionable tasks")
		}
		return taskErr
	}

	annotations, annotationsErr := app.taskSvc.GetAnnotations(ctx, task.ShortID)

	if annotationsErr != nil {
		return fmt.Errorf("loading annotations: %w", annotationsErr)
	}

	tags, tagsErr := app.tagSvc.GetTaskTags(ctx, task.ID)

	if tagsErr != nil {
		return fmt.Errorf("loading tags: %w", tagsErr)
	}

	// Fetch and resolve relations (same pattern as runGet)
	var resolved []resolvedRelation
	if app.relationSvc != nil {
		rels, relErr := app.relationSvc.GetByTask(ctx, task.ShortID)

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
				if other, lookupErr := app.taskSvc.GetByID(ctx, rel.SourceID); lookupErr == nil {
					rr.RelatedShortID = other.ShortID
					rr.RelatedTitle = other.Title
				}
			} else {
				rr.Label = rel.RelationType
				if other, lookupErr := app.taskSvc.GetByID(ctx, rel.TargetID); lookupErr == nil {
					rr.RelatedShortID = other.ShortID
					rr.RelatedTitle = other.Title
				}
			}
			resolved = append(resolved, rr)
		}
	}

	renderer := app.newRenderer(cmd.Context(), cmd.OutOrStdout(), nil)
	return renderer.renderTaskInfo(task, annotations, tags, resolved)
}

func (app *App) runModify(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	shortID := args[0]

	input := strings.Join(args[1:], " ")
	fs, parseErrs := filter.Parse(input)
	if len(parseErrs) > 0 {
		return fmt.Errorf("%s", filter.FormatErrors(parseErrs))
	}

	urgencyInputs := make([]urgencyFieldInput, len(fs.Fields))
	for index, field := range fs.Fields {
		urgencyInputs[index] = urgencyFieldInput{Key: field.Key, Value: field.Value, Modifier: field.Modifier}
	}
	urgencyResult, notConsumed, urgencyErr := parseUrgencyFields(urgencyInputs)

	if urgencyErr != nil {
		return urgencyErr
	}

	if len(notConsumed) != len(fs.Fields) {
		remaining := make([]filter.FieldFilter, 0, len(notConsumed))
		for _, idx := range notConsumed {
			remaining = append(remaining, fs.Fields[idx])
		}
		fs.Fields = remaining
	}

	// Auto-fetch current version
	current, currentErr := app.taskSvc.GetByShortID(ctx, shortID)

	if currentErr != nil {
		return fmt.Errorf("%s", formatError(currentErr, shortID))
	}

	upd := domain.TaskUpdate{
		ShortID: shortID,
		Version: current.Version,
	}

	if urgencyResult.ClearAll || len(urgencyResult.Clear) > 0 || len(urgencyResult.Set) > 0 {
		upd.UrgencyMergePatch = &domain.UrgencyOverridesPatch{
			ClearAll: urgencyResult.ClearAll,
			Clear:    urgencyResult.Clear,
			Set:      urgencyResult.Set,
		}
	}
	if len(urgencyResult.Delta) > 0 {
		upd.UrgencyDelta = urgencyResult.Delta
	}

	var stdinFile *os.File
	if file, ok := cmd.InOrStdin().(*os.File); ok {
		stdinFile = file
	}
	state := &expandState{}

	// Title: inline field wins over free text; both pass through the expander.
	if field, ok := fs.GetField("title"); ok {
		expanded, expandErr := app.expandRefsWithState(field.Value, stdinFile, state)

		if expandErr != nil {
			return expandErr
		}

		upd.Title = &expanded
	} else if title := fs.Title(); title != "" {
		expanded, expandErr := app.expandRefsWithState(title, stdinFile, state)

		if expandErr != nil {
			return expandErr
		}

		upd.Title = &expanded
	}

	// Description (double pointer: outer nil = don't change, outer non-nil + inner nil = clear)
	if field, ok := fs.GetField("description"); ok {
		if field.Value == "" {
			var nilStr *string
			upd.Description = &nilStr
		} else {
			expanded, expandErr := app.expandRefsWithState(field.Value, stdinFile, state)

			if expandErr != nil {
				return expandErr
			}

			dp := &expanded
			upd.Description = &dp
		}
	}

	// Level (double pointer: empty value clears the level)
	if field, ok := fs.GetField("level"); ok {
		if field.Modifier != 0 {
			return fmt.Errorf("modifier %q not supported on level", string(field.Modifier))
		}
		if field.Value == "" {
			var nilStr *string
			upd.Level = &nilStr
		} else {
			levelVal := field.Value
			levelPtr := &levelVal
			upd.Level = &levelPtr
		}
	}

	// Priority
	if field, ok := fs.GetField("priority"); ok {
		priority, priorityErr := filter.ParsePriorityValue(field.Value)

		if priorityErr != nil {
			return priorityErr
		}

		upd.Priority = &priority
	}

	// Order (double pointer: nil = don't change, *nil = clear, *v = set absolute)
	if field, ok := fs.GetField("order"); ok {
		if field.Modifier != 0 {
			return fmt.Errorf("order does not accept + or - modifiers; use tusk task move to reposition")
		}
		if field.Value == "" {
			var nilFloat *float64
			upd.Order = &nilFloat
		} else {
			orderVal, orderErr := strconv.ParseFloat(field.Value, 64)

			if orderErr != nil {
				return fmt.Errorf("invalid order value %q: %w", field.Value, orderErr)
			}

			orderPtr := &orderVal
			upd.Order = &orderPtr
		}
	}

	// Status
	if field, ok := fs.GetField("status"); ok {
		statusVal := field.Value
		upd.Status = &statusVal
	}

	// Due date (double pointer: outer nil = don't change, outer non-nil + inner nil = clear)
	if field, ok := fs.GetField("due"); ok {
		if field.Value == "" {
			var nilTime *time.Time
			upd.DueAt = &nilTime
		} else {
			dueDate, dueDateErr := filter.ParseDateValue(field.Value)

			if dueDateErr != nil {
				return dueDateErr
			}

			duePtr := &dueDate
			upd.DueAt = &duePtr
		}
	}

	// Project
	if field, ok := fs.GetField("project"); ok {
		resolved, resolveErr := app.taskSvc.ResolveProjectName(ctx, field.Value)

		if resolveErr != nil {
			return fmt.Errorf("resolving project %q: %w", field.Value, resolveErr)
		}

		upd.ProjectID = &resolved
	}

	// Parent (double pointer: empty string = clear parent)
	if field, ok := fs.GetField("parent"); ok {
		if field.Value == "" {
			var nilUUID *uuid.UUID
			upd.ParentID = &nilUUID
		} else {
			parentTask, parentErr := app.taskSvc.GetByShortID(ctx, field.Value)

			if parentErr != nil {
				return fmt.Errorf("%s", formatError(parentErr, field.Value))
			}

			parentID := parentTask.ID
			parentPtr := &parentID
			upd.ParentID = &parentPtr
		}
	}

	if validateErr := validateKnownFields(fs); validateErr != nil {
		return validateErr
	}
	udaMap, udaErr := collectUDAs(fs)

	if udaErr != nil {
		return udaErr
	}

	if udaMap != nil {
		upd.UDA = &udaMap
	}

	updated, updateErr := app.taskSvc.Update(ctx, upd)

	if updateErr != nil {
		if errors.Is(updateErr, domain.ErrTaxonomyViolation) {
			projectID := current.ProjectID
			if upd.ProjectID != nil {
				projectID = *upd.ProjectID
			}
			if msg, ok := formatTaxonomyError(updateErr, app.projectNameForTask(ctx, projectID)); ok {
				return fmt.Errorf("%s", msg)
			}
		}
		return fmt.Errorf("%s", formatError(updateErr, shortID))
	}

	// Add new tags
	incTags := fs.IncludeTags()
	if len(incTags) > 0 {
		if assignErr := app.tagSvc.AssignToTask(ctx, updated.ID, incTags); assignErr != nil {
			return fmt.Errorf("assigning tags: %w", assignErr)
		}
	}

	// Remove excluded tags
	excTags := fs.ExcludeTags()
	if len(excTags) > 0 {
		if removeErr := app.tagSvc.RemoveFromTask(ctx, updated.ID, excTags); removeErr != nil {
			return fmt.Errorf("removing tags: %w", removeErr)
		}
	}

	// Fetch tags for output
	modTags, modTagsErr := app.tagSvc.GetTaskTags(ctx, updated.ID)

	if modTagsErr != nil {
		return fmt.Errorf("loading tags: %w", modTagsErr)
	}

	renderer := app.newRenderer(cmd.Context(), cmd.OutOrStdout(), nil)
	return renderer.renderMutationResult("Modified", updated, modTags)
}

func (app *App) runStart(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	shortID := args[0]

	// Auto-register player if --player is set
	if app.playerID != "" {
		if err := app.ensurePlayer(ctx); err != nil {
			return err
		}
	}

	current, currentErr := app.taskSvc.GetByShortID(ctx, shortID)

	if currentErr != nil {
		return fmt.Errorf("%s", formatError(currentErr, shortID))
	}

	updated, updateErr := app.taskSvc.Start(ctx, shortID, current.Version, app.playerID)

	if updateErr != nil {
		if errors.Is(updateErr, domain.ErrTaskClaimed) {
			return fmt.Errorf("%s", formatClaimError(updateErr, shortID))
		}
		return fmt.Errorf("%s", formatError(updateErr, shortID))
	}

	renderer := app.newRenderer(cmd.Context(), cmd.OutOrStdout(), nil)
	return renderer.renderMutationResult("Started", updated, nil)
}

func (app *App) runDone(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	shortID := args[0]

	current, currentErr := app.taskSvc.GetByShortID(ctx, shortID)

	if currentErr != nil {
		return fmt.Errorf("%s", formatError(currentErr, shortID))
	}

	updated, updateErr := app.taskSvc.Complete(ctx, shortID, current.Version)

	if updateErr != nil {
		return fmt.Errorf("%s", formatError(updateErr, shortID))
	}

	renderer := app.newRenderer(cmd.Context(), cmd.OutOrStdout(), nil)
	return renderer.renderMutationResult("Completed", updated, nil)
}

func (app *App) runDelete(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	shortID := args[0]

	current, currentErr := app.taskSvc.GetByShortID(ctx, shortID)

	if currentErr != nil {
		return fmt.Errorf("%s", formatError(currentErr, shortID))
	}

	updated, updateErr := app.taskSvc.Delete(ctx, shortID, current.Version)

	if updateErr != nil {
		return fmt.Errorf("%s", formatError(updateErr, shortID))
	}

	renderer := app.newRenderer(cmd.Context(), cmd.OutOrStdout(), nil)
	return renderer.renderMutationResult("Deleted", updated, nil)
}

func (app *App) runAnnotate(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	shortID := args[0]
	body := strings.Join(args[1:], " ")

	var stdinFile *os.File
	if file, ok := cmd.InOrStdin().(*os.File); ok {
		stdinFile = file
	}
	expandedBody, expandBodyErr := app.expandRefs(body, stdinFile)

	if expandBodyErr != nil {
		return expandBodyErr
	}

	_, annotateErr := app.taskSvc.Annotate(ctx, shortID, expandedBody)

	if annotateErr != nil {
		return fmt.Errorf("%s", formatError(annotateErr, shortID))
	}

	task, taskErr := app.taskSvc.GetByShortID(ctx, shortID)

	if taskErr != nil {
		return fmt.Errorf("%s", formatError(taskErr, shortID))
	}

	renderer := app.newRenderer(cmd.Context(), cmd.OutOrStdout(), nil)
	return renderer.renderMutationResult("Annotated", task, nil)
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

func (app *App) runLink(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	sourceShortID := args[0]
	relType := args[1]
	targetShortID := args[2]

	rel, linkErr := app.relationSvc.Add(ctx, sourceShortID, targetShortID, relType)

	if linkErr != nil {
		return fmt.Errorf("%s", formatRelationError(linkErr, sourceShortID, targetShortID))
	}

	renderer := app.newRenderer(cmd.Context(), cmd.OutOrStdout(), nil)
	return renderer.renderLinkResult(rel, sourceShortID, targetShortID)
}

func (app *App) runUnlink(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	sourceShortID := args[0]
	relType := args[1]
	targetShortID := args[2]

	if err := app.relationSvc.Remove(ctx, sourceShortID, targetShortID, relType); err != nil {
		return fmt.Errorf("%s", formatRelationError(err, sourceShortID, targetShortID))
	}

	renderer := app.newRenderer(cmd.Context(), cmd.OutOrStdout(), nil)
	return renderer.renderUnlinkResult(sourceShortID, relType, targetShortID)
}

func (app *App) runClaim(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	shortID := args[0]

	if app.playerID == "" {
		return fmt.Errorf("--player flag is required for claim")
	}

	// Auto-register player if not already registered
	if err := app.ensurePlayer(ctx); err != nil {
		return err
	}

	current, currentErr := app.taskSvc.GetByShortID(ctx, shortID)

	if currentErr != nil {
		return fmt.Errorf("%s", formatError(currentErr, shortID))
	}

	updated, updateErr := app.taskSvc.Claim(ctx, shortID, app.playerID, current.Version)

	if updateErr != nil {
		return fmt.Errorf("%s", formatClaimError(updateErr, shortID))
	}

	renderer := app.newRenderer(cmd.Context(), cmd.OutOrStdout(), nil)
	return renderer.renderMutationResult("Claimed", updated, nil)
}

func (app *App) runRelease(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	shortID := args[0]

	if app.playerID == "" {
		return fmt.Errorf("--player flag is required for release")
	}

	current, currentErr := app.taskSvc.GetByShortID(ctx, shortID)

	if currentErr != nil {
		return fmt.Errorf("%s", formatError(currentErr, shortID))
	}

	updated, updateErr := app.taskSvc.Release(ctx, shortID, app.playerID, current.Version)

	if updateErr != nil {
		return fmt.Errorf("%s", formatClaimError(updateErr, shortID))
	}

	renderer := app.newRenderer(cmd.Context(), cmd.OutOrStdout(), nil)
	return renderer.renderMutationResult("Released", updated, nil)
}

// ensurePlayer auto-registers the current --player as "human" if not yet registered.
func (app *App) ensurePlayer(ctx context.Context) error {
	if app.playerSvc == nil || app.playerID == "" {
		return nil
	}
	_, err := app.playerSvc.GetByID(ctx, app.playerID)
	if err == nil {
		return nil // already registered
	}
	if errors.Is(err, domain.ErrNotFound) {
		_, regErr := app.playerSvc.Register(ctx, app.playerID, "human")
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

func (app *App) runAvailable(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	if app.playerID == "" {
		return fmt.Errorf("--player flag is required for available")
	}

	if err := app.ensurePlayer(ctx); err != nil {
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
		filterExpr, resolveErrs = app.resolver.ResolveExpr(ctx, expr)
		if len(resolveErrs) > 0 {
			return resolveErrs[0]
		}
	}

	tasks, listErr := app.taskSvc.Available(ctx, filterExpr)

	if listErr != nil {
		return listErr
	}

	// Fetch tags for all tasks in one query
	taskIDs := make([]uuid.UUID, len(tasks))
	for index, task := range tasks {
		taskIDs[index] = task.ID
	}
	tagsByTaskID, tagsErr := app.tagSvc.GetTaskTagsBatch(ctx, taskIDs)

	if tagsErr != nil {
		return fmt.Errorf("loading tags: %w", tagsErr)
	}

	taskTags := make(map[string][]*domain.Tag, len(tagsByTaskID))
	for taskID, tags := range tagsByTaskID {
		taskTags[taskID.String()] = tags
	}

	renderer := app.newRenderer(cmd.Context(), cmd.OutOrStdout(), app.buildDimStatuses())
	return renderer.renderTaskList(tasks, taskTags)
}

func (app *App) runPop(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	if app.playerID == "" {
		return fmt.Errorf("--player flag is required for pop")
	}

	if err := app.ensurePlayer(ctx); err != nil {
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
		filterExpr, resolveErrs = app.resolver.ResolveExpr(ctx, expr)
		if len(resolveErrs) > 0 {
			return resolveErrs[0]
		}
	}

	task, popErr := app.taskSvc.Pop(ctx, app.playerID, filterExpr)

	if popErr != nil {
		return fmt.Errorf("%s", formatError(popErr, ""))
	}

	// Load tags for the task
	tags, tagsErr := app.tagSvc.GetTaskTags(ctx, task.ID)

	if tagsErr != nil {
		return fmt.Errorf("loading tags: %w", tagsErr)
	}

	annotations, annotationsErr := app.taskSvc.GetAnnotations(ctx, task.ShortID)

	if annotationsErr != nil {
		return fmt.Errorf("loading annotations: %w", annotationsErr)
	}

	// Fetch and resolve relations
	var resolved []resolvedRelation
	if app.relationSvc != nil {
		rels, relErr := app.relationSvc.GetByTask(ctx, task.ShortID)

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
				if other, lookupErr := app.taskSvc.GetByID(ctx, rel.SourceID); lookupErr == nil {
					rr.RelatedShortID = other.ShortID
					rr.RelatedTitle = other.Title
				}
			} else {
				rr.Label = rel.RelationType
				if other, lookupErr := app.taskSvc.GetByID(ctx, rel.TargetID); lookupErr == nil {
					rr.RelatedShortID = other.ShortID
					rr.RelatedTitle = other.Title
				}
			}
			resolved = append(resolved, rr)
		}
	}

	renderer := app.newRenderer(cmd.Context(), cmd.OutOrStdout(), nil)
	return renderer.renderTaskInfo(task, annotations, tags, resolved)
}

// buildPlayerCmd creates the `tusk player` subcommand group.
func (app *App) buildPlayerCmd() *cobra.Command {
	registerCmd := &cobra.Command{
		Use:   "register <id>",
		Short: "Register a new player",
		Args:  cobra.ExactArgs(1),
		RunE:  app.runPlayerRegister,
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
		RunE: app.runPlayerModify,
	}

	playerCmd := &cobra.Command{
		Use:   "player",
		Short: "Player management",
	}
	playerCmd.AddCommand(registerCmd)
	playerCmd.AddCommand(modifyCmd)
	return playerCmd
}

func (app *App) runPlayerRegister(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	id := args[0]
	playerType, _ := cmd.Flags().GetString("type")

	player, registerErr := app.playerSvc.Register(ctx, id, playerType)

	if registerErr != nil {
		return fmt.Errorf("%s", registerErr)
	}

	renderer := app.newRenderer(cmd.Context(), cmd.OutOrStdout(), nil)
	return renderer.renderPlayerResult("Registered", player)
}

func (app *App) runPlayerModify(cmd *cobra.Command, args []string) error {
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

	for _, field := range fs.Fields {
		switch field.Key {
		case "note-window-size":
			if field.Modifier != 0 {
				return fmt.Errorf("note-window-size does not accept %q prefix", string(field.Modifier))
			}
			sawNoteWindow = true
			if field.Value == "" {
				newSize = nil
				continue
			}
			size, parseErr := strconv.Atoi(field.Value)

			if parseErr != nil {
				return fmt.Errorf("note-window-size must be an integer, got %q", field.Value)
			}

			if size <= 0 {
				return fmt.Errorf("note-window-size must be positive, got %d", size)
			}
			newSize = &size
		default:
			return fmt.Errorf("unknown field %q on player modify", field.Key)
		}
	}

	if !sawNoteWindow {
		return fmt.Errorf("no modifiable fields supplied")
	}

	player, updateErr := app.playerSvc.SetNoteWindowSize(ctx, id, newSize)

	if updateErr != nil {
		return fmt.Errorf("%s", updateErr)
	}

	renderer := app.newRenderer(cmd.Context(), cmd.OutOrStdout(), nil)
	return renderer.renderPlayerResult("Updated", player)
}
