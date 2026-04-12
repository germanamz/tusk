package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/germanamz/tusk/config"
	"github.com/germanamz/tusk/inmem"
	"github.com/germanamz/tusk/internal/tui"
	"github.com/germanamz/tusk/migrations"
	"github.com/germanamz/tusk/service"
	"github.com/germanamz/tusk/sqlite"
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
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	dbPath, err := resolveDBPath(cfg.Storage.Path)
	if err != nil {
		return err
	}

	configPath, _ := config.ConfigFilePath()
	baseDir := "."
	if configPath != "" {
		baseDir = filepath.Dir(configPath)
	}

	registry, err := sqlite.NewStoreRegistry(dbPath, baseDir, cfg.Projects, migrations.FS)
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer registry.Close()

	projectRepo := inmem.NewProjectRepository(cfg.Projects)
	workflowRepo := inmem.NewWorkflowRepository(cfg.Workflows)

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

	var bundleMu sync.Mutex
	bundleCache := map[*sqlite.Store]*service.RepoBundle{}
	bundleFor := func(store *sqlite.Store) *service.RepoBundle {
		bundleMu.Lock()
		defer bundleMu.Unlock()
		if b, ok := bundleCache[store]; ok {
			return b
		}
		db := store.DB()
		b := &service.RepoBundle{
			Store:       store,
			Tasks:       sqlite.NewTaskRepo(db),
			Annotations: sqlite.NewAnnotationRepo(db),
			Relations:   sqlite.NewRelationRepo(db),
			Tags:        sqlite.NewTagRepo(db),
			Players:     sqlite.NewPlayerRepo(db),
		}
		bundleCache[store] = b
		return b
	}

	resolver := func(_ context.Context, projectID string) (*service.RepoBundle, error) {
		store, err := registry.Get(projectID)
		if err != nil {
			return nil, err
		}
		return bundleFor(store), nil
	}
	projectLister := func(context.Context) ([]string, error) {
		return registry.ProjectIDs(), nil
	}

	taskSvc := service.NewTaskService(resolver, projectLister, projectRepo, workflowSvc, urgencyEngine)
	tagSvc := service.NewTagService(resolver)
	relationSvc := service.NewRelationService(resolver, projectLister)

	projectSvc := service.NewProjectService(projectRepo)

	defaultBundle, err := resolver(context.Background(), config.DefaultProjectID)
	if err != nil {
		return fmt.Errorf("resolving default bundle for players: %w", err)
	}
	playerSvc := service.NewPlayerService(defaultBundle.Players)

	app := tui.New(taskSvc, tagSvc, relationSvc, projectSvc, workflowSvc, playerSvc, tui.VersionInfo{
		Version: version,
		Commit:  commit,
		Date:    date,
	}, cfg.TUI, cfg.MCP, nil)
	return app.Run(stripDBFlag(os.Args[1:]))
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
