# High-level Client Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `Client` type to the root `tusk` package that wires up SQLite, migrations, repos, and services from a single `Config` struct — letting external Go programs embed tusk as a library.

**Architecture:** Single `client.go` file in the root package. `NewClient(Config)` validates config, applies defaults (builtin kanban workflow, default project, default urgency weights), opens SQLite with auto-migration, creates all repos and services, returns a `Client` with services as public fields. `Close()` releases the DB.

**Tech Stack:** Go, SQLite, existing tusk packages (`config`, `service`, `sqlite`, `inmem`, `migrations`, `domain`, `repository`)

---

## File Structure

| File | Responsibility |
|------|---------------|
| `client.go` (create) | `Config` struct, `Client` struct, `NewClient`, `Close`, builtin default constants |
| `client_test.go` (create) | 4 integration tests proving wiring, defaults, validation, cleanup |
| `config/config.go` (modify) | Export `validate()` → `Validate()` (one-line rename + update call site) |

---

### Task 1: Export config.Validate

**Files:**
- Modify: `config/config.go:222` (rename `validate` → `Validate`)
- Modify: `config/config.go:215` (update call site)

- [ ] **Step 1: Rename validate to Validate**

In `config/config.go`, rename the method receiver and update the call site:

```go
// config/config.go:215 — update call site in Load()
// Change:
	if err := cfg.validate(); err != nil {
// To:
	if err := cfg.Validate(); err != nil {
```

```go
// config/config.go:222 — rename method
// Change:
func (c *Config) validate() error {
// To:
func (c *Config) Validate() error {
```

- [ ] **Step 2: Run existing config tests**

Run: `go test -v ./config/...`
Expected: All tests pass (no behavior change, just visibility).

- [ ] **Step 3: Commit**

```bash
git add config/config.go
git commit -m "refactor(config): export Validate method for use by root Client"
```

---

### Task 2: Write failing tests for NewClient

**Files:**
- Create: `client_test.go`

- [ ] **Step 1: Write all four test cases**

```go
package tusk

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/germanamz/tusk/domain"
)

func TestNewClient_CreateAndGetTask(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	client, err := NewClient(Config{DBPath: dbPath})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer client.Close()

	ctx := context.Background()

	task := &domain.Task{Title: "Test task"}
	if err := client.Tasks.Create(ctx, task); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if task.ShortID == "" {
		t.Fatal("expected ShortID to be set after create")
	}

	got, err := client.Tasks.GetByShortID(ctx, task.ShortID)
	if err != nil {
		t.Fatalf("GetByShortID: %v", err)
	}

	if got.Title != "Test task" {
		t.Errorf("Title = %q, want %q", got.Title, "Test task")
	}
}

func TestNewClient_DefaultConfig(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	client, err := NewClient(Config{DBPath: dbPath})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer client.Close()

	ctx := context.Background()

	projects, err := client.Projects.List(ctx)
	if err != nil {
		t.Fatalf("Projects.List: %v", err)
	}
	found := false
	for _, p := range projects {
		if p.ID == "default" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected builtin 'default' project")
	}

	workflows, err := client.Workflows.List(ctx)
	if err != nil {
		t.Fatalf("Workflows.List: %v", err)
	}
	found = false
	for _, w := range workflows {
		if w.Name == "kanban" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected builtin 'kanban' workflow")
	}
}

func TestNewClient_EmptyDBPath(t *testing.T) {
	_, err := NewClient(Config{})
	if err == nil {
		t.Fatal("expected error for empty DBPath")
	}
}

func TestClose(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	client, err := NewClient(Config{DBPath: dbPath})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	if err := client.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Operations after close should fail.
	ctx := context.Background()
	task := &domain.Task{Title: "After close"}
	if err := client.Tasks.Create(ctx, task); err == nil {
		t.Fatal("expected error after Close")
	}
}
```

- [ ] **Step 2: Verify tests fail to compile**

Run: `go test -v -run TestNewClient -count=1 .`
Expected: Compilation failure — `NewClient` and `Config` undefined (not implemented yet).

- [ ] **Step 3: Commit**

```bash
git add client_test.go
git commit -m "test: add failing tests for root Client type"
```

---

### Task 3: Implement NewClient and Close

**Files:**
- Create: `client.go`

- [ ] **Step 1: Write client.go**

