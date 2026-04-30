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
func (repo *EventRepo) Record(ctx context.Context, event *domain.Event) error {
	if event.Payload == nil {
		return fmt.Errorf("event payload is nil")
	}

	if event.Payload.EventKind() != event.Type {
		return fmt.Errorf("event type %q does not match payload kind %q", event.Type, event.Payload.EventKind())
	}

	payloadJSON, marshalErr := json.Marshal(event.Payload)

	if marshalErr != nil {
		return fmt.Errorf("marshaling event payload: %w", marshalErr)
	}

	_, execErr := repo.db.ExecContext(ctx,
		`INSERT INTO events (id, event_type, entity_id, entity_kind, player_id, payload, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		event.ID.String(), string(event.Type), event.EntityID, string(event.EntityKind),
		nullableString(event.PlayerID), string(payloadJSON),
		event.CreatedAt.UTC().Format(timeFormat),
	)

	if execErr != nil {
		return fmt.Errorf("inserting event: %w", execErr)
	}

	return repo.maybePrune(ctx)
}

// maybePrune enforces the lazy retention policy. It is a no-op when
// maxEvents is zero.
func (repo *EventRepo) maybePrune(ctx context.Context) error {
	if repo.maxEvents == 0 {
		return nil
	}

	var count int

	if err := repo.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM events`).Scan(&count); err != nil {
		return fmt.Errorf("counting events: %w", err)
	}

	if count <= repo.maxEvents+repo.pruneSlack {
		return nil
	}

	toDelete := count - repo.maxEvents
	_, execErr := repo.db.ExecContext(ctx,
		`DELETE FROM events WHERE id IN (
			SELECT id FROM events ORDER BY created_at ASC, id ASC LIMIT ?
		)`, toDelete)

	if execErr != nil {
		return fmt.Errorf("pruning events: %w", execErr)
	}

	return nil
}

// Count returns the total number of rows in the events table.
func (repo *EventRepo) Count(ctx context.Context) (int64, error) {
	var count int64

	if err := repo.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM events`).Scan(&count); err != nil {
		return 0, fmt.Errorf("counting events: %w", err)
	}

	return count, nil
}

// PruneToSize deletes the oldest rows until the table contains at most
// maxRows rows, returning the number of rows deleted. Pruning the table
// down to zero is allowed (maxRows=0).
func (repo *EventRepo) PruneToSize(ctx context.Context, maxRows int) (int64, error) {
	if maxRows < 0 {
		return 0, fmt.Errorf("maxRows must be non-negative, got %d", maxRows)
	}

	var count int

	if err := repo.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM events`).Scan(&count); err != nil {
		return 0, fmt.Errorf("counting events: %w", err)
	}

	if count <= maxRows {
		return 0, nil
	}

	toDelete := count - maxRows
	res, execErr := repo.db.ExecContext(ctx,
		`DELETE FROM events WHERE id IN (
			SELECT id FROM events ORDER BY created_at ASC, id ASC LIMIT ?
		)`, toDelete)

	if execErr != nil {
		return 0, fmt.Errorf("pruning events: %w", execErr)
	}

	rowsAffected, rowsErr := res.RowsAffected()

	if rowsErr != nil {
		return 0, rowsErr
	}

	return rowsAffected, nil
}

// List returns events matching the filter, ordered by (created_at ASC, id ASC).
// Unknown event types decode into domain.UnknownPayload so callers can still
// round-trip rows whose kind they don't recognize.
func (repo *EventRepo) List(ctx context.Context, filter repository.EventFilter) ([]*domain.Event, error) {
	var (
		conds []string
		args  []any
	)

	if filter.EntityKind != nil {
		conds = append(conds, "entity_kind = ?")
		args = append(args, string(*filter.EntityKind))
	}

	if filter.EntityID != nil {
		conds = append(conds, "entity_id = ?")
		args = append(args, *filter.EntityID)
	}

	if filter.Type != nil {
		conds = append(conds, "event_type = ?")
		args = append(args, string(*filter.Type))
	}

	if filter.Since != nil {
		conds = append(conds, "created_at >= ?")
		args = append(args, filter.Since.UTC().Format(timeFormat))
	}

	if filter.Until != nil {
		conds = append(conds, "created_at <= ?")
		args = append(args, filter.Until.UTC().Format(timeFormat))
	}

	query := `SELECT ` + eventColumns + ` FROM events`

	if len(conds) > 0 {
		query += ` WHERE ` + strings.Join(conds, " AND ")
	}

	query += ` ORDER BY created_at ASC, id ASC`

	if filter.Limit > 0 {
		query += ` LIMIT ?`
		args = append(args, filter.Limit)
	}

	rows, queryErr := repo.db.QueryContext(ctx, query, args...)

	if queryErr != nil {
		return nil, fmt.Errorf("querying events: %w", queryErr)
	}

	defer rows.Close()

	events := make([]*domain.Event, 0)

	for rows.Next() {
		event, scanErr := scanEvent(rows)

		if scanErr != nil {
			return nil, scanErr
		}

		events = append(events, event)
	}

	return events, rows.Err()
}

type eventScanner interface {
	Scan(dest ...any) error
}

