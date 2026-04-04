package tui

import (
	"fmt"
	"strings"

	"github.com/germanamz/tusk/internal/service"
	"github.com/spf13/cobra"
)

// buildProjectCmd creates the `tusk project` command group with its subcommands.
func (a *App) buildProjectCmd() *cobra.Command {
	projectCmd := &cobra.Command{
		Use:   "project",
		Short: "Manage projects",
	}

	// tusk project list
	projectCmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List all projects",
		Args:  cobra.NoArgs,
		RunE:  a.runProjectList,
	})

	// tusk project create <name>
	createCmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a new project",
		Args:  cobra.ExactArgs(1),
		RunE:  a.runProjectCreate,
	}
	createCmd.Flags().StringP("description", "d", "", "project description")
	projectCmd.AddCommand(createCmd)

	// tusk project modify <name>
	modifyCmd := &cobra.Command{
		Use:   "modify <name>",
		Short: "Modify a project",
		Args:  cobra.ExactArgs(1),
		RunE:  a.runProjectModify,
	}
	modifyCmd.Flags().StringP("description", "d", "", "new description")
	modifyCmd.Flags().StringArray("set", nil, "set a settings value (dot-path=value)")
	modifyCmd.Flags().StringArray("unset", nil, "unset a settings key (dot-path)")
	projectCmd.AddCommand(modifyCmd)

	return projectCmd
}

func (a *App) runProjectList(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	projects, err := a.projectSvc.List(ctx)
	if err != nil {
		return err
	}
	return renderProjectList(cmd.OutOrStdout(), projects, a.format)
}

func (a *App) runProjectCreate(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	name := args[0]
	description, _ := cmd.Flags().GetString("description")

	project, err := a.projectSvc.Create(ctx, name, description)
	if err != nil {
		return err
	}
	return renderProjectResult(cmd.OutOrStdout(), "Created", project, a.format)
}

func (a *App) runProjectModify(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	name := args[0]

	opts := service.ModifyOptions{}

	if cmd.Flags().Changed("description") {
		desc, _ := cmd.Flags().GetString("description")
		opts.Description = &desc
	}

	setFlags, _ := cmd.Flags().GetStringArray("set")
	if len(setFlags) > 0 {
		opts.Sets = make(map[string]string, len(setFlags))
		for _, s := range setFlags {
			parts := strings.SplitN(s, "=", 2)
			if len(parts) != 2 {
				return fmt.Errorf("invalid --set format %q: expected key=value", s)
			}
			opts.Sets[parts[0]] = parts[1]
		}
	}

	unsetFlags, _ := cmd.Flags().GetStringArray("unset")
	if len(unsetFlags) > 0 {
		opts.Unsets = unsetFlags
	}

	project, err := a.projectSvc.Modify(ctx, name, opts)
	if err != nil {
		return err
	}
	return renderProjectResult(cmd.OutOrStdout(), "Modified", project, a.format)
}
