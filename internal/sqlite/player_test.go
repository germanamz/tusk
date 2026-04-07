package sqlite_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/germanamz/tusk/internal/domain"
	"github.com/germanamz/tusk/internal/repository"
	"github.com/germanamz/tusk/internal/sqlite"
	"github.com/germanamz/tusk/migrations"
)

var _ repository.PlayerRepository = (*sqlite.PlayerRepo)(nil)

func newTestPlayerRepo(t *testing.T) *sqlite.PlayerRepo {
	t.Helper()
	store, err := sqlite.New(t.TempDir()+"/test.db", migrations.FS)
	if err != nil {
		t.Fatalf("opening test db: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return sqlite.NewPlayerRepo(store.DB())
}

func TestPlayerRepo_CreateAndGetByID(t *testing.T) {
	repo := newTestPlayerRepo(t)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Millisecond)
	player := &domain.Player{
		ID:           "agent-1",
		Type:         "agent",
		RegisteredAt: now,
		LastSeenAt:   now,
	}
	if err := repo.Create(ctx, player); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.GetByID(ctx, "agent-1")
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.ID != "agent-1" {
		t.Errorf("got ID %q, want %q", got.ID, "agent-1")
	}
	if got.Type != "agent" {
		t.Errorf("got Type %q, want %q", got.Type, "agent")
	}
}

func TestPlayerRepo_CreateDuplicate(t *testing.T) {
	repo := newTestPlayerRepo(t)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Millisecond)
	player := &domain.Player{ID: "dup-1", Type: "human", RegisteredAt: now, LastSeenAt: now}
	if err := repo.Create(ctx, player); err != nil {
		t.Fatalf("first Create: %v", err)
	}
	err := repo.Create(ctx, player)
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("second Create: got %v, want ErrConflict", err)
	}
}

func TestPlayerRepo_GetByID_NotFound(t *testing.T) {
	repo := newTestPlayerRepo(t)
	ctx := context.Background()

	_, err := repo.GetByID(ctx, "nonexistent")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("GetByID: got %v, want ErrNotFound", err)
	}
}

func TestPlayerRepo_UpdateLastSeen(t *testing.T) {
	repo := newTestPlayerRepo(t)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Millisecond)
	player := &domain.Player{ID: "agent-2", Type: "agent", RegisteredAt: now, LastSeenAt: now}
	if err := repo.Create(ctx, player); err != nil {
		t.Fatalf("Create: %v", err)
	}

	time.Sleep(10 * time.Millisecond)
	if err := repo.UpdateLastSeen(ctx, "agent-2"); err != nil {
		t.Fatalf("UpdateLastSeen: %v", err)
	}

	got, _ := repo.GetByID(ctx, "agent-2")
	if !got.LastSeenAt.After(now) {
		t.Errorf("LastSeenAt not updated: got %v, registered %v", got.LastSeenAt, now)
	}
}

func TestPlayerRepo_UpdateLastSeen_NotFound(t *testing.T) {
	repo := newTestPlayerRepo(t)
	ctx := context.Background()

	err := repo.UpdateLastSeen(ctx, "ghost")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("UpdateLastSeen: got %v, want ErrNotFound", err)
	}
}

func TestPlayerRepo_List(t *testing.T) {
	repo := newTestPlayerRepo(t)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Millisecond)
	for _, id := range []string{"a", "b", "c"} {
		p := &domain.Player{ID: id, Type: "agent", RegisteredAt: now, LastSeenAt: now}
		if err := repo.Create(ctx, p); err != nil {
			t.Fatalf("Create %s: %v", id, err)
		}
	}

	players, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(players) != 3 {
		t.Fatalf("List: got %d players, want 3", len(players))
	}
}
