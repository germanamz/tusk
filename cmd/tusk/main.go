package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/germanamz/tusk/config"
	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/internal/tui"
	"github.com/germanamz/tusk/migrations"
	"github.com/germanamz/tusk/service"
	"github.com/germanamz/tusk/sqlite"
	"github.com/google/uuid"
)

// version, commit, and date are set by goreleaser at build time via -ldflags.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}
}

func run() error {
	if isCompletionInvocation(os.Args[1:]) {
		app := tui.New(
			nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
			tui.VersionInfo{Version: version, Commit: commit, Date: date},
			config.TUIConfig{}, config.MCPConfig{}, config.InlineConfig{}, nil,
		)
		return app.Run(stripConfigFlag(stripDBFlag(os.Args[1:])))
	}

	explicitConfig, err := resolveConfigPath()
	if err != nil {
		return err
	}

	var loadOpts []config.Option
	if explicitConfig != "" {
		loadOpts = append(loadOpts, config.WithExplicitFile(explicitConfig))
	}
	if startDir, err := os.Getwd(); err == nil && startDir != "" {
		loadOpts = append(loadOpts, config.WithStartDir(startDir))
	}

	cfg, err := config.Load(loadOpts...)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	dbPath, err := resolveDBPath(cfg.Storage.Path)
	if err != nil {
		return err
	}

	baseDir := "."
	if cfg.Sources.File != "" {
		baseDir = filepath.Dir(cfg.Sources.File)
	}

	absDB, err := sqlite.ResolveWorkspacePath(dbPath, baseDir)
	if err != nil {
		return fmt.Errorf("resolving db path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(absDB), 0o755); err != nil {
		return fmt.Errorf("creating db dir: %w", err)
	}
	store, err := sqlite.New(absDB, migrations.FS)
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer store.Close()

	db := store.DB()
	projectRepo := sqlite.NewProjectRepo(db)
	workflowRepo := sqlite.NewWorkflowRepo(db)

	workflowSvc := service.NewWorkflowService(workflowRepo, projectRepo)

	urgencyEngine := service.NewUrgencyEngine(service.UrgencyWeights{
		Priority:    cfg.Urgency.PriorityWeight,
		Due:         cfg.Urgency.DueWeight,
		Age:         cfg.Urgency.AgeWeight,
		Active:      cfg.Urgency.ActiveWeight,
		Blocking:    cfg.Urgency.BlockingWeight,
		Blocked:     cfg.Urgency.BlockedWeight,
		Tags:        cfg.Urgency.TagsWeight,
		Project:     cfg.Urgency.ProjectWeight,
		Annotations: cfg.Urgency.AnnotationsWeight,
		Waiting:     cfg.Urgency.WaitingWeight,
	})

	bundle := &service.RepoBundle{
		Store:       store,
		Tasks:       sqlite.NewTaskRepo(db),
		Annotations: sqlite.NewAnnotationRepo(db),
		Notes:       sqlite.NewNoteRepo(db),
		Relations:   sqlite.NewRelationRepo(db),
		Tags:        sqlite.NewTagRepo(db),
		Players:     sqlite.NewPlayerRepo(db),
	}

	resolver := func(ctx context.Context, projectID uuid.UUID) (*service.RepoBundle, error) {
		if _, err := projectRepo.GetByID(ctx, projectID); err != nil {
			return nil, fmt.Errorf("unknown project %v: %w", projectID, err)
		}
		return bundle, nil
	}
	projectLister := func(ctx context.Context) ([]uuid.UUID, error) {
		projects, err := projectRepo.List(ctx)
		if err != nil {
			return nil, err
		}
		ids := make([]uuid.UUID, 0, len(projects))
		for _, p := range projects {
			ids = append(ids, p.ID)
		}
		return ids, nil
	}

	taskSvc := service.NewTaskService(resolver, projectLister, projectRepo, workflowSvc, urgencyEngine)
	tagSvc := service.NewTagService(resolver)
	relationSvc := service.NewRelationService(resolver, projectLister)

	projectDefaults := service.ProjectDefaults{
		Urgency: service.UrgencyWeights{
			Priority:    cfg.Urgency.PriorityWeight,
			Due:         cfg.Urgency.DueWeight,
			Age:         cfg.Urgency.AgeWeight,
			Active:      cfg.Urgency.ActiveWeight,
			Blocking:    cfg.Urgency.BlockingWeight,
			Blocked:     cfg.Urgency.BlockedWeight,
			Tags:        cfg.Urgency.TagsWeight,
			Project:     cfg.Urgency.ProjectWeight,
			Annotations: cfg.Urgency.AnnotationsWeight,
			Waiting:     cfg.Urgency.WaitingWeight,
		},
	}
	projectSvc := service.NewProjectService(projectRepo, bundle.Tasks, store, projectDefaults)

	defaultBundle, err := resolver(context.Background(), domain.DefaultProjectUUID)
	if err != nil {
		return fmt.Errorf("resolving default bundle for players: %w", err)
	}
	playerSvc := service.NewPlayerService(defaultBundle.Players)
	noteSvc := service.NewNoteService(bundle.Notes, bundle.Players, projectRepo, bundle.Tasks, cfg.Notes.WindowSize)

	app := tui.New(
		taskSvc, tagSvc, relationSvc, projectSvc, workflowSvc, playerSvc, noteSvc,
		workflowRepo, projectRepo, urgencyEngine,
		tui.VersionInfo{
			Version: version,
			Commit:  commit,
			Date:    date,
		},
		cfg.TUI, cfg.MCP, cfg.Inline, loadOpts,
	)
	return app.Run(stripConfigFlag(stripDBFlag(os.Args[1:])))
}

// stripDBFlag removes --db and its value from args so Cobra doesn't see them.
func stripDBFlag(args []string) []string {
	var out []string
	for i := 0; i < len(args); i++ {
		if args[i] == "--db" && i+1 < len(args) {
			i++ // skip value
			continue
		}
		if strings.HasPrefix(args[i], "--db=") {
			continue
		}
		out = append(out, args[i])
	}
	return out
}

// stripConfigFlag removes --config and its value from args so Cobra doesn't see them.
// Mirrors stripDBFlag for the --config global flag.
func stripConfigFlag(args []string) []string {
	var out []string
	for i := 0; i < len(args); i++ {
		if args[i] == "--config" && i+1 < len(args) {
			i++ // skip value
			continue
		}
		if strings.HasPrefix(args[i], "--config=") {
			continue
		}
		out = append(out, args[i])
	}
	return out
}

// resolveDBPath returns the database path from: --db flag > TUSK_DB env > config value.
// The config value always has a default ("~/.local/share/tusk/tusk.db") so it acts as
// the final fallback. We check os.Args directly for --db because Cobra hasn't parsed yet.
func resolveDBPath(configPath string) (string, error) {
	for i, arg := range os.Args {
		if arg == "--db" {
			if i+1 >= len(os.Args) {
				return "", fmt.Errorf("--db requires a value")
			}
			return os.Args[i+1], nil
		}
		if strings.HasPrefix(arg, "--db=") {
			val := arg[5:]
			if val == "" {
				return "", fmt.Errorf("--db requires a value")
			}
			return val, nil
		}
	}

	// Check environment variable
	if v := os.Getenv("TUSK_DB"); v != "" {
		return v, nil
	}

	// Config value (with tilde expansion). Always populated — Viper provides
	// the default "~/.local/share/tusk/tusk.db" when no config file is present.
	return config.ExpandPath(configPath), nil
}

// resolveConfigPath returns the explicit config file path from:
//  1. --config flag (either "--config foo" or "--config=foo")
//  2. TUSK_CONFIG env var
//  3. "" — meaning "no explicit file, fall back to the global search"
//
// It does not stat the file; config.Load is responsible for turning a
// missing explicit file into a hard error via config.WithExplicitFile.
func resolveConfigPath() (string, error) {
	for i, arg := range os.Args {
		if arg == "--config" {
			if i+1 >= len(os.Args) {
				return "", fmt.Errorf("--config requires a value")
			}
			return os.Args[i+1], nil
		}
		if strings.HasPrefix(arg, "--config=") {
			val := arg[len("--config="):]
			if val == "" {
				return "", fmt.Errorf("--config requires a value")
			}
			return val, nil
		}
	}

	if v := os.Getenv("TUSK_CONFIG"); v != "" {
		return v, nil
	}
	return "", nil
}

// isCompletionInvocation reports whether args (the slice after the binary
// name) dispatches to either the human-facing `completion` command or
// Cobra's hidden `__complete` shell-completion RPC. It walks the args,
// skipping the known global flags (and their values) so that
// `tusk --config foo completion bash` and `tusk --db=/tmp/x __complete task ""`
// both return true.
func isCompletionInvocation(args []string) bool {
	valueFlags := map[string]bool{
		"--config": true,
		"--db":     true,
		"--format": true,
		"--player": true,
	}
	boolFlags := map[string]bool{
		"--no-color": true,
	}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if eq := strings.IndexByte(a, '='); eq > 0 {
			name := a[:eq]
			if valueFlags[name] || boolFlags[name] {
				continue
			}
		}
		if valueFlags[a] {
			i++ // skip value
			continue
		}
		if boolFlags[a] {
			continue
		}
		return a == "completion" || a == "__complete"
	}
	return false
}
