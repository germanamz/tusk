# Phase 1 — Event Log Data Layer

**Design reference:** `docs/superpowers/specs/2026-04-18-event-log-design.md`.
**Milestone:** v0.13 — Initiative: Event Log.

## Prerequisites

None beyond the base codebase.

## Goal

Introduce the event table, domain types, repository interface, and SQLite implementation. After this phase the `events` table exists, an `EventRepository` can be constructed against a `DBTX`, and the repository is covered by unit tests. Nothing in the codebase emits events yet — the data layer is stand-alone.

The framework is kind-agnostic: `EventType` and `EntityKind` are open string aliases. Task and relation payload types ship now; future initiatives add sibling files (`domain/event_project.go`, `domain/event_player.go`, etc.) without touching anything in this phase.

## Tasks

### 1.1 — Migration `009_events.{up,down}.sql`

Create `migrations/009_events.up.sql`:

```sql
CREATE TABLE events (
    id          TEXT PRIMARY KEY,
    event_type  TEXT NOT NULL,
    entity_id   TEXT NOT NULL,
    entity_kind TEXT NOT NULL,
    player_id   TEXT,
    payload     TEXT NOT NULL,
    created_at  TEXT NOT NULL
);

CREATE INDEX idx_events_created_at ON events(created_at);
CREATE INDEX idx_events_entity     ON events(entity_kind, entity_id, created_at);
CREATE INDEX idx_events_type       ON events(event_type, created_at);
```

`entity_id` is `TEXT` (not UUID-typed) so future entity kinds with non-UUID identifiers (e.g., player IDs) fit without a schema change.

Create `migrations/009_events.down.sql` dropping the three indexes and the `events` table in reverse order.

Migrations are picked up automatically by `sqlite/store.go`'s migration loop — no Go wiring needed.

### 1.2 — `domain/event.go`

Create the generic core of the framework:

```go
package domain

import (
    "time"

    "github.com/google/uuid"
)

// EventType is an open string alias. Predefined constants for the event
// types shipping services emit live in domain/event_task.go and
// domain/event_relation.go. Future initiatives declare additional
// constants without touching this file.
type EventType string

// EntityKind is an open string alias identifying which entity an event
// describes. Predefined constants ship for the kinds that emit events
// today; future kinds add their own constants.
type EntityKind string

const (
    EntityTask     EntityKind = "task"
    EntityRelation EntityKind = "relation"
)

// Event is the generic event record stored in the events table.
type Event struct {
    ID         uuid.UUID
    Type       EventType
    EntityID   string      // opaque; UUID string for tasks/relations, arbitrary for future kinds
    EntityKind EntityKind
    PlayerID   *string     // nil when no actor is attached to context
    Payload    EventPayload
    CreatedAt  time.Time
}

// EventPayload is a sealed interface realized by every typed payload
// struct. The sqlite Record path asserts Event.Type == Payload.eventKind()
// so the stored discriminator cannot drift from the struct identity.
type EventPayload interface {
    eventKind() EventType
}
```

### 1.3 — Payload structs for tasks and relations

Create `domain/event_task.go` declaring the nine task-related event-type constants, their payload structs, and constructors that return `*Event`:

```go
package domain

import (
    "time"

    "github.com/google/uuid"
)

const (
    EventTaskCreated   EventType = "task_created"
    EventTaskModified  EventType = "task_modified"
    EventStatusChanged EventType = "status_changed"
    EventTaskStarted   EventType = "task_started"
    EventTaskClaimed   EventType = "task_claimed"
    EventTaskReleased  EventType = "task_released"
    EventTaskCompleted EventType = "task_completed"
    EventTaskDeleted   EventType = "task_deleted"
    EventTaskPopped    EventType = "task_popped"
)

type TaskCreatedPayload struct {
    Kind      EventType         `json:"kind"`      // = EventTaskCreated
    ShortID   string            `json:"short_id"`
    Title     string            `json:"title"`
    Status    string            `json:"status"`
    Priority  int               `json:"priority"`
    ProjectID string            `json:"project_id"`
    ParentID  *string           `json:"parent_id,omitempty"`
    Order     *float64          `json:"order,omitempty"`
    Tags      []string          `json:"tags,omitempty"`
}
func (TaskCreatedPayload) eventKind() EventType { return EventTaskCreated }

type FieldChange struct {
    From any `json:"from"`
    To   any `json:"to"`
}

type TaskModifiedPayload struct {
    Kind    EventType              `json:"kind"`    // = EventTaskModified
    ShortID string                 `json:"short_id"`
    Changes map[string]FieldChange `json:"changes"`
}
func (TaskModifiedPayload) eventKind() EventType { return EventTaskModified }

type StatusChangedPayload struct {
    Kind    EventType `json:"kind"`    // = EventStatusChanged
    ShortID string    `json:"short_id"`
    From    string    `json:"from"`
    To      string    `json:"to"`
    ToRoles []string  `json:"to_roles"`
    Source  string    `json:"source"` // "user" | "auto_complete" | "auto_revert"
}
func (StatusChangedPayload) eventKind() EventType { return EventStatusChanged }

type TaskStartedPayload struct {
    Kind        EventType `json:"kind"`    // = EventTaskStarted
    ShortID     string    `json:"short_id"`
    PrevStatus  string    `json:"prev_status"`
    AutoClaimed bool      `json:"auto_claimed"`
}
func (TaskStartedPayload) eventKind() EventType { return EventTaskStarted }

type TaskClaimedPayload struct {
    Kind      EventType `json:"kind"`    // = EventTaskClaimed
    ShortID   string    `json:"short_id"`
    ClaimedBy string    `json:"claimed_by"`
}
func (TaskClaimedPayload) eventKind() EventType { return EventTaskClaimed }

type TaskReleasedPayload struct {
    Kind       EventType `json:"kind"`    // = EventTaskReleased
    ShortID    string    `json:"short_id"`
    ReleasedBy string    `json:"released_by"`
}
func (TaskReleasedPayload) eventKind() EventType { return EventTaskReleased }

type TaskCompletedPayload struct {
    Kind       EventType `json:"kind"`    // = EventTaskCompleted
    ShortID    string    `json:"short_id"`
    PrevStatus string    `json:"prev_status"`
}
func (TaskCompletedPayload) eventKind() EventType { return EventTaskCompleted }

type TaskDeletedPayload struct {
    Kind       EventType `json:"kind"`    // = EventTaskDeleted
    ShortID    string    `json:"short_id"`
    PrevStatus string    `json:"prev_status"`
}
func (TaskDeletedPayload) eventKind() EventType { return EventTaskDeleted }

type TaskPoppedPayload struct {
    Kind       EventType `json:"kind"`    // = EventTaskPopped
    ShortID    string    `json:"short_id"`
    ClaimedBy  string    `json:"claimed_by"`
    PrevStatus string    `json:"prev_status"`
}
func (TaskPoppedPayload) eventKind() EventType { return EventTaskPopped }
```