func scanEvent(scanner eventScanner) (*domain.Event, error) {
	var (
		idStr, eventType, entityID, entityKind, payloadJSON, createdAt string
		playerID                                                       sql.NullString
	)

	if err := scanner.Scan(&idStr, &eventType, &entityID, &entityKind, &playerID, &payloadJSON, &createdAt); err != nil {
		return nil, err
	}

	parsedID, parseIDErr := uuid.Parse(idStr)

	if parseIDErr != nil {
		return nil, fmt.Errorf("parsing event id %q: %w", idStr, parseIDErr)
	}

	parsedTime, parseTimeErr := time.Parse(timeFormat, createdAt)

	if parseTimeErr != nil {
		return nil, fmt.Errorf("parsing event created_at %q: %w", createdAt, parseTimeErr)
	}

	event := &domain.Event{
		ID:         parsedID,
		Type:       domain.EventType(eventType),
		EntityID:   entityID,
		EntityKind: domain.EntityKind(entityKind),
		CreatedAt:  parsedTime,
	}

	if playerID.Valid {
		pid := playerID.String
		event.PlayerID = &pid
	}

	payload, decodeErr := decodePayload(event.Type, []byte(payloadJSON))

	if decodeErr != nil {
		return nil, decodeErr
	}

	event.Payload = payload

	return event, nil
}

// decodePayload dispatches JSON unmarshaling by event type. Unrecognized types
// decode into domain.UnknownPayload so callers can still round-trip rows.
func decodePayload(kind domain.EventType, raw []byte) (domain.EventPayload, error) {
	switch kind {
	case domain.EventTaskCreated:
		var payload domain.TaskCreatedPayload

		if err := json.Unmarshal(raw, &payload); err != nil {
			return nil, fmt.Errorf("decoding %s payload: %w", kind, err)
		}

		return payload, nil
	case domain.EventTaskModified:
		var payload domain.TaskModifiedPayload

		if err := json.Unmarshal(raw, &payload); err != nil {
			return nil, fmt.Errorf("decoding %s payload: %w", kind, err)
		}

		return payload, nil
	case domain.EventStatusChanged:
		var payload domain.StatusChangedPayload

		if err := json.Unmarshal(raw, &payload); err != nil {
			return nil, fmt.Errorf("decoding %s payload: %w", kind, err)
		}

		return payload, nil
	case domain.EventTaskStarted:
		var payload domain.TaskStartedPayload

		if err := json.Unmarshal(raw, &payload); err != nil {
			return nil, fmt.Errorf("decoding %s payload: %w", kind, err)
		}

		return payload, nil
	case domain.EventTaskClaimed:
		var payload domain.TaskClaimedPayload

		if err := json.Unmarshal(raw, &payload); err != nil {
			return nil, fmt.Errorf("decoding %s payload: %w", kind, err)
		}

		return payload, nil
	case domain.EventTaskReleased:
		var payload domain.TaskReleasedPayload

		if err := json.Unmarshal(raw, &payload); err != nil {
			return nil, fmt.Errorf("decoding %s payload: %w", kind, err)
		}

		return payload, nil
	case domain.EventTaskCompleted:
		var payload domain.TaskCompletedPayload

		if err := json.Unmarshal(raw, &payload); err != nil {
			return nil, fmt.Errorf("decoding %s payload: %w", kind, err)
		}

		return payload, nil
	case domain.EventTaskDeleted:
		var payload domain.TaskDeletedPayload

		if err := json.Unmarshal(raw, &payload); err != nil {
			return nil, fmt.Errorf("decoding %s payload: %w", kind, err)
		}

		return payload, nil
	case domain.EventTaskPopped:
		var payload domain.TaskPoppedPayload

		if err := json.Unmarshal(raw, &payload); err != nil {
			return nil, fmt.Errorf("decoding %s payload: %w", kind, err)
		}

		return payload, nil
	case domain.EventTaskMoved:
		var payload domain.TaskMovedPayload

		if err := json.Unmarshal(raw, &payload); err != nil {
			return nil, fmt.Errorf("decoding %s payload: %w", kind, err)
		}

		return payload, nil
	case domain.EventRelationAdded:
		var payload domain.RelationAddedPayload

		if err := json.Unmarshal(raw, &payload); err != nil {
			return nil, fmt.Errorf("decoding %s payload: %w", kind, err)
		}

		return payload, nil
	case domain.EventRelationRemoved:
		var payload domain.RelationRemovedPayload

		if err := json.Unmarshal(raw, &payload); err != nil {
			return nil, fmt.Errorf("decoding %s payload: %w", kind, err)
		}

		return payload, nil
	case domain.EventWorkspaceImported:
		var payload domain.WorkspaceImportedPayload

		if err := json.Unmarshal(raw, &payload); err != nil {
			return nil, fmt.Errorf("decoding %s payload: %w", kind, err)
		}

		return payload, nil
	default:
		mapped := map[string]any{}

		if err := json.Unmarshal(raw, &mapped); err != nil {
			return nil, fmt.Errorf("decoding unknown payload %s: %w", kind, err)
		}

		return domain.UnknownPayload{Kind: kind, Raw: mapped}, nil
	}
}
