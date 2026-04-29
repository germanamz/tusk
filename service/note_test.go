package service_test

import (
	"context"
	"errors"
	"fmt"
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

func newNoteTestEnv(test *testing.T) *noteTestEnv {
	return newNoteTestEnvWithWindow(test, 20)
}

func newNoteTestEnvWithWindow(test *testing.T, defaultWindow int) *noteTestEnv {
	test.Helper()
	store, projRepo, _ := sqlitetest.NewStore(test)
	db := store.DB()
	noteRepo := sqlite.NewNoteRepo(db)
	playerRepo := sqlite.NewPlayerRepo(db)
	taskRepo := sqlite.NewTaskRepo(db)
	svc := service.NewNoteService(noteRepo, playerRepo, projRepo, taskRepo, defaultWindow)
	return &noteTestEnv{
		svc:      svc,
		noteRepo: noteRepo,
		projRepo: projRepo,
		playRepo: playerRepo,
		taskRepo: taskRepo,
	}
}

func seedPlayerWithWindow(test *testing.T, repo *sqlite.PlayerRepo, id string, size *int) {
	test.Helper()
	now := time.Now().UTC().Truncate(time.Millisecond)
	player := &domain.Player{ID: id, Type: "human", NoteWindowSize: size, RegisteredAt: now, LastSeenAt: now}
	createErr := repo.Create(context.Background(), player)

	if createErr != nil {
		test.Fatalf("seed player %q: %v", id, createErr)
	}
}

func setProjectWindow(test *testing.T, repo *sqlite.ProjectRepo, id uuid.UUID, size *int) {
	test.Helper()
	ctx := context.Background()
	proj, getErr := repo.GetByID(ctx, id)

	if getErr != nil {
		test.Fatalf("get project: %v", getErr)
	}

	proj.Settings.NoteWindowSize = size
	updateErr := repo.Update(ctx, proj)

	if updateErr != nil {
		test.Fatalf("update project: %v", updateErr)
	}
}

func seedNotes(test *testing.T, svc *service.NoteService, count int, playerID string, projectID uuid.UUID) {
	test.Helper()
	ctx := context.Background()
	for index := range count {
		note := &domain.Note{
			ProjectID: projectID,
			PlayerID:  playerID,
			Body:      fmt.Sprintf("note %d", index),
		}
		if err := svc.Create(ctx, note); err != nil {
			test.Fatalf("seed note %d: %v", index, err)
		}
	}
}

func intPtr(value int) *int { return &value }

func seedPlayer(test *testing.T, repo *sqlite.PlayerRepo, id string) {
	test.Helper()
	now := time.Now().UTC().Truncate(time.Millisecond)
	player := &domain.Player{ID: id, Type: "human", RegisteredAt: now, LastSeenAt: now}
	if err := repo.Create(context.Background(), player); err != nil {
		test.Fatalf("seed player %q: %v", id, err)
	}
}

func seedTask(test *testing.T, repo *sqlite.TaskRepo, projectID uuid.UUID, shortID string) *domain.Task {
	test.Helper()
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
		test.Fatalf("seed task %q: %v", shortID, err)
	}
	return task
}

func TestNoteService_Create(test *testing.T) {
	env := newNoteTestEnv(test)
	ctx := context.Background()
	seedPlayer(test, env.playRepo, "p1")

	note := &domain.Note{ProjectID: uuid.Nil, PlayerID: "p1", Body: "hello"}
	if err := env.svc.Create(ctx, note); err != nil {
		test.Fatalf("Create: %v", err)
	}
	if note.ID == uuid.Nil {
		test.Error("expected ID set")
	}
	if note.CreatedAt.IsZero() {
		test.Error("expected CreatedAt set")
	}
	if note.ArchivedAt != nil {
		test.Error("expected ArchivedAt nil on fresh note")
	}
}

