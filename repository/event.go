package repository

import (
	"context"
	"time"

	"github.com/germanamz/tusk/domain"
)

// EventFilter narrows the results returned by EventRepository.List.
// All pointer fields are optional — a nil pointer means "no filter on this
// field". Limit=0 means "no limit".
type EventFilter struct {
	EntityKind *domain.EntityKind
	EntityID   *string
	Type       *domain.EventType
	Since      *time.Time
	Until      *time.Time
	Limit      int
}

// EventRepository persists and retrieves domain.Event records. The data layer
// is kind-agnostic: filters operate on the generic Event fields, and payload
// decoding dispatches on Event.Type.
type EventRepository interface {
	Record(ctx context.Context, evt *domain.Event) error
	List(ctx context.Context, f EventFilter) ([]*domain.Event, error)
	Count(ctx context.Context) (int64, error)
	PruneToSize(ctx context.Context, maxRows int) (deleted int64, err error)
}
