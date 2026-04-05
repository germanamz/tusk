package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/germanamz/tusk/internal/config"
	"github.com/germanamz/tusk/internal/inmem"
	"github.com/germanamz/tusk/internal/service"
	"github.com/germanamz/tusk/internal/sqlite"
	"github.com/germanamz/tusk/internal/tui"
	"github.com/germanamz/tusk/migrations"
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

	// Ensure parent directory exists
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating data directory %s: %w", dir, err)
	}

	store, err := sqlite.New(dbPath, migrations.FS)
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer store.Close()

	db := store.DB()
	taskRepo := sqlite.NewTaskRepo(db)
	annotationRepo := sqlite.NewAnnotationRepo(db)
	projectRepo := inmem.NewProjectRepository(cfg.Projects)
	workflowRepo := sqlite.NewWorkflowRepo(db)

	tagRepo := sqlite.NewTagRepo(db)
	relationRepo := sqlite.NewRelationRepo(db)

	workflowSvc := service.NewWorkflowService(workflowRepo)
	taskSvc := service.NewTaskService(taskRepo, annotationRepo, projectRepo, workflowSvc, store)
	tagSvc := service.NewTagService(tagRepo)
	relationSvc := service.NewRelationService(relationRepo, taskRepo, store)

	projectSvc := service.NewProjectService(projectRepo)

	app := tui.New(taskSvc, tagSvc, relationSvc, projectSvc, workflowSvc, tui.VersionInfo{
		Version: version,
		Commit:  commit,
		Date:    date,
	}, cfg.TUI, cfg.MCP)
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
