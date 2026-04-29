package tui

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/service"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

// buildProjectCmd creates the `tusk project` command group.
func (app *App) buildProjectCmd() *cobra.Command {
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
			return app.runProjectDelete(cmd, args, force)
		},
	}
	deleteCmd.Flags().BoolVar(&force, "force", false, "bypass task-reference and built-in guards")

	projectCmd.AddCommand(
		&cobra.Command{
			Use:   "list",
			Short: "List all projects",
			Args:  cobra.NoArgs,
			RunE:  app.runProjectList,
		},
		&cobra.Command{
			Use:   "show <name>",
			Short: "Show a project's workflow, settings, and effective taxonomy",
			Long: `Display a single project in full detail, including the effective taxonomy
and provenance source (workspace default vs. project override).`,
			Args: cobra.ExactArgs(1),
			RunE: app.runProjectShow,
		},
		&cobra.Command{
			Use:   "create <name> [fields...]",
			Short: "Create a new project",
			Long: `Create a new project. Fields are set via inline key=value syntax.

Accepted fields:
  workflow=<name>                 Workflow to use (must already exist)
  auto-complete.trigger=<status>  Status that triggers parent auto-complete
  auto-complete.target=<status>   Status to set on auto-completed parent
  auto-revert.trigger=<status>    Status that triggers parent auto-revert
  auto-revert.target=<status>     Status to set on auto-reverted parent
  urgency.<weight>=<value>        Override a global urgency weight`,
			Example: `  tusk project create backend workflow=kanban
  tusk project create ops workflow=kanban urgency.blocking-weight=15 auto-complete.trigger=completed`,
			Args: cobra.MinimumNArgs(1),
			RunE: app.runProjectCreate,
		},
		&cobra.Command{
			Use:   "modify <name> [fields...]",
			Short: "Modify an existing project",
			Long: `Modify an existing project. Bare assignment replaces the value; +/- prefixes on
numeric urgency weights apply arithmetic deltas relative to the effective value.

  workflow=sprint                Replace workflow
  urgency.blocking-weight=10    Set absolute override
  +urgency.blocking-weight=2    Add 2 to effective weight
  -urgency.blocking-weight=1    Subtract 1 from effective weight`,
			Example: `  tusk project modify backend workflow=sprint
  tusk project modify backend +urgency.blocking-weight=2
  tusk project modify backend auto-complete.trigger=completed auto-complete.target=completed`,
			Args: cobra.MinimumNArgs(2),
			RunE: app.runProjectModify,
		},
		deleteCmd,
	)
	return projectCmd
}

func (app *App) runProjectShow(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	name := args[0]

	project, projectErr := app.projectSvc.GetByName(ctx, name)

	if projectErr != nil {
		if errors.Is(projectErr, domain.ErrNotFound) {
			return fmt.Errorf("project %q: not found", name)
		}
		return projectErr
	}

	workflow, workflowErr := app.workflowSvc.GetByID(ctx, project.WorkflowID)

	if workflowErr != nil && !errors.Is(workflowErr, domain.ErrNotFound) {
		return workflowErr
	}

	workflowName := ""
	if workflow != nil {
		workflowName = workflow.Name
	}
	taxonomy, source := app.projectSvc.EffectiveTaxonomy(project)
	renderer := NewRenderer(cmd.OutOrStdout(), app.format, app.colorEnabled(), nil)
	return renderer.renderProjectShow(project, workflowName, taxonomy, source)
}

func (app *App) runProjectList(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	projects, projectsErr := app.projectSvc.List(ctx)

	if projectsErr != nil {
		return projectsErr
	}

	workflows, workflowsErr := app.workflowSvc.List(ctx)

	if workflowsErr != nil {
		return workflowsErr
	}

	wfNames := make(map[uuid.UUID]string, len(workflows))
	for _, workflow := range workflows {
		wfNames[workflow.ID] = workflow.Name
	}
	renderer := NewRenderer(cmd.OutOrStdout(), app.format, app.colorEnabled(), nil)
	return renderer.renderProjectList(projects, wfNames)
}

func (app *App) runProjectCreate(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	name := args[0]

	parsed, parseErr := parseProjectCreate(args[1:])

	if parseErr != nil {
		return parseErr
	}

	if parsed.Workflow == "" {
		return fmt.Errorf("project create requires workflow=<name>")
	}
	var stdinFile *os.File
	if stdinAsFile, ok := cmd.InOrStdin().(*os.File); ok {
		stdinFile = stdinAsFile
	}
	if parsed.Description != "" {
		expanded, expandErr := app.expandRefs(parsed.Description, stdinFile)

		if expandErr != nil {
			return fmt.Errorf("description: %w", expandErr)
		}

		parsed.Description = expanded
	}

	workflow, workflowErr := app.workflowSvc.GetByName(ctx, parsed.Workflow)

	if workflowErr != nil {
		return fmt.Errorf("resolving workflow %q: %w", parsed.Workflow, workflowErr)
	}

	if _, createErr := app.projectSvc.Create(ctx, service.CreateProjectInput{
		Name:        name,
		WorkflowID:  workflow.ID,
		Description: parsed.Description,
		Settings:    parsed.Settings,
	}); createErr != nil {
		return createErr
	}
	renderer := NewRenderer(cmd.OutOrStdout(), app.format, app.colorEnabled(), nil)
	return renderer.renderProjectMutation("Created", name)
}

