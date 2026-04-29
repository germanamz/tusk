package tui

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/germanamz/tusk/config"
	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/filter"
	tuskmcp "github.com/germanamz/tusk/internal/mcp"
	"github.com/germanamz/tusk/repository"
	"github.com/germanamz/tusk/service"
	"github.com/google/uuid"
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
	taskSvc        *service.TaskService
	tagSvc         *service.TagService
	relationSvc    *service.RelationService
	projectSvc     *service.ProjectService
	workflowSvc    *service.WorkflowService
	playerSvc      *service.PlayerService
	noteSvc        *service.NoteService
	portabilitySvc *service.PortabilityService
	workflowRepo   repository.WorkflowRepository
	projectRepo    repository.ProjectRepository
	urgencyEngine  *service.UrgencyEngine
	playerID       string // from --player flag
	resolver       *filter.Resolver
	root           *cobra.Command
	format         string
	noColor        bool
	version        VersionInfo
	tuiCfg         config.TUIConfig
	mcpCfg         config.MCPConfig
	inlineCfg      config.InlineConfig
	loadOpts       []config.Option
}

// newRenderer creates a Renderer wired with a per-call project-name cache
// backed by the configured ProjectService. dimStatuses may be nil.
func (app *App) newRenderer(ctx context.Context, writer io.Writer, dimStatuses map[string]bool) *Renderer {
	renderer := NewRenderer(writer, app.format, app.colorEnabled(), dimStatuses)
	if app.projectSvc != nil {
		nameCache := map[uuid.UUID]string{}
		renderer.SetProjectNameResolver(func(id uuid.UUID) string {
			if name, ok := nameCache[id]; ok {
				return name
			}
			project, err := app.projectSvc.GetByID(ctx, id)

			if err != nil {
				nameCache[id] = id.String()
				return id.String()
			}

			nameCache[id] = project.Name
			return project.Name
		})

		taxCache := map[uuid.UUID]bool{}
		renderer.SetTaxonomyResolver(func(id uuid.UUID) bool {
			if cached, ok := taxCache[id]; ok {
				return cached
			}
			project, err := app.projectSvc.GetByID(ctx, id)

			if err != nil {
				taxCache[id] = false
				return false
			}

			taxonomy, _ := app.projectSvc.EffectiveTaxonomy(project)
			has := !taxonomy.IsEmpty()
			taxCache[id] = has
			return has
		})
	}
	return renderer
}

// colorEnabled resolves whether color output is active.
// Precedence: --no-color flag > NO_COLOR env > tui.color config.
func (app *App) colorEnabled() bool {
	if app.noColor {
		return false
	}
	if _, ok := os.LookupEnv("NO_COLOR"); ok {
		return false
	}
	return app.tuiCfg.Color
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
	noteSvc *service.NoteService,
	portabilitySvc *service.PortabilityService,
	workflowRepo repository.WorkflowRepository,
	projectRepo repository.ProjectRepository,
	urgencyEngine *service.UrgencyEngine,
	vi VersionInfo,
	tuiCfg config.TUIConfig,
	mcpCfg config.MCPConfig,
	inlineCfg config.InlineConfig,
	loadOpts []config.Option,
) *App {
	app := &App{
		taskSvc:        taskSvc,
		tagSvc:         tagSvc,
		relationSvc:    relationSvc,
		projectSvc:     projectSvc,
		workflowSvc:    workflowSvc,
		playerSvc:      playerSvc,
		noteSvc:        noteSvc,
		portabilitySvc: portabilitySvc,
		workflowRepo:   workflowRepo,
		projectRepo:    projectRepo,
		urgencyEngine:  urgencyEngine,
		version:        vi,
		tuiCfg:         tuiCfg,
		mcpCfg:         mcpCfg,
		inlineCfg:      inlineCfg,
		loadOpts:       loadOpts,
	}
	app.resolver = filter.NewResolver(taskSvc, projectSvc, collectNonTerminalStatuses(workflowSvc))

	app.root = &cobra.Command{
		Use:           "tusk",
		Short:         "A concurrent-safe task management tool",
		Version:       vi.Version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	app.root.CompletionOptions.DisableDefaultCmd = true

	app.root.SetVersionTemplate(fmt.Sprintf("tusk %s (commit: %s, built: %s)\n", vi.Version, vi.Commit, vi.Date))
	app.root.PersistentFlags().StringVar(&app.format, "format", "text", `output format: "text", "json", or "markdown" (markdown is supported only on tree)`)
	app.root.PersistentFlags().BoolVar(&app.noColor, "no-color", false, "disable colored output")
	app.root.PersistentFlags().StringVar(&app.playerID, "player", "", "player ID for claim/release operations")

	app.root.PersistentPreRun = func(cmd *cobra.Command, _ []string) {
		if app.playerID != "" {
			cmd.SetContext(service.WithActor(cmd.Context(), app.playerID))
		}
	}

	app.root.AddCommand(app.buildTaskCmd())
	app.registerMovedStubs()
	app.root.AddCommand(app.buildProjectCmd())
	app.root.AddCommand(app.buildTagCmd())
	app.root.AddCommand(app.buildWorkflowCmd())
	app.root.AddCommand(app.buildPlayerCmd())
	app.root.AddCommand(app.buildNoteCmd())
	app.root.AddCommand(app.buildConfigCmd())
	app.root.AddCommand(app.buildExportCmd())
	app.root.AddCommand(app.buildImportCmd())
	app.root.AddCommand(&cobra.Command{
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
				app.workflowSvc, app.playerSvc, app.noteSvc,
				app.workflowRepo, app.projectRepo, app.urgencyEngine,
				vi.Version, app.mcpCfg, app.loadOpts,
			)

			if err != nil {
				return fmt.Errorf("initializing MCP server: %w", err)
			}

			return mcpServer.Serve()
		},
	})
	app.root.AddCommand(mcpCmd)
	app.root.AddCommand(app.buildCompletionCmd())

	return app
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
	for _, workflow := range workflows {
		for _, name := range workflow.NonTerminalStatuses() {
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
func (app *App) buildDimStatuses() map[string]bool {
	workflows, err := app.workflowSvc.List(context.Background())

	if err != nil {
		return nil
	}

	dim := make(map[string]bool)
	for _, workflow := range workflows {
		for name, statusConfig := range workflow.Statuses {
			if statusConfig.HasRole(domain.RoleDim) {
				dim[name] = true
			}
		}
	}
	return dim
}

// buildHighlightStatuses collects all highlight statuses from every workflow
// config into a lookup set, mirroring buildDimStatuses.
func (app *App) buildHighlightStatuses() map[string]bool {
	workflows, err := app.workflowSvc.List(context.Background())

	if err != nil {
		return nil
	}

	highlight := make(map[string]bool)
	for _, workflow := range workflows {
		for name, statusConfig := range workflow.Statuses {
			if statusConfig.HasRole(domain.RoleHighlight) {
				highlight[name] = true
			}
		}
	}
	return highlight
}

// Run executes the Cobra command tree with the given arguments.
func (app *App) Run(args []string) error {
	app.root.SetArgs(args)
	return app.root.Execute()
}
