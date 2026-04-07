# Phase 2: Player Service, Task Claiming & CLI

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add PlayerService for registration, extend TaskService with Claim/Release methods and auto-claim on Start, wire into CLI with `--player` flag, `tusk player register`, `tusk claim`, `tusk release` commands, and claim field display.

**Architecture:** PlayerService is a thin wrapper around PlayerRepository. TaskService gains `playerRepo` dependency for claim validation and `last_seen_at` updates. CLI gets a `--player` persistent flag parsed in main.go (same pattern as `--db`), and new commands for player registration and claim management.

**Tech Stack:** Go, Cobra, SQLite

**Prerequisites:** Phase 1 must be completed. Phase 1 introduced: `domain.Player`, `domain.ErrTaskClaimed`, `ClaimedBy`/`ClaimedAt` on `Task` and `TaskUpdate`, `repository.PlayerRepository`, `sqlite.PlayerRepo`, migration 002.

**Design Spec:** `docs/superpowers/specs/2026-04-06-player-entity-registration-design.md`

---

## Inherits From

**Phase 1** added:
- `internal/domain/player.go` — Player struct with `ID`, `Type`, `RegisteredAt`, `LastSeenAt`
- `internal/domain/errors.go` — `ErrTaskClaimed` sentinel
- `internal/domain/task.go` — `ClaimedBy *string` and `ClaimedAt *time.Time` on Task; `ClaimedBy **string` and `ClaimedAt **time.Time` on TaskUpdate
- `internal/repository/player.go` — `PlayerRepository` interface with `Create`, `GetByID`, `UpdateLastSeen`, `List`
- `internal/sqlite/player.go` — `PlayerRepo` implementing the interface
- `internal/sqlite/task.go` — taskColumns, scanTask, Create, Update all handle `claimed_by`/`claimed_at`
- `migrations/002_players.up.sql` — `players` table and task claim columns

---

### Task 1: PlayerService

**Files:**
- Create: `internal/service/player.go`
- Create: `internal/service/player_test.go`

- [ ] **Step 1: Write tests in `internal/service/player_test.go`**

```go
package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/germanamz/tusk/internal/domain"
	"github.com/germanamz/tusk/internal/service"
	"github.com/germanamz/tusk/internal/sqlite"
	"github.com/germanamz/tusk/migrations"
)

func newPlayerTestEnv(t *testing.T) (*service.PlayerService, *sqlite.PlayerRepo) {
	t.Helper()
	store, err := sqlite.New(t.TempDir()+"/test.db", migrations.FS)
	if err != nil {
		t.Fatalf("opening test db: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	repo := sqlite.NewPlayerRepo(store.DB())
	svc := service.NewPlayerService(repo)
	return svc, repo
}

func TestPlayerService_Register(t *testing.T) {
	svc, _ := newPlayerTestEnv(t)
	ctx := context.Background()

	p, err := svc.Register(ctx, "agent-1", "agent")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if p.ID != "agent-1" {
		t.Errorf("got ID %q, want %q", p.ID, "agent-1")
	}
	if p.Type != "agent" {
		t.Errorf("got Type %q, want %q", p.Type, "agent")
	}
	if p.RegisteredAt.IsZero() {
		t.Error("RegisteredAt should be set")
	}
}

func TestPlayerService_Register_InvalidType(t *testing.T) {
	svc, _ := newPlayerTestEnv(t)
	ctx := context.Background()

	_, err := svc.Register(ctx, "bad", "robot")
	if err == nil {
		t.Fatal("expected error for invalid type")
	}
}

func TestPlayerService_Register_EmptyID(t *testing.T) {
	svc, _ := newPlayerTestEnv(t)
	ctx := context.Background()

	_, err := svc.Register(ctx, "", "human")
	if err == nil {
		t.Fatal("expected error for empty ID")
	}
}

func TestPlayerService_Register_Duplicate(t *testing.T) {
	svc, _ := newPlayerTestEnv(t)
	ctx := context.Background()

	if _, err := svc.Register(ctx, "dup", "human"); err != nil {
		t.Fatalf("first Register: %v", err)
	}
	_, err := svc.Register(ctx, "dup", "human")
	if err != domain.ErrConflict {
		t.Fatalf("second Register: got %v, want ErrConflict", err)
	}
}

func TestPlayerService_GetByID(t *testing.T) {
	svc, _ := newPlayerTestEnv(t)
	ctx := context.Background()

	svc.Register(ctx, "lookup", "human")
	p, err := svc.GetByID(ctx, "lookup")
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if p.ID != "lookup" {
		t.Errorf("got %q, want %q", p.ID, "lookup")
	}
}

func TestPlayerService_UpdateLastSeen(t *testing.T) {
	svc, _ := newPlayerTestEnv(t)
	ctx := context.Background()

	p, _ := svc.Register(ctx, "seen", "agent")
	original := p.LastSeenAt

	time.Sleep(10 * time.Millisecond)
	if err := svc.UpdateLastSeen(ctx, "seen"); err != nil {
		t.Fatalf("UpdateLastSeen: %v", err)
	}

	updated, _ := svc.GetByID(ctx, "seen")
	if !updated.LastSeenAt.After(original) {
		t.Error("LastSeenAt should have been updated")
	}
}

func TestPlayerService_List(t *testing.T) {
	svc, _ := newPlayerTestEnv(t)
	ctx := context.Background()

	svc.Register(ctx, "a", "human")
	svc.Register(ctx, "b", "agent")
	players, err := svc.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(players) != 2 {
		t.Fatalf("got %d players, want 2", len(players))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test -v ./internal/service/ -run TestPlayerService
```

