# Phase 2 — Shared Wiring: WriteTx, Actor Context, Config

**Design reference:** `docs/superpowers/specs/2026-04-18-event-log-design.md`.
**Milestone:** v0.13 — Initiative: Event Log.

## Prerequisites

Phase 1 (Event Log Data Layer) must be complete. Specifically, this phase relies on:

- `migrations/009_events.*` applying cleanly.
- `domain/event.go`, `domain/event_task.go`, `domain/event_relation.go` providing the `Event` type, `EventType`/`EntityKind` aliases, payload structs, and the 11 event constructors.
- `repository/event.go` exposing the `EventRepository` interface and `EventFilter`.
- `sqlite/event.go` exposing `NewEventRepo(db DBTX, maxEvents, pruneSlack int) *EventRepo`.

## Inherits from phase 1

- The `events` table exists in the schema but is never written to — no service emits events yet.
- `sqlite.EventRepo` can be constructed directly but is not wired into `RepoBundle`, `Client`, or the CLI's `cmd/tusk/main.go` wiring.
- Service layer is behaviorally unchanged.

## Goal

Install the plumbing that phases 3–5 will use to emit events atomically with repo writes. After this phase:

- `sqlite.Tx` exposes `Events() *EventRepo`.
- A service-level `WriteTx` interface bundles every repository any future emitting service needs inside a single transaction; `WriteTxProvider` is the interface services depend on.
- `service.WithActor` / `service.ActorFromContext` propagate the acting player through `context.Context`.
- CLI (`--player`) and MCP (`player_id` tool argument) set the actor at their entry points.
- The `[events]` configuration section exists with defaults and is threaded through `tusk.Config` / `NewClient` / `cmd/tusk/main.go` into `NewEventRepo`.
- `RepoBundle` gains a `WriteTx` field; the library and CLI both populate it. Existing `RepoBundle.Store` and the legacy `WithTaskTx`/`WithRelationTx`/`WithProjectTx` helpers stay in place — no service has been refactored to use `WriteTx` yet.

Nothing emits events at the end of this phase. The phase is a no-op from any user's perspective.

## Tasks

### 2.1 — `sqlite.Tx.Events()` accessor

In `sqlite/store.go`, add an `Events()` method to `Tx` alongside the existing `Tasks() / Relations() / Annotations() / Notes() / Tags() / Projects()`:

```go
// Events returns an EventRepo operating within this transaction. The
// retention parameters (maxEvents, pruneSlack) are attached at tx time
// because they are transaction-scoped policy, not repository-scoped.
func (t *Tx) Events(maxEvents, pruneSlack int) *EventRepo {
    return NewEventRepo(t.tx, maxEvents, pruneSlack)
}
```

The retention knobs are passed per call because `Tx` does not hold configuration. Phase 2 wiring (task 2.4) captures them in the `WriteTx` adapter so callers never pass them manually.

No changes to `WithTx`, `WithTaskTx`, `WithRelationTx`, `WithProjectTx` — they remain for paths that don't emit events.

### 2.2 — `service.WriteTx` and `service.WriteTxProvider`

Create `service/tx.go`:

```go
package service

import (
    "context"

    "github.com/germanamz/tusk/repository"
)

// WriteTx exposes every repository that mutating services may need inside
// a single transaction, plus the event repository for atomic emission.
//
// Phase 2 declares only the repositories v0.13 services need. As future
// initiatives adopt event emission they will add their repo accessors
// here (Projects, Workflows, Players, Notes, Annotations, Tags).
type WriteTx interface {
    Tasks()     repository.TaskRepository
    Relations() repository.RelationRepository
    Events()    repository.EventRepository
}

// WriteTxProvider runs fn inside a shared transaction whose repositories
// all write through the same *sql.Tx. Commits on nil return; rolls back
// otherwise.
type WriteTxProvider interface {
    WithTx(ctx context.Context, fn func(tx WriteTx) error) error
}
```

### 2.3 — `service/actor.go`

Create `service/actor.go`:

```go
package service

import "context"

type actorKey struct{}

// WithActor returns a context carrying the given player ID. An empty
// playerID returns ctx unchanged — ActorFromContext will surface nil,
// matching the "no actor" case.
func WithActor(ctx context.Context, playerID string) context.Context {
    if playerID == "" {
        return ctx
    }
    return context.WithValue(ctx, actorKey{}, playerID)
}

// ActorFromContext returns the actor attached to ctx, or nil if none.
// The returned *string is safe to pass directly into an Event.PlayerID
// field (which is *string, NULL-safe).
func ActorFromContext(ctx context.Context) *string {
    v, ok := ctx.Value(actorKey{}).(string)
    if !ok || v == "" {
        return nil
    }
    return &v
}
```

