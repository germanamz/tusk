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

func (p *storeWriteTx) WithTx(ctx context.Context, fn func(tx service.WriteTx) error) error {
	return p.store.WithTx(ctx, func(stx *sqlite.Tx) error {
		return fn(&storeWriteTxAdapter{tx: stx})
	})
}

// newClaimTestEnv creates a full service environment for claim tests.
func newClaimTestEnv(t *testing.T) (*service.TaskService, *service.PlayerService) {
	t.Helper()
	store, projectRepo, workflowRepo := sqlitetest.NewStore(t)

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

func createTestTask(t *testing.T, svc *service.TaskService, title string) *domain.Task {
	t.Helper()
	ctx := context.Background()
	task := &domain.Task{Title: title}
	if err := svc.Create(ctx, task); err != nil {
		t.Fatalf("Create task: %v", err)
	}
	return task
}

func TestTaskService_Claim(t *testing.T) {
	taskSvc, playerSvc := newClaimTestEnv(t)
	ctx := context.Background()

	playerSvc.Register(ctx, "agent-1", "agent")
	task := createTestTask(t, taskSvc, "Claimable task")

	claimed, err := taskSvc.Claim(ctx, task.ShortID, "agent-1", task.Version)
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if claimed.ClaimedBy == nil || *claimed.ClaimedBy != "agent-1" {
		t.Errorf("ClaimedBy: got %v, want agent-1", claimed.ClaimedBy)
	}
	if claimed.ClaimedAt == nil {
		t.Error("ClaimedAt should be set")
	}
}

func TestTaskService_Claim_AlreadyClaimed(t *testing.T) {
	taskSvc, playerSvc := newClaimTestEnv(t)
	ctx := context.Background()

	playerSvc.Register(ctx, "agent-1", "agent")
	playerSvc.Register(ctx, "agent-2", "agent")
	task := createTestTask(t, taskSvc, "Contested task")

	claimed, _ := taskSvc.Claim(ctx, task.ShortID, "agent-1", task.Version)

	_, err := taskSvc.Claim(ctx, task.ShortID, "agent-2", claimed.Version)
	if !errors.Is(err, domain.ErrTaskClaimed) {
		t.Fatalf("Claim by agent-2: got %v, want ErrTaskClaimed", err)
	}
}

func TestTaskService_Claim_SamePlayer(t *testing.T) {
	taskSvc, playerSvc := newClaimTestEnv(t)
	ctx := context.Background()

	playerSvc.Register(ctx, "agent-1", "agent")
	task := createTestTask(t, taskSvc, "Re-claimable task")

	claimed, _ := taskSvc.Claim(ctx, task.ShortID, "agent-1", task.Version)

	// Same player re-claiming should succeed (idempotent)
	reclaimed, err := taskSvc.Claim(ctx, task.ShortID, "agent-1", claimed.Version)
	if err != nil {
		t.Fatalf("re-Claim same player: %v", err)
	}
	if *reclaimed.ClaimedBy != "agent-1" {
		t.Errorf("ClaimedBy: got %v, want agent-1", *reclaimed.ClaimedBy)
	}
}

func TestTaskService_Release(t *testing.T) {
	taskSvc, playerSvc := newClaimTestEnv(t)
	ctx := context.Background()

	playerSvc.Register(ctx, "agent-1", "agent")
	task := createTestTask(t, taskSvc, "Releasable task")

	claimed, _ := taskSvc.Claim(ctx, task.ShortID, "agent-1", task.Version)

	released, err := taskSvc.Release(ctx, task.ShortID, "agent-1", claimed.Version)
	if err != nil {
		t.Fatalf("Release: %v", err)
	}
	if released.ClaimedBy != nil {
		t.Errorf("ClaimedBy should be nil after release, got %v", *released.ClaimedBy)
	}
	if released.ClaimedAt != nil {
		t.Errorf("ClaimedAt should be nil after release")
	}
}

func TestTaskService_Release_WrongPlayer(t *testing.T) {
	taskSvc, playerSvc := newClaimTestEnv(t)
	ctx := context.Background()

	playerSvc.Register(ctx, "agent-1", "agent")
	playerSvc.Register(ctx, "agent-2", "agent")
	task := createTestTask(t, taskSvc, "Guarded task")

	claimed, _ := taskSvc.Claim(ctx, task.ShortID, "agent-1", task.Version)

	_, err := taskSvc.Release(ctx, task.ShortID, "agent-2", claimed.Version)
	if err == nil {
		t.Fatal("Release by wrong player should fail")
	}
}

func TestTaskService_Start_AutoClaim(t *testing.T) {
	taskSvc, playerSvc := newClaimTestEnv(t)
	ctx := context.Background()

	playerSvc.Register(ctx, "agent-1", "agent")
	task := createTestTask(t, taskSvc, "Auto-claim task")

	started, err := taskSvc.Start(ctx, task.ShortID, task.Version, "agent-1")
	if err != nil {
		t.Fatalf("Start with player: %v", err)
	}
	if started.ClaimedBy == nil || *started.ClaimedBy != "agent-1" {
		t.Errorf("auto-claim: ClaimedBy should be agent-1, got %v", started.ClaimedBy)
	}
	if started.Status != "active" {
		t.Errorf("status should be active, got %s", started.Status)
	}
}

func TestTaskService_Start_ClaimedByOther(t *testing.T) {
	taskSvc, playerSvc := newClaimTestEnv(t)
	ctx := context.Background()

	playerSvc.Register(ctx, "agent-1", "agent")
	playerSvc.Register(ctx, "agent-2", "agent")
	task := createTestTask(t, taskSvc, "Contested start")

	claimed, _ := taskSvc.Claim(ctx, task.ShortID, "agent-1", task.Version)

	_, err := taskSvc.Start(ctx, task.ShortID, claimed.Version, "agent-2")
	if !errors.Is(err, domain.ErrTaskClaimed) {
		t.Fatalf("Start by other player: got %v, want ErrTaskClaimed", err)
	}
}

func TestTaskService_Start_NoPlayer(t *testing.T) {
	taskSvc, _ := newClaimTestEnv(t)
	ctx := context.Background()

	task := createTestTask(t, taskSvc, "No player start")

	// Empty player ID — should work as before (no claim logic)
	started, err := taskSvc.Start(ctx, task.ShortID, task.Version, "")
	if err != nil {
		t.Fatalf("Start without player: %v", err)
	}
	if started.ClaimedBy != nil {
		t.Errorf("ClaimedBy should be nil when no player specified")
	}
}