Expected: compilation error — `service.NewPlayerService` does not exist yet.

- [ ] **Step 3: Implement `internal/service/player.go`**

```go
package service

import (
	"context"
	"fmt"
	"time"

	"github.com/germanamz/tusk/internal/domain"
	"github.com/germanamz/tusk/internal/repository"
)

// PlayerService handles player registration and liveness tracking.
type PlayerService struct {
	repo repository.PlayerRepository
}

// NewPlayerService creates a new PlayerService.
func NewPlayerService(repo repository.PlayerRepository) *PlayerService {
	return &PlayerService{repo: repo}
}

// Register creates a new player. Type must be "human" or "agent".
// Returns domain.ErrConflict if a player with the same ID already exists.
func (s *PlayerService) Register(ctx context.Context, id, playerType string) (*domain.Player, error) {
	if id == "" {
		return nil, fmt.Errorf("player ID must not be empty")
	}
	if playerType != "human" && playerType != "agent" {
		return nil, fmt.Errorf("player type must be \"human\" or \"agent\", got %q", playerType)
	}

	now := time.Now().UTC().Truncate(time.Millisecond)
	player := &domain.Player{
		ID:           id,
		Type:         playerType,
		RegisteredAt: now,
		LastSeenAt:   now,
	}
	if err := s.repo.Create(ctx, player); err != nil {
		return nil, err
	}
	return player, nil
}

// GetByID retrieves a player by ID.
func (s *PlayerService) GetByID(ctx context.Context, id string) (*domain.Player, error) {
	return s.repo.GetByID(ctx, id)
}

// UpdateLastSeen refreshes a player's last_seen_at timestamp.
func (s *PlayerService) UpdateLastSeen(ctx context.Context, id string) error {
	return s.repo.UpdateLastSeen(ctx, id)
}

// List returns all registered players.
func (s *PlayerService) List(ctx context.Context) ([]*domain.Player, error) {
	return s.repo.List(ctx)
}
```

- [ ] **Step 4: Run tests**

```bash
go test -v ./internal/service/ -run TestPlayerService
```

Expected: all 6 tests pass.

- [ ] **Step 5: Commit**

```bash
git add internal/service/player.go internal/service/player_test.go
git commit -m "feat(service): add PlayerService with registration and liveness"
```

---

### Task 2: TaskService Claim and Release methods

**Files:**
- Modify: `internal/service/task.go` (struct, constructor, new methods)
- Create: `internal/service/task_claim_test.go`

- [ ] **Step 1: Write tests in `internal/service/task_claim_test.go`**

Use the existing test infrastructure from `internal/service/task_test.go` (if it has helper functions for creating a TaskService). If not, create a minimal helper. The tests need a full TaskService with a playerRepo wired in.

