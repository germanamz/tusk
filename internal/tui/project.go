package tui

import (
	"context"
	"fmt"

	"github.com/germanamz/tusk/config"
	"github.com/germanamz/tusk/filter"
	"github.com/spf13/cobra"
)

// buildProjectCmd creates the `tusk project` command group.
func (a *App) buildProjectCmd() *cobra.Command {
	projectCmd := &cobra.Command{
		Use:   "project",
		Short: "Manage projects",
	}

	var force bool
	deleteCmd := &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete a project from config",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runProjectDelete(cmd, args, force)
		},
	}
	deleteCmd.Flags().BoolVar(&force, "force", false, "bypass task-reference and built-in guards")

	projectCmd.AddCommand(
		&cobra.Command{
			Use:   "list",
			Short: "List all projects",
			Args:  cobra.NoArgs,
			RunE:  a.runProjectList,
		},
		&cobra.Command{
			Use:   "create <name> [fields...]",
			Short: "Create a new project",
			Args:  cobra.MinimumNArgs(1),
			RunE:  a.runProjectCreate,
		},
		&cobra.Command{
			Use:   "modify <name> [fields...]",
			Short: "Modify an existing project",
			Args:  cobra.MinimumNArgs(2),
			RunE:  a.runProjectModify,
		},
		deleteCmd,
	)
	return projectCmd
}

func (a *App) runProjectList(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	projects, err := a.projectSvc.List(ctx)
	if err != nil {
		return err
	}
	r := NewRenderer(cmd.OutOrStdout(), a.format, a.colorEnabled(), nil)
	return r.renderProjectList(projects)
}

func (a *App) runProjectCreate(cmd *cobra.Command, args []string) error {
	name := args[0]
	proj, err := parseProjectCreate(args[1:])
	if err != nil {
		return err
	}
	path, err := config.ConfigFilePath(a.loadOpts...)
	if err != nil {
		return err
	}
	if err := config.CreateProject(path, name, proj); err != nil {
		return err
	}
	r := NewRenderer(cmd.OutOrStdout(), a.format, a.colorEnabled(), nil)
	return r.renderProjectMutation("Created", name)
}

func (a *App) runProjectModify(cmd *cobra.Command, args []string) error {
	name := args[0]
	mut, err := parseProjectModify(args[1:])
	if err != nil {
		return err
	}
	path, err := config.ConfigFilePath(a.loadOpts...)
	if err != nil {
		return err
	}
	if err := config.ModifyProject(path, name, mut); err != nil {
		return err
	}
	r := NewRenderer(cmd.OutOrStdout(), a.format, a.colorEnabled(), nil)
	return r.renderProjectMutation("Modified", name)
}

func (a *App) runProjectDelete(cmd *cobra.Command, args []string, force bool) error {
	name := args[0]
	path, err := config.ConfigFilePath(a.loadOpts...)
	if err != nil {
		return err
	}
	checker := func(projectName string) (int, error) {
		return a.countTasksForProject(cmd.Context(), projectName)
	}
	if err := config.DeleteProject(path, name, checker, force); err != nil {
		return err
	}
	r := NewRenderer(cmd.OutOrStdout(), a.format, a.colorEnabled(), nil)
	return r.renderProjectMutation("Deleted", name)
}

func (a *App) countTasksForProject(ctx context.Context, projectName string) (int, error) {
	expr, parseErrs := filter.ParseExpr(fmt.Sprintf("project=%s", projectName))
	if len(parseErrs) > 0 {
		return 0, fmt.Errorf("building filter: %s", filter.FormatErrors(parseErrs))
	}
	filterExpr, resolveErrs := a.resolver.ResolveExpr(ctx, expr)
	if len(resolveErrs) > 0 {
		return 0, resolveErrs[0]
	}
	tasks, err := a.taskSvc.List(ctx, filterExpr)
	if err != nil {
		return 0, err
	}
	return len(tasks), nil
}
