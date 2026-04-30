package sqlite

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/repository"
	"github.com/google/uuid"
)

// Compile-time check: *NoteRepo must implement repository.NoteRepository.
var _ repository.NoteRepository = (*NoteRepo)(nil)

func mustCreatePlayer(test *testing.T, store *Store, id string) {
	test.Helper()
	repo := NewPlayerRepo(store.DB())
	player := &domain.Player{
		ID:           id,
		Type:         "human",
		RegisteredAt: time.Now().UTC().Truncate(time.Millisecond),
		LastSeenAt:   time.Now().UTC().Truncate(time.Millisecond),
	}

	if err := repo.Create(context.Background(), player); err != nil {
		test.Fatalf("mustCreatePlayer: %v", err)
	}
}

func TestNoteCreateAndGetByID(test *testing.T) {
	store := testStore(test)
	taskRepo := NewTaskRepo(store.DB())
	repo := NewNoteRepo(store.DB())
	ctx := context.Background()

	mustCreatePlayer(test, store, "german")
	task := newTestTask()
	mustCreateTask(test, taskRepo, task)

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
		test.Fatalf("Create: %v", err)
	}

	got, err := repo.GetByID(ctx, note.ID)

	if err != nil {
		test.Fatalf("GetByID: %v", err)
	}

	if got.Body != note.Body {
		test.Fatalf("body: got %q, want %q", got.Body, note.Body)
	}
	if got.PlayerID != "german" {
		test.Fatalf("player_id: got %q, want %q", got.PlayerID, "german")
	}
	if got.TaskID == nil || *got.TaskID != taskID {
		test.Fatalf("task_id: got %v, want %v", got.TaskID, taskID)
	}
	if got.ArchivedAt != nil {
		test.Fatalf("archived_at: expected nil, got %v", got.ArchivedAt)
	}
	if got.Metadata["topic"] != "auth" {
		test.Fatalf("metadata: got %v, want topic=auth", got.Metadata)
	}
}

