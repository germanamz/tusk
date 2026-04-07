package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/germanamz/tusk/internal/config"
	"github.com/germanamz/tusk/internal/domain"
	"github.com/germanamz/tusk/internal/inmem"
	"github.com/germanamz/tusk/internal/service"
	"github.com/germanamz/tusk/internal/sqlite"
	"github.com/germanamz/tusk/migrations"
)

// newClaimTestEnv creates a full service environment for claim tests.
func newClaimTestEnv(t *testing.T) (*service.TaskService, *service.PlayerService) {
	t.Helper()
	store, err := sqlite.New(t.TempDir()+"/test.db", migrations.FS)
	if err != nil {
		t.Fatalf("opening test db: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	db := store.DB()
	taskRepo := sqlite.NewTaskRepo(db)
	annotationRepo := sqlite.NewAnnotationRepo(db)
	tagRepo := sqlite.NewTagRepo(db)
	relationRepo := sqlite.NewRelationRepo(db)
	playerRepo := sqlite.NewPlayerRepo(db)

	projectRepo := inmem.NewProjectRepository(map[string]config.ProjectConfig{
		"default": {
			Workflow: "kanban",
		},
	})
	workflowRepo := inmem.NewWorkflowRepository(map[string]config.WorkflowConfig{
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
		},
	})
	workflowSvc := service.NewWorkflowService(workflowRepo, projectRepo)

	taskSvc := service.NewTaskService(taskRepo, annotationRepo, relationRepo, tagRepo, projectRepo, workflowSvc, store, nil, playerRepo)
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
