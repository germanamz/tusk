package domain

import (
	"time"

	"github.com/google/uuid"
)

type Task struct {
	ID             uuid.UUID
	ShortID        string
	ParentID       *uuid.UUID
	ProjectID      *uuid.UUID
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
// For nullable fields (ParentID, ProjectID, DueAt, WaitUntil, RecurrenceRule),
// a double pointer is used: outer nil = don't change, outer non-nil + inner nil = set to NULL,
// outer non-nil + inner non-nil = set to value.
type TaskUpdate struct {
	ShortID        string // required — identifies the task to update
	Version        int    // required — optimistic locking check
	Title          *string
	Description    *string
	Status         *string
	Priority       *int
	ParentID       **uuid.UUID
	ProjectID      **uuid.UUID
	DueAt          **time.Time
	WaitUntil      **time.Time
	RecurrenceRule **string
	UDA            *map[string]any
}
