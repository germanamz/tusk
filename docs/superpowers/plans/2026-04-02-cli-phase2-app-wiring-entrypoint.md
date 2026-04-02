# CLI Phase 2: App Wiring & Entry Point

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the Cobra command tree in `internal/tui/app.go` and the `main()` entry point in `cmd/tusk/main.go` so the binary compiles, shows help, and can resolve the database path — but commands are stubs that print "not implemented" until Phase 3/4.

**Architecture:** `App` struct in `internal/tui/` owns the Cobra root command and holds service dependencies. `main.go` handles DB path resolution, opens SQLite, wires dependencies, and calls `app.Run()`. Each subcommand is registered in `app.go` with its `RunE` pointing to a stub method in `commands.go`.

**Tech Stack:** `github.com/spf13/cobra` (already in go.mod), `internal/sqlite`, `internal/service`, Go standard library (`os`, `path/filepath`).

**Depends on:** Phase 1 (filter.go and render.go must exist so the package compiles).

---

### Task 1: App struct and Cobra command tree

**Files:**
- Create: `internal/tui/app.go` (replace the empty stub)
- Create: `internal/tui/commands.go` (replace the empty stub)

- [ ] **Step 1: Write a compilation test**

Create `internal/tui/app_test.go`:

```go
package tui

import "testing"

func TestNewApp_NotNil(t *testing.T) {
	// Pass nil dependencies — we only check that New() builds the command tree
	app := New(nil, nil)
	if app == nil {
		t.Fatal("expected non-nil App")
	}
	if app.root == nil {
		t.Fatal("expected non-nil root command")
	}
}

func TestApp_SubcommandsRegistered(t *testing.T) {
	app := New(nil, nil)
	want := []string{"add", "list", "info", "modify", "start", "done", "delete", "annotate"}
	cmds := app.root.Commands()
	names := make(map[string]bool)
	for _, c := range cmds {
		names[c.Name()] = true
	}
	for _, name := range want {
		if !names[name] {
			t.Errorf("expected subcommand %q to be registered", name)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/tui/ -v -run "TestNewApp|TestApp_Subcommands"`
Expected: Compilation failure — `New` function not defined.

- [ ] **Step 3: Implement app.go**

Replace `internal/tui/app.go` with:

```go
package tui

import (
	"github.com/germanamz/tusk/internal/repository"
	"github.com/germanamz/tusk/internal/service"
	"github.com/spf13/cobra"
)

// App holds the CLI's dependencies and Cobra command tree.
type App struct {
	taskSvc     *service.TaskService
	projectRepo repository.ProjectRepository
	root        *cobra.Command
	format      string
}

// New creates a new App and builds the Cobra command tree.
// taskSvc and projectRepo may be nil for testing command registration.
func New(taskSvc *service.TaskService, projectRepo repository.ProjectRepository) *App {
	a := &App{
		taskSvc:     taskSvc,
		projectRepo: projectRepo,
	}

	a.root = &cobra.Command{
		Use:   "tusk",
		Short: "A concurrent-safe task management tool",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

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
	)

	return a
}

// Run executes the Cobra command tree with the given arguments.
func (a *App) Run(args []string) error {
	a.root.SetArgs(args)
	return a.root.Execute()
}
```

- [ ] **Step 4: Implement stub commands in commands.go**

Replace `internal/tui/commands.go` with:

