package domain

import (
	"time"

	"github.com/google/uuid"
)

type Annotation struct {
	ID        uuid.UUID
	TaskID    uuid.UUID
	Body      string
	CreatedAt time.Time
}
