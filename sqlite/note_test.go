package sqlite

import (
	"context"
	"fmt"
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

func TestNoteArchive(t *testing.T) {
	s := testStore(t)
	repo := NewNoteRepo(s.DB())
	ctx := context.Background()

	mustCreatePlayer(t, s, "german")

	note := &domain.Note{
		ID:        uuid.New(),
		ProjectID: domain.DefaultProjectUUID,
		PlayerID:  "german",
		Body:      "Will be archived",
		Metadata:  map[string]any{},
		CreatedAt: time.Now().UTC().Truncate(time.Millisecond),
	}
	if err := repo.Create(ctx, note); err != nil {
		t.Fatal(err)
	}

	archivedAt := time.Now().UTC().Truncate(time.Millisecond)
	if err := repo.Archive(ctx, note.ID, archivedAt); err != nil {
		t.Fatalf("Archive: %v", err)
	}

	got, err := repo.GetByID(ctx, note.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ArchivedAt == nil {
		t.Fatal("expected archived_at to be set")
	}
	if !got.ArchivedAt.Equal(archivedAt) {
		t.Fatalf("archived_at: got %v, want %v", got.ArchivedAt, archivedAt)
	}
}

func TestNoteArchiveNotFound(t *testing.T) {
	s := testStore(t)
	repo := NewNoteRepo(s.DB())

	err := repo.Archive(context.Background(), uuid.New(), time.Now().UTC())
	if err != domain.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestNoteList(t *testing.T) {
	s := testStore(t)
	taskRepo := NewTaskRepo(s.DB())
	repo := NewNoteRepo(s.DB())
	ctx := context.Background()

	mustCreatePlayer(t, s, "german")
	task := newTestTask()
	mustCreateTask(t, taskRepo, task)

	// Create 3 notes with increasing timestamps.
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i, body := range []string{"First", "Second", "Third"} {
		taskID := task.ID
		note := &domain.Note{
			ID:        uuid.New(),
			ProjectID: domain.DefaultProjectUUID,
			PlayerID:  "german",
			TaskID:    &taskID,
			Body:      body,
			Metadata:  map[string]any{},
			CreatedAt: base.Add(time.Duration(i) * time.Hour),
		}
		if err := repo.Create(ctx, note); err != nil {
			t.Fatal(err)
		}
	}

	// List all notes for this player+project, no limit.
	notes, err := repo.List(ctx, repository.NoteListOptions{
		ProjectID: domain.DefaultProjectUUID,
		PlayerID:  "german",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 3 {
		t.Fatalf("expected 3 notes, got %d", len(notes))
	}
	// Should be in descending created_at order (newest first).
	if notes[0].Body != "Third" {
		t.Fatalf("expected newest first, got %q", notes[0].Body)
	}
}

func TestNoteListWindowLimit(t *testing.T) {
	s := testStore(t)
	repo := NewNoteRepo(s.DB())
	ctx := context.Background()

	mustCreatePlayer(t, s, "german")

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := range 5 {
		note := &domain.Note{
			ID:        uuid.New(),
			ProjectID: domain.DefaultProjectUUID,
			PlayerID:  "german",
			Body:      fmt.Sprintf("Note %d", i),
			Metadata:  map[string]any{},
			CreatedAt: base.Add(time.Duration(i) * time.Hour),
		}
		if err := repo.Create(ctx, note); err != nil {
			t.Fatal(err)
		}
	}

	// Window of 2 should return only the 2 newest.
	notes, err := repo.List(ctx, repository.NoteListOptions{
		ProjectID: domain.DefaultProjectUUID,
		PlayerID:  "german",
		Limit:     2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 2 {
		t.Fatalf("expected 2 notes, got %d", len(notes))
	}
	if notes[0].Body != "Note 4" {
		t.Fatalf("expected newest first, got %q", notes[0].Body)
	}
	if notes[1].Body != "Note 3" {
		t.Fatalf("expected second newest, got %q", notes[1].Body)
	}
}

func TestNoteListSince(t *testing.T) {
	s := testStore(t)
	repo := NewNoteRepo(s.DB())
	ctx := context.Background()

	mustCreatePlayer(t, s, "german")

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := range 3 {
		note := &domain.Note{
			ID:        uuid.New(),
			ProjectID: domain.DefaultProjectUUID,
			PlayerID:  "german",
			Body:      fmt.Sprintf("Note %d", i),
			Metadata:  map[string]any{},
			CreatedAt: base.Add(time.Duration(i) * 24 * time.Hour),
		}
		if err := repo.Create(ctx, note); err != nil {
			t.Fatal(err)
		}
	}

	// Since the second day — should return notes 1 and 2.
	since := base.Add(24 * time.Hour)
	notes, err := repo.List(ctx, repository.NoteListOptions{
		ProjectID: domain.DefaultProjectUUID,
		PlayerID:  "german",
		Since:     &since,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 2 {
		t.Fatalf("expected 2 notes since day 2, got %d", len(notes))
	}
}

func TestNoteListExcludesArchived(t *testing.T) {
	s := testStore(t)
	repo := NewNoteRepo(s.DB())
	ctx := context.Background()

	mustCreatePlayer(t, s, "german")

	now := time.Now().UTC().Truncate(time.Millisecond)
	active := &domain.Note{
		ID:        uuid.New(),
		ProjectID: domain.DefaultProjectUUID,
		PlayerID:  "german",
		Body:      "Active note",
		Metadata:  map[string]any{},
		CreatedAt: now,
	}
	archived := &domain.Note{
		ID:        uuid.New(),
		ProjectID: domain.DefaultProjectUUID,
		PlayerID:  "german",
		Body:      "Archived note",
		Metadata:  map[string]any{},
		CreatedAt: now.Add(-time.Hour),
	}
	if err := repo.Create(ctx, active); err != nil {
		t.Fatal(err)
	}
	if err := repo.Create(ctx, archived); err != nil {
		t.Fatal(err)
	}
	if err := repo.Archive(ctx, archived.ID, now); err != nil {
		t.Fatal(err)
	}

	// Default: exclude archived.
	notes, err := repo.List(ctx, repository.NoteListOptions{
		ProjectID: domain.DefaultProjectUUID,
		PlayerID:  "german",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 1 {
		t.Fatalf("expected 1 active note, got %d", len(notes))
	}

	// Include archived.
	notes, err = repo.List(ctx, repository.NoteListOptions{
		ProjectID:       domain.DefaultProjectUUID,
		PlayerID:        "german",
		IncludeArchived: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 2 {
		t.Fatalf("expected 2 notes with archived, got %d", len(notes))
	}
}

func TestNoteListByTask(t *testing.T) {
	s := testStore(t)
	taskRepo := NewTaskRepo(s.DB())
	repo := NewNoteRepo(s.DB())
	ctx := context.Background()

	mustCreatePlayer(t, s, "german")
	task := newTestTask()
	mustCreateTask(t, taskRepo, task)

	now := time.Now().UTC().Truncate(time.Millisecond)
	taskID := task.ID

	// One task-scoped note, one project-level note.
	taskNote := &domain.Note{
		ID:        uuid.New(),
		ProjectID: domain.DefaultProjectUUID,
		PlayerID:  "german",
		TaskID:    &taskID,
		Body:      "Task-scoped",
		Metadata:  map[string]any{},
		CreatedAt: now,
	}
	projectNote := &domain.Note{
		ID:        uuid.New(),
		ProjectID: domain.DefaultProjectUUID,
		PlayerID:  "german",
		TaskID:    nil,
		Body:      "Project-level",
		Metadata:  map[string]any{},
		CreatedAt: now.Add(-time.Hour),
	}
	if err := repo.Create(ctx, taskNote); err != nil {
		t.Fatal(err)
	}
	if err := repo.Create(ctx, projectNote); err != nil {
		t.Fatal(err)
	}

	// Filter by task — should return only the task-scoped note.
	notes, err := repo.List(ctx, repository.NoteListOptions{
		ProjectID: domain.DefaultProjectUUID,
		PlayerID:  "german",
		TaskID:    &taskID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 1 {
		t.Fatalf("expected 1 task-scoped note, got %d", len(notes))
	}
	if notes[0].Body != "Task-scoped" {
		t.Fatalf("expected task-scoped note, got %q", notes[0].Body)
	}
}
