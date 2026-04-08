package domain

import "time"

// Player represents a human or agent that interacts with tusk.
// Players self-register on first contact. The ID is a self-declared
// unique string (not a UUID). Type is immutable after creation.
type Player struct {
	ID           string
	Type         string // "human" or "agent"
	RegisteredAt time.Time
	LastSeenAt   time.Time
}
