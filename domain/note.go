package domain

import (
	"time"

	"github.com/google/uuid"
)

// Note is a player-scoped entry in the project notebook. Notes are
// append-only: they can be archived but never edited after creation.
type Note struct {
	ID         uuid.UUID
	ProjectID  uuid.UUID
	PlayerID   string     // FK to Player.ID (string, not UUID)
	TaskID     *uuid.UUID // optional — nil means project-level note
	Body       string
	Metadata   map[string]any // arbitrary key-value pairs (e.g. topic=auth)
	ArchivedAt *time.Time     // nil means active; non-nil means archived
	CreatedAt  time.Time
}