func TestNoteService_Create_WithTask(test *testing.T) {
	env := newNoteTestEnv(test)
	ctx := context.Background()
	seedPlayer(test, env.playRepo, "p1")
	task := seedTask(test, env.taskRepo, uuid.Nil, "withtask1")

	note := &domain.Note{ProjectID: uuid.Nil, PlayerID: "p1", Body: "task note", TaskID: &task.ID}
	if err := env.svc.Create(ctx, note); err != nil {
		test.Fatalf("Create: %v", err)
	}
}

func TestNoteService_Create_TaskWrongProject(test *testing.T) {
	env := newNoteTestEnv(test)
	ctx := context.Background()
	seedPlayer(test, env.playRepo, "p1")
	other := sqlitetest.SeedProject(test, env.projRepo, "other")
	task := seedTask(test, env.taskRepo, uuid.Nil, "wrongproj")

	note := &domain.Note{ProjectID: other.ID, PlayerID: "p1", Body: "oops", TaskID: &task.ID}
	err := env.svc.Create(ctx, note)
	if err == nil {
		test.Fatal("expected error for task in wrong project")
	}
	if !strings.Contains(err.Error(), "belongs to project") {
		test.Errorf("got %v, want message containing 'belongs to project'", err)
	}
}

func TestNoteService_Create_EmptyBody(test *testing.T) {
	env := newNoteTestEnv(test)
	ctx := context.Background()
	seedPlayer(test, env.playRepo, "p1")

	note := &domain.Note{ProjectID: uuid.Nil, PlayerID: "p1", Body: "  "}
	err := env.svc.Create(ctx, note)
	if err == nil {
		test.Fatal("expected error for empty body")
	}
	if !strings.Contains(err.Error(), "body must not be empty") {
		test.Errorf("got %v, want 'body must not be empty'", err)
	}
}

func TestNoteService_Create_MissingPlayer(test *testing.T) {
	env := newNoteTestEnv(test)
	ctx := context.Background()

	note := &domain.Note{ProjectID: uuid.Nil, PlayerID: "ghost", Body: "hi"}
	err := env.svc.Create(ctx, note)
	if !errors.Is(err, domain.ErrNotFound) {
		test.Fatalf("got %v, want wrapping ErrNotFound", err)
	}
}

func TestNoteService_Create_MissingProject(test *testing.T) {
	env := newNoteTestEnv(test)
	ctx := context.Background()
	seedPlayer(test, env.playRepo, "p1")

	note := &domain.Note{ProjectID: uuid.New(), PlayerID: "p1", Body: "hi"}
	err := env.svc.Create(ctx, note)
	if !errors.Is(err, domain.ErrNotFound) {
		test.Fatalf("got %v, want wrapping ErrNotFound", err)
	}
}

func TestNoteService_GetByID(test *testing.T) {
	env := newNoteTestEnv(test)
	ctx := context.Background()
	seedPlayer(test, env.playRepo, "p1")

	note := &domain.Note{ProjectID: uuid.Nil, PlayerID: "p1", Body: "fetch me"}
	createErr := env.svc.Create(ctx, note)

	if createErr != nil {
		test.Fatalf("Create: %v", createErr)
	}

	got, getErr := env.svc.GetByID(ctx, note.ID)

	if getErr != nil {
		test.Fatalf("GetByID: %v", getErr)
	}

	if got.ID != note.ID || got.Body != "fetch me" {
		test.Errorf("got %+v, want id=%s body=%q", got, note.ID, "fetch me")
	}

	if _, err := env.svc.GetByID(ctx, uuid.New()); !errors.Is(err, domain.ErrNotFound) {
		test.Errorf("GetByID unknown: got %v, want wrapping ErrNotFound", err)
	}
}