```go
package tui

import (
	"fmt"

	"github.com/spf13/cobra"
)

func (a *App) runAdd(cmd *cobra.Command, args []string) error {
	return fmt.Errorf("not implemented")
}

func (a *App) runList(cmd *cobra.Command, args []string) error {
	return fmt.Errorf("not implemented")
}

func (a *App) runInfo(cmd *cobra.Command, args []string) error {
	return fmt.Errorf("not implemented")
}

func (a *App) runModify(cmd *cobra.Command, args []string) error {
	return fmt.Errorf("not implemented")
}

func (a *App) runStart(cmd *cobra.Command, args []string) error {
	return fmt.Errorf("not implemented")
}

func (a *App) runDone(cmd *cobra.Command, args []string) error {
	return fmt.Errorf("not implemented")
}

func (a *App) runDelete(cmd *cobra.Command, args []string) error {
	return fmt.Errorf("not implemented")
}

func (a *App) runAnnotate(cmd *cobra.Command, args []string) error {
	return fmt.Errorf("not implemented")
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/tui/ -v -run "TestNewApp|TestApp_Subcommands"`
Expected: Both tests PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/app.go internal/tui/app_test.go internal/tui/commands.go
git commit -m "feat(tui): add App struct with Cobra command tree and stub commands"
```

---

### Task 2: Entry point — `cmd/tusk/main.go`

**Files:**
- Create: `cmd/tusk/main.go` (replace the empty stub)

This wires everything together: DB path resolution, SQLite store, repos, services, App.

- [ ] **Step 1: Implement main.go**

Replace `cmd/tusk/main.go` with:

```go
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
	return app.Run(os.Args[1:])
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
```

- [ ] **Step 2: Verify it compiles and shows help**

Run: `cd /Users/germanamz/projects/tusk && go build -o /tmp/tusk ./cmd/tusk/ && /tmp/tusk --help`

Expected output should include:
```
A concurrent-safe task management tool

Usage:
  tusk [command]

Available Commands:
  add         Create a new task
  annotate    Add a note to a task
  ...
```

- [ ] **Step 3: Verify --db flag with a temp database**

Run: `cd /Users/germanamz/projects/tusk && /tmp/tusk --db /tmp/tusk-test.db list`
Expected: Error "not implemented" (the stub), but no database errors. The file `/tmp/tusk-test.db` should be created.

- [ ] **Step 4: Clean up temp files**

Run: `rm -f /tmp/tusk /tmp/tusk-test.db /tmp/tusk-test.db-wal /tmp/tusk-test.db-shm`

- [ ] **Step 5: Commit**

```bash
git add cmd/tusk/main.go
git commit -m "feat(cmd): implement main entry point with DB path resolution and DI wiring"
```

---

### Task 3: Error formatting helper

**Files:**
- Modify: `internal/tui/commands.go`

The commands will need to translate domain errors into user-friendly messages. Add a helper now before implementing the real commands.

- [ ] **Step 1: Write the failing test**

Create or append to `internal/tui/commands_test.go`:

```go
package tui

import (
	"fmt"
	"testing"

	"github.com/germanamz/tusk/internal/domain"
)

func TestFormatError_NotFound(t *testing.T) {
	err := fmt.Errorf("getting task: %w", domain.ErrNotFound)
	got := formatError(err, "abc12345")
	want := "Task not found: abc12345"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestFormatError_Conflict(t *testing.T) {
	err := domain.ErrConflict
	got := formatError(err, "abc12345")
	want := "Version conflict - task was modified by another process"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestFormatError_InvalidTransition(t *testing.T) {
	err := fmt.Errorf("transition %q → %q not allowed: %w", "pending", "completed", domain.ErrInvalidTransition)
	got := formatError(err, "abc12345")
	if got != err.Error() {
		t.Fatalf("expected original error message, got %q", got)
	}
}

func TestFormatError_Generic(t *testing.T) {
	err := fmt.Errorf("something went wrong")
	got := formatError(err, "abc12345")
	if got != "something went wrong" {
		t.Fatalf("expected original message, got %q", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/tui/ -v -run TestFormatError`
Expected: Compilation failure — `formatError` not defined.

- [ ] **Step 3: Implement formatError**

Add to `internal/tui/commands.go` (update imports to include `"errors"` and remove `"fmt"` from the stub if needed — you'll still need `"fmt"` for the stubs):

```go
import (
	"errors"
	"fmt"

	"github.com/germanamz/tusk/internal/domain"
	"github.com/spf13/cobra"
)

// formatError translates domain errors into user-friendly messages.
func formatError(err error, shortID string) string {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return fmt.Sprintf("Task not found: %s", shortID)
	case errors.Is(err, domain.ErrConflict):
		return "Version conflict - task was modified by another process"
	default:
		return err.Error()
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/germanamz/projects/tusk && go test ./internal/tui/ -v -run TestFormatError`
Expected: All 4 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/commands.go internal/tui/commands_test.go
git commit -m "feat(tui): add error formatting helper for domain errors"
```
