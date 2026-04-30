package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/germanamz/tusk/config"
	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/internal/tui"
	"github.com/germanamz/tusk/migrations"
	"github.com/germanamz/tusk/repository"
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
		if errors.Is(err, tui.ErrLevelViolations) {
			// `tusk task level-check` found violations. The renderer already
			// listed them; suppress the redundant error line and exit 1.
			os.Exit(1)
		}
		if errors.Is(err, tui.ErrImportFailed) {
			// `tusk import` failed validation. The renderer already wrote
			// every issue to stderr; suppress the redundant error line.
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}
}

func run() error {
	if isCompletionInvocation(os.Args[1:]) {
		app := tui.New(
			nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
			tui.VersionInfo{Version: version, Commit: commit, Date: date},
			config.TUIConfig{}, config.MCPConfig{}, config.InlineConfig{}, nil,
		)
		return app.Run(stripConfigFlag(stripDBFlag(os.Args[1:])))
	}

	explicitConfig, configPathErr := resolveConfigPath()

	if configPathErr != nil {
		return configPathErr
	}

	var loadOpts []config.Option
	if explicitConfig != "" {
		loadOpts = append(loadOpts, config.WithExplicitFile(explicitConfig))
	}
	if startDir, err := os.Getwd(); err == nil && startDir != "" {
		loadOpts = append(loadOpts, config.WithStartDir(startDir))
	}

	cfg, configErr := config.Load(loadOpts...)

	if configErr != nil {
		return fmt.Errorf("loading config: %w", configErr)
	}

	dbPath, dbPathErr := resolveDBPath(cfg.Storage.Path)

	if dbPathErr != nil {
		return dbPathErr
	}

	baseDir := "."
	if cfg.Sources.File != "" {
		baseDir = filepath.Dir(cfg.Sources.File)
	}

	absDB, resolveErr := sqlite.ResolveWorkspacePath(dbPath, baseDir)

	if resolveErr != nil {
		return fmt.Errorf("resolving db path: %w", resolveErr)
	}

	if err := os.MkdirAll(filepath.Dir(absDB), 0o755); err != nil {
		return fmt.Errorf("creating db dir: %w", err)
	}

	store, storeErr := sqlite.New(absDB, migrations.FS)

	if storeErr != nil {
		return fmt.Errorf("opening database: %w", storeErr)
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

	writeTx := &sqliteWriteTxProvider{
		store:      store,
		maxEvents:  cfg.Events.MaxEvents,
		pruneSlack: cfg.Events.PruneSlack,
	}
	bundle := &service.RepoBundle{
		Store:       store,
		WriteTx:     writeTx,
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
		for _, project := range projects {
			ids = append(ids, project.ID)
		}
		return ids, nil
	}

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
	projectSvc := service.NewProjectService(projectRepo, bundle.Tasks, store, projectDefaults, cfg)

	taskSvc := service.NewTaskService(resolver, projectLister, projectRepo, projectSvc, workflowSvc, urgencyEngine)
	tagSvc := service.NewTagService(resolver)
	relationSvc := service.NewRelationService(resolver, projectLister)

	defaultBundle, bundleErr := resolver(context.Background(), domain.DefaultProjectUUID)

	if bundleErr != nil {
		return fmt.Errorf("resolving default bundle for players: %w", bundleErr)
	}

	playerSvc := service.NewPlayerService(defaultBundle.Players)
	noteSvc := service.NewNoteService(bundle.Notes, bundle.Players, projectRepo, bundle.Tasks, cfg.Notes.WindowSize)

	portabilitySvc := service.NewPortabilityService(
		writeTx,
		taskSvc, projectSvc, workflowSvc, relationSvc,
		tagSvc, playerSvc, noteSvc,
		bundle,
		version,
	)

	app := tui.New(
		taskSvc, tagSvc, relationSvc, projectSvc, workflowSvc, playerSvc, noteSvc,
		portabilitySvc,
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

// sqliteWriteTxProvider adapts *sqlite.Store to service.WriteTxProvider.
// Retention knobs are captured here so services never pass them by hand.
type sqliteWriteTxProvider struct {
	store      *sqlite.Store
	maxEvents  int
	pruneSlack int
}

type sqliteWriteTx struct {
	tx         *sqlite.Tx
	maxEvents  int
	pruneSlack int
}

func (writeTx *sqliteWriteTx) Tasks() repository.TaskRepository { return writeTx.tx.Tasks() }
func (writeTx *sqliteWriteTx) Relations() repository.RelationRepository {
	return writeTx.tx.Relations()
}
func (writeTx *sqliteWriteTx) Events() repository.EventRepository {
	return writeTx.tx.Events(writeTx.maxEvents, writeTx.pruneSlack)
}

func (writeTx *sqliteWriteTx) Projects() repository.ProjectRepository { return writeTx.tx.Projects() }
func (writeTx *sqliteWriteTx) Workflows() repository.WorkflowRepository {
	return writeTx.tx.Workflows()
}
func (writeTx *sqliteWriteTx) Players() repository.PlayerRepository { return writeTx.tx.Players() }
func (writeTx *sqliteWriteTx) Tags() repository.TagRepository       { return writeTx.tx.Tags() }
func (writeTx *sqliteWriteTx) Annotations() repository.AnnotationRepository {
	return writeTx.tx.Annotations()
}
func (writeTx *sqliteWriteTx) Notes() repository.NoteRepository { return writeTx.tx.Notes() }

func (writeTx *sqliteWriteTx) TruncateAll(ctx context.Context) error {
	return writeTx.tx.TruncateAll(ctx)
}

func (provider *sqliteWriteTxProvider) WithTx(ctx context.Context, fn func(tx service.WriteTx) error) error {
	return provider.store.WithTx(ctx, func(stx *sqlite.Tx) error {
		return fn(&sqliteWriteTx{tx: stx, maxEvents: provider.maxEvents, pruneSlack: provider.pruneSlack})
	})
}

// stripDBFlag removes --db and its value from args so Cobra doesn't see them.
func stripDBFlag(args []string) []string {
	var out []string
	for index := 0; index < len(args); index++ {
		if args[index] == "--db" && index+1 < len(args) {
			index++ // skip value
			continue
		}
		if strings.HasPrefix(args[index], "--db=") {
			continue
		}
		out = append(out, args[index])
	}
	return out
}

// stripConfigFlag removes --config and its value from args so Cobra doesn't see them.
// Mirrors stripDBFlag for the --config global flag.
func stripConfigFlag(args []string) []string {
	var out []string
	for index := 0; index < len(args); index++ {
		if args[index] == "--config" && index+1 < len(args) {
			index++ // skip value
			continue
		}
		if strings.HasPrefix(args[index], "--config=") {
			continue
		}
		out = append(out, args[index])
	}
	return out
}

// resolveDBPath returns the database path from: --db flag > TUSK_DB env > config value.
// The config value always has a default ("~/.local/share/tusk/tusk.db") so it acts as
// the final fallback. We check os.Args directly for --db because Cobra hasn't parsed yet.
func resolveDBPath(configPath string) (string, error) {
	for index, arg := range os.Args {
		if arg == "--db" {
			if index+1 >= len(os.Args) {
				return "", fmt.Errorf("--db requires a value")
			}
			return os.Args[index+1], nil
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
	if envVal := os.Getenv("TUSK_DB"); envVal != "" {
		return envVal, nil
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
	for index, arg := range os.Args {
		if arg == "--config" {
			if index+1 >= len(os.Args) {
				return "", fmt.Errorf("--config requires a value")
			}
			return os.Args[index+1], nil
		}
		if strings.HasPrefix(arg, "--config=") {
			val := arg[len("--config="):]
			if val == "" {
				return "", fmt.Errorf("--config requires a value")
			}
			return val, nil
		}
	}

	if envVal := os.Getenv("TUSK_CONFIG"); envVal != "" {
		return envVal, nil
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
	for index := 0; index < len(args); index++ {
		arg := args[index]
		if eq := strings.IndexByte(arg, '='); eq > 0 {
			name := arg[:eq]
			if valueFlags[name] || boolFlags[name] {
				continue
			}
		}
		if valueFlags[arg] {
			index++ // skip value
			continue
		}
		if boolFlags[arg] {
			continue
		}
		return arg == "completion" || arg == "__complete"
	}
	return false
}
