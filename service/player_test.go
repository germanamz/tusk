package service_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/migrations"
	"github.com/germanamz/tusk/service"
	"github.com/germanamz/tusk/sqlite"
)

func ptrInt(v int) *int { return &v }

func newPlayerTestEnv(test *testing.T) (*service.PlayerService, *sqlite.PlayerRepo) {
	test.Helper()
	store, err := sqlite.New(test.TempDir()+"/test.db", migrations.FS)

	if err != nil {
		test.Fatalf("opening test db: %v", err)
	}

	test.Cleanup(func() { store.Close() })
	repo := sqlite.NewPlayerRepo(store.DB())
	svc := service.NewPlayerService(repo)
	return svc, repo
}

func TestPlayerService_Register(test *testing.T) {
	svc, _ := newPlayerTestEnv(test)
	ctx := context.Background()

	player, err := svc.Register(ctx, "agent-1", "agent")

	if err != nil {
		test.Fatalf("Register: %v", err)
	}

	if player.ID != "agent-1" {
		test.Errorf("got ID %q, want %q", player.ID, "agent-1")
	}
	if player.Type != "agent" {
		test.Errorf("got Type %q, want %q", player.Type, "agent")
	}
	if player.RegisteredAt.IsZero() {
		test.Error("RegisteredAt should be set")
	}
}

func TestPlayerService_Register_InvalidType(test *testing.T) {
	svc, _ := newPlayerTestEnv(test)
	ctx := context.Background()

	_, err := svc.Register(ctx, "bad", "robot")
	if err == nil {
		test.Fatal("expected error for invalid type")
	}
}

func TestPlayerService_Register_EmptyID(test *testing.T) {
	svc, _ := newPlayerTestEnv(test)
	ctx := context.Background()

	_, err := svc.Register(ctx, "", "human")
	if err == nil {
		test.Fatal("expected error for empty ID")
	}
}

func TestPlayerService_Register_Duplicate(test *testing.T) {
	svc, _ := newPlayerTestEnv(test)
	ctx := context.Background()

	if _, err := svc.Register(ctx, "dup", "human"); err != nil {
		test.Fatalf("first Register: %v", err)
	}
	_, err := svc.Register(ctx, "dup", "human")
	if err != domain.ErrConflict {
		test.Fatalf("second Register: got %v, want ErrConflict", err)
	}
}

func TestPlayerService_GetByID(test *testing.T) {
	svc, _ := newPlayerTestEnv(test)
	ctx := context.Background()

	svc.Register(ctx, "lookup", "human")
	player, err := svc.GetByID(ctx, "lookup")

	if err != nil {
		test.Fatalf("GetByID: %v", err)
	}

	if player.ID != "lookup" {
		test.Errorf("got %q, want %q", player.ID, "lookup")
	}
}

func TestPlayerService_UpdateLastSeen(test *testing.T) {
	svc, _ := newPlayerTestEnv(test)
	ctx := context.Background()

	player, _ := svc.Register(ctx, "seen", "agent")
	original := player.LastSeenAt

	time.Sleep(10 * time.Millisecond)
	if err := svc.UpdateLastSeen(ctx, "seen"); err != nil {
		test.Fatalf("UpdateLastSeen: %v", err)
	}

	updated, _ := svc.GetByID(ctx, "seen")
	if !updated.LastSeenAt.After(original) {
		test.Error("LastSeenAt should have been updated")
	}
}

func TestPlayerSetNoteWindowSize_SetAndClear(test *testing.T) {
	svc, _ := newPlayerTestEnv(test)
	ctx := context.Background()

	if _, err := svc.Register(ctx, "agent-1", "agent"); err != nil {
		test.Fatalf("Register: %v", err)
	}

	updated, setErr := svc.SetNoteWindowSize(ctx, "agent-1", ptrInt(50))

	if setErr != nil {
		test.Fatalf("SetNoteWindowSize set: %v", setErr)
	}

	if updated.NoteWindowSize == nil || *updated.NoteWindowSize != 50 {
		test.Fatalf("got NoteWindowSize %v, want 50", updated.NoteWindowSize)
	}

	reloaded, reloadErr := svc.GetByID(ctx, "agent-1")

	if reloadErr != nil {
		test.Fatalf("GetByID: %v", reloadErr)
	}

	if reloaded.NoteWindowSize == nil || *reloaded.NoteWindowSize != 50 {
		test.Fatalf("repo NoteWindowSize %v, want 50", reloaded.NoteWindowSize)
	}

	cleared, clearErr := svc.SetNoteWindowSize(ctx, "agent-1", nil)

	if clearErr != nil {
		test.Fatalf("SetNoteWindowSize clear: %v", clearErr)
	}

	if cleared.NoteWindowSize != nil {
		test.Fatalf("got NoteWindowSize %v, want nil", cleared.NoteWindowSize)
	}
}

func TestPlayerSetNoteWindowSize_NotFound(test *testing.T) {
	svc, _ := newPlayerTestEnv(test)
	ctx := context.Background()

	_, err := svc.SetNoteWindowSize(ctx, "ghost", ptrInt(10))
	if !errors.Is(err, domain.ErrNotFound) {
		test.Fatalf("got %v, want ErrNotFound", err)
	}
}

func TestPlayerSetNoteWindowSize_EmptyID(test *testing.T) {
	svc, _ := newPlayerTestEnv(test)
	ctx := context.Background()

	_, err := svc.SetNoteWindowSize(ctx, "", ptrInt(10))
	if err == nil {
		test.Fatal("expected error for empty ID")
	}
	if !strings.Contains(err.Error(), "must not be empty") {
		test.Fatalf("got %q, want error containing 'must not be empty'", err.Error())
	}
}

func TestPlayerSetNoteWindowSize_InvalidSize(test *testing.T) {
	svc, _ := newPlayerTestEnv(test)
	ctx := context.Background()

	if _, err := svc.Register(ctx, "agent-1", "agent"); err != nil {
		test.Fatalf("Register: %v", err)
	}

	for _, size := range []int{0, -3} {
		_, err := svc.SetNoteWindowSize(ctx, "agent-1", ptrInt(size))
		if err == nil {
			test.Fatalf("size %d: expected error", size)
		}
		if !strings.Contains(err.Error(), "must be positive") {
			test.Fatalf("size %d: got %q, want error containing 'must be positive'", size, err.Error())
		}
	}
}

func TestPlayerService_List(test *testing.T) {
	svc, _ := newPlayerTestEnv(test)
	ctx := context.Background()

	svc.Register(ctx, "a", "human")
	svc.Register(ctx, "b", "agent")
	players, err := svc.List(ctx)

	if err != nil {
		test.Fatalf("List: %v", err)
	}

	if len(players) != 2 {
		test.Fatalf("got %d players, want 2", len(players))
	}
}
