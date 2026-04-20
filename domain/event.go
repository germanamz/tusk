package domain

import (
	"time"

	"github.com/google/uuid"
)

// EventType is an open string alias. Predefined constants for the event
// types shipping services emit live in domain/event_task.go and
// domain/event_relation.go. Future initiatives declare additional
// constants without touching this file.
type EventType string

// EntityKind is an open string alias identifying which entity an event
// describes. Predefined constants ship for the kinds that emit events
// today; future kinds add their own constants.
type EntityKind string

const (
	EntityTask     EntityKind = "task"
	EntityRelation EntityKind = "relation"
)

// Event is the generic event record stored in the events table.
type Event struct {
	ID         uuid.UUID
	Type       EventType
	EntityID   string // opaque; UUID string for tasks/relations, arbitrary for future kinds
	EntityKind EntityKind
	PlayerID   *string // nil when no actor is attached to context
	Payload    EventPayload
	CreatedAt  time.Time
}

// EventPayload is a sealed interface realized by every typed payload
// struct. The sqlite Record path asserts Event.Type == Payload.EventKind()
// so the stored discriminator cannot drift from the struct identity.
type EventPayload interface {
	EventKind() EventType
}

// UnknownPayload is returned by the sqlite List path when an event row's
// event_type is not one of the predefined constants. It lets future
// consumers round-trip events whose kind they don't recognize.
type UnknownPayload struct {
	Kind EventType      `json:"kind"`
	Raw  map[string]any `json:"-"` // decoded from the stored JSON
}

// EventKind returns the stored discriminator so UnknownPayload satisfies
// the EventPayload interface.
func (p UnknownPayload) EventKind() EventType { return p.Kind }