```go
package service_test

import (
	"context"
	"errors"
	"testing"

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

	projectRepo := inmem.NewProjectRepository(nil)
	workflowRepo := inmem.NewWorkflowRepository(nil)
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
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test -v ./internal/service/ -run "TestTaskService_Claim|TestTaskService_Release|TestTaskService_Start_Auto|TestTaskService_Start_Claimed|TestTaskService_Start_NoPlayer"
```

Expected: compilation errors — `Claim`, `Release` methods don't exist, `Start` has wrong signature.

- [ ] **Step 3: Add `playerRepo` to TaskService struct and constructor**

In `internal/service/task.go`, add `playerRepo` to the struct (after `urgencyEngine` at line 36):

```go
type TaskService struct {
	taskRepo       repository.TaskRepository
	annotationRepo repository.AnnotationRepository
	relationRepo   repository.RelationRepository
	tagRepo        repository.TagRepository
	projectRepo    repository.ProjectRepository
	workflowSvc    *WorkflowService
	txProvider     TaskTxProvider
	urgencyEngine  *UrgencyEngine
	playerRepo     repository.PlayerRepository
}
```

Update the constructor signature to add `playerRepo` as the last parameter:

```go
func NewTaskService(
	tr repository.TaskRepository,
	ar repository.AnnotationRepository,
	rr repository.RelationRepository,
	tagr repository.TagRepository,
	pr repository.ProjectRepository,
	ws *WorkflowService,
	txp TaskTxProvider,
	ue *UrgencyEngine,
	playerRepo repository.PlayerRepository,
) *TaskService {
	if ue != nil && (rr == nil || tagr == nil) {
		panic("NewTaskService: urgencyEngine requires relationRepo and tagRepo")
	}
	return &TaskService{
		taskRepo:       tr,
		annotationRepo: ar,
		relationRepo:   rr,
		tagRepo:        tagr,
		projectRepo:    pr,
		workflowSvc:    ws,
		txProvider:     txp,
		urgencyEngine:  ue,
		playerRepo:     playerRepo,
	}
}
```

**IMPORTANT:** This changes the `NewTaskService` signature. You must update ALL callers:

- `cmd/tusk/main.go` — add `playerRepo` argument (pass the `sqlite.NewPlayerRepo(db)` instance)
- Any test files that call `NewTaskService` — add `nil` as the last argument if player functionality is not needed

Search for all callers:

```bash
grep -rn "NewTaskService" --include="*.go"
```

Update each call site. For example, in `cmd/tusk/main.go` (line 78):

```go
playerRepo := sqlite.NewPlayerRepo(db)
taskSvc := service.NewTaskService(taskRepo, annotationRepo, relationRepo, tagRepo, projectRepo, workflowSvc, store, urgencyEngine, playerRepo)
```

For test files that create a TaskService without player support, pass `nil`:

```go
service.NewTaskService(taskRepo, annotationRepo, relationRepo, tagRepo, projectRepo, workflowSvc, store, urgencyEngine, nil)
```

- [ ] **Step 4: Change `Start` method signature to accept optional playerID**

In `internal/service/task.go`, change the `Start` method (line 452) from:

```go
func (s *TaskService) Start(ctx context.Context, shortID string, version int) (*domain.Task, error) {
	return s.Update(ctx, domain.TaskUpdate{
		ShortID: shortID,
		Version: version,
		Status:  ptr("active"),
	})
}
```

to:

```go
func (s *TaskService) Start(ctx context.Context, shortID string, version int, playerID string) (*domain.Task, error) {
	if playerID != "" && s.playerRepo != nil {
		// Validate player exists
		if _, err := s.playerRepo.GetByID(ctx, playerID); err != nil {
			return nil, fmt.Errorf("player %q: %w", playerID, err)
		}

		// Check if task is claimed by someone else
		task, err := s.taskRepo.GetByShortID(ctx, shortID)
		if err != nil {
			return nil, err
		}
		if task.ClaimedBy != nil && *task.ClaimedBy != playerID {
			return nil, domain.ErrTaskClaimed
		}

		// Auto-claim if unclaimed
		upd := domain.TaskUpdate{
			ShortID: shortID,
			Version: version,
			Status:  ptr("active"),
		}
		if task.ClaimedBy == nil {
			now := time.Now().UTC().Truncate(time.Millisecond)
			upd.ClaimedBy = &(&playerID)
			upd.ClaimedAt = &(&now)
		}

		result, err := s.Update(ctx, upd)
		if err != nil {
			return nil, err
		}

		s.playerRepo.UpdateLastSeen(ctx, playerID) //nolint:errcheck
		return result, nil
	}

	return s.Update(ctx, domain.TaskUpdate{
		ShortID: shortID,
		Version: version,
		Status:  ptr("active"),
	})
}
```