Add constructor helpers in the same file, one per payload type. Each builds an `*Event` with a fresh UUIDv7 ID, fills `Type`, `EntityID = task.ID.String()`, `EntityKind = EntityTask`, `PlayerID = actor`, `CreatedAt = time.Now().UTC().Truncate(time.Millisecond)`. Signatures:

```go
func NewTaskCreatedEvent(task *Task, actor *string) *Event
func NewTaskModifiedEvent(task *Task, changes map[string]FieldChange, actor *string) *Event
func NewStatusChangedEvent(task *Task, from, to string, toRoles []string, source string, actor *string) *Event
func NewTaskStartedEvent(task *Task, prevStatus string, autoClaimed bool, actor *string) *Event
func NewTaskClaimedEvent(task *Task, claimedBy string, actor *string) *Event
func NewTaskReleasedEvent(task *Task, releasedBy string, actor *string) *Event
func NewTaskCompletedEvent(task *Task, prevStatus string, actor *string) *Event
func NewTaskDeletedEvent(task *Task, prevStatus string, actor *string) *Event
func NewTaskPoppedEvent(task *Task, claimedBy, prevStatus string, actor *string) *Event
```

Use `uuid.Must(uuid.NewV7())` for time-ordered IDs. If `github.com/google/uuid` does not expose `NewV7` in the currently pinned version, fall back to `uuid.New()` — the `created_at` index makes ID ordering non-load-bearing.

Create `domain/event_relation.go` with the two relation constants and payloads:

```go
package domain

const (
    EventRelationAdded   EventType = "relation_added"
    EventRelationRemoved EventType = "relation_removed"
)

type RelationAddedPayload struct {
    Kind          EventType `json:"kind"`    // = EventRelationAdded
    SourceShortID string    `json:"source_short_id"`
    TargetShortID string    `json:"target_short_id"`
    RelationKind  string    `json:"relation_kind"` // "blocks" | "relates_to" | "duplicates"
}
func (RelationAddedPayload) eventKind() EventType { return EventRelationAdded }

type RelationRemovedPayload struct {
    Kind          EventType `json:"kind"`    // = EventRelationRemoved
    SourceShortID string    `json:"source_short_id"`
    TargetShortID string    `json:"target_short_id"`
    RelationKind  string    `json:"relation_kind"`
}
func (RelationRemovedPayload) eventKind() EventType { return EventRelationRemoved }

func NewRelationAddedEvent(rel *Relation, sourceShortID, targetShortID string, actor *string) *Event { /* … */ }
func NewRelationRemovedEvent(rel *Relation, sourceShortID, targetShortID string, actor *string) *Event { /* … */ }
```

Both constructors set `EntityID = rel.ID.String()`, `EntityKind = EntityRelation`.

### 1.4 — Repository interface and SQLite implementation

Create `repository/event.go`:

```go
package repository

import (
    "context"
    "time"

    "github.com/germanamz/tusk/domain"
)

type EventFilter struct {
    EntityKind *domain.EntityKind
    EntityID   *string
    Type       *domain.EventType
    Since      *time.Time
    Until      *time.Time
    Limit      int // 0 = no limit
}

type EventRepository interface {
    Record(ctx context.Context, evt *domain.Event) error
    List(ctx context.Context, f EventFilter) ([]*domain.Event, error)
    Count(ctx context.Context) (int64, error)
    PruneToSize(ctx context.Context, maxRows int) (deleted int64, err error)
}
```

