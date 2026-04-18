# Event Log — Design

**Status:** Draft — brainstorming output, not yet plan-reviewed.
**Milestone:** v0.13 (Roadmap Self-Host).
**Scope:** `ROADMAP.md:975-984` — Initiative: Event Log.
**Dependencies:** none. This ships before Data Portability in the same milestone; the v0.15 live dashboard and v0.16 undo initiatives both depend on this foundation.

## Goals

Give tusk an append-only, typed, entity-agnostic record of every mutation, emitted atomically with the mutation itself. This record underwrites:

- JSON export/import round-trips that preserve history (v0.13 Data Portability).
- Live dashboard deltas without re-querying state (v0.15).
- Single-step undo via event inversion (v0.16).
- Future webhook notifications and audit trails.

## Non-Goals (v0.13)

- Any user-facing read surface. No `tusk events list` CLI. No `tusk_events_list` MCP tool. Events are reachable in v0.13 only through `tusk export --format json`, which the Data Portability initiative adds later in the milestone.
- Emission from services other than `TaskService` and `RelationService`. Project, workflow, player, note, annotation, and tag mutations are out of scope for v0.13 shipping.
- Full before/after entity snapshots. Payloads carry only the action-intent fields needed to describe what happened.
- Time-based retention. v0.13 ships count-based retention only.
- Cross-workspace or cross-database event propagation.

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│ CLI (cmd/tusk) / MCP (internal/mcp) / Library (Client)          │
│   service.WithActor(ctx, playerID) set at the entry-point       │
└──────────────────────────────┬──────────────────────────────────┘
                               │ ctx carries actor
                               ▼
┌─────────────────────────────────────────────────────────────────┐
│ service.TaskService / service.RelationService                   │
│   Every mutation runs inside WriteTxProvider.WithTx(ctx, fn).   │
│   Inside fn: repo.Mutate(...); tx.Events().Record(ctx, evt).    │
│   evt.PlayerID = service.ActorFromContext(ctx).                 │
└──────────────────────────────┬──────────────────────────────────┘
                               │ Tx is a single *sql.Tx
                               ▼
┌─────────────────────────────────────────────────────────────────┐
│ repository.EventRepository (kind-agnostic)                      │
│   sqlite.EventRepo (record + list + count + prune)              │
│   sqlite.Tx.Events() returns a tx-scoped repo                   │
│   Lazy count-based retention runs inside Record                 │
└─────────────────────────────────────────────────────────────────┘
```

The framework is entity-agnostic: `EventType` and `EntityKind` are open string aliases. Future initiatives (project events, workflow events, player events, note events, …) add a sibling `domain/event_<kind>.go` file with their payload structs and call `tx.Events().Record` from their services. No repository, schema, or retention changes.

## Event type set (v0.13)

Action-intent based: the event type names the action the user took, not the field delta. One event per user-facing action, even when the action decomposes into multiple repo writes (e.g., `Pop` emits one `task_popped`, not a separate `task_claimed` + `task_started`).

| Type | Emitting service method | Payload fields |
|---|---|---|
| `task_created` | `TaskService.Create` | task snapshot: `task_id, short_id, title, status, priority, project_id, parent_id, order, tags` |
| `task_modified` | `TaskService.Update` (non-status fields) | `changes: {field: {from, to}, ...}` |
| `status_changed` | `TaskService.Update` (status field), auto-complete, auto-revert | `from, to, to_roles, source` where `source ∈ {"user", "auto_complete", "auto_revert"}` |
| `task_started` | `TaskService.Start` | `prev_status, auto_claimed` |
| `task_claimed` | `TaskService.Claim` (explicit) | `claimed_by` |
| `task_released` | `TaskService.Release` | `released_by` |
| `task_completed` | `TaskService.Complete` | `prev_status` |
| `task_deleted` | `TaskService.Delete` | `prev_status` |
| `task_popped` | `TaskService.Pop` | `claimed_by, prev_status` |
| `relation_added` | `RelationService.Add` | `source_short_id, target_short_id, kind` |
| `relation_removed` | `RelationService.Remove` | `source_short_id, target_short_id, kind` |

Deviation from the literal roadmap list (9 types → 11 types): `task_started` is added because Start is its own action, and `task_popped` is added because Pop is a distinct action the log must not force consumers to reconstruct by correlating `task_claimed + task_started`.

`Update` that changes both status and other fields emits one `status_changed` + one `task_modified` atomically in the same transaction.

`Complete` and `Delete` do **not** also emit `status_changed`. The action-specific event is the record of truth for terminal transitions; `status_changed` is reserved for non-terminal transitions and for auto-complete/auto-revert rollups.

## Schema

Migration `009_events.up.sql`:

```sql
CREATE TABLE events (
    id          TEXT PRIMARY KEY,   -- UUIDv7, time-ordered
    event_type  TEXT NOT NULL,      -- open registry; matches payload.kind
    entity_id   TEXT NOT NULL,      -- opaque: UUID string for tasks/relations, player ID for future player events, etc.
    entity_kind TEXT NOT NULL,      -- open registry: "task", "relation", future "project"/"workflow"/"player"/…
    player_id   TEXT,               -- nullable; actor from context, NULL for library/system callers
    payload     TEXT NOT NULL,      -- JSON-serialized typed payload struct; always includes a "kind" discriminator
    created_at  TEXT NOT NULL       -- ISO8601 UTC, same format as the rest of the schema
);

