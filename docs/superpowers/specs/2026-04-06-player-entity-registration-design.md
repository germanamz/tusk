# Player Entity & Registration — Design Spec

**Initiative:** v0.7 Player Management — Player Entity & Registration  
**Date:** 2026-04-06  
**Status:** Draft

---

## Overview

Add a Player entity to tusk that tracks which player (human or agent) is working on which task. Players self-register and claim tasks, preventing overlapping work. This spec covers the first initiative of v0.7: the player entity, storage, CLI commands, and MCP tools for registration and claiming.

---

## Domain Model

### Player Entity

| Field           | Type     | Description                                   |
| --------------- | -------- | --------------------------------------------- |
| `id`            | string   | Primary key. Self-declared unique identifier.  |
| `type`          | string   | `"human"` or `"agent"`. Immutable after creation. |
| `registered_at` | datetime | Set once on creation.                          |
| `last_seen_at`  | datetime | Updated on player actions.                     |

No UUID, no short ID, no version field. Player IDs are self-declared strings. No mutations beyond `last_seen_at`, so no optimistic locking needed.

### Task Additions

Two new nullable fields on `Task`:

| Field        | Type               | Description                         |
| ------------ | ------------------ | ----------------------------------- |
| `claimed_by` | `*string`          | FK to Player ID. Who holds the claim. |
| `claimed_at` | `*time.Time`       | When the claim was made.            |

In `TaskUpdate`, these use the double-pointer pattern:
- `ClaimedBy **string` — nil = don't change, `*nil` = clear, `*"value"` = set
- `ClaimedAt **time.Time` — same

### Error Additions

- `ErrTaskClaimed = errors.New("task is already claimed by another player")` — generic, does not leak claimant identity.

---

## Repository Interfaces

### PlayerRepository (new)

```go
type PlayerRepository interface {
    Create(ctx context.Context, player *domain.Player) error
    GetByID(ctx context.Context, id string) (*domain.Player, error)
    UpdateLastSeen(ctx context.Context, id string) error
    List(ctx context.Context) ([]*domain.Player, error)
}
```

Minimal interface. No `Update`, no `Delete`. `Create` returns `domain.ErrConflict` if ID already exists.

### TaskRepository (unchanged)

`ClaimedBy`/`ClaimedAt` flow through the existing `Update` method via `TaskUpdate` struct. The existing `List` with filters handles new filter fields.

---

## Storage — SQLite

### Migration `002_players.up.sql`

```sql
CREATE TABLE players (
    id TEXT PRIMARY KEY,
    type TEXT NOT NULL CHECK (type IN ('human', 'agent')),
    registered_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    last_seen_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

ALTER TABLE tasks ADD COLUMN claimed_by TEXT REFERENCES players(id);
ALTER TABLE tasks ADD COLUMN claimed_at TEXT;

CREATE INDEX idx_tasks_claimed_by ON tasks(claimed_by);
```

### Migration `002_players.down.sql`

Drop columns (SQLite rename-recreate pattern) and drop `players` table.

### `internal/sqlite/player.go` (new)

Implements `PlayerRepository`. Follows existing patterns:
- `DBTX` interface for DB/Tx compatibility
- `const playerColumns` for SELECT
- `scanPlayer` helper for row-to-domain mapping
- Time values formatted via `timeFormat` constant

### `internal/sqlite/task.go` (modified)

- Add `claimed_by`, `claimed_at` to column constants and scan helpers
- Handle nullable fields using existing `nullableString`/`nullableTime` patterns
- Update `buildUpdateQuery` for `ClaimedBy`/`ClaimedAt` double-pointer fields

---

## Service Layer

### PlayerService (new — `internal/service/player.go`)

```go
type PlayerService struct {
    repo repository.PlayerRepository
}
```

Methods:
- `Register(ctx, id, playerType string) (*Player, error)` — validates type is `"human"` or `"agent"`, calls `repo.Create`.
- `GetByID(ctx, id string) (*Player, error)` — passthrough.
- `UpdateLastSeen(ctx, id string) error` — passthrough.
- `List(ctx) ([]*Player, error)` — passthrough.

### TaskService (modified)

**New dependency:** `playerRepo repository.PlayerRepository` — for player existence validation and `last_seen_at` updates.

**New methods:**
- `Claim(ctx, shortID, playerID string, version int) (*Task, error)` — get task, verify unclaimed (or same player), set `claimed_by`/`claimed_at`, update with version check.
- `Release(ctx, shortID, playerID string, version int) (*Task, error)` — get task, verify caller is the claimant, clear `claimed_by`/`claimed_at`.

**Modified methods:**
- `Start()` — new optional `playerID` parameter. If non-empty: auto-register (as default type for caller context), auto-claim if unclaimed, return `ErrTaskClaimed` if claimed by another. If empty: works as before.
- `Complete()` and `Delete()` — **no changes**. Claims are preserved as historical attribution.

