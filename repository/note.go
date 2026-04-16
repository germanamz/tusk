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
// FindByIDPrefix resolves a UUID prefix to zero, one, or more candidate notes.
type NoteRepository interface {
	Create(ctx context.Context, note *domain.Note) error
	GetByID(ctx context.Context, id uuid.UUID) (*domain.Note, error)
	// FindByIDPrefix returns notes whose UUID (hyphenated lowercase form)
	// begins with the given prefix. Prefix must contain at least 8 characters;
	// shorter prefixes return (nil, nil) after the caller-side guard runs, so
	// callers should enforce length themselves and rely on this method to
	// report actual collisions. An exact 36-char UUID is accepted and will
	// match at most one row. Results are returned in deterministic order
	// (ascending UUID string). The repository does not distinguish active
	// from archived notes here; the caller decides how to treat archived
	// matches.
	FindByIDPrefix(ctx context.Context, prefix string) ([]*domain.Note, error)
	Archive(ctx context.Context, id uuid.UUID, archivedAt time.Time) error
	List(ctx context.Context, opts NoteListOptions) ([]*domain.Note, error)
}
