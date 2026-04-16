package service_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/service"
	"github.com/germanamz/tusk/sqlite"
	"github.com/germanamz/tusk/sqlite/sqlitetest"
	"github.com/google/uuid"
)

type noteTestEnv struct {
	svc      *service.NoteService
	noteRepo *sqlite.NoteRepo
	projRepo *sqlite.ProjectRepo
	playRepo *sqlite.PlayerRepo
	taskRepo *sqlite.TaskRepo
}

func newNoteTestEnv(t *testing.T) *noteTestEnv {
	t.Helper()
	store, projRepo, _ := sqlitetest.NewStore(t)
	db := store.DB()
	noteRepo := sqlite.NewNoteRepo(db)
	playerRepo := sqlite.NewPlayerRepo(db)
	taskRepo := sqlite.NewTaskRepo(db)
	svc := service.NewNoteService(noteRepo, playerRepo, projRepo, taskRepo, 20)
	return &noteTestEnv{
		svc:      svc,
		noteRepo: noteRepo,
		projRepo: projRepo,
		playRepo: playerRepo,
		taskRepo: taskRepo,
	}
}

func seedPlayer(t *testing.T, repo *sqlite.PlayerRepo, id string) {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Millisecond)
	p := &domain.Player{ID: id, Type: "human", RegisteredAt: now, LastSeenAt: now}
	if err := repo.Create(context.Background(), p); err != nil {
		t.Fatalf("seed player %q: %v", id, err)
	}
}

func seedTask(t *testing.T, repo *sqlite.TaskRepo, projectID uuid.UUID, shortID string) *domain.Task {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Millisecond)
	task := &domain.Task{
		ID:         uuid.New(),
		ShortID:    shortID,
		ProjectID:  projectID,
		Title:      "test task",
		Status:     "pending",
		Version:    1,
		CreatedAt:  now,
		ModifiedAt: now,
	}
	if err := repo.Create(context.Background(), task); err != nil {
		t.Fatalf("seed task %q: %v", shortID, err)
	}
	return task
}

func TestNoteService_Create(t *testing.T) {
	env := newNoteTestEnv(t)
	ctx := context.Background()
	seedPlayer(t, env.playRepo, "p1")

	note := &domain.Note{ProjectID: uuid.Nil, PlayerID: "p1", Body: "hello"}
	if err := env.svc.Create(ctx, note); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if note.ID == uuid.Nil {
		t.Error("expected ID set")
	}
	if note.CreatedAt.IsZero() {
		t.Error("expected CreatedAt set")
	}
	if note.ArchivedAt != nil {
		t.Error("expected ArchivedAt nil on fresh note")
	}
}

func TestNoteService_Create_WithTask(t *testing.T) {
	env := newNoteTestEnv(t)
	ctx := context.Background()
	seedPlayer(t, env.playRepo, "p1")
	task := seedTask(t, env.taskRepo, uuid.Nil, "withtask1")

	note := &domain.Note{ProjectID: uuid.Nil, PlayerID: "p1", Body: "task note", TaskID: &task.ID}
	if err := env.svc.Create(ctx, note); err != nil {
		t.Fatalf("Create: %v", err)
	}
}

func TestNoteService_Create_TaskWrongProject(t *testing.T) {
	env := newNoteTestEnv(t)
	ctx := context.Background()
	seedPlayer(t, env.playRepo, "p1")
	other := sqlitetest.SeedProject(t, env.projRepo, "other")
	task := seedTask(t, env.taskRepo, uuid.Nil, "wrongproj")

	note := &domain.Note{ProjectID: other.ID, PlayerID: "p1", Body: "oops", TaskID: &task.ID}
	err := env.svc.Create(ctx, note)
	if err == nil {
		t.Fatal("expected error for task in wrong project")
	}
	if !strings.Contains(err.Error(), "belongs to project") {
		t.Errorf("got %v, want message containing 'belongs to project'", err)
	}
}

func TestNoteService_Create_EmptyBody(t *testing.T) {
	env := newNoteTestEnv(t)
	ctx := context.Background()
	seedPlayer(t, env.playRepo, "p1")

	note := &domain.Note{ProjectID: uuid.Nil, PlayerID: "p1", Body: "  "}
	err := env.svc.Create(ctx, note)
	if err == nil {
		t.Fatal("expected error for empty body")
	}
	if !strings.Contains(err.Error(), "body must not be empty") {
		t.Errorf("got %v, want 'body must not be empty'", err)
	}
}