func TestNoteService_FindByIDPrefix(test *testing.T) {
	env := newNoteTestEnv(test)
	ctx := context.Background()
	seedPlayer(test, env.playRepo, "p1")

	note := &domain.Note{ProjectID: uuid.Nil, PlayerID: "p1", Body: "prefix me"}
	createErr := env.svc.Create(ctx, note)

	if createErr != nil {
		test.Fatalf("Create: %v", createErr)
	}

	prefix := note.ID.String()[:8]
	matches, matchErr := env.svc.FindByIDPrefix(ctx, prefix)

	if matchErr != nil {
		test.Fatalf("FindByIDPrefix: %v", matchErr)
	}

	if len(matches) != 1 || matches[0].ID != note.ID {
		test.Errorf("got %d matches, want 1 matching %s", len(matches), note.ID)
	}

	none, noneErr := env.svc.FindByIDPrefix(ctx, "00000000")

	if noneErr != nil {
		test.Fatalf("FindByIDPrefix miss: %v", noneErr)
	}

	if len(none) != 0 {
		test.Errorf("expected empty result for miss, got %d", len(none))
	}
}

func TestNoteService_Archive(test *testing.T) {
	env := newNoteTestEnv(test)
	ctx := context.Background()
	seedPlayer(test, env.playRepo, "p1")

	note := &domain.Note{ProjectID: uuid.Nil, PlayerID: "p1", Body: "to archive"}
	createErr := env.svc.Create(ctx, note)

	if createErr != nil {
		test.Fatalf("Create: %v", createErr)
	}

	archiveErr := env.svc.Archive(ctx, note.ID, "p1")

	if archiveErr != nil {
		test.Fatalf("Archive: %v", archiveErr)
	}

	got, getErr := env.noteRepo.GetByID(ctx, note.ID)

	if getErr != nil {
		test.Fatalf("GetByID: %v", getErr)
	}

	if got.ArchivedAt == nil {
		test.Error("expected ArchivedAt set after Archive")
	}
}

func TestNoteService_Archive_NotAuthor(test *testing.T) {
	env := newNoteTestEnv(test)
	ctx := context.Background()
	seedPlayer(test, env.playRepo, "p1")
	seedPlayer(test, env.playRepo, "p2")

	note := &domain.Note{ProjectID: uuid.Nil, PlayerID: "p1", Body: "mine"}
	if err := env.svc.Create(ctx, note); err != nil {
		test.Fatalf("Create: %v", err)
	}
	err := env.svc.Archive(ctx, note.ID, "p2")
	if !errors.Is(err, domain.ErrForbidden) {
		test.Fatalf("got %v, want wrapping ErrForbidden", err)
	}
}

func TestNoteService_Archive_NotFound(test *testing.T) {
	env := newNoteTestEnv(test)
	ctx := context.Background()

	err := env.svc.Archive(ctx, uuid.New(), "p1")
	if !errors.Is(err, domain.ErrNotFound) {
		test.Fatalf("got %v, want wrapping ErrNotFound", err)
	}
}

func TestNoteService_List_DefaultWindow(test *testing.T) {
	env := newNoteTestEnv(test)
	ctx := context.Background()
	seedPlayer(test, env.playRepo, "p1")

	for index := range 25 {
		note := &domain.Note{ProjectID: uuid.Nil, PlayerID: "p1", Body: "n"}
		if err := env.svc.Create(ctx, note); err != nil {
			test.Fatalf("Create %d: %v", index, err)
		}
	}
	notes, listErr := env.svc.List(ctx, service.NoteListParams{ProjectID: uuid.Nil})

	if listErr != nil {
		test.Fatalf("List: %v", listErr)
	}

	if len(notes) != 20 {
		test.Errorf("got %d notes, want 20", len(notes))
	}
}

func TestNoteService_List_PlayerFilter(test *testing.T) {
	env := newNoteTestEnv(test)
	ctx := context.Background()
	seedPlayer(test, env.playRepo, "p1")
	seedPlayer(test, env.playRepo, "p2")

	for index := range 3 {
		if err := env.svc.Create(ctx, &domain.Note{ProjectID: uuid.Nil, PlayerID: "p1", Body: "a"}); err != nil {
			test.Fatalf("Create p1 %d: %v", index, err)
		}
	}
	for index := range 2 {
		if err := env.svc.Create(ctx, &domain.Note{ProjectID: uuid.Nil, PlayerID: "p2", Body: "b"}); err != nil {
			test.Fatalf("Create p2 %d: %v", index, err)
		}
	}
	notes, listErr := env.svc.List(ctx, service.NoteListParams{ProjectID: uuid.Nil, PlayerID: "p1"})

	if listErr != nil {
		test.Fatalf("List: %v", listErr)
	}

	if len(notes) != 3 {
		test.Errorf("got %d, want 3", len(notes))
	}
}

