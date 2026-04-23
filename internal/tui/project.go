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
			Use:   "show <name>",
			Short: "Show a project's workflow, settings, and effective taxonomy",
			Long: `Display a single project in full detail, including the effective taxonomy
and provenance source (workspace default vs. project override).`,
			Args: cobra.ExactArgs(1),
			RunE: a.runProjectShow,
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
			RunE: a.runProjectCreate,
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
			RunE: a.runProjectModify,
		},
		deleteCmd,
	)
	return projectCmd
}

func (a *App) runProjectShow(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	name := args[0]
	p, err := a.projectSvc.GetByName(ctx, name)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return fmt.Errorf("project %q: not found", name)
		}
		return err
	}
	wf, err := a.workflowSvc.GetByID(ctx, p.WorkflowID)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return err
	}
	workflowName := ""
	if wf != nil {
		workflowName = wf.Name
	}
	taxonomy, source := a.projectSvc.EffectiveTaxonomy(p)
	r := NewRenderer(cmd.OutOrStdout(), a.format, a.colorEnabled(), nil)
	return r.renderProjectShow(p, workflowName, taxonomy, source)
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

	var stdinFile *os.File
	if f, ok := cmd.InOrStdin().(*os.File); ok {
		stdinFile = f
	}
	expanded, err := expandTaxonomyRefs(args[1:], stdinFile, a.inlineCfg.MaxExpansionSize)
	if err != nil {
		return err
	}

	parsed, err := parseProjectModify(expanded)
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
	if _, err := a.projectSvc.Modify(ctx, input); err != nil {
		return err
	}
	r := NewRenderer(cmd.OutOrStdout(), a.format, a.colorEnabled(), nil)
	return r.renderProjectMutation("Modified", name)
}

// expandTaxonomyRefs pre-processes `taxonomy=@...` arguments so that
// parseProjectModify receives the JSON body inline. The @ reference expander
// only treats `@` at a word boundary; the value portion of a field always
// starts at position 0 (a boundary), so splitting on the first `=` lets us
// reuse the shared expander. Other arguments pass through untouched.
func expandTaxonomyRefs(args []string, stdin *os.File, maxSize int64) ([]string, error) {
	out := make([]string, len(args))
	state := &expandState{}
	for i, arg := range args {
		key, value, ok := strings.Cut(arg, "=")
		if !ok || key != "taxonomy" || value == "" || value[0] != '@' {
			out[i] = arg
			continue
		}
		expanded, err := expandRefsWithState(value, stdin, maxSize, state)
		if err != nil {
			return nil, err
		}
		out[i] = "taxonomy=" + expanded
	}
	return out, nil
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