Note: The double-pointer `&(&playerID)` won't compile in Go. Use the `ptr` helper that already exists in the file:

```go
if task.ClaimedBy == nil {
	now := time.Now().UTC().Truncate(time.Millisecond)
	claimedBy := ptr(playerID)
	claimedAt := ptr(now)
	upd.ClaimedBy = &claimedBy
	upd.ClaimedAt = &claimedAt
}
```

**IMPORTANT:** This changes the `Start` method signature. Update ALL callers:

```bash
grep -rn "\.Start(" --include="*.go"
```

Every caller of `taskSvc.Start(ctx, shortID, version)` must become `taskSvc.Start(ctx, shortID, version, "")` (pass empty string for no-player behavior). This includes:
- `internal/tui/commands.go` — `runStart` method
- `internal/mcp/tools.go` — `handleTaskStart` handler
- Any test files

- [ ] **Step 5: Add `Claim` and `Release` methods**

Add to `internal/service/task.go`:

```go
// Claim assigns a task to a player. Returns ErrTaskClaimed if claimed by another player.
// Re-claiming by the same player is idempotent.
func (s *TaskService) Claim(ctx context.Context, shortID, playerID string, version int) (*domain.Task, error) {
	if s.playerRepo == nil {
		return nil, fmt.Errorf("player support not configured")
	}

	// Validate player exists
	if _, err := s.playerRepo.GetByID(ctx, playerID); err != nil {
		return nil, fmt.Errorf("player %q: %w", playerID, err)
	}

	task, err := s.taskRepo.GetByShortID(ctx, shortID)
	if err != nil {
		return nil, err
	}

	// Check current claim
	if task.ClaimedBy != nil && *task.ClaimedBy != playerID {
		return nil, domain.ErrTaskClaimed
	}

	// Already claimed by this player — idempotent re-claim
	if task.ClaimedBy != nil && *task.ClaimedBy == playerID {
		// Still do the update to bump version and refresh claimed_at
	}

	now := time.Now().UTC().Truncate(time.Millisecond)
	claimedBy := ptr(playerID)
	claimedAt := ptr(now)
	result, err := s.Update(ctx, domain.TaskUpdate{
		ShortID:   shortID,
		Version:   version,
		ClaimedBy: &claimedBy,
		ClaimedAt: &claimedAt,
	})
	if err != nil {
		return nil, err
	}

	s.playerRepo.UpdateLastSeen(ctx, playerID) //nolint:errcheck
	return result, nil
}

// Release clears a task's claim. Only the current claimant can release.
func (s *TaskService) Release(ctx context.Context, shortID, playerID string, version int) (*domain.Task, error) {
	if s.playerRepo == nil {
		return nil, fmt.Errorf("player support not configured")
	}

	task, err := s.taskRepo.GetByShortID(ctx, shortID)
	if err != nil {
		return nil, err
	}

	// Can only release if you are the claimant
	if task.ClaimedBy == nil {
		return nil, fmt.Errorf("task is not claimed")
	}
	if *task.ClaimedBy != playerID {
		return nil, fmt.Errorf("task is claimed by a different player")
	}

	var nilStr *string
	var nilTime *time.Time
	result, err := s.Update(ctx, domain.TaskUpdate{
		ShortID:   shortID,
		Version:   version,
		ClaimedBy: &nilStr,
		ClaimedAt: &nilTime,
	})
	if err != nil {
		return nil, err
	}

	s.playerRepo.UpdateLastSeen(ctx, playerID) //nolint:errcheck
	return result, nil
}
```

- [ ] **Step 6: Update the `Update` method to apply ClaimedBy/ClaimedAt from TaskUpdate**