CREATE INDEX idx_events_created_at ON events(created_at);
CREATE INDEX idx_events_entity    ON events(entity_kind, entity_id, created_at);
CREATE INDEX idx_events_type      ON events(event_type, created_at);
```

Down migration drops the three indexes and the table.

## Domain types

`domain/event.go`:

```go
type EventType  string  // open string alias; constants ship, external callers may define more
type EntityKind string  // open string alias; predefined constants for shipping kinds

const (
    EntityTask     EntityKind = "task"
    EntityRelation EntityKind = "relation"
    // Future: EntityProject, EntityWorkflow, EntityPlayer, EntityNote, EntityAnnotation, EntityTag
)

type Event struct {
    ID         uuid.UUID
    Type       EventType
    EntityID   string        // string, not uuid.UUID, so non-UUID keys (player IDs) fit
    EntityKind EntityKind
    PlayerID   *string       // nil when no actor attached to context
    Payload    EventPayload
    CreatedAt  time.Time
}

// EventPayload is a sealed interface realized by every typed payload struct.
// The Record path asserts Event.Type == Payload.eventKind() so the stored
// discriminator cannot drift from the struct identity.
type EventPayload interface { eventKind() EventType }
```

`domain/event_task.go` — constants `EventTaskCreated … EventTaskPopped`, their payload structs, and per-event constructors (`domain.NewTaskCreatedEvent(task, actor)` etc.).

`domain/event_relation.go` — constants `EventRelationAdded`, `EventRelationRemoved`, their payload structs and constructors.

Each payload struct embeds `Kind EventType \`json:"kind"\`` matching its event type. JSON export serializes payloads self-describingly; a future importer can round-trip events whose payload shape it doesn't statically know.

## Repository interface

`repository/event.go`:

```go
type EventRepository interface {
    Record(ctx context.Context, evt *domain.Event) error
    List(ctx context.Context, f EventFilter) ([]*domain.Event, error)
    Count(ctx context.Context) (int64, error)
    PruneToSize(ctx context.Context, maxRows int) (deleted int64, err error)
}

type EventFilter struct {
    EntityKind *domain.EntityKind
    EntityID   *string
    Type       *domain.EventType
    Since      *time.Time
    Until      *time.Time
    Limit      int  // 0 = no limit
}
```

No method is task-specific or relation-specific. The same interface covers every current and future read path (JSON export, v0.15 dashboard deltas, v0.16 undo).

## SQLite implementation

`sqlite/event.go`:

```go
type EventRepo struct {
    db         DBTX
    maxEvents  int
    pruneSlack int
}

func NewEventRepo(db DBTX, maxEvents, pruneSlack int) *EventRepo { ... }
```

`Record` inserts the row, then calls `maybePrune(ctx)`:

```go
func (r *EventRepo) maybePrune(ctx context.Context) error {
    if r.maxEvents == 0 { return nil } // retention disabled
    var count int
    if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM events`).Scan(&count); err != nil {
        return fmt.Errorf("counting events: %w", err)
    }
    if count <= r.maxEvents + r.pruneSlack { return nil }
    toDelete := count - r.maxEvents
    _, err := r.db.ExecContext(ctx,
        `DELETE FROM events WHERE id IN (
            SELECT id FROM events ORDER BY created_at ASC, id ASC LIMIT ?
         )`, toDelete)
    return err
}
```

Prune runs inside the outer service transaction, so it is atomic with the mutation. `COUNT(*)` on a capped table (≤ maxEvents + pruneSlack ≈ 11_000 rows at defaults) is a few milliseconds. A triggered prune adds ~1–5 ms for ~1_000 deletions, amortized to roughly one extra millisecond per write.

`maxEvents == 0` disables retention entirely — escape hatch for library embedders managing retention externally.

## Tx extension

`sqlite/store.go`:

- Add `Events() *EventRepo` on `sqlite.Tx`, completing the `Tasks() / Relations() / Annotations() / Notes() / Tags() / Projects()` family.

`service/tx.go` (new file):

```go
type WriteTx interface {
    Tasks()     repository.TaskRepository
    Relations() repository.RelationRepository
    Events()    repository.EventRepository
    // Future services adopting event emission add their repos here.
}

type WriteTxProvider interface {
    WithTx(ctx context.Context, fn func(tx WriteTx) error) error
}
```

The concrete adapter in `cmd/tusk/main.go` and `client.go` wraps `*sqlite.Store.WithTx`, translating `*sqlite.Tx` into `service.WriteTx` via a small adapter struct (wrapping is needed because `sqlite.Tx` methods return concrete `*EventRepo` etc., while `WriteTx` is specified in terms of the repository interfaces).

`RepoBundle` gains `WriteTx WriteTxProvider`. Existing `WithTaskTx` / `WithRelationTx` / `WithProjectTx` helpers on `*sqlite.Store` stay — they serve code paths that don't emit events and don't need the broader `WriteTx` surface.

## Actor plumbing

`service/actor.go` (new file):

```go
type actorKey struct{}

func WithActor(ctx context.Context, playerID string) context.Context {
    if playerID == "" { return ctx }
    return context.WithValue(ctx, actorKey{}, playerID)
}

