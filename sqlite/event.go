package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/germanamz/tusk/domain"
	"github.com/germanamz/tusk/repository"
	"github.com/google/uuid"
)

// eventColumns lists the columns selected in every event query. Order must
// match what scanEvent expects in its Scan call.
const eventColumns = `id, event_type, entity_id, entity_kind, player_id, payload, created_at`

// EventRepo implements repository.EventRepository using SQLite.
//
// Retention is enforced lazily: every Record call checks the row count and
// deletes the oldest rows once count exceeds maxEvents+pruneSlack. Callers
// that need a specific retention size on demand use PruneToSize.
type EventRepo struct {
	db         DBTX
	maxEvents  int
	pruneSlack int
}

// NewEventRepo returns an EventRepo. maxEvents=0 disables retention entirely;
// any positive value triggers lazy pruning back down to maxEvents once the
// row count exceeds maxEvents+pruneSlack.
func NewEventRepo(db DBTX, maxEvents, pruneSlack int) *EventRepo {
	return &EventRepo{db: db, maxEvents: maxEvents, pruneSlack: pruneSlack}
}

// Record inserts an event and, if retention is configured, opportunistically
// prunes the oldest rows once the row count exceeds maxEvents+pruneSlack.
func (r *EventRepo) Record(ctx context.Context, evt *domain.Event) error {
	if evt.Payload == nil {
		return fmt.Errorf("event payload is nil")
	}
	if evt.Payload.EventKind() != evt.Type {
		return fmt.Errorf("event type %q does not match payload kind %q", evt.Type, evt.Payload.EventKind())
	}
	payload, err := json.Marshal(evt.Payload)
	if err != nil {
		return fmt.Errorf("marshaling event payload: %w", err)
	}
	_, err = r.db.ExecContext(ctx,
		`INSERT INTO events (id, event_type, entity_id, entity_kind, player_id, payload, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		evt.ID.String(), string(evt.Type), evt.EntityID, string(evt.EntityKind),
		nullableString(evt.PlayerID), string(payload),
		evt.CreatedAt.UTC().Format(timeFormat),
	)
	if err != nil {
		return fmt.Errorf("inserting event: %w", err)
	}
	return r.maybePrune(ctx)
}

// maybePrune enforces the lazy retention policy. It is a no-op when
// maxEvents is zero.
func (r *EventRepo) maybePrune(ctx context.Context) error {
	if r.maxEvents == 0 {
		return nil
	}
	var count int
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM events`).Scan(&count); err != nil {
		return fmt.Errorf("counting events: %w", err)
	}
	if count <= r.maxEvents+r.pruneSlack {
		return nil
	}
	toDelete := count - r.maxEvents
	_, err := r.db.ExecContext(ctx,
		`DELETE FROM events WHERE id IN (
			SELECT id FROM events ORDER BY created_at ASC, id ASC LIMIT ?
		)`, toDelete)
	if err != nil {
		return fmt.Errorf("pruning events: %w", err)
	}
	return nil
}

// Count returns the total number of rows in the events table.
func (r *EventRepo) Count(ctx context.Context) (int64, error) {
	var n int64
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM events`).Scan(&n); err != nil {
		return 0, fmt.Errorf("counting events: %w", err)
	}
	return n, nil
}

// PruneToSize deletes the oldest rows until the table contains at most
// maxRows rows, returning the number of rows deleted. Pruning the table
// down to zero is allowed (maxRows=0).
func (r *EventRepo) PruneToSize(ctx context.Context, maxRows int) (int64, error) {
	if maxRows < 0 {
		return 0, fmt.Errorf("maxRows must be non-negative, got %d", maxRows)
	}
	var count int
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM events`).Scan(&count); err != nil {
		return 0, fmt.Errorf("counting events: %w", err)
	}
	if count <= maxRows {
		return 0, nil
	}
	toDelete := count - maxRows
	res, err := r.db.ExecContext(ctx,
		`DELETE FROM events WHERE id IN (
			SELECT id FROM events ORDER BY created_at ASC, id ASC LIMIT ?
		)`, toDelete)
	if err != nil {
		return 0, fmt.Errorf("pruning events: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	return n, nil
}

// List returns events matching the filter, ordered by (created_at ASC, id ASC).
// Unknown event types decode into domain.UnknownPayload so callers can still
// round-trip rows whose kind they don't recognize.
func (r *EventRepo) List(ctx context.Context, f repository.EventFilter) ([]*domain.Event, error) {
	var (
		conds []string
		args  []any
	)
	if f.EntityKind != nil {
		conds = append(conds, "entity_kind = ?")
		args = append(args, string(*f.EntityKind))
	}
	if f.EntityID != nil {
		conds = append(conds, "entity_id = ?")
		args = append(args, *f.EntityID)
	}
	if f.Type != nil {
		conds = append(conds, "event_type = ?")
		args = append(args, string(*f.Type))
	}
	if f.Since != nil {
		conds = append(conds, "created_at >= ?")
		args = append(args, f.Since.UTC().Format(timeFormat))
	}
	if f.Until != nil {
		conds = append(conds, "created_at <= ?")
		args = append(args, f.Until.UTC().Format(timeFormat))
	}

	query := `SELECT ` + eventColumns + ` FROM events`
	if len(conds) > 0 {
		query += ` WHERE ` + strings.Join(conds, " AND ")
	}
	query += ` ORDER BY created_at ASC, id ASC`
	if f.Limit > 0 {
		query += ` LIMIT ?`
		args = append(args, f.Limit)
	}

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying events: %w", err)
	}
	defer rows.Close()

	events := make([]*domain.Event, 0)
	for rows.Next() {
		evt, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, evt)
	}
	return events, rows.Err()
}

