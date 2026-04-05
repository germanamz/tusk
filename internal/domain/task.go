package domain

import (
	"time"

	"github.com/google/uuid"
)

type Task struct {
	ID             uuid.UUID
	ShortID        string
	ParentID       *uuid.UUID
	ProjectID      string
	Title          string
	Description    string
	Status         string
	Priority       int
	Version        int
	DueAt          *time.Time
	WaitUntil      *time.Time
	RecurrenceRule *string
	UDA            map[string]any
	CreatedAt      time.Time
	ModifiedAt     time.Time
}

// TaskUpdate represents a partial update to a task.
// Nil pointer fields mean "don't change this field".
// For nullable fields (ParentID, DueAt, WaitUntil, RecurrenceRule),
// a double pointer is used: outer nil = don't change, outer non-nil + inner nil = set to NULL,
// outer non-nil + inner non-nil = set to value.
// ProjectID uses a single pointer: nil = don't change, non-nil = set to value.
type TaskUpdate struct {
	ShortID        string // required — identifies the task to update
	Version        int    // required — optimistic locking check
	Title          *string
	Description    *string
	Status         *string
	Priority       *int
	ParentID       **uuid.UUID
	ProjectID      *string
	DueAt          **time.Time
	WaitUntil      **time.Time
	RecurrenceRule **string
	UDA            *map[string]any
}