In the `Update` method of `internal/service/task.go`, in the "Apply patch" section (around line 310), add handling for the new fields after the UDA block:

```go
if upd.ClaimedBy != nil {
	task.ClaimedBy = *upd.ClaimedBy
}
if upd.ClaimedAt != nil {
	task.ClaimedAt = *upd.ClaimedAt
}
```

- [ ] **Step 7: Run all tests**

```bash
go test ./internal/service/... -v
```

Expected: all tests pass (both existing and new claim tests).

- [ ] **Step 8: Commit**

```bash
git add internal/service/task.go internal/service/task_claim_test.go cmd/tusk/main.go
git add -u  # catch any other files with updated Start() calls
git commit -m "feat(service): add Claim/Release to TaskService, auto-claim on Start"
```

---

### Task 3: CLI `--player` global flag and `tusk player register`

**Files:**
- Modify: `cmd/tusk/main.go` — add `--player` flag stripping (same pattern as `--db`)
- Modify: `internal/tui/app.go` — add `playerSvc` field and `playerID` field, accept in constructor
- Modify: `internal/tui/commands.go` — add `buildPlayerCmd()`, wire into command tree

- [ ] **Step 1: Add `--player` flag parsing to `cmd/tusk/main.go`**

Follow the exact same pattern as `--db` / `stripDBFlag`. Add a `resolvePlayerID` function and a `stripPlayerFlag` function:

```go
// resolvePlayerID reads the --player flag from os.Args (before Cobra parsing).
func resolvePlayerID() string {
	for i, arg := range os.Args {
		if arg == "--player" {
			if i+1 < len(os.Args) {
				return os.Args[i+1]
			}
		}
		if strings.HasPrefix(arg, "--player=") {
			return arg[9:]
		}
	}
	return ""
}

// stripPlayerFlag removes --player and its value from args.
func stripPlayerFlag(args []string) []string {
	var out []string
	for i := 0; i < len(args); i++ {
		if args[i] == "--player" && i+1 < len(args) {
			i++ // skip value
			continue
		}
		if strings.HasPrefix(args[i], "--player=") {
			continue
		}
		out = append(out, args[i])
	}
	return out
}
```

In the `run()` function, after creating `playerRepo` and `playerSvc`, resolve the player ID and pass it to the TUI app. Update the `stripDBFlag` call to also strip `--player`:

```go
playerID := resolvePlayerID()

app := tui.New(taskSvc, tagSvc, relationSvc, projectSvc, workflowSvc, playerSvc, playerID, tui.VersionInfo{...}, cfg.TUI, cfg.MCP)
return app.Run(stripPlayerFlag(stripDBFlag(os.Args[1:])))
```

- [ ] **Step 2: Update `tui.App` to accept `playerSvc` and `playerID`**

In `internal/tui/app.go`, add fields to the `App` struct:

```go
type App struct {
	taskSvc     *service.TaskService
	tagSvc      *service.TagService
	relationSvc *service.RelationService
	projectSvc  *service.ProjectService
	workflowSvc *service.WorkflowService
	playerSvc   *service.PlayerService
	playerID    string // from --player flag
	resolver    *filter.Resolver
	root        *cobra.Command
	format      string
	noColor     bool
	version     VersionInfo
	tuiCfg      config.TUIConfig
	mcpCfg      config.MCPConfig
}
```

Update the `New` constructor signature to accept `playerSvc *service.PlayerService` and `playerID string`:

```go
func New(taskSvc *service.TaskService, tagSvc *service.TagService, relationSvc *service.RelationService, projectSvc *service.ProjectService, workflowSvc *service.WorkflowService, playerSvc *service.PlayerService, playerID string, vi VersionInfo, tuiCfg config.TUIConfig, mcpCfg config.MCPConfig) *App {
```

Wire the new fields in the constructor body. Also add the player command to the command tree:

```go
a.root.AddCommand(a.buildPlayerCmd())
```

And pass `playerSvc` to the MCP server creation (update the `mcpCmd` RunE). This will be a bridge — for now, pass it as nil since MCP player tools are Phase 3:

```go
// In the mcp serve command RunE — no change to MCP New() signature yet
mcpServer, err := tuskmcp.New(taskSvc, tagSvc, relationSvc, projectSvc, a.workflowSvc, vi.Version, a.mcpCfg)
```

- [ ] **Step 3: Add `buildPlayerCmd()` and `runPlayerRegister` to `internal/tui/commands.go`**

```go
// buildPlayerCmd creates the `tusk player` subcommand group.
func (a *App) buildPlayerCmd() *cobra.Command {
	registerCmd := &cobra.Command{
		Use:   "register <id>",
		Short: "Register a new player",
		Args:  cobra.ExactArgs(1),
		RunE:  a.runPlayerRegister,
	}
	registerCmd.Flags().String("type", "agent", `player type: "human" or "agent"`)

	playerCmd := &cobra.Command{
		Use:   "player",
		Short: "Player management",
	}
	playerCmd.AddCommand(registerCmd)
	return playerCmd
}

func (a *App) runPlayerRegister(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	id := args[0]
	playerType, _ := cmd.Flags().GetString("type")

	player, err := a.playerSvc.Register(ctx, id, playerType)
	if err != nil {
		return fmt.Errorf("%s", err)
	}

	r := NewRenderer(cmd.OutOrStdout(), a.format, a.colorEnabled(), nil)
	return r.renderPlayerResult("Registered", player)
}
```

- [ ] **Step 4: Add `renderPlayerResult` to `internal/tui/render.go`**

```go
// playerJSON is the JSON serialization format for a player.
type playerJSON struct {
	ID           string `json:"id"`
	Type         string `json:"type"`
	RegisteredAt string `json:"registered_at"`
	LastSeenAt   string `json:"last_seen_at"`
}

func toPlayerJSON(p *domain.Player) playerJSON {
	return playerJSON{
		ID:           p.ID,
		Type:         p.Type,
		RegisteredAt: p.RegisteredAt.Format(time.RFC3339),
		LastSeenAt:   p.LastSeenAt.Format(time.RFC3339),
	}
}

// renderPlayerResult writes a player mutation result.
func (r *Renderer) renderPlayerResult(action string, player *domain.Player) error {
	if r.format == "json" {
		enc := json.NewEncoder(r.w)
		enc.SetIndent("", "  ")
		return enc.Encode(toPlayerJSON(player))
	}
	_, err := fmt.Fprintf(r.w, "%s player %s (type: %s)\n", action, player.ID, player.Type)
	return err
}
```

- [ ] **Step 5: Build and verify**

```bash
go build ./...
```

Expected: compiles cleanly.

- [ ] **Step 6: Commit**

```bash
git add cmd/tusk/main.go internal/tui/app.go internal/tui/commands.go internal/tui/render.go
git commit -m "feat(tui): add --player flag and tusk player register command"
```

---

### Task 4: CLI `tusk claim` and `tusk release` commands

**Files:**
- Modify: `internal/tui/commands.go` — add claim/release to task commands

- [ ] **Step 1: Add claim and release commands to `buildTaskCmds()`**

In `internal/tui/commands.go`, add to the slice returned by `buildTaskCmds()`:

```go
{
	Use:   "claim <short_id>",
	Short: "Claim a task for the current player",
	Args:  cobra.ExactArgs(1),
	RunE:  a.runClaim,
},
{
	Use:   "release <short_id>",
	Short: "Release a task claim",
	Args:  cobra.ExactArgs(1),
	RunE:  a.runRelease,
},
```

- [ ] **Step 2: Implement `runClaim` and `runRelease` handlers**

