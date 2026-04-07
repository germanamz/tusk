package domain

import (
	"fmt"
	"regexp"
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
	ClaimedBy      *string    // FK to Player.ID — who holds the claim
	ClaimedAt      *time.Time // when the claim was made
	Urgency        float64    // Computed at read time, not persisted in DB.
}

// TaskUpdate represents a partial update to a task.
// Nil pointer fields mean "don't change this field".
// For nullable/clearable fields (ParentID, DueAt, WaitUntil, RecurrenceRule, Description),
// a double pointer is used: outer nil = don't change, outer non-nil + inner nil = set to NULL/empty,
// outer non-nil + inner non-nil = set to value.
// ProjectID uses a single pointer: nil = don't change, non-nil = set to value.
type TaskUpdate struct {
	ShortID        string // required — identifies the task to update
	Version        int    // required — optimistic locking check
	Title          *string
	Description    **string
	Status         *string
	Priority       *int
	ParentID       **uuid.UUID
	ProjectID      *string
	DueAt          **time.Time
	WaitUntil      **time.Time
	RecurrenceRule **string
	UDA            *map[string]any
	ClaimedBy      **string    // nil = don't change, *nil = clear, *"value" = set
	ClaimedAt      **time.Time // nil = don't change, *nil = clear, *value = set
}

// udaKeyPattern matches valid UDA key names: starts with letter or underscore,
// followed by alphanumeric, hyphens, or underscores.
var udaKeyPattern = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_-]*$`)

// ValidateUDAKey checks that a UDA key name is valid.
// Keys must start with a letter or underscore, followed by alphanumeric,
// hyphens, or underscores. This prevents injection of JSON path characters
// (., $, [) which are used in SQLite json_extract queries.
func ValidateUDAKey(key string) error {
	if key == "" {
		return fmt.Errorf("UDA key must not be empty")
	}
	if !udaKeyPattern.MatchString(key) {
		return fmt.Errorf("invalid UDA key %q: must match %s", key, udaKeyPattern.String())
	}
	return nil
}

// ValidateUDA checks that all keys are valid and all values are strings.
// A nil map is valid (no UDAs). Returns a descriptive error naming the
// offending key or value type.
func ValidateUDA(uda map[string]any) error {
	for k, v := range uda {
		if err := ValidateUDAKey(k); err != nil {
			return err
		}
		if _, ok := v.(string); !ok {
			return fmt.Errorf("UDA value for %q must be a string, got %T", k, v)
		}
	}
	return nil
}