Create `sqlite/event.go`:

```go
package sqlite

import (
    "context"
    "database/sql"
    "encoding/json"
    "fmt"
    "time"

    "github.com/germanamz/tusk/domain"
    "github.com/germanamz/tusk/repository"
)

type EventRepo struct {
    db         DBTX
    maxEvents  int
    pruneSlack int
}

func NewEventRepo(db DBTX, maxEvents, pruneSlack int) *EventRepo {
    return &EventRepo{db: db, maxEvents: maxEvents, pruneSlack: pruneSlack}
}

func (r *EventRepo) Record(ctx context.Context, evt *domain.Event) error {
    if evt.Payload.eventKind() != evt.Type {
        return fmt.Errorf("event type %q does not match payload kind %q", evt.Type, evt.Payload.eventKind())
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

func (r *EventRepo) Count(ctx context.Context) (int64, error) { /* SELECT COUNT(*) */ }
func (r *EventRepo) PruneToSize(ctx context.Context, maxRows int) (int64, error) { /* parameterized delete, returns rows affected */ }
func (r *EventRepo) List(ctx context.Context, f repository.EventFilter) ([]*domain.Event, error) {
    // Build SQL with dynamic WHERE clauses for EntityKind, EntityID, Type, Since, Until.
    // ORDER BY created_at ASC, id ASC. Apply Limit if > 0.
    // Payload decoding: dispatch by event_type to the matching payload struct,
    // then json.Unmarshal. Unknown event types unmarshal into a generic
    // map[string]any wrapped in an UnknownPayload struct (add to event.go)
    // so future consumers can round-trip events whose kind they don't know.
}
```

List's payload dispatch requires a switch on `event_type`; enumerate the 11 known types (9 task + 2 relation) and fall through to an `UnknownPayload` map for anything else. Add `UnknownPayload` to `domain/event.go`:

```go
type UnknownPayload struct {
    Kind EventType      `json:"kind"`
    Raw  map[string]any `json:"-"` // decoded from the stored JSON
}
func (p UnknownPayload) eventKind() EventType { return p.Kind }
```

Use `nullableString` and `parseNullString`-style helpers already present in `sqlite/store.go`; the `timeFormat` constant there is the canonical format to reuse.

### 1.5 — Repository tests

Create `sqlite/event_test.go` using the existing `sqlitetest.NewStore(t)` helper:

1. **Roundtrip per event type.** For each of the 11 payload types, construct an `*Event` via the matching `domain.NewXxxEvent` constructor, call `Record`, then `List` with a filter targeting that event, and assert every field (including payload contents) matches.
2. **Filter coverage.** Insert a mix of task and relation events; assert `EventFilter` filters by `EntityKind`, `EntityID`, `Type`, `Since`, `Until`, and `Limit` work independently and combined.
3. **Ordering.** Assert `List` returns events in `created_at ASC, id ASC` order.
4. **Lazy prune trigger.** With `maxEvents=10, pruneSlack=3`: insert 12 events → count stays at 12; insert the 14th → lazy threshold exceeded, count drops to 10 and the oldest 4 are removed.
5. **Retention disabled.** With `maxEvents=0`: insert 100 events, assert count stays at 100.
6. **PruneToSize.** Insert 20 events, call `PruneToSize(ctx, 5)`, assert count==5 and the 5 newest survived.
7. **Type/payload mismatch rejected.** Construct an `Event` whose `Type` doesn't match `Payload.eventKind()` (manually, not via the constructor), assert `Record` returns an error and no row is inserted.

Use `t.Parallel()` where safe. Structure tests as table-driven where duplication warrants it.

## User-visible behavior preserved

- All existing CLI, MCP, and library behavior is unchanged: no service emits events yet, no new commands appear, and the shipping config schema is untouched.
- Existing test suites (`make test`, `make test-e2e`) continue to pass.
- `make build` and `make lint` pass.

## Changes introduced

- **New files:**
  - `migrations/009_events.up.sql`, `migrations/009_events.down.sql`
  - `domain/event.go`, `domain/event_task.go`, `domain/event_relation.go`
  - `repository/event.go`
  - `sqlite/event.go`, `sqlite/event_test.go`
- **New schema:** `events` table plus three indexes (see 1.1).
- **No modified interfaces.** Existing types, functions, and signatures untouched.
- **No new environment variables.** No config changes in this phase — the `[events]` config section lands in phase 2.
- **No new third-party dependencies.** Uses `github.com/google/uuid` (already pinned).
- **No bridge code.** Phase is self-contained.

## Acceptance criteria

- `make build`, `make vet`, `make lint` all pass.
- `go test ./sqlite/...` passes, including the new `event_test.go`.
- Full test suite (`make test`) passes; no regressions in any service or e2e test.
- Migration 009 applies cleanly to a fresh database and to an existing 008-level database. Rolling it down leaves no orphan indexes.
- `grep -r "EventRepository" service/` returns no matches (no service uses events yet — confirms phase isolation).
