package tui

import (
	"errors"
	"fmt"

	"github.com/germanamz/tusk/domain"
	"github.com/spf13/cobra"
)

// buildWorkflowCmd creates the `tusk workflow` command group.
func (a *App) buildWorkflowCmd() *cobra.Command {
	workflowCmd := &cobra.Command{
		Use:   "workflow",
		Short: "Manage workflows",
	}

	workflowCmd.AddCommand(
		&cobra.Command{
			Use:   "list",
			Short: "List all workflows",
			Args:  cobra.NoArgs,
			RunE:  a.runWorkflowList,
		},
		&cobra.Command{
			Use:   "info <name>",
			Short: "Show workflow details",
			Args:  cobra.ExactArgs(1),
			RunE:  a.runWorkflowInfo,
		},
		&cobra.Command{
			Use:   "create <name> [fields...]",
			Short: "Create a new workflow",
			Args:  cobra.MinimumNArgs(1),
			RunE:  a.runWorkflowCreate,
		},
		&cobra.Command{
			Use:   "modify <name> [fields...]",
			Short: "Modify an existing workflow",
			Args:  cobra.MinimumNArgs(1),
			RunE:  a.runWorkflowModify,
		},
		&cobra.Command{
			Use:   "delete <name>",
			Short: "Delete a workflow",
			Args:  cobra.ExactArgs(1),
			RunE:  a.runWorkflowDelete,
		},
	)

	return workflowCmd
}

func (a *App) runWorkflowList(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	workflows, err := a.workflowSvc.List(ctx)
	if err != nil {
		return err
	}

	workflowProjects := make(map[string][]string, len(workflows))
	for _, wf := range workflows {
		_, projectIDs, err := a.workflowSvc.GetWorkflowWithProjects(ctx, wf.Name)
		if err != nil {
			return err
		}
		workflowProjects[wf.Name] = projectIDs
	}

	r := NewRenderer(cmd.OutOrStdout(), a.format, a.colorEnabled(), nil)
	return r.renderWorkflowList(workflows, workflowProjects)
}

func (a *App) runWorkflowInfo(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	name := args[0]

	wf, projectIDs, err := a.workflowSvc.GetWorkflowWithProjects(ctx, name)
	if err != nil {
		return fmt.Errorf("workflow %q: %w", name, err)
	}
	r := NewRenderer(cmd.OutOrStdout(), a.format, a.colorEnabled(), nil)
	return r.renderWorkflowInfo(wf, projectIDs)
}

func (a *App) runWorkflowCreate(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	name := args[0]
	if len(args) < 2 {
		return fmt.Errorf("at least one status definition is required")
	}
	input, err := parseWorkflowCreate(args[1:])
	if err != nil {
		return err
	}
	input.Name = name
	if _, err := a.workflowSvc.Create(ctx, input); err != nil {
		return err
	}
	r := NewRenderer(cmd.OutOrStdout(), a.format, a.colorEnabled(), nil)
	return r.renderWorkflowMutation("Created", name)
}

func (a *App) runWorkflowModify(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	name := args[0]
	if len(args) < 2 {
		return fmt.Errorf("at least one modification is required")
	}
	input, err := parseWorkflowModify(args[1:])
	if err != nil {
		return err
	}
	current, err := a.workflowSvc.GetByName(ctx, name)
	if err != nil {
		return err
	}
	input.Name = name
	input.ExpectedVersion = current.Version
	if _, err := a.workflowSvc.Modify(ctx, input); err != nil {
		return err
	}
	r := NewRenderer(cmd.OutOrStdout(), a.format, a.colorEnabled(), nil)
	return r.renderWorkflowMutation("Modified", name)
}

func (a *App) runWorkflowDelete(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	name := args[0]
	wf, err := a.workflowSvc.GetByName(ctx, name)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return fmt.Errorf("workflow %q: not found", name)
		}
		return err
	}
	if err := a.workflowSvc.Delete(ctx, wf.ID, wf.Version); err != nil {
		return err
	}
	r := NewRenderer(cmd.OutOrStdout(), a.format, a.colorEnabled(), nil)
	return r.renderWorkflowMutation("Deleted", name)
}
