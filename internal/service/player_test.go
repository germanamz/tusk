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
