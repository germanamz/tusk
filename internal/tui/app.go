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
	taskSvc       *service.TaskService
	tagSvc        *service.TagService
	relationSvc   *service.RelationService
	projectSvc    *service.ProjectService
	workflowSvc   *service.WorkflowService
	playerSvc     *service.PlayerService
	noteSvc       *service.NoteService
	workflowRepo  repository.WorkflowRepository
	projectRepo   repository.ProjectRepository
	urgencyEngine *service.UrgencyEngine
	playerID      string // from --player flag
	resolver      *filter.Resolver
	root          *cobra.Command
	format        string
	noColor       bool
	version       VersionInfo
	tuiCfg        config.TUIConfig
	mcpCfg        config.MCPConfig
	inlineCfg     config.InlineConfig
	loadOpts      []config.Option
}

// newRenderer creates a Renderer wired with a per-call project-name cache
// backed by the configured ProjectService. dimStatuses may be nil.
func (a *App) newRenderer(ctx context.Context, w io.Writer, dimStatuses map[string]bool) *Renderer {
	r := NewRenderer(w, a.format, a.colorEnabled(), dimStatuses)
	if a.projectSvc != nil {
		nameCache := map[uuid.UUID]string{}
		r.SetProjectNameResolver(func(id uuid.UUID) string {
			if name, ok := nameCache[id]; ok {
				return name
			}
			p, err := a.projectSvc.GetByID(ctx, id)
			if err != nil {
				nameCache[id] = id.String()
				return id.String()
			}
			nameCache[id] = p.Name
			return p.Name
		})

		taxCache := map[uuid.UUID]bool{}
		r.SetTaxonomyResolver(func(id uuid.UUID) bool {
			if v, ok := taxCache[id]; ok {
				return v
			}
			p, err := a.projectSvc.GetByID(ctx, id)
			if err != nil {
				taxCache[id] = false
				return false
			}
			tax, _ := a.projectSvc.EffectiveTaxonomy(p)
			has := !tax.IsEmpty()
			taxCache[id] = has
			return has
		})
	}
	return r
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
	noteSvc *service.NoteService,
	workflowRepo repository.WorkflowRepository,
	projectRepo repository.ProjectRepository,
	urgencyEngine *service.UrgencyEngine,
	vi VersionInfo,
	tuiCfg config.TUIConfig,
	mcpCfg config.MCPConfig,
	inlineCfg config.InlineConfig,
	loadOpts []config.Option,
) *App {
	a := &App{
		taskSvc:       taskSvc,
		tagSvc:        tagSvc,
		relationSvc:   relationSvc,
		projectSvc:    projectSvc,
		workflowSvc:   workflowSvc,
		playerSvc:     playerSvc,
		noteSvc:       noteSvc,
		workflowRepo:  workflowRepo,
		projectRepo:   projectRepo,
		urgencyEngine: urgencyEngine,
		version:       vi,
		tuiCfg:        tuiCfg,
		mcpCfg:        mcpCfg,
		inlineCfg:     inlineCfg,
		loadOpts:      loadOpts,
	}
	a.resolver = filter.NewResolver(taskSvc, projectSvc, collectNonTerminalStatuses(workflowSvc))

	a.root = &cobra.Command{
		Use:           "tusk",
		Short:         "A concurrent-safe task management tool",
		Version:       vi.Version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	a.root.CompletionOptions.DisableDefaultCmd = true

	a.root.SetVersionTemplate(fmt.Sprintf("tusk %s (commit: %s, built: %s)\n", vi.Version, vi.Commit, vi.Date))
	a.root.PersistentFlags().StringVar(&a.format, "format", "text", `output format: "text" or "json"`)
	a.root.PersistentFlags().BoolVar(&a.noColor, "no-color", false, "disable colored output")
	a.root.PersistentFlags().StringVar(&a.playerID, "player", "", "player ID for claim/release operations")

	a.root.PersistentPreRun = func(cmd *cobra.Command, _ []string) {
		if a.playerID != "" {
			cmd.SetContext(service.WithActor(cmd.Context(), a.playerID))
		}
	}

	a.root.AddCommand(a.buildTaskCmd())
	a.registerMovedStubs()
	a.root.AddCommand(a.buildProjectCmd())
	a.root.AddCommand(a.buildTagCmd())
	a.root.AddCommand(a.buildWorkflowCmd())
	a.root.AddCommand(a.buildPlayerCmd())
	a.root.AddCommand(a.buildNoteCmd())
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
				a.workflowSvc, a.playerSvc, a.noteSvc,
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
	a.root.AddCommand(a.buildCompletionCmd())

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

// buildHighlightStatuses collects all highlight statuses from every workflow
// config into a lookup set, mirroring buildDimStatuses.
func (a *App) buildHighlightStatuses() map[string]bool {
	workflows, err := a.workflowSvc.List(context.Background())
	if err != nil {
		return nil
	}
	highlight := make(map[string]bool)
	for _, wf := range workflows {
		for name, sc := range wf.Statuses {
			if sc.HasRole(domain.RoleHighlight) {
				highlight[name] = true
			}
		}
	}
	return highlight
}

// Run executes the Cobra command tree with the given arguments.
func (a *App) Run(args []string) error {
	a.root.SetArgs(args)
	return a.root.Execute()
}