func TestNoteService_Create_MissingPlayer(t *testing.T) {
	env := newNoteTestEnv(t)
	ctx := context.Background()

	note := &domain.Note{ProjectID: uuid.Nil, PlayerID: "ghost", Body: "hi"}
	err := env.svc.Create(ctx, note)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("got %v, want wrapping ErrNotFound", err)
	}
}

func TestNoteService_Create_MissingProject(t *testing.T) {
	env := newNoteTestEnv(t)
	ctx := context.Background()
	seedPlayer(t, env.playRepo, "p1")

	note := &domain.Note{ProjectID: uuid.New(), PlayerID: "p1", Body: "hi"}
	err := env.svc.Create(ctx, note)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("got %v, want wrapping ErrNotFound", err)
	}
}

func TestNoteService_Archive(t *testing.T) {
	env := newNoteTestEnv(t)
	ctx := context.Background()
	seedPlayer(t, env.playRepo, "p1")

	note := &domain.Note{ProjectID: uuid.Nil, PlayerID: "p1", Body: "to archive"}
	if err := env.svc.Create(ctx, note); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := env.svc.Archive(ctx, note.ID, "p1"); err != nil {
		t.Fatalf("Archive: %v", err)
	}
	got, err := env.noteRepo.GetByID(ctx, note.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.ArchivedAt == nil {
		t.Error("expected ArchivedAt set after Archive")
	}
}

func TestNoteService_Archive_NotAuthor(t *testing.T) {
	env := newNoteTestEnv(t)
	ctx := context.Background()
	seedPlayer(t, env.playRepo, "p1")
	seedPlayer(t, env.playRepo, "p2")

	note := &domain.Note{ProjectID: uuid.Nil, PlayerID: "p1", Body: "mine"}
	if err := env.svc.Create(ctx, note); err != nil {
		t.Fatalf("Create: %v", err)
	}
	err := env.svc.Archive(ctx, note.ID, "p2")
	if !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("got %v, want wrapping ErrForbidden", err)
	}
}

func TestNoteService_Archive_NotFound(t *testing.T) {
	env := newNoteTestEnv(t)
	ctx := context.Background()

	err := env.svc.Archive(ctx, uuid.New(), "p1")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("got %v, want wrapping ErrNotFound", err)
	}
}

func TestNoteService_List_DefaultWindow(t *testing.T) {
	env := newNoteTestEnv(t)
	ctx := context.Background()
	seedPlayer(t, env.playRepo, "p1")

	for i := range 25 {
		note := &domain.Note{ProjectID: uuid.Nil, PlayerID: "p1", Body: "n"}
		if err := env.svc.Create(ctx, note); err != nil {
			t.Fatalf("Create %d: %v", i, err)
		}
	}
	notes, err := env.svc.List(ctx, service.NoteListParams{ProjectID: uuid.Nil})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(notes) != 20 {
		t.Errorf("got %d notes, want 20", len(notes))
	}
}

func TestNoteService_List_PlayerFilter(t *testing.T) {
	env := newNoteTestEnv(t)
	ctx := context.Background()
	seedPlayer(t, env.playRepo, "p1")
	seedPlayer(t, env.playRepo, "p2")

	for i := range 3 {
		if err := env.svc.Create(ctx, &domain.Note{ProjectID: uuid.Nil, PlayerID: "p1", Body: "a"}); err != nil {
			t.Fatalf("Create p1 %d: %v", i, err)
		}
	}
	for i := range 2 {
		if err := env.svc.Create(ctx, &domain.Note{ProjectID: uuid.Nil, PlayerID: "p2", Body: "b"}); err != nil {
			t.Fatalf("Create p2 %d: %v", i, err)
		}
	}
	notes, err := env.svc.List(ctx, service.NoteListParams{ProjectID: uuid.Nil, PlayerID: "p1"})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(notes) != 3 {
		t.Errorf("got %d, want 3", len(notes))
	}
}
