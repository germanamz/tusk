package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/repository"
	"github.com/google/uuid"
)

// NoteService provides append-only note operations over a single SQLite store.
// Notes belong to a (project, player) pair and optionally reference a task.
type NoteService struct {
	notes             repository.NoteRepository
	players           repository.PlayerRepository
	projects          repository.ProjectRepository
	tasks             repository.TaskRepository
	defaultWindowSize int
}

// NewNoteService wires the repositories and default List window size.
// A non-positive defaultWindowSize falls back to 20.
func NewNoteService(
	notes repository.NoteRepository,
	players repository.PlayerRepository,
	projects repository.ProjectRepository,
	tasks repository.TaskRepository,
	defaultWindowSize int,
) *NoteService {
	if defaultWindowSize <= 0 {
		defaultWindowSize = 20
	}
	return &NoteService{
		notes:             notes,
		players:           players,
		projects:          projects,
		tasks:             tasks,
		defaultWindowSize: defaultWindowSize,
	}
}

// Create validates, stamps, and persists a new note. The caller provides
// ProjectID, PlayerID, Body, and optionally TaskID and Metadata; Create
// fills in ID and CreatedAt and forces ArchivedAt to nil.
func (s *NoteService) Create(ctx context.Context, note *domain.Note) error {
	note.Body = strings.TrimSpace(note.Body)
	if note.Body == "" {
		return fmt.Errorf("note body must not be empty")
	}

	if _, err := s.players.GetByID(ctx, note.PlayerID); err != nil {
		return fmt.Errorf("player %q: %w", note.PlayerID, err)
	}

	if _, err := s.projects.GetByID(ctx, note.ProjectID); err != nil {
		return fmt.Errorf("project %v: %w", note.ProjectID, err)
	}

	if note.TaskID != nil {
		task, err := s.tasks.GetByID(ctx, *note.TaskID)
		if err != nil {
			return fmt.Errorf("task %v: %w", *note.TaskID, err)
		}
		if task.ProjectID != note.ProjectID {
			return fmt.Errorf("task %s belongs to project %v, not %v", task.ShortID, task.ProjectID, note.ProjectID)
		}
	}

	note.ID = uuid.New()
	note.CreatedAt = time.Now().UTC().Truncate(time.Millisecond)
	note.ArchivedAt = nil
	if note.Metadata == nil {
		note.Metadata = make(map[string]any)
	}

	return s.notes.Create(ctx, note)
}

// Archive soft-deletes a note. Only the authoring player may archive.
// Returns domain.ErrNotFound if no note matches, or wraps
// domain.ErrForbidden when the caller is not the author.
func (s *NoteService) Archive(ctx context.Context, id uuid.UUID, callerPlayerID string) error {
	note, err := s.notes.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if note.PlayerID != callerPlayerID {
		return fmt.Errorf("note %s belongs to player %q, not %q: %w", id, note.PlayerID, callerPlayerID, domain.ErrForbidden)
	}
	return s.notes.Archive(ctx, id, time.Now().UTC().Truncate(time.Millisecond))
}

// NoteListParams controls List at the service level. CallerPlayerID is
// carried now but only consumed by the window resolution chain starting
// in phase 2; keeping it here avoids changing the method signature.
type NoteListParams struct {
	ProjectID       uuid.UUID
	PlayerID        string
	CallerPlayerID  string
	TaskID          *uuid.UUID
	Since           *time.Time
	IncludeArchived bool
	WindowOverride  *int
}

// List returns notes newest-first. Applies the default window size unless
// WindowOverride supplies a positive override.
func (s *NoteService) List(ctx context.Context, params NoteListParams) ([]*domain.Note, error) {
	limit := s.defaultWindowSize
	if params.WindowOverride != nil && *params.WindowOverride > 0 {
		limit = *params.WindowOverride
	}

	opts := repository.NoteListOptions{
		ProjectID:       params.ProjectID,
		PlayerID:        params.PlayerID,
		TaskID:          params.TaskID,
		Since:           params.Since,
		IncludeArchived: params.IncludeArchived,
		Limit:           limit,
	}
	return s.notes.List(ctx, opts)
}
