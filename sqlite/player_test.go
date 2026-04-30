package sqlite_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/migrations"
	"github.com/germanamz/tusk/repository"
	"github.com/germanamz/tusk/sqlite"
)

var _ repository.PlayerRepository = (*sqlite.PlayerRepo)(nil)

func newTestPlayerRepo(test *testing.T) *sqlite.PlayerRepo {
	test.Helper()
	store, err := sqlite.New(test.TempDir()+"/test.db", migrations.FS)

	if err != nil {
		test.Fatalf("opening test db: %v", err)
	}

	test.Cleanup(func() { store.Close() })

	return sqlite.NewPlayerRepo(store.DB())
}

func TestPlayerRepo_CreateAndGetByID(test *testing.T) {
	repo := newTestPlayerRepo(test)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Millisecond)
	player := &domain.Player{
		ID:           "agent-1",
		Type:         "agent",
		RegisteredAt: now,
		LastSeenAt:   now,
	}

	if err := repo.Create(ctx, player); err != nil {
		test.Fatalf("Create: %v", err)
	}

	got, err := repo.GetByID(ctx, "agent-1")

	if err != nil {
		test.Fatalf("GetByID: %v", err)
	}

	if got.ID != "agent-1" {
		test.Errorf("got ID %q, want %q", got.ID, "agent-1")
	}

	if got.Type != "agent" {
		test.Errorf("got Type %q, want %q", got.Type, "agent")
	}
}

func TestPlayerRepo_CreateDuplicate(test *testing.T) {
	repo := newTestPlayerRepo(test)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Millisecond)
	player := &domain.Player{ID: "dup-1", Type: "human", RegisteredAt: now, LastSeenAt: now}

	if err := repo.Create(ctx, player); err != nil {
		test.Fatalf("first Create: %v", err)
	}

	err := repo.Create(ctx, player)

	if !errors.Is(err, domain.ErrConflict) {
		test.Fatalf("second Create: got %v, want ErrConflict", err)
	}
}

func TestPlayerRepo_GetByID_NotFound(test *testing.T) {
	repo := newTestPlayerRepo(test)
	ctx := context.Background()

	_, err := repo.GetByID(ctx, "nonexistent")

	if !errors.Is(err, domain.ErrNotFound) {
		test.Fatalf("GetByID: got %v, want ErrNotFound", err)
	}
}

func TestPlayerRepo_UpdateLastSeen(test *testing.T) {
	repo := newTestPlayerRepo(test)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Millisecond)
	player := &domain.Player{ID: "agent-2", Type: "agent", RegisteredAt: now, LastSeenAt: now}

	if err := repo.Create(ctx, player); err != nil {
		test.Fatalf("Create: %v", err)
	}

	time.Sleep(10 * time.Millisecond)

	if err := repo.UpdateLastSeen(ctx, "agent-2"); err != nil {
		test.Fatalf("UpdateLastSeen: %v", err)
	}

	got, _ := repo.GetByID(ctx, "agent-2")

	if !got.LastSeenAt.After(now) {
		test.Errorf("LastSeenAt not updated: got %v, registered %v", got.LastSeenAt, now)
	}
}

func TestPlayerRepo_UpdateLastSeen_NotFound(test *testing.T) {
	repo := newTestPlayerRepo(test)
	ctx := context.Background()

	err := repo.UpdateLastSeen(ctx, "ghost")

	if !errors.Is(err, domain.ErrNotFound) {
		test.Fatalf("UpdateLastSeen: got %v, want ErrNotFound", err)
	}
}

func TestPlayerRepo_CreateWithNoteWindowSize(test *testing.T) {
	repo := newTestPlayerRepo(test)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Millisecond)
	size := 30
	player := &domain.Player{ID: "p1", Type: "human", NoteWindowSize: &size, RegisteredAt: now, LastSeenAt: now}

	if err := repo.Create(ctx, player); err != nil {
		test.Fatalf("Create: %v", err)
	}

	got, err := repo.GetByID(ctx, "p1")

	if err != nil {
		test.Fatalf("GetByID: %v", err)
	}

	if got.NoteWindowSize == nil || *got.NoteWindowSize != 30 {
		test.Fatalf("NoteWindowSize: got %v, want 30", got.NoteWindowSize)
	}
}

func TestPlayerRepo_UpdateNoteWindowSize(test *testing.T) {
	repo := newTestPlayerRepo(test)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Millisecond)

	if err := repo.Create(ctx, &domain.Player{ID: "p1", Type: "human", RegisteredAt: now, LastSeenAt: now}); err != nil {
		test.Fatalf("Create: %v", err)
	}

	size := 50

	if err := repo.UpdateNoteWindowSize(ctx, "p1", &size); err != nil {
		test.Fatalf("set: %v", err)
	}

	got, _ := repo.GetByID(ctx, "p1")

	if got.NoteWindowSize == nil || *got.NoteWindowSize != 50 {
		test.Fatalf("after set: got %v, want 50", got.NoteWindowSize)
	}

	if err := repo.UpdateNoteWindowSize(ctx, "p1", nil); err != nil {
		test.Fatalf("clear: %v", err)
	}

	got, _ = repo.GetByID(ctx, "p1")

	if got.NoteWindowSize != nil {
		test.Fatalf("after clear: got %v, want nil", got.NoteWindowSize)
	}

	err := repo.UpdateNoteWindowSize(ctx, "ghost", &size)

	if !errors.Is(err, domain.ErrNotFound) {
		test.Fatalf("ghost: got %v, want ErrNotFound", err)
	}
}

func TestPlayerRepo_List(test *testing.T) {
	repo := newTestPlayerRepo(test)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Millisecond)

	for _, id := range []string{"a", "b", "c"} {
		player := &domain.Player{ID: id, Type: "agent", RegisteredAt: now, LastSeenAt: now}

		if err := repo.Create(ctx, player); err != nil {
			test.Fatalf("Create %s: %v", id, err)
		}
	}

	players, err := repo.List(ctx)

	if err != nil {
		test.Fatalf("List: %v", err)
	}

	if len(players) != 3 {
		test.Fatalf("List: got %d players, want 3", len(players))
	}
}
