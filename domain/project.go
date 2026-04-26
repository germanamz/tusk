package domain

import (
	"time"

	"github.com/google/uuid"
)

// Project is a persisted container for tasks. Each project binds to a workflow
// and carries per-project settings (automation + urgency overrides).
type Project struct {
	ID          uuid.UUID
	Name        string
	WorkflowID  uuid.UUID
	Description string
	Settings    ProjectSettings
	Version     int
	CreatedAt   time.Time
	UpdatedAt   time.Time
}