Add a small unit test `service/actor_test.go` verifying:

- `ActorFromContext(context.Background())` returns `nil`.
- `WithActor(ctx, "")` returns the same ctx and `ActorFromContext` still yields `nil`.
- `WithActor(ctx, "german")` → `ActorFromContext` returns a non-nil `*string` equal to `"german"`.

### 2.4 — Adapter wiring, `RepoBundle.WriteTx`, and `[events]` config

**Config struct additions (`config/config.go`).** Add after `NotesConfig`:

```go
// EventsConfig controls event-log retention.
type EventsConfig struct {
    // MaxEvents is the steady-state target row count in the events table.
    // Zero disables retention entirely — the library embedder's escape hatch.
    MaxEvents int `mapstructure:"max_events" toml:"max_events" json:"max_events"`
    // PruneSlack allows the count to grow to MaxEvents+PruneSlack before
    // a single insert triggers a batch delete down to MaxEvents.
    PruneSlack int `mapstructure:"prune_slack" toml:"prune_slack" json:"prune_slack"`
}
```

Add an `Events EventsConfig` field to `Config` (matching the pattern of `Urgency`, `Notes`):

```go
Events EventsConfig `mapstructure:"events" toml:"events" json:"events"`
```

**Defaults (`config/default.toml`).** Append:

```toml
[events]
max_events  = 10000   # Steady-state event count; 0 disables retention
prune_slack = 1000    # Allow count to grow this far past max_events before pruning
```

**`tusk.Config` threading (`client.go`).** Add an `Events config.EventsConfig` field to `tusk.Config` next to `Urgency` and `Notes`. Propagate defaults in `NewClient` when the field is zero-valued (e.g., `if cfg.Events == (config.EventsConfig{}) { cfg.Events = config.EventsConfig{MaxEvents: 10000, PruneSlack: 1000} }`).

**Adapter struct.** Add to `service/tx.go` a constructor that produces a `WriteTxProvider` backed by `*sqlite.Store`:

```go
// adapter types must live alongside the sqlite package since they reference
// *sqlite.Store; put the adapter itself in cmd/tusk/main.go and client.go
// where the store is already imported. See below.
```

