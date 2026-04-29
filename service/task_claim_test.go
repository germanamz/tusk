package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/repository"
	"github.com/germanamz/tusk/service"
	"github.com/germanamz/tusk/sqlite"
	"github.com/germanamz/tusk/sqlite/sqlitetest"
	"github.com/google/uuid"
)

// storeWriteTx adapts a *sqlite.Store to service.WriteTxProvider for external
// service_test tests. Mirrors the production adapter in cmd/tusk/main.go.
type storeWriteTx struct{ store *sqlite.Store }

type storeWriteTxAdapter struct{ tx *sqlite.Tx }

func (w *storeWriteTxAdapter) Tasks() repository.TaskRepository         { return w.tx.Tasks() }
func (w *storeWriteTxAdapter) Relations() repository.RelationRepository { return w.tx.Relations() }
func (w *storeWriteTxAdapter) Events() repository.EventRepository       { return w.tx.Events(10000, 1000) }

func (w *storeWriteTxAdapter) Projects() repository.ProjectRepository   { return w.tx.Projects() }
func (w *storeWriteTxAdapter) Workflows() repository.WorkflowRepository { return w.tx.Workflows() }
func (w *storeWriteTxAdapter) Players() repository.PlayerRepository     { return w.tx.Players() }
func (w *storeWriteTxAdapter) Tags() repository.TagRepository           { return w.tx.Tags() }
func (w *storeWriteTxAdapter) Annotations() repository.AnnotationRepository {
	return w.tx.Annotations()
}
func (w *storeWriteTxAdapter) Notes() repository.NoteRepository { return w.tx.Notes() }

func (w *storeWriteTxAdapter) TruncateAll(ctx context.Context) error { return w.tx.TruncateAll(ctx) }

func (provider *storeWriteTx) WithTx(ctx context.Context, fn func(tx service.WriteTx) error) error {
	return provider.store.WithTx(ctx, func(stx *sqlite.Tx) error {
		return fn(&storeWriteTxAdapter{tx: stx})
	})
}

// newClaimTestEnv creates a full service environment for claim tests.
func newClaimTestEnv(test *testing.T) (*service.TaskService, *service.PlayerService) {
	test.Helper()
	store, projectRepo, workflowRepo := sqlitetest.NewStore(test)

	db := store.DB()
	playerRepo := sqlite.NewPlayerRepo(db)
	bundle := &service.RepoBundle{
		Store:       store,
		WriteTx:     &storeWriteTx{store: store},
		Tasks:       sqlite.NewTaskRepo(db),
		Annotations: sqlite.NewAnnotationRepo(db),
		Relations:   sqlite.NewRelationRepo(db),
		Tags:        sqlite.NewTagRepo(db),
		Players:     playerRepo,
	}

	workflowSvc := service.NewWorkflowService(workflowRepo, projectRepo)

	resolver := func(_ context.Context, _ uuid.UUID) (*service.RepoBundle, error) {
		return bundle, nil
	}
	projects := func(context.Context) ([]uuid.UUID, error) {
		return []uuid.UUID{domain.DefaultProjectUUID}, nil
	}
	taskSvc := service.NewTaskService(resolver, projects, projectRepo, nil, workflowSvc, nil)
	playerSvc := service.NewPlayerService(playerRepo)

	return taskSvc, playerSvc
}

func createTestTask(test *testing.T, svc *service.TaskService, title string) *domain.Task {
	test.Helper()
	ctx := context.Background()
	task := &domain.Task{Title: title}

	if err := svc.Create(ctx, task); err != nil {
		test.Fatalf("Create task: %v", err)
	}

	return task
}

func TestTaskService_Claim(test *testing.T) {
	taskSvc, playerSvc := newClaimTestEnv(test)
	ctx := context.Background()

	playerSvc.Register(ctx, "agent-1", "agent")
	task := createTestTask(test, taskSvc, "Claimable task")

	claimed, err := taskSvc.Claim(ctx, task.ShortID, "agent-1", task.Version)

	if err != nil {
		test.Fatalf("Claim: %v", err)
	}

	if claimed.ClaimedBy == nil || *claimed.ClaimedBy != "agent-1" {
		test.Errorf("ClaimedBy: got %v, want agent-1", claimed.ClaimedBy)
	}

	if claimed.ClaimedAt == nil {
		test.Error("ClaimedAt should be set")
	}
}

