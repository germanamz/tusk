package domain

import (
	"time"

	"github.com/google/uuid"
)

type Project struct {
	ID              uuid.UUID
	Name            string
	Description     string
	DefaultWorkflow string
	Settings        ProjectSettings
	CreatedAt       time.Time
}
