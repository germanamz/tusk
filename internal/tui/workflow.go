package tui

import (
	"errors"
	"fmt"

	"github.com/germanamz/tusk/domain"
	"github.com/spf13/cobra"
)

// buildWorkflowCmd creates the `tusk workflow` command group.
func (app *App) buildWorkflowCmd() *cobra.Command {
	workflowCmd := &cobra.Command{
		Use:   "workflow",
		Short: "Manage workflows",
	}

	workflowCmd.AddCommand(
		&cobra.Command{
			Use:   "list",
			Short: "List all workflows",
			Args:  cobra.NoArgs,
			RunE:  app.runWorkflowList,
		},
		&cobra.Command{
			Use:   "info <name>",
			Short: "Show workflow details",
			Args:  cobra.ExactArgs(1),
			RunE:  app.runWorkflowInfo,
		},
		&cobra.Command{
			Use:   "create <name> [fields...]",
			Short: "Create a new workflow",
			Long: `Create a new workflow with statuses and transitions defined inline.

  status=<name>(<roles>)        Define a status with optional roles
  transition=<from>:<to>        Define a transition (comma-separated for multiple)

Roles: initial, start, terminal, done, delete, highlight, dim
Constraints: exactly one initial, one start, at least one terminal.`,
			Example: `  tusk workflow create sprint \
    status=backlog(initial) \
    status=doing(start,highlight) \
    status=done(terminal,done,dim) \
    status=cancelled(terminal,delete,dim) \
    transition=backlog:doing,doing:done,doing:cancelled`,
			Args: cobra.MinimumNArgs(1),
			RunE: app.runWorkflowCreate,
		},
		&cobra.Command{
			Use:   "modify <name> [fields...]",
			Short: "Modify an existing workflow",
			Long: `Modify an existing workflow. Bare assignment replaces; +/- prefixes add or
remove list entries.

  status=<name>(<roles>)        Replace roles on existing status
  +status=<name>(<roles>)       Add a new status
  -status=<name>                Remove a status
  +transition=<from>:<to>       Add transitions
  -transition=<from>:<to>       Remove transitions`,
			Example: `  # Add a review status and transitions
  tusk workflow modify sprint +status=review +transition=doing:review,review:done

  # Update roles on an existing status
  tusk workflow modify sprint status=doing(start,highlight)

  # Remove a status and its transitions
  tusk workflow modify sprint -status=review -transition=doing:review,review:done`,
			Args: cobra.MinimumNArgs(1),
			RunE: app.runWorkflowModify,
		},
		&cobra.Command{
			Use:   "delete <name>",
			Short: "Delete a workflow",
			Args:  cobra.ExactArgs(1),
			RunE:  app.runWorkflowDelete,
		},
	)

	return workflowCmd
}

func (app *App) runWorkflowList(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	workflows, err := app.workflowSvc.List(ctx)

	if err != nil {
		return err
	}

	workflowProjects := make(map[string][]string, len(workflows))
	for _, workflow := range workflows {
		_, projectIDs, err := app.workflowSvc.GetWorkflowWithProjects(ctx, workflow.Name)

		if err != nil {
			return err
		}

		workflowProjects[workflow.Name] = projectIDs
	}

	renderer := NewRenderer(cmd.OutOrStdout(), app.format, app.colorEnabled(), nil)
	return renderer.renderWorkflowList(workflows, workflowProjects)
}

func (app *App) runWorkflowInfo(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	name := args[0]

	workflow, projectIDs, err := app.workflowSvc.GetWorkflowWithProjects(ctx, name)

	if err != nil {
		return fmt.Errorf("workflow %q: %w", name, err)
	}

	renderer := NewRenderer(cmd.OutOrStdout(), app.format, app.colorEnabled(), nil)
	return renderer.renderWorkflowInfo(workflow, projectIDs)
}

func (app *App) runWorkflowCreate(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	name := args[0]
	if len(args) < 2 {
		return fmt.Errorf("at least one status definition is required")
	}

	input, inputErr := parseWorkflowCreate(args[1:])

	if inputErr != nil {
		return inputErr
	}

	input.Name = name

	if _, createErr := app.workflowSvc.Create(ctx, input); createErr != nil {
		return createErr
	}

	renderer := NewRenderer(cmd.OutOrStdout(), app.format, app.colorEnabled(), nil)
	return renderer.renderWorkflowMutation("Created", name)
}

func (app *App) runWorkflowModify(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	name := args[0]
	if len(args) < 2 {
		return fmt.Errorf("at least one modification is required")
	}

	input, inputErr := parseWorkflowModify(args[1:])

	if inputErr != nil {
		return inputErr
	}

	current, currentErr := app.workflowSvc.GetByName(ctx, name)

	if currentErr != nil {
		return currentErr
	}

	input.Name = name
	input.ExpectedVersion = current.Version

	if _, modifyErr := app.workflowSvc.Modify(ctx, input); modifyErr != nil {
		return modifyErr
	}

	renderer := NewRenderer(cmd.OutOrStdout(), app.format, app.colorEnabled(), nil)
	return renderer.renderWorkflowMutation("Modified", name)
}

func (app *App) runWorkflowDelete(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	name := args[0]

	workflow, err := app.workflowSvc.GetByName(ctx, name)

	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return fmt.Errorf("workflow %q: not found", name)
		}
		return err
	}

	if deleteErr := app.workflowSvc.Delete(ctx, workflow.ID, workflow.Version); deleteErr != nil {
		return deleteErr
	}

	renderer := NewRenderer(cmd.OutOrStdout(), app.format, app.colorEnabled(), nil)
	return renderer.renderWorkflowMutation("Deleted", name)
}
