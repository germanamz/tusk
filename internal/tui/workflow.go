package tui

import (
	"fmt"

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
	)

	return workflowCmd
}

func (a *App) runWorkflowList(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	workflows, err := a.workflowSvc.List(ctx)
	if err != nil {
		return err
	}
	return renderWorkflowList(cmd.OutOrStdout(), workflows, a.format)
}

func (a *App) runWorkflowInfo(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	name := args[0]

	wf, projectIDs, err := a.workflowSvc.GetWorkflowWithProjects(ctx, name)
	if err != nil {
		return fmt.Errorf("workflow %q: %w", name, err)
	}
	return renderWorkflowInfo(cmd.OutOrStdout(), wf, projectIDs, a.format)
}