type eventScanner interface {
	Scan(dest ...any) error
}

func scanEvent(s eventScanner) (*domain.Event, error) {
	var (
		idStr, eventType, entityID, entityKind, payloadJSON, createdAt string
		playerID                                                       sql.NullString
	)
	if err := s.Scan(&idStr, &eventType, &entityID, &entityKind, &playerID, &payloadJSON, &createdAt); err != nil {
		return nil, err
	}
	id, err := uuid.Parse(idStr)
	if err != nil {
		return nil, fmt.Errorf("parsing event id %q: %w", idStr, err)
	}
	t, err := time.Parse(timeFormat, createdAt)
	if err != nil {
		return nil, fmt.Errorf("parsing event created_at %q: %w", createdAt, err)
	}
	evt := &domain.Event{
		ID:         id,
		Type:       domain.EventType(eventType),
		EntityID:   entityID,
		EntityKind: domain.EntityKind(entityKind),
		CreatedAt:  t,
	}
	if playerID.Valid {
		pid := playerID.String
		evt.PlayerID = &pid
	}
	payload, err := decodePayload(evt.Type, []byte(payloadJSON))
	if err != nil {
		return nil, err
	}
	evt.Payload = payload
	return evt, nil
}

// decodePayload dispatches JSON unmarshaling by event type. Unrecognized types
// decode into domain.UnknownPayload so callers can still round-trip rows.
func decodePayload(kind domain.EventType, raw []byte) (domain.EventPayload, error) {
	switch kind {
	case domain.EventTaskCreated:
		var p domain.TaskCreatedPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("decoding %s payload: %w", kind, err)
		}
		return p, nil
	case domain.EventTaskModified:
		var p domain.TaskModifiedPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("decoding %s payload: %w", kind, err)
		}
		return p, nil
	case domain.EventStatusChanged:
		var p domain.StatusChangedPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("decoding %s payload: %w", kind, err)
		}
		return p, nil
	case domain.EventTaskStarted:
		var p domain.TaskStartedPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("decoding %s payload: %w", kind, err)
		}
		return p, nil
	case domain.EventTaskClaimed:
		var p domain.TaskClaimedPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("decoding %s payload: %w", kind, err)
		}
		return p, nil
	case domain.EventTaskReleased:
		var p domain.TaskReleasedPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("decoding %s payload: %w", kind, err)
		}
		return p, nil
	case domain.EventTaskCompleted:
		var p domain.TaskCompletedPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("decoding %s payload: %w", kind, err)
		}
		return p, nil
	case domain.EventTaskDeleted:
		var p domain.TaskDeletedPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("decoding %s payload: %w", kind, err)
		}
		return p, nil
	case domain.EventTaskPopped:
		var p domain.TaskPoppedPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("decoding %s payload: %w", kind, err)
		}
		return p, nil
	case domain.EventTaskMoved:
		var p domain.TaskMovedPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("decoding %s payload: %w", kind, err)
		}
		return p, nil
	case domain.EventRelationAdded:
		var p domain.RelationAddedPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("decoding %s payload: %w", kind, err)
		}
		return p, nil
	case domain.EventRelationRemoved:
		var p domain.RelationRemovedPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("decoding %s payload: %w", kind, err)
		}
		return p, nil
	default:
		m := map[string]any{}
		if err := json.Unmarshal(raw, &m); err != nil {
			return nil, fmt.Errorf("decoding unknown payload %s: %w", kind, err)
		}
		return domain.UnknownPayload{Kind: kind, Raw: m}, nil
	}
}