func (app *App) runProjectModify(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	name := args[0]

	var stdinFile *os.File
	if stdinAsFile, ok := cmd.InOrStdin().(*os.File); ok {
		stdinFile = stdinAsFile
	}

	expanded, expandErr := expandProjectFieldRefs(args[1:], stdinFile, app.inlineCfg.MaxExpansionSize)

	if expandErr != nil {
		return expandErr
	}

	parsed, parseErr := parseProjectModify(expanded)

	if parseErr != nil {
		return parseErr
	}

	if parsed.Description != nil && *parsed.Description != nil && **parsed.Description != "" {
		state := &expandState{}

		expandedDesc, descErr := app.expandRefsWithState(**parsed.Description, stdinFile, state)

		if descErr != nil {
			return fmt.Errorf("description: %w", descErr)
		}

		inner := &expandedDesc
		parsed.Description = &inner
	}

	current, lookupErr := app.projectSvc.GetByName(ctx, name)

	if lookupErr != nil {
		return lookupErr
	}

	input := service.ModifyProjectInput{
		Name:            name,
		ExpectedVersion: current.Version,
		Description:     parsed.Description,
		AutoComplete:    parsed.AutoComplete,
		AutoRevert:      parsed.AutoRevert,
		Urgency: service.UrgencyMutation{
			Set:   parsed.UrgencySet,
			Delta: parsed.UrgencyDelta,
		},
	}
	if parsed.Workflow != nil {
		workflow, workflowErr := app.workflowSvc.GetByName(ctx, *parsed.Workflow)

		if workflowErr != nil {
			return fmt.Errorf("resolving workflow %q: %w", *parsed.Workflow, workflowErr)
		}

		wfID := workflow.ID
		input.WorkflowID = &wfID
	}
	switch parsed.TaxonomyAction {
	case taxonomyActionNone:
		// no change
	case taxonomyActionClear:
		input.Taxonomy = &service.TaxonomyMutation{Clear: true}
	case taxonomyActionEmpty:
		input.Taxonomy = &service.TaxonomyMutation{Value: domain.Taxonomy{}}
	case taxonomyActionSet:
		input.Taxonomy = &service.TaxonomyMutation{Value: parsed.TaxonomyValue}
	}

	if _, modifyErr := app.projectSvc.Modify(ctx, input); modifyErr != nil {
		return modifyErr
	}

	renderer := NewRenderer(cmd.OutOrStdout(), app.format, app.colorEnabled(), nil)
	return renderer.renderProjectMutation("Modified", name)
}

// expandProjectFieldRefs pre-processes `taxonomy=@...` arguments so that the
// modify parser receives the JSON body inline. Description references are not
// expanded here — their content can contain whitespace which the lexer breaks
// on; descriptions are expanded after parsing instead.
func expandProjectFieldRefs(args []string, stdin *os.File, maxSize int64) ([]string, error) {
	out := make([]string, len(args))
	state := &expandState{}
	for index, arg := range args {
		key, value, ok := strings.Cut(arg, "=")
		if !ok || key != "taxonomy" || value == "" || value[0] != '@' {
			out[index] = arg
			continue
		}

		expanded, expandErr := expandRefsWithState(value, stdin, maxSize, state)

		if expandErr != nil {
			return nil, expandErr
		}

		out[index] = key + "=" + expanded
	}
	return out, nil
}

func (app *App) runProjectDelete(cmd *cobra.Command, args []string, force bool) error {
	ctx := cmd.Context()
	name := args[0]

	project, lookupErr := app.projectSvc.GetByName(ctx, name)

	if lookupErr != nil {
		if errors.Is(lookupErr, domain.ErrNotFound) {
			return fmt.Errorf("project %q: not found", name)
		}
		return lookupErr
	}

	if deleteErr := app.projectSvc.Delete(ctx, project.ID, project.Version, force); deleteErr != nil {
		return deleteErr
	}

	renderer := NewRenderer(cmd.OutOrStdout(), app.format, app.colorEnabled(), nil)
	return renderer.renderProjectMutation("Deleted", name)
}