**`last_seen_at` updates:** When `playerID` is non-empty on mutation methods, call `playerRepo.UpdateLastSeen`.

---

## CLI Interface

### Global Flag

`--player <id>` on the root Cobra command. Parsed in `cmd/tusk/main.go`. Threaded to commands that need it.

### New Commands

- `tusk player register <id> --type human|agent` — explicit registration. `--type` defaults to `"agent"`.
- `tusk claim <short_id>` — requires `--player`. Calls `TaskService.Claim`.
- `tusk release <short_id>` — requires `--player`. Calls `TaskService.Release`.

### Modified Commands

- `tusk start` — accepts optional `--player`. If provided: auto-registers (as `"human"` for CLI auto-register), passes to `TaskService.Start` for auto-claim. If omitted: works as before.
- `tusk list` / `tusk info` / `tusk tree` — display `claimed_by` and `claimed_at` when present. `--player` on reads is a filter shorthand for `claimed_by:<id>`.

### Auto-Register Behavior

On `start` and `claim`, if `--player` is provided and the player doesn't exist, auto-register as `"human"` before proceeding. CLI-only convenience.

### Filter Additions

- `claimed_by:<player_id>` — tasks claimed by a specific player
- `unclaimed:true` — tasks where `claimed_by IS NULL`

---

## MCP Tools

### New Tools

**`tusk_player_register`** (group: `"player"`):
- Input: `player_id` (required string). Type hardcoded to `"agent"`.
- Calls `PlayerService.Register`.

**`tusk_task_claim`** (group: `"task"`):
- Input: `short_id`, `player_id`, `version` (all required).
- Calls `TaskService.Claim`.

**`tusk_task_release`** (group: `"task"`):
- Input: `short_id`, `player_id`, `version` (all required).
- Calls `TaskService.Release`.

### Modified Tools

- `tusk_task_start` — add optional `player_id`. If provided: auto-registers as `"agent"`, passes to `TaskService.Start` for auto-claim. If omitted: works as before.
- All task tool **responses** include `claimed_by` and `claimed_at` in JSON output.

### `player_id` on Read Tools (opt-in liveness)

`tusk_task_list`, `tusk_task_get`, `tusk_task_tree` accept optional `player_id`. If provided, calls `PlayerService.UpdateLastSeen`. No auto-register on reads.

### Filter Support

`claimed_by:<player_id>` and `unclaimed:true` work in the filter string parameter, same as CLI.

---

## Testing

### Unit Tests

- `internal/service/player_test.go` — Register (happy, duplicate ID, invalid type), GetByID, UpdateLastSeen.
- `internal/service/task_test.go` — Claim (happy, already claimed, version conflict), Release (happy, not claimant), Start with auto-claim, Start when claimed by another.

### SQLite Repo Tests

- `internal/sqlite/player_test.go` — CRUD operations, duplicate ID conflict, UpdateLastSeen.

### E2E Tests

- `tests/e2e/player_test.go`:
  - `tusk player register` with explicit type
  - `tusk claim` / `tusk release` flow
  - `tusk start --player` auto-claim + auto-register
  - `tusk start` on already-claimed task (expect error)
  - `claimed_by` visible in `tusk info` output
  - `claimed_by:<id>` and `unclaimed:true` filters
  - Claim preserved after `tusk done`

### MCP E2E Tests

- `tests/e2e/mcp_player_test.go`:
  - `tusk_player_register` tool
  - `tusk_task_claim` / `tusk_task_release`
  - `tusk_task_start` with `player_id` auto-claim
  - `player_id` on read tools updates `last_seen_at`

---

## Design Decisions

| Decision | Chosen | Rejected | Rationale |
| --- | --- | --- | --- |
| Player ID type | Self-declared string | UUID | Consumers own naming. No resolution burden. |
| Player type mutability | Immutable after registration | Updatable | No use case for changing type. Simpler model. |
| Claim logic location | TaskService | Separate ClaimService / PlayerService | Claiming is a task state mutation, colocated with other task transitions. |
| Error message on claimed | Generic ("already claimed by another player") | Include claimant identity | Self-declared IDs have no auth — leaking identity enables impersonation of release. |
| Claim on complete/delete | Preserved as historical attribution | Auto-released | Completed/deleted tasks don't compete for attention. Claim is useful as a record of who did the work. |
| `--player` source | Flag only | Flag + env + config | Keep it simple. Agents use MCP `player_id` per-call. |
| `--type` default | `"agent"` | `"human"` | Auto-register path is more likely scripted/agent-driven. Explicit `tusk player register` for humans. |
| MCP player type | Hardcoded `"agent"` | Configurable | MCP callers are agents by definition. |
| `player_id` on reads | Opt-in (updates `last_seen_at` if provided) | Required / ignored | Best liveness signal without forcing it on simple queries. |
