# Data Portability — Phase 1: Foundations (WriteTx + event payload)

**Initiative:** v0.13 — Data Portability
**Spec:** `docs/superpowers/specs/2026-04-26-data-portability-design.md`
**Phase:** 1 of 4
**Prerequisites:** None — this phase runs against the base codebase on branch `feat/data-portability`.
**Can run in parallel with:** Phase 2 (codec package). Phase 2 touches only `internal/portability/`; Phase 1 touches `service/`, `sqlite/`, `domain/`, `client.go`. Zero overlap.

---

## Why this phase

The Data Portability initiative needs two foundational primitives before any business logic can be written:

1. **Atomic multi-entity writes.** `service.WriteTx` currently surfaces only `Tasks()`, `Relations()`, and `Events()`. Import has to upsert workflows, projects, players, tags, tasks, relations, annotations, notes, and events inside one SQLite transaction. Without WriteTx exposing the missing repos, the import service cannot guarantee atomicity.
2. **A typed event payload for the `workspace_imported` envelope event.** The import emits one event covering the whole import, with counts and provenance metadata. The event log infrastructure already supports new payload types — the additions go in `domain/event_portability.go` next to `event_task.go` and `event_relation.go`.

Both primitives are pure additions. They touch no behavior in shipping code paths and unblock Phases 3 and 4 without exposing any new CLI or MCP surface.

---

## Tasks

### Task 1 — Extend `service.WriteTx` with the six missing entity accessors and `TruncateAll`

**File:** `service/tx.go`

Add six entity accessors plus one `TruncateAll` method to the `WriteTx` interface:

```go
import "context" // already imported elsewhere in the package

type WriteTx interface {
    Tasks() repository.TaskRepository
    Relations() repository.RelationRepository
    Events() repository.EventRepository

    // New in v0.13 — required by the portability service for atomic
    // multi-entity imports. Existing callers that don't need these
    // accessors can ignore them.
    Projects() repository.ProjectRepository
    Workflows() repository.WorkflowRepository
    Players() repository.PlayerRepository
    Tags() repository.TagRepository
    Annotations() repository.AnnotationRepository
    Notes() repository.NoteRepository

    // TruncateAll wipes every entity table inside the current
    // transaction in reverse-FK order. Used exclusively by the
    // PortabilityService under --replace --truncate. Returns the first
    // error encountered, leaving the transaction's rollback policy to
    // the caller.
    TruncateAll(ctx context.Context) error
}
```

Update the comment on `WriteTx` (currently "Phase 2 declares only the repositories v0.13 services need…") to drop the "Phase 2" framing — the interface now spans every entity kind portability touches plus a workspace-wide truncate.

### Task 2 — Add `Workflows()`, `Players()`, and `TruncateAll()` on the sqlite `Tx`

**File:** `sqlite/store.go`

The sqlite `Tx` struct already exposes `Tasks()`, `Relations()`, `Annotations()`, `Notes()`, `Tags()`, `Projects()`, and `Events()`. Add three methods next to those:

```go
// Workflows returns a WorkflowRepo operating within this transaction.
func (t *Tx) Workflows() *WorkflowRepo { return NewWorkflowRepo(t.tx) }

// Players returns a PlayerRepo operating within this transaction.
func (t *Tx) Players() *PlayerRepo { return NewPlayerRepo(t.tx) }

// TruncateAll wipes every entity table inside this transaction in
// reverse-FK order. Used exclusively by the PortabilityService under
// --replace --truncate. The order is:
//   events, notes, annotations, relations, task_tags,
//   tasks, projects, workflows, tags, players
//
// Each DELETE is issued as a raw `DELETE FROM <table>` against
// t.tx — no per-row WHERE clauses, no version checks. The single
// transaction wrapping the call rolls everything back atomically on
// any error.
func (t *Tx) TruncateAll(ctx context.Context) error {
    tables := []string{
        "events", "notes", "annotations", "relations", "task_tags",
        "tasks", "projects", "workflows", "tags", "players",
    }
    for _, table := range tables {
        if _, err := t.tx.ExecContext(ctx, "DELETE FROM " + table); err != nil {
            return fmt.Errorf("truncating %s: %w", table, err)
        }
    }
    return nil
}
```

