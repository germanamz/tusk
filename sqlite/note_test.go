package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/repository"
	"github.com/google/uuid"
)

// Compile-time check: *NoteRepo must implement repository.NoteRepository.
var _ repository.NoteRepository = (*NoteRepo)(nil)

func mustCreatePlayer(t *testing.T, s *Store, id string) {
	t.Helper()
	repo := NewPlayerRepo(s.DB())
	p := &domain.Player{
		ID:           id,
		Type:         "human",
		RegisteredAt: time.Now().UTC().Truncate(time.Millisecond),
		LastSeenAt:   time.Now().UTC().Truncate(time.Millisecond),
	}
	if err := repo.Create(context.Background(), p); err != nil {
		t.Fatalf("mustCreatePlayer: %v", err)
	}
}

func TestNoteCreateAndGetByID(t *testing.T) {
	s := testStore(t)
	taskRepo := NewTaskRepo(s.DB())
	repo := NewNoteRepo(s.DB())
	ctx := context.Background()

	mustCreatePlayer(t, s, "german")
	task := newTestTask()
	mustCreateTask(t, taskRepo, task)

	now := time.Now().UTC().Truncate(time.Millisecond)
	taskID := task.ID
	note := &domain.Note{
		ID:        uuid.New(),
		ProjectID: domain.DefaultProjectUUID,
		PlayerID:  "german",
		TaskID:    &taskID,
		Body:      "Auth middleware needs retry logic",
		Metadata:  map[string]any{"topic": "auth"},
		CreatedAt: now,
	}

	if err := repo.Create(ctx, note); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.GetByID(ctx, note.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Body != note.Body {
		t.Fatalf("body: got %q, want %q", got.Body, note.Body)
	}
	if got.PlayerID != "german" {
		t.Fatalf("player_id: got %q, want %q", got.PlayerID, "german")
	}
	if got.TaskID == nil || *got.TaskID != taskID {
		t.Fatalf("task_id: got %v, want %v", got.TaskID, taskID)
	}
	if got.ArchivedAt != nil {
		t.Fatalf("archived_at: expected nil, got %v", got.ArchivedAt)
	}
	if got.Metadata["topic"] != "auth" {
		t.Fatalf("metadata: got %v, want topic=auth", got.Metadata)
	}
}

func TestNoteGetByIDNotFound(t *testing.T) {
	s := testStore(t)
	repo := NewNoteRepo(s.DB())

	_, err := repo.GetByID(context.Background(), uuid.New())
	if err != domain.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestNoteCreateProjectLevel(t *testing.T) {
	s := testStore(t)
	repo := NewNoteRepo(s.DB())
	ctx := context.Background()

	mustCreatePlayer(t, s, "agent-1")

	note := &domain.Note{
		ID:        uuid.New(),
		ProjectID: domain.DefaultProjectUUID,
		PlayerID:  "agent-1",
		TaskID:    nil,
		Body:      "Caching strategy won't work for this project",
		Metadata:  map[string]any{},
		CreatedAt: time.Now().UTC().Truncate(time.Millisecond),
	}

	if err := repo.Create(ctx, note); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.GetByID(ctx, note.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.TaskID != nil {
		t.Fatalf("task_id: expected nil, got %v", got.TaskID)
	}
}
