package repository

import (
	"context"
	"time"

	"github.com/germanamz/tusk/domain"
	"github.com/google/uuid"
)

// NoteListOptions controls the List query at the storage level.
// All fields are optional — zero values mean "no filter".
type NoteListOptions struct {
	ProjectID       uuid.UUID  // required — scopes to a single project
	PlayerID        string     // empty = all players
	TaskID          *uuid.UUID // nil = all notes (project + task-scoped); non-nil = only that task
	Since           *time.Time // nil = no lower bound; non-nil = created_at >= since
	IncludeArchived bool       // false = active only (archived_at IS NULL)
	Limit           int        // 0 = no limit; positive = SQL LIMIT
}

// NoteRepository defines storage operations for Note entities.
// Create and Archive are the only mutations — notes cannot be edited.
// GetByID returns domain.ErrNotFound if no note matches.
// Archive returns domain.ErrNotFound if no note matches.
type NoteRepository interface {
	Create(ctx context.Context, note *domain.Note) error
	GetByID(ctx context.Context, id uuid.UUID) (*domain.Note, error)
	Archive(ctx context.Context, id uuid.UUID, archivedAt time.Time) error
	List(ctx context.Context, opts NoteListOptions) ([]*domain.Note, error)
}
