package tui

import (
	"errors"
	"fmt"

	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/service"
	"github.com/google/uuid"
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
		Short: "Delete a project",
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
	workflows, err := a.workflowSvc.List(ctx)
	if err != nil {
		return err
	}
	wfNames := make(map[uuid.UUID]string, len(workflows))
	for _, wf := range workflows {
		wfNames[wf.ID] = wf.Name
	}
	r := NewRenderer(cmd.OutOrStdout(), a.format, a.colorEnabled(), nil)
	return r.renderProjectList(projects, wfNames)
}

func (a *App) runProjectCreate(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	name := args[0]
	parsed, err := parseProjectCreate(args[1:])
	if err != nil {
		return err
	}
	if parsed.Workflow == "" {
		return fmt.Errorf("project create requires workflow=<name>")
	}
	wf, err := a.workflowSvc.GetByName(ctx, parsed.Workflow)
	if err != nil {
		return fmt.Errorf("resolving workflow %q: %w", parsed.Workflow, err)
	}
	if _, err := a.projectSvc.Create(ctx, service.CreateProjectInput{
		Name:       name,
		WorkflowID: wf.ID,
		Settings:   parsed.Settings,
	}); err != nil {
		return err
	}
	r := NewRenderer(cmd.OutOrStdout(), a.format, a.colorEnabled(), nil)
	return r.renderProjectMutation("Created", name)
}

func (a *App) runProjectModify(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	name := args[0]
	parsed, err := parseProjectModify(args[1:])
	if err != nil {
		return err
	}
	current, err := a.projectSvc.GetByName(ctx, name)
	if err != nil {
		return err
	}
	input := service.ModifyProjectInput{
		Name:            name,
		ExpectedVersion: current.Version,
		AutoComplete:    parsed.AutoComplete,
		AutoRevert:      parsed.AutoRevert,
		Urgency: service.UrgencyMutation{
			Set:   parsed.UrgencySet,
			Delta: parsed.UrgencyDelta,
		},
	}
	if parsed.Workflow != nil {
		wf, err := a.workflowSvc.GetByName(ctx, *parsed.Workflow)
		if err != nil {
			return fmt.Errorf("resolving workflow %q: %w", *parsed.Workflow, err)
		}
		id := wf.ID
		input.WorkflowID = &id
	}
	if _, err := a.projectSvc.Modify(ctx, input); err != nil {
		return err
	}
	r := NewRenderer(cmd.OutOrStdout(), a.format, a.colorEnabled(), nil)
	return r.renderProjectMutation("Modified", name)
}

func (a *App) runProjectDelete(cmd *cobra.Command, args []string, force bool) error {
	ctx := cmd.Context()
	name := args[0]
	p, err := a.projectSvc.GetByName(ctx, name)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return fmt.Errorf("project %q: not found", name)
		}
		return err
	}
	if err := a.projectSvc.Delete(ctx, p.ID, p.Version, force); err != nil {
		return err
	}
	r := NewRenderer(cmd.OutOrStdout(), a.format, a.colorEnabled(), nil)
	return r.renderProjectMutation("Deleted", name)
}
