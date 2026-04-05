package domain

import "github.com/google/uuid"

type Workflow struct {
	ID        uuid.UUID
	ProjectID string
	Name      string
	Statuses  []string
}

type WorkflowTransition struct {
	ID         uuid.UUID
	WorkflowID uuid.UUID
	FromStatus string
	ToStatus   string
}