func TestNoteGetByIDNotFound(test *testing.T) {
	store := testStore(test)
	repo := NewNoteRepo(store.DB())

	_, err := repo.GetByID(context.Background(), uuid.New())

	if err != domain.ErrNotFound {
		test.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestNoteCreateProjectLevel(test *testing.T) {
	store := testStore(test)
	repo := NewNoteRepo(store.DB())
	ctx := context.Background()

	mustCreatePlayer(test, store, "agent-1")

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
		test.Fatalf("Create: %v", err)
	}

	got, err := repo.GetByID(ctx, note.ID)

	if err != nil {
		test.Fatalf("GetByID: %v", err)
	}

	if got.TaskID != nil {
		test.Fatalf("task_id: expected nil, got %v", got.TaskID)
	}
}

func TestNoteArchive(test *testing.T) {
	store := testStore(test)
	repo := NewNoteRepo(store.DB())
	ctx := context.Background()

	mustCreatePlayer(test, store, "german")

	note := &domain.Note{
		ID:        uuid.New(),
		ProjectID: domain.DefaultProjectUUID,
		PlayerID:  "german",
		Body:      "Will be archived",
		Metadata:  map[string]any{},
		CreatedAt: time.Now().UTC().Truncate(time.Millisecond),
	}

	if err := repo.Create(ctx, note); err != nil {
		test.Fatal(err)
	}

	archivedAt := time.Now().UTC().Truncate(time.Millisecond)

	if err := repo.Archive(ctx, note.ID, archivedAt); err != nil {
		test.Fatalf("Archive: %v", err)
	}

	got, err := repo.GetByID(ctx, note.ID)

	if err != nil {
		test.Fatal(err)
	}

	if got.ArchivedAt == nil {
		test.Fatal("expected archived_at to be set")
	}
	if !got.ArchivedAt.Equal(archivedAt) {
		test.Fatalf("archived_at: got %v, want %v", got.ArchivedAt, archivedAt)
	}
}

func TestNoteArchiveNotFound(test *testing.T) {
	store := testStore(test)
	repo := NewNoteRepo(store.DB())

	err := repo.Archive(context.Background(), uuid.New(), time.Now().UTC())

	if err != domain.ErrNotFound {
		test.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestNoteList(test *testing.T) {
	store := testStore(test)
	taskRepo := NewTaskRepo(store.DB())
	repo := NewNoteRepo(store.DB())
	ctx := context.Background()

	mustCreatePlayer(test, store, "german")
	task := newTestTask()
	mustCreateTask(test, taskRepo, task)

	// Create 3 notes with increasing timestamps.
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for idx, body := range []string{"First", "Second", "Third"} {
		taskID := task.ID
		note := &domain.Note{
			ID:        uuid.New(),
			ProjectID: domain.DefaultProjectUUID,
			PlayerID:  "german",
			TaskID:    &taskID,
			Body:      body,
			Metadata:  map[string]any{},
			CreatedAt: base.Add(time.Duration(idx) * time.Hour),
		}
		if err := repo.Create(ctx, note); err != nil {
			test.Fatal(err)
		}
	}

	// List all notes for this player+project, no limit.
	notes, err := repo.List(ctx, repository.NoteListOptions{
		ProjectID: domain.DefaultProjectUUID,
		PlayerID:  "german",
	})

	if err != nil {
		test.Fatal(err)
	}

	if len(notes) != 3 {
		test.Fatalf("expected 3 notes, got %d", len(notes))
	}
	// Should be in descending created_at order (newest first).
	if notes[0].Body != "Third" {
		test.Fatalf("expected newest first, got %q", notes[0].Body)
	}
}

func TestNoteListWindowLimit(test *testing.T) {
	store := testStore(test)
	repo := NewNoteRepo(store.DB())
	ctx := context.Background()

	mustCreatePlayer(test, store, "german")

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for idx := range 5 {
		note := &domain.Note{
			ID:        uuid.New(),
			ProjectID: domain.DefaultProjectUUID,
			PlayerID:  "german",
			Body:      fmt.Sprintf("Note %d", idx),
			Metadata:  map[string]any{},
			CreatedAt: base.Add(time.Duration(idx) * time.Hour),
		}
		if err := repo.Create(ctx, note); err != nil {
			test.Fatal(err)
		}
	}

	// Window of 2 should return only the 2 newest.
	notes, err := repo.List(ctx, repository.NoteListOptions{
		ProjectID: domain.DefaultProjectUUID,
		PlayerID:  "german",
		Limit:     2,
	})

	if err != nil {
		test.Fatal(err)
	}

	if len(notes) != 2 {
		test.Fatalf("expected 2 notes, got %d", len(notes))
	}
	if notes[0].Body != "Note 4" {
		test.Fatalf("expected newest first, got %q", notes[0].Body)
	}
	if notes[1].Body != "Note 3" {
		test.Fatalf("expected second newest, got %q", notes[1].Body)
	}
}

func TestNoteListSince(test *testing.T) {
	store := testStore(test)
	repo := NewNoteRepo(store.DB())
	ctx := context.Background()

	mustCreatePlayer(test, store, "german")

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for idx := range 3 {
		note := &domain.Note{
			ID:        uuid.New(),
			ProjectID: domain.DefaultProjectUUID,
			PlayerID:  "german",
			Body:      fmt.Sprintf("Note %d", idx),
			Metadata:  map[string]any{},
			CreatedAt: base.Add(time.Duration(idx) * 24 * time.Hour),
		}
		if err := repo.Create(ctx, note); err != nil {
			test.Fatal(err)
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
		test.Fatal(err)
	}

	if len(notes) != 2 {
		test.Fatalf("expected 2 notes since day 2, got %d", len(notes))
	}
}

func TestNoteListExcludesArchived(test *testing.T) {
	store := testStore(test)
	repo := NewNoteRepo(store.DB())
	ctx := context.Background()

	mustCreatePlayer(test, store, "german")

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
		test.Fatal(err)
	}

	if err := repo.Create(ctx, archived); err != nil {
		test.Fatal(err)
	}

	if err := repo.Archive(ctx, archived.ID, now); err != nil {
		test.Fatal(err)
	}

	// Default: exclude archived.
	notes, err := repo.List(ctx, repository.NoteListOptions{
		ProjectID: domain.DefaultProjectUUID,
		PlayerID:  "german",
	})

	if err != nil {
		test.Fatal(err)
	}

	if len(notes) != 1 {
		test.Fatalf("expected 1 active note, got %d", len(notes))
	}

	// Include archived.
	notes, err = repo.List(ctx, repository.NoteListOptions{
		ProjectID:       domain.DefaultProjectUUID,
		PlayerID:        "german",
		IncludeArchived: true,
	})

	if err != nil {
		test.Fatal(err)
	}

	if len(notes) != 2 {
		test.Fatalf("expected 2 notes with archived, got %d", len(notes))
	}
}

func TestNoteFindByIDPrefixUnique(test *testing.T) {
	store := testStore(test)
	repo := NewNoteRepo(store.DB())
	ctx := context.Background()

	mustCreatePlayer(test, store, "german")

	note := &domain.Note{
		ID:        uuid.New(),
		ProjectID: domain.DefaultProjectUUID,
		PlayerID:  "german",
		Body:      "Prefix lookup",
		Metadata:  map[string]any{},
		CreatedAt: time.Now().UTC().Truncate(time.Millisecond),
	}

	if err := repo.Create(ctx, note); err != nil {
		test.Fatal(err)
	}

	prefix := note.ID.String()[:8]
	got, err := repo.FindByIDPrefix(ctx, prefix)

	if err != nil {
		test.Fatalf("FindByIDPrefix: %v", err)
	}

	if len(got) != 1 {
		test.Fatalf("expected 1 match, got %d", len(got))
	}
	if got[0].ID != note.ID {
		test.Fatalf("id mismatch: got %s, want %s", got[0].ID, note.ID)
	}
}

func TestNoteFindByIDPrefixFullUUID(test *testing.T) {
	store := testStore(test)
	repo := NewNoteRepo(store.DB())
	ctx := context.Background()

	mustCreatePlayer(test, store, "german")

	note := &domain.Note{
		ID:        uuid.New(),
		ProjectID: domain.DefaultProjectUUID,
		PlayerID:  "german",
		Body:      "Full UUID lookup",
		Metadata:  map[string]any{},
		CreatedAt: time.Now().UTC().Truncate(time.Millisecond),
	}

	if err := repo.Create(ctx, note); err != nil {
		test.Fatal(err)
	}

	got, err := repo.FindByIDPrefix(ctx, note.ID.String())

	if err != nil {
		test.Fatalf("FindByIDPrefix: %v", err)
	}

	if len(got) != 1 {
		test.Fatalf("expected 1 match, got %d", len(got))
	}
	if got[0].ID != note.ID {
		test.Fatalf("id mismatch: got %s, want %s", got[0].ID, note.ID)
	}
}

func TestNoteFindByIDPrefixNoMatch(test *testing.T) {
	store := testStore(test)
	repo := NewNoteRepo(store.DB())
	ctx := context.Background()

	mustCreatePlayer(test, store, "german")

	note := &domain.Note{
		ID:        uuid.New(),
		ProjectID: domain.DefaultProjectUUID,
		PlayerID:  "german",
		Body:      "Populated row",
		Metadata:  map[string]any{},
		CreatedAt: time.Now().UTC().Truncate(time.Millisecond),
	}

	if err := repo.Create(ctx, note); err != nil {
		test.Fatal(err)
	}

	got, err := repo.FindByIDPrefix(ctx, "00000000")

	if err != nil {
		test.Fatalf("FindByIDPrefix: %v", err)
	}

	if len(got) != 0 {
		test.Fatalf("expected 0 matches, got %d", len(got))
	}
}

func TestNoteFindByIDPrefixAmbiguous(test *testing.T) {
	store := testStore(test)
	repo := NewNoteRepo(store.DB())
	ctx := context.Background()

	mustCreatePlayer(test, store, "german")

	idA, parseErrA := uuid.Parse("abcdef12-0000-4000-8000-000000000001")

	if parseErrA != nil {
		test.Fatal(parseErrA)
	}

	idB, parseErrB := uuid.Parse("abcdef12-0000-4000-8000-000000000002")

	if parseErrB != nil {
		test.Fatal(parseErrB)
	}

	now := time.Now().UTC().Truncate(time.Millisecond)
	for _, id := range []uuid.UUID{idA, idB} {
		note := &domain.Note{
			ID:        id,
			ProjectID: domain.DefaultProjectUUID,
			PlayerID:  "german",
			Body:      "Collision " + id.String(),
			Metadata:  map[string]any{},
			CreatedAt: now,
		}

		if err := repo.Create(ctx, note); err != nil {
			test.Fatal(err)
		}
	}

	got, err := repo.FindByIDPrefix(ctx, "abcdef12")

	if err != nil {
		test.Fatalf("FindByIDPrefix: %v", err)
	}

	if len(got) != 2 {
		test.Fatalf("expected 2 matches, got %d", len(got))
	}
	seen := map[uuid.UUID]bool{got[0].ID: true, got[1].ID: true}
	if !seen[idA] || !seen[idB] {
		test.Fatalf("expected both %s and %s in results, got %v", idA, idB, got)
	}
}

func TestNoteFindByIDPrefixCaseSensitive(test *testing.T) {
	store := testStore(test)
	repo := NewNoteRepo(store.DB())
	ctx := context.Background()

	mustCreatePlayer(test, store, "german")

	note := &domain.Note{
		ID:        uuid.New(),
		ProjectID: domain.DefaultProjectUUID,
		PlayerID:  "german",
		Body:      "Case check",
		Metadata:  map[string]any{},
		CreatedAt: time.Now().UTC().Truncate(time.Millisecond),
	}

	if err := repo.Create(ctx, note); err != nil {
		test.Fatal(err)
	}

	got, err := repo.FindByIDPrefix(ctx, strings.ToUpper(note.ID.String()[:8]))

	if err != nil {
		test.Fatalf("FindByIDPrefix: %v", err)
	}

	if len(got) != 0 {
		test.Fatalf("expected 0 matches (case-sensitive), got %d", len(got))
	}
}

func TestNoteListByTask(test *testing.T) {
	store := testStore(test)
	taskRepo := NewTaskRepo(store.DB())
	repo := NewNoteRepo(store.DB())
	ctx := context.Background()

	mustCreatePlayer(test, store, "german")
	task := newTestTask()
	mustCreateTask(test, taskRepo, task)

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
		test.Fatal(err)
	}

	if err := repo.Create(ctx, projectNote); err != nil {
		test.Fatal(err)
	}

	// Filter by task — should return only the task-scoped note.
	notes, err := repo.List(ctx, repository.NoteListOptions{
		ProjectID: domain.DefaultProjectUUID,
		PlayerID:  "german",
		TaskID:    &taskID,
	})

	if err != nil {
		test.Fatal(err)
	}

	if len(notes) != 1 {
		test.Fatalf("expected 1 task-scoped note, got %d", len(notes))
	}
	if notes[0].Body != "Task-scoped" {
		test.Fatalf("expected task-scoped note, got %q", notes[0].Body)
	}
}