```go
func (a *App) runClaim(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	shortID := args[0]

	if a.playerID == "" {
		return fmt.Errorf("--player flag is required for claim")
	}

	// Auto-register player if not already registered
	if err := a.ensurePlayer(ctx); err != nil {
		return err
	}

	current, err := a.taskSvc.GetByShortID(ctx, shortID)
	if err != nil {
		return fmt.Errorf("%s", formatError(err, shortID))
	}

	updated, err := a.taskSvc.Claim(ctx, shortID, a.playerID, current.Version)
	if err != nil {
		return fmt.Errorf("%s", formatClaimError(err, shortID))
	}

	r := NewRenderer(cmd.OutOrStdout(), a.format, a.colorEnabled(), nil)
	return r.renderMutationResult("Claimed", updated, nil)
}

func (a *App) runRelease(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	shortID := args[0]

	if a.playerID == "" {
		return fmt.Errorf("--player flag is required for release")
	}

	current, err := a.taskSvc.GetByShortID(ctx, shortID)
	if err != nil {
		return fmt.Errorf("%s", formatError(err, shortID))
	}

	updated, err := a.taskSvc.Release(ctx, shortID, a.playerID, current.Version)
	if err != nil {
		return fmt.Errorf("%s", formatClaimError(err, shortID))
	}

	r := NewRenderer(cmd.OutOrStdout(), a.format, a.colorEnabled(), nil)
	return r.renderMutationResult("Released", updated, nil)
}

// ensurePlayer auto-registers the current --player as "human" if not yet registered.
func (a *App) ensurePlayer(ctx context.Context) error {
	if a.playerSvc == nil || a.playerID == "" {
		return nil
	}
	_, err := a.playerSvc.GetByID(ctx, a.playerID)
	if err == nil {
		return nil // already registered
	}
	if errors.Is(err, domain.ErrNotFound) {
		_, regErr := a.playerSvc.Register(ctx, a.playerID, "human")
		if regErr != nil && !errors.Is(regErr, domain.ErrConflict) {
			return fmt.Errorf("auto-registering player: %w", regErr)
		}
		return nil
	}
	return fmt.Errorf("checking player: %w", err)
}

func formatClaimError(err error, shortID string) string {
	switch {
	case errors.Is(err, domain.ErrTaskClaimed):
		return fmt.Sprintf("Task %s is already claimed by another player", shortID)
	case errors.Is(err, domain.ErrNotFound):
		return fmt.Sprintf("Task not found: %s", shortID)
	case errors.Is(err, domain.ErrConflict):
		return "Version conflict - task was modified by another process"
	default:
		return err.Error()
	}
}
```

- [ ] **Step 3: Update `runStart` to pass `playerID`**

In `internal/tui/commands.go`, update `runStart`:

```go
func (a *App) runStart(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	shortID := args[0]

	// Auto-register player if --player is set
	if a.playerID != "" {
		if err := a.ensurePlayer(ctx); err != nil {
			return err
		}
	}

	current, err := a.taskSvc.GetByShortID(ctx, shortID)
	if err != nil {
		return fmt.Errorf("%s", formatError(err, shortID))
	}

	updated, err := a.taskSvc.Start(ctx, shortID, current.Version, a.playerID)
	if err != nil {
		if errors.Is(err, domain.ErrTaskClaimed) {
			return fmt.Errorf("%s", formatClaimError(err, shortID))
		}
		return fmt.Errorf("%s", formatError(err, shortID))
	}

	r := NewRenderer(cmd.OutOrStdout(), a.format, a.colorEnabled(), nil)
	return r.renderMutationResult("Started", updated, nil)
}
```

- [ ] **Step 4: Add claim fields to task display**

In `internal/tui/render.go`:

Add `ClaimedBy` and `ClaimedAt` to `taskJSON`:

```go
ClaimedBy *string `json:"claimed_by,omitempty"`
ClaimedAt *string `json:"claimed_at,omitempty"`
```

Update `toTaskJSON` to populate them:

```go
if t.ClaimedBy != nil {
	tj.ClaimedBy = t.ClaimedBy
}
if t.ClaimedAt != nil {
	s := t.ClaimedAt.Format(time.RFC3339)
	tj.ClaimedAt = &s
}
```

In the `renderTaskInfo` text output, add claim fields after the existing nullable fields (e.g., after `RecurrenceRule`):

```go
if task.ClaimedBy != nil {
	if _, err := fmt.Fprintf(r.w, "%s %s\n", r.paddedLabel("Claimed By:", 13), *task.ClaimedBy); err != nil {
		return err
	}
}
if task.ClaimedAt != nil {
	if _, err := fmt.Fprintf(r.w, "%s %s\n", r.paddedLabel("Claimed At:", 13), task.ClaimedAt.Format("2006-01-02 15:04:05")); err != nil {
		return err
	}
}
```

- [ ] **Step 5: Build and verify**