func ActorFromContext(ctx context.Context) *string {
    v, ok := ctx.Value(actorKey{}).(string)
    if !ok || v == "" { return nil }
    return &v
}
```

Entry points set the actor once at the boundary:

- **CLI** (`internal/tui/app.go`): when `a.playerID != ""`, wrap the root command's context in a `PersistentPreRun` hook: `ctx = service.WithActor(ctx, a.playerID)`.
- **MCP** (`internal/mcp/server.go`): every tool handler that accepts a `player_id` argument wraps its service-call context: `ctx = service.WithActor(ctx, args.PlayerID)`.
- **Library** (`tusk.Client`): library callers call `service.WithActor` themselves if they care about attribution. Events record `NULL` otherwise.
- **Auto-complete / auto-revert rollup**: the rollup inside `checkAutoComplete` / `checkAutoRevert` carries forward whatever actor is already on the triggering context. Differentiation from a user-driven status change is encoded in the event payload's `source` field (`"user" | "auto_complete" | "auto_revert"`), not in the actor.

## Service refactor pattern

Each mutating method in `TaskService` and `RelationService` moves its repo write inside `WriteTx.WithTx` and records the event in the same fn. Example:

```go
func (s *TaskService) Create(ctx context.Context, task *domain.Task) error {
    // ... unchanged validation and field population ...

    bundle, err := s.resolve(ctx, task.ProjectID)
    if err != nil { return fmt.Errorf("resolving project store: %w", err) }

    actor := ActorFromContext(ctx)
    return bundle.WriteTx.WithTx(ctx, func(tx WriteTx) error {
        if err := tx.Tasks().Create(ctx, task); err != nil { return err }
        return tx.Events().Record(ctx, domain.NewTaskCreatedEvent(task, actor))
    })
}
```

Same shape for `Update`, `Complete`, `Delete`, `Claim`, `Release`, `RelationService.Add`, `RelationService.Remove`. Two methods need more than a mechanical wrap:

- **`Start`** (auto-claim path): today calls claim semantics internally via separate repo calls. Refactored to do claim + status change in one `WithTx` and emit a single `task_started` event with `auto_claimed: true` in the payload. No separate `task_claimed` event.
- **`Pop`** (lines 830–851): today calls `s.Claim` then `s.Start` sequentially (two transactions). Refactored to run filter → claim → start inside one `WithTx` against the repo interfaces directly and emit a single `task_popped` event. The helper methods `s.Claim` and `s.Start` are left intact for direct callers.

## Configuration

`config/default.toml` gains:

```toml
[events]
max_events  = 10000
prune_slack = 1000
```

A matching `config.EventsConfig { MaxEvents, PruneSlack int }` threads through `tusk.Config` / `NewClient` / the CLI wiring to `sqlite.NewEventRepo(db, maxEvents, pruneSlack)`.

Zero values fall back to the defaults above. Setting `max_events = 0` disables retention entirely (escape hatch for library embedders).

## Testing

| Layer | File | Coverage |
|---|---|---|
| Repository | `sqlite/event_test.go` (new) | Insert roundtrip per event type; filter by entity kind/ID, type, time range; ordering by `created_at`; lazy prune triggers at threshold and deletes the correct batch; `maxEvents=0` disables pruning; `PruneToSize` trims correctly. |
| Service — Task | `service/task_test.go` (extended) | Each mutation emits exactly the expected event and payload. Cases: actor resolved from `WithActor(ctx, …)`; auto-complete emits `status_changed` with `source: "auto_complete"`; `Start` unclaimed emits `task_started` with `auto_claimed: true` (no separate `task_claimed`); `Pop` emits a single `task_popped`; `Update` with status + other fields emits two events atomically. |
| Service — Relation | `service/relation_test.go` (extended) | `Add` emits `relation_added`; `Remove` emits `relation_removed`; payloads carry `source_short_id, target_short_id, kind`. |
| Service — transactional invariant | `service/task_test.go` (new case) | When event insert fails (mocked `WriteTx`), the task mutation rolls back — asserted by reading back the task repo and confirming the row is not present. |
| E2E | `tests/e2e/event_log_test.go` (deferred to Data Portability) | Black-box scenario: `tusk task create → start → done → export --format json`, then assert the emitted events on the export payload. Lands with the Data Portability initiative in the same milestone; the Event Log initiative ships with repository + service coverage only. |

## Phase sequencing

The Event Log initiative is the first in v0.13 and ships with repository + service-level tests. End-to-end coverage lands with the Data Portability initiative's JSON export, which gains the `events` array and the `tests/e2e/event_log_test.go` scenario. The milestone exit criteria gate on full coverage — the gap closes inside v0.13.

## Open questions

None at design time. Decisions locked:

- Actor via context (vs. per-method parameter vs. NULL-only).
- Service-layer emission inside `WithTx` (vs. repo-layer vs. best-effort).
- Typed payload structs with `kind` discriminator (vs. snapshots vs. opaque JSON).
- Count-based lazy prune with slack (vs. prune-every-write vs. time-based).
- Action-intent event types, 11 total (vs. collapsed to `status_changed`).
- Generic entity-kind framework (vs. task/relation-only framework).
