# Task 2.1 — `file_state` table and `FileStateRepo` CRUD

**Phase:** 2
**Spec:** `docs/superpowers/specs/2026-05-26-tusk-concurrent-instances.md`
**Plan:** `docs/superpowers/plans/2026-05-26-concurrent-instances/PLAN.md`
**Prereqs:** Phase 1 complete.
**Ships as:** one PR.

## Execution Rules

1. **This task = one PR.**
2. **Build green, tests green, lint clean.** Lefthook pre-commit runs the
   full Go test suite.
3. **No production code consumes `FileStateRepo` in this task.** Handlers
   and reindex will start using it in Phase 4 and Phase 6 respectively.
   The PR adds the table, the repo type, the CRUD methods, and unit tests
   for the repo — nothing else.
4. **No bridge code introduced.**

## Goal

Create the `file_state` SQLite table (schema defined in the spec
§ *`file_state` — per-file coordination table*) and a `FileStateRepo` type
exposing the CRUD needed by later tasks. Bump `SchemaVersion` so existing
indexes drop-and-rebuild on first open after upgrade.

## Scope

### Files to add

- `internal/index/file_state_repo.go` — new file. Mirrors the style of
  `internal/index/embed_queue_repo.go`:
  - `FileStateRow` struct matching every column on the table.
  - `FileStateRepo` type backed by `*sql.DB`, constructed via
    `NewFileStateRepo(idx *Index) *FileStateRepo`.
  - Methods needed by later phases:
    - `Get(path string) (*FileStateRow, error)`
    - `Upsert(row FileStateRow) error` — used by handlers and reindex to
      record observed state.
    - `Tombstone(path string) error` — soft-delete: sets
      `state = 'tombstone'` and updates `updated_at_ns`. The row stays
      in the table as an audit trail; this is the deletion convention
      for the entire work stream (matches the spec's explicit
      `'live' | 'tombstone'` schema). Do **not** add a hard-delete
      method; reap and `node_delete` use this.
    - `ListByGenLessThan(gen int64) ([]FileStateRow, error)` — used by
      Phase 6's reap. Returns only rows with `state = 'live'`;
      tombstones are excluded.
  - Lease-specific methods (`Claim`, `Release`, stale-temp cleanup) live
    in T2.3, not here. Keep this task narrow.

- `internal/index/file_state_repo_test.go` — unit tests for each method
  above against a fresh in-memory or `t.TempDir`-backed SQLite database.
  Use the same test helper pattern as `embed_queue_repo_test.go` (check
  whether one exists; if not, copy its construction style).

### Files to modify

- `internal/index/index.go` — extend the `CREATE TABLE` block (around
  line 80 based on the spec's reference) with the `file_state` table
  schema as written in the spec § *`file_state` — per-file coordination
  table*. Include both indexes (`idx_file_state_lease`,
  `idx_file_state_seen`). Keep DDL in the same statement block as the
  existing tables so the rebuild flow picks them up.
- `internal/index/schema_version.go` — bump the `SchemaVersion` constant
  string. Suggested value: `"2026-05-26-file-state"` or similar — any
  string that differs from the current value triggers the rebuild path.
  Update the doc comment with a one-line note: *"adds file_state for
  per-file coordination"*.

### Tests

- Unit tests in the new `_test.go` file cover Get / Upsert / Delete /
  ListByGenLessThan against a fresh DB.
- Integration coverage is already provided by
  `internal/workspace/indexopen/integration_test.go` — a test there
  verifies a mismatched `SchemaVersion` triggers rebuild. After the bump,
  that test continues to pass because the rebuild emits the new
  `SchemaVersion` value. No change required to that test.

## Verification

1. `make build` — compiles.
2. `make test` — all tests pass, including the new `FileStateRepo` tests
   and the existing `indexopen` rebuild test.
3. `make vet`, `make lint`, `make fmt` — clean.
4. Manual smoke: build the binary, open a pre-existing workspace from a
   prior version, observe the rebuild log line. Re-open; no rebuild
   should occur the second time.

## Out of Scope

- The lease primitive (`Claim` / `Release` / cleanup) — that's T2.3.
- Any handler change — that's Phase 4.
- Populating `file_state` from existing nodes during rebuild — the table
  starts empty; Phase 4's `WriteWithLease` helper lazy-creates a
  placeholder row on first write to any path (via `INSERT … ON CONFLICT
  DO NOTHING` inside the helper, see T4.1), and Phase 6 reindex
  populates the rest during the walk.
- `pending_temp_path` cleanup behavior — defined in T2.3 alongside
  `Claim`.

## Notes for the Implementer

- The spec's schema declares `last_seen_gen INTEGER NOT NULL DEFAULT 0`.
  That default lets handlers Upsert rows without caring about the reindex
  generation; Phase 6 sets it explicitly when walking.
- `state` is a string column with values `'live' | 'tombstone'`. No CHECK
  constraint is required at this stage; rely on the repo to write valid
  values. Tombstones are soft deletes — the row stays in the table.
- `content_hash` is record-keeping for the watcher's dedup, **not** a CAS
  token. Do not introduce CAS-style `WHERE content_hash = ?expected`
  patterns in this task. The spec § *Optimistic concurrency* explicitly
  rejects that.