```go
package tusk

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/germanamz/tusk/config"
	"github.com/germanamz/tusk/inmem"
	"github.com/germanamz/tusk/migrations"
	"github.com/germanamz/tusk/service"
	"github.com/germanamz/tusk/sqlite"
)

// Config holds configuration for creating a Client.
// Consumers build this programmatically — no file loading, no env vars.
type Config struct {
	// DBPath is the path to the SQLite database file. Required.
	DBPath string

	// Workflows defines workflow status sets and transitions.
	// When nil or empty, the builtin kanban workflow is used.
	Workflows map[string]config.WorkflowConfig

	// Projects defines projects and their workflow assignments.
	// When nil or empty, the builtin default project is used.
	Projects map[string]config.ProjectConfig

	// Urgency holds weights for the urgency scoring algorithm.
	// When zero-valued, default weights are used.
	Urgency config.UrgencyConfig
}

// Client provides access to all tusk services, backed by a SQLite database.
type Client struct {
	Tasks     *service.TaskService
	Tags      *service.TagService
	Relations *service.RelationService
	Projects  *service.ProjectService
	Workflows *service.WorkflowService
	Players   *service.PlayerService

	store *sqlite.Store
}

// defaultWorkflows returns the builtin kanban workflow definition.
func defaultWorkflows() map[string]config.WorkflowConfig {
	return map[string]config.WorkflowConfig{
		"kanban": {
			Statuses: []string{"pending", "active", "completed", "deleted"},
			Transitions: []config.WorkflowTransitionConfig{
				{From: "pending", To: "active"},
				{From: "pending", To: "deleted"},
				{From: "active", To: "completed"},
				{From: "active", To: "pending"},
				{From: "active", To: "deleted"},
				{From: "completed", To: "pending"},
			},
			HighlightStatuses: []string{"active"},
			DimStatuses:       []string{"completed", "deleted"},
		},
	}
}

// defaultProjects returns the builtin default project definition.
func defaultProjects() map[string]config.ProjectConfig {
	return map[string]config.ProjectConfig{
		"default": {
			Workflow: "kanban",
		},
	}
}

// defaultUrgency returns the builtin urgency weights.
func defaultUrgency() config.UrgencyConfig {
	return config.UrgencyConfig{
		PriorityWeight:    6.0,
		DueWeight:         12.0,
		AgeWeight:         2.0,
		ActiveWeight:      4.0,
		BlockingWeight:    8.0,
		BlockedWeight:     -5.0,
		TagsWeight:        1.0,
		ProjectWeight:     1.0,
		AnnotationsWeight: 1.0,
		WaitingWeight:     -3.0,
	}
}

// NewClient creates a Client backed by a SQLite database at cfg.DBPath.
// It opens the database, runs migrations, and wires all services.
// Call Close when done.
func NewClient(cfg Config) (*Client, error) {
	if cfg.DBPath == "" {
		return nil, fmt.Errorf("tusk: DBPath is required")
	}

	// Apply defaults for zero-valued config fields.
	if len(cfg.Workflows) == 0 {
		cfg.Workflows = defaultWorkflows()
	}
	if len(cfg.Projects) == 0 {
		cfg.Projects = defaultProjects()
	}
	if cfg.Urgency == (config.UrgencyConfig{}) {
		cfg.Urgency = defaultUrgency()
	}

	// Validate cross-references (projects must reference existing workflows).
	validationCfg := config.Config{
		Workflows: cfg.Workflows,
		Projects:  cfg.Projects,
	}
	if err := validationCfg.Validate(); err != nil {
		return nil, fmt.Errorf("tusk: invalid config: %w", err)
	}

	// Ensure the parent directory exists.
	dir := filepath.Dir(cfg.DBPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("tusk: creating data directory %s: %w", dir, err)
	}

	// Open SQLite with WAL mode, auto-migrate.
	store, err := sqlite.New(cfg.DBPath, migrations.FS)
	if err != nil {
		return nil, fmt.Errorf("tusk: opening database: %w", err)
	}

	db := store.DB()

	// Create repositories.
	taskRepo := sqlite.NewTaskRepo(db)
	annotationRepo := sqlite.NewAnnotationRepo(db)
	tagRepo := sqlite.NewTagRepo(db)
	relationRepo := sqlite.NewRelationRepo(db)
	playerRepo := sqlite.NewPlayerRepo(db)
	projectRepo := inmem.NewProjectRepository(cfg.Projects)
	workflowRepo := inmem.NewWorkflowRepository(cfg.Workflows)

	// Create services.
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
	taskSvc := service.NewTaskService(
		taskRepo, annotationRepo, relationRepo, tagRepo,
		projectRepo, workflowSvc, store, urgencyEngine, playerRepo,
	)
	tagSvc := service.NewTagService(tagRepo)
	relationSvc := service.NewRelationService(relationRepo, taskRepo, store)
	projectSvc := service.NewProjectService(projectRepo)
	playerSvc := service.NewPlayerService(playerRepo)

	return &Client{
		Tasks:     taskSvc,
		Tags:      tagSvc,
		Relations: relationSvc,
		Projects:  projectSvc,
		Workflows: workflowSvc,
		Players:   playerSvc,
		store:     store,
	}, nil
}

// Close releases the underlying database connection.
func (c *Client) Close() error {
	return c.store.Close()
}
```

- [ ] **Step 2: Run the tests**

Run: `go test -v -run TestNewClient -count=1 .`
Expected: All 3 `TestNewClient_*` tests pass.

Run: `go test -v -run TestClose -count=1 .`
Expected: `TestClose` passes.

- [ ] **Step 3: Run the full test suite**

Run: `make test`
Expected: All existing tests still pass — no regressions.

- [ ] **Step 4: Commit**

```bash
git add client.go client_test.go
git commit -m "feat: add root Client type for programmatic access"
```

---

### Task 4: Final verification

- [ ] **Step 1: Run tests with race detector**

Run: `make test-race`
Expected: All tests pass with no data races.

- [ ] **Step 2: Run linter**

Run: `make lint`
Expected: No new lint warnings.

- [ ] **Step 3: Run vet**

Run: `make vet`
Expected: Clean.
