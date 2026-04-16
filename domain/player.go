package domain

import "time"

// Player represents a human or agent that interacts with tusk.
// Players self-register on first contact. The ID is a self-declared
// unique string (not a UUID). Type is immutable after creation.
type Player struct {
	ID             string
	Type           string // "human" or "agent"
	NoteWindowSize *int   // nil = no preference; player uses project/global default
	RegisteredAt   time.Time
	LastSeenAt     time.Time
}
