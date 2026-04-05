package domain

import (
	"time"

	"github.com/google/uuid"
)

type TaskFilter struct {
	ProjectID   *string
	ParentID    *uuid.UUID
	RootID      *uuid.UUID // for tree: all descendants
	Statuses    []string   // OR match
	Tags        []string   // include
	ExcludeTags []string   // exclude
	PriorityMin *int
	PriorityMax *int
	DueAfter    *time.Time
	DueBefore   *time.Time
	WaitingOnly *bool // if true, only tasks with wait_until in future
}
