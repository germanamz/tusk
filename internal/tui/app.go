package tui

import (
	"context"
	"fmt"

	"github.com/germanamz/tusk/internal/domain"
	"github.com/germanamz/tusk/internal/filter"
	"github.com/germanamz/tusk/internal/service"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

// VersionInfo holds build-time version metadata injected via ldflags.
type VersionInfo struct {
	Version string
	Commit  string
	Date    string
}

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
	version     VersionInfo
}

// New creates a new App and builds the Cobra command tree.
// taskSvc, tagSvc, and projectRepo may be nil for testing command registration.
func New(taskSvc *service.TaskService, tagSvc *service.TagService, projectRepo ProjectLookup, vi VersionInfo) *App {
	a := &App{
		taskSvc:     taskSvc,
		tagSvc:      tagSvc,
		projectRepo: projectRepo,
		version:     vi,
	}
	a.resolver = filter.NewResolver(projectRepo, taskSvc)

	a.root = &cobra.Command{
		Use:           "tusk",
		Short:         "A concurrent-safe task management tool",
		Version:       vi.Version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	a.root.SetVersionTemplate(fmt.Sprintf("tusk %s (commit: %s, built: %s)\n", vi.Version, vi.Commit, vi.Date))
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
		&cobra.Command{
			Use:   "version",
			Short: "Print version information",
			Run: func(cmd *cobra.Command, args []string) {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "tusk %s (commit: %s, built: %s)\n", vi.Version, vi.Commit, vi.Date)
			},
		},
	)

	return a
}

// Run executes the Cobra command tree with the given arguments.
func (a *App) Run(args []string) error {
	a.root.SetArgs(args)
	return a.root.Execute()
}
