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

const defaultHardcodedWindowSize = 20

// NewNoteService wires the repositories and default List window size.
// A non-positive defaultWindowSize leaves the field at zero; resolution
// then falls through to the hardcoded default.
func NewNoteService(
	notes repository.NoteRepository,
	players repository.PlayerRepository,
	projects repository.ProjectRepository,
	tasks repository.TaskRepository,
	defaultWindowSize int,
) *NoteService {
	return &NoteService{
		notes:             notes,
		players:           players,
		projects:          projects,
		tasks:             tasks,
		defaultWindowSize: defaultWindowSize,
	}
}

// resolveWindowSize walks the resolution chain: CLI override → player DB
// setting → project settings → global config default → hardcoded fallback.
// Lookup errors are swallowed — they cause fallthrough to the next tier.
func (s *NoteService) resolveWindowSize(ctx context.Context, callerPlayerID string, projectID uuid.UUID, override *int) int {
	if override != nil && *override > 0 {
		return *override
	}

	if callerPlayerID != "" {
		if player, err := s.players.GetByID(ctx, callerPlayerID); err == nil && player.NoteWindowSize != nil && *player.NoteWindowSize > 0 {
			return *player.NoteWindowSize
		}
	}

	if project, err := s.projects.GetByID(ctx, projectID); err == nil && project.Settings.NoteWindowSize != nil && *project.Settings.NoteWindowSize > 0 {
		return *project.Settings.NoteWindowSize
	}

	if s.defaultWindowSize > 0 {
		return s.defaultWindowSize
	}

	return defaultHardcodedWindowSize
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

// GetByID retrieves a note by its UUID.
func (s *NoteService) GetByID(ctx context.Context, id uuid.UUID) (*domain.Note, error) {
	return s.notes.GetByID(ctx, id)
}

// FindByIDPrefix resolves a UUID prefix to notes. Used by the CLI to
// accept short IDs for archive. See repository.NoteRepository for
// semantics.
func (s *NoteService) FindByIDPrefix(ctx context.Context, prefix string) ([]*domain.Note, error) {
	return s.notes.FindByIDPrefix(ctx, prefix)
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

// List returns notes newest-first. Limit is resolved through the window
// resolution chain (override → player → project → config → hardcoded).
func (s *NoteService) List(ctx context.Context, params NoteListParams) ([]*domain.Note, error) {
	limit := s.resolveWindowSize(ctx, params.CallerPlayerID, params.ProjectID, params.WindowOverride)

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

// ListAllForProject returns every active (non-archived) note in a project
// with no window cap. Used by exporters (markdown tree, portability dump)
// that need the full set rather than the player-facing recent window. Does
// not consult the window resolution chain.
func (s *NoteService) ListAllForProject(ctx context.Context, projectID uuid.UUID) ([]*domain.Note, error) {
	return s.notes.List(ctx, repository.NoteListOptions{
		ProjectID:       projectID,
		IncludeArchived: false,
		Limit:           0,
	})
}
