package tui

import (
	"github.com/spf13/cobra"
)

// buildProjectCmd creates the `tusk project` command group.
// Projects are config-driven — only list is available.
func (a *App) buildProjectCmd() *cobra.Command {
	projectCmd := &cobra.Command{
		Use:   "project",
		Short: "Manage projects",
	}

	projectCmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List all projects",
		Args:  cobra.NoArgs,
		RunE:  a.runProjectList,
	})

	return projectCmd
}

func (a *App) runProjectList(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	projects, err := a.projectSvc.List(ctx)
	if err != nil {
		return err
	}
	r := NewRenderer(cmd.OutOrStdout(), a.format, a.colorEnabled())
	return r.renderProjectList(projects)
}
