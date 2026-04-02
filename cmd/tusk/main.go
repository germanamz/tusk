package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/germanamz/tusk/internal/service"
	"github.com/germanamz/tusk/internal/sqlite"
	"github.com/germanamz/tusk/internal/tui"
	"github.com/germanamz/tusk/migrations"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}
}

func run() error {
	dbPath := resolveDBPath()

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
	projectRepo := sqlite.NewProjectRepo(db)
	workflowRepo := sqlite.NewWorkflowRepo(db)

	workflowSvc := service.NewWorkflowService(workflowRepo)
	taskSvc := service.NewTaskService(taskRepo, annotationRepo, projectRepo, workflowSvc)

	app := tui.New(taskSvc, projectRepo)
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
		if len(args[i]) > 5 && args[i][:5] == "--db=" {
			continue
		}
		out = append(out, args[i])
	}
	return out
}

// resolveDBPath returns the database path from: --db flag, TUSK_DB env, or default.
// We check os.Args directly for --db because Cobra hasn't parsed yet at this point.
func resolveDBPath() string {
	// Check os.Args for --db flag
	for i, arg := range os.Args {
		if arg == "--db" && i+1 < len(os.Args) {
			return os.Args[i+1]
		}
		if len(arg) > 5 && arg[:5] == "--db=" {
			return arg[5:]
		}
	}

	// Check environment variable
	if v := os.Getenv("TUSK_DB"); v != "" {
		return v
	}

	// Default path
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, ".local", "share", "tusk", "tusk.db")
}
