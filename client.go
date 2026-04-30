package tusk

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/germanamz/tusk/config"
	"github.com/germanamz/tusk/migrations"
	"github.com/germanamz/tusk/repository"
	"github.com/germanamz/tusk/service"
	"github.com/germanamz/tusk/sqlite"
	"github.com/google/uuid"
)

// Config holds configuration for creating a Client.
// Consumers build this programmatically — no file loading, no env vars.
// Projects and workflows are managed exclusively in the database; the
// built-in kanban workflow and default project are seeded by migrations,
// and subsequent changes go through the Projects/Workflows services.
type Config struct {
	// DBPath is the path to the SQLite database file. Required.
	DBPath string

	// Urgency holds weights for the urgency scoring algorithm.
	// When zero-valued, default weights are used.
	Urgency config.UrgencyConfig

	// Notes controls note listing defaults.
	// When zero-valued, defaults are used.
	Notes config.NotesConfig

	// Events controls event-log retention.
	// When zero-valued, defaults are used.
	Events config.EventsConfig

	// Taxonomy supplies the workspace-level task level taxonomy consulted by
	// ProjectService.EffectiveTaxonomy. Leave zero-valued to disable workspace
	// taxonomy; projects can still carry per-project overrides via their
	// ProjectSettings.Taxonomy.
	Taxonomy config.TaxonomyConfig
}

// Client provides access to all tusk services, backed by a SQLite database.
type Client struct {
	Tasks       *service.TaskService
	Tags        *service.TagService
	Relations   *service.RelationService
	Projects    *service.ProjectService
	Workflows   *service.WorkflowService
	Players     *service.PlayerService
	Notes       *service.NoteService
	Portability *service.PortabilityService

	store *sqlite.Store
}

// tuskVersionForClient returns the version string library consumers see
// in exported PortableWorkspace headers. The CLI replaces this with a
// build-injected value when constructing its own Client; library use
// surfaces a stable placeholder because tusk_version is informational.
func tuskVersionForClient() string { return "library" }

// defaultEvents returns the builtin event-log retention defaults.
func defaultEvents() config.EventsConfig {
	return config.EventsConfig{
		MaxEvents:  10000,
		PruneSlack: 1000,
	}
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

	if cfg.Urgency == (config.UrgencyConfig{}) {
		cfg.Urgency = defaultUrgency()
	}

	if cfg.Events == (config.EventsConfig{}) {
		cfg.Events = defaultEvents()
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
	projectRepo := sqlite.NewProjectRepo(db)
	workflowRepo := sqlite.NewWorkflowRepo(db)

	resolver := func(context.Context, uuid.UUID) (*service.RepoBundle, error) {
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
	projectSvcCfg := &config.Config{
		Urgency:  cfg.Urgency,
		Notes:    cfg.Notes,
		Events:   cfg.Events,
		Taxonomy: cfg.Taxonomy,
	}
	projectSvc := service.NewProjectService(projectRepo, bundle.Tasks, store, projectDefaults, projectSvcCfg)

	taskSvc := service.NewTaskService(resolver, projectLister, projectRepo, projectSvc, workflowSvc, urgencyEngine)
	tagSvc := service.NewTagService(resolver)
	relationSvc := service.NewRelationService(resolver, projectLister)
	playerSvc := service.NewPlayerService(bundle.Players)
	windowSize := cfg.Notes.WindowSize
	if windowSize <= 0 {
		windowSize = 20
	}
	noteSvc := service.NewNoteService(bundle.Notes, bundle.Players, projectRepo, bundle.Tasks, windowSize)

	portabilitySvc := service.NewPortabilityService(
		writeTx,
		taskSvc, projectSvc, workflowSvc, relationSvc,
		tagSvc, playerSvc, noteSvc,
		bundle,
		tuskVersionForClient(),
	)

	return &Client{
		Tasks:       taskSvc,
		Tags:        tagSvc,
		Relations:   relationSvc,
		Projects:    projectSvc,
		Workflows:   workflowSvc,
		Players:     playerSvc,
		Notes:       noteSvc,
		Portability: portabilitySvc,
		store:       store,
	}, nil
}

// Close releases the underlying database connection.
func (client *Client) Close() error {
	return client.store.Close()
}
