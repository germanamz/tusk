package tui

import (
	"context"
	"fmt"
	"os"

	"github.com/germanamz/tusk/config"
	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/filter"
	"github.com/germanamz/tusk/inmem"
	tuskmcp "github.com/germanamz/tusk/internal/mcp"
	"github.com/germanamz/tusk/service"
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
	taskSvc       *service.TaskService
	tagSvc        *service.TagService
	relationSvc   *service.RelationService
	projectSvc    *service.ProjectService
	workflowSvc   *service.WorkflowService
	playerSvc     *service.PlayerService
	workflowRepo  *inmem.WorkflowRepository
	projectRepo   *inmem.ProjectRepository
	urgencyEngine *service.UrgencyEngine
	playerID      string // from --player flag
	resolver      *filter.Resolver
	root          *cobra.Command
	format        string
	noColor       bool
	version       VersionInfo
	tuiCfg        config.TUIConfig
	mcpCfg        config.MCPConfig
	loadOpts      []config.Option
}

// colorEnabled resolves whether color output is active.
// Precedence: --no-color flag > NO_COLOR env > tui.color config.
func (a *App) colorEnabled() bool {
	if a.noColor {
		return false
	}
	if _, ok := os.LookupEnv("NO_COLOR"); ok {
		return false
	}
	return a.tuiCfg.Color
}

// New creates a new App and builds the Cobra command tree.
// taskSvc, tagSvc, and projectSvc may be nil for testing command registration.
func New(
	taskSvc *service.TaskService,
	tagSvc *service.TagService,
	relationSvc *service.RelationService,
	projectSvc *service.ProjectService,
	workflowSvc *service.WorkflowService,
	playerSvc *service.PlayerService,
	workflowRepo *inmem.WorkflowRepository,
	projectRepo *inmem.ProjectRepository,
	urgencyEngine *service.UrgencyEngine,
	vi VersionInfo,
	tuiCfg config.TUIConfig,
	mcpCfg config.MCPConfig,
	loadOpts []config.Option,
) *App {
	a := &App{
		taskSvc:       taskSvc,
		tagSvc:        tagSvc,
		relationSvc:   relationSvc,
		projectSvc:    projectSvc,
		workflowSvc:   workflowSvc,
		playerSvc:     playerSvc,
		workflowRepo:  workflowRepo,
		projectRepo:   projectRepo,
		urgencyEngine: urgencyEngine,
		version:       vi,
		tuiCfg:        tuiCfg,
		mcpCfg:        mcpCfg,
		loadOpts:      loadOpts,
	}
	a.resolver = filter.NewResolver(taskSvc, collectNonTerminalStatuses(workflowSvc))

	a.root = &cobra.Command{
		Use:           "tusk",
		Short:         "A concurrent-safe task management tool",
		Version:       vi.Version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	a.root.SetVersionTemplate(fmt.Sprintf("tusk %s (commit: %s, built: %s)\n", vi.Version, vi.Commit, vi.Date))
	a.root.PersistentFlags().StringVar(&a.format, "format", "text", `output format: "text" or "json"`)
	a.root.PersistentFlags().BoolVar(&a.noColor, "no-color", false, "disable colored output")
	a.root.PersistentFlags().StringVar(&a.playerID, "player", "", "player ID for claim/release operations")

	a.root.AddCommand(a.buildTaskCmds()...)
	a.root.AddCommand(a.buildProjectCmd())
	a.root.AddCommand(a.buildTagCmd())
	a.root.AddCommand(a.buildWorkflowCmd())
	a.root.AddCommand(a.buildPlayerCmd())
	a.root.AddCommand(a.buildConfigCmd())
	a.root.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run: func(cmd *cobra.Command, args []string) {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "tusk %s (commit: %s, built: %s)\n", vi.Version, vi.Commit, vi.Date)
		},
	})

	mcpCmd := &cobra.Command{
		Use:   "mcp",
		Short: "MCP server commands",
	}
	mcpCmd.AddCommand(&cobra.Command{
		Use:   "serve",
		Short: "Start MCP server with stdio transport",
		RunE: func(cmd *cobra.Command, args []string) error {
			mcpServer, err := tuskmcp.New(
				taskSvc, tagSvc, relationSvc, projectSvc,
				a.workflowSvc, a.playerSvc,
				a.workflowRepo, a.projectRepo, a.urgencyEngine,
				vi.Version, a.mcpCfg, a.loadOpts,
			)
			if err != nil {
				return fmt.Errorf("initializing MCP server: %w", err)
			}
			return mcpServer.Serve()
		},
	})
	a.root.AddCommand(mcpCmd)

	return a
}

// collectNonTerminalStatuses returns the union of non-terminal status names
// across every configured workflow. Used as the default status set for
// the filter resolver. Falls back to ["pending","active"] if listing fails
// or no statuses are found, so the CLI still functions when config is broken.
func collectNonTerminalStatuses(wfSvc *service.WorkflowService) []string {
	if wfSvc == nil {
		return []string{"pending", "active"}
	}
	workflows, err := wfSvc.List(context.Background())
	if err != nil {
		return []string{"pending", "active"}
	}
	seen := make(map[string]bool)
	var result []string
	for _, wf := range workflows {
		for _, name := range wf.NonTerminalStatuses() {
			if !seen[name] {
				seen[name] = true
				result = append(result, name)
			}
		}
	}
	if len(result) == 0 {
		return []string{"pending", "active"}
	}
	return result
}

// buildDimStatuses collects all dim statuses from all workflow configs into a lookup set.
func (a *App) buildDimStatuses() map[string]bool {
	workflows, err := a.workflowSvc.List(context.Background())
	if err != nil {
		return nil
	}
	dim := make(map[string]bool)
	for _, wf := range workflows {
		for name, sc := range wf.Statuses {
			if sc.HasRole(domain.RoleDim) {
				dim[name] = true
			}
		}
	}
	return dim
}

// Run executes the Cobra command tree with the given arguments.
func (a *App) Run(args []string) error {
	a.root.SetArgs(args)
	return a.root.Execute()
}
