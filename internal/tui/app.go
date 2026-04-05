package tui

import (
	"fmt"

	"github.com/germanamz/tusk/internal/config"
	"github.com/germanamz/tusk/internal/filter"
	tuskmcp "github.com/germanamz/tusk/internal/mcp"
	"github.com/germanamz/tusk/internal/service"
	"github.com/spf13/cobra"
)

// VersionInfo holds build-time version metadata injected via ldflags.
type VersionInfo struct {
	Version string
	Commit  string
	Date    string
}

// App holds the CLI's dependencies and Cobra command tree.
type App struct {
	taskSvc     *service.TaskService
	tagSvc      *service.TagService
	relationSvc *service.RelationService
	projectSvc  *service.ProjectService
	workflowSvc *service.WorkflowService
	resolver    *filter.Resolver
	root        *cobra.Command
	format      string
	version     VersionInfo
	tuiCfg      config.TUIConfig
	mcpCfg      config.MCPConfig
}

// New creates a new App and builds the Cobra command tree.
// taskSvc, tagSvc, and projectSvc may be nil for testing command registration.
func New(taskSvc *service.TaskService, tagSvc *service.TagService, relationSvc *service.RelationService, projectSvc *service.ProjectService, workflowSvc *service.WorkflowService, vi VersionInfo, tuiCfg config.TUIConfig, mcpCfg config.MCPConfig) *App {
	a := &App{
		taskSvc:     taskSvc,
		tagSvc:      tagSvc,
		relationSvc: relationSvc,
		projectSvc:  projectSvc,
		workflowSvc: workflowSvc,
		version:     vi,
		tuiCfg:      tuiCfg,
		mcpCfg:      mcpCfg,
	}
	a.resolver = filter.NewResolver(taskSvc)

	a.root = &cobra.Command{
		Use:           "tusk",
		Short:         "A concurrent-safe task management tool",
		Version:       vi.Version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	a.root.SetVersionTemplate(fmt.Sprintf("tusk %s (commit: %s, built: %s)\n", vi.Version, vi.Commit, vi.Date))
	a.root.PersistentFlags().StringVar(&a.format, "format", "text", `output format: "text" or "json"`)

	treeCmd := &cobra.Command{
		Use:   "tree [short_id]",
		Short: "Display tasks as a tree hierarchy",
		Long:  "Show all tasks in a tree hierarchy. Optionally specify a short_id to show only that subtree.",
		Args:  cobra.MaximumNArgs(1),
		RunE:  a.runTree,
	}
	treeCmd.Flags().Bool("all", false, "include deleted tasks")

	addCmd := &cobra.Command{
		Use:   "add [title] [key:value...] [+tag...]",
		Short: "Create a new task",
		Args:  cobra.MinimumNArgs(1),
		RunE:  a.runAdd,
	}
	addCmd.Flags().StringP("description", "d", "", `task description (use @file to read from file, @- for stdin)`)

	a.root.AddCommand(
		addCmd,
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
		treeCmd,
		&cobra.Command{
			Use:   "link <short_id> <relation_type> <short_id>",
			Short: "Create a relation between two tasks",
			Long:  `Create a typed relation. Types: blocks, relates_to, duplicates.`,
			Args:  cobra.ExactArgs(3),
			RunE:  a.runLink,
		},
		&cobra.Command{
			Use:   "unlink <short_id> <relation_type> <short_id>",
			Short: "Remove a relation between two tasks",
			Long:  `Remove a typed relation. Types: blocks, relates_to, duplicates.`,
			Args:  cobra.ExactArgs(3),
			RunE:  a.runUnlink,
		},
		&cobra.Command{
			Use:   "version",
			Short: "Print version information",
			Run: func(cmd *cobra.Command, args []string) {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "tusk %s (commit: %s, built: %s)\n", vi.Version, vi.Commit, vi.Date)
			},
		},
	)

	a.root.AddCommand(a.buildProjectCmd())
	a.root.AddCommand(a.buildTagCmd())
	a.root.AddCommand(a.buildWorkflowCmd())

	mcpCmd := &cobra.Command{
		Use:   "mcp",
		Short: "MCP server commands",
	}
	mcpCmd.AddCommand(&cobra.Command{
		Use:   "serve",
		Short: "Start MCP server with stdio transport",
		RunE: func(cmd *cobra.Command, args []string) error {
			mcpServer, err := tuskmcp.New(taskSvc, tagSvc, relationSvc, projectSvc, a.workflowSvc, vi.Version, a.mcpCfg)
			if err != nil {
				return fmt.Errorf("initializing MCP server: %w", err)
			}
			return mcpServer.Serve()
		},
	})
	a.root.AddCommand(mcpCmd)

	return a
}

// Run executes the Cobra command tree with the given arguments.
func (a *App) Run(args []string) error {
	a.root.SetArgs(args)
	return a.root.Execute()
}
