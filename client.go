package tusk

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/germanamz/tusk/config"
	"github.com/germanamz/tusk/inmem"
	"github.com/germanamz/tusk/migrations"
	"github.com/germanamz/tusk/service"
	"github.com/germanamz/tusk/sqlite"
	"github.com/google/uuid"
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
			Statuses: map[string]config.StatusConfig{
				"pending":   {Roles: []string{config.RoleInitial}},
				"active":    {Roles: []string{config.RoleStart, config.RoleHighlight}},
				"completed": {Roles: []string{config.RoleTerminal, config.RoleDone, config.RoleDim}},
				"deleted":   {Roles: []string{config.RoleTerminal, config.RoleDelete, config.RoleDim}},
			},
			Transitions: []config.WorkflowTransitionConfig{
				{From: "pending", To: "active"},
				{From: "pending", To: "deleted"},
				{From: "active", To: "completed"},
				{From: "active", To: "pending"},
				{From: "active", To: "deleted"},
				{From: "completed", To: "pending"},
			},
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

	// Programmatic clients use a single-store bundle resolved from the
	// same SQLite file. Per-project per-file routing is a CLI-only
	// concern for now.
	bundle := &service.RepoBundle{
		Store:       store,
		Tasks:       sqlite.NewTaskRepo(db),
		Annotations: sqlite.NewAnnotationRepo(db),
		Relations:   sqlite.NewRelationRepo(db),
		Tags:        sqlite.NewTagRepo(db),
		Players:     sqlite.NewPlayerRepo(db),
	}
	projectRepo := inmem.NewProjectRepository(cfg.Projects)

	resolver := func(context.Context, uuid.UUID) (*service.RepoBundle, error) {
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
	taskSvc := service.NewTaskService(resolver, projectLister, projectRepo, workflowSvc, urgencyEngine)
	tagSvc := service.NewTagService(resolver)
	relationSvc := service.NewRelationService(resolver, projectLister)
	projectSvc := service.NewProjectService(projectRepo)
	playerSvc := service.NewPlayerService(bundle.Players)

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