func TestNoteService_ResolveWindow_CLIOverride(test *testing.T) {
	env := newNoteTestEnvWithWindow(test, 20)
	ctx := context.Background()
	seedPlayerWithWindow(test, env.playRepo, "p1", intPtr(50))
	setProjectWindow(test, env.projRepo, uuid.Nil, intPtr(30))
	seedNotes(test, env.svc, 15, "p1", uuid.Nil)

	notes, listErr := env.svc.List(ctx, service.NoteListParams{
		ProjectID:      uuid.Nil,
		CallerPlayerID: "p1",
		WindowOverride: intPtr(10),
	})

	if listErr != nil {
		test.Fatalf("List: %v", listErr)
	}

	if len(notes) != 10 {
		test.Errorf("got %d notes, want 10 (CLI override)", len(notes))
	}
}

func TestNoteService_ResolveWindow_PlayerSetting(test *testing.T) {
	env := newNoteTestEnvWithWindow(test, 20)
	ctx := context.Background()
	seedPlayerWithWindow(test, env.playRepo, "p1", intPtr(5))
	setProjectWindow(test, env.projRepo, uuid.Nil, intPtr(30))
	seedNotes(test, env.svc, 10, "p1", uuid.Nil)

	notes, listErr := env.svc.List(ctx, service.NoteListParams{
		ProjectID:      uuid.Nil,
		CallerPlayerID: "p1",
	})

	if listErr != nil {
		test.Fatalf("List: %v", listErr)
	}

	if len(notes) != 5 {
		test.Errorf("got %d notes, want 5 (player setting)", len(notes))
	}
}

func TestNoteService_ResolveWindow_ProjectSetting(test *testing.T) {
	env := newNoteTestEnvWithWindow(test, 20)
	ctx := context.Background()
	seedPlayerWithWindow(test, env.playRepo, "p1", nil)
	setProjectWindow(test, env.projRepo, uuid.Nil, intPtr(8))
	seedNotes(test, env.svc, 12, "p1", uuid.Nil)

	notes, listErr := env.svc.List(ctx, service.NoteListParams{
		ProjectID:      uuid.Nil,
		CallerPlayerID: "p1",
	})

	if listErr != nil {
		test.Fatalf("List: %v", listErr)
	}

	if len(notes) != 8 {
		test.Errorf("got %d notes, want 8 (project setting)", len(notes))
	}
}

func TestNoteService_ResolveWindow_ConfigDefault(test *testing.T) {
	env := newNoteTestEnvWithWindow(test, 15)
	ctx := context.Background()
	seedPlayerWithWindow(test, env.playRepo, "p1", nil)
	seedNotes(test, env.svc, 20, "p1", uuid.Nil)

	notes, listErr := env.svc.List(ctx, service.NoteListParams{
		ProjectID:      uuid.Nil,
		CallerPlayerID: "p1",
	})

	if listErr != nil {
		test.Fatalf("List: %v", listErr)
	}

	if len(notes) != 15 {
		test.Errorf("got %d notes, want 15 (config default)", len(notes))
	}
}

func TestNoteService_ResolveWindow_HardcodedFallback(test *testing.T) {
	env := newNoteTestEnvWithWindow(test, 0)
	ctx := context.Background()
	seedPlayerWithWindow(test, env.playRepo, "p1", nil)
	seedNotes(test, env.svc, 25, "p1", uuid.Nil)

	notes, listErr := env.svc.List(ctx, service.NoteListParams{
		ProjectID:      uuid.Nil,
		CallerPlayerID: "p1",
	})

	if listErr != nil {
		test.Fatalf("List: %v", listErr)
	}

	if len(notes) != 20 {
		test.Errorf("got %d notes, want 20 (hardcoded fallback)", len(notes))
	}
}