Both `NewWorkflowRepo` and `NewPlayerRepo` already accept the `DBTX` interface that `*sql.Tx` satisfies, so no other changes are needed for the entity accessors.

If a table name in the list above doesn't match the actual schema (verify against `migrations/*.up.sql`), correct it during implementation. The reverse-FK order is what matters — children before parents.

You will need to import `"context"` and `"fmt"` if they aren't already present in `sqlite/store.go`.

### Task 3 — Update the `sqliteWriteTx` adapter in `client.go`

**File:** `client.go`

The `sqliteWriteTx` struct in `client.go` adapts `*sqlite.Tx` to `service.WriteTx`. It currently implements `Tasks()`, `Relations()`, and `Events()` only. Add the six new accessors plus `TruncateAll`:

```go
func (w *sqliteWriteTx) Projects() repository.ProjectRepository       { return w.tx.Projects() }
func (w *sqliteWriteTx) Workflows() repository.WorkflowRepository     { return w.tx.Workflows() }
func (w *sqliteWriteTx) Players() repository.PlayerRepository         { return w.tx.Players() }
func (w *sqliteWriteTx) Tags() repository.TagRepository               { return w.tx.Tags() }
func (w *sqliteWriteTx) Annotations() repository.AnnotationRepository { return w.tx.Annotations() }
func (w *sqliteWriteTx) Notes() repository.NoteRepository             { return w.tx.Notes() }

func (w *sqliteWriteTx) TruncateAll(ctx context.Context) error        { return w.tx.TruncateAll(ctx) }
```

Each delegates straight to the matching method on `w.tx` (which is `*sqlite.Tx` from Task 2).

### Task 4 — Update test stubs that implement `WriteTx`

**Files (all under `service/`):**
- `service/bundle_helpers_test.go`
- `service/task_claim_test.go`
- `service/task_tx_invariant_test.go`

Each of these declares a `testWriteTx` (or similarly named) struct that satisfies `service.WriteTx`. Without updates they'll break compilation of the test binary because the interface grew by seven methods (six entity accessors + `TruncateAll`).

For each file, locate the struct literal or type that implements `WriteTx` and add the seven new methods. Two implementation patterns appear in the existing code:

- **Pattern A (delegating to `*sqlite.Tx`):** add the same seven delegation lines as in `client.go` Task 3 — the test stub wraps a real `*sqlite.Tx`, so it can delegate.
- **Pattern B (failing or no-op stub):** the test wants the stub to not actually use these repos. Implement each method with a `panic("unimplemented: <method> not used in this test")` so any accidental call surfaces loudly.

Pick whichever pattern matches each file's existing approach. Look at how `Events()` is currently implemented in each test stub; mirror that pattern for the seven new methods.

After updating all three files, run `make test` and confirm everything still compiles and passes. If any test fails because of the new methods, the failure is in the test setup, not in production code — fix the stub.

### Task 5 — Add `domain/event_portability.go`

**New file:** `domain/event_portability.go`

```go
package domain

import (
	"time"
)

const (
	// EventWorkspaceImported is emitted exactly once per `tusk import`
	// invocation, inside the apply transaction, recording what the import
	// did at a workspace-wide level. Per-entity events are not emitted —
	// the dump's existing event log already captures per-entity history.
	EventWorkspaceImported EventType = "workspace_imported"

	// EntityWorkspace identifies workspace-scoped events that don't belong
	// to a single task or relation. The event's EntityID for an
	// EventWorkspaceImported is the empty string.
	EntityWorkspace EntityKind = "workspace"
)

// WorkspaceImportedPayload describes a completed import operation.
// Counts are keyed by entity kind ("tasks", "projects", …) and report the
// number of rows inserted or updated by the import, including under
// --replace. Replaced and Truncated reflect the ImportOptions used.
type WorkspaceImportedPayload struct {
	Kind          EventType      `json:"kind"`
	SchemaVersion int            `json:"schema_version"`
	SourceTuskVer string         `json:"source_tusk_version"`
	ExportedAt    time.Time      `json:"exported_at"`
	Replace       bool           `json:"replace"`
	Truncate      bool           `json:"truncate"`
	Counts        map[string]int `json:"counts"`
}

// EventKind satisfies the EventPayload interface.
func (WorkspaceImportedPayload) EventKind() EventType { return EventWorkspaceImported }
```

