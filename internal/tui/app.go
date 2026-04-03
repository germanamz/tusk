package tui

import (
	"context"

	"github.com/germanamz/tusk/internal/domain"
	"github.com/germanamz/tusk/internal/filter"
	"github.com/germanamz/tusk/internal/service"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

// ProjectLookup is the subset of project operations the TUI needs.
type ProjectLookup interface {
	GetByID(ctx context.Context, id uuid.UUID) (*domain.Project, error)
	GetByName(ctx context.Context, name string) (*domain.Project, error)
}

// App holds the CLI's dependencies and Cobra command tree.
type App struct {
	taskSvc     *service.TaskService
	tagSvc      *service.TagService
	projectRepo ProjectLookup
	resolver    *filter.Resolver
	root        *cobra.Command
	format      string
}

// New creates a new App and builds the Cobra command tree.
// taskSvc, tagSvc, and projectRepo may be nil for testing command registration.
func New(taskSvc *service.TaskService, tagSvc *service.TagService, projectRepo ProjectLookup) *App {
	a := &App{
		taskSvc:     taskSvc,
		tagSvc:      tagSvc,
		projectRepo: projectRepo,
	}
	a.resolver = filter.NewResolver(projectRepo, taskSvc)

	a.root = &cobra.Command{
		Use:   "tusk",
		Short: "A concurrent-safe task management tool",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	a.root.PersistentFlags().StringVar(&a.format, "format", "text", `output format: "text" or "json"`)

	a.root.AddCommand(
		&cobra.Command{
			Use:   "add [title] [key:value...] [+tag...]",
			Short: "Create a new task",
			Args:  cobra.MinimumNArgs(1),
			RunE:  a.runAdd,
		},
		&cobra.Command{
			Use:   "list [filters...]",
			Short: "List tasks",
			RunE:  a.runList,
		},
		&cobra.Command{
			Use:   "info <short_id>",
			Short: "Show task details",
			Args:  cobra.ExactArgs(1),
			RunE:  a.runInfo,
		},
		&cobra.Command{
			Use:   "modify <short_id> [key:value...]",
			Short: "Modify a task",
			Args:  cobra.MinimumNArgs(1),
			RunE:  a.runModify,
		},
		&cobra.Command{
			Use:   "start <short_id>",
			Short: "Transition task to active",
			Args:  cobra.ExactArgs(1),
			RunE:  a.runStart,
		},
		&cobra.Command{
			Use:   "done <short_id>",
			Short: "Transition task to completed",
			Args:  cobra.ExactArgs(1),
			RunE:  a.runDone,
		},
		&cobra.Command{
			Use:   "delete <short_id>",
			Short: "Transition task to deleted",
			Args:  cobra.ExactArgs(1),
			RunE:  a.runDelete,
		},
		&cobra.Command{
			Use:   "annotate <short_id> <message...>",
			Short: "Add a note to a task",
			Args:  cobra.MinimumNArgs(2),
			RunE:  a.runAnnotate,
		},
	)

	return a
}

// Run executes the Cobra command tree with the given arguments.
func (a *App) Run(args []string) error {
	a.root.SetArgs(args)
	return a.root.Execute()
}