func TestTaskService_Claim_AlreadyClaimed(test *testing.T) {
	taskSvc, playerSvc := newClaimTestEnv(test)
	ctx := context.Background()

	playerSvc.Register(ctx, "agent-1", "agent")
	playerSvc.Register(ctx, "agent-2", "agent")
	task := createTestTask(test, taskSvc, "Contested task")

	claimed, _ := taskSvc.Claim(ctx, task.ShortID, "agent-1", task.Version)

	_, err := taskSvc.Claim(ctx, task.ShortID, "agent-2", claimed.Version)

	if !errors.Is(err, domain.ErrTaskClaimed) {
		test.Fatalf("Claim by agent-2: got %v, want ErrTaskClaimed", err)
	}
}

func TestTaskService_Claim_SamePlayer(test *testing.T) {
	taskSvc, playerSvc := newClaimTestEnv(test)
	ctx := context.Background()

	playerSvc.Register(ctx, "agent-1", "agent")
	task := createTestTask(test, taskSvc, "Re-claimable task")

	claimed, _ := taskSvc.Claim(ctx, task.ShortID, "agent-1", task.Version)

	// Same player re-claiming should succeed (idempotent)
	reclaimed, err := taskSvc.Claim(ctx, task.ShortID, "agent-1", claimed.Version)

	if err != nil {
		test.Fatalf("re-Claim same player: %v", err)
	}

	if *reclaimed.ClaimedBy != "agent-1" {
		test.Errorf("ClaimedBy: got %v, want agent-1", *reclaimed.ClaimedBy)
	}
}

func TestTaskService_Release(test *testing.T) {
	taskSvc, playerSvc := newClaimTestEnv(test)
	ctx := context.Background()

	playerSvc.Register(ctx, "agent-1", "agent")
	task := createTestTask(test, taskSvc, "Releasable task")

	claimed, _ := taskSvc.Claim(ctx, task.ShortID, "agent-1", task.Version)

	released, err := taskSvc.Release(ctx, task.ShortID, "agent-1", claimed.Version)

	if err != nil {
		test.Fatalf("Release: %v", err)
	}

	if released.ClaimedBy != nil {
		test.Errorf("ClaimedBy should be nil after release, got %v", *released.ClaimedBy)
	}

	if released.ClaimedAt != nil {
		test.Errorf("ClaimedAt should be nil after release")
	}
}

func TestTaskService_Release_WrongPlayer(test *testing.T) {
	taskSvc, playerSvc := newClaimTestEnv(test)
	ctx := context.Background()

	playerSvc.Register(ctx, "agent-1", "agent")
	playerSvc.Register(ctx, "agent-2", "agent")
	task := createTestTask(test, taskSvc, "Guarded task")

	claimed, _ := taskSvc.Claim(ctx, task.ShortID, "agent-1", task.Version)

	_, err := taskSvc.Release(ctx, task.ShortID, "agent-2", claimed.Version)

	if err == nil {
		test.Fatal("Release by wrong player should fail")
	}
}

func TestTaskService_Start_AutoClaim(test *testing.T) {
	taskSvc, playerSvc := newClaimTestEnv(test)
	ctx := context.Background()

	playerSvc.Register(ctx, "agent-1", "agent")
	task := createTestTask(test, taskSvc, "Auto-claim task")

	started, err := taskSvc.Start(ctx, task.ShortID, task.Version, "agent-1")

	if err != nil {
		test.Fatalf("Start with player: %v", err)
	}

	if started.ClaimedBy == nil || *started.ClaimedBy != "agent-1" {
		test.Errorf("auto-claim: ClaimedBy should be agent-1, got %v", started.ClaimedBy)
	}

	if started.Status != "active" {
		test.Errorf("status should be active, got %s", started.Status)
	}
}

func TestTaskService_Start_ClaimedByOther(test *testing.T) {
	taskSvc, playerSvc := newClaimTestEnv(test)
	ctx := context.Background()

	playerSvc.Register(ctx, "agent-1", "agent")
	playerSvc.Register(ctx, "agent-2", "agent")
	task := createTestTask(test, taskSvc, "Contested start")

	claimed, _ := taskSvc.Claim(ctx, task.ShortID, "agent-1", task.Version)

	_, err := taskSvc.Start(ctx, task.ShortID, claimed.Version, "agent-2")

	if !errors.Is(err, domain.ErrTaskClaimed) {
		test.Fatalf("Start by other player: got %v, want ErrTaskClaimed", err)
	}
}

func TestTaskService_Start_NoPlayer(test *testing.T) {
	taskSvc, _ := newClaimTestEnv(test)
	ctx := context.Background()

	task := createTestTask(test, taskSvc, "No player start")

	// Empty player ID — should work as before (no claim logic)
	started, err := taskSvc.Start(ctx, task.ShortID, task.Version, "")

	if err != nil {
		test.Fatalf("Start without player: %v", err)
	}

	if started.ClaimedBy != nil {
		test.Errorf("ClaimedBy should be nil when no player specified")
	}
}