Keep the file shape parallel to `domain/event_task.go` and `domain/event_relation.go`. Do not add a constructor (`NewWorkspaceImportedEvent`) yet — the constructor lives in `service/portability.go` in Phase 3 because it needs access to the import options and counts that only the service knows.

### Task 6 — Round-trip test for `WorkspaceImportedPayload`

**New file:** `domain/event_portability_test.go`

The existing event-payload contract is: a payload struct can be JSON-encoded, persisted via `EventRepository.Record`, listed back via `EventRepository.List`, and the listed payload's `EventKind()` matches the original `Type`. Add a single test that exercises this for `WorkspaceImportedPayload`:

```go
package domain_test

import (
	"testing"
	"time"

	"github.com/germanamz/tusk/domain"
)

func TestWorkspaceImportedPayload_EventKind(t *testing.T) {
	p := domain.WorkspaceImportedPayload{
		Kind:          domain.EventWorkspaceImported,
		SchemaVersion: 1,
		SourceTuskVer: "v0.13.0",
		ExportedAt:    time.Date(2026, 4, 26, 12, 0, 0, 0, time.UTC),
		Replace:       true,
		Truncate:      false,
		Counts:        map[string]int{"tasks": 42, "projects": 1},
	}
	if got := p.EventKind(); got != domain.EventWorkspaceImported {
		t.Fatalf("EventKind() = %q, want %q", got, domain.EventWorkspaceImported)
	}
}
```

A full repo round-trip test belongs in Phase 3 (where the service emits the event); this phase only needs to confirm the type satisfies the `EventPayload` interface. If `domain` already has a pattern for testing payload structs (look at `domain/event_task.go` neighbors), follow that pattern.

---

## Acceptance criteria

The implementer agent should treat these as hard gates:

1. `make build` succeeds.
2. `make test` succeeds — every existing test still passes; the new payload test passes.
3. `make vet` and `make lint` succeed.
4. `service.WriteTx` exposes ten methods total: the original three, plus six entity accessors and `TruncateAll(ctx)` added in Task 1. Any code that implements the interface (production or test) compiles.
5. `*sqlite.Tx` exposes `Workflows()`, `Players()`, and `TruncateAll(ctx)`.
6. `domain.EventWorkspaceImported`, `domain.EntityWorkspace`, and `domain.WorkspaceImportedPayload` are exported and documented.
7. **No behavior change visible to CLI or MCP users.** Running every existing `tusk` command should produce identical output to before this phase.

---

## User-visible behavior preserved

- All existing `tusk` CLI commands (every subcommand under `tusk task`, `tusk project`, `tusk workflow`, `tusk tag`, `tusk player`, `tusk note`, `tusk config`, `tusk mcp`, `tusk completion`, `tusk version`) work identically.
- All existing MCP tools and resources work identically.
- Existing event log entries continue to round-trip through `EventRepository.List` exactly as before.
- The library `Client` API is unchanged (no new fields yet — those land in Phase 3).

---

## Changes Introduced

**New files:**
- `domain/event_portability.go` — `EventWorkspaceImported`, `EntityWorkspace`, `WorkspaceImportedPayload`.
- `domain/event_portability_test.go` — `EventKind()` assertion for `WorkspaceImportedPayload`.

**Modified files:**
- `service/tx.go` — `WriteTx` interface gains six entity accessors plus `TruncateAll(ctx)`. Comment on the interface updated to drop Phase 2 framing.
- `sqlite/store.go` — `*Tx` gains `Workflows()`, `Players()`, and `TruncateAll(ctx)` methods.
- `client.go` — `sqliteWriteTx` gains seven delegation methods.
- `service/bundle_helpers_test.go`, `service/task_claim_test.go`, `service/task_tx_invariant_test.go` — test stubs gain seven methods each (delegating or panicking; mirror existing per-file pattern).

**Modified interfaces:**
- `service.WriteTx` adds `Projects()`, `Workflows()`, `Players()`, `Tags()`, `Annotations()`, `Notes()`, and `TruncateAll(ctx context.Context) error`. Pre-existing methods unchanged.

**No new env vars, no schema migration, no new dependencies, no bridge code.**
