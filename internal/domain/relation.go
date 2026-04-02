package domain

import (
	"time"

	"github.com/google/uuid"
)

type Relation struct {
	ID           uuid.UUID
	SourceID     uuid.UUID
	TargetID     uuid.UUID
	RelationType string
	CreatedAt    time.Time
}