Concretely: add a private adapter type in **both** `cmd/tusk/main.go` and `client.go` (they can't share code without dragging `sqlite` into `service`, which would invert the dependency). The adapter is ~20 lines of boilerplate; a small amount of duplication is cheaper than a new package.

Adapter shape (use the same definition in both files, or factor into an `internal/storetx` package if either file would grow unwieldy):

```go
type sqliteWriteTxProvider struct {
    store      *sqlite.Store
    maxEvents  int
    pruneSlack int
}

type sqliteWriteTx struct {
    tx         *sqlite.Tx
    maxEvents  int
    pruneSlack int
}

func (w *sqliteWriteTx) Tasks() repository.TaskRepository         { return w.tx.Tasks() }
func (w *sqliteWriteTx) Relations() repository.RelationRepository { return w.tx.Relations() }
func (w *sqliteWriteTx) Events() repository.EventRepository {
    return w.tx.Events(w.maxEvents, w.pruneSlack)
}

func (p *sqliteWriteTxProvider) WithTx(ctx context.Context, fn func(tx service.WriteTx) error) error {
    return p.store.WithTx(ctx, func(stx *sqlite.Tx) error {
        return fn(&sqliteWriteTx{tx: stx, maxEvents: p.maxEvents, pruneSlack: p.pruneSlack})
    })
}
```

**`RepoBundle` field.** In `service/repos.go`, add a field:

```go
type RepoBundle struct {
    Store       *sqlite.Store
    WriteTx     WriteTxProvider
    Tasks       repository.TaskRepository
    Annotations repository.AnnotationRepository
    Notes       repository.NoteRepository
    Relations   repository.RelationRepository
    Tags        repository.TagRepository
    Players     repository.PlayerRepository
}
```

**Update bundle constructors:**

- `client.go`: populate `WriteTx` on the bundle using the adapter built from `cfg.Events`.
- `cmd/tusk/main.go`: populate `WriteTx` in every `RepoBundle` construction site (there may be one per project store — instrument all of them).
- `service/bundle_helpers_test.go`: update `bundleFromStore(store)` to also set `WriteTx` using the adapter with test-friendly defaults (e.g., `MaxEvents: 10000, PruneSlack: 1000`).
- `service/tag_test.go` (line 81) and `service/task_claim_test.go` (line 22): existing places that build a `RepoBundle{…}` literally — set `WriteTx` to nil is acceptable here since these tests don't exercise emission paths. If any test later calls into a mutation path through this bundle, the nil deref becomes its signal to migrate to `bundleFromStore` or supply a fake.

### 2.5 — CLI and MCP actor propagation

**CLI (`internal/tui/app.go`).** In `a.root.PersistentPreRun` (or equivalent; create one if none exists), wrap the command context when `a.playerID != ""`:

```go
a.root.PersistentPreRun = func(cmd *cobra.Command, _ []string) {
    if a.playerID != "" {
        cmd.SetContext(service.WithActor(cmd.Context(), a.playerID))
    }
}
```

If the root already has a `PersistentPreRun` or `PersistentPreRunE`, splice the actor-wrapping into it. Do not override anything it already does.

**MCP (`internal/mcp/server.go`).** Every tool handler that accepts a `player_id` argument already extracts it into a local variable before calling the service. Immediately before the service call, add:

```go
ctx = service.WithActor(ctx, playerID)
```

Repeat for every tool in the file that has `player_id` in its schema. `grep -n "player_id" internal/mcp/server.go` enumerates the relevant sites; expect on the order of 6–10. There is no global middleware layer to centralize this on today — threading per handler is accepted.

**Library (`client.go`).** No changes. Library callers call `service.WithActor` themselves if they care about attribution. Events record `NULL` player_id otherwise.

## User-visible behavior preserved

- All CLI commands behave identically to before. `--player` continues to do exactly what it did (claim/release operations, player auto-registration) — the only added effect is that a `service.WithActor` context is set, which is inert until phase 3.
- All MCP tool calls behave identically. The added `WithActor` wrap is inert.
- Every previously passing test continues to pass — no service logic changes.
- `make build`, `make vet`, `make lint` pass.
- Running `tusk config show` on a fresh install displays the new `[events]` section with its defaults; existing config files without the section still load (Viper fills defaults).

## Changes introduced

- **New files:**
  - `service/tx.go` (WriteTx interface + WriteTxProvider interface)
  - `service/actor.go`, `service/actor_test.go`
- **Modified files:**
  - `sqlite/store.go` — `Tx.Events(maxEvents, pruneSlack int) *EventRepo` accessor.
  - `config/config.go` — `EventsConfig` struct; `Config.Events` field.
  - `config/default.toml` — new `[events]` section.
  - `client.go` — `Config.Events`, default population, adapter + bundle wiring.
  - `cmd/tusk/main.go` — adapter + bundle wiring.
  - `service/repos.go` — `RepoBundle.WriteTx` field.
  - `service/bundle_helpers_test.go` — update `bundleFromStore` to populate `WriteTx`.
  - `internal/tui/app.go` — root `PersistentPreRun` wraps ctx with `WithActor`.
  - `internal/mcp/server.go` — per-handler `WithActor` wrap for tools with `player_id`.
- **Modified interfaces:** `RepoBundle` gains a field. This is additive — existing code that constructs `RepoBundle{…}` literals keeps compiling (new field defaults to nil). Tests covering mutation paths that will later emit events (phase 3+) are already routed through `bundleFromStore`, so they pick up the adapter automatically.
- **New environment variables:** `TUSK_EVENTS_MAX_EVENTS`, `TUSK_EVENTS_PRUNE_SLACK` (via Viper's prefix convention, automatic — no extra wiring).
- **No new third-party dependencies.**
- **No bridge code.** Phase ends with `WriteTx` exposed but unused by any service. Phase 3 begins consuming it.

## Acceptance criteria

- `make build`, `make vet`, `make lint` all pass.
- Full test suite (`make test`) passes.
- `tusk config show` prints the `[events]` section with defaults (`max_events = 10000`, `prune_slack = 1000`).
- `TUSK_EVENTS_MAX_EVENTS=42 tusk config get events.max_events` prints `42` (Viper env-var plumbing works).
- A smoke test (`go test ./... -run WithTx -count=1`) demonstrates the adapter: constructing a `WriteTx` from a test store, opening a transaction, calling `tx.Events().Record(...)` and `tx.Tasks().Create(...)` inside the same fn, and committing — both rows land atomically. Add this as `service/tx_test.go` with one test function.
- `grep -rn "bundle.WriteTx" service/` returns no matches (no service consumes it yet — confirms phase isolation).