```bash
go build ./...
go test ./internal/tui/... ./internal/service/...
```

Expected: compiles and all tests pass.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/commands.go internal/tui/render.go
git commit -m "feat(tui): add tusk claim/release commands, auto-claim on start, claim display"
```

---

### Task 5: Add `claimed_by` and `unclaimed` filter support

**Files:**
- Modify: `internal/domain/filter.go` — add `ClaimedBy` and `Unclaimed` to TaskFilter
- Modify: `internal/filter/resolve.go` — handle `claimed_by` and `unclaimed` in resolveField
- Modify: `internal/sqlite/task.go` — handle new filter fields in buildFilter

- [ ] **Step 1: Add fields to `TaskFilter` in `internal/domain/filter.go`**

Add after the `UDA` field:

```go
ClaimedBy *string // filter by player ID
Unclaimed *bool   // if true, only tasks where claimed_by IS NULL
```

- [ ] **Step 2: Add cases to `resolveField` in `internal/filter/resolve.go`**

Add before the `default:` case (around line 248):

```go
case "claimed_by":
	v := field.Value
	tf.ClaimedBy = &v

case "unclaimed":
	v := field.Value == "true"
	tf.Unclaimed = &v
```

- [ ] **Step 3: Add SQL conditions to `buildFilter` in `internal/sqlite/task.go`**

Add after the `DescriptionContains` handling (around line 254):

```go
if filter.ClaimedBy != nil {
	conditions = append(conditions, "claimed_by = ?")
	args = append(args, *filter.ClaimedBy)
}
if filter.Unclaimed != nil && *filter.Unclaimed {
	conditions = append(conditions, "claimed_by IS NULL")
}
```

- [ ] **Step 4: Build and run tests**

```bash
go build ./...
go test ./internal/sqlite/... ./internal/filter/...
```

Expected: compiles and all tests pass.

- [ ] **Step 5: Commit**

```bash
git add internal/domain/filter.go internal/filter/resolve.go internal/sqlite/task.go
git commit -m "feat(filter): add claimed_by and unclaimed filter support"
```

---

## Changes Introduced

**New files:**
- `internal/service/player.go` — PlayerService
- `internal/service/player_test.go` — PlayerService tests
- `internal/service/task_claim_test.go` — Claim/Release/Start-auto-claim tests

**Modified files:**
- `internal/service/task.go` — added `playerRepo` field, `Claim`, `Release` methods, modified `Start` signature
- `cmd/tusk/main.go` — `--player` flag parsing, `playerRepo`/`playerSvc` wiring, updated `tui.New` call
- `internal/tui/app.go` — added `playerSvc` and `playerID` fields, updated constructor, added `buildPlayerCmd`
- `internal/tui/commands.go` — added `runClaim`, `runRelease`, `runPlayerRegister`, `ensurePlayer`, `formatClaimError`, updated `runStart`
- `internal/tui/render.go` — added `playerJSON`, `renderPlayerResult`, `ClaimedBy`/`ClaimedAt` to `taskJSON` and text display
- `internal/domain/filter.go` — added `ClaimedBy` and `Unclaimed` to TaskFilter
- `internal/filter/resolve.go` — added `claimed_by` and `unclaimed` cases
- `internal/sqlite/task.go` — added `claimed_by`/`unclaimed` filter conditions

**New dependencies:** None.

**Bridge code:**
- MCP server constructor (`tuskmcp.New`) is NOT updated in this phase — MCP still works without player support. Phase 3 adds MCP player tools and the `playerSvc` dependency.

**User-visible behavior preserved:**
- All existing CLI commands work identically when `--player` is not specified
- `tusk start` without `--player` behaves exactly as before
- All existing E2E tests pass
- MCP server works identically (no changes to MCP layer)

**New user-visible behavior:**
- `tusk player register <id> --type human|agent` — register a player
- `tusk claim <short_id>` — claim a task (requires `--player`)
- `tusk release <short_id>` — release a claim (requires `--player`)
- `tusk start <short_id>` with `--player` — auto-claims the task
- `tusk info` / list output shows `claimed_by` and `claimed_at`
- `claimed_by:<id>` and `unclaimed:true` filter syntax
